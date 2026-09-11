package programmer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/controller/internal/link"
)

type fakeReadbackResetSession struct {
	pulse func(context.Context, time.Duration) error
	close func() error
}

func (session *fakeReadbackResetSession) PulseDTR(ctx context.Context, duration time.Duration) error {
	return session.pulse(ctx, duration)
}

func (session *fakeReadbackResetSession) Close() error { return session.close() }

func reentryFixtureOptions(t *testing.T, operation Operation) (Options, []byte) {
	t.Helper()
	image := &IntelHexImage{data: map[uint32]byte{0: 0x12, 7: 0xA5}}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "input.hex")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return Options{
		Method: MethodUrclock, Operation: operation, Port: "OFFLINE",
		HexPath: path, Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
		ConfirmEEPROMWrite: operation == OperationWriteEEPROM,
	}, content
}

func TestReadbackReentryFollowsCompletedWriteBeforeIndependentRead(t *testing.T) {
	for _, operation := range []Operation{OperationWriteFlash, OperationWriteEEPROM} {
		t.Run(string(operation), func(t *testing.T) {
			options, content := reentryFixtureOptions(t, operation)
			fixture := &readbackFixtureRunner{content: content}
			var trace []string
			writing := false
			runner := &productionCommandRunner{
				CommandRunner: CommandRunnerFunc(func(ctx context.Context, command Command, output io.Writer) error {
					if len(fixture.commands) == 0 {
						writing = true
						defer func() { writing = false; trace = append(trace, "write exited") }()
					} else {
						trace = append(trace, "read")
					}
					return fixture.Run(ctx, command, output)
				}),
				openReset: func(ctx context.Context, port string, baud int) (readbackResetSession, error) {
					if writing || port != "OFFLINE" || baud != generatedBoardBaud {
						t.Fatalf("reset ownership/identity: writing=%t port=%q baud=%d", writing, port, baud)
					}
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > readbackResetTimeout {
						t.Fatal("reset stage has no bounded deadline")
					}
					trace = append(trace, "open")
					return &fakeReadbackResetSession{
						pulse: func(_ context.Context, pulse time.Duration) error {
							if pulse != 120*time.Millisecond {
								t.Fatalf("pulse=%s", pulse)
							}
							trace = append(trace, "DTR pulse")
							return nil
						},
						close: func() error { trace = append(trace, "close"); return nil },
					}, nil
				},
			}
			if err := ExecuteWithRunner(context.Background(), options, io.Discard, runner); err != nil {
				t.Fatal(err)
			}
			if want := []string{"write exited", "open", "DTR pulse", "close", "read"}; !reflect.DeepEqual(trace, want) {
				t.Fatalf("trace=%v want=%v", trace, want)
			}
			if len(fixture.commands) != 2 {
				t.Fatalf("unexpected command count %d", len(fixture.commands))
			}
		})
	}
}

