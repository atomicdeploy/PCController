package control

import (
	"errors"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
)

// This fence deliberately excludes effect/configuration providers. In particular,
// a Store.Update callback already owns the configuration write lock and must not
// call Snapshot, whose effect catalog can re-enter Store.Current.
func pwmRecallOwnerSnapshot(runtime *Runtime) Snapshot {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	value := Snapshot{Connected: runtime.sessionUsableLocked(), Port: runtime.port, ConnectionGeneration: runtime.generation, PWMRevision: runtime.pwmRevision}
	if runtime.havePWMValues {
		values := runtime.pwmValues
		value.PWMValues = &values
	}
	if value.Connected && runtime.pwmRecallGeneration == runtime.generation {
		value.PWMRecall = runtime.pwmRecall
	}
	return value
}

// The configured primary owns persistence. A second facade must not install an
// unrelated store, and no configuration callback runs under the runtime lock.
func (runtime *Runtime) configurePWMRecallPersistence(options func() CommandOptions) {
	runtime.pwmRecallOptionsMu.Lock()
	runtime.pwmRecallOptions = options
	runtime.pwmRecallOptionsMu.Unlock()
}

// PWMRecallPersistenceError is a confirmed output with failed configuration
// persistence, not an unknown hardware result and never permission to retry I/O.
type PWMRecallPersistenceError struct{ Cause error }

func (err *PWMRecallPersistenceError) Error() string {
	if err.Cause == nil {
		return "PWM applied, but recall persistence failed; output was not retried"
	}
	return "PWM applied, but recall persistence failed; output was not retried: " + err.Cause.Error()
}
func (err *PWMRecallPersistenceError) Unwrap() error { return err.Cause }

var errPWMRecallUnchanged = errors.New("PWM recall is already persisted")

// CommitPWMOffRecall consumes confirmed zero readback, not an arbitrary duty or
// a cached draft. It performs no device request. Only explicit operator routes
// call this; generic native cleanup/telemetry/positive streaming do not.
func (runtime *Runtime) CommitPWMOffRecall(generation, revision uint64, channels []byte) error {
	runtime.pwmRecallOptionsMu.RLock()
	provider := runtime.pwmRecallOptions
	runtime.pwmRecallOptionsMu.RUnlock()
	if provider == nil {
		return nil // A memory-only embedder has not configured a durable store.
	}
	options := provider()
	if options.HostConfig == nil || options.UpdateHostConfig == nil {
		return nil
	}
	snapshot := pwmRecallOwnerSnapshot(runtime)
	if !snapshot.Connected || snapshot.ConnectionGeneration != generation || snapshot.PWMValues == nil || !snapshot.PWMValues.Available || snapshot.PWMRevision != revision {
		return &PWMRecallPersistenceError{Cause: errors.New("PWM recall readback owner or revision changed")}
	}
	identity := pwmProfileIdentity(snapshot)
	config := options.HostConfig()
	profile, configured := config.BoardProfiles[identity]
	updates := make(map[string]uint16)
	for _, channel := range channels {
		if channel >= appconfig.PWMChannelCount || snapshot.PWMValues.Values[channel] != 0 {
			return &PWMRecallPersistenceError{Cause: errors.New("PWM recall requires confirmed selected-channel Off")}
		}
		if appconfig.PWMOnBehavior(appconfig.PWMChannelForProfile(config, identity, int(channel))) != "restore" {
			continue
		}
		recall := snapshot.PWMRecall[channel]
		if recall.Known && recall.Raw > 0 {
			updates[appconfig.PWMChannelKey(int(channel))] = recall.Raw
		}
	}
	if len(updates) == 0 {
		return nil
	}
	if !configured {
		return &PWMRecallPersistenceError{Cause: errors.New("configure the board profile before persisting brightness")}
	}
	changed := false
	for key, raw := range updates {
		changed = changed || profile.PWMRecall[key] != raw
	}
	if !changed {
		return nil
	}
	var changedKeys []string
	var profileRevision string
	err := options.UpdateHostConfig(func(currentConfig *appconfig.Config) error {
		live := pwmRecallOwnerSnapshot(runtime)
		current, present := currentConfig.BoardProfiles[identity]
		if !live.Connected || live.ConnectionGeneration != generation || live.PWMRevision < revision || live.PWMValues == nil || !present || current.Key != profile.Key {
			return errors.New("PWM recall profile or owner changed before persistence")
		}
		for _, channel := range channels {
			key := appconfig.PWMChannelKey(int(channel))
			raw, save := updates[key]
			if !save {
				continue
			}
			if live.PWMValues.Values[channel] != 0 || live.PWMRecall[channel] != snapshot.PWMRecall[channel] {
				return errors.New("PWM recall evidence changed before persistence")
			}
			if appconfig.PWMOnBehavior(appconfig.PWMChannelForProfile(*currentConfig, identity, int(channel))) != "restore" || current.PWMRecall[key] == raw {
				continue
			}
			if current.PWMRecall == nil {
				current.PWMRecall = make(map[string]uint16)
			}
			current.PWMRecall[key] = raw
			changedKeys = append(changedKeys, key)
		}
		if len(changedKeys) == 0 {
			return errPWMRecallUnchanged
		}
		currentConfig.BoardProfiles[identity] = current
		if err := currentConfig.Validate(); err != nil {
			return err
		}
		profileRevision = appconfig.BoardProfileRevision(identity, current, currentConfig.UI.PeripheralNames)
		return nil
	})
	if errors.Is(err, errPWMRecallUnchanged) {
		return nil
	}
	if err != nil {
		return &PWMRecallPersistenceError{Cause: err}
	}
	runtime.PublishStructuredEvent(Event{Kind: "peripherals.changed", Stream: EventStreamState,
		Source: "host", Target: "refresh", Action: "refresh", Text: "PWM recall persisted",
		Metadata: map[string]string{"board_identity": identity, "profile_key": profile.Key,
			"revision": profileRevision, "changed_keys": strings.Join(changedKeys, ","), "changed_fields": "pwm_recall", "action": "refresh"}})
	return nil
}

