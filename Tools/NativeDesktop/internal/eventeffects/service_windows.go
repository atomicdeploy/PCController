//go:build windows

package eventeffects

import (
	"rayanlamp.local/app/internal/automation"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/model"
	"rayanlamp.local/app/internal/sfx"
)

// NewWindows binds the host-neutral event policy to the existing Windows
// automation runner and system-sound implementation.
func NewWindows(
	cfg config.Config,
	events <-chan model.Event,
	logger LogFunc,
) (*Service, error) {
	return NewWindowsAt("", cfg, events, logger)
}

// NewWindowsAt binds audio assets to the same resolved configuration directory
// used by the owning primary.
func NewWindowsAt(
	configPath string,
	cfg config.Config,
	events <-chan model.Event,
	logger LogFunc,
) (*Service, error) {
	runner := automation.New(cfg.Automation, automation.LogFunc(logger))
	sounds, soundWarning := sfx.NewManager(sfx.Options{
		ConfigPath:    configPath,
		Notifications: cfg.Notifications,
	})
	if sounds == nil {
		runner.Close()
		return nil, soundWarning
	}
	effectiveNotifications, reconcileWarning :=
		sounds.ReconcileConfig(cfg.Notifications)
	cfg.Notifications = effectiveNotifications
	if reconcileWarning != nil {
		soundWarning = reconcileWarning
	}
	service, err := New(Options{
		Config:     cfg,
		Events:     events,
		Automation: runner,
		PlayCue:    sounds.Play,
		Log:        logger,
	})
	if err != nil {
		runner.Close()
		return nil, err
	}
	service.sounds = sounds
	if soundWarning != nil {
		service.logf(
			"selected custom sound is unavailable; using bundled sounds: %v",
			soundWarning,
		)
	}
	return service, nil
}
