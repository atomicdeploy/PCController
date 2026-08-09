//go:build !linux

package runtimeinstall

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("PCController runtime provisioning is supported only on Linux; no state was changed")

func Install(context.Context, InstallOptions) (Report, error) {
	return Report{Operation: "install", UserDataPreserved: true}, errUnsupported
}

func Status(context.Context, OperationOptions) (Report, error) {
	return Report{Operation: "status", UserDataPreserved: true}, errUnsupported
}

func Rollback(context.Context, OperationOptions) (Report, error) {
	return Report{Operation: "rollback", UserDataPreserved: true}, errUnsupported
}

func Uninstall(context.Context, OperationOptions) (Report, error) {
	return Report{Operation: "uninstall", UserDataPreserved: true}, errUnsupported
}