func TestReadbackReentryDoesNotResetWhileWriterRuns(t *testing.T) {
	options, content := reentryFixtureOptions(t, OperationWriteFlash)
	fixture := &readbackFixtureRunner{content: content}
	started, release := make(chan struct{}), make(chan struct{})
	var opened atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runner := &productionCommandRunner{
		CommandRunner: CommandRunnerFunc(func(ctx context.Context, command Command, output io.Writer) error {
			if len(fixture.commands) == 0 {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return fixture.Run(ctx, command, output)
		}),
		openReset: func(context.Context, string, int) (readbackResetSession, error) {
			opened.Add(1)
			return &fakeReadbackResetSession{
				pulse: func(context.Context, time.Duration) error { return nil },
				close: func() error { return nil },
			}, nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- ExecuteWithRunner(ctx, options, io.Discard, runner) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("writer did not start")
	}
	if opened.Load() != 0 {
		t.Fatal("reset ran before writer exited")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil || opened.Load() != 1 {
			t.Fatalf("completed err=%v resets=%d", err, opened.Load())
		}
	case <-ctx.Done():
		t.Fatal("readback did not finish")
	}
}

func TestReadbackReentryFailuresNeverRepeatWriteOrAcceptUnverifiedBytes(t *testing.T) {
	for _, stage := range []string{"write", "open", "pulse", "close", "read", "mismatch"} {
		t.Run(stage, func(t *testing.T) {
			options, content := reentryFixtureOptions(t, OperationWriteFlash)
			if stage == "mismatch" {
				wrong := &IntelHexImage{data: map[uint32]byte{0: 0x13, 7: 0xA5}}
				content, _ = wrong.Canonical()
			}
			fixture := &readbackFixtureRunner{content: content}
			sentinel := errors.New("injected " + stage)
			commands, opens, closes := 0, 0, 0
			runner := &productionCommandRunner{
				CommandRunner: CommandRunnerFunc(func(ctx context.Context, command Command, output io.Writer) error {
					commands++
					if (stage == "write" && commands == 1) || (stage == "read" && commands == 2) {
						return sentinel
					}
					return fixture.Run(ctx, command, output)
				}),
				openReset: func(context.Context, string, int) (readbackResetSession, error) {
					opens++
					if stage == "open" {
						return nil, sentinel
					}
					return &fakeReadbackResetSession{
						pulse: func(context.Context, time.Duration) error {
							if stage == "pulse" {
								return sentinel
							}
							return nil
						},
						close: func() error {
							closes++
							if stage == "close" {
								return sentinel
							}
							return nil
						},
					}, nil
				},
			}
			err := ExecuteWithRunner(context.Background(), options, io.Discard, runner)
			if stage == "mismatch" {
				if err == nil || !strings.Contains(err.Error(), "readback mismatch") {
					t.Fatalf("mismatch accepted: %v", err)
				}
			} else if !errors.Is(err, sentinel) {
				t.Fatalf("lost stage error: %v", err)
			}
			wantCommands := 1
			if stage == "read" || stage == "mismatch" {
				wantCommands = 2
			}
			if commands != wantCommands {
				t.Fatalf("commands=%d want=%d", commands, wantCommands)
			}
			if stage == "write" && opens != 0 {
				t.Fatal("reset followed failed write")
			}
			if stage != "write" && stage != "open" && closes != 1 {
				t.Fatalf("reset handle closes=%d", closes)
			}
		})
	}
}

func TestReadbackRecoveryReentersUARTWithoutWriting(t *testing.T) {
	options, content := reentryFixtureOptions(t, OperationWriteFlash)
	fixture := &readbackFixtureRunner{content: content}
	closed := false
	runner := &productionCommandRunner{
		CommandRunner: CommandRunnerFunc(func(ctx context.Context, command Command, output io.Writer) error {
			if !closed || !strings.Contains(strings.Join(command.Args, " "), "-Uflash:r:") {
				t.Fatalf("recovery violated closed-handle/read-only boundary: %+v", command)
			}
			return fixture.Run(ctx, command, output)
		}),
		openReset: func(context.Context, string, int) (readbackResetSession, error) {
			return &fakeReadbackResetSession{
				pulse: func(context.Context, time.Duration) error { return nil },
				close: func() error { closed = true; return nil },
			}, nil
		},
	}
	if err := VerifyFlashReadbackWithRunner(context.Background(), options, io.Discard, runner); err != nil {
		t.Fatal(err)
	}
	if len(fixture.commands) != 1 {
		t.Fatalf("recovery commands=%d", len(fixture.commands))
	}
}

func TestReadbackReentryScopeAndCancellation(t *testing.T) {
	runner := NewCommandRunner().(*productionCommandRunner)
	opens, closes := 0, 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.openReset = func(_ context.Context, _ string, baud int) (readbackResetSession, error) {
		opens++
		if baud != 57600 {
			t.Fatalf("explicit baud lost: %d", baud)
		}
		return &fakeReadbackResetSession{
			pulse: func(pulseContext context.Context, _ time.Duration) error { cancel(); return pulseContext.Err() },
			close: func() error { closes++; return nil },
		}, nil
	}
	for _, options := range []Options{{Method: MethodUSBasp}, {Method: MethodCompile}, {Method: MethodAvrdude, Programmer: "usbasp"}} {
		if err := runner.PrepareReadback(ctx, options, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.PrepareReadback(ctx, Options{Method: MethodUrclock, Port: "tcp://127.0.0.1:9000"}, nil); !errors.Is(err, link.ErrControlLinesUnsupported) {
		t.Fatalf("network control-line request accepted: %v", err)
	}
	if opens != 0 {
		t.Fatal("nonserial request opened hardware")
	}
	err := runner.PrepareReadback(ctx, Options{Method: MethodAvrdude, Programmer: "urclock", Port: "OFFLINE", BaudRate: 57600}, nil)
	if !errors.Is(err, context.Canceled) || opens != 1 || closes != 1 {
		t.Fatalf("cancel cleanup: err=%v opens=%d closes=%d", err, opens, closes)
	}
	err = runner.PrepareReadback(ctx, Options{Method: MethodUrclock, Port: "OFFLINE"}, nil)
	if !errors.Is(err, context.Canceled) || opens != 1 {
		t.Fatalf("expired request opened port: %v opens=%d", err, opens)
	}
}
