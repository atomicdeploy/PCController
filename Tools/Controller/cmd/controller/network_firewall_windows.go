//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pccontroller.local/controller/internal/installer"
)

func platformEnsureCanonicalNetworkFirewall(ctx context.Context) (networkFirewallReport, error) {
	executable, err := os.Executable()
	if err != nil {
		return networkFirewallReport{}, fmt.Errorf("locate running executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return networkFirewallReport{}, err
	}
	root, err := installer.DefaultInstallRoot()
	if err != nil {
		return networkFirewallReport{}, err
	}
	canonical := filepath.Join(root, "bin", "controller.exe")
	if !strings.EqualFold(filepath.Clean(executable), filepath.Clean(canonical)) {
		return networkFirewallReport{}, fmt.Errorf("firewall rules may only target the canonical installed executable %s; running %s", canonical, executable)
	}
	info, statErr := os.Stat(executable)
	if statErr != nil {
		return networkFirewallReport{}, fmt.Errorf("canonical installed executable is unavailable: %w", statErr)
	}
	if info.IsDir() {
		return networkFirewallReport{}, errors.New("canonical installed executable path is a directory")
	}

	run := func(commandContext context.Context, name string, arguments ...string) ([]byte, error) {
		output, commandErr := exec.CommandContext(commandContext, name, arguments...).CombinedOutput()
		if commandErr != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = commandErr.Error()
			}
			return nil, errors.New(message)
		}
		return output, nil
	}
	remove := []string{"advfirewall", "firewall", "delete", "rule", "name=all", "dir=in", "program=" + executable}
	if err := firewallCommandOutput(ctx, run, remove...); err != nil {
		return networkFirewallReport{}, fmt.Errorf("remove exact existing firewall rules (run from an elevated terminal): %w", err)
	}
	rules := []networkFirewallRule{
		{Name: "PCController canonical TCP", Direction: "in", Action: "allow", Profile: "private", Protocol: "TCP"},
		{Name: "PCController canonical UDP", Direction: "in", Action: "allow", Profile: "private", Protocol: "UDP"},
	}
	for _, rule := range rules {
		arguments := []string{
			"advfirewall", "firewall", "add", "rule", "name=" + rule.Name,
			"dir=" + rule.Direction, "action=" + rule.Action, "enable=yes",
			"profile=" + rule.Profile, "protocol=" + rule.Protocol, "program=" + executable,
		}
		if err := firewallCommandOutput(ctx, run, arguments...); err != nil {
			return networkFirewallReport{}, fmt.Errorf("create %s firewall rule: %w", rule.Protocol, err)
		}
	}
	return networkFirewallReport{Executable: executable, RemovedRules: true, CanonicalRules: rules}, nil
}
