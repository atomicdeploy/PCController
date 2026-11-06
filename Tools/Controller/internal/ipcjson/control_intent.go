package ipcjson

import (
	"context"
	"errors"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
)

// Optional for legacy clients; when any owner field is supplied the entire
// fence is mandatory. Native input captures it from its rendered catalog.
type controlIntentOwner struct {
	HostInstanceID string `json:"expected_host_instance_id,omitempty"`
	Generation     uint64 `json:"expected_connection_generation,omitempty"`
	BoardIdentity  string `json:"board_identity,omitempty"`
	Revision       string `json:"expected_revision,omitempty"`
	PWMRevision    string `json:"expected_pwm_configuration_revision,omitempty"`
}

func validateControlIntent(owner controlIntentOwner, host string, config appconfig.Config, snapshot controller.Snapshot, pwm bool) error {
	if owner == (controlIntentOwner{}) {
		return nil
	}
	if owner.HostInstanceID == "" || owner.HostInstanceID != host || owner.Generation == 0 || owner.BoardIdentity == "" || owner.Revision == "" {
		return errors.New("current host, board generation, identity and profile revision are required")
	}
	profile, _ := describeBoardProfile(config, snapshot)
	if !snapshot.Connected || snapshot.ConnectionGeneration != owner.Generation || !profile.Configured || profile.BoardIdentity != owner.BoardIdentity || profile.Revision != owner.Revision {
		return errors.New("channel control owner changed; refresh before retrying")
	}
	if pwm && (owner.PWMRevision == "" || owner.PWMRevision != appconfig.PWMConfigurationRevision(config.UI)) {
		return errors.New("PWM configuration changed; refresh before applying the value")
	}
	return nil
}

func (service *Service) guardControlIntent(ctx context.Context, owner controlIntentOwner, pwm bool) (context.Context, error) {
	if owner == (controlIntentOwner{}) {
		return ctx, nil
	}
	validate := func() error {
		return validateControlIntent(owner, service.HostInstanceID, service.hostConfig(), service.Client.BoardSnapshot(), pwm)
	}
	if err := validate(); err != nil {
		return ctx, err
	}
	// Recheck after request-slot/writer queues, before the first output byte.
	// The link composes this with runtime generation and motion-lease guards.
	return link.WithRequestWriteGuard(ctx, validate), nil
}
