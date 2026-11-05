//go:build windows

package dialogs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"rayanlamp.local/app/internal/commands"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/diagnostics"
)

func TestValueMapping(t *testing.T) {
	if got := indexOfValue([]string{"auto", "dark"}, " DARK "); got != 1 {
		t.Fatalf("indexOfValue = %d, want 1", got)
	}
	value, err := valueAt([]string{"auto", "dark"}, 1, "theme")
	if err != nil || value != "dark" {
		t.Fatalf("valueAt = %q, %v", value, err)
	}
	if _, err := valueAt([]string{"auto"}, -1, "theme"); err == nil {
		t.Fatal("valueAt accepted an invalid selection")
	}
	if len(viewModeLabels) != 2 ||
		len(viewModeValues) != 2 ||
		viewModeLabels[0] != "Compact lamp" ||
		viewModeValues[0] != config.ViewCompact ||
		viewModeLabels[1] != "Dashboard" ||
		viewModeValues[1] != config.ViewDashboard {
		t.Fatalf("unexpected view-mode choices: labels=%#v values=%#v", viewModeLabels, viewModeValues)
	}
	if got := indexOfValue(viewModeValues, " DASHBOARD "); got != 1 {
		t.Fatalf("dashboard index = %d, want 1", got)
	}
	wantBackdropLabels := []string{
		"Automatic",
		"Solid (no effect)",
		"Transparent (no backdrop)",
		"Blur",
		"Acrylic",
		"Mica",
		"Tabbed",
	}
	wantBackdropValues := []string{
		config.BackdropAuto,
		config.BackdropSolid,
		config.BackdropTransparent,
		config.BackdropBlur,
		config.BackdropAcrylic,
		config.BackdropMica,
		config.BackdropTabbed,
	}
	if strings.Join(backdropLabels, "|") != strings.Join(wantBackdropLabels, "|") ||
		strings.Join(backdropValues, "|") != strings.Join(wantBackdropValues, "|") {
		t.Fatalf("unexpected backdrop choices: labels=%#v values=%#v", backdropLabels, backdropValues)
	}
	if got := indexOfValue(backdropValues, " TABBED "); got != 6 {
		t.Fatalf("tabbed index = %d, want 6", got)
	}
	if strings.Join(renderBackendValues, "|") != strings.Join(
		[]string{
			config.RenderBackendAuto,
			config.RenderBackendDirectX,
			config.RenderBackendLegacy,
		},
		"|",
	) {
		t.Fatalf(
			"unexpected renderer choices: labels=%#v values=%#v",
			renderBackendLabels,
			renderBackendValues,
		)
	}
	if len(titleBarLabels) != 3 ||
		strings.Join(titleBarValues, "|") != strings.Join(
			[]string{
				config.TitleBarNone,
				config.TitleBarCustom,
				config.TitleBarNative,
			},
			"|",
		) {
		t.Fatalf(
			"unexpected title-bar choices: labels=%#v values=%#v",
			titleBarLabels,
			titleBarValues,
		)
	}
	if strings.Join(borderModeLabels, "|") !=
		"Automatic (resizable)|None (frameless)" ||
		strings.Join(borderModeValues, "|") !=
			strings.Join([]string{config.BorderAuto, config.BorderNone}, "|") {
		t.Fatalf(
			"unexpected border-mode choices: labels=%#v values=%#v",
			borderModeLabels,
			borderModeValues,
		)
	}
}

func TestDialogMaterialStopsAreOpaqueSafeAndOrdered(t *testing.T) {
	for _, dark := range []bool{false, true} {
		stops := dialogMaterialStops(dark)
		if len(stops) < 3 {
			t.Fatalf("dark=%v material stops = %d, want at least 3", dark, len(stops))
		}
		for index, stop := range stops {
			if index > 0 && stop.Offset <= stops[index-1].Offset {
				t.Fatalf(
					"dark=%v stop %d offset = %.2f, previous = %.2f",
					dark,
					index,
					stop.Offset,
					stops[index-1].Offset,
				)
			}
			if stop.Color == 0 {
				t.Fatalf(
					"dark=%v stop %d uses the full-client DWM black key",
					dark,
					index,
				)
			}
		}
		if stops[0].Offset != 0 || stops[len(stops)-1].Offset != 1 {
			t.Fatalf(
				"dark=%v material does not span the client: %#v",
				dark,
				stops,
			)
		}
	}
}

