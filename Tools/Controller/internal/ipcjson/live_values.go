package ipcjson

import (
	"context"
	controller "pccontroller.local/controller"
	"sort"
	"strings"
	"time"
)

// Each continuous domain retains one trailing value. Audio, display digits and
// lifecycle edges remain ordered and immediate. A quiet stream still flushes.
func streamLatestState(ctx context.Context, after uint64, interval time.Duration,
	read func(context.Context, uint64) (controller.Event, error), write func(controller.Event) error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	incoming := make(chan controller.Event, 1)
	go func() {
		defer close(incoming)
		for ctx.Err() == nil {
			event, err := read(ctx, after)
			if err != nil {
				return
			}
			after = event.ID
			select {
			case incoming <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	budget := make(stateFrameBudget)
	pending := make(map[string]controller.Event)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var wake <-chan time.Time
	arm := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		wake = nil
		delay := time.Hour
		for kind := range pending {
			left := time.Until(budget[kind].Add(interval))
			if left < delay {
				delay = left
			}
		}
		if len(pending) > 0 {
			if delay < 0 {
				delay = 0
			}
			timer.Reset(delay)
			wake = timer.C
		}
	}
	flush := func() bool {
		now := time.Now()
		ready := make([]controller.Event, 0, len(pending))
		for kind, event := range pending {
			if budget.allow(event, interval, now) {
				ready = append(ready, event)
				delete(pending, kind)
			}
		}
		sort.Slice(ready, func(i, j int) bool { return ready[i].ID < ready[j].ID })
		for _, event := range ready {
			if write(event) != nil {
				return false
			}
		}
		arm()
		return true
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			if !flush() {
				return
			}
		case event, ok := <-incoming:
			if !ok {
				return
			}
			key := strings.ToLower(strings.TrimSpace(event.Kind))
			if interval > 0 && highRateStateEvent(event) && !budget.allow(event, interval, time.Now()) {
				pending[key] = event
				arm()
				continue
			}
			delete(pending, key)
			if write(event) != nil {
				return
			}
			arm()
		}
	}
}
