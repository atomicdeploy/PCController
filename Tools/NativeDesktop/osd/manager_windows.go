//go:build windows

package osd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/lxn/win"
)

const maxStackedOSDs = 6

type overlay struct {
	window   *presenter
	position string
	serial   uint64
}

// manager serializes admissions but leaves each overlay's window, animation and
// input on its own DPI-aware thread. Lifecycle signals are coalesced, not polled.
type manager struct {
	mu      sync.Mutex
	options Options
	active  []*overlay
	history []Status
	serial  uint64
	closed  bool
	once    sync.Once
	changed chan struct{}
	done    chan struct{}
}

func newPresenter(o Options) Presenter {
	return &manager{options: o, changed: make(chan struct{}, 1), done: make(chan struct{})}
}

func (m *manager) signal() {
	select {
	case m.changed <- struct{}{}:
	default:
	}
}
func (m *manager) worker() {
	for {
		select {
		case <-m.done:
			return
		case <-m.changed:
			m.mu.Lock()
			if !m.closed {
				m.collectLocked()
				m.layoutLocked()
			}
			m.mu.Unlock()
		}
	}
}

func (e *overlay) status() Status { s := e.window.Status(); s.Accepted = e.serial; return s }
func (m *manager) rememberLocked(s Status) {
	s.Items = nil
	s.ActiveCount = 0
	m.history = append(m.history, s)
	if len(m.history) > 64 {
		m.history = m.history[len(m.history)-64:]
	}
}
func (m *manager) collectLocked() {
	kept := m.active[:0]
	for _, e := range m.active {
		s := e.status()
		if s.Active {
			kept = append(kept, e)
		} else {
			m.rememberLocked(s)
			e.window.Close()
		}
	}
	m.active = kept
}

func (m *manager) statusLocked() Status {
	s := Status{Supported: true, Accepted: m.serial}
	if len(m.history) > 0 {
		s = m.history[len(m.history)-1]
	}
	var items []Status
	for _, e := range m.active {
		v := e.status()
		if v.Active {
			items = append(items, v)
		}
	}
	if len(items) > 0 {
		s = items[len(items)-1]
	}
	s.Active = len(items) > 0
	s.ActiveCount = len(items)
	s.Items = items
	return s
}
func (m *manager) Status() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.statusLocked() }
func (m *manager) Result(id string) (Status, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == "" {
		return m.statusLocked(), true
	}
	for i := len(m.active) - 1; i >= 0; i-- {
		s := m.active[i].status()
		if s.ID == id {
			return s, true
		}
	}
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].ID == id {
			return m.history[i], true
		}
	}
	return Status{}, false
}

func stackOffset(position string, before, total, height, gap int) int {
	if position == "center" {
		return before - (total-height)/2
	}
	return before
}
func (m *manager) layoutLocked() {
	groups := make(map[string][]*overlay)
	for _, e := range m.active {
		groups[e.position] = append(groups[e.position], e)
	}
	for position, group := range groups {
		total := 0
		for i, e := range group {
			s := e.status()
			total += s.PixelHeight
			if i > 0 {
				total += int(math.Round(10 * float64(s.DPI) / 96))
			}
		}
		before := 0
		for _, e := range group {
			s := e.status()
			gap := int(math.Round(10 * float64(s.DPI) / 96))
			offset := stackOffset(position, before, total, s.PixelHeight, gap)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, _ = e.window.send(ctx, command{offset: &offset})
			cancel()
			before += s.PixelHeight + gap
		}
	}
}

func (m *manager) Show(ctx context.Context, r Request) (Status, error) {
	r, err := Normalize(r)
	if err != nil {
		return m.Status(), err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return m.statusLocked(), errors.New("OSD presenter closed")
	}
	m.once.Do(func() { go m.worker() })
	m.collectLocked()
	var survivors []*overlay
	for _, e := range m.active {
		if r.Behavior == "stack" && (r.ID == "" || e.status().ID != r.ID) {
			survivors = append(survivors, e)
		}
	}
	if len(survivors) >= maxStackedOSDs {
		return m.statusLocked(), errors.New("OSD stack is full (six active overlays); dismiss one or use replace")
	}
	next := newWindowPresenter(m.options)
	next.onChange = m.signal
	if err = next.start(ctx); err != nil {
		next.Close()
		return m.statusLocked(), err
	}
	dpi := int(win.GetDpiForWindow(win.HWND(next.hwnd.Load())))
	dpi = min(384, max(96, dpi))
	scale := float64(dpi) / 96
	px := func(v int) int { return int(math.Round(float64(v) * scale)) }
	height := px(106 + strings.Count(r.Text, "\n")*24)
	if rows := (len(r.Actions) + 2) / 3; rows > 0 {
		height += px(rows*40 + 4)
	}
	before := 0
	for _, e := range survivors {
		if e.position == r.Position {
			before += e.status().PixelHeight + px(10)
		}
	}
	next.stackOffset = stackOffset(r.Position, before, before+height, height, px(10))
	if r.ID == "" {
		for n := m.serial + 1; ; n++ {
			candidate := fmt.Sprintf("osd-%d", n)
			exists := false
			for _, e := range m.active {
				if e.status().ID == candidate {
					exists = true
					break
				}
			}
			for _, s := range m.history {
				if s.ID == candidate {
					exists = true
					break
				}
			}
			if !exists {
				r.ID = candidate
				break
			}
		}
	}
	if _, err = next.Show(ctx, r); err != nil {
		next.Close()
		return m.statusLocked(), err
	}
	// Retire replacements only after the new surface has been admitted. A failed
	// render never destroys the user's existing notification or script choice.
	for _, e := range m.active {
		keep := false
		for _, s := range survivors {
			if e == s {
				keep = true
				break
			}
		}
		if !keep {
			finishCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, _ = e.window.send(finishCtx, command{finish: "replaced"})
			cancel()
			m.rememberLocked(e.status())
			e.window.Close()
		}
	}
	m.serial++
	entry := &overlay{window: next, position: r.Position, serial: m.serial}
	m.active = append(survivors, entry)
	m.layoutLocked()
	result := entry.status()
	current := m.statusLocked()
	result.ActiveCount = current.ActiveCount
	result.Items = current.Items
	return result, nil
}

func (m *manager) Dismiss(ctx context.Context, id string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return m.statusLocked(), errors.New("OSD presenter closed")
	}
	for i := len(m.active) - 1; i >= 0; i-- {
		e := m.active[i]
		if id == "" || e.status().ID == id {
			s, err := e.window.Dismiss(ctx, e.status().ID)
			s.Accepted = e.serial
			return s, err
		}
	}
	if id == "" {
		return m.statusLocked(), nil
	}
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i].ID == id {
			return m.history[i], nil
		}
	}
	return m.statusLocked(), errors.New("OSD id is not active")
}
func (m *manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	close(m.done)
	for _, e := range m.active {
		e.window.Close()
		m.rememberLocked(e.status())
	}
	m.active = nil
}