func TestDialogCardHeadingUsesReadableSemanticEmphasis(t *testing.T) {
	heading := dialogCardHeading("Device endpoint")
	if heading.Text != "Device endpoint" {
		t.Fatalf("heading text = %#v", heading.Text)
	}
	if !heading.Font.Bold ||
		heading.Font.Family != "Segoe UI" ||
		heading.Font.PointSize < 10 {
		t.Fatalf("heading font = %#v", heading.Font)
	}
}

func TestApplyAppearanceSettingsMapsAllTitleBarModes(t *testing.T) {
	for _, mode := range []string{
		config.TitleBarNone,
		config.TitleBarCustom,
		config.TitleBarNative,
	} {
		ui := config.Default().UI
		applyAppearanceSettings(&ui, appearanceSettingsValues{
			viewMode:          ui.ViewMode,
			theme:             ui.Theme,
			backdrop:          ui.Backdrop,
			roundedCorners:    ui.RoundedCorners,
			layeredForeground: ui.LayeredForeground,
			titleBarMode:      mode,
			borderMode:        ui.BorderMode,
			alwaysOnTop:       ui.AlwaysOnTop,
			closeToTray:       ui.CloseToTray,
			prewarmTrayMenu:   ui.PrewarmTrayMenu,
			startMinimized:    ui.StartMinimized,
			showTaskbar:       ui.ShowTaskbar,
			animations:        ui.Animations,
			magneticSnap:      ui.MagneticSnap,
			snapDistance:      ui.MagneticSnapDistance,
		})
		if ui.TitleBarMode != mode {
			t.Fatalf("mode %q mapped to %q", mode, ui.TitleBarMode)
		}
		if ui.ShowTitleBar != (mode == config.TitleBarCustom) {
			t.Fatalf(
				"mode %q produced legacy show_title_bar=%v",
				mode,
				ui.ShowTitleBar,
			)
		}
	}
}

func TestApplyAppearanceSettingsMapsBorderMode(t *testing.T) {
	for _, mode := range []string{config.BorderAuto, config.BorderNone} {
		ui := config.Default().UI
		applyAppearanceSettings(&ui, appearanceSettingsValues{
			viewMode:          ui.ViewMode,
			theme:             ui.Theme,
			backdrop:          ui.Backdrop,
			roundedCorners:    ui.RoundedCorners,
			layeredForeground: ui.LayeredForeground,
			titleBarMode:      config.TitleBarNone,
			borderMode:        mode,
			alwaysOnTop:       ui.AlwaysOnTop,
			closeToTray:       ui.CloseToTray,
			prewarmTrayMenu:   ui.PrewarmTrayMenu,
			startMinimized:    ui.StartMinimized,
			showTaskbar:       ui.ShowTaskbar,
			animations:        ui.Animations,
			magneticSnap:      ui.MagneticSnap,
			snapDistance:      ui.MagneticSnapDistance,
		})
		if ui.BorderMode != mode {
			t.Fatalf("border mode %q mapped to %q", mode, ui.BorderMode)
		}
	}
}

func TestApplyAppearanceSettingsMapsPrewarmTrayMenu(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		ui := config.Default().UI
		applyAppearanceSettings(&ui, appearanceSettingsValues{
			prewarmTrayMenu: enabled,
		})
		if ui.PrewarmTrayMenu != enabled {
			t.Fatalf(
				"tray-menu prewarm mapped to %v, want %v",
				ui.PrewarmTrayMenu,
				enabled,
			)
		}
	}
}

func TestSettingsTabIndexIsStableAndBounded(t *testing.T) {
	for index := 0; index < config.SettingsTabCount; index++ {
		if got := settingsTabIndex(index); got != index {
			t.Fatalf("settingsTabIndex(%d) = %d", index, got)
		}
	}
	for _, index := range []int{-1, config.SettingsTabCount, 99} {
		if got := settingsTabIndex(index); got != 0 {
			t.Fatalf("settingsTabIndex(%d) = %d, want 0", index, got)
		}
	}
}

