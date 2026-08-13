package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/defaultassets"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/shell"
)

type primaryArtifactExecutor struct {
	client    *controllerapi.Client
	store     *appconfig.Store
	paths     programmer.HostDataPaths
	shutdown  func()
	forceExit func(int)
	execute   func(context.Context, string) (string, error)
}

func newArtifactHostService(
	client *controllerapi.Client,
	store *appconfig.Store,
	shutdown func(),
) (*artifacts.Service, error) {
	if client == nil || store == nil {
		return nil, errors.New("artifact service requires the primary client and configuration store")
	}
	dataPaths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		return nil, err
	}
	if err := programmer.EnsureHostDataPaths(dataPaths); err != nil {
		return nil, err
	}
	artifactStore, err := artifacts.NewStore(filepath.Join(dataPaths.DataDir, "artifacts"))
	if err != nil {
		return nil, err
	}
	executor := &primaryArtifactExecutor{
		client: client, store: store, paths: dataPaths, shutdown: shutdown,
		forceExit: os.Exit,
	}
	service, err := artifacts.NewService(artifacts.Options{
		Store: artifactStore, Executor: executor,
		Events: func(kind, text string, metadata map[string]string) {
			client.EmitHostActionEvent(kind, text, "artifact-service", "update", metadata)
		},
		BoardIdentity: func() artifacts.BoardIdentity {
			snapshot := client.Snapshot()
			identity := artifacts.BoardIdentity{Connected: snapshot.Connected}
			if snapshot.Connected {
				identity.BuildHash = fmt.Sprintf("%08X", snapshot.Hello.BuildHash)
				identity.BuildTimestamp = snapshot.Hello.BuildStamp
				identity.PackedTimestamp = snapshot.Hello.BuildTimestamp
			}
			return identity
		},
		RemoteProgrammingEnabled: func() bool {
			return store.Current().IPC.AllowRemote && store.Current().IPC.RemotePolicy.Programming
		},
	})
	if err != nil {
		return nil, err
	}
	if err := registerCurrentHostArtifact(service); err != nil {
		service.Close()
		return nil, err
	}
	if err := registerEmbeddedDefaults(service); err != nil {
		service.Close()
		return nil, err
	}
	if configured := strings.TrimSpace(store.Current().Paths.FirmwareHex); configured != "" {
		if info, statErr := os.Stat(configured); statErr == nil && info.Mode().IsRegular() {
			_, _ = artifactStore.PutFile(configured, artifacts.PutOptions{
				Kind: artifacts.KindFirmware, Name: filepath.Base(configured), Source: "configured-firmware",
			})
		}
	}
	return service, nil
}

func registerEmbeddedDefaults(service *artifacts.Service) error {
	bundle, err := defaultassets.Load()
	if err != nil {
		return fmt.Errorf("load embedded default board images: %w", err)
	}
	if !bundle.Enabled {
		return nil
	}
	firmware, err := service.Store().Put(bytes.NewReader(bundle.Firmware.Data), artifacts.PutOptions{
		Kind: artifacts.KindFirmware, Name: bundle.Firmware.Name, Source: "embedded",
		ExpectedSHA256: bundle.Firmware.SHA256, ExpectedBytes: int64(bundle.Firmware.Bytes),
		BuildHash: bundle.Firmware.BuildHash, BuildTimestamp: bundle.Firmware.BuildTimestamp,
		Embedded: true,
	})
	if err != nil {
		return fmt.Errorf("register embedded default firmware: %w", err)
	}
	eeprom, err := service.Store().Put(bytes.NewReader(bundle.EEPROM.Data), artifacts.PutOptions{
		Kind: artifacts.KindEEPROM, Name: bundle.EEPROM.Name, Source: "embedded",
		ExpectedSHA256: bundle.EEPROM.SHA256, ExpectedBytes: int64(bundle.EEPROM.Bytes),
		Embedded: true,
	})
	if err != nil {
		return fmt.Errorf("register embedded default EEPROM: %w", err)
	}
	if err := service.SetDefault(artifacts.KindFirmware, firmware.SHA256); err != nil {
		return err
	}
	return service.SetDefault(artifacts.KindEEPROM, eeprom.SHA256)
}

