//go:build windows

package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"rayanlamp.local/app/internal/config"
)

// OpenTransport selects and opens one transport. Auto mode attaches to a
// running GUI through IPC first, then its authenticated loopback API, before
// atomically becoming the headless primary itself.
func OpenTransport(
	ctx context.Context,
	options TransportOptions,
) (Transport, error) {
	options, err := normalizeTransportOptions(options)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		cfg           config.Config
		configPath    string
		configErr     error
		configAttempt bool
	)
	loadConfig := func() error {
		if configAttempt {
			return configErr
		}
		configAttempt = true
		configPath, configErr = config.ResolvePath(options.ConfigPath)
		if configErr != nil {
			return configErr
		}
		cfg, configErr = config.Load(configPath)
		return configErr
	}
	probeContext := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, options.RequestTimeout)
	}

	openIPC := func() (Transport, error) {
		probeCtx, cancel := probeContext()
		defer cancel()
		return newIPCTransport(probeCtx)
	}
	openAPI := func() (Transport, error) {
		rawURL := options.APIURL
		token := options.APIToken
		if rawURL == "" || token == "" {
			if loadErr := loadConfig(); loadErr != nil {
				return nil, fmt.Errorf("load configuration for local API: %w", loadErr)
			}
			if rawURL == "" {
				rawURL = "http://" + cfg.Integration.LocalAPIListen
			}
			if token == "" {
				token = cfg.Integration.LocalAPIToken
			}
		}
		transport, apiErr := newAPITransport(
			rawURL,
			token,
			options.RequestTimeout,
			(*http.Client)(nil),
		)
		if apiErr != nil {
			return nil, apiErr
		}
		probeCtx, cancel := probeContext()
		defer cancel()
		if _, apiErr = transport.Status(probeCtx); apiErr != nil {
			_ = transport.Close()
			return nil, apiErr
		}
		return transport, nil
	}
	openStandalone := func() (Transport, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if loadErr := loadConfig(); loadErr != nil {
			return nil, loadErr
		}
		return newStandaloneTransport(cfg, configPath)
	}

	switch options.Mode {
	case ModeIPC:
		transport, openErr := openIPC()
		if openErr != nil {
			return nil, fmt.Errorf("IPC mode requires a running RayanLamp primary: %w", openErr)
		}
		return transport, nil
	case ModeAPI:
		transport, openErr := openAPI()
		if openErr != nil {
			return nil, fmt.Errorf("API mode could not attach to the local RayanLamp API: %w", openErr)
		}
		return transport, nil
	case ModeStandalone:
		transport, openErr := openStandalone()
		if openErr != nil {
			return nil, fmt.Errorf(
				"standalone mode could not become the headless primary: %w",
				openErr,
			)
		}
		return transport, nil
	}

	var attempts []error
	if transport, openErr := openIPC(); openErr == nil {
		return transport, nil
	} else {
		attempts = append(attempts, openErr)
	}

	apiAttempted := false
	tryAPI := options.APIURL != ""
	if !tryAPI {
		if loadErr := loadConfig(); loadErr != nil {
			attempts = append(attempts, loadErr)
		} else {
			tryAPI = cfg.Integration.LocalAPIEnabled
		}
	}
	if tryAPI {
		apiAttempted = true
		if transport, openErr := openAPI(); openErr == nil {
			return transport, nil
		} else {
			attempts = append(attempts, openErr)
		}
	}
	if transport, openErr := openStandalone(); openErr == nil {
		return transport, nil
	} else {
		attempts = append(attempts, openErr)
	}

	// The GUI can win the primary-instance race between our first IPC probe
	// and ListenPipe. Retry only the side-effect-free attachment probes. Never
	// retry a lamp command across transports.
	if transport, openErr := openIPC(); openErr == nil {
		return transport, nil
	} else {
		attempts = append(attempts, openErr)
	}
	if apiAttempted {
		if transport, openErr := openAPI(); openErr == nil {
			return transport, nil
		} else {
			attempts = append(attempts, openErr)
		}
	}
	// A racing primary can claim the pipe, fail during startup, and disappear
	// before either reattachment probe succeeds. Make one final atomic
	// ownership attempt so auto mode can still become the headless primary.
	if transport, openErr := openStandalone(); openErr == nil {
		return transport, nil
	} else {
		attempts = append(attempts, openErr)
	}

	return nil, fmt.Errorf(
		"automatic transport selection failed: %w",
		sanitizeSelectionErrors(attempts),
	)
}

func sanitizeSelectionErrors(attempts []error) error {
	filtered := make([]error, 0, len(attempts))
	for _, err := range attempts {
		if err == nil {
			continue
		}
		// Errors never include bearer values; still avoid returning a giant
		// repeated chain when auto mode tried the same endpoint twice.
		message := strings.TrimSpace(err.Error())
		duplicate := false
		for _, previous := range filtered {
			if previous.Error() == message {
				duplicate = true
				break
			}
		}
		if !duplicate {
			filtered = append(filtered, errors.New(message))
		}
	}
	if len(filtered) == 0 {
		return errors.New("no transport was available")
	}
	return errors.Join(filtered...)
}
