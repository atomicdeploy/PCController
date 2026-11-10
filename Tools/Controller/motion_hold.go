package controller

import (
	"context"
	"errors"
	"time"

	"pccontroller.local/controller/internal/control"
)

type MotionHoldLease = control.MotionHoldLease

func (client *Client) StartMotionHold(ctx context.Context, generation uint64, side byte, direction, id string) (MotionHoldLease, error) {
	if err := client.requireMotionAllowed(ctx); err != nil {
		return MotionHoldLease{}, err
	}
	value := byte(0)
	switch direction {
	case "up":
		value = 1
	case "down":
		value = 2
	default:
		return MotionHoldLease{}, errors.New("hold direction must be up or down")
	}
	return client.runtime.StartMotionHold(ctx, generation, side, value, id)
}

func (client *Client) RenewMotionHold(lease MotionHoldLease) (MotionHoldLease, error) {
	policy := client.motionDoorPolicy()
	if policy != "always" {
		state := client.runtime.Snapshot()
		if !state.HaveStatus || time.Since(state.StatusUpdated) > 1500*time.Millisecond {
			return MotionHoldLease{}, errors.New("motion hold requires fresh door status")
		}
		if err := client.checkMotionDoorPolicy(state.Status); err != nil {
			return MotionHoldLease{}, err
		}
	}
	return client.runtime.RenewMotionHold(lease)
}

func (client *Client) ReleaseMotionHold(ctx context.Context, lease MotionHoldLease) error {
	return client.runtime.ReleaseMotionHold(ctx, lease)
}
