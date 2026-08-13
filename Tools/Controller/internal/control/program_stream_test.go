package control

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/programmer"
)

func TestProgramEventWriterNormalizesAndOrdersRemoteOutput(t *testing.T) {
	runtime := New(Options{})
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	writer := newProgramEventWriter(runtime, "build-123", programmer.Options{
		Method: programmer.MethodCompile, Operation: programmer.OperationWriteFlash,
		CompileSourceRoot: home + `\Desktop\PCController`,
		BuildPath:         home + `\AppData\Local\PCController\build`,
	})
	_, _ = writer.Write([]byte("Using board '328' from platform\n\x1b[31m" + home + `\Desktop\PCController\Project\main.cpp: error` + "\x1b[0m\npartial"))
	_, _ = writer.Write([]byte(" line\n"))
	writer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	first, err := runtime.WaitEvent(ctx, 0, "program.output")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.Text, home) || strings.Contains(first.Text, "\x1b") {
		t.Fatalf("machine-local/control data leaked: %q", first.Text)
	}
	if !strings.Contains(first.Text, `<firmware-source>\Project\main.cpp: error`) {
		t.Fatalf("normalized error = %q", first.Text)
	}
	if first.Metadata["operation_id"] != "build-123" || first.Metadata["sequence"] != "1" {
		t.Fatalf("first metadata = %#v", first.Metadata)
	}
	second, err := runtime.WaitEvent(ctx, first.ID, "program.output")
	if err != nil {
		t.Fatal(err)
	}
	if second.Text != "partial line" || second.Metadata["sequence"] != "2" {
		t.Fatalf("second event = %#v", second)
	}
}

func TestNormalizeProgramOutputMatchesRemotePresentationWithoutHidingErrors(t *testing.T) {
	root := `C:\work\PCController`
	output := "Using core 'MiniCore'\r\n" +
		"\x1b[33m" + root + `\Project\main.cpp:42: error: broken` + "\x1b[0m\r\n"
	got := NormalizeProgramOutput(output, root)
	if got != `<project>\Project\main.cpp:42: error: broken` {
		t.Fatalf("normalized output = %q", got)
	}
}
