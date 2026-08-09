package native

import "testing"

func TestI2CTransferPayloadAndResponse(t *testing.T) {
	payload, err := I2CTransferPayload(0x41, 2, []byte{0x00, 0x10}, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x41, 2, 2, 4, 0x00, 0x10}
	if string(payload) != string(want) {
		t.Fatalf("payload=% X want=% X", payload, want)
	}
	result, err := ParseI2CTransfer([]byte{0, 0x41, 2, 0xAA, 0x55})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != 0 || result.Address != 0x41 || string(result.Data) != string([]byte{0xAA, 0x55}) {
		t.Fatalf("result=%+v", result)
	}
}

func TestI2CTransferBoundsAndLeaseRelease(t *testing.T) {
	if _, err := I2CTransferPayload(0x40, 11, nil, 0); err == nil {
		t.Fatal("accepted lease longer than firmware bound")
	}
	if _, err := I2CTransferPayload(0, 1, nil, 0); err == nil {
		t.Fatal("accepted malformed lease release")
	}
	if _, err := I2CTransferPayload(0, 0, nil, 0); err != nil {
		t.Fatalf("valid lease release: %v", err)
	}
	if _, err := ParseI2CTransfer([]byte{0, 0x27, 2, 1}); err == nil {
		t.Fatal("accepted truncated response")
	}
	if _, err := ParseI2CTransfer([]byte{0, 0x80, 0}); err == nil {
		t.Fatal("accepted response with non-7-bit address")
	}
	if _, err := ParseI2CTransfer([]byte{2, 0x27, 1, 0xAA}); err == nil {
		t.Fatal("accepted read data alongside an I2C error status")
	}
}
