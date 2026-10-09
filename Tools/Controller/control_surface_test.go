package controller

import (
	"context"
	"strings"
	"testing"
)

// This catalog test is the executable counterpart to the documented control-
// surface matrix. It proves every peripheral/domain enters the shared command
// engine used by the CLI, TUI prompt, IPC, network transports, and libraries.
func TestCommandCatalogCoversEveryControllerDomain(t *testing.T) {
	client := New(Options{})
	defer client.Shutdown()

	want := map[string][]string{
		"settings":                    {"settings"},
		"menu and front panel":        {"menu", "display"},
		"relay and motion":            {"relay"},
		"PWM and lighting":            {"pwm", "rgb", "strip"},
		"buzzer and melodies":         {"buzzer", "melody", "silent"},
		"RF learn, map, and transmit": {"rf"},
		"macros and automation":       {"macro", "automation"},
		"sensors and I2C":             {"status", "temp", "i2c"},
		"reset and programming":       {"reset", "toolchain", "boot", "program"},
		"host state and OS actions":   {"program-state", "os"},
	}
	available := make(map[string]CommandDescriptor)
	for _, descriptor := range client.CommandCatalog() {
		available[descriptor.Name] = descriptor
	}
	for domain, commands := range want {
		for _, name := range commands {
			descriptor, ok := available[name]
			if !ok {
				t.Errorf("%s has no shared command %q", domain, name)
				continue
			}
			if strings.TrimSpace(descriptor.Usage) == "" || strings.TrimSpace(descriptor.Summary) == "" || strings.TrimSpace(descriptor.Group) == "" {
				t.Errorf("%s command %q has incomplete metadata: %#v", domain, name, descriptor)
			}
			output, err := client.Execute(context.Background(), "help "+name)
			if err != nil || !strings.Contains(output, descriptor.Usage) {
				t.Errorf("library generic dispatch help %q output=%q err=%v", name, output, err)
			}
		}
	}
}
