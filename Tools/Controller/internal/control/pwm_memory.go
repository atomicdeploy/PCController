package control

import (
	"errors"
	"pccontroller.local/controller/internal/appconfig"
)

// This fence deliberately excludes effect/configuration providers. In particular,
// a Store.Update callback already owns the configuration write lock and must not
// call Snapshot, whose effect catalog can re-enter Store.Current.
func pwmRecallOwnerSnapshot(runtime *Runtime) Snapshot {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	value := Snapshot{Connected: runtime.sessionUsableLocked(), Port: runtime.port, ConnectionGeneration: runtime.generation}
	if value.Connected && runtime.pwmRecallGeneration == runtime.generation {
		value.PWMRecall = runtime.pwmRecall
	}
	return value
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
