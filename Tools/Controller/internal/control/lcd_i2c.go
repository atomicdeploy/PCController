package control

import (
	"context"
	"fmt"
	"sync"
	"time"

	"pccontroller.local/controller/internal/native"
)

const (
	lcdBacklight = byte(0x08)
	lcdEnable    = byte(0x04)
	lcdData      = byte(0x01)

	// Firmware reveals this host-preloaded hidden DDRAM page with sixteen 0x18
	// shifts after host timeout; no second MCU-side LCD renderer is required.
	lcdOfflineLine1 = "PC offline      "
	lcdOfflineLine2 = "Connect USB toPC"
)

type lcdTransferFunc func(
	context.Context, byte, byte, []byte, byte,
) (native.I2CTransferResult, error)

type pcOwnedLCDState struct {
	Available bool
	Address   byte
	Lines     [2]string
	LastError string
}

// pcOwnedLCD drives the fixed 2x16 HD44780/PCF8574 backpack. The firmware does
// not access the LCD address, so each atomic Wire transaction uses lease zero
// and does not pause unrelated INA219/PWM service.
type pcOwnedLCD struct {
	opMu      sync.Mutex
	stateMu   sync.RWMutex
	reported  pcOwnedLCDState
	transfer  lcdTransferFunc
	sleep     func(time.Duration)
	device    string
	address   byte
	lines     [2]string
	nextProbe time.Time
	lastError string
}

func newPCOwnedLCD(transfer lcdTransferFunc) *pcOwnedLCD {
	return &pcOwnedLCD{transfer: transfer, sleep: time.Sleep}
}

func (lcd *pcOwnedLCD) state() pcOwnedLCDState {
	lcd.stateMu.RLock()
	defer lcd.stateMu.RUnlock()
	return lcd.reported
}

// publishState snapshots operation-owned fields without making UI/status
// readers wait for a multi-frame UART render to finish.
func (lcd *pcOwnedLCD) publishState() {
	state := pcOwnedLCDState{
		Available: lcd.address != 0 && lcd.lastError == "",
		Address:   lcd.address, Lines: lcd.lines, LastError: lcd.lastError,
	}
	lcd.stateMu.Lock()
	lcd.reported = state
	lcd.stateMu.Unlock()
}

func (lcd *pcOwnedLCD) reset() {
	lcd.opMu.Lock()
	defer lcd.opMu.Unlock()
	lcd.device = ""
	lcd.address = 0
	lcd.lines = [2]string{}
	lcd.nextProbe = time.Time{}
	lcd.lastError = ""
	lcd.publishState()
}

func (lcd *pcOwnedLCD) render(ctx context.Context, device, line1, line2 string) error {
	lcd.opMu.Lock()
	defer lcd.opMu.Unlock()
	defer lcd.publishState()
	if lcd.transfer == nil {
		lcd.lastError = "PC-owned LCD transfer is unavailable"
		return fmt.Errorf("%s", lcd.lastError)
	}
	if lcd.device != device {
		lcd.device = device
		lcd.address = 0
		lcd.lines = [2]string{}
		lcd.nextProbe = time.Time{}
	}
	initialized := false
	if lcd.address == 0 {
		if !lcd.nextProbe.IsZero() && time.Now().Before(lcd.nextProbe) {
			return fmt.Errorf("PC-owned LCD not detected at 0x27 or 0x3F")
		}
		if err := lcd.discoverAndInitialize(ctx); err != nil {
			lcd.address = 0
			lcd.lines = [2]string{}
			lcd.lastError = err.Error()
			lcd.nextProbe = time.Now().Add(5 * time.Second)
			return err
		}
		initialized = true
	}
	if !initialized {
		if err := lcd.returnHome(ctx); err != nil {
			lcd.lastError = err.Error()
			return err
		}
	}
	lines := [2]string{lcdLine(line1), lcdLine(line2)}
	for row := range lines {
		if lcd.lines[row] == lines[row] {
			continue
		}
		sequence := make([]byte, 0, 102)
		sequence = appendLCDByte(sequence, 0x80+byte(row)*0x40, false)
		for index := 0; index < 16; index++ {
			sequence = appendLCDByte(sequence, lines[row][index], true)
		}
		if err := lcd.writeSequence(ctx, sequence); err != nil {
			lcd.lastError = err.Error()
			return err
		}
		lcd.lines[row] = lines[row]
	}
	lcd.lastError = ""
	return nil
}