func TestSettingsActionMnemonicsAreExplicitAndUniquePerSurface(t *testing.T) {
	groups := [][]string{
		{settingsSaveCaption, settingsCancelCaption},
		{
			settingsSaveCaption,
			settingsCancelCaption,
			settingsTestToastCaption,
			settingsTestSoundCaption,
		},
		{
			settingsSaveCaption,
			settingsCancelCaption,
			settingsBrowseOnCaption,
			settingsBrowseOffCaption,
			settingsBrowseMessageCaption,
			settingsBrowseFolderCaption,
		},
		{
			settingsSaveCaption,
			settingsCancelCaption,
			settingsRevealCaption,
			settingsCopyTokenCaption,
			settingsRegenerateCaption,
			settingsRegisterShellCaption,
			settingsRegisterCOMCaption,
		},
	}
	for _, captions := range groups {
		used := make(map[rune]string)
		for _, caption := range captions {
			mnemonic := testDialogMnemonic(caption)
			if mnemonic == 0 {
				t.Fatalf("settings action %q has no explicit mnemonic", caption)
			}
			mnemonic = []rune(strings.ToLower(string(mnemonic)))[0]
			if previous := used[mnemonic]; previous != "" {
				t.Fatalf(
					"settings actions %q and %q both use Alt+%s",
					previous,
					caption,
					strings.ToUpper(string(mnemonic)),
				)
			}
			used[mnemonic] = caption
		}
	}
}

func TestRegistrationPresentationShowsExactPortableState(t *testing.T) {
	status := commands.RegistrationStatus{
		Executable: `C:\Portable Apps\RayanLamp.exe`,
	}
	got := registrationPresentation(status, true, nil, "", true, true)
	if got.executable != status.Executable {
		t.Fatalf("executable = %q, want exact %q", got.executable, status.Executable)
	}
	if got.shellStatus != "Not registered (registration is opt-in)" ||
		got.comStatus != "Not registered (registration is opt-in)" {
		t.Fatalf(
			"portable statuses = shell:%q COM:%q",
			got.shellStatus,
			got.comStatus,
		)
	}
	if got.shellButton != settingsRegisterShellCaption ||
		got.comButton != settingsRegisterCOMCaption ||
		!got.shellEnabled || !got.comEnabled {
		t.Fatalf("portable registration actions = %+v", got)
	}
}

func TestRegistrationPresentationTracksOwnedAndBusyState(t *testing.T) {
	status := commands.RegistrationStatus{
		Executable:      `D:\Tools\RayanLamp.exe`,
		ShellRegistered: true,
		COMRegistered:   true,
	}
	got := registrationPresentation(
		status,
		true,
		nil,
		commands.MethodIntegrationShellUnregister,
		true,
		true,
	)
	if got.shellButton != settingsRemoveShellCaption ||
		got.comButton != settingsRemoveCOMCaption {
		t.Fatalf("owned action captions = %+v", got)
	}
	if got.shellEnabled || got.comEnabled {
		t.Fatalf("buttons remained enabled while mutation was active: %+v", got)
	}
	if !strings.Contains(got.shellStatus, "Updating") ||
		got.comStatus != "Registered to this executable" {
		t.Fatalf("busy presentation = %+v", got)
	}
}

func TestRegistrationPresentationRequiresExactExecutable(t *testing.T) {
	got := registrationPresentation(
		commands.RegistrationStatus{},
		true,
		nil,
		"",
		true,
		true,
	)
	if got.executable != "Not reported" ||
		got.shellEnabled || got.comEnabled ||
		!strings.Contains(got.shellStatus, "Exact current executable") {
		t.Fatalf("missing executable presentation = %+v", got)
	}
}

func TestRegistrationMethodIsExplicitForEachOwnedState(t *testing.T) {
	tests := []struct {
		name       string
		kind       settingsRegistrationKind
		status     commands.RegistrationStatus
		wantMethod string
		wantRemove bool
	}{
		{
			name:       "register shell",
			kind:       settingsRegistrationShell,
			wantMethod: commands.MethodIntegrationShellRegister,
		},
		{
			name:       "unregister shell",
			kind:       settingsRegistrationShell,
			status:     commands.RegistrationStatus{ShellRegistered: true},
			wantMethod: commands.MethodIntegrationShellUnregister,
			wantRemove: true,
		},
		{
			name:       "register COM",
			kind:       settingsRegistrationCOM,
			wantMethod: commands.MethodIntegrationCOMRegister,
		},
		{
			name:       "unregister COM",
			kind:       settingsRegistrationCOM,
			status:     commands.RegistrationStatus{COMRegistered: true},
			wantMethod: commands.MethodIntegrationCOMUnregister,
			wantRemove: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			method, remove := registrationMethod(test.kind, test.status)
			if method != test.wantMethod || remove != test.wantRemove {
				t.Fatalf(
					"registrationMethod = %q, %t; want %q, %t",
					method,
					remove,
					test.wantMethod,
					test.wantRemove,
				)
			}
		})
	}
}

