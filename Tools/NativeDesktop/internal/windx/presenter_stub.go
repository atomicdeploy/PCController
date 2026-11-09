//go:build !windows

package windx

import (
	"image"
	"sync"
)

// Presenter is unavailable off Windows. The stub keeps cross-platform
// analysis, tests, and Windows cross-build callers free of build-tag plumbing.
type Presenter struct {
	mu     sync.Mutex
	status Status
}

func Probe() (Capability, error) {
	return Capability{Backend: BackendLegacy}, ErrUnavailable
}

func NewPresenter(_ uintptr, width, height int) (*Presenter, error) {
	return nil, ErrUnavailable
}

func (presenter *Presenter) Present(_ *image.RGBA) error {
	if presenter == nil {
		return ErrClosed
	}
	return ErrUnavailable
}

func (presenter *Presenter) Resize(_, _ int) error {
	if presenter == nil {
		return ErrClosed
	}
	return ErrUnavailable
}

func (presenter *Presenter) PresentResized(_ *image.RGBA) error {
	if presenter == nil {
		return ErrClosed
	}
	return ErrUnavailable
}

func (presenter *Presenter) SetOpacity(_ float64) error {
	if presenter == nil {
		return ErrClosed
	}
	return ErrUnavailable
}

func (presenter *Presenter) Capability() Capability {
	return Capability{Backend: BackendLegacy}
}

func (presenter *Presenter) Status() Status {
	if presenter == nil {
		return Status{
			Backend: BackendLegacy,
			Closed:  true,
		}
	}
	presenter.mu.Lock()
	defer presenter.mu.Unlock()
	return presenter.status
}

func (presenter *Presenter) Close() error {
	if presenter == nil {
		return nil
	}
	presenter.mu.Lock()
	presenter.status.Closed = true
	presenter.mu.Unlock()
	return nil
}