func (lcd *pcOwnedLCD) discoverAndInitialize(ctx context.Context) error {
	for _, address := range []byte{0x27, 0x3F} {
		result, err := lcd.transfer(ctx, address, 0, nil, 0)
		if err != nil {
			return err
		}
		if result.Status == 0 {
			lcd.address = address
			break
		}
	}
	if lcd.address == 0 {
		return fmt.Errorf("PC-owned LCD not detected at 0x27 or 0x3F")
	}
	lcd.sleep(50 * time.Millisecond)
	for _, delay := range []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, time.Millisecond} {
		if err := lcd.writeSequence(ctx, appendLCDNibble(nil, 0x30, false)); err != nil {
			return err
		}
		lcd.sleep(delay)
	}
	if err := lcd.writeSequence(ctx, appendLCDNibble(nil, 0x20, false)); err != nil {
		return err
	}
	lcd.sleep(time.Millisecond)
	for _, command := range []byte{0x28, 0x08, 0x01} {
		if err := lcd.writeSequence(ctx, appendLCDByte(nil, command, false)); err != nil {
			return err
		}
		if command == 0x01 {
			lcd.sleep(2 * time.Millisecond)
		}
	}
	if err := lcd.writeSequence(ctx, appendLCDByte(appendLCDByte(nil, 0x06, false), 0x0C, false)); err != nil {
		return err
	}
	if err := lcd.preloadOfflinePage(ctx); err != nil {
		return err
	}
	// Return-home preserves the hidden page while resetting any display shift.
	if err := lcd.returnHome(ctx); err != nil {
		return err
	}
	lcd.lines = [2]string{}
	lcd.nextProbe = time.Time{}
	return nil
}

func (lcd *pcOwnedLCD) returnHome(ctx context.Context) error {
	if err := lcd.writeSequence(ctx, appendLCDByte(nil, 0x02, false)); err != nil {
		return err
	}
	lcd.sleep(2 * time.Millisecond)
	return nil
}

func (lcd *pcOwnedLCD) ensureHome(ctx context.Context, device string) error {
	lcd.opMu.Lock()
	defer lcd.opMu.Unlock()
	if lcd.address == 0 || lcd.device != device || lcd.lastError != "" {
		return nil
	}
	if err := lcd.returnHome(ctx); err != nil {
		lcd.lastError = err.Error()
		lcd.publishState()
		return err
	}
	return nil
}

func (lcd *pcOwnedLCD) preloadOfflinePage(ctx context.Context) error {
	for row, line := range []string{lcdOfflineLine1, lcdOfflineLine2} {
		sequence := appendLCDByte(nil, 0x90+byte(row)*0x40, false)
		for index := 0; index < 16; index++ {
			sequence = appendLCDByte(sequence, line[index], true)
		}
		if err := lcd.writeSequence(ctx, sequence); err != nil {
			return err
		}
	}
	return nil
}

func (lcd *pcOwnedLCD) writeSequence(ctx context.Context, sequence []byte) error {
	// Three PCF8574 writes form one stable low/high/low enable pulse. Chunk on
	// that boundary so separate UART requests cannot strand E high.
	if len(sequence)%3 != 0 {
		return fmt.Errorf("LCD PCF8574 sequence has incomplete enable pulse (%d bytes)", len(sequence))
	}
	for len(sequence) != 0 {
		count := len(sequence)
		if count > 15 {
			count = 15
		}
		result, err := lcd.transfer(ctx, lcd.address, 0, sequence[:count], 0)
		if err != nil {
			return err
		}
		if result.Status != 0 {
			return fmt.Errorf("LCD 0x%02X write failed: %s", lcd.address, I2CStatusText(result.Status))
		}
		sequence = sequence[count:]
	}
	return nil
}

func appendLCDNibble(sequence []byte, nibble byte, data bool) []byte {
	value := nibble & 0xF0
	if data {
		value |= lcdData
	}
	value |= lcdBacklight
	return append(sequence, value, value|lcdEnable, value)
}

func appendLCDByte(sequence []byte, value byte, data bool) []byte {
	sequence = appendLCDNibble(sequence, value, data)
	return appendLCDNibble(sequence, value<<4, data)
}
