package portowner

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOwnerHelperInvocationProducesOneStrictBoundedJSONValue(t *testing.T) {
	owner := Owner{
		PID: 41, Name: strings.Repeat("terminal", 100),
		Executable: `C:\Apps\terminal.exe`,
		Window:     Window{Title: "Serial\x1b[31m console", Class: "TerminalWindow", Visible: true},
	}
	var output bytes.Buffer
	err := runHelperInvocationWith(
		context.Background(),
		[]string{ownerHelperArgument, `\\.\com7`},
		&output,
		func(_ context.Context, port string) (Owner, bool, error) {
			if port != "COM7" {
				t.Fatalf("helper port=%q", port)
			}
			return owner, true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if output.Len() > maxOwnerHelperOutput || bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("helper output bytes=%d value=%q", output.Len(), output.String())
	}
	decoded, found, err := decodeOwnerHelperResult("COM7", output.Bytes())
	if err != nil || !found || decoded.PID != 41 || len(decoded.Name) > 512 {
		t.Fatalf("owner=%+v found=%t err=%v", decoded, found, err)
	}
	if strings.ContainsRune(decoded.Window.Title, '\x1b') {
		t.Fatalf("terminal control character survived helper boundary: %q", decoded.Window.Title)
	}
}

func TestOwnerHelperInvocationRejectsAnythingButExactCOMSelector(t *testing.T) {
	for _, args := range [][]string{
		{ownerHelperArgument},
		{ownerHelperArgument, "COM7", "extra"},
		{ownerHelperArgument, "friendly device"},
		{ownerHelperArgument, "COM1234567890123456"},
		{"owner-scan", "COM7"},
	} {
		err := runHelperInvocationWith(
			context.Background(),
			args,
			&bytes.Buffer{},
			func(context.Context, string) (Owner, bool, error) {
				t.Fatal("invalid invocation reached native scan")
				return Owner{}, false, nil
			},
		)
		if err == nil {
			t.Fatalf("invalid helper args accepted: %q", args)
		}
	}
}

func TestOwnerHelperDecoderRejectsUntrustedOrAmbiguousOutput(t *testing.T) {
	for _, encoded := range [][]byte{
		nil,
		[]byte(`{"version":1,"port":"COM7","found":false,"extra":true}`),
		[]byte("{\"version\":1,\"port\":\"COM7\",\"found\":false}\n{}\n"),
		[]byte(`{"version":1,"port":"COM8","found":false}`),
		[]byte(`{"version":1,"port":"COM7","found":true}`),
		bytes.Repeat([]byte("x"), maxOwnerHelperOutput+1),
	} {
		if _, _, err := decodeOwnerHelperResult("COM7", encoded); err == nil {
			t.Fatalf("untrusted helper output accepted: %.80q", encoded)
		}
	}
}

func TestOwnerHelperCarriesScanFailureInsideSingleJSONResult(t *testing.T) {
	var output bytes.Buffer
	err := runHelperInvocationWith(
		context.Background(),
		[]string{ownerHelperArgument, "COM9"},
		&output,
		func(context.Context, string) (Owner, bool, error) {
			return Owner{}, false, errors.New("native scan unavailable")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeOwnerHelperResult("COM9", output.Bytes()); err == nil ||
		!strings.Contains(err.Error(), "native scan unavailable") {
		t.Fatalf("scan failure decode err=%v", err)
	}
}

func TestOwnerHelperUTF8TruncationDoesNotSplitRune(t *testing.T) {
	value := strings.Repeat("€", 10)
	truncated := truncateUTF8(value, 8)
	if truncated != "€€" {
		t.Fatalf("UTF-8 truncation=%q bytes=%d", truncated, len(truncated))
	}
}

func TestHelperChildAddsItsOwnDeadlineWithoutParentDeadline(t *testing.T) {
	_, _, err := boundedHelperLookup(context.Background(), "COM7", func(ctx context.Context, _ string) (Owner, bool, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > ownerHelperLifetime {
			t.Error("helper scan has no independent bounded lifetime")
		}
		return Owner{}, false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHelperReturnsToMainWhileNativeScanRemainsBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	finished := make(chan error, 1)
	var output bytes.Buffer
	go func() {
		finished <- runHelperInvocationWith(ctx, []string{ownerHelperArgument, "COM7"}, &output, func(context.Context, string) (Owner, bool, error) {
			close(entered)
			<-release // Model a native query that ignores context cancellation.
			return Owner{}, false, nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("scan did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("helper remained blocked instead of returning to main for process exit")
	}
	if _, found, err := decodeOwnerHelperResult("COM7", output.Bytes()); found || err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("timeout result found=%t err=%v", found, err)
	}
}

func TestHelperPreservesEarlierDeadlineAndRejectsCancelledScan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), ownerHelperLifetime/2)
	defer cancel()
	want, _ := ctx.Deadline()
	_, _, err := boundedHelperLookup(ctx, "COM7", func(ctx context.Context, _ string) (Owner, bool, error) {
		got, _ := ctx.Deadline()
		if !got.Equal(want) {
			t.Errorf("helper extended caller deadline")
		}
		return Owner{}, false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_, _, err = boundedHelperLookup(ctx, "COM7", func(context.Context, string) (Owner, bool, error) {
		t.Error("cancelled invocation reached native scan")
		return Owner{}, false, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error=%v", err)
	}
}
