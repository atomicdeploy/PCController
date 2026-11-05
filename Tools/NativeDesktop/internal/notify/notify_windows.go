//go:build windows

package notify

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"git.sr.ht/~jackmordaunt/go-toast/wintoast"

	"rayanlamp.local/app/assets"
	"rayanlamp.local/app/internal/buildinfo"
	"rayanlamp.local/app/internal/i18n"
)

type Mode string

const (
	ModeAuto    Mode = "auto"
	ModeToast   Mode = "toast"
	ModeBalloon Mode = "balloon"
	ModeOff     Mode = "off"

	defaultRequestTimeout = 5 * time.Second
	defaultCloseTimeout   = 2 * time.Second
)

var (
	errManagerClosed = errors.New("notification manager is closed")

	// ErrRegistrationRequired means modern toast delivery was intentionally
	// disabled because this portable executable does not own an opt-in shell
	// registration. Auto mode can still fall back to the tray balloon.
	ErrRegistrationRequired = errors.New(
		"modern toast delivery requires opt-in Windows shell registration",
	)
)

type BalloonFunc func(title, message string) error
type ActivationFunc func(action string)

type Options struct {
	// AllowPersistentRegistration permits the toast library to write its
	// per-user AppUserModelID/activator metadata. Keep this false for portable
	// startup; set it only after ownership of an explicit shell registration
	// has been verified.
	AllowPersistentRegistration bool

	// Locale controls native toast language metadata and localized action
	// labels. Auto follows the current Windows language.
	Locale i18n.Locale
}

type Manager struct {
	mu         sync.RWMutex
	mode       Mode
	iconPath   string
	balloon    BalloonFunc
	activation ActivationFunc
	requests   chan toastRequest
	stop       chan struct{}
	workerDone chan struct{}
	closeOnce  sync.Once
	localizer  i18n.Localizer

	requestTimeout              time.Duration
	closeTimeout                time.Duration
	allowPersistentRegistration bool
	setupToast                  func() error
	pushToast                   func(toastRequest) error
}

type toastRequest struct {
	title   string
	message string
	audible bool
	result  chan error
}

func New(
	cacheDir string,
	mode Mode,
	balloon BalloonFunc,
	activation ActivationFunc,
	options ...Options,
) (*Manager, error) {
	if cacheDir == "" {
		return nil, fmt.Errorf("notification cache directory is empty")
	}
	if len(options) > 1 {
		return nil, fmt.Errorf("notification manager accepts at most one options value")
	}
	var opts Options
	if len(options) == 1 {
		opts = options[0]
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("create notification cache: %w", err)
	}

	iconData, err := assets.Files.ReadFile("icons/app.png")
	if err != nil {
		return nil, fmt.Errorf("read embedded toast icon: %w", err)
	}
	iconPath := filepath.Join(cacheDir, "toast-icon.png")
	if err := writeIfChanged(iconPath, iconData); err != nil {
		return nil, err
	}

	m := &Manager{
		mode:       normalizeMode(mode),
		iconPath:   iconPath,
		balloon:    balloon,
		activation: activation,
		requests:   make(chan toastRequest),
		stop:       make(chan struct{}),
		workerDone: make(chan struct{}),

		requestTimeout:              defaultRequestTimeout,
		closeTimeout:                defaultCloseTimeout,
		allowPersistentRegistration: opts.AllowPersistentRegistration,
		localizer:                   i18n.New(opts.Locale),
	}
	m.setupToast = m.configureToast
	m.pushToast = m.deliverToast
	// This callback handles activation while the process is alive. We
	// intentionally do not publish an ActivationExe here, so a toast can never
	// create an unquoted or unsupported cold-start COM server registration.
	wintoast.SetActivationCallback(func(_ string, args string, _ []wintoast.UserData) {
		m.mu.RLock()
		fn := m.activation
		m.mu.RUnlock()
		if fn != nil {
			fn(strings.TrimSpace(args))
		}
	})
	go m.toastWorker()
	return m, nil
}

func (m *Manager) SetMode(mode Mode) {
	m.mu.Lock()
	m.mode = normalizeMode(mode)
	m.mu.Unlock()
}