func TestRegistrationStatusReadDoesNotInvokeMutation(t *testing.T) {
	statusCalls, actionCalls := 0, 0
	want := commands.RegistrationStatus{Executable: `C:\RayanLamp.exe`}
	actions := SettingsActions{
		RegistrationStatus: func() (commands.RegistrationStatus, error) {
			statusCalls++
			return want, nil
		},
		RegistrationAction: func(string) error {
			actionCalls++
			return nil
		},
	}
	got, err := actions.queryRegistrationStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || statusCalls != 1 || actionCalls != 0 {
		t.Fatalf(
			"status read = %+v, calls status:%d action:%d",
			got,
			statusCalls,
			actionCalls,
		)
	}
}

func TestRegistrationUnregisterPromptNamesExactExecutable(t *testing.T) {
	executable := `C:\Portable\RayanLamp.exe`
	title, instruction, content := registrationUnregisterPrompt(
		settingsRegistrationShell,
		executable,
	)
	if !strings.Contains(title, "shell") ||
		!strings.Contains(instruction, "current executable") ||
		!strings.Contains(content, executable) ||
		!strings.Contains(content, "another executable") {
		t.Fatalf(
			"unregister prompt = title:%q instruction:%q content:%q",
			title,
			instruction,
			content,
		)
	}
}

type testSettingsCodedError struct {
	code string
}

func (err testSettingsCodedError) Error() string {
	return err.code
}

func (err testSettingsCodedError) Code() string {
	return err.code
}

func TestRegistrationPartialFailureHasSpecificTitle(t *testing.T) {
	if got := registrationActionErrorTitle(testSettingsCodedError{"partial"}); !strings.Contains(strings.ToLower(got), "partly") {
		t.Fatalf("partial error title = %q", got)
	}
	if got := registrationActionErrorTitle(errors.New("boom")); got != "Windows registration failed" {
		t.Fatalf("ordinary error title = %q", got)
	}
}

func testDialogMnemonic(caption string) rune {
	runes := []rune(caption)
	for index := 0; index+1 < len(runes); index++ {
		if runes[index] != '&' {
			continue
		}
		if runes[index+1] == '&' {
			index++
			continue
		}
		return runes[index+1]
	}
	return 0
}

func TestParseDuration(t *testing.T) {
	got, err := parseDuration(" 1m30s ", "Timeout")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value() != 90*time.Second {
		t.Fatalf("parseDuration = %s", got)
	}
	if _, err := parseDuration("eventually", "Timeout"); err == nil {
		t.Fatal("parseDuration accepted invalid text")
	}
}

func TestCanonicalizeHotkeys(t *testing.T) {
	hotkeys := config.HotkeysConfig{
		Toggle: " control + alt + l ",
		On:     "SHIFT+F3",
	}
	if err := canonicalizeHotkeys(&hotkeys); err != nil {
		t.Fatal(err)
	}
	if hotkeys.Toggle != "Ctrl+Alt+L" || hotkeys.On != "Shift+F3" {
		t.Fatalf("canonicalized hotkeys = %#v", hotkeys)
	}
	hotkeys.Off = "Ctrl+NoSuchKey"
	if err := canonicalizeHotkeys(&hotkeys); err == nil {
		t.Fatal("canonicalizeHotkeys accepted an unknown key")
	}
}

