package link

import (
	"context"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestPendingSerialAcquisitionRejectsOperationsBeforeFirstClose(t *testing.T) {
	acquisition := &serialOpenAcquisition{result: make(chan serialOpenResult, 1)}
	session := newPendingOpenSession("TEST", acquisition)
	for _, operation := range []struct {
		name string
		call func() error
	}{
		{"write", func() error { return session.WriteRaw([]byte{1}) }},
		{"status", func() error {
			_, err := session.Request(context.Background(), native.OpGetStatus, nil, native.OpStatus)
			return err
		}},
		{"DTR", func() error { return session.PulseDTR(context.Background(), time.Millisecond) }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.call(); !errors.Is(err, ErrClosed) {
				t.Fatalf("close-only acquisition accepted %s: %v", operation.name, err)
			}
		})
	}
	if err := session.Close(); !errors.Is(err, ErrSerialOpenPending) {
		t.Fatalf("cleanup ownership lost before acquisition: %v", err)
	}
	port := newOpenAcquisitionPort()
	acquisition.result <- serialOpenResult{port: port}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	port.closeMu.Lock()
	defer port.closeMu.Unlock()
	if port.closeCalls != 1 {
		t.Fatalf("late port cleanup calls = %d", port.closeCalls)
	}
}
