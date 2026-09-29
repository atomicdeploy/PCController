package programmer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestProgressStreamsPipeHashesBeforeNewline(t *testing.T) {
	var observed []Progress
	var output bytes.Buffer
	ctx := WithProgress(context.Background(), func(value Progress) { observed = append(observed, value) })
	writer := &progressOutput{ctx: ctx, output: &output, stage: "flash read"}
	for _, fragment := range []string{"Read", "ing | ", "#", "##"} {
		if _, err := writer.Write([]byte(fragment)); err != nil {
			t.Fatal(err)
		}
	}
	if len(observed) != 3 || observed[2].Percent != 6 || observed[2].Stage != "flash read:reading" {
		t.Fatalf("incremental observations = %#v", observed)
	}
	if output.String() != "Reading | ###" {
		t.Fatalf("output was altered: %q", output.String())
	}
	_, _ = writer.Write([]byte(" | 6% 0.10s\nReading | ##- | 4% 0.2s\n"))
	if observed[len(observed)-1].Percent != 4 {
		t.Fatalf("failure bar fabricated completion: %#v", observed)
	}
}

func TestProgressCRLFExplicitAndBoundedRecords(t *testing.T) {
	var observed []Progress
	ctx := WithProgress(context.Background(), func(value Progress) { observed = append(observed, value) })
	writer := &progressOutput{ctx: ctx, output: io.Discard, stage: "flash write"}
	for _, fragment := range []string{"\rWriting | #   | 1%", " 0.1s\rWriting | ## | 3", "% 0.2s\r\n"} {
		_, _ = writer.Write([]byte(fragment))
	}
	if len(observed) != 2 || observed[0].Percent != 1 || observed[1].Percent != 3 {
		t.Fatalf("TTY reports rounded or fabricated: %#v", observed)
	}
	_, _ = writer.Write([]byte(strings.Repeat("x", progressRecordLimit*8)))
	if len(writer.record) > progressRecordLimit || !writer.discard {
		t.Fatal("unbounded record")
	}
	_, _ = writer.Write([]byte("Reading | ##\nnot progress: 100%\nWriting | invalid | 999%\nReading | #"))
	if len(observed) != 3 || observed[2].Percent != 2 {
		t.Fatalf("ignored noise: %#v", observed)
	}
}

func TestProgressUnknownStagesAndContextSurviveCleanup(t *testing.T) {
	var observed []Progress
	ctx, cancel := context.WithCancel(WithProgress(context.Background(), func(value Progress) { observed = append(observed, value) }))
	cancel()
	ReportProgress(context.WithoutCancel(ctx), Progress{Stage: "reconnect", Percent: -1})
	ReportProgress(ctx, Progress{Stage: "settings", Percent: 101})
	if len(observed) != 2 || observed[0].Percent != -1 || observed[1].Percent != -1 {
		t.Fatal(observed)
	}
}

func TestRunStreamsProgressBeforeExitAndCancels(t *testing.T) {
	observed := make(chan Progress, 8)
	ctx, cancel := context.WithCancel(WithProgress(context.Background(), func(value Progress) { observed <- value }))
	defer cancel()
	command := programmerHelperCommand("progress-hang")
	command.Stage = "flash read"
	command.Timeout = 5 * time.Second
	result := make(chan error, 1)
	go func() { result <- Run(ctx, command, io.Discard) }()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case value := <-observed:
			if value.Percent != 6 {
				continue
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("child did not terminate")
			}
			return
		case err := <-result:
			t.Fatalf("process exited before progress: %v", err)
		case <-timer.C:
			t.Fatal("no streaming progress before newline/exit")
		}
	}
}
