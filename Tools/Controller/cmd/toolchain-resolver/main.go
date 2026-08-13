// Command toolchain-resolver is the hardware-free dependency-policy worker used
// by CI. It intentionally imports no serial, IPC, TUI, or integration package.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"pccontroller.local/controller/internal/envfile"
	"pccontroller.local/controller/internal/programmer"
)

func main() {
	if _, err := envfile.LoadProcess(); err != nil {
		fmt.Fprintln(os.Stderr, "environment:", err)
		os.Exit(1)
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "toolchain-resolver:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("usage: toolchain-resolver check|update [flags]")
	}
	action := arguments[0]
	flags := flag.NewFlagSet("toolchain-resolver "+action, flag.ContinueOnError)
	policyPath := flags.String("policy", "toolchain-profile.json", "latest-compatible policy JSON")
	lockPath := flags.String("lock", "toolchain-lock.json", "exact resolved lock JSON")
	includeCanary := flags.Bool("include-canary", false, "observe prerelease/main candidates without selecting them")
	directRetry := flags.Bool("direct-retry", true, "retry failed registry reads without proxy variables")
	requireCurrent := flags.Bool("require-current", false, "fail check when the stable lock is stale")
	jsonOutput := flags.Bool("json", false, "emit machine-readable result")
	moduleDir := flags.String("module-dir", ".", "directory containing go.mod and go.sum")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || (action != "check" && action != "update") {
		return errors.New("usage: toolchain-resolver check|update [--policy FILE] [--lock FILE] [--include-canary] [--json]")
	}
	policy, err := programmer.LoadToolchainPolicy(*policyPath)
	if err != nil {
		return err
	}
	current, loadErr := programmer.LoadToolchainLock(*lockPath)
	if loadErr != nil && action == "check" {
		return loadErr
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	resolution, err := programmer.ResolveToolchainPolicy(ctx, policy, programmer.ToolchainResolveOptions{
		DirectRetry: *directRetry, IncludeCanary: *includeCanary, ModuleDir: *moduleDir,
	})
	if err != nil {
		return err
	}
	changes := programmer.CompareToolchainLocks(current, resolution.Lock)
	written := false
	if action == "update" {
		written, err = programmer.UpdateToolchainLock(*lockPath, current, resolution.Lock)
		if err != nil {
			return err
		}
	}
	result := struct {
		Current bool                         `json:"current"`
		Written bool                         `json:"written"`
		Changes []programmer.ToolchainChange `json:"changes"`
		Canary  programmer.ToolchainCanary   `json:"canary,omitempty"`
	}{Current: len(changes) == 0, Written: written, Changes: changes, Canary: resolution.Canary}
	if *jsonOutput {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	} else if len(changes) == 0 {
		fmt.Println("resolved stable lock is current")
	} else {
		for _, change := range changes {
			fmt.Printf("%s|%s|%s|%s\n", change.Area, change.Name, change.Current, change.Resolved)
		}
	}
	if action == "check" && *requireCurrent && len(changes) != 0 {
		return fmt.Errorf("resolved lock is stale (%d changes)", len(changes))
	}
	return nil
}
