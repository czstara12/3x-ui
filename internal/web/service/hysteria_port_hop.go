package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/porthop"
	"gorm.io/gorm"
)

var hysteriaHopMu sync.Mutex
var applyHysteriaHop = porthop.Apply

func collectHysteriaHopRules(inbounds []model.Inbound) ([]porthop.Rule, error) {
	var rules []porthop.Rule
	for _, ib := range inbounds {
		if ib.NodeID != nil || !ib.Enable || ib.Protocol != model.Hysteria {
			continue
		}
		ranges, err := porthop.FromStream(ib.StreamSettings)
		if err != nil {
			return nil, fmt.Errorf("Hysteria inbound %d: %w", ib.Id, err)
		}
		for _, ports := range ranges {
			rule := porthop.Rule{InboundID: ib.Id, Ports: ports, Target: ib.Port, Listen: ib.Listen, IPv6Only: streamV6Only(ib.StreamSettings)}
			for _, other := range inbounds {
				if other.Id == ib.Id || other.NodeID != nil || !other.Enable || !listenOverlaps(inboundBindAddr(&ib), inboundBindAddr(&other)) {
					continue
				}
				if inboundTransports(other.Protocol, other.StreamSettings, other.Settings)&transportUDP != 0 && other.Port >= ports.First && other.Port <= ports.Last {
					return nil, fmt.Errorf("Hysteria inbound %d hop range %d-%d conflicts with inbound %d UDP port %d", ib.Id, ports.First, ports.Last, other.Id, other.Port)
				}
			}
			for _, previous := range rules {
				if previous.InboundID != rule.InboundID && ports.First <= previous.Ports.Last && previous.Ports.First <= ports.Last && listenOverlaps(bindAddr{listen: previous.Listen, v6only: previous.IPv6Only}, inboundBindAddr(&ib)) {
					return nil, fmt.Errorf("Hysteria hop ranges overlap for inbounds %d and %d", previous.InboundID, ib.Id)
				}
			}
			rules = append(rules, rule)
		}
	}
	if _, err := porthop.Script(rules, false); err != nil {
		return nil, err
	}
	return rules, nil
}

func validateHysteriaHopTx(db *gorm.DB, candidate *model.Inbound, ignoreID int) error {
	if candidate.NodeID != nil {
		return nil
	}
	var rows []model.Inbound
	if err := db.Where("node_id IS NULL AND enable = ?", true).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	filtered := rows[:0]
	for _, row := range rows {
		if row.Id != ignoreID {
			filtered = append(filtered, row)
		}
	}
	copy := *candidate
	if copy.Id == 0 {
		copy.Id = -1
	}
	filtered = append(filtered, copy)
	_, err := collectHysteriaHopRules(filtered)
	return err
}

func ReconcileHysteriaPortHopping() error {
	hysteriaHopMu.Lock()
	defer hysteriaHopMu.Unlock()
	var rows []model.Inbound
	if err := database.GetDB().Where("node_id IS NULL AND enable = ?", true).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	rules, err := collectHysteriaHopRules(rows)
	if err != nil {
		return err
	}
	return applyHysteriaHop(context.Background(), rules, nil)
}

func syncHysteriaHopAfterSave(err *error, restart *bool, relevant bool) {
	if *err != nil || !relevant {
		return
	}
	if syncErr := ReconcileHysteriaPortHopping(); syncErr != nil {
		if *restart {
			(&XrayService{}).SetToNeedRestart()
		}
		*err = fmt.Errorf("inbound change was saved, but automatic Hysteria port forwarding failed (will retry): %w", syncErr)
	}
}

func hasLocalHysteriaHop(ib *model.Inbound) bool {
	if ib == nil || ib.NodeID != nil || ib.Protocol != model.Hysteria {
		return false
	}
	ranges, err := porthop.FromStream(ib.StreamSettings)
	return err != nil || len(ranges) > 0
}
