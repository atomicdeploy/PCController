package hostui

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

type AppAction struct {
	Kind   string    `json:"kind"`
	Value  string    `json:"value,omitempty"`
	Source string    `json:"source,omitempty"`
	At     time.Time `json:"at"`
}

type ActionBroker struct {
	events chan AppAction
	mu     sync.RWMutex
	watch  func(AppAction)
}

func NewActionBroker() *ActionBroker {
	return &ActionBroker{events: make(chan AppAction, 64)}
}

func (broker *ActionBroker) Events() <-chan AppAction { return broker.events }

// SetObserver installs a single delivery observer without changing the
// broker's original bounded TUI queue. The observer is used by the primary
// process to mirror valid app.page actions into the typed runtime event
// history, whose subscribers are independently cursor-based. Observer
// delivery remains independent when a headless process does not drain the
// optional TUI queue.
func (broker *ActionBroker) SetObserver(observer func(AppAction)) {
	broker.mu.Lock()
	broker.watch = observer
	broker.mu.Unlock()
}

func (broker *ActionBroker) Publish(action AppAction) error {
	action.Kind = strings.ToLower(strings.TrimSpace(action.Kind))
	action.Value = strings.TrimSpace(action.Value)
	if action.At.IsZero() {
		action.At = time.Now()
	}
	switch action.Kind {
	case "app.page":
		if action.Value == "" {
			return errors.New("app.page requires a page name")
		}
	case "app.quit", "app.port.open", "app.port.close":
		if action.Value != "" {
			return fmt.Errorf("%s does not accept a value", action.Kind)
		}
	case "command":
		if action.Value == "" {
			return errors.New("command action requires a command")
		}
	default:
		return fmt.Errorf("unsupported app action %q", action.Kind)
	}
	broker.mu.RLock()
	observer := broker.watch
	broker.mu.RUnlock()
	select {
	case broker.events <- action:
		if observer != nil {
			observer(action)
		}
		return nil
	default:
		if observer != nil {
			observer(action)
		}
		return errors.New("app action queue is full")
	}
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
	if err != nil || !strings.EqualFold(parsed.Scheme, "pccontroller") {
		return AppAction{}, errors.New("action URI must use pccontroller://")
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
	case "port":
		return ParseAction("app "+path, "uri")
	default:
		return AppAction{}, fmt.Errorf("unsupported pccontroller URI host %q", parsed.Host)
	}
}

func ActionURI(action AppAction) (string, error) {
	switch action.Kind {
	case "app.page":
		if strings.TrimSpace(action.Value) == "" {
			return "", errors.New("app.page requires a page")
		}
		return "pccontroller://page/" + url.PathEscape(action.Value), nil
	case "command":
		if strings.TrimSpace(action.Value) == "" {
			return "", errors.New("command requires a value")
		}
		return "pccontroller://command/" + url.PathEscape(action.Value), nil
	case "app.quit":
		return "pccontroller://app/quit", nil
	case "app.port.open":
		return "pccontroller://port/open", nil
	case "app.port.close":
		return "pccontroller://port/close", nil
	default:
		return "", fmt.Errorf("unsupported action URI kind %q", action.Kind)
	}
}
