package main

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/sessionsnapshot"
)

// hostSessionRecorder owns the rolling graceful-exit diagnostic cache. It is
// intentionally independent from both persistent host config and MCU EEPROM.
type hostSessionRecorder struct {
	recorder  *sessionsnapshot.Recorder
	dataPaths programmer.HostDataPaths
	setupErr  error
}

func newHostSessionRecorder(
	client *controllerapi.Client,
	store *appconfig.Store,
) *hostSessionRecorder {
	paths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		return &hostSessionRecorder{setupErr: err}
	}
	recorder := newHostSessionRecorderAt(paths.LastSessionPath, client, func() sessionsnapshot.HostIdentity {
		configuredTitle := ""
		if store != nil {
			configuredTitle = store.Current().UI.AppTitle
		}
		return sessionsnapshot.HostIdentity{
			Title: productidentity.Title(configuredTitle), Role: "primary-host",
			Version: version, SourceHash: sourceHash, BuildTime: buildTime,
		}
	})
	recorder.dataPaths = paths
	return recorder
}

// attachArtifactContext wires passive, cached artifact and recovery metadata
// into the atomic shutdown snapshot after the artifact service is constructed.
func (recorder *hostSessionRecorder) attachArtifactContext(service *artifacts.Service) {
	if recorder == nil || recorder.recorder == nil || recorder.setupErr != nil {
		return
	}
	if service == nil {
		recorder.setupErr = errors.New("session snapshot artifact service is unavailable")
		return
	}
	if err := recorder.recorder.SetOperationalContextProvider(func() (sessionsnapshot.OperationalContext, error) {
		return collectSessionOperationalContext(service, recorder.dataPaths)
	}); err != nil {
		recorder.setupErr = err
	}
}

func collectSessionOperationalContext(
	service *artifacts.Service,
	paths programmer.HostDataPaths,
) (sessionsnapshot.OperationalContext, error) {
	context := sessionsnapshot.OperationalContext{RecoveryMarkers: []sessionsnapshot.RecoveryMarker{}}
	var failures []error
	manifest, err := service.Manifest()
	if err != nil {
		failures = append(failures, fmt.Errorf("artifact manifest: %w", err))
	} else {
		context.Artifacts = sessionsnapshot.ArtifactHashes{
			CurrentFirmwareSHA256: artifactDescriptorHash(manifest.Current.Firmware),
			CurrentEEPROMSHA256:   artifactDescriptorHash(manifest.Current.EEPROM),
			CurrentFlashSHA256:    artifactDescriptorHash(manifest.Current.FlashReadback),
			CurrentHostSHA256:     artifactDescriptorHash(manifest.Current.Host),
			DefaultFirmwareSHA256: artifactDescriptorHash(manifest.Defaults.Firmware),
			DefaultEEPROMSHA256:   artifactDescriptorHash(manifest.Defaults.EEPROM),
		}
		if manifest.Update != nil {
			update := manifest.Update
			context.Programming = &sessionsnapshot.ProgrammingOperation{
				ID: update.ID, Kind: update.Kind, State: update.State,
				Active:               update.State != "completed" && update.State != "failed",
				ProgressPercent:      update.ProgressPercent,
				ArtifactSHA256:       update.ArtifactSHA256,
				ProgrammingMethod:    string(update.ProgrammingMethod),
				BootloaderOutcome:    string(update.BootloaderOutcome),
				ISPFallbackSuggested: update.ISPFallbackSuggested,
				ErrorCode:            update.ErrorCode, StartedAt: update.StartedAt, UpdatedAt: update.UpdatedAt,
			}
		}
	}
	markers, markerErr := control.InspectProgrammingRecoveryMarkers(paths)
	if markerErr != nil {
		failures = append(failures, markerErr)
	}
	for _, marker := range markers {
		context.RecoveryMarkers = append(context.RecoveryMarkers, sessionsnapshot.RecoveryMarker{
			MarkerSHA256:           marker.MarkerSHA256,
			TargetFirmwareSHA256:   marker.TargetFirmwareSHA256,
			SettingsSnapshotSHA256: marker.SettingsSnapshotSHA256,
			DeviceFingerprint:      marker.DeviceFingerprint,
			PreparedAt:             marker.PreparedAt, Phase: marker.Phase,
			HostResult: marker.HostResult, DiagnosticState: marker.DiagnosticState,
			WarningCount: marker.WarningCount, RestorationPending: marker.RestorationPending,
			WriteCompletionProven: false,
		})
	}
	return context, errors.Join(failures...)
}

func artifactDescriptorHash(descriptor *artifacts.Descriptor) string {
	if descriptor == nil {
		return ""
	}
	return descriptor.SHA256
}

func newHostSessionRecorderAt(
	path string,
	source sessionsnapshot.Source,
	identity func() sessionsnapshot.HostIdentity,
) *hostSessionRecorder {
	recorder, err := sessionsnapshot.NewRecorder(path, source, identity)
	return &hostSessionRecorder{recorder: recorder, setupErr: err}
}

func (recorder *hostSessionRecorder) read() (any, error) {
	if recorder == nil {
		return nil, errors.New("graceful-exit diagnostic snapshot is unavailable")
	}
	if recorder.setupErr != nil {
		return nil, recorder.setupErr
	}
	return recorder.recorder.Stored()
}

func (recorder *hostSessionRecorder) save() (sessionsnapshot.SaveResult, error) {
	if recorder == nil {
		return sessionsnapshot.SaveResult{}, errors.New("graceful-exit diagnostic snapshot is unavailable")
	}
	if recorder.setupErr != nil {
		return sessionsnapshot.SaveResult{}, recorder.setupErr
	}
	return recorder.recorder.Save()
}

// persistAndPublish saves before the IPC/event fan-out is stopped, allowing
// connected API consumers to observe the compact result when shutdown begins.
func (recorder *hostSessionRecorder) persistAndPublish(runtime *control.Runtime) error {
	result, err := recorder.save()
	if runtime == nil {
		return err
	}
	metadata := map[string]string{}
	lifecycle := "saved"
	state := "complete"
	text := "graceful-exit diagnostic snapshot saved"
	if !result.Complete {
		state = "partial"
		text = "partial graceful-exit diagnostic snapshot saved"
	}
	if result.Path != "" {
		metadata["path"] = result.Path
	}
	if result.SHA256 != "" {
		metadata["sha256"] = result.SHA256
	}
	metadata["complete"] = strconv.FormatBool(result.Complete)
	metadata["error_count"] = strconv.Itoa(result.ErrorCount)
	metadata["bytes"] = strconv.FormatInt(result.Bytes, 10)
	if err != nil {
		lifecycle = "failed"
		state = "error"
		text = "graceful-exit diagnostic snapshot failed: " + err.Error()
	}
	runtime.PublishStructuredEvent(control.Event{
		Kind: "diagnostic.snapshot", Text: text, Lifecycle: lifecycle,
		State: state, Source: "primary-host", Target: "host.storage",
		Action: "persist", Metadata: metadata,
	})
	return err
}

func (recorder *hostSessionRecorder) logOnExit(output io.Writer) {
	result, err := recorder.save()
	if output == nil {
		return
	}
	if err != nil {
		fmt.Fprintln(output, "graceful-exit diagnostic snapshot:", err)
		return
	}
	fmt.Fprintf(
		output,
		"graceful-exit diagnostic snapshot: %s complete=%t errors=%d sha256=%s\n",
		result.Path, result.Complete, result.ErrorCount, result.SHA256,
	)
}