// SetLocale updates subsequent toast language metadata and action labels
// without recreating the notification worker.
func (m *Manager) SetLocale(locale i18n.Locale) {
	m.mu.Lock()
	m.localizer = i18n.New(locale)
	m.mu.Unlock()
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.activation = nil
		close(m.stop)
		m.mu.Unlock()
	})

	timeout := m.closeTimeout
	if timeout <= 0 {
		timeout = defaultCloseTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-m.workerDone:
		return nil
	case <-timer.C:
		return fmt.Errorf("close notification manager: worker did not stop within %s", timeout)
	}
}

func (m *Manager) Show(title, message string, audible bool) error {
	m.mu.RLock()
	mode := m.mode
	balloon := m.balloon
	m.mu.RUnlock()
	return m.showWithMode(mode, balloon, title, message, audible)
}

// ShowTest exercises the configured delivery mechanism even when ordinary
// notifications are disabled. It does not mutate the manager's live mode, so
// a test cannot race with event delivery or silently re-enable notifications.
func (m *Manager) ShowTest(title, message string, audible bool) error {
	m.mu.RLock()
	mode := m.mode
	balloon := m.balloon
	m.mu.RUnlock()
	if mode == ModeOff {
		mode = ModeAuto
	}
	return m.showWithMode(mode, balloon, title, message, audible)
}

func (m *Manager) showWithMode(
	mode Mode,
	balloon BalloonFunc,
	title string,
	message string,
	audible bool,
) error {
	title = strings.TrimSpace(title)
	message = strings.TrimSpace(message)
	if title == "" {
		title = buildinfo.DisplayName
	}
	if message == "" || mode == ModeOff {
		return nil
	}
	select {
	case <-m.stop:
		return errManagerClosed
	default:
	}

	if mode == ModeBalloon {
		if balloon == nil {
			return fmt.Errorf("legacy balloon notification is unavailable")
		}
		return balloon(title, message)
	}

	result := make(chan error, 1)
	request := toastRequest{title: title, message: message, audible: audible, result: result}
	timeout := m.requestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case m.requests <- request:
	case <-m.stop:
		return errManagerClosed
	case <-timer.C:
		return m.handleToastError(mode, balloon, title, message, errors.New("notification worker is unavailable"))
	}

	select {
	case err := <-result:
		if err == nil {
			return nil
		}
		return m.handleToastError(mode, balloon, title, message, err)
	case <-m.stop:
		return errManagerClosed
	case <-timer.C:
		return m.handleToastError(mode, balloon, title, message, errors.New("notification request timed out"))
	}
}

func (m *Manager) handleToastError(
	mode Mode,
	balloon BalloonFunc,
	title string,
	message string,
	err error,
) error {
	if err == nil {
		return nil
	}
	if mode == ModeToast || balloon == nil {
		return fmt.Errorf("show Windows toast: %w", err)
	}
	return balloon(title, message)
}

func (m *Manager) toastWorker() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(m.workerDone)

	setupErr := m.setupToast()
	for {
		select {
		case <-m.stop:
			return
		case request := <-m.requests:
			select {
			case <-m.stop:
				select {
				case request.result <- errManagerClosed:
				default:
				}
				return
			default:
			}
			err := setupErr
			if err == nil {
				err = m.pushToast(request)
			}
			select {
			case request.result <- err:
			default:
			}
		}
	}
}

func (m *Manager) configureToast() error {
	if !m.allowPersistentRegistration {
		return ErrRegistrationRequired
	}
	appData := wintoast.AppData{
		AppID:               "YektaYar.RayanLamp",
		GUID:                "{66F0C709-6E91-45BE-9013-5A96219E6D3D}",
		IconPath:            m.iconPath,
		IconBackgroundColor: "#10283A",
	}
	// Do not ask the toast library to create a LocalServer32 registration.
	// Its raw executable value is not safely quoted, and this process does not
	// implement a cold-start server for that callback class. Live activation
	// remains available through the class factory registered by wintoast.Push.
	return wintoast.SetAppData(appData)
}

