//go:build !windows

package osd

import "context"

type unsupported struct{}

func newPresenter(Options) Presenter                                { return unsupported{} }
func (unsupported) Show(context.Context, Request) (Status, error)   { return Status{}, ErrUnsupported }
func (unsupported) Dismiss(context.Context, string) (Status, error) { return Status{}, ErrUnsupported }
func (unsupported) Status() Status                                  { return Status{} }
func (unsupported) Result(string) (Status, bool)                    { return Status{}, false }
func (unsupported) Close()                                          {}
func RegisterTaskbarWindow(uintptr) error                           { return ErrUnsupported }
func UnregisterTaskbarWindow(uintptr)                               {}
