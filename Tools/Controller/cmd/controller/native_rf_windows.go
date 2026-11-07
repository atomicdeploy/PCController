//go:build windows && !no_native_gui

package main

import (
	"context"
	"errors"
	"fmt"
	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/desktop/desktop"
	"strconv"
	"sync"
)

type nativeRFEntry struct {
	controllerapi.RFEntryView
	BoardAssignment struct {
		Label string `json:"label"`
	} `json:"board_assignment"`
}
type nativeRFCatalog struct {
	Entries     []nativeRFEntry                 `json:"entries"`
	Preferences appconfig.RFLearningPreferences `json:"preferences"`
	Learning    control.RFLearnState            `json:"learning"`
	Peripherals struct {
		Controls []appconfig.ControlDescriptor `json:"controls"`
	} `json:"peripherals"`
	Receptions     []control.RFReception `json:"receptions"`
	BoardError     string                `json:"board_error"`
	HostInstanceID string                `json:"host_instance_id"`
	Generation     uint64                `json:"connection_generation"`
}
type nativeRFCache struct {
	mu                  sync.Mutex
	entries             []nativeRFEntry
	ready               bool
	generation, version uint64
	owner               string
}

func (p nativePages) loadNativeRFCatalog(ctx context.Context, state control.Snapshot) (nativeRFCatalog, error) {
	catalog := nativeRFCatalog{}
	catalog.Preferences.TimeoutSeconds = 60
	if p.preview {
		return catalog, nil
	}
	cache := p.rfCache
	if cache != nil {
		cache.mu.Lock()
		defer cache.mu.Unlock()
	}
	version := p.events.version("rf-table")
	read := state.Connected && (cache == nil || !cache.ready || cache.generation != state.ConnectionGeneration || cache.version != version)
	if err := p.call(ctx, "controller.rf.catalog", map[string]bool{"read_board": read}, &catalog); err != nil {
		return catalog, err
	}
	if state.Connected && cache != nil && !read && cache.owner != catalog.HostInstanceID {
		read = true
		if err := p.call(ctx, "controller.rf.catalog", map[string]bool{"read_board": true}, &catalog); err != nil {
			return catalog, err
		}
	}
	if catalog.BoardError != "" {
		return catalog, errors.New(catalog.BoardError)
	}
	if cache != nil {
		if read {
			cache.entries = append([]nativeRFEntry(nil), catalog.Entries...)
			cache.ready = true
			cache.generation = state.ConnectionGeneration
			cache.version = version
			cache.owner = catalog.HostInstanceID
		} else if cache.generation == state.ConnectionGeneration {
			catalog.Entries = append([]nativeRFEntry(nil), cache.entries...)
		}
	}
	return catalog, nil
}

func nativeRFIdentity(code uint32, bits, protocol byte) string {
	return fmt.Sprintf("%d:%d:%d", code, bits, protocol)
}
func (p nativePages) nativeRFReceiptRows(page *desktop.Page, catalog nativeRFCatalog, state control.Snapshot) {
	page.Owner = fmt.Sprintf("%s:%d", catalog.HostInstanceID, state.ConnectionGeneration)
	page.Receipts = map[string]desktop.RFReceipt{}
	known := map[string]string{}
	for _, entry := range catalog.Entries {
		identity := nativeRFIdentity(entry.Code, entry.Bits, entry.Protocol)
		id := strconv.Itoa(int(entry.ID))
		known[identity] = id
		page.Receipts[id] = desktop.RFReceipt{Identity: identity, Learned: true, Connected: state.Connected}
	}
	for _, receipt := range catalog.Receptions {
		identity := nativeRFIdentity(receipt.Code, receipt.Bits, receipt.Protocol)
		id, learned := known[identity]
		if !learned {
			id = "observed:" + identity
			page.Rows = append(page.Rows, []string{id, fmt.Sprintf("0x%X", receipt.Code), fmt.Sprintf("%d / %d", receipt.Bits, receipt.Protocol), "Unlearned", "○", "0"})
		}
		page.Receipts[id] = desktop.RFReceipt{Identity: identity, Count: receipt.ReceivedCount, LastSeen: receipt.LastSeen, Learned: learned, Connected: state.Connected && receipt.Generation == state.ConnectionGeneration}
	}
}

func (p nativePages) submitNativeRFExtra(ctx context.Context, action string, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot change RF state")
	}
	var result any
	if action != "refresh-codes" {
		var state control.Snapshot
		if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
			return desktop.Page{}, err
		}
		catalog, err := p.loadNativeRFCatalog(ctx, state)
		if err != nil {
			return desktop.Page{}, err
		}
		if action == "add-observed" {
			found := false
			for _, receipt := range catalog.Receptions {
				if "observed:"+nativeRFIdentity(receipt.Code, receipt.Bits, receipt.Protocol) == values["selected"] {
					found = true
					err = p.call(ctx, "controller.rf.observed.add", map[string]any{"code": receipt.Code, "bits": receipt.Bits, "protocol": receipt.Protocol}, &result)
					break
				}
			}
			if !found {
				return desktop.Page{}, errors.New("select a currently observed unlearned waveform")
			}
			if err != nil {
				return desktop.Page{}, err
			}
		} else {
			id, err := strconv.Atoi(values["selected"])
			if err != nil || id < 0 || id > 19 {
				return desktop.Page{}, errors.New("select a learned code first")
			}
			switch action {
			case "map-board":
				err = p.call(ctx, "controller.rf.map", map[string]any{"id": id, "action": values["board_action"], "target": values["board_target"], "behavior": values["board_behavior"]}, &result)
			case "remove":
				err = p.call(ctx, "controller.rf.remove", map[string]any{"id": id}, &result)
			default:
				err = errors.New("unknown RF operation")
			}
			if err != nil {
				return desktop.Page{}, err
			}
		}
	}
	if p.rfCache != nil {
		p.rfCache.mu.Lock()
		p.rfCache.ready = false
		p.rfCache.mu.Unlock()
	}
	return p.load(ctx, "rf")
}
