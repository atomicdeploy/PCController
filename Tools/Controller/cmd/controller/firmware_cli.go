package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/programmer"
)

const firmwareArtifactUsage = "usage: controller firmware inspect --manifest firmware-manifest.json | identity --input IMAGE.hex | patch-identity --input IMAGE.hex --output PATCHED.hex --source-sha256 SHA256 --hash HEX8 --timestamp HEX8"

// runFirmwareArtifact provides hardware-free inspection and guarded patching
// for build-declared flash regions. It never opens a serial port.
func runFirmwareArtifact(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New(firmwareArtifactUsage)
	}
	switch strings.ToLower(args[0]) {
	case "inspect":
		flags := flag.NewFlagSet("firmware inspect", flag.ContinueOnError)
		flags.SetOutput(stderr)
		manifest := flags.String("manifest", "", "compile firmware-manifest.json")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*manifest) == "" {
			return errors.New(firmwareArtifactUsage)
		}
		inspection, err := programmer.InspectManifestRegions(*manifest)
		if err != nil {
			return err
		}
		return writeFirmwareArtifactJSON(stdout, inspection)

	case "identity":
		flags := flag.NewFlagSet("firmware identity", flag.ContinueOnError)
		flags.SetOutput(stderr)
		input := flags.String("input", "", "firmware Intel HEX image")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*input) == "" {
			return errors.New(firmwareArtifactUsage)
		}
		identity, err := programmer.InspectFirmwareIdentity(*input)
		if err != nil {
			return err
		}
		return writeFirmwareArtifactJSON(stdout, identity)

	case "patch-identity":
		flags := flag.NewFlagSet("firmware patch-identity", flag.ContinueOnError)
		flags.SetOutput(stderr)
		input := flags.String("input", "", "source firmware Intel HEX image")
		output := flags.String("output", "", "new non-overwriting Intel HEX artifact")
		sourceSHA := flags.String("source-sha256", "", "exact source file SHA-256")
		hashText := flags.String("hash", "", "replacement 8-digit source hash")
		timestampText := flags.String("timestamp", "", "replacement packed timestamp")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*input) == "" ||
			strings.TrimSpace(*output) == "" || strings.TrimSpace(*sourceSHA) == "" {
			return errors.New(firmwareArtifactUsage)
		}
		hash, err := parseFirmwareUint32(*hashText, "hash")
		if err != nil {
			return err
		}
		timestamp, err := parseFirmwareUint32(*timestampText, "timestamp")
		if err != nil {
			return err
		}
		result, err := programmer.PatchFirmwareIdentity(
			*input, *output, *sourceSHA, hash, timestamp,
		)
		if err != nil {
			return err
		}
		return writeFirmwareArtifactJSON(stdout, result)
	default:
		return errors.New(firmwareArtifactUsage)
	}
}

func parseFirmwareUint32(value, name string) (uint32, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if value == "" || len(value) > 8 {
		return 0, fmt.Errorf("firmware %s must be 1-8 hexadecimal digits", name)
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("firmware %s must be hexadecimal: %w", name, err)
	}
	return uint32(parsed), nil
}

func writeFirmwareArtifactJSON(output io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
