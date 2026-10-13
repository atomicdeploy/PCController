package ipcjson

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPWMWriteAndReadbackShareDeadlineWithoutRetry(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	writes := 0
	var writeDeadline time.Time
	_, err := changePWMAndReadback(parent, func(ctx context.Context) error {
		writes++
		writeDeadline, _ = ctx.Deadline()
		return nil
	}, func(ctx context.Context) (pwmValues, error) {
		readDeadline, _ := ctx.Deadline()
		if readDeadline != writeDeadline {
			t.Fatal("readback received a fresh timeout budget")
		}
		<-ctx.Done()
		return pwmValues{}, ctx.Err()
	})
	var confirmation *pwmConfirmationError
	if writes != 1 || !errors.As(err, &confirmation) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write count=%d error=%v", writes, err)
	}
	rpc := dispatchRPCError(err)
	data := rpc.Data.(map[string]any)
	if data["kind"] != "confirmation_unavailable" || data["write_acknowledged"] != true || data["retryable"] != false {
		t.Fatalf("lost acknowledgement evidence: %#v", data)
	}
}

func TestPWMUnacknowledgedWriteDoesNotReadOrRetry(t *testing.T) {
	writes, reads := 0, 0
	_, err := changePWMAndReadback(context.Background(), func(ctx context.Context) error {
		writes++
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) > pwmOperationTimeout {
			t.Fatal("unbounded PWM write")
		}
		return context.DeadlineExceeded
	}, func(context.Context) (pwmValues, error) { reads++; return pwmValues{}, nil })
	var confirmation *pwmConfirmationError
	if writes != 1 || reads != 0 || errors.As(err, &confirmation) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("writes=%d reads=%d error=%v", writes, reads, err)
	}
}
