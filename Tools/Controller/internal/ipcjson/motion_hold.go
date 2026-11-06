package ipcjson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	controller "pccontroller.local/controller"
)

func (service *Service) requireMotionHoldSide(side byte) error {
	if side < 1 || side > 2 {
		return errors.New("motion side must be 1 or 2")
	}
	key := []string{"seat.a", "seat.b"}[side-1]
	for _, control := range service.peripheralSettings().Controls {
		if control.Key == key && control.Kind == "seat" {
			return service.rejectLockedPeripheral(key)
		}
	}
	return fmt.Errorf("%s is not an advertised motion control in the current board profile", key)
}

func (service *Service) dispatchMotionHold(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method == "controller.motion.hold.start" {
		var params struct {
			ID         string    `json:"id"`
			ExpiresAt  time.Time `json:"expires_at"`
			Side       byte      `json:"side"`
			Generation uint64    `json:"generation"`
			Direction  string    `json:"direction"`
		}
		if err := decodeStrictParams(raw, &params); err != nil {
			return nil, err
		}
		if err := service.requireMotionHoldSide(params.Side); err != nil {
			return nil, err
		}
		if !params.ExpiresAt.After(time.Now()) || params.ExpiresAt.After(time.Now().Add(2*time.Second)) {
			return nil, errors.New("motion press deadline expired or clocks are not synchronized")
		}
		request, cancel := context.WithDeadline(ctx, params.ExpiresAt)
		defer cancel()
		return service.Client.StartMotionHold(request, params.Generation, params.Side, params.Direction, params.ID)
	}
	var lease controller.MotionHoldLease
	if err := decodeStrictParams(raw, &lease); err != nil {
		return nil, err
	}
	if lease.ID == "" || lease.Generation == 0 || lease.Side < 1 || lease.Side > 2 {
		return nil, errors.New("current motion lease id, side and generation are required")
	}
	if method == "controller.motion.hold.renew" {
		if err := service.requireMotionHoldSide(lease.Side); err != nil {
			return nil, err
		}
		return service.Client.RenewMotionHold(lease)
	}
	err := service.Client.ReleaseMotionHold(ctx, lease)
	return map[string]bool{"released": err == nil}, err
}
