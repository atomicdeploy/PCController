package script

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeExecutor struct {
	commands []string
	failOn   string
}

func (executor *fakeExecutor) Execute(_ context.Context, command string) (string, error) {
	executor.commands = append(executor.commands, command)
	if command == executor.failOn {
		return "", errors.New("requested failure")
	}
	return "ok", nil
}

func TestRunVariablesRepeatAndComments(t *testing.T) {
	executor := &fakeExecutor{}
	var results []Result
	err := Run(context.Background(), strings.NewReader(`
# setup
set CHANNEL 7
repeat 2 pwm set ${CHANNEL} 1024
sleep 0s
status
`), executor, Options{OnResult: func(result Result) {
		results = append(results, result)
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pwm set 7 1024", "pwm set 7 1024", "status"}
	if strings.Join(executor.commands, "|") != strings.Join(want, "|") {
		t.Fatalf("commands = %#v, want %#v", executor.commands, want)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
}

func TestRunStopsOnError(t *testing.T) {
	executor := &fakeExecutor{failOn: "bad"}
	err := Run(context.Background(), strings.NewReader("good\nbad\nafter\n"), executor, Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(executor.commands) != 2 {
		t.Fatalf("commands = %#v", executor.commands)
	}
}
