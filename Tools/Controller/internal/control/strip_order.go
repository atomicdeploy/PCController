package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

type stripOrderContextKey struct{}

func (runtime *Runtime) stripOutputConfig() appconfig.UI {
	runtime.stripConfigMu.RLock()
	provider := runtime.stripConfig
	runtime.stripConfigMu.RUnlock()
	if provider != nil {
		return provider()
	}
	return appconfig.UI{}
}

// Compensate for the board's compiled encoder, not merely a cosmetic preview.
// Both the requested strip order and the firmware's native order describe
// bytes on the LED wire. The default BRG/BRG preserves production behavior;
// custom WS2812B builds declare GRB in ui.strip_firmware_order.
func reorderStripPayload(payload []byte, desired, firmware string) ([]byte, error) {
	if err := appconfig.ValidateStripOrder(desired); err != nil {
		return nil, err
	}
	if err := appconfig.ValidateStripOrder(firmware); err != nil {
		return nil, err
	}
	desired, firmware = appconfig.EffectiveStripOrder(desired), appconfig.EffectiveStripOrder(firmware)
	if desired == firmware || len(payload) == 0 {
		return payload, nil
	}
	start, end := 0, 0
	if payload[0] == 0xFD {
		if len(payload) < 5 || (len(payload)-2)%3 != 0 {
			return payload, nil
		}
		start, end = 2, len(payload)
	} else if len(payload) == 5 && (payload[0] <= native.AddressableLEDMaxPixel || payload[0] == native.AddressableLEDFill) {
		start, end = 1, 4 // Never permute pixel index or brightness.
	} else {
		return payload, nil
	} // Config, show and malformed payloads stay untouched.
	result := append([]byte(nil), payload...)
	for offset := start; offset < end; offset += 3 {
		for slot := 0; slot < 3; slot++ {
			result[offset+strings.IndexByte("RGB", firmware[slot])] = payload[offset+strings.IndexByte("RGB", desired[slot])]
		}
	}
	return result, nil
}

func (runtime *Runtime) stripWirePayload(ctx context.Context, opcode byte, payload []byte) ([]byte, error) {
	if opcode != native.OpAddressableLED {
		return payload, nil
	}
	config, ok := ctx.Value(stripOrderContextKey{}).(appconfig.UI)
	if !ok {
		config = runtime.stripOutputConfig()
	}
	// Prepared timelines and raw IPC callers also send chunked frames. Latch
	// the mapping at pixel zero so a settings change cannot split one frame.
	runtime.mu.RLock()
	generation := runtime.generation
	runtime.mu.RUnlock()
	runtime.stripConfigMu.Lock()
	if len(payload) >= 5 && payload[0] == 0xFD && (len(payload)-2)%3 == 0 {
		if payload[1] == 0 || !runtime.stripFrameActive || runtime.stripFrameGeneration != generation {
			runtime.stripFrameConfig, runtime.stripFrameActive = config, true
			runtime.stripFrameGeneration = generation
		}
		config = runtime.stripFrameConfig
	} else if len(payload) == 1 && payload[0] == 0xFC {
		runtime.stripFrameActive = false
	}
	runtime.stripConfigMu.Unlock()
	return reorderStripPayload(payload, config.StripColorOrder, config.StripFirmwareOrder)
}

func stripOrderCommand(outputs *OutputScheduler, options CommandOptions, args []string) (string, error) {
	if options.HostConfig == nil && options.StripOrder == nil {
		return "", errors.New("host configuration is unavailable")
	}
	if len(args) > 1 {
		return "", errors.New("usage: strip order [GRB|RGB|BRG|BGR|RBG|GBR]")
	}
	if len(args) == 1 {
		if options.UpdateHostConfig == nil && options.SaveStripOrder == nil {
			return "", errors.New("host configuration is read-only")
		}
		// A running frame is allowed to finish with one mapping; never stop it
		// or issue an unsolicited output just to save this preference.
		outputs.stripMu.Lock()
		defer outputs.stripMu.Unlock()
		change := func(config *appconfig.Config) error {
			order := strings.ToUpper(args[0])
			if order == "" {
				return errors.New("strip color order cannot be empty")
			}
			if order == "NEXT" || order == "PREVIOUS" {
				orders := appconfig.StripColorOrders()
				for index, candidate := range orders {
					if candidate == appconfig.EffectiveStripOrder(config.UI.StripColorOrder) {
						delta := 1
						if order == "PREVIOUS" {
							delta = -1
						}
						order = orders[(index+delta+len(orders))%len(orders)]
						break
					}
				}
			}
			if err := appconfig.ValidateStripOrder(order); err != nil {
				return err
			}
			config.UI.StripColorOrder = order
			return nil
		}
		var err error
		if options.SaveStripOrder != nil {
			candidate := appconfig.Config{UI: stripOrderUI(options)}
			if err = change(&candidate); err == nil {
				err = options.SaveStripOrder(candidate.UI.StripColorOrder)
			}
		} else {
			err = options.UpdateHostConfig(change)
		}
		if err != nil {
			return "", err
		}
	}
	config := stripOrderUI(options)
	data, err := json.Marshal(map[string]any{
		"color_order":    appconfig.EffectiveStripOrder(config.StripColorOrder),
		"firmware_order": appconfig.EffectiveStripOrder(config.StripFirmwareOrder),
		"orders":         appconfig.StripColorOrders(),
	})
	return string(data), err
}

func stripOrderUI(options CommandOptions) appconfig.UI {
	if options.StripOrder != nil {
		order, firmware := options.StripOrder()
		return appconfig.UI{StripColorOrder: order, StripFirmwareOrder: firmware}
	}
	return options.HostConfig().UI
}
