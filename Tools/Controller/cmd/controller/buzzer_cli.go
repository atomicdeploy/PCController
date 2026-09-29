package main

import (
	"errors"
	"flag"
	"io"
	"strconv"

	"pccontroller.local/controller/internal/appconfig"
)

// runBuzzer is a typed spelling of the canonical command-engine operation. It
// deliberately shares exec's primary/local routing and board-silent preflight.
func runBuzzer(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	connection, command, err := parseBuzzerCommand(args, stderr, store.Current().Connection)
	if err != nil {
		return err
	}
	return runExecCommand(connection, command, stdout, store)
}

func parseBuzzerCommand(args []string, stderr io.Writer, config appconfig.Connection) (*connectionFlags, string, error) {
	flags := flag.NewFlagSet("buzzer", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, config)
	frequency := flags.Uint("frequency", 0, "tone frequency in Hz (0 stops the buzzer)")
	duration := flags.Uint("duration", 0, "tone duration in milliseconds")
	if err := flags.Parse(args); err != nil {
		return nil, "", err
	}
	connection.captureOverrides(flags)
	if flags.NArg() != 0 {
		return nil, "", errors.New("usage: controller buzzer --frequency HZ --duration MS [connection flags]")
	}
	if *duration == 0 || *duration > 0xffff {
		return nil, "", errors.New("buzzer duration must be 1..65535 ms")
	}
	if *frequency > 0xffff || (*frequency != 0 && (*frequency < 20 || *frequency > 20000)) {
		return nil, "", errors.New("buzzer frequency must be 0 or 20..20000 Hz")
	}
	command := "buzzer " + strconv.FormatUint(uint64(*frequency), 10) + " " + strconv.FormatUint(uint64(*duration), 10)
	return connection, command, nil
}
