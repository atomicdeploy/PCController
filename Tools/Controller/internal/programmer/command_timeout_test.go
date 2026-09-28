package programmer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildAssignsOperationSpecificProgrammerDeadlines(t *testing.T) {
	base := Options{
		Method: MethodUrclock, Port: "COM18",
		Avrdude: "avrdude", AvrdudeConf: "avrdude.conf",
	}
	metadata := base
	metadata.Operation = OperationMetadata
	metadataCommand, err := Build(metadata)
	if err != nil {
		t.Fatal(err)
	}
	read := base
	read.Operation = OperationReadFlash
	read.OutputPath = "flash.hex"
	readCommand, err := Build(read)
	if err != nil {
		t.Fatal(err)
	}
	if metadataCommand.Stage != "metadata handshake" ||
		metadataCommand.Timeout != defaultProgrammerHandshakeTimeout {
		t.Fatalf("metadata policy=%#v", metadataCommand)
	}
	if readCommand.Stage != "flash read" ||
		readCommand.Timeout != defaultProgrammerTransferTimeout {
		t.Fatalf("read policy=%#v", readCommand)
	}
	if readCommand.Timeout <= metadataCommand.Timeout {
		t.Fatalf("transfer timeout %s must exceed handshake timeout %s", readCommand.Timeout, metadataCommand.Timeout)
	}

	metadata.ProgrammerTimeout = 73 * time.Second
	overridden, err := Build(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if overridden.Timeout != 73*time.Second {
		t.Fatalf("override timeout=%s", overridden.Timeout)
	}
}

func TestRunTimesOutHungProgrammerWithStageAndCommand(t *testing.T) {
	command := programmerHelperCommand("hang")
	command.Stage = "metadata handshake"
	command.Timeout = time.Second
	started := time.Now()
	err := Run(context.Background(), command, nil)
	elapsed := time.Since(started)
	var timeoutErr *CommandTimeoutError
	if !errors.As(err, &timeoutErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%T %v, want CommandTimeoutError wrapping DeadlineExceeded", err, err)
	}
	if timeoutErr.Stage != command.Stage || timeoutErr.Command != command.String() ||
		timeoutErr.Timeout != command.Timeout {
		t.Fatalf("timeout context=%#v", timeoutErr)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("hung child returned after %s", elapsed)
	}
	for _, text := range []string{"metadata handshake", "deadline 1s", command.Name} {
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("timeout omitted %q: %v", text, err)
		}
	}
}

func TestRunTimeoutTerminatesProcessTreeAndReleasesResource(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "listener.txt")
	command := programmerHelperCommand("spawn-listener", readyPath)
	command.Stage = "programmer handshake"
	command.Timeout = 3 * time.Second
	result := make(chan error, 1)
	go func() { result <- Run(context.Background(), command, nil) }()

	address := waitForHelperValue(t, readyPath, 2*time.Second)
	err := <-result
	var timeoutErr *CommandTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("err=%v, want timeout", err)
	}
	listener, listenErr := net.Listen("tcp", address)
	if listenErr != nil {
		t.Fatalf("timed-out child still owns %s: %v", address, listenErr)
	}
	_ = listener.Close()
}

func TestRunPreservesExplicitCallerCancellation(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "listener.txt")
	command := programmerHelperCommand("listen", readyPath)
	command.Stage = "flash read"
	command.Timeout = 10 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- Run(ctx, command, nil) }()
	waitForHelperValue(t, readyPath, time.Second)
	cancel()

	err := <-result
	var timeoutErr *CommandTimeoutError
	if !errors.Is(err, context.Canceled) || errors.As(err, &timeoutErr) {
		t.Fatalf("err=%T %v, want caller cancellation", err, err)
	}
	if !strings.Contains(err.Error(), command.Stage) || !strings.Contains(err.Error(), command.Name) {
		t.Fatalf("cancellation omitted stage/command: %v", err)
	}
}

func TestRunAllowsLongStreamingProgrammerOperation(t *testing.T) {
	command := programmerHelperCommand("stream", "700ms", "50ms")
	command.Stage = "flash read"
	command.Timeout = 3 * time.Second
	var output bytes.Buffer
	if err := Run(context.Background(), command, &output); err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(output.String(), "programmer progress"); count < 5 {
		t.Fatalf("streamed progress count=%d output=%q", count, output.String())
	}
}

func TestProgrammerCommandHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	arguments := os.Args[separator+1:]
	switch arguments[0] {
	case "hang":
		for {
			time.Sleep(time.Hour)
		}
	case "listen":
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := os.WriteFile(arguments[1], []byte(listener.Addr().String()), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "spawn-listener":
		child := exec.Command(
			os.Args[0], "-test.run=^TestProgrammerCommandHelper$", "--", "listen", arguments[1],
		)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "stream":
		duration, err := time.ParseDuration(arguments[1])
		if err != nil {
			os.Exit(2)
		}
		interval, err := time.ParseDuration(arguments[2])
		if err != nil {
			os.Exit(2)
		}
		deadline := time.Now().Add(duration)
		for sequence := 0; time.Now().Before(deadline); sequence++ {
			fmt.Println("programmer progress", sequence)
			time.Sleep(interval)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func programmerHelperCommand(arguments ...string) Command {
	return Command{
		Name: os.Args[0],
		Args: append(
			[]string{"-test.run=^TestProgrammerCommandHelper$", "--"},
			arguments...,
		),
	}
}

func waitForHelperValue(t *testing.T, path string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil && len(content) != 0 {
			return string(content)
		}
		time.Sleep(10 * time.Millisecond)
	}
	content, err := os.ReadFile(path)
	t.Fatalf("helper did not publish %s within %s: content=%q err=%v", path, timeout, content, err)
	return string(content)
}
