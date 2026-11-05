//go:build windows

package notify

import (
	"encoding/xml"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/i18n"
)

func TestConfigureToastRequiresOptInRegistration(t *testing.T) {
	t.Parallel()

	manager := &Manager{}
	err := manager.configureToast()
	if !errors.Is(err, ErrRegistrationRequired) {
		t.Fatalf("configureToast() error = %v, want ErrRegistrationRequired", err)
	}
}

func TestPortableAutoModeFallsBackToTrayBalloon(t *testing.T) {
	t.Parallel()

	delivered := make(chan struct{}, 1)
	unexpectedPush := make(chan struct{}, 1)
	manager := &Manager{
		mode:       ModeAuto,
		balloon:    func(string, string) error { delivered <- struct{}{}; return nil },
		requests:   make(chan toastRequest),
		stop:       make(chan struct{}),
		workerDone: make(chan struct{}),

		requestTimeout: 500 * time.Millisecond,
		closeTimeout:   500 * time.Millisecond,
	}
	manager.setupToast = manager.configureToast
	manager.pushToast = func(toastRequest) error {
		unexpectedPush <- struct{}{}
		return nil
	}
	go manager.toastWorker()
	defer manager.Close()

	if err := manager.Show("PCController", "Portable fallback", false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delivered:
	case <-unexpectedPush:
		t.Fatal("portable mode unexpectedly attempted a modern toast")
	case <-time.After(time.Second):
		t.Fatal("portable auto mode did not use the tray balloon")
	}
}

func TestToastXMLTreatsRemoteMessageAsText(t *testing.T) {
	payload, err := makeToastXML(
		`Device <status>`,
		`hello ]]> <action arguments="off"/> & goodbye`,
		false,
		`C:\Program Files\PCController\toast icon.png`,
	)
	if err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, payload)
	}
	if strings.Count(payload, "<action ") != 2 {
		t.Fatalf("remote text injected an XML action:\n%s", payload)
	}
	if strings.Contains(payload, "<status>") {
		t.Fatalf("title was not XML-escaped:\n%s", payload)
	}
	if !strings.Contains(payload, "file:///C:/Program%20Files/") {
		t.Fatalf("icon file URL was not encoded:\n%s", payload)
	}
}

func TestPersianToastCarriesLanguageAndLocalizedActions(t *testing.T) {
	payload, err := makeLocalizedToastXML(
		"PCController",
		"وضعیت چراغ تغییر کرد",
		false,
		i18n.New(i18n.Persian),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`lang="fa-IR"`,
		`content="روشن کردن"`,
		`content="خاموش کردن"`,
		`arguments="on"`,
		`arguments="off"`,
	} {
		if !strings.Contains(payload, want) {
			t.Fatalf("Persian toast XML missing %q:\n%s", want, payload)
		}
	}
}

func TestCleanRemoteTextBoundsAndControls(t *testing.T) {
	got := cleanRemoteText(" \x00hello\x01 world ", 8)
	if got != "hello wo" {
		t.Fatalf("cleanRemoteText = %q", got)
	}
}

func TestShowTimesOutStalledToastAndUsesBalloonFallback(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	fallback := make(chan struct{}, 1)

	manager := newTestManager(
		ModeAuto,
		100*time.Millisecond,
		func(title, message string) error {
			fallback <- struct{}{}
			return nil
		},
		func(toastRequest) error {
			startedOnce.Do(func() {
				close(started)
			})
			<-release
			return nil
		},
	)

	begin := time.Now()
	if err := manager.Show("Device", "Still bounded", false); err != nil {
		close(release)
		_ = manager.Close()
		t.Fatalf("Show: %v", err)
	}
	if elapsed := time.Since(begin); elapsed > time.Second {
		close(release)
		_ = manager.Close()
		t.Fatalf("Show took %s", elapsed)
	}
	select {
	case <-started:
	default:
		close(release)
		_ = manager.Close()
		t.Fatal("toast worker never received the request")
	}
	select {
	case <-fallback:
	default:
		close(release)
		_ = manager.Close()
		t.Fatal("stalled toast did not use the balloon fallback")
	}

	close(release)
	if err := manager.Close(); err != nil {
		t.Fatalf("Close after releasing worker: %v", err)
	}
}

func TestCloseIsBoundedWhenToastWorkerStalls(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	manager := newTestManager(
		ModeToast,
		75*time.Millisecond,
		nil,
		func(toastRequest) error {
			close(started)
			<-release
			return nil
		},
	)
	manager.requestTimeout = time.Second

	showDone := make(chan error, 1)
	go func() {
		showDone <- manager.Show("Device", "Closing", false)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		_ = manager.Close()
		t.Fatal("toast worker never received the request")
	}

	begin := time.Now()
	err := manager.Close()
	if err == nil {
		close(release)
		t.Fatal("Close did not report the stalled worker")
	}
	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		close(release)
		t.Fatalf("Close took %s", elapsed)
	}
	select {
	case err := <-showDone:
		if !errors.Is(err, errManagerClosed) {
			close(release)
			t.Fatalf("Show error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		close(release)
		t.Fatal("Show remained blocked after Close")
	}
	if err := manager.Show("Device", "After close", false); !errors.Is(err, errManagerClosed) {
		close(release)
		t.Fatalf("Show after Close error = %v", err)
	}

	close(release)
	select {
	case <-manager.workerDone:
	case <-time.After(time.Second):
		t.Fatal("toast worker did not exit after the stalled request completed")
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close after worker exit: %v", err)
	}
}

func newTestManager(
	mode Mode,
	timeout time.Duration,
	balloon BalloonFunc,
	push func(toastRequest) error,
) *Manager {
	manager := &Manager{
		mode:           mode,
		balloon:        balloon,
		requests:       make(chan toastRequest),
		stop:           make(chan struct{}),
		workerDone:     make(chan struct{}),
		requestTimeout: timeout,
		closeTimeout:   timeout,
		setupToast: func() error {
			return nil
		},
		pushToast: push,
	}
	go manager.toastWorker()
	return manager
}
