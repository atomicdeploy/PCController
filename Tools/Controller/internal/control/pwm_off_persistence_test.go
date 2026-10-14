package control

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

func copyPWMOffPersistenceConfig(t *testing.T, config appconfig.Config) appconfig.Config {
	t.Helper()
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var copy appconfig.Config
	if err := json.Unmarshal(encoded, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func pwmOffPersistenceConfig() appconfig.Config {
	config := appconfig.Defaults()
	for _, channel := range []int{1, 2} {
		value := appconfig.PWMChannel(config.UI, channel)
		value.OnBehavior = "restore"
		config.UI.PWMChannels[appconfig.PWMChannelKey(channel)] = value
	}
	config.BoardProfiles = map[string]appconfig.BoardProfile{"port:fixture": {
		Key: "fixture", Mode: appconfig.BoardModeOrdinaryRelays,
		PWMRecall: map[string]uint16{"pwm.2": 555},
	}}
	return config
}

func TestPWMOffPersistenceEvidenceAndSerializedStore(t *testing.T) {
	for _, mode := range []string{"save", "duplicate", "unknown", "full policy", "stale generation", "stale revision", "nonzero", "owner replaced in store", "slot changed in store", "other slot changed", "same save in store", "policy changed in store"} {
		t.Run(mode, func(t *testing.T) {
			config := pwmOffPersistenceConfig()
			runtime := New(Options{})
			session := link.NewForPort("fixture", newReconnectTestPort())
			t.Cleanup(func() { _ = session.Close() })
			runtime.session, runtime.generation, runtime.port = session, 7, ports.Info{Name: "fixture"}
			runtime.havePWMValues, runtime.pwmValues, runtime.pwmRevision, runtime.pwmRecallGeneration = true, native.PWMValues{Available: true}, 9, 7
			runtime.pwmRecall[1] = PWMRecallValue{Known: true, Raw: 239}
			attempts, saves := 0, 0
			options := CommandOptions{HostConfig: func() appconfig.Config { return config }, UpdateHostConfig: func(change func(*appconfig.Config) error) error {
				attempts++
				copy := copyPWMOffPersistenceConfig(t, config)
				switch mode {
				case "owner replaced in store":
					runtime.generation++
				case "slot changed in store":
					runtime.pwmRevision++
					runtime.pwmValues.Values[1] = 777
				case "other slot changed":
					runtime.pwmRevision++
					runtime.pwmValues.Values[3] = 777
				case "same save in store":
					profile := copy.BoardProfiles["port:fixture"]
					profile.PWMRecall["pwm.1"] = 239
					copy.BoardProfiles["port:fixture"] = profile
				case "policy changed in store":
					value := copy.UI.PWMChannels["pwm.1"]
					value.OnBehavior = "full"
					copy.UI.PWMChannels = appconfig.DefaultPWMChannels()
					copy.UI.PWMChannels["pwm.1"] = value
				}
				if err := change(&copy); err != nil {
					return err
				}
				config = copy
				saves++
				return nil
			}}
			runtime.configurePWMRecallPersistence(func() CommandOptions { return options })
			generation, revision := uint64(7), uint64(9)
			switch mode {
			case "duplicate":
				profile := config.BoardProfiles["port:fixture"]
				profile.PWMRecall["pwm.1"] = 239
				config.BoardProfiles["port:fixture"] = profile
			case "unknown":
				runtime.pwmRecall[1] = PWMRecallValue{}
			case "full policy":
				value := config.UI.PWMChannels["pwm.1"]
				value.OnBehavior = "full"
				config.UI.PWMChannels["pwm.1"] = value
			case "stale generation":
				generation--
			case "stale revision":
				revision--
			case "nonzero":
				runtime.pwmValues.Values[1] = 239
			}
			err := runtime.CommitPWMOffRecall(generation, revision, []byte{1})
			wantError := mode == "stale generation" || mode == "stale revision" || mode == "nonzero" || mode == "owner replaced in store" || mode == "slot changed in store"
			var persistence *PWMRecallPersistenceError
			if wantError != errors.As(err, &persistence) || !wantError && err != nil {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			wantSaves := 0
			if mode == "save" || mode == "other slot changed" {
				wantSaves = 1
			}
			if saves != wantSaves || attempts > 1 {
				t.Fatalf("attempts=%d saves=%d want=%d", attempts, saves, wantSaves)
			}
			if config.BoardProfiles["port:fixture"].PWMRecall["pwm.2"] != 555 || runtime.pwmValues.Values[1] != 0 && mode != "nonzero" && mode != "slot changed in store" {
				t.Fatal("persistence changed unrelated memory or output")
			}
		})
	}
}

func TestPWMOffPersistenceFramedOperatorRoutesAndNativeCleanup(t *testing.T) {
	for _, route := range []string{"semantic Off", "CLI Off", "CLI set zero", "CLI raw zero", "All-Off", "native cleanup", "positive stream"} {
		t.Run(route, func(t *testing.T) {
			runtime := New(Options{RequestTimeout: 200 * time.Millisecond})
			config := pwmOffPersistenceConfig()
			values := native.PWMValues{Available: true}
			values.Values[1], values.Values[2] = 239, 777
			port := &pwmActionTestPort{reconnectTestPort: newReconnectTestPort(), replies: make(chan []byte, 4)}
			port.handle = func(frame native.Frame) (native.Frame, error) {
				switch frame.Opcode {
				case native.OpPWMGet:
					payload := make([]byte, 34)
					payload[0] = 1
					for channel, raw := range values.Values {
						binary.LittleEndian.PutUint16(payload[2+channel*2:], raw)
					}
					return native.Frame{Opcode: native.OpPWMValues, Payload: payload}, nil
				case native.OpPWMSet:
					values.Values[frame.Payload[0]] = binary.LittleEndian.Uint16(frame.Payload[1:])
				case native.OpPWMAllOff:
					values.Values = [16]uint16{}
				default:
					return native.Frame{}, errors.New("unexpected fixture command")
				}
				return native.Frame{Opcode: native.OpACK, Payload: []byte{frame.Opcode, 0}}, nil
			}
			session := link.NewForPort("fixture", port)
			runtime.session, runtime.generation, runtime.port = session, 7, ports.Info{Name: "fixture"}
			t.Cleanup(func() { _ = session.Close() })
			saves := 0
			engine := NewCommandEngine(runtime, CommandOptions{HostConfig: func() appconfig.Config { return config }, UpdateHostConfig: func(change func(*appconfig.Config) error) error {
				copy := copyPWMOffPersistenceConfig(t, config)
				if err := change(&copy); err != nil {
					return err
				}
				config = copy
				saves++
				return nil
			}})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var err error
			switch route {
			case "semantic Off":
				_, err = runtime.ApplyPWMAction(ctx, 7, 1, "off", appconfig.PWMChannel(config.UI, 1))
			case "CLI Off":
				_, err = engine.Execute(ctx, "pwm off 1")
			case "CLI set zero":
				_, err = engine.Execute(ctx, "pwm set 1 0")
			case "CLI raw zero":
				_, err = engine.Execute(ctx, "pwm raw 1 0")
			case "All-Off":
				_, err = engine.Execute(ctx, "pwm off")
			case "native cleanup":
				err = runtime.Command(ctx, native.OpPWMAllOff, nil)
			case "positive stream":
				for _, percent := range []string{"20", "40", "60"} {
					_, err = engine.Execute(ctx, "pwm set 1 "+percent)
					if err != nil {
						break
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			wantSaves := 1
			if route == "native cleanup" || route == "positive stream" {
				wantSaves = 0
			}
			if saves != wantSaves {
				t.Fatalf("saves=%d want=%d", saves, wantSaves)
			}
			if wantSaves == 1 && config.BoardProfiles["port:fixture"].PWMRecall["pwm.1"] != 239 {
				t.Fatal("Off lost the pre-write confirmed brightness")
			}
			if route == "All-Off" && config.BoardProfiles["port:fixture"].PWMRecall["pwm.2"] != 777 {
				t.Fatal("All-Off did not coalesce both channels into one save")
			}
		})
	}
}
