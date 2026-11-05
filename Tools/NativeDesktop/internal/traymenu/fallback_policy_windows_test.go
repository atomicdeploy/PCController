//go:build windows

package traymenu

import (
	"errors"
	"testing"
)

func TestStyledPolicyReturnsFirstCustomSuccess(t *testing.T) {
	var customCalls, nativeCalls, discardCalls int
	want := Result{Action: "toggle", BackdropUsed: BackdropAcrylic}
	got, err := showWithPolicy(
		Options{},
		false,
		func(options Options) (Result, error) {
			customCalls++
			if options.DisableWindowReuse {
				t.Fatal("first custom attempt unexpectedly disabled reuse")
			}
			return want, nil
		},
		func() (Result, error) {
			nativeCalls++
			return Result{}, nil
		},
		func() { discardCalls++ },
	)
	if err != nil {
		t.Fatalf("showWithPolicy: %v", err)
	}
	if got != want || customCalls != 1 || nativeCalls != 0 || discardCalls != 0 {
		t.Fatalf(
			"got=%+v custom=%d native=%d discard=%d",
			got,
			customCalls,
			nativeCalls,
			discardCalls,
		)
	}
}

func TestStyledPolicyRetriesWithFreshWindowWithoutNativeDowngrade(t *testing.T) {
	initial := errors.New("stale cached HWND")
	var customCalls, nativeCalls, discardCalls int
	got, err := showWithPolicy(
		Options{},
		false,
		func(options Options) (Result, error) {
			customCalls++
			if customCalls == 1 {
				return Result{}, initial
			}
			if !options.DisableWindowReuse {
				t.Fatal("fresh retry did not disable window reuse")
			}
			return Result{Action: "settings", BackdropUsed: BackdropBlur}, nil
		},
		func() (Result, error) {
			nativeCalls++
			return Result{}, nil
		},
		func() { discardCalls++ },
	)
	if err != nil {
		t.Fatalf("showWithPolicy: %v", err)
	}
	if got.Action != "settings" || got.UsedNativeFallback ||
		customCalls != 2 || nativeCalls != 0 || discardCalls != 1 {
		t.Fatalf(
			"got=%+v custom=%d native=%d discard=%d",
			got,
			customCalls,
			nativeCalls,
			discardCalls,
		)
	}
}

func TestStyledPolicyReportsBothFailuresAndNeverCallsNative(t *testing.T) {
	initial := errors.New("initial renderer failure")
	retry := errors.New("fresh renderer failure")
	var customCalls, nativeCalls int
	_, err := showWithPolicy(
		Options{DisableWindowReuse: true},
		false,
		func(options Options) (Result, error) {
			customCalls++
			if !options.DisableWindowReuse {
				t.Fatal("ephemeral attempt unexpectedly enabled reuse")
			}
			// The first call is already ephemeral; the bounded policy still
			// retries once to recover from transient USER/GDI pressure.
			if customCalls == 1 {
				return Result{}, initial
			}
			return Result{}, retry
		},
		func() (Result, error) {
			nativeCalls++
			return Result{}, nil
		},
		func() {},
	)
	if !errors.Is(err, ErrCustomPresentation) ||
		!errors.Is(err, initial) ||
		!errors.Is(err, retry) {
		t.Fatalf("error = %v; expected typed error preserving both causes", err)
	}
	if customCalls != 2 || nativeCalls != 0 {
		t.Fatalf("custom=%d native=%d", customCalls, nativeCalls)
	}
}

func TestStyledPolicyCancelsReentrantPopupWithoutRetry(t *testing.T) {
	var customCalls, nativeCalls, discardCalls int
	got, err := showWithPolicy(
		Options{},
		false,
		func(Options) (Result, error) {
			customCalls++
			return Result{}, errPopupAlreadyActive
		},
		func() (Result, error) {
			nativeCalls++
			return Result{}, nil
		},
		func() { discardCalls++ },
	)
	if err != nil {
		t.Fatalf("showWithPolicy: %v", err)
	}
	if !got.Cancelled || customCalls != 1 || nativeCalls != 0 || discardCalls != 0 {
		t.Fatalf(
			"got=%+v custom=%d native=%d discard=%d",
			got,
			customCalls,
			nativeCalls,
			discardCalls,
		)
	}
}

func TestStyledPolicyUsesNativeOnlyForExplicitOrHighContrast(t *testing.T) {
	tests := []struct {
		name         string
		options      Options
		highContrast bool
		wantReason   NativeReason
	}{
		{
			name:       "explicit",
			options:    Options{PreferNative: true},
			wantReason: NativeReasonExplicit,
		},
		{
			name:         "high contrast",
			highContrast: true,
			wantReason:   NativeReasonHighContrast,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var customCalls, nativeCalls, discardCalls int
			got, err := showWithPolicy(
				test.options,
				test.highContrast,
				func(Options) (Result, error) {
					customCalls++
					return Result{}, nil
				},
				func() (Result, error) {
					nativeCalls++
					return Result{Cancelled: true}, nil
				},
				func() { discardCalls++ },
			)
			if err != nil {
				t.Fatalf("showWithPolicy: %v", err)
			}
			if !got.Cancelled || !got.UsedNativeFallback ||
				got.NativeReason != test.wantReason ||
				customCalls != 0 || nativeCalls != 1 || discardCalls != 0 {
				t.Fatalf(
					"got=%+v custom=%d native=%d discard=%d",
					got,
					customCalls,
					nativeCalls,
					discardCalls,
				)
			}
		})
	}
}
