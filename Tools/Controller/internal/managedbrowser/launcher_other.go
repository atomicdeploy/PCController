//go:build !linux

package managedbrowser

import (
	"context"
	"errors"
	"io"
)

// Options is retained cross-platform so callers can make an explicit fallback.
type Options struct {
	Executable string
	URL        string
	ProfileDir string
	Token      string
	Stdout     io.Writer
	Stderr     io.Writer
}

// Session is unavailable outside the audited Linux Chrome runtime path.
type Session struct{}

func Start(context.Context, Options) (*Session, error) {
	return nil, errors.New("managed browser authentication is not available on this platform")
}

func (*Session) Wait(context.Context) error  { return nil }
func (*Session) Close(context.Context) error { return nil }