func registerCurrentHostArtifact(service *artifacts.Service) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current host executable: %w", err)
	}
	descriptor, err := service.Store().PutFile(executable, artifacts.PutOptions{
		Kind: artifacts.KindHostExecutable, Name: filepath.Base(executable),
		Source: "current-host", BuildHash: sourceHash, BuildTimestamp: buildTime,
		Platform: runtime.GOOS + "/" + runtime.GOARCH, Current: true,
	})
	if err != nil {
		return fmt.Errorf("register current host executable: %w", err)
	}
	return service.Store().SetCurrent(artifacts.KindHostExecutable, descriptor.SHA256)
}

func (executor *primaryArtifactExecutor) Capture(
	ctx artifacts.Context,
	request artifacts.CaptureRequest,
	progress artifacts.ProgressFunc,
) ([]artifacts.CapturedFile, error) {
	method, err := executor.method(request.Method)
	if err != nil {
		return nil, err
	}
	words := []string{"program", "backup", method, executor.paths.BackupsDir}
	if method == string(programmer.MethodUrclock) {
		port := executor.port(request.Port)
		if port == "" {
			return nil, artifacts.NewExecutionFailure(
				artifacts.ProgrammingMethodUrclock, artifacts.BootloaderNotAttempted,
				"serial_port_required", false,
				errors.New("Urclock capture requires a connected board or explicit port"),
			)
		}
		words = append(words, port)
	}
	progress("reading", 25, "releasing UART and reading flash, EEPROM, and bootloader metadata")
	output, err := executor.client.Execute(ctx, shell.Join(words))
	if err != nil {
		return nil, programmingExecutionFailure(method, err)
	}
	manifestPath, err := backupManifestPath(output)
	if err != nil {
		return nil, err
	}
	validated, err := programmer.ValidateBackupManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("validate captured backup: %w", err)
	}
	packed := uint32(0)
	if value := strings.TrimSpace(validated.Manifest.ApplicationPackedTimestamp); value != "" {
		parsed, parseErr := strconv.ParseUint(value, 16, 32)
		if parseErr != nil {
			return nil, fmt.Errorf("parse captured firmware timestamp: %w", parseErr)
		}
		packed = uint32(parsed)
	}
	progress("verifying", 80, "verified flash and EEPROM readback hashes")
	result := make([]artifacts.CapturedFile, 0, 2)
	if flash, ok := validated.Files["flash"]; ok {
		result = append(result, artifacts.CapturedFile{
			Kind: artifacts.KindFlashBackup, Name: filepath.Base(flash.Path), Path: flash.Path,
			BuildHash:      validated.Manifest.ApplicationHash,
			BuildTimestamp: validated.Manifest.ApplicationTimestamp, PackedTimestamp: packed,
		})
	}
	if eeprom, ok := validated.Files["eeprom"]; ok {
		result = append(result, artifacts.CapturedFile{
			Kind: artifacts.KindEEPROM, Name: filepath.Base(eeprom.Path), Path: eeprom.Path,
		})
	}
	return result, nil
}

func (executor *primaryArtifactExecutor) ProgramFirmware(
	ctx artifacts.Context,
	artifact artifacts.Descriptor,
	request artifacts.UpdateRequest,
	progress artifacts.ProgressFunc,
) error {
	method, err := executor.method(request.Method)
	if err != nil {
		return err
	}
	if strings.TrimSpace(artifact.LocalPath) == "" {
		return errors.New("firmware artifact has no verified local path")
	}
	words := []string{"program", "flash", artifact.LocalPath, "--method", method}
	if method == string(programmer.MethodUrclock) {
		port := executor.port(request.Port)
		if port == "" {
			return artifacts.NewExecutionFailure(
				artifacts.ProgrammingMethodUrclock, artifacts.BootloaderNotAttempted,
				"serial_port_required", false,
				errors.New("Urclock firmware update requires a connected board or explicit port"),
			)
		}
		words = append(words, port)
	}
	if request.ReinitializeEEPROM {
		words = append(words, "--reinitialize-eeprom")
	}
	progress("programming", 40, "guarded backup-then-flash transaction started")
	if _, err := executor.executeCommand(ctx, shell.Join(words)); err != nil {
		return programmingExecutionFailure(method, err)
	}
	progress("verifying", 95, "firmware write verified and application HELLO restored")
	return nil
}

