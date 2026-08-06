package main

import (
	"strings"
	"testing"
)

func TestBoardBlankConfirmationRequiresExactAuthenticatedName(t *testing.T) {
	if err := validateBoardBlankConfirmation("TEST-01", "COM4", "TEST-01"); err != nil {
		t.Fatal(err)
	}
	for _, supplied := range []string{"test-01", "ERASE-BOARD", "TEST-02", ""} {
		if err := validateBoardBlankConfirmation(supplied, "COM4", "TEST-01"); err == nil {
			t.Fatalf("confirmation %q was accepted", supplied)
		}
	}
}

func TestBoardBlankConfirmationWithoutUARTRequiresLiteral(t *testing.T) {
	if err := validateBoardBlankConfirmation("ERASE-BOARD", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := validateBoardBlankConfirmation("TEST-01", "", ""); err == nil || !strings.Contains(err.Error(), "UART identity is unavailable") {
		t.Fatalf("missing-UART confirmation error=%v", err)
	}
}
