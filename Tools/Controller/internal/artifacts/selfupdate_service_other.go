//go:build !windows

package artifacts

import (
	"context"
	"errors"
)

type platformUpdateServices struct{}

func (platformUpdateServices) Validate(string, string, int) error {
	return errors.New("Windows service updates require Windows")
}
func (platformUpdateServices) Stop(context.Context, string, string, int) error {
	return errors.New("Windows service updates require Windows")
}
func (platformUpdateServices) Start(context.Context, string, string) (updateProcess, error) {
	return nil, errors.New("Windows service updates require Windows")
}
