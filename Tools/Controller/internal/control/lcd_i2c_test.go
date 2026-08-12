package control

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestHostOwnedLCDDiscoversInitializesAndCaches(t *testing.T) {
	var writes [][]byte
	var leases []byte
	transfer := func(
		_ context.Context,
		address, lease byte,
		write []byte,
		_ byte,
	) (native.I2CTransferResult, error) {
		leases = append(leases, lease)
		status := byte(2)
		if address == 0x27 {
			status = 0
		}
		if len(write) != 0 {
			copyOfWrite := append([]byte(nil), write...)
			writes = append(writes, copyOfWrite)
		}
		return native.I2CTransferResult{Status: status, Address: address}, nil
	}
	lcd := newHostOwnedLCD(transfer)
	lcd.sleep = func(time.Duration) {}
	if err := lcd.render(context.Background(), "device-1", "PC offline", "Connect USB"); err != nil {
		t.Fatal(err)
	}
	state := lcd.state()
	if !state.Available || state.Address != 0x27 {
		t.Fatalf("state=%+v", state)
	}
	if len(writes) == 0 {
		t.Fatal("LCD produced no PCF8574 writes")
	}
	for index, lease := range leases {
		if lease != 0 {
			t.Fatalf("transfer %d paused unrelated I2C service with lease %d", index, lease)
		}
	}
	for index, write := range writes {
		if len(write) > native.I2CMaximumWrite || len(write)%3 != 0 {
			t.Fatalf("write %d has unsafe pulse chunk length %d", index, len(write))
		}
		if write[len(write)-1]&lcdEnable != 0 {
			t.Fatalf("write %d strands LCD enable high: % X", index, write)
		}
	}
	before := len(writes)
	if err := lcd.render(context.Background(), "device-1", "PC offline", "Connect USB"); err != nil {
		t.Fatal(err)
	}
	if len(writes) != before+1 || string(writes[before]) != string(appendLCDByte(nil, 0x02, false)) {
		t.Fatalf("unchanged render must only restore home; extra=% X", writes[before:])
	}
}

func TestLCDWriteSequenceAbortsAfterFirstI2CFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		result native.I2CTransferResult
		err    error
	}{
		{name: "transport", err: errors.New("UART timeout")},
		{name: "device-status", result: native.I2CTransferResult{Status: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			lcd := newHostOwnedLCD(func(
				_ context.Context, _ byte, _ byte, _ []byte, _ byte,
			) (native.I2CTransferResult, error) {
				calls++
				return test.result, test.err
			})
			lcd.address = 0x27
			// More than one 15-byte UART/I2C chunk proves no later chunk is sent.
			sequence := make([]byte, 30)
			if err := lcd.writeSequence(context.Background(), sequence); err == nil {
				t.Fatal("failed I2C transfer was accepted")
			}
			if calls != 1 {
				t.Fatalf("LCD continued after first failure: calls=%d", calls)
			}
		})
	}
}

func TestLCDNibbleUsesCommonPCF8574MappingAndSafePulse(t *testing.T) {
	command := appendLCDNibble(nil, 0xA0, false)
	wantCommand := []byte{0xA8, 0xAC, 0xA8}
	if string(command) != string(wantCommand) {
		t.Fatalf("command pulse=% X want=% X", command, wantCommand)
	}
	data := appendLCDNibble(nil, 0x50, true)
	wantData := []byte{0x59, 0x5D, 0x59}
	if string(data) != string(wantData) {
		t.Fatalf("data pulse=% X want=% X", data, wantData)
	}
}

func TestLCDRejectsIncompleteEnablePulse(t *testing.T) {
	lcd := newHostOwnedLCD(nil)
	if err := lcd.writeSequence(context.Background(), []byte{0x08, 0x0C}); err == nil {
		t.Fatal("accepted a sequence that could strand enable high")
	}
}

func TestLCDPreloadsExactHiddenOfflineFallback(t *testing.T) {
	if len(lcdOfflineLine1) != 16 || len(lcdOfflineLine2) != 16 {
		t.Fatalf("offline page must be exactly 2x16: %q/%q", lcdOfflineLine1, lcdOfflineLine2)
	}
	var flattened []byte
	lcd := newHostOwnedLCD(func(
		_ context.Context, address, lease byte, write []byte, _ byte,
	) (native.I2CTransferResult, error) {
		if lease != 0 {
			t.Fatalf("offline preload used lease %d", lease)
		}
		flattened = append(flattened, write...)
		return native.I2CTransferResult{Status: 0, Address: address}, nil
	})
	lcd.address = 0x27
	if err := lcd.preloadOfflinePage(context.Background()); err != nil {
		t.Fatal(err)
	}
	for row, line := range []string{lcdOfflineLine1, lcdOfflineLine2} {
		want := appendLCDByte(nil, 0x90+byte(row)*0x40, false)
		for index := 0; index < len(line); index++ {
			want = appendLCDByte(want, line[index], true)
		}
		if !bytes.Contains(flattened, want) {
			t.Fatalf("hidden row %d was not encoded exactly; want=% X", row, want)
		}
	}
}

func TestLCDStateDoesNotBlockBehindUARTRender(t *testing.T) {
	blocking := false
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	lcd := newHostOwnedLCD(func(
		_ context.Context, address, _ byte, write []byte, _ byte,
	) (native.I2CTransferResult, error) {
		if blocking && len(write) != 0 {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
		}
		return native.I2CTransferResult{Status: 0, Address: address}, nil
	})
	lcd.sleep = func(time.Duration) {}
	if err := lcd.render(context.Background(), "device", "first", "state"); err != nil {
		t.Fatal(err)
	}
	blocking = true
	done := make(chan error, 1)
	go func() {
		done <- lcd.render(context.Background(), "device", "second", "state")
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("render did not enter blocked UART transfer")
	}
	stateDone := make(chan hostOwnedLCDState, 1)
	go func() { stateDone <- lcd.state() }()
	select {
	case state := <-stateDone:
		if !state.Available || state.Lines[0] != lcdLine("first") {
			t.Fatalf("nonblocking state lost last confirmation: %+v", state)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("LCD state blocked behind a UART render")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHostOwnedLCDReportsMissingBackpack(t *testing.T) {
	lcd := newHostOwnedLCD(func(
		_ context.Context,
		address, _ byte,
		_ []byte,
		_ byte,
	) (native.I2CTransferResult, error) {
		return native.I2CTransferResult{Status: 2, Address: address}, nil
	})
	lcd.sleep = func(time.Duration) {}
	if err := lcd.render(context.Background(), "device-1", "one", "two"); err == nil {
		t.Fatal("missing LCD was reported as available")
	}
	state := lcd.state()
	if state.Available || state.Address != 0 || state.LastError == "" {
		t.Fatalf("state=%+v", state)
	}
}