func pwmProfileIdentity(snapshot Snapshot) string {
	return appconfig.ResolveBoardIdentity(snapshot.Port.SerialNumber, snapshot.Port.InstanceID, snapshot.Port.Name).Value
}

// Persist only explicit Off/toggle or hold-release intent after confirmed I/O.
// A telemetry animation or repeat tick never writes the configuration file.
func persistPWMRecall(runtime *Runtime, options CommandOptions, generation uint64, channel byte) error {
	if options.HostConfig == nil || options.UpdateHostConfig == nil {
		return errors.New("persistent PWM recall configuration is unavailable")
	}
	if channel >= 16 {
		return errors.New("PWM recall channel must be 0..15")
	}
	snapshot := pwmRecallOwnerSnapshot(runtime)
	if !snapshot.Connected || snapshot.ConnectionGeneration != generation {
		return errors.New("PWM recall owner changed")
	}
	recall := snapshot.PWMRecall[channel]
	if !recall.Known || recall.Raw == 0 {
		return nil
	}
	identity := pwmProfileIdentity(snapshot)
	profile, configured := options.HostConfig().BoardProfiles[identity]
	if !configured {
		return errors.New("configure the board profile before persisting RF brightness")
	}
	key := appconfig.PWMChannelKey(int(channel))
	if profile.PWMRecall[key] == recall.Raw {
		return nil
	}
	return options.UpdateHostConfig(func(config *appconfig.Config) error {
		live := pwmRecallOwnerSnapshot(runtime)
		current, present := config.BoardProfiles[identity]
		if !live.Connected || live.ConnectionGeneration != generation || !present || current.Key != profile.Key {
			return errors.New("PWM recall profile or owner changed")
		}
		if current.PWMRecall == nil {
			current.PWMRecall = make(map[string]uint16)
		}
		current.PWMRecall[key] = recall.Raw
		config.BoardProfiles[identity] = current
		return config.Validate()
	})
}
