package control

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/native"
)

const i2cUsage = "i2c scan | read ADDRESS COUNT | write ADDRESS BYTE... | " +
	"transfer ADDRESS LEASE_SECONDS READ_COUNT [WRITE_BYTE...] | release | lcd status|rescan"

func i2cCommand(ctx context.Context, runtime *Runtime, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: %s", i2cUsage)
	}
	switch strings.ToLower(args[0]) {
	case "scan":
		if len(args) != 1 {
			return "", fmt.Errorf("usage: i2c scan")
		}
		addresses, err := scanForFirmware(ctx, runtime)
		if err != nil {
			return "", err
		}
		values := make([]string, 0, len(addresses))
		for _, address := range addresses {
			values = append(values, fmt.Sprintf("0x%02X", address))
		}
		if len(values) == 0 {
			return "I2C scan: no devices", nil
		}
		return "I2C scan: " + strings.Join(values, " "), nil

	case "read":
		if len(args) != 3 {
			return "", fmt.Errorf("usage: i2c read ADDRESS COUNT")
		}
		address, err := parseI2CByte(args[1], 0x7F, "address")
		if err != nil {
			return "", err
		}
		count, err := parseI2CByte(args[2], native.I2CMaximumRead, "read count")
		if err != nil {
			return "", err
		}
		return formatI2CTransfer(TransferI2C(ctx, runtime, address, 2, nil, count))

	case "write":
		if len(args) < 3 {
			return "", fmt.Errorf("usage: i2c write ADDRESS BYTE...")
		}
		address, err := parseI2CByte(args[1], 0x7F, "address")
		if err != nil {
			return "", err
		}
		write, err := parseI2CBytes(args[2:])
		if err != nil {
			return "", err
		}
		return formatI2CTransfer(TransferI2C(ctx, runtime, address, 2, write, 0))

	case "transfer":
		if len(args) < 4 {
			return "", fmt.Errorf("usage: i2c transfer ADDRESS LEASE_SECONDS READ_COUNT [WRITE_BYTE...]")
		}
		address, err := parseI2CByte(args[1], 0x7F, "address")
		if err != nil {
			return "", err
		}
		lease, err := parseI2CByte(args[2], 10, "lease seconds")
		if err != nil {
			return "", err
		}
		count, err := parseI2CByte(args[3], native.I2CMaximumRead, "read count")
		if err != nil {
			return "", err
		}
		write, err := parseI2CBytes(args[4:])
		if err != nil {
			return "", err
		}
		return formatI2CTransfer(TransferI2C(ctx, runtime, address, lease, write, count))

	case "release":
		if len(args) != 1 {
			return "", fmt.Errorf("usage: i2c release")
		}
		if err := ReleaseI2C(ctx, runtime); err != nil {
			return "", err
		}
		return "I2C cooperative lease released", nil

	case "lcd":
		if len(args) != 2 {
			return "", fmt.Errorf("usage: i2c lcd status|rescan")
		}
		switch strings.ToLower(args[1]) {
		case "status":
			snapshot := runtime.Snapshot()
			if !snapshot.Connected {
				return "LCD offline; retained physical contents are unverified", nil
			}
			if snapshot.Hello.Capabilities&native.CapabilityI2CTransfer == 0 {
				return fmt.Sprintf(
					"LCD enabled=%t owner=firmware address=0x%02X",
					snapshot.Settings.Flags&0x02 != 0, snapshot.Status.LCDAddress,
				), nil
			}
			state := runtime.LCDPresenter().State()
			return fmt.Sprintf(
				"LCD enabled=%t owner=PC physical=%t address=0x%02X error=%q",
				state.Enabled, state.Physical, state.Address, state.LastError,
			), nil
		case "rescan":
			address, err := runtime.LCDPresenter().RescanPhysical(ctx)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("PC-owned LCD initialized at 0x%02X", address), nil
		default:
			return "", fmt.Errorf("usage: i2c lcd status|rescan")
		}
	default:
		return "", fmt.Errorf("usage: %s", i2cUsage)
	}
}

func scanForFirmware(ctx context.Context, runtime *Runtime) ([]byte, error) {
	return ScanI2C(ctx, runtime)
}

func parseI2CByte(value string, maximum byte, name string) (byte, error) {
	parsed, err := strconv.ParseUint(value, 0, 8)
	if err != nil || parsed > uint64(maximum) {
		return 0, fmt.Errorf("%s %q must be 0..%d", name, value, maximum)
	}
	return byte(parsed), nil
}

func parseI2CBytes(values []string) ([]byte, error) {
	if len(values) > native.I2CMaximumWrite {
		return nil, fmt.Errorf("I2C write has %d bytes; maximum is %d", len(values), native.I2CMaximumWrite)
	}
	result := make([]byte, len(values))
	for index, value := range values {
		parsed, err := parseI2CByte(value, 0xFF, fmt.Sprintf("write byte %d", index))
		if err != nil {
			return nil, err
		}
		result[index] = parsed
	}
	return result, nil
}

func formatI2CTransfer(result native.I2CTransferResult, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if result.Status != 0 {
		return "", fmt.Errorf("I2C 0x%02X: %s", result.Address, I2CStatusText(result.Status))
	}
	return fmt.Sprintf(
		"I2C address=0x%02X status=ok read=% X",
		result.Address, result.Data,
	), nil
}
