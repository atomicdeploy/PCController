// Package shell provides the controller's host-side command shell.
//
// The line-oriented command model, history, completion hooks, and VT-friendly
// interaction are inspired by Ardush and atomicdeploy/portable-shell. This is
// an original Go implementation and does not copy their parser or editor.
package shell

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

var ErrExit = errors.New("shell exit requested")

type Handler func(context.Context, []string) (string, error)

type Command struct {
	Name    string
	Aliases []string
	Usage   string
	Summary string
	// Group places the command in a task-oriented help section.
	Group string
	Run   Handler
}

// CommandDescriptor is the immutable, JSON-safe command contract exposed to
// IPC, network clients, and library consumers. Handlers are intentionally not
// included, so every surface continues to execute through Engine.Execute.
type CommandDescriptor struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Usage   string   `json:"usage"`
	Summary string   `json:"summary"`
	Group   string   `json:"group"`
}

type Engine struct {
	commands  map[string]*Command
	ordered   []*Command
	history   []string
	max       int
	historyMu sync.Mutex
}

func New(historyLimit int) *Engine {
	if historyLimit < 1 {
		historyLimit = 1
	}
	return &Engine{commands: make(map[string]*Command), max: historyLimit}
}

func (engine *Engine) Register(command Command) error {
	command.Name = strings.ToLower(strings.TrimSpace(command.Name))
	if command.Name == "" || command.Run == nil {
		return errors.New("command requires a name and handler")
	}
	keys := append([]string{command.Name}, command.Aliases...)
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return errors.New("command alias cannot be empty")
		}
		if _, exists := engine.commands[key]; exists {
			return fmt.Errorf("command name or alias %q is already registered", key)
		}
	}
	stored := command
	engine.ordered = append(engine.ordered, &stored)
	for _, key := range keys {
		engine.commands[strings.ToLower(strings.TrimSpace(key))] = &stored
	}
	return nil
}

func (engine *Engine) Execute(ctx context.Context, line string) (string, error) {
	words, err := Split(line)
	if err != nil {
		return "", err
	}
	if len(words) == 0 {
		return "", nil
	}
	engine.addHistory(strings.TrimSpace(line))

	name := strings.ToLower(words[0])
	if name == "help" || name == "?" {
		if len(words) == 1 {
			return engine.Help(), nil
		}
		command := engine.commands[strings.ToLower(words[1])]
		if command == nil {
			return "", fmt.Errorf("unknown command %q", words[1])
		}
		return command.Usage + " — " + command.Summary, nil
	}
	command := engine.commands[name]
	if command == nil {
		return "", fmt.Errorf("unknown command %q; type help", words[0])
	}
	return command.Run(ctx, words[1:])
}

func (engine *Engine) Help() string {
	return engine.renderHelp(false)
}

// HelpANSI returns the same command reference with selective VT-100 styling.
func (engine *Engine) HelpANSI() string {
	return engine.renderHelp(true)
}

// Catalog returns a detached command catalog in registration order.
func (engine *Engine) Catalog() []CommandDescriptor {
	result := make([]CommandDescriptor, 0, len(engine.ordered))
	for _, command := range engine.ordered {
		result = append(result, CommandDescriptor{
			Name: command.Name, Aliases: append([]string(nil), command.Aliases...),
			Usage: command.Usage, Summary: command.Summary,
			Group: commandHelpGroup(command),
		})
	}
	return result
}

func (engine *Engine) renderHelp(color bool) string {
	var builder strings.Builder
	writeStyled(&builder, color, "1;36", "Commands:")
	builder.WriteByte('\n')
	groups := []string{
		"Connection and telemetry",
		"Board configuration and menus",
		"Outputs and front panel",
		"RF, macros and automation",
		"PC and operating system",
		"Protocol and diagnostics",
		"Firmware and bootloader",
		"Console",
		"Other",
	}
	for _, group := range groups {
		wroteHeading := false
		for _, command := range engine.ordered {
			if commandHelpGroup(command) != group {
				continue
			}
			if !wroteHeading {
				if builder.Len() != 0 && builder.String()[builder.Len()-1] != '\n' {
					builder.WriteByte('\n')
				}
				writeStyled(&builder, color, "1;33", group+":")
				builder.WriteByte('\n')
				wroteHeading = true
			}
			builder.WriteString("  ")
			writeStyled(&builder, color, "1;32", command.Usage)
			padding := 32 - len(command.Usage)
			if padding < 2 {
				padding = 2
			}
			builder.WriteString(strings.Repeat(" ", padding))
			writeStyled(&builder, color, "2", command.Summary)
			builder.WriteByte('\n')
		}
		if group == "Console" {
			if !wroteHeading {
				writeStyled(&builder, color, "1;33", group+":")
				builder.WriteByte('\n')
			}
			builder.WriteString("  ")
			writeStyled(&builder, color, "1;32", "help [command]")
			builder.WriteString(strings.Repeat(" ", 18))
			writeStyled(&builder, color, "2", "show command help")
			builder.WriteByte('\n')
		}
	}
	return strings.TrimRight(builder.String(), "\n")
}

