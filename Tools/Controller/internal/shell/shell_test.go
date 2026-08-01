package shell

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestSplitQuotesAndEscapes(t *testing.T) {
	got, err := Split(`send "hello world" one\ two 'three four'`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"send", "hello world", "one two", "three four"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestJoinRoundTripsSplit(t *testing.T) {
	words := []string{
		"program",
		"write-flash",
		`C:\build output\firmware "final".hex`,
		"",
		"single'quote",
	}
	line := Join(words)
	got, err := Split(line)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, words) {
		t.Fatalf("Split(Join()) = %#v, want %#v (line %q)", got, words, line)
	}
}

func TestEngineHistoryCompletionAndExecution(t *testing.T) {
	engine := New(2)
	if err := engine.Register(Command{
		Name: "status", Aliases: []string{"st"}, Usage: "status",
		Summary: "read status",
		Run: func(context.Context, []string) (string, error) {
			return "ok", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	output, err := engine.Execute(context.Background(), "st")
	if err != nil || output != "ok" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	_, _ = engine.Execute(context.Background(), "status")
	_, _ = engine.Execute(context.Background(), "st")
	if got := engine.History(); !reflect.DeepEqual(got, []string{"status", "st"}) {
		t.Fatalf("history %#v", got)
	}
	if got := engine.Complete("sta"); !reflect.DeepEqual(got, []string{"status"}) {
		t.Fatalf("completion %#v", got)
	}
}

func TestHelpIsTaskGroupedAndANSIHasPlainFallback(t *testing.T) {
	engine := New(4)
	register := func(name, usage string) {
		t.Helper()
		if err := engine.Register(Command{
			Name: name, Usage: usage, Summary: name + " summary",
			Run: func(context.Context, []string) (string, error) { return "", nil },
		}); err != nil {
			t.Fatal(err)
		}
	}
	register("status", "status")
	register("relay", "relay N on|off")
	register("open", "open [PORT]")

	plain := engine.Help()
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain help contains ANSI: %q", plain)
	}
	connection := strings.Index(plain, "Connection and telemetry:")
	status := strings.Index(plain, "status")
	open := strings.Index(plain, "open [PORT]")
	outputs := strings.Index(plain, "Outputs and front panel:")
	relay := strings.Index(plain, "relay N on|off")
	if connection < 0 || status < connection || open < status || outputs < open || relay < outputs {
		t.Fatalf("help is not task grouped in registration order:\n%s", plain)
	}

	styled := engine.HelpANSI()
	if !strings.Contains(styled, "\x1b[1;36mCommands:") ||
		!strings.Contains(styled, "\x1b[1;32mstatus") ||
		!strings.Contains(styled, "\x1b[2mstatus summary") {
		t.Fatalf("selective ANSI styling missing: %q", styled)
	}
	stripANSI := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if got := stripANSI.ReplaceAllString(styled, ""); got != plain {
		t.Fatalf("ANSI help changed content\ngot:\n%s\nwant:\n%s", got, plain)
	}
}
