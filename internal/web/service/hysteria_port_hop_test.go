package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/porthop"
)

func hopStream(ports string) string {
	return fmt.Sprintf(`{"network":"hysteria","finalmask":{"quicParams":{"udpHop":{"ports":%q}}}}`, ports)
}

func fakeHopApply(t *testing.T, fn func([]porthop.Rule) error) {
	t.Helper()
	old := applyHysteriaHop
	applyHysteriaHop = func(_ context.Context, rules []porthop.Rule, _ porthop.Runner) error { return fn(rules) }
	t.Cleanup(func() { applyHysteriaHop = old })
}

func TestHysteriaHopLifecycle(t *testing.T) {
	setupConflictDB(t)
	var applied []porthop.Rule
	fakeHopApply(t, func(rules []porthop.Rule) error { applied = append([]porthop.Rule(nil), rules...); return nil })
	svc := &InboundService{}
	ib := &model.Inbound{Enable: true, Tag: "hop-test", Port: 443, Protocol: model.Hysteria, Settings: `{"clients":[]}`, StreamSettings: hopStream("20000-30000")}
	if _, _, err := svc.AddInbound(ib); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0].Target != 443 {
		t.Fatalf("add: %v", applied)
	}
	ib.Port = 8443
	ib.StreamSettings = hopStream("40000-50000")
	if _, _, err := svc.UpdateInbound(ib); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0].Target != 8443 || applied[0].Ports.First != 40000 {
		t.Fatalf("update: %v", applied)
	}
	if _, err := svc.SetInboundEnable(ib.Id, false); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("disable: %v", applied)
	}
	if _, err := svc.SetInboundEnable(ib.Id, true); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 {
		t.Fatalf("enable: %v", applied)
	}
	applied = nil
	if err := ReconcileHysteriaPortHopping(); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 {
		t.Fatal("restart/firewall reset did not restore rules")
	}
	ib.StreamSettings = `{"network":"hysteria"}`
	if _, _, err := svc.UpdateInbound(ib); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("turning UDP Hop off: %v", applied)
	}
	ib.StreamSettings = hopStream("40000-50000")
	if _, _, err := svc.UpdateInbound(ib); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 {
		t.Fatalf("turning UDP Hop back on: %v", applied)
	}
	if _, err := svc.DelInbound(ib.Id); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("delete: %v", applied)
	}
}

func TestHysteriaHopConflicts(t *testing.T) {
	base := model.Inbound{Id: 1, Enable: true, Port: 443, Protocol: model.Hysteria, StreamSettings: hopStream("20000-30000")}
	for _, tc := range []struct {
		name     string
		other    model.Inbound
		conflict bool
	}{
		{"overlapping hops", model.Inbound{Id: 2, Enable: true, Port: 8443, Protocol: model.Hysteria, StreamSettings: hopStream("25000-35000")}, true},
		{"UDP listener", model.Inbound{Id: 2, Enable: true, Port: 25000, Protocol: model.TUIC}, true},
		{"TCP listener", model.Inbound{Id: 2, Enable: true, Port: 25000, Protocol: model.VLESS}, false},
		{"disabled listener", model.Inbound{Id: 2, Enable: false, Port: 25000, Protocol: model.TUIC}, false},
		{"remote listener", model.Inbound{Id: 2, Enable: true, Port: 25000, Protocol: model.TUIC, NodeID: new(int)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := collectHysteriaHopRules([]model.Inbound{base, tc.other})
			if (err != nil) != tc.conflict {
				t.Fatalf("err: %v", err)
			}
		})
	}
	a, b := base, base
	a.Listen = "192.0.2.1"
	b.Listen = "192.0.2.2"
	b.Id = 2
	if _, err := collectHysteriaHopRules([]model.Inbound{a, b}); err != nil {
		t.Fatal(err)
	}
}

func TestHysteriaHopSaveFailureIsExplicitAndRetryable(t *testing.T) {
	setupConflictDB(t)
	fail := true
	fakeHopApply(t, func([]porthop.Rule) error {
		if fail {
			return errors.New("missing CAP_NET_ADMIN")
		}
		return nil
	})
	ib := &model.Inbound{Enable: true, Port: 443, Protocol: model.Hysteria, Settings: `{"clients":[]}`, StreamSettings: hopStream("20000-30000")}
	_, _, err := (&InboundService{}).AddInbound(ib)
	if err == nil || !strings.Contains(err.Error(), "was saved") {
		t.Fatalf("error: %v", err)
	}
	var stored model.Inbound
	if err := database.GetDB().First(&stored, ib.Id).Error; err != nil {
		t.Fatal(err)
	}
	fail = false
	if err := ReconcileHysteriaPortHopping(); err != nil {
		t.Fatal(err)
	}
}

func TestHysteriaHopConcurrentConflictingAdds(t *testing.T) {
	setupConflictDB(t)
	fakeHopApply(t, func([]porthop.Rule) error { return nil })
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := (&InboundService{}).AddInbound(&model.Inbound{Enable: true, Tag: fmt.Sprintf("hop-%d", i), Port: 443 + i, Protocol: model.Hysteria, Settings: `{"clients":[]}`, StreamSettings: hopStream("20000-30000")})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	winners := 0
	for err := range errs {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("got %d successful conflicting adds", winners)
	}
}

func TestHysteriaHopRejectEnableWithoutChangingDatabase(t *testing.T) {
	setupConflictDB(t)
	fakeHopApply(t, func([]porthop.Rule) error { return nil })
	seedInboundConflict(t, "owner", "", 25000, model.TUIC, `{}`, `{"clients":[]}`)
	ib := model.Inbound{Enable: false, Tag: "disabled-hop", Port: 443, Protocol: model.Hysteria, Settings: `{"clients":[]}`, StreamSettings: hopStream("20000-30000")}
	if err := database.GetDB().Create(&ib).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&InboundService{}).SetInboundEnable(ib.Id, true); err == nil {
		t.Fatal("enabled conflicting hop range")
	}
	if err := database.GetDB().First(&ib, ib.Id).Error; err != nil {
		t.Fatal(err)
	}
	if ib.Enable {
		t.Fatal("rejected enable was persisted")
	}
}