func writeStyled(builder *strings.Builder, color bool, code, value string) {
	if color {
		builder.WriteString("\x1b[")
		builder.WriteString(code)
		builder.WriteByte('m')
	}
	builder.WriteString(value)
	if color {
		builder.WriteString("\x1b[0m")
	}
}

func commandHelpGroup(command *Command) string {
	if strings.TrimSpace(command.Group) != "" {
		return strings.TrimSpace(command.Group)
	}
	switch command.Name {
	case "ports", "open", "close", "reconnect", "hello", "status", "event", "temp", "stream":
		return "Connection and telemetry"
	case "settings", "menu", "display", "host-menu":
		return "Board configuration and menus"
	case "relay", "pwm", "rgb", "strip", "buzzer", "melody", "silent", "i2c":
		return "Outputs and front panel"
	case "rf", "macro", "automation":
		return "RF, macros and automation"
	case "os", "state", "program-state", "keyboard", "bridge":
		return "PC and operating system"
	case "reset", "query", "write":
		return "Protocol and diagnostics"
	case "toolchain", "boot", "program":
		return "Firmware and bootloader"
	case "clear", "quit":
		return "Console"
	default:
		return "Other"
	}
}

func (engine *Engine) Complete(prefix string) []string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	seen := make(map[string]bool)
	var matches []string
	for key, command := range engine.commands {
		if strings.HasPrefix(key, prefix) && !seen[command.Name] {
			seen[command.Name] = true
			matches = append(matches, command.Name)
		}
	}
	if strings.HasPrefix("help", prefix) {
		matches = append(matches, "help")
	}
	sort.Strings(matches)
	return matches
}

func (engine *Engine) History() []string {
	engine.historyMu.Lock()
	defer engine.historyMu.Unlock()
	return append([]string(nil), engine.history...)
}

func (engine *Engine) addHistory(line string) {
	engine.historyMu.Lock()
	defer engine.historyMu.Unlock()
	if line == "" {
		return
	}
	if len(engine.history) != 0 && engine.history[len(engine.history)-1] == line {
		return
	}
	engine.history = append(engine.history, line)
	if len(engine.history) > engine.max {
		engine.history = append([]string(nil), engine.history[len(engine.history)-engine.max:]...)
	}
}

func Split(line string) ([]string, error) {
	var result []string
	var word strings.Builder
	var quote rune
	escaped := false
	started := false

	flush := func() {
		if started {
			result = append(result, word.String())
			word.Reset()
			started = false
		}
	}

	for _, character := range line {
		if escaped {
			word.WriteRune(character)
			started = true
			escaped = false
			continue
		}
		if character == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				word.WriteRune(character)
			}
			started = true
			continue
		}
		switch {
		case character == '\'' || character == '"':
			quote = character
			started = true
		case unicode.IsSpace(character):
			flush()
		default:
			word.WriteRune(character)
			started = true
		}
	}
	if escaped {
		return nil, errors.New("trailing escape in command")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in command")
	}
	flush()
	return result, nil
}

// Join serializes already-tokenized command arguments so a subsequent Split
// recovers the exact same values. It is used when a secondary CLI process
// forwards a command to the serial-owning primary process over IPC.
func Join(words []string) string {
	quoted := make([]string, len(words))
	for index, word := range words {
		quoted[index] = quote(word)
	}
	return strings.Join(quoted, " ")
}

func quote(word string) string {
	if word != "" && !strings.ContainsAny(word, " \t\r\n'\"\\") {
		return word
	}
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range word {
		if character == '"' || character == '\\' {
			builder.WriteByte('\\')
		}
		builder.WriteRune(character)
	}
	builder.WriteByte('"')
	return builder.String()
}
