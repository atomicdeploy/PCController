//go:build linux

package managedbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	initialBrowserTarget            = "data:text/html,%3Cmeta%20name%3Dcolor-scheme%20content%3Ddark%3E%3Cstyle%3Ehtml%7Bbackground%3A%23070b14%7D%3C%2Fstyle%3E"
	legacySessionTokenCleanupScript = "try{sessionStorage.removeItem('pccontroller.session-token')}catch{}"
)

// Options describes a dedicated local browser application. Token is kept in
// the Go process and supplied only at the intercepted network boundary.
type Options struct {
	Executable string
	URL        string
	ProfileDir string
	Token      string
	Stdout     io.Writer
	Stderr     io.Writer
}

// Session owns the browser process and its private DevTools pipes.
type Session struct {
	command *exec.Cmd
	client  *protocolClient
	wait    chan error
	events  chan error
	ready   chan struct{}
	readyMu sync.Once
	paused  atomic.Uint64
	bound   atomic.Uint64
	proof   atomic.Uint64
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
}

// Start launches Chrome at an inert dark document, installs exact-origin request
// mediation, and only then navigates to the clean loopback application URL.
func Start(ctx context.Context, options Options) (*Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target, err := cleanLoopbackURL(options.URL)
	if err != nil {
		return nil, err
	}
	executable, err := cleanExecutable(options.Executable)
	if err != nil {
		return nil, err
	}
	profile, err := prepareProfileDirectory(options.ProfileDir)
	if err != nil {
		return nil, err
	}
	toChromeRead, toChromeWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create Chrome DevTools input pipe: %w", err)
	}
	fromChromeRead, fromChromeWrite, err := os.Pipe()
	if err != nil {
		_ = toChromeRead.Close()
		_ = toChromeWrite.Close()
		return nil, fmt.Errorf("create Chrome DevTools output pipe: %w", err)
	}
	closePipes := func() {
		_ = toChromeRead.Close()
		_ = toChromeWrite.Close()
		_ = fromChromeRead.Close()
		_ = fromChromeWrite.Close()
	}
	arguments := chromeArguments(profile)
	command := exec.Command(executable, arguments...)
	command.ExtraFiles = []*os.File{toChromeRead, fromChromeWrite}
	command.Stdin = nil
	command.Stdout = firstWriter(options.Stdout, io.Discard)
	command.Stderr = firstWriter(options.Stderr, io.Discard)
	command.Env = browserEnvironmentWithoutSecret(os.Environ(), options.Token)
	if err = command.Start(); err != nil {
		closePipes()
		return nil, fmt.Errorf("start managed Chrome application: %w", err)
	}
	_ = toChromeRead.Close()
	_ = fromChromeWrite.Close()
	client := newProtocolClient(toChromeWrite, fromChromeRead)
	lifetimeContext, lifetimeCancel := context.WithCancel(context.Background())
	session := &Session{
		command: command, client: client, wait: make(chan error, 1),
		events: make(chan error, 1), ready: make(chan struct{}),
		ctx: lifetimeContext, cancel: lifetimeCancel,
	}
	go func() {
		session.wait <- command.Wait()
		close(session.wait)
	}()
	bootstrapContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	origin := target.Scheme + "://" + target.Host
	if err = session.bootstrap(
		bootstrapContext, target.String(), origin, strings.TrimSpace(options.Token),
	); err != nil {
		session.stop()
		return nil, err
	}
	return session, nil
}

func chromeArguments(profile string) []string {
	return []string{
		"--app=" + initialBrowserTarget,
		"--user-data-dir=" + profile,
		"--remote-debugging-pipe",
		"--no-first-run",
		"--no-default-browser-check",
		"--noerrdialogs",
		"--disable-session-crashed-bubble",
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-sync",
	}
}

func browserEnvironmentWithoutSecret(environment []string, secret string) []string {
	secret = strings.TrimSpace(secret)
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if secret != "" && strings.Contains(entry, secret) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func cleanExecutable(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("managed browser executable is required")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("managed browser executable is not an executable regular file")
	}
	return filepath.Clean(absolute), nil
}

func prepareProfileDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("managed browser profile directory is required")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create managed browser profile: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("managed browser profile must be a real directory")
	}
	if err = os.Chmod(absolute, 0o700); err != nil {
		return "", fmt.Errorf("protect managed browser profile: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func firstWriter(value, fallback io.Writer) io.Writer {
	if value != nil {
		return value
	}
	return fallback
}

func (session *Session) bootstrap(ctx context.Context, navigationURL, targetOrigin, token string) error {
	targetID, err := session.waitForBlankTarget(ctx)
	if err != nil {
		return err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := session.client.call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": targetID, "flatten": true,
	}, &attached, false); err != nil || attached.SessionID == "" {
		if err == nil {
			err = errors.New("Chrome returned an empty target session")
		}
		return fmt.Errorf("attach managed Chrome target: %w", err)
	}
	go session.mediateRequests(session.ctx, attached.SessionID, targetOrigin, token, session.events)
	if err := session.client.call(ctx, attached.SessionID, "Page.enable", map[string]any{}, nil, false); err != nil {
		return fmt.Errorf("enable managed Chrome page: %w", err)
	}
	if err := session.client.call(ctx, attached.SessionID, "Page.addScriptToEvaluateOnNewDocument", map[string]string{
		"source": legacySessionTokenCleanupScript,
	}, nil, false); err != nil {
		return fmt.Errorf("clear legacy managed Chrome credential storage: %w", err)
	}
	if err := session.client.call(ctx, attached.SessionID, "Network.enable", map[string]any{}, nil, false); err != nil {
		return fmt.Errorf("observe managed Chrome authentication: %w", err)
	}
	if err := session.client.call(ctx, attached.SessionID, "Network.setBypassServiceWorker", map[string]bool{"bypass": true}, nil, false); err != nil {
		return fmt.Errorf("bypass browser storage for managed Chrome authentication: %w", err)
	}
	if err := session.client.call(ctx, attached.SessionID, "Fetch.enable", map[string]any{
		"patterns": []map[string]string{{"urlPattern": targetOrigin + "/api*", "requestStage": "Request"}},
	}, nil, false); err != nil {
		return fmt.Errorf("protect managed Chrome requests: %w", err)
	}
	var navigation struct {
		ErrorText string `json:"errorText"`
	}
	if err := session.client.call(ctx, attached.SessionID, "Page.navigate", map[string]string{"url": navigationURL}, &navigation, false); err != nil {
		return fmt.Errorf("navigate managed Chrome application: %w", err)
	}
	if navigation.ErrorText != "" {
		return errors.New("managed Chrome application navigation failed")
	}
	if err := session.client.call(ctx, "", "Target.activateTarget", map[string]string{"targetId": targetID}, nil, false); err != nil {
		return fmt.Errorf("activate managed Chrome application: %w", err)
	}
	select {
	case <-session.ready:
		return nil
	case err := <-session.events:
		return err
	case <-ctx.Done():
		return fmt.Errorf(
			"managed Chrome did not complete an authenticated API request before the readiness deadline (paused=%d bound=%d protected-responses=%d)",
			session.paused.Load(), session.bound.Load(), session.proof.Load(),
		)
	}
}

