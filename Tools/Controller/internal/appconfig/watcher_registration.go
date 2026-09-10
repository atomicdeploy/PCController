package appconfig

import (
	"context"
	"errors"
	"os"
	"time"
)

// An atomic config replacement can remove a temporary file while kqueue is
// enumerating the directory for its initial watch. Retry only that transient
// failure; real watcher failures still use the existing reported polling path.
func retryWatchRegistration(ctx context.Context, register func() error) error {
	const attempts = 3
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := register()
		if err == nil || !errors.Is(err, os.ErrNotExist) || attempt+1 == attempts {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
