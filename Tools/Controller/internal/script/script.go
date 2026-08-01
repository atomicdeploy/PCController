// Package script runs deterministic, line-oriented controller command files.
package script

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type Executor interface {
	Execute(context.Context, string) (string, error)
}

type Result struct {
	Line    int    `json:"line"`
	Command string `json:"command"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Options struct {
	ContinueOnError bool
	MaxLineBytes    int
	OnResult        func(Result)
}

func Run(ctx context.Context, input io.Reader, executor Executor, options Options) error {
	maxLine := options.MaxLineBytes
	if maxLine <= 0 {
		maxLine = 64 * 1024
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 1024), maxLine)
	variables := make(map[string]string)
	var firstError error
	lineNumber := 0
	var continued strings.Builder
	startLine := 0

	emit := func(result Result) {
		if options.OnResult != nil {
			options.OnResult(result)
		}
	}
	execute := func(number int, command string) error {
		output, err := executor.Execute(ctx, command)
		result := Result{Line: number, Command: command, Output: output}
		if err != nil {
			result.Error = err.Error()
		}
		emit(result)
		return err
	}

	for scanner.Scan() {
		lineNumber++
		text := strings.TrimSpace(scanner.Text())
		if continued.Len() != 0 {
			continued.WriteString(text)
			text = continued.String()
		}
		if strings.HasSuffix(text, "\\") {
			if continued.Len() == 0 {
				startLine = lineNumber
			}
			continued.Reset()
			continued.WriteString(strings.TrimSuffix(text, "\\"))
			continued.WriteByte(' ')
			continue
		}
		if startLine != 0 {
			lineNumberForCommand := startLine
			startLine = 0
			continued.Reset()
			if err := runLine(ctx, lineNumberForCommand, text, variables, execute); err != nil {
				if firstError == nil {
					firstError = err
				}
				if !options.ContinueOnError {
					return err
				}
			}
			continue
		}
		if err := runLine(ctx, lineNumber, text, variables, execute); err != nil {
			if firstError == nil {
				firstError = err
			}
			if !options.ContinueOnError {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if continued.Len() != 0 {
		return fmt.Errorf("line %d: trailing continuation", startLine)
	}
	return firstError
}

func runLine(
	ctx context.Context,
	lineNumber int,
	text string,
	variables map[string]string,
	execute func(int, string) error,
) error {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "#") || strings.HasPrefix(text, ";") {
		return nil
	}
	expanded, err := expand(text, variables)
	if err != nil {
		return fmt.Errorf("line %d: %w", lineNumber, err)
	}
	fields := strings.Fields(expanded)
	if len(fields) == 0 {
		return nil
	}
	switch strings.ToLower(fields[0]) {
	case "set":
		if len(fields) < 3 {
			return fmt.Errorf("line %d: usage: set NAME VALUE", lineNumber)
		}
		name := fields[1]
		if !validName(name) {
			return fmt.Errorf("line %d: invalid variable name %q", lineNumber, name)
		}
		variables[name] = strings.Join(fields[2:], " ")
		return nil
	case "unset":
		if len(fields) != 2 {
			return fmt.Errorf("line %d: usage: unset NAME", lineNumber)
		}
		delete(variables, fields[1])
		return nil
	case "sleep":
		if len(fields) != 2 {
			return fmt.Errorf("line %d: usage: sleep DURATION", lineNumber)
		}
		duration, parseErr := time.ParseDuration(fields[1])
		if parseErr != nil || duration < 0 {
			return fmt.Errorf("line %d: invalid sleep duration %q", lineNumber, fields[1])
		}
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	case "repeat":
		if len(fields) < 3 {
			return fmt.Errorf("line %d: usage: repeat COUNT COMMAND...", lineNumber)
		}
		count, parseErr := strconv.Atoi(fields[1])
		if parseErr != nil || count < 1 || count > 10000 {
			return fmt.Errorf("line %d: repeat count must be 1..10000", lineNumber)
		}
		commandIndex := strings.Index(expanded, fields[2])
		command := expanded[commandIndex:]
		for index := 0; index < count; index++ {
			if err := execute(lineNumber, command); err != nil {
				return fmt.Errorf("line %d repeat %d: %w", lineNumber, index+1, err)
			}
		}
		return nil
	default:
		if err := execute(lineNumber, expanded); err != nil {
			return fmt.Errorf("line %d: %w", lineNumber, err)
		}
		return nil
	}
}

func expand(text string, variables map[string]string) (string, error) {
	var result strings.Builder
	for index := 0; index < len(text); {
		if index+2 <= len(text) && text[index:index+2] == "${" {
			end := strings.IndexByte(text[index+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated variable reference")
			}
			name := text[index+2 : index+2+end]
			value, ok := variables[name]
			if !ok {
				return "", fmt.Errorf("variable %q is not set", name)
			}
			result.WriteString(value)
			index += end + 3
			continue
		}
		result.WriteByte(text[index])
		index++
	}
	return result.String(), nil
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			character == '_' ||
			(index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return true
}
