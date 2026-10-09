package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/ports"
)

func selectInteractiveDevice(
	options *connectionFlags,
	input io.Reader,
	output io.Writer,
) error {
	list, err := ports.List()
	if err != nil {
		return err
	}
	return selectInteractiveDeviceFrom(options, list, input, output)
}

func selectInteractiveDeviceFrom(
	options *connectionFlags,
	all []ports.Info,
	input io.Reader,
	output io.Writer,
) error {
	if options == nil {
		return errors.New("connection options are nil")
	}
	filter := options.filter()
	candidates := ports.Candidates(all, filter)
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return nil
	}
	if _, ok := ports.PreferredCandidate(
		candidates,
		filter.Preferred,
	); ok {
		return nil
	}

	fmt.Fprintln(output, "Multiple matching controller serial devices:")
	for index, candidate := range candidates {
		fmt.Fprintf(output, "  %d) %s\n", index+1, candidate.Label())
	}
	fmt.Fprint(output, "Select device [1]: ")
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		value := strings.TrimSpace(scanner.Text())
		if value == "" {
			value = "1"
		}
		index, err := strconv.Atoi(value)
		if err == nil && index >= 1 && index <= len(candidates) {
			setSelectedDevice(options, candidates[index-1])
			return nil
		}
		fmt.Fprintf(
			output,
			"Enter a number from 1 to %d: ",
			len(candidates),
		)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errors.New("device selection ended before a choice was made")
}

func setSelectedDevice(
	options *connectionFlags,
	selected ports.Info,
) {
	options.device = ""
	options.port = selected.Name
	options.vid = ""
	options.pid = ""
	options.name = ""
	delete(options.overrides, "device")
	delete(options.overrides, "vid")
	delete(options.overrides, "pid")
	delete(options.overrides, "name")
	options.overrides["port"] = true
}
