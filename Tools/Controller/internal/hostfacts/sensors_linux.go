//go:build linux

package hostfacts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type sensorOutput struct {
	bytes.Buffer
	overflow bool
}

func (output *sensorOutput) Write(data []byte) (int, error) {
	if output.Len()+len(data) > maxSensorInputBytes {
		output.overflow = true
		return 0, errors.New("sensor command output exceeds 2 MiB")
	}
	return output.Buffer.Write(data)
}

func linuxSensorFacts(ctx context.Context) ([]map[string]any, bool, error) {
	path, err := exec.LookPath("sensors")
	if err != nil {
		return nil, false, errors.New("PC sensors unavailable: install lm-sensors and configure the appropriate kernel sensor drivers")
	}
	command := exec.CommandContext(ctx, path, "-j")
	command.WaitDelay = 250 * time.Millisecond
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "LC_ALL=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "LC_ALL=C")
	var output, diagnostic sensorOutput
	command.Stdout, command.Stderr = &output, &diagnostic
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, fmt.Errorf("read lm-sensors: %w", err)
	}
	if output.overflow || diagnostic.overflow {
		return nil, false, errors.New("lm-sensors output exceeds 2 MiB")
	}
	return parseLMSensors(output.Bytes())
}
