package hostui

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

type AppAction struct {
	Kind   string `json:"kind"`
	Value  string `json:"value,omitempty"`
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
	// OperationID correlates an exact-target delivery with its coordinator
	// outcome. Navigation synchronization keeps its independent coordinator
	// operation metadata and must not manufacture this field.
	OperationID string            `json:"operation_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	At          time.Time         `json:"at"`
}

type ActionBroker struct {
	events     chan AppAction
	mu         sync.RWMutex
	subscribed bool
	watch      func(AppAction)
}

func NewActionBroker() *ActionBroker {
	return &ActionBroker{events: make(chan AppAction, 64)}
}

// Events enables the optional bounded queue used by the interactive TUI. Web,
// tray, service, and one-shot hosts intentionally rely on the observer-backed
// runtime event stream and never subscribe, so those surfaces cannot fill an
// unread TUI queue.
func (broker *ActionBroker) Events() <-chan AppAction {
	broker.mu.Lock()
	broker.subscribed = true
	events := broker.events
	broker.mu.Unlock()
	return events
}

// SetObserver installs a single delivery observer without changing the
// broker's original bounded TUI queue. The observer is used by the primary
// process to mirror valid application actions into the typed runtime event
// history, whose subscribers are independently cursor-based. Observer
// delivery remains independent when a headless process does not drain the
// optional TUI queue.
func (broker *ActionBroker) SetObserver(observer func(AppAction)) {
	broker.mu.Lock()
	broker.watch = observer
	broker.mu.Unlock()
}

func (broker *ActionBroker) Publish(action AppAction) error {
	var err error
	action, err = NormalizeAppAction(action)
	if err != nil {
		return err
	}
	broker.mu.RLock()
	observer := broker.watch
	subscribed := broker.subscribed
	broker.mu.RUnlock()
	if !subscribed {
		if observer != nil {
			observer(cloneAppAction(action))
		}
		return nil
	}
	select {
	case broker.events <- cloneAppAction(action):
		if observer != nil {
			observer(cloneAppAction(action))
		}
		return nil
	default:
		if observer != nil {
			observer(cloneAppAction(action))
		}
		return errors.New("app action queue is full")
	}
}

// NormalizeAppAction validates one living, versionless application action and
// returns the exact normalized value which will be delivered. Coordinators use
// this before freezing a target set so an invalid command cannot create a
// partially published operation.
func NormalizeAppAction(action AppAction) (AppAction, error) {
	action.Kind = strings.ToLower(strings.TrimSpace(action.Kind))
	action.Value = strings.TrimSpace(action.Value)
	action.Target = strings.TrimSpace(action.Target)
	action.OperationID = strings.TrimSpace(action.OperationID)
	if action.Target != "" && action.Target != "*" && !instanceIDPattern.MatchString(action.Target) {
		return AppAction{}, errors.New("app action target must be *, or a valid instance id or surface")
	}
	if action.OperationID != "" && !instanceIDPattern.MatchString(action.OperationID) {
		return AppAction{}, errors.New("app action operation_id is invalid")
	}
	if action.At.IsZero() {
		action.At = time.Now()
	}
	metadata, err := normalizeAppActionMetadata(action.Metadata)
	if err != nil {
		return AppAction{}, err
	}
	action.Metadata = metadata
	switch action.Kind {
	case "app.page":
		if action.Value == "" {
			return AppAction{}, errors.New("app.page requires a page name")
		}
	case "app.title":
		if !strings.EqualFold(action.Value, "auto") {
			var err error
			action.Value, err = ValidateTerminalTitle(action.Value)
			if err != nil {
				return AppAction{}, err
			}
		}
	case "app.osc":
		var err error
		action.Value, err = ValidateOSCPayload(action.Value)
		if err != nil {
			return AppAction{}, err
		}
	case "app.progress":
		if _, err := ParseTerminalProgress(action.Value); err != nil {
			return AppAction{}, err
		}
	case "app.quit", "app.port.open", "app.port.close":
		if action.Value != "" {
			return AppAction{}, fmt.Errorf("%s does not accept a value", action.Kind)
		}
	case "command":
		if action.Value == "" {
			return AppAction{}, errors.New("command action requires a command")
		}
	default:
		return AppAction{}, fmt.Errorf("unsupported app action %q", action.Kind)
	}
	return action, nil
}

func normalizeAppActionMetadata(values map[string]string) (map[string]string, error) {
	if len(values) > 16 {
		return nil, errors.New("app action metadata exceeds 16 entries")
	}
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(values))
	for rawKey, rawValue := range values {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		value := strings.TrimSpace(rawValue)
		if !instanceValuePattern.MatchString(key) || instanceSecretPattern.MatchString(key) {
			return nil, errors.New("app action metadata key is invalid or credential-like")
		}
		if len(value) > 1024 || strings.ContainsAny(value, "\x00\r\n") {
			return nil, errors.New("app action metadata value is too long or contains controls")
		}
		result[key] = value
	}
	return result, nil
}

func cloneAppAction(action AppAction) AppAction {
	if action.Metadata != nil {
		metadata := make(map[string]string, len(action.Metadata))
		for key, value := range action.Metadata {
			metadata[key] = value
		}
		action.Metadata = metadata
	}
	return action
}

func ParseAction(value, source string) (AppAction, error) {
	value = strings.TrimSpace(value)
	words := strings.Fields(value)
	if len(words) == 0 {
		return AppAction{}, errors.New("app action is empty")
	}
	action := AppAction{Source: source, At: time.Now()}
	if strings.EqualFold(words[0], "app") && len(words) >= 2 {
		switch strings.ToLower(words[1]) {
		case "page":
			if len(words) != 3 {
				return AppAction{}, errors.New("usage: app page NAME")
			}
			action.Kind, action.Value = "app.page", words[2]
		case "title", "osc", "progress":
			if len(words) < 3 {
				return AppAction{}, fmt.Errorf("usage: app %s VALUE", strings.ToLower(words[1]))
			}
			action.Kind = "app." + strings.ToLower(words[1])
			action.Value = strings.Join(words[2:], " ")
		case "quit", "exit":
			action.Kind = "app.quit"
		case "open":
			action.Kind = "app.port.open"
		case "close":
			action.Kind = "app.port.close"
		default:
			return AppAction{}, fmt.Errorf("unknown app action %q", words[1])
		}
		return action, nil
	}
	action.Kind, action.Value = "command", value
	return action, nil
}

func ParseActionURI(value string) (AppAction, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !strings.EqualFold(parsed.Scheme, productidentity.ProtocolScheme) {
		return AppAction{}, fmt.Errorf("action URI must use %s://", productidentity.ProtocolScheme)
	}
	host := strings.ToLower(parsed.Host)
	path, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil {
		return AppAction{}, err
	}
	switch host {
	case "page":
		return AppAction{Kind: "app.page", Value: path, Source: "uri", At: time.Now()}, nil
	case "command":
		return AppAction{Kind: "command", Value: path, Source: "uri", At: time.Now()}, nil
	case "app":
		return ParseAction("app "+strings.ReplaceAll(path, "/", " "), "uri")
	case "title", "osc", "progress":
		return ParseAction("app "+host+" "+path, "uri")
	case "port":
		return ParseAction("app "+path, "uri")
	default:
		return AppAction{}, fmt.Errorf("unsupported %s URI host %q", productidentity.ProtocolScheme, parsed.Host)
	}
}

func ActionURI(action AppAction) (string, error) {
	switch action.Kind {
	case "app.page":
		if strings.TrimSpace(action.Value) == "" {
			return "", errors.New("app.page requires a page")
		}
		return productidentity.ProtocolScheme + "://page/" + url.PathEscape(action.Value), nil
	case "command":
		if strings.TrimSpace(action.Value) == "" {
			return "", errors.New("command requires a value")
		}
		return productidentity.ProtocolScheme + "://command/" + url.PathEscape(action.Value), nil
	case "app.title", "app.osc", "app.progress":
		value := strings.TrimPrefix(action.Kind, "app.")
		return productidentity.ProtocolScheme + "://" + value + "/" + url.PathEscape(action.Value), nil
	case "app.quit":
		return productidentity.ProtocolScheme + "://app/quit", nil
	case "app.port.open":
		return productidentity.ProtocolScheme + "://port/open", nil
	case "app.port.close":
		return productidentity.ProtocolScheme + "://port/close", nil
	default:
		return "", fmt.Errorf("unsupported action URI kind %q", action.Kind)
	}
}
