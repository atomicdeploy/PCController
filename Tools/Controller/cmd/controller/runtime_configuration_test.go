package main

import (
	"flag"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
)

func TestReconnectPolicyUsesConfigEnvironmentFlagPriority(t *testing.T) {
	config := appconfig.Defaults().Connection
	config.ReconnectInitialMS = 700
	config.ReconnectMaximumMS = 12_000
	config.LastDevice = &appconfig.DeviceIdentity{
		Port: "COM7", VID: "1A86", PID: "7523", InstanceID: `USB\OLD`,
	}
	t.Setenv("PCCONTROLLER_PORT", "COM4")
	t.Setenv("PCCONTROLLER_RECONNECT_INITIAL_MS", "900")
	t.Setenv("PCCONTROLLER_RECONNECT_MAXIMUM_MS", "14000")

	flags := flag.NewFlagSet("connection-priority", flag.ContinueOnError)
	options := addConnectionFlags(flags, config)
	if err := flags.Parse([]string{"--reconnect-maximum=9s"}); err != nil {
		t.Fatal(err)
	}
	options.captureOverrides(flags)

	resolved := runtimeOptions(options)
	if resolved.Filter.Port != "COM4" {
		t.Fatalf("environment port override = %#v", resolved.Filter)
	}
	if resolved.Filter.Preferred.Port != "" {
		t.Fatalf("explicit environment port retained remembered preference: %#v", resolved.Filter)
	}
	if resolved.ReconnectInitialDelay != 900*time.Millisecond ||
		resolved.ReconnectMaximumDelay != 9*time.Second {
		t.Fatalf("initial resolved retry policy = %+v", resolved)
	}

	// A later file reload must not displace higher-priority environment/flag
	// values captured at process startup.
	config.ReconnectInitialMS = 1100
	config.ReconnectMaximumMS = 20_000
	reloaded := options.fromConfig(config)
	if reloaded.Filter.Port != "COM4" ||
		reloaded.ReconnectInitialDelay != 900*time.Millisecond ||
		reloaded.ReconnectMaximumDelay != 9*time.Second {
		t.Fatalf("reload displaced override priority: %+v", reloaded)
	}
}