func (m *Manager) deliverToast(request toastRequest) error {
	m.mu.RLock()
	localizer := m.localizer
	m.mu.RUnlock()
	payload, err := makeLocalizedToastXML(
		request.title,
		request.message,
		request.audible,
		localizer,
		m.iconPath,
	)
	if err == nil {
		// Deliberately omit PowershellFallback. The caller uses the already
		// present tray icon for a genuine legacy NIF_INFO balloon instead.
		err = wintoast.Push(payload)
	}
	return err
}

type toastDocument struct {
	XMLName        xml.Name     `xml:"toast"`
	ActivationType string       `xml:"activationType,attr"`
	Launch         string       `xml:"launch,attr"`
	Duration       string       `xml:"duration,attr"`
	Visual         toastVisual  `xml:"visual"`
	Audio          toastAudio   `xml:"audio"`
	Actions        toastActions `xml:"actions"`
}

type toastVisual struct {
	Language string       `xml:"lang,attr,omitempty"`
	Binding  toastBinding `xml:"binding"`
}

type toastBinding struct {
	Template string      `xml:"template,attr"`
	Image    *toastImage `xml:"image,omitempty"`
	Text     []toastText `xml:"text"`
}

type toastImage struct {
	Placement string `xml:"placement,attr"`
	Source    string `xml:"src,attr"`
}

type toastText struct {
	Value string `xml:",chardata"`
}

type toastAudio struct {
	Source string `xml:"src,attr,omitempty"`
	Silent string `xml:"silent,attr,omitempty"`
}

type toastActions struct {
	Action []toastAction `xml:"action"`
}

type toastAction struct {
	ActivationType string `xml:"activationType,attr"`
	Content        string `xml:"content,attr"`
	Arguments      string `xml:"arguments,attr"`
}

func makeToastXML(title, message string, audible bool, iconPath ...string) (string, error) {
	return makeLocalizedToastXML(
		title,
		message,
		audible,
		i18n.New(i18n.English),
		iconPath...,
	)
}

func makeLocalizedToastXML(
	title,
	message string,
	audible bool,
	localizer i18n.Localizer,
	iconPath ...string,
) (string, error) {
	audio := toastAudio{Silent: "true"}
	if audible {
		audio = toastAudio{Source: "ms-winsoundevent:Notification.Default"}
	}
	metadata := localizer.Metadata()
	doc := toastDocument{
		ActivationType: "foreground",
		Launch:         "show",
		Duration:       "short",
		Visual: toastVisual{Language: metadata.LanguageTag, Binding: toastBinding{
			Template: "ToastGeneric",
			Text: []toastText{
				{Value: cleanRemoteText(title, 96)},
				{Value: cleanRemoteText(message, 768)},
			},
		}},
		Audio: audio,
		Actions: toastActions{Action: []toastAction{
			{
				ActivationType: "foreground",
				Content:        localizer.Text(i18n.MsgActionTurnOn),
				Arguments:      "on",
			},
			{
				ActivationType: "foreground",
				Content:        localizer.Text(i18n.MsgActionTurnOff),
				Arguments:      "off",
			},
		}},
	}
	if len(iconPath) > 0 && iconPath[0] != "" {
		urlPath := filepath.ToSlash(iconPath[0])
		if len(urlPath) >= 2 && urlPath[1] == ':' {
			urlPath = "/" + urlPath
		}
		iconURL := &url.URL{Scheme: "file", Path: urlPath}
		doc.Visual.Binding.Image = &toastImage{
			Placement: "appLogoOverride",
			Source:    iconURL.String(),
		}
	}
	data, err := xml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("encode toast XML: %w", err)
	}
	return string(data), nil
}

func cleanRemoteText(value string, limit int) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	capacity := len(value)
	if capacity > limit {
		capacity = limit
	}
	b.Grow(capacity)
	for _, r := range value {
		if b.Len() >= limit {
			break
		}
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeMode(mode Mode) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case ModeToast:
		return ModeToast
	case ModeBalloon:
		return ModeBalloon
	case ModeOff:
		return ModeOff
	default:
		return ModeAuto
	}
}

func writeIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write toast icon: %w", err)
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install toast icon: %w", err)
	}
	return nil
}
