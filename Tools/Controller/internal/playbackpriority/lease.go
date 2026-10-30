// Package playbackpriority owns a temporary host scheduling lease, never a
// startup priority policy. Its lifetime is the prepared serial reservation.
package playbackpriority

import (
	"fmt"
	"sync"
)

const (
	normalClass   = uint32(0x20)
	highClass     = uint32(0x80)
	realtimeClass = uint32(0x100)
)

type manager struct {
	mu       sync.Mutex
	get      func() (uint32, error)
	set      func(uint32) error
	refs     int
	previous uint32
	changed  bool
}

func (m *manager) acquire() (func() error, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refs == 0 {
		current, err := m.get()
		if err != nil {
			return nil, fmt.Errorf("read playback scheduling priority: %w", err)
		}
		// A failed previous restore retains its original baseline for retry.
		if !m.changed || current != highClass {
			m.changed = false
			m.previous = current
		}
		if current != highClass && current != realtimeClass {
			if err := m.set(highClass); err != nil {
				return nil, fmt.Errorf("acquire playback scheduling priority: %w", err)
			}
			m.changed = true
		}
	}
	m.refs++
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.refs--
			if m.refs != 0 || !m.changed {
				return
			}
			current, err := m.get()
			if err != nil {
				releaseErr = fmt.Errorf("inspect playback priority restoration: %w", err)
				return
			}
			// Never overwrite a priority change made by another owner.
			if current != highClass {
				m.changed = false
				return
			}
			if err := m.set(m.previous); err != nil {
				releaseErr = fmt.Errorf("restore playback scheduling priority: %w", err)
				return
			}
			m.changed = false
		})
		return releaseErr
	}, nil
}
