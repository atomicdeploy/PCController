package appconfig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestWatchRegistrationRetriesOnlyTransientMissingEntries(t *testing.T) {
	missing := fmt.Errorf("temporary directory entry: %w", os.ErrNotExist)
	for _, test := range []struct {
		name     string
		failures []error
		want     error
	}{
		{name: "success", failures: []error{nil}},
		{name: "atomic rename", failures: []error{missing, nil}},
		{name: "second atomic rename", failures: []error{missing, missing, nil}},
		{name: "persistent missing directory", failures: []error{missing, missing, missing}, want: os.ErrNotExist},
		{name: "permissions", failures: []error{os.ErrPermission}, want: os.ErrPermission},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := retryWatchRegistration(context.Background(), func() error {
				if calls >= len(test.failures) {
					t.Fatal("unexpected registration retry")
				}
				failure := test.failures[calls]
				calls++
				return failure
			})
			if !errors.Is(err, test.want) || calls != len(test.failures) {
				t.Fatalf("registration err=%v calls=%d; want err=%v calls=%d", err, calls, test.want, len(test.failures))
			}
		})
	}
}

func TestWatchRegistrationCancellationStopsRetries(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprintf("before-registration=%t", before), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if before {
				cancel()
			}
			calls := 0
			err := retryWatchRegistration(ctx, func() error {
				calls++
				cancel()
				return os.ErrNotExist
			})
			wantCalls := 1
			if before {
				wantCalls = 0
			}
			if !errors.Is(err, context.Canceled) || calls != wantCalls {
				t.Fatalf("registration err=%v calls=%d; want canceled after %d calls", err, calls, wantCalls)
			}
		})
	}
}
