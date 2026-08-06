package pcspeaker

import "testing"

func TestPITDivisorAndBounds(t *testing.T) {
	divisor, err := pitDivisor(440)
	if err != nil {
		t.Fatal(err)
	}
	if divisor != 2711 {
		t.Fatalf("440 Hz divisor=%d, want 2711", divisor)
	}
	if _, err := pitDivisor(0); err == nil {
		t.Fatal("zero frequency was accepted")
	}
}
