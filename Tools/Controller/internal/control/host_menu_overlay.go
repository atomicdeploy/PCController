package control

import (
	"context"
	"errors"
	"fmt"

	"pccontroller.local/controller/internal/native"
)

var ErrHostMenuOverlayUnsupported = errors.New("firmware does not advertise volatile host-menu overlay capability bit 24")

func (runtime *Runtime) ReplaceHostMenuDirectory(ctx context.Context, directory native.HostMenuDirectory) error {
	if runtime.Snapshot().Hello.Capabilities&native.CapabilityHostMenuOverlay == 0 {
		return ErrHostMenuOverlayUnsupported
	}
	payload, err := native.EncodeHostMenuDirectory(directory)
	if err != nil {
		return err
	}
	if err := runtime.Command(ctx, native.OpHostMenuDirectory, payload); err != nil {
		return fmt.Errorf("replace volatile host-menu directory: %w", err)
	}
	return nil
}

func (runtime *Runtime) PushHostMenuContent(ctx context.Context, content native.HostMenuContent) error {
	if runtime.Snapshot().Hello.Capabilities&native.CapabilityHostMenuOverlay == 0 {
		return ErrHostMenuOverlayUnsupported
	}
	payload, err := native.EncodeHostMenuContent(content)
	if err != nil {
		return err
	}
	if err := runtime.Command(ctx, native.OpHostMenuContent, payload); err != nil {
		return fmt.Errorf("push host-menu content: %w", err)
	}
	return nil
}

func (runtime *Runtime) HostMenuState(ctx context.Context) (native.HostMenuState, error) {
	if runtime.Snapshot().Hello.Capabilities&native.CapabilityHostMenuOverlay == 0 {
		return native.HostMenuState{}, ErrHostMenuOverlayUnsupported
	}
	frame, err := runtime.Request(ctx, native.OpHostMenuStateGet, nil, native.OpHostMenuState)
	if err != nil {
		return native.HostMenuState{}, err
	}
	return native.ParseHostMenuState(frame.Payload)
}
