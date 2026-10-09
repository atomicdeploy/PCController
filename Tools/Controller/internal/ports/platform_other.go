//go:build !windows

package ports

import (
	"context"
	"time"
)

func enrichPlatform(values []Info) []Info {
	return values
}

func watchPlatformChanges(ctx context.Context) (<-chan Change, error) {
	changes := make(chan Change, 1)
	go func() {
		defer close(changes)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case at := <-ticker.C:
				select {
				case changes <- Change{
					At: at, Reason: "serial enumeration fallback",
				}:
				default:
				}
			}
		}
	}()
	return changes, nil
}
