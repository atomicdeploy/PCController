package main

import (
	"context"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

type statusSubscriber interface {
	SubscribeStatus(context.Context, time.Duration) (<-chan controllerapi.StatusUpdate, error)
}

// subscribeConfiguredStatus keeps one stable consumer channel while replacing
// the underlying shared-sampler subscription whenever the authoritative host
// configuration changes. The old subscription is joined before the new one is
// created, so a TUI never owns two cadence requests at once.
func subscribeConfiguredStatus(
	ctx context.Context,
	client statusSubscriber,
	configuration <-chan appconfig.Config,
) <-chan controllerapi.StatusUpdate {
	output := make(chan controllerapi.StatusUpdate, 1)
	go func() {
		defer close(output)
		var (
			updates  <-chan controllerapi.StatusUpdate
			cancel   context.CancelFunc
			interval time.Duration
		)
		defer func() {
			if cancel != nil {
				cancel()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case value, ok := <-configuration:
				if !ok {
					configuration = nil
					continue
				}
				next := time.Duration(value.UI.StatusIntervalMS) * time.Millisecond
				if next == interval && updates != nil {
					continue
				}
				if cancel != nil {
					cancel()
					for range updates {
					}
				}
				subscriptionContext, stop := context.WithCancel(ctx)
				created, err := client.SubscribeStatus(subscriptionContext, next)
				if err != nil {
					stop()
					publishLatestStatus(output, controllerapi.StatusUpdate{
						Time: time.Now(), Error: err.Error(),
					})
					updates, cancel, interval = nil, nil, 0
					continue
				}
				updates, cancel, interval = created, stop, next
			case update, ok := <-updates:
				if !ok {
					updates, cancel, interval = nil, nil, 0
					continue
				}
				publishLatestStatus(output, update)
			}
		}
	}()
	return output
}

func publishLatestStatus(output chan controllerapi.StatusUpdate, update controllerapi.StatusUpdate) {
	select {
	case output <- update:
	default:
		select {
		case <-output:
		default:
		}
		select {
		case output <- update:
		default:
		}
	}
}
