package porthop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

const Table = "xui_hysteria_hop"

type Rule struct {
	InboundID int
	Ports     Range
	Target    int
	Listen    string
	IPv6Only  bool
}

type Runner func(context.Context, string, ...string) ([]byte, error)

func command(ctx context.Context, input string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "nft", args...)
	cmd.Stdin = strings.NewReader(input)
	return cmd.CombinedOutput()
}

func Script(rules []Rule, exists bool) (string, error) {
	var b strings.Builder
	if exists {
		fmt.Fprintf(&b, "delete table inet %s\n", Table)
	}
	if len(rules) == 0 {
		return b.String(), nil
	}
	fmt.Fprintf(&b, "table inet %s {\n chain prerouting {\n  type nat hook prerouting priority dstnat; policy accept;\n", Table)
	for _, r := range rules {
		if r.Ports.First < 1 || r.Ports.Last > 65535 || r.Ports.First > r.Ports.Last || r.Target < 1 || r.Target > 65535 {
			return "", fmt.Errorf("invalid rule for inbound %d", r.InboundID)
		}
		match, action := "", fmt.Sprintf("redirect to :%d", r.Target)
		addr := strings.TrimSpace(r.Listen)
		if addr != "" {
			ip := net.ParseIP(addr)
			if ip == nil || ip.IsLoopback() || ip.IsMulticast() {
				return "", fmt.Errorf("unsupported UDP hop listen address %q", addr)
			}
			if !ip.IsUnspecified() {
				if ip.To4() != nil {
					match = "ip daddr " + ip.String() + " "
					action = fmt.Sprintf("dnat ip to %s:%d", ip.String(), r.Target)
				} else {
					match = "ip6 daddr " + ip.String() + " "
					action = fmt.Sprintf("dnat ip6 to [%s]:%d", ip.String(), r.Target)
				}
			}
		}
		if match == "" && r.IPv6Only {
			match = "meta nfproto ipv6 "
		}
		ports := fmt.Sprint(r.Ports.First)
		if r.Ports.First != r.Ports.Last {
			ports = fmt.Sprintf("%d-%d", r.Ports.First, r.Ports.Last)
		}
		fmt.Fprintf(&b, "  fib daddr type local %sudp dport %s %s comment \"3x-ui inbound %d\"\n", match, ports, action, r.InboundID)
	}
	b.WriteString(" }\n}\n")
	return b.String(), nil
}

func Apply(ctx context.Context, rules []Rule, run Runner) error {
	if _, err := Script(rules, false); err != nil {
		return err
	}
	if run == nil {
		if _, err := exec.LookPath("nft"); err != nil {
			if len(rules) == 0 {
				return nil
			}
			return fmt.Errorf("automatic Hysteria port hopping requires nftables (nft): %w", err)
		}
		run = command
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := run(ctx, "", "-j", "list", "tables")
	if err != nil {
		return fmt.Errorf("list nftables tables (requires CAP_NET_ADMIN): %w: %s", err, bytes.TrimSpace(out))
	}
	var listing struct {
		NFTables []struct {
			Table *struct{ Family, Name string } `json:"table"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &listing); err != nil {
		return fmt.Errorf("decode nftables tables: %w", err)
	}
	exists := false
	for _, entry := range listing.NFTables {
		if entry.Table != nil && entry.Table.Family == "inet" && entry.Table.Name == Table {
			exists = true
		}
	}
	script, err := Script(rules, exists)
	if err != nil || script == "" {
		return err
	}
	out, err = run(ctx, script, "-f", "-")
	if err != nil {
		return fmt.Errorf("apply Hysteria port hopping atomically: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}
