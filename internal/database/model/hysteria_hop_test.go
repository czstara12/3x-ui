package model

import (
	"strings"
	"testing"
)

func TestHysteriaHopMaskOnlyRemovedFromRuntime(t *testing.T) {
	raw := `{"network":"hysteria","finalmask":{"udp":[{"type":"salamander","settings":{"password":"test"}},{"type":"udphop","settings":{"remotePorts":"20000-30000"}}]}}`
	ib := Inbound{Protocol: Hysteria, Port: 443, Settings: `{"version":2}`, StreamSettings: raw}
	runtime := ib.GenXrayInboundConfig()
	if strings.Contains(string(runtime.StreamSettings), "udphop") || !strings.Contains(string(runtime.StreamSettings), "salamander") {
		t.Fatalf("runtime masks: %s", runtime.StreamSettings)
	}
	if ib.StreamSettings != raw {
		t.Fatal("changed persisted/client configuration")
	}
}