// RestoreFlash keeps captured readbacks on a dedicated public contract while
// reusing the primary command engine's proven guarded flash transaction. That
// transaction owns the UART, captures flash/EEPROM/metadata before writing,
// verifies the write, reconnects HELLO, and restores the application lifecycle.
func (executor *primaryArtifactExecutor) RestoreFlash(
	ctx artifacts.Context,
	artifact artifacts.Descriptor,
	request artifacts.UpdateRequest,
	progress artifacts.ProgressFunc,
) error {
	if artifact.Kind != artifacts.KindFlashBackup {
		return fmt.Errorf("captured-flash restore requires %q, got %q", artifacts.KindFlashBackup, artifact.Kind)
	}
	method, err := explicitRestoreMethod(request.Method)
	if err != nil {
		return err
	}
	if strings.TrimSpace(artifact.LocalPath) == "" {
		return errors.New("captured-flash artifact has no verified local path")
	}
	words := []string{"program", "flash", artifact.LocalPath, "--method", method}
	if method == string(programmer.MethodUrclock) {
		port := executor.port(request.Port)
		if port == "" {
			return artifacts.NewExecutionFailure(
				artifacts.ProgrammingMethodUrclock, artifacts.BootloaderNotAttempted,
				"serial_port_required", false,
				errors.New("Urclock captured-flash restore requires a connected board or explicit port"),
			)
		}
		words = append(words, port)
	}
	progress("backing-up", 20, "capturing flash, EEPROM, and metadata before captured-flash restore")
	if _, err := executor.executeCommand(ctx, shell.Join(words)); err != nil {
		return programmingExecutionFailure(method, err)
	}
	progress("verifying", 95, "captured flash verified; application HELLO reconnected and lifecycle restored")
	return nil
}

func (executor *primaryArtifactExecutor) ProgramEEPROM(
	ctx artifacts.Context,
	artifact artifacts.Descriptor,
	request artifacts.UpdateRequest,
	progress artifacts.ProgressFunc,
) error {
	method, err := executor.method(request.Method)
	if err != nil {
		return err
	}
	if strings.TrimSpace(artifact.LocalPath) == "" {
		return errors.New("EEPROM artifact has no verified local path")
	}
	words := []string{"program", "write-eeprom", method, artifact.LocalPath, "CONFIRM"}
	if method == string(programmer.MethodUrclock) {
		port := executor.port(request.Port)
		if port == "" {
			return artifacts.NewExecutionFailure(
				artifacts.ProgrammingMethodUrclock, artifacts.BootloaderNotAttempted,
				"serial_port_required", false,
				errors.New("Urclock EEPROM restore requires a connected board or explicit port"),
			)
		}
		words = append(words, port)
	}
	progress("programming", 70, "writing EEPROM after verified flash and EEPROM backup")
	if _, err := executor.client.Execute(ctx, shell.Join(words)); err != nil {
		return programmingExecutionFailure(method, err)
	}
	progress("verifying", 95, "EEPROM write completed and application HELLO restored")
	return nil
}

func (executor *primaryArtifactExecutor) StageHostUpdate(
	ctx artifacts.Context,
	artifact artifacts.Descriptor,
	_ artifacts.UpdateRequest,
	progress artifacts.ProgressFunc,
) error {
	current, err := os.Executable()
	if err != nil {
		return err
	}
	progress("staging", 55, "verifying and staging host replacement")
	if _, err := artifacts.PrepareSelfUpdate(ctx, current, artifact.LocalPath, artifact.SHA256, nil); err != nil {
		return err
	}
	progress("staged", 95, "host replacement staged; primary will restart")
	executor.scheduleHostUpdateShutdown(500*time.Millisecond, 12*time.Second)
	return nil
}