func TestSettingsPreviewRollsBackOnClose(t *testing.T) {
	original := config.Default()
	updated := original
	updated.UI.Theme = "dark"

	var applied []config.Config
	controller := newSettingsPreviewController(
		original,
		[]SettingsPreviewFunc{func(candidate config.Config) {
			applied = append(applied, candidate)
		}},
	)
	controller.Preview(updated)
	controller.Close()

	if len(applied) != 2 {
		t.Fatalf("preview callback count = %d, want 2", len(applied))
	}
	if !reflect.DeepEqual(applied[0], updated) {
		t.Fatal("first callback did not contain the preview configuration")
	}
	if !reflect.DeepEqual(applied[1], original) {
		t.Fatal("closing did not restore the original configuration")
	}
}

func TestSettingsTabNavigationDoesNotRunFullLivePreview(t *testing.T) {
	original := config.Default()
	original.UI.SettingsTab = 0
	applied := 0
	controller := newSettingsPreviewController(
		original,
		[]SettingsPreviewFunc{func(config.Config) {
			applied++
		}},
	)

	navigated := original
	navigated.UI.SettingsTab = 4
	controller.Preview(navigated)
	if applied != 0 {
		t.Fatalf("tab-only navigation invoked full live preview %d times", applied)
	}
	if controller.current.UI.SettingsTab != 4 {
		t.Fatalf(
			"current settings tab = %d, want 4",
			controller.current.UI.SettingsTab,
		)
	}
	if controller.previewed {
		t.Fatal("tab-only navigation marked the runtime configuration previewed")
	}

	edited := navigated
	edited.UI.AlwaysOnTop = !edited.UI.AlwaysOnTop
	controller.Preview(edited)
	if applied != 1 {
		t.Fatalf("real settings edit applied %d times, want 1", applied)
	}
}

func TestSettingsPreviewCommitDoesNotRollBack(t *testing.T) {
	original := config.Default()
	updated := original
	updated.UI.Backdrop = "mica"

	var applied []config.Config
	controller := newSettingsPreviewController(
		original,
		[]SettingsPreviewFunc{func(candidate config.Config) {
			applied = append(applied, candidate)
		}},
	)
	controller.Commit(updated)
	controller.Close()

	if len(applied) != 1 || !reflect.DeepEqual(applied[0], updated) {
		t.Fatalf("committed callbacks = %#v, want one updated configuration", applied)
	}
}

func TestSettingsPreviewResetAdoptsExternalBaseline(t *testing.T) {
	original := config.Default()
	previewed := original
	previewed.UI.Theme = "dark"
	external := original
	external.UI.Theme = "light"

	var applied []config.Config
	controller := newSettingsPreviewController(
		original,
		[]SettingsPreviewFunc{func(candidate config.Config) {
			applied = append(applied, candidate)
		}},
	)
	controller.Preview(previewed)
	controller.Reset(external)
	controller.Close()

	if len(applied) != 1 || applied[0].UI.Theme != "dark" {
		t.Fatalf("reset replayed a stale baseline: %#v", applied)
	}
	if controller.original.UI.Theme != "light" ||
		controller.current.UI.Theme != "light" {
		t.Fatalf("external baseline was not adopted: %+v", controller)
	}
}

func TestSettingsNotificationTestActionsAreIndependent(t *testing.T) {
	var toastCalls, soundCalls int
	toastErr := errors.New("toast unavailable")
	actions := SettingsActions{
		TestToast: func() error {
			toastCalls++
			return toastErr
		},
		TestSound: func() error {
			soundCalls++
			return nil
		},
	}

	if err := actions.runTestToast(); !errors.Is(err, toastErr) {
		t.Fatalf("toast error = %v, want %v", err, toastErr)
	}
	if toastCalls != 1 || soundCalls != 0 {
		t.Fatalf(
			"toast action calls = toast:%d sound:%d, want 1 and 0",
			toastCalls,
			soundCalls,
		)
	}
	if err := actions.runTestSound(); err != nil {
		t.Fatalf("sound action: %v", err)
	}
	if toastCalls != 1 || soundCalls != 1 {
		t.Fatalf(
			"sound action calls = toast:%d sound:%d, want 1 and 1",
			toastCalls,
			soundCalls,
		)
	}
}

