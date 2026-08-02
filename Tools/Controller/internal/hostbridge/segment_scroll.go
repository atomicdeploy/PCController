package hostbridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

type segmentScrollTarget struct {
	active bool
	page   byte
	text   string
	dwell  time.Duration
}

func (left segmentScrollTarget) equal(right segmentScrollTarget) bool {
	return left.active == right.active && left.page == right.page &&
		left.text == right.text && left.dwell == right.dwell
}

// segmentScrollPresenter coalesces watched configuration and high-rate STATUS
// observations into one native display command per semantic change.
type segmentScrollPresenter struct {
	ctx     context.Context
	client  *controller.Client
	onState func(segmentScrollTarget)
	onError func(error)

	mu      sync.Mutex
	desired segmentScrollTarget
	applied segmentScrollTarget
	wake    chan struct{}
	sendMu  sync.Mutex
}

func newSegmentScrollPresenter(
	ctx context.Context,
	client *controller.Client,
	onState func(segmentScrollTarget),
	onError func(error),
) *segmentScrollPresenter {
	return &segmentScrollPresenter{
		ctx: ctx, client: client, onState: onState, onError: onError,
		wake: make(chan struct{}, 1),
	}
}

func (presenter *segmentScrollPresenter) Observe(
	config appconfig.SegmentScroll,
	snapshot controller.Snapshot,
) {
	target := segmentScrollTargetFor(config, snapshot)
	presenter.mu.Lock()
	changed := !presenter.desired.equal(target)
	if changed {
		presenter.desired = target
	}
	presenter.mu.Unlock()
	if changed {
		select {
		case presenter.wake <- struct{}{}:
		default:
		}
	}
}

func (presenter *segmentScrollPresenter) Run() {
	for {
		select {
		case <-presenter.ctx.Done():
			return
		case <-presenter.wake:
			presenter.applyLatest()
		}
	}
}

func (presenter *segmentScrollPresenter) applyLatest() {
	for presenter.ctx.Err() == nil {
		presenter.mu.Lock()
		target := presenter.desired
		if presenter.applied.equal(target) {
			presenter.mu.Unlock()
			return
		}
		presenter.mu.Unlock()

		if err := presenter.apply(target); err != nil {
			if presenter.onError != nil && presenter.ctx.Err() == nil {
				presenter.onError(err)
			}
			return
		}
		presenter.mu.Lock()
		presenter.applied = target
		current := presenter.desired
		presenter.mu.Unlock()
		if presenter.onState != nil {
			presenter.onState(target)
		}
		if current.equal(target) {
			return
		}
	}
}

func (presenter *segmentScrollPresenter) apply(target segmentScrollTarget) error {
	presenter.sendMu.Lock()
	defer presenter.sendMu.Unlock()
	requestContext, cancel := context.WithTimeout(presenter.ctx, 3*time.Second)
	defer cancel()
	text, dwell := "", time.Duration(0)
	if target.active {
		text, dwell = target.text, target.dwell
	}
	if err := presenter.client.SetSegmentText(requestContext, text, dwell); err != nil {
		return fmt.Errorf("HOST segment scroll: %w", err)
	}
	if _, err := presenter.client.RefreshFrontPanel(requestContext); err != nil &&
		presenter.onError != nil {
		presenter.onError(fmt.Errorf("HOST segment preview refresh: %w", err))
	}
	return nil
}

// PrepareDisconnect releases a persistent scroll while the authenticated
// session is still writable. A physical disconnect remains safe because the
// firmware independently falls back to its local OPEN/CLSD Door rendering.
func (presenter *segmentScrollPresenter) PrepareDisconnect(ctx context.Context) error {
	presenter.mu.Lock()
	wasActive := presenter.desired.active || presenter.applied.active
	presenter.desired = segmentScrollTarget{}
	presenter.mu.Unlock()
	if !wasActive {
		return nil
	}
	presenter.sendMu.Lock()
	defer presenter.sendMu.Unlock()
	err := presenter.client.SetSegmentText(ctx, "", 0)
	if err == nil {
		presenter.mu.Lock()
		presenter.applied = segmentScrollTarget{}
		presenter.mu.Unlock()
		if presenter.onState != nil {
			presenter.onState(segmentScrollTarget{})
		}
	}
	return err
}

func segmentScrollTargetFor(
	config appconfig.SegmentScroll,
	snapshot controller.Snapshot,
) segmentScrollTarget {
	if !config.Enabled || !snapshot.Connected || !snapshot.HaveStatus ||
		!snapshot.Hello.IsPCController() ||
		!segmentScrollPageEnabled(config.Pages, snapshot.Status.MenuPage) {
		return segmentScrollTarget{}
	}
	text := config.DoorClosedText
	if snapshot.Status.DoorOpen {
		text = config.DoorOpenText
	}
	text += strings.Repeat(" ", config.GapCells)
	return segmentScrollTarget{
		active: true, page: snapshot.Status.MenuPage, text: text,
		dwell: time.Duration(config.SpeedMS) * time.Millisecond,
	}
}

func segmentScrollPageEnabled(references []string, current byte) bool {
	for _, reference := range references {
		reference = strings.TrimSpace(reference)
		if number, err := strconv.ParseUint(reference, 0, 8); err == nil {
			if byte(number) == current {
				return true
			}
			continue
		}
		for _, page := range controller.MenuPages() {
			if page.ID != current {
				continue
			}
			for _, candidate := range []string{page.Key, page.Name, page.Label} {
				if strings.EqualFold(strings.TrimSpace(candidate), reference) {
					return true
				}
			}
		}
	}
	return false
}
