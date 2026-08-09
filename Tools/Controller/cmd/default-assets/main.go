// Command default-assets materializes the canonical build-time EEPROM image.
// It is a packaging helper only and never opens a serial port or programmer.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"pccontroller.local/controller/internal/programmer"
)

func main() {
	output := flag.String("output", "", "destination Intel HEX EEPROM image")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "--output is required")
		os.Exit(2)
	}
	content, err := programmer.GenerateDefaultEEPROMIntelHex()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	absolute, err := filepath.Abs(*output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	temporary := absolute + ".new"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Rename(temporary, absolute); err != nil {
		_ = os.Remove(temporary)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