func (session *Session) waitForBlankTarget(ctx context.Context) (string, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var targets struct {
			TargetInfos []struct {
				ID   string `json:"targetId"`
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"targetInfos"`
		}
		if err := session.client.call(ctx, "", "Target.getTargets", map[string]any{}, &targets, false); err != nil {
			return "", fmt.Errorf("discover managed Chrome target: %w", err)
		}
		for _, target := range targets.TargetInfos {
			if target.Type == "page" && target.URL == initialBrowserTarget {
				return target.ID, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", errors.New("managed Chrome did not expose its inert application target")
		case <-ticker.C:
		}
	}
}

func (session *Session) mediateRequests(
	ctx context.Context,
	sessionID, targetOrigin, token string,
	errorsOut chan<- error,
) {
	authenticatedPaths := make(map[string]uint64)
	authority := newFrameAuthority(targetOrigin)
	for {
		event, ok := session.client.nextEvent(ctx)
		if !ok {
			return
		}
		if event.SessionID != sessionID {
			continue
		}
		switch event.Method {
		case "Page.frameNavigated":
			var navigated struct {
				Frame struct {
					ID  string `json:"id"`
					URL string `json:"url"`
				} `json:"frame"`
			}
			if json.Unmarshal(event.Params, &navigated) == nil {
				authority.navigate(navigated.Frame.ID, navigated.Frame.URL)
			}
			continue
		case "Page.frameStartedNavigating", "Page.frameRequestedNavigation":
			var starting struct {
				FrameID string `json:"frameId"`
			}
			if json.Unmarshal(event.Params, &starting) == nil {
				// Navigation initiation always revokes ambient authority. A frame
				// regains it only after an exact-origin commit is observed.
				authority.detach(starting.FrameID)
			}
			continue
		case "Page.navigatedWithinDocument":
			var navigated struct {
				FrameID string `json:"frameId"`
				URL     string `json:"url"`
			}
			if json.Unmarshal(event.Params, &navigated) == nil {
				authority.navigate(navigated.FrameID, navigated.URL)
			}
			continue
		case "Page.frameDetached":
			var detached struct {
				FrameID string `json:"frameId"`
			}
			if json.Unmarshal(event.Params, &detached) == nil {
				authority.detach(detached.FrameID)
			}
			continue
		case "Network.responseReceived":
			var received struct {
				RequestID string `json:"requestId"`
				Response  struct {
					URL    string  `json:"url"`
					Status float64 `json:"status"`
				} `json:"response"`
			}
			if json.Unmarshal(event.Params, &received) != nil || !authenticationProofRequest(targetOrigin, received.Response.URL) {
				continue
			}
			session.proof.Add(1)
			requestURL, _ := url.Parse(received.Response.URL)
			requestPath := requestURL.EscapedPath()
			remaining := authenticatedPaths[requestPath]
			injected := remaining != 0
			if !injected && token != "" {
				continue
			}
			if remaining <= 1 {
				delete(authenticatedPaths, requestPath)
			} else {
				authenticatedPaths[requestPath] = remaining - 1
			}
			status := int(received.Response.Status)
			if status >= 200 && status < 300 {
				session.readyMu.Do(func() { close(session.ready) })
				continue
			}
			if status == 401 || status == 403 {
				select {
				case errorsOut <- fmt.Errorf("managed Chrome authentication for %s was rejected with HTTP %d", requestPath, status):
				default:
				}
				return
			}
			continue
		case "Fetch.requestPaused":
			var paused struct {
				RequestID string `json:"requestId"`
				NetworkID string `json:"networkId"`
				FrameID   string `json:"frameId"`
				Request   struct {
					URL     string            `json:"url"`
					Headers map[string]string `json:"headers"`
				} `json:"request"`
			}
			if json.Unmarshal(event.Params, &paused) != nil || paused.RequestID == "" {
				select {
				case errorsOut <- errors.New("managed Chrome emitted an invalid paused request"):
				default:
				}
				return
			}
			params := map[string]any{"requestId": paused.RequestID}
			headers, authenticated := authenticatedRequestHeaders(
				authority.allows(paused.FrameID, paused.Request.URL),
				targetOrigin, paused.Request.URL, paused.Request.Headers, token,
			)
			if authenticated {
				session.paused.Add(1)
				params["headers"] = headers
				if request, err := url.Parse(paused.Request.URL); err == nil {
					authenticatedPaths[request.EscapedPath()]++
				}
				if paused.NetworkID != "" {
					session.bound.Add(1)
				}
			}
			requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := session.client.call(
				requestContext, sessionID, "Fetch.continueRequest", params, nil, authenticated,
			)
			cancel()
			if err != nil {
				select {
				case errorsOut <- fmt.Errorf("continue managed Chrome request: %w", err):
				default:
				}
				return
			}
		default:
			continue
		}
	}
}

// Wait blocks until Chrome exits or ctx is cancelled.
func (session *Session) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case err := <-session.wait:
		return normalizeProcessExit(err)
	case err := <-session.events:
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return errors.Join(err, session.Close(closeContext))
	case <-ctx.Done():
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return session.Close(closeContext)
	}
}

// Close stops the managed browser and waits for its exact child process.
func (session *Session) Close(ctx context.Context) error {
	if session == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	session.stop()
	select {
	case err := <-session.wait:
		return normalizeProcessExit(err)
	case <-ctx.Done():
		_ = session.command.Process.Kill()
		<-session.wait
		return nil
	}
}

func (session *Session) stop() {
	session.once.Do(func() {
		session.cancel()
		closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = session.client.call(closeContext, "", "Browser.close", map[string]any{}, nil, false)
		cancel()
		session.client.close()
	})
}

func normalizeProcessExit(err error) error {
	if err == nil {
		return nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ProcessState != nil && exitError.ProcessState.Success() {
		return nil
	}
	return fmt.Errorf("managed Chrome exited: %w", err)
}
