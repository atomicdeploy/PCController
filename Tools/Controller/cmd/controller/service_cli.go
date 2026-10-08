package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
)

const (
	windowsServiceName        = "PCController"
	windowsServiceDisplayName = "PCController Hardware Service"
)

type serviceRuntimeOptions struct {
	ConfigPath string
	DataDir    string
	Listen     string
	Start      bool
	BinaryPath string
}

func runServiceCommand(args []string, configPath string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: controller service install|repair|status|start|stop|restart|remove")
	}
	return runPlatformServiceCommand(strings.ToLower(args[0]), args[1:], configPath, stdout, stderr)
}

func parseServiceRuntimeOptions(command string, args []string, configPath string, stderr io.Writer) (serviceRuntimeOptions, error) {
	resolvedConfig, err := appconfig.ResolvePath(configPath)
	if err != nil {
		return serviceRuntimeOptions{}, err
	}
	resolvedConfig, err = filepath.Abs(resolvedConfig)
	if err != nil {
		return serviceRuntimeOptions{}, fmt.Errorf("resolve service config path: %w", err)
	}
	options := serviceRuntimeOptions{
		ConfigPath: resolvedConfig,
		DataDir:    filepath.Join(filepath.Dir(resolvedConfig), "service-data"),
		Start:      command != "run",
	}
	flags := flag.NewFlagSet("service "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.DataDir, "data-dir", options.DataDir, "absolute durable data/log directory owned by the service")
	flags.StringVar(&options.Listen, "listen", "", "optional transient loopback WebUI address")
	if command == "install" || command == "repair" {
		flags.BoolVar(&options.Start, "start", true, "start the service after reconciling it")
		flags.StringVar(&options.BinaryPath, "binary", "", "controller.exe to register (defaults to this executable)")
	}
	if err := flags.Parse(args); err != nil {
		return serviceRuntimeOptions{}, err
	}
	if flags.NArg() != 0 {
		return serviceRuntimeOptions{}, fmt.Errorf("service %s does not accept positional arguments", command)
	}
	options.DataDir, err = filepath.Abs(options.DataDir)
	if err != nil {
		return serviceRuntimeOptions{}, fmt.Errorf("resolve service data directory: %w", err)
	}
	if !filepath.IsAbs(options.ConfigPath) || !filepath.IsAbs(options.DataDir) {
		return serviceRuntimeOptions{}, errors.New("service config and data paths must be absolute")
	}
	if options.BinaryPath == "" && (command == "install" || command == "repair") {
		options.BinaryPath, err = os.Executable()
		if err != nil {
			return serviceRuntimeOptions{}, fmt.Errorf("resolve service executable: %w", err)
		}
	}
	if options.BinaryPath != "" {
		options.BinaryPath, err = filepath.Abs(options.BinaryPath)
		if err != nil {
			return serviceRuntimeOptions{}, fmt.Errorf("resolve service executable: %w", err)
		}
	}
	if strings.TrimSpace(options.Listen) != "" {
		if _, err := loopbackWebListen(options.Listen); err != nil {
			return serviceRuntimeOptions{}, err
		}
	}
	return options, nil
}

func serviceImageArguments(options serviceRuntimeOptions) []string {
	args := []string{"--config=" + options.ConfigPath, "service", "run", "--data-dir=" + options.DataDir}
	if strings.TrimSpace(options.Listen) != "" {
		args = append(args, "--listen="+options.Listen)
	}
	return args
}
