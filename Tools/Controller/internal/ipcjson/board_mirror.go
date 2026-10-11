package ipcjson

import (
	"context"
	"errors"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/boardmirror"
	"pccontroller.local/controller/internal/fleet"
)

type BoardMirrorService interface {
	MirrorInspect(context.Context) (boardmirror.State, error)
	MirrorApply(context.Context, boardmirror.Write) (boardmirror.Result, error)
	MirrorStatus() []boardmirror.GroupStatus
	MirrorResync(context.Context, string, fleet.BoardID) ([]boardmirror.Result, error)
}

func (service *Service) dispatchMirror(ctx context.Context, request Request) (any, error) {
	if request.Method == "controller.mirror.configure" {
		var config fleet.MirrorConfig
		if err := decodeStrictParams(request.Params, &config); err != nil {
			return nil, err
		}
		if err := config.Validate(); err != nil {
			return nil, err
		}
		if service.UpdateHostConfig == nil {
			return nil, errors.New("host configuration store unavailable")
		}
		err := service.UpdateHostConfig(func(value *appconfig.Config) error { value.Integrations.BoardMirror = config; return nil })
		return config, err
	}
	if service.BoardMirror == nil || service.BoardMirror() == nil {
		return nil, errors.New("board mirror manager unavailable")
	}
	manager := service.BoardMirror()
	switch request.Method {
	case "controller.mirror.inspect":
		return manager.MirrorInspect(ctx)
	case "controller.mirror.status":
		return manager.MirrorStatus(), nil
	case "controller.mirror.apply":
		var write boardmirror.Write
		if err := decodeStrictParams(request.Params, &write); err != nil {
			return nil, err
		}
		return manager.MirrorApply(ctx, write)
	case "controller.mirror.resync":
		var params struct {
			GroupID string        `json:"group_id"`
			Source  fleet.BoardID `json:"source_board_id"`
		}
		if err := decodeStrictParams(request.Params, &params); err != nil {
			return nil, err
		}
		return manager.MirrorResync(ctx, params.GroupID, params.Source)
	}
	return nil, errors.New("unsupported mirror method")
}
