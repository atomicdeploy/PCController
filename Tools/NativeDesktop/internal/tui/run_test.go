package tui

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunRejectsRedirectedStreamsWithoutTerminalEscapes(t *testing.T) {
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		t.Fatal(err)
	}
	previousInput := os.Stdin
	previousOutput := os.Stdout
	os.Stdin = inputReader
	os.Stdout = outputWriter
	t.Cleanup(func() {
		os.Stdin = previousInput
		os.Stdout = previousOutput
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
	})

	runErr := Run(context.Background(), nil, LaunchOptions{})
	if runErr == nil ||
		!strings.Contains(runErr.Error(), "interactive terminal") {
		t.Fatalf("Run() error = %v, want interactive-terminal guidance", runErr)
	}

	if err := outputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(outputReader)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 0 {
		t.Fatalf("redirected TUI emitted output/control bytes: %q", output)
	}
}