func TestAllowedDiagnosticEndpoints(t *testing.T) {
	normal := allowedDiagnosticEndpoints(config.DiagnosticsConfig{
		SafeEndpoints: []string{"/config"},
	})
	if containsString(normal, "/config") {
		t.Fatal("configured unsafe endpoint leaked into normal allowlist")
	}
	if containsString(normal, "/pu") {
		t.Fatal("retired PU endpoint visible")
	}
	if containsString(normal, "/datetime/get") {
		t.Fatal("PU-backed datetime endpoint visible")
	}

	expert := allowedDiagnosticEndpoints(config.DiagnosticsConfig{
		ExpertEnabled:   true,
		ExpertEndpoints: []string{"/config"},
	})
	if !containsString(expert, "/info") || containsString(expert, "/pu") {
		t.Fatalf("expert endpoints = %#v", expert)
	}
	if containsString(expert, "/config") {
		t.Fatal("configured unsafe endpoint leaked into expert allowlist")
	}
}

func TestFormatParsedDiagnosticResultUsesRedactedFieldsAndExplicitRaw(t *testing.T) {
	document := diagnostics.Parse(
		diagnostics.EndpointInfo,
		[]byte("hostname: lamp\napi token: secret-value\n"),
		time.Now(),
	)
	result := diagnostics.EndpointResult{
		Document:    document,
		LastAttempt: document.FetchedAt,
		LastSuccess: document.FetchedAt,
	}
	parsed := formatParsedDiagnosticResult(diagnostics.EndpointInfo, result, false)
	if !strings.Contains(parsed, "Hostname: lamp") ||
		!strings.Contains(parsed, "[redacted]") ||
		strings.Contains(parsed, "secret-value") {
		t.Fatalf("parsed output = %s", parsed)
	}
	raw := formatParsedDiagnosticResult(diagnostics.EndpointInfo, result, true)
	if !strings.Contains(raw, "api token: secret-value") {
		t.Fatalf("explicit native raw output lost source response: %s", raw)
	}
}

func TestDiagnosticWindowsAndBuildSummaryIncludesRenderingStatus(t *testing.T) {
	got := diagnosticWindowsAndBuildSummary(
		"Windows NT: 10.0.26100\r\nDWM composition: enabled",
		"Requested backend: auto\nActive backend: DirectX hardware",
	)
	for _, want := range []string{
		"Windows NT: 10.0.26100",
		"\r\n\r\nRendering\r\n",
		"Requested backend: auto\r\nActive backend: DirectX hardware",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic summary %q does not contain %q", got, want)
		}
	}

	if got := diagnosticWindowsAndBuildSummary("Windows", " \t\r\n"); !strings.Contains(got, "Rendering\r\nNot reported") {
		t.Fatalf("empty rendering summary = %q", got)
	}
}

func TestFormatDiagnosticPayload(t *testing.T) {
	got := formatDiagnosticPayload(
		"/info",
		[]byte("{\"state\":\"ON\",\"nested\":{\"value\":1}}\x00"),
		time.Date(2026, 7, 23, 12, 0, 0, 0, time.Local),
	)
	if !strings.Contains(got, `"state": "ON"`) {
		t.Fatalf("JSON was not formatted:\n%s", got)
	}
	if strings.ContainsRune(got, '\x00') || !strings.Contains(got, "␀") {
		t.Fatalf("NUL was not made safe:\n%s", got)
	}
	if !strings.Contains(got, "Endpoint: /info") {
		t.Fatalf("endpoint missing:\n%s", got)
	}
}

func TestFormatDiagnosticPayloadIsBounded(t *testing.T) {
	payload := make([]byte, maxDiagnosticOutput+64)
	for index := range payload {
		payload[index] = 'x'
	}
	got := formatDiagnosticPayload("/info", payload, time.Now())
	if !strings.Contains(got, "display truncated") {
		t.Fatal("oversized diagnostic response was not marked truncated")
	}
	if len(got) > maxDiagnosticOutput+1024 {
		t.Fatalf("formatted diagnostic output is unexpectedly large: %d", len(got))
	}
}

func TestCallDiagnosticFetchRecoversPanic(t *testing.T) {
	_, err := callDiagnosticFetch(
		context.Background(),
		func(context.Context, string) ([]byte, error) {
			panic("broken fetcher")
		},
		"/info",
	)
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("panic error = %v", err)
	}
}

func TestFormatDiagnosticErrorRecognizesTimeout(t *testing.T) {
	got := formatDiagnosticError("/info", context.DeadlineExceeded, time.Now())
	if !strings.Contains(got, "configured lamp request timeout") {
		t.Fatalf("timeout text = %q", got)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
