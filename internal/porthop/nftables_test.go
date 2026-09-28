package porthop

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestScriptAddressScopes(t *testing.T) {
	for _, tc := range []struct {
		listen string
		v6     bool
		want   string
	}{
		{"", false, "fib daddr type local udp dport 20000-30000 redirect to :443"},
		{"::", true, "meta nfproto ipv6 udp dport"},
		{"192.0.2.1", false, "ip daddr 192.0.2.1 udp dport 20000-30000 dnat ip to 192.0.2.1:443"},
		{"2001:db8::1", false, "ip6 daddr 2001:db8::1 udp dport 20000-30000 dnat ip6 to [2001:db8::1]:443"},
	} {
		script, err := Script([]Rule{{InboundID: 1, Ports: Range{20000, 30000}, Target: 443, Listen: tc.listen, IPv6Only: tc.v6}}, true)
		if err != nil || !strings.Contains(script, tc.want) || !strings.HasPrefix(script, "delete table inet "+Table+"\n") {
			t.Fatalf("script %s, err %v", script, err)
		}
	}
	for _, addr := range []string{"127.0.0.1", "::1", "localhost", "1.2.3.4; flush ruleset"} {
		if _, err := Script([]Rule{{Ports: Range{1, 2}, Target: 443, Listen: addr}}, false); err == nil {
			t.Fatalf("accepted %s", addr)
		}
	}
}

func TestApplyLifecycle(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			calls := 0
			var rules []Rule
			if enabled {
				rules = []Rule{{InboundID: 1, Ports: Range{20000, 30000}, Target: 443}}
			}
			err := Apply(context.Background(), rules, func(ctx context.Context, input string, args ...string) ([]byte, error) {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
				if calls == 1 {
					if input != "" || strings.Join(args, " ") != "-j list tables" {
						t.Fatal("unexpected discovery")
					}
					if exists {
						return []byte(`{"nftables":[{"table":{"family":"inet","name":"xui_hysteria_hop"}}]}`), nil
					}
					return []byte(`{"nftables":[{"table":{"family":"inet","name":"other_firewall"}}]}`), nil
				}
				if calls > 2 || strings.Join(args, " ") != "-f -" {
					t.Fatal("not a single atomic batch")
				}
				if strings.Contains(input, "other_firewall") || strings.Contains(input, "flush ruleset") {
					t.Fatal("touched foreign rules")
				}
				if strings.Contains(input, "delete table") != exists {
					t.Fatal("incorrect cleanup")
				}
				if strings.Contains(input, "redirect to :443") != enabled {
					t.Fatal("incorrect rule state")
				}
				return nil, nil
			})
			want := 2
			if !exists && !enabled {
				want = 1
			}
			if err != nil || calls != want {
				t.Fatalf("calls %d, err %v", calls, err)
			}
		}
	}
}

func TestApplyFailureDoesNotRetryNonAtomically(t *testing.T) {
	calls := 0
	err := Apply(context.Background(), []Rule{{Ports: Range{20000, 30000}, Target: 443}}, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte(`{"nftables":[{"table":{"family":"inet","name":"xui_hysteria_hop"}}]}`), nil
		}
		return []byte("operation not permitted"), errors.New("exit 1")
	})
	if err == nil || calls != 2 || !strings.Contains(err.Error(), "operation not permitted") {
		t.Fatalf("calls %d, err %v", calls, err)
	}
}
