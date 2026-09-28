package porthop

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Range struct{ First, Last int }

func Parse(raw string) ([]Range, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("empty UDP hop ports")
	}
	if len(raw) > 8192 {
		return nil, fmt.Errorf("UDP hop ports exceed 8192 characters")
	}
	var ranges []Range
	for _, item := range strings.Split(raw, ",") {
		parts := strings.Split(strings.TrimSpace(item), "-")
		if len(parts) > 2 {
			return nil, fmt.Errorf("invalid UDP hop range %q", item)
		}
		values := make([]int, len(parts))
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				return nil, fmt.Errorf("invalid UDP hop range %q", item)
			}
			for _, c := range p {
				if c < '0' || c > '9' {
					return nil, fmt.Errorf("invalid UDP hop port %q", p)
				}
			}
			v, err := strconv.Atoi(p)
			if err != nil || v < 1 || v > 65535 {
				return nil, fmt.Errorf("invalid UDP hop port %q", p)
			}
			values[i] = v
		}
		r := Range{values[0], values[len(values)-1]}
		if r.First > r.Last {
			return nil, fmt.Errorf("reversed UDP hop range %q", item)
		}
		ranges = append(ranges, r)
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].First < ranges[j].First })
	merged := ranges[:0]
	for _, r := range ranges {
		if len(merged) > 0 && r.First <= merged[len(merged)-1].Last+1 {
			if r.Last > merged[len(merged)-1].Last {
				merged[len(merged)-1].Last = r.Last
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged, nil
}

func FromStream(raw string) ([]Range, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var stream struct {
		FinalMask struct {
			UDP []struct {
				Type     string `json:"type"`
				Settings struct {
					RemotePorts string `json:"remotePorts"`
				} `json:"settings"`
			} `json:"udp"`
			QuicParams struct {
				UDPHop *struct {
					Ports string `json:"ports"`
				} `json:"udpHop"`
			} `json:"quicParams"`
		} `json:"finalmask"`
	}
	if err := json.Unmarshal([]byte(raw), &stream); err != nil {
		return nil, fmt.Errorf("invalid Hysteria stream settings: %w", err)
	}
	for _, mask := range stream.FinalMask.UDP {
		if mask.Type == "udphop" {
			return Parse(mask.Settings.RemotePorts)
		}
	}
	if hop := stream.FinalMask.QuicParams.UDPHop; hop != nil {
		return Parse(hop.Ports)
	}
	return nil, nil
}
