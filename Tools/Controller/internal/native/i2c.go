package native

import "fmt"

const (
	I2CMaximumWrite = 16
	I2CMaximumRead  = 16
)

// I2CTransferResult is the bounded Wire result returned by current firmware.
// Status uses Arduino Wire endTransmission codes; zero means success.
type I2CTransferResult struct {
	Status  byte   `json:"status"`
	Address byte   `json:"address"`
	Data    []byte `json:"data,omitempty"`
}

// I2CTransferPayload encodes address, cooperative lease, write bytes, and the
// requested read length. Address zero releases an existing lease.
func I2CTransferPayload(address, leaseSeconds byte, write []byte, readLength byte) ([]byte, error) {
	if address > 0x7F {
		return nil, fmt.Errorf("I2C address 0x%02X is outside 7-bit range", address)
	}
	if leaseSeconds > 10 {
		return nil, fmt.Errorf("I2C lease %d exceeds 10 seconds", leaseSeconds)
	}
	if len(write) > I2CMaximumWrite || readLength > I2CMaximumRead {
		return nil, fmt.Errorf(
			"I2C transfer write=%d read=%d exceeds %d-byte bounds",
			len(write), readLength, I2CMaximumWrite,
		)
	}
	if address == 0 && (leaseSeconds != 0 || len(write) != 0 || readLength != 0) {
		return nil, fmt.Errorf("I2C lease release must contain only address zero")
	}
	payload := make([]byte, 4+len(write))
	payload[0] = address
	payload[1] = leaseSeconds
	payload[2] = byte(len(write))
	payload[3] = readLength
	copy(payload[4:], write)
	return payload, nil
}

func ParseI2CTransfer(payload []byte) (I2CTransferResult, error) {
	if len(payload) < 3 {
		return I2CTransferResult{}, fmt.Errorf("I2C_TRANSFER response is %d bytes, need at least 3", len(payload))
	}
	if payload[1] == 0 || payload[1] > 0x7F {
		return I2CTransferResult{}, fmt.Errorf(
			"I2C_TRANSFER response address 0x%02X is outside usable 7-bit range",
			payload[1],
		)
	}
	count := int(payload[2])
	if count > I2CMaximumRead || len(payload) != 3+count {
		return I2CTransferResult{}, fmt.Errorf(
			"I2C_TRANSFER read count %d does not match %d-byte payload",
			count, len(payload),
		)
	}
	if payload[0] != 0 && count != 0 {
		return I2CTransferResult{}, fmt.Errorf(
			"I2C_TRANSFER error status %d unexpectedly contains %d read bytes",
			payload[0], count,
		)
	}
	return I2CTransferResult{
		Status: payload[0], Address: payload[1],
		Data: append([]byte(nil), payload[3:]...),
	}, nil
}