func (executor *primaryArtifactExecutor) scheduleHostUpdateShutdown(gracefulDelay, forceDelay time.Duration) {
	if executor == nil || executor.shutdown == nil {
		return
	}
	go func() {
		time.Sleep(gracefulDelay)
		executor.shutdown()
		// A terminal UI can remain blocked in an OS input read after IPC,
		// serial, and integrations are closed. The verified external helper
		// owns rollback now, so bound how long the outgoing image can delay it.
		if executor.forceExit != nil {
			time.Sleep(forceDelay)
			executor.forceExit(0)
		}
	}()
}

func (executor *primaryArtifactExecutor) method(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(executor.store.Current().Programming.Method))
	}
	if value == "" {
		value = string(programmer.MethodUrclock)
	}
	if value != string(programmer.MethodUrclock) && value != string(programmer.MethodUSBasp) {
		return "", fmt.Errorf("update method %q is unsupported; use urclock or usbasp", value)
	}
	return value, nil
}

func (executor *primaryArtifactExecutor) ResolveProgrammingMethod(value string) (artifacts.ProgrammingMethod, error) {
	method, err := executor.method(value)
	if err != nil {
		return "", err
	}
	return artifacts.ProgrammingMethod(method), nil
}

func programmingExecutionFailure(method string, cause error) error {
	if cause == nil {
		return nil
	}
	typedMethod := artifacts.ProgrammingMethod(method)
	if typedMethod != artifacts.ProgrammingMethodUrclock {
		return artifacts.NewExecutionFailure(
			typedMethod, artifacts.BootloaderNotAttempted,
			"programming_failed", false, cause,
		)
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return artifacts.NewExecutionFailure(
			typedMethod, artifacts.BootloaderTimedOut,
			"bootloader_timeout", true, cause,
		)
	}
	lower := strings.ToLower(cause.Error())
	if strings.Contains(lower, "not in sync") ||
		strings.Contains(lower, "programmer is not responding") ||
		strings.Contains(lower, "initialization failed") ||
		strings.Contains(lower, "bootloader unavailable") {
		return artifacts.NewExecutionFailure(
			typedMethod, artifacts.BootloaderUnavailable,
			"bootloader_unavailable", true, cause,
		)
	}
	return artifacts.NewExecutionFailure(
		typedMethod, artifacts.BootloaderFailed,
		"programming_failed", false, cause,
	)
}

// explicitRestoreMethod defaults to the UART bootloader and accepts ISP only
// when the caller names it in the separately authorized restore request.
func explicitRestoreMethod(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return string(programmer.MethodUrclock), nil
	}
	if value != string(programmer.MethodUrclock) && value != string(programmer.MethodUSBasp) {
		return "", fmt.Errorf("flash restore method %q is unsupported; use urclock or explicitly request usbasp", value)
	}
	return value, nil
}

func (executor *primaryArtifactExecutor) executeCommand(
	ctx artifacts.Context,
	command string,
) (string, error) {
	if executor.execute != nil {
		return executor.execute(ctx, command)
	}
	return executor.client.Execute(ctx, command)
}

func (executor *primaryArtifactExecutor) port(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return strings.TrimSpace(executor.client.Snapshot().Port.Name)
}

func backupManifestPath(output string) (string, error) {
	const marker = "Backup complete; manifest:"
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if strings.HasPrefix(line, marker) {
			path := strings.TrimSpace(strings.TrimPrefix(line, marker))
			if path == "" || !filepath.IsAbs(path) {
				return "", errors.New("programmer returned an invalid backup manifest path")
			}
			return filepath.Clean(path), nil
		}
	}
	return "", errors.New("programmer completed without a backup manifest path")
}

var _ context.Context = (artifacts.Context)(nil)
