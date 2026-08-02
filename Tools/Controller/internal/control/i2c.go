package control

import (
	"context"
	"fmt"

	"pccontroller.local/controller/internal/native"
)

// TransferI2C performs one bounded cooperative transaction through firmware.
// It intentionally exposes the Wire status instead of treating address NACK as
// a transport failure, which lets host-side discovery probe safely.
func TransferI2C(
	ctx context.Context,
	runtime *Runtime,
	address, leaseSeconds byte,
	write []byte,
	readLength byte,
) (native.I2CTransferResult, error) {
	if runtime == nil {
		return native.I2CTransferResult{}, fmt.Errorf("I2C runtime is unavailable")
	}
	if runtime.Snapshot().Hello.Capabilities&native.CapabilityI2CTransfer == 0 {
		return native.I2CTransferResult{}, fmt.Errorf("connected firmware does not advertise bounded I2C transfer")
	}
	payload, err := native.I2CTransferPayload(address, leaseSeconds, write, readLength)
	if err != nil {
		return native.I2CTransferResult{}, err
	}
	if address == 0 {
		if err := runtime.Command(ctx, native.OpI2CTransfer, payload); err != nil {
			return native.I2CTransferResult{}, err
		}
		return native.I2CTransferResult{}, nil
	}
	frame, err := runtime.Request(ctx, native.OpI2CTransfer, payload, native.OpI2CTransferResp)
	if err != nil {
		return native.I2CTransferResult{}, err
	}
	result, err := native.ParseI2CTransfer(frame.Payload)
	if err != nil {
		return native.I2CTransferResult{}, err
	}
	if result.Address != address {
		return native.I2CTransferResult{}, fmt.Errorf(
			"I2C response address 0x%02X does not match request 0x%02X",
			result.Address, address,
		)
	}
	if result.Status == 0 && len(result.Data) != int(readLength) {
		return native.I2CTransferResult{}, fmt.Errorf(
			"I2C 0x%02X returned %d of %d requested bytes",
			address, len(result.Data), readLength,
		)
	}
	return result, nil
}

func ReleaseI2C(ctx context.Context, runtime *Runtime) error {
	_, err := TransferI2C(ctx, runtime, 0, 0, nil, 0)
	return err
}

func ScanI2C(ctx context.Context, runtime *Runtime) ([]byte, error) {
	if runtime == nil {
		return nil, fmt.Errorf("I2C runtime is unavailable")
	}
	if runtime.Snapshot().Hello.Capabilities&native.CapabilityI2CTransfer == 0 {
		return nil, fmt.Errorf("firmware does not advertise generic I2C transfers")
	}
	addresses := make([]byte, 0, 8)
	for address := byte(0x08); address <= 0x77; address++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := TransferI2C(ctx, runtime, address, 0, nil, 0)
		if err != nil {
			return nil, err
		}
		if result.Status == 0 {
			addresses = append(addresses, address)
		}
	}
	return addresses, nil
}

func I2CStatusText(status byte) string {
	switch status {
	case 0:
		return "ok"
	case 1:
		return "data too long"
	case 2:
		return "address NACK"
	case 3:
		return "data NACK"
	case 4:
		return "bus error"
	case 5:
		return "timeout"
	default:
		return fmt.Sprintf("Wire status %d", status)
	}
}
