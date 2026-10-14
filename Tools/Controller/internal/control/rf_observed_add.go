package control

import (
	"context"
	"errors"
	"pccontroller.local/controller/internal/native"
)

type rfPinnedTransport struct {
	runtime    *Runtime
	generation uint64
}

func (p rfPinnedTransport) Request(ctx context.Context, opcode byte, payload []byte, expected ...byte) (native.Frame, error) {
	return p.runtime.requestAtGeneration(ctx, p.generation, opcode, payload, expected...)
}
func (p rfPinnedTransport) Command(ctx context.Context, opcode byte, payload []byte) error {
	_, err := p.Request(ctx, opcode, payload, native.OpACK)
	return err
}

// AddObservedRF explicitly saves a genuinely received waveform without entering
// learning or assigning an output. Preserve every existing complete record.
func (runtime *Runtime) AddObservedRF(ctx context.Context, code uint32, bits, protocol byte) error {
	runtime.mu.RLock()
	generation := runtime.generation
	runtime.mu.RUnlock()
	if runtime.RFLearnState().Active {
		return errors.New("stop learning before adding an observed code")
	}
	runtime.eventMu.Lock()
	receipt, known := runtime.rfReceptions[rfGestureKey{code, bits, protocol}]
	runtime.eventMu.Unlock()
	if !known || receipt.Generation != generation {
		return errors.New("waveform was not received from the current board")
	}
	service := NewRFReplaceService(runtime)
	service.transport = rfPinnedTransport{runtime, generation}
	unlock := service.lockTransaction()
	defer unlock()
	if support := service.Support(); !support.Supported {
		return errors.New(support.Reason)
	}
	original, err := service.Fetch(ctx)
	if err != nil {
		return err
	}
	for _, entry := range original {
		if entry.Code == code && entry.Bits == bits && entry.Protocol == protocol {
			return nil
		}
	}
	if len(original) >= 20 {
		return errors.New("RF storage is full")
	}
	var id byte
	for ; id < 20; id++ {
		used := false
		for _, entry := range original {
			used = used || entry.ID == id
		}
		if !used {
			break
		}
	}
	desired := append(append([]native.RFEntry(nil), original...), native.RFEntry{ID: id, Code: code, Bits: bits, Protocol: protocol, PulseUS: receipt.PulseUS})
	return service.replaceSnapshotLocked(ctx, original, desired)
}
