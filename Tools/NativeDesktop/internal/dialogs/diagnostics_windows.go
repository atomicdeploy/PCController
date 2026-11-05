//go:build windows

package dialogs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lxn/walk"
	decl "github.com/lxn/walk/declarative"

	"rayanlamp.local/app/internal/buildinfo"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/diagnostics"
	"rayanlamp.local/app/internal/i18n"
	"rayanlamp.local/app/internal/model"
	"rayanlamp.local/app/internal/winfx"
)

const maxDiagnosticOutput = 1 << 20

type diagnosticsController struct {
	dialog        *walk.Dialog
	cfg           config.Config
	source        DiagnosticsSource
	endpoints     []string
	endpointCombo *walk.ComboBox
	refreshButton *walk.PushButton
	status        *walk.Label
	output        *walk.TextEdit
	lampSummary   *walk.TextEdit
	rawView       *walk.CheckBox
	snapshot      diagnostics.Snapshot
	lampSnapshot  model.Snapshot
	localizer     i18n.Localizer

	closed   atomic.Bool
	sequence atomic.Uint64
	cancelMu sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
	unsub    func()
}

type endpointFetchResult struct {
	serial   uint64
	endpoint string
	snapshot diagnostics.Snapshot
	err      error
	finished time.Time
}

// DiagnosticsSource is implemented by appcore.Core. Keeping the UI against a
// narrow interface lets the native dialog and embedded web UI consume the same
// parsed, cached device metadata without giving either one arbitrary network
// access.
type DiagnosticsSource interface {
	DiagnosticsSnapshot() diagnostics.Snapshot
	RefreshDiagnostics(
		context.Context,
		...string,
	) (diagnostics.Snapshot, error)
	SubscribeDiagnostics(
		int,
	) (<-chan diagnostics.Snapshot, func())
}

// ShowDiagnostics displays current connectivity and Windows capability state,
// and permits asynchronous GETs only to the firmware-audited endpoint
// allowlist. Parsed and raw views are sourced from the shared Core cache.
func ShowDiagnostics(
	owner walk.Form,
	cfg config.Config,
	snapshot model.Snapshot,
	source DiagnosticsSource,
	windowChanged func(*walk.Dialog, func(config.Config)),
	renderingSummary string,
	icons ...walk.Image,
) error {
	working := cfg
	if err := working.Normalize(); err != nil {
		return fmt.Errorf("normalize diagnostics configuration: %w", err)
	}

	endpoints := allowedDiagnosticEndpoints(working.Diagnostics)
	if len(endpoints) == 0 {
		return fmt.Errorf("no diagnostic endpoints are available")
	}
	localizer := dialogLocalizer(working)
	_ = winfx.EnsurePersianFont()
	localeFonts := make(dialogFontCache)
	defer localeFonts.dispose()

	osInfo, osErr := winfx.GetOSInfo()
	osSummary := localizedDiagnosticOSSummary(localizer, osInfo, osErr)
	osSummary = localizedDiagnosticWindowsAndBuildSummary(
		localizer,
		osSummary,
		renderingSummary,
	)
	diagnosticSnapshot := diagnostics.NewSnapshot(working.Lamp.BaseURL)
	if source != nil {
		diagnosticSnapshot = source.DiagnosticsSnapshot()
	}
	stateSummary := localizedDiagnosticStateAndDeviceSummary(
		localizer,
		working,
		snapshot,
		diagnosticSnapshot,
	)
	initialOutput := localizedDiagnosticIntroduction(localizer, false)

	controller := &diagnosticsController{
		cfg:          working,
		source:       source,
		endpoints:    endpoints,
		snapshot:     diagnosticSnapshot,
		lampSnapshot: snapshot,
		localizer:    localizer,
		done:         make(chan struct{}),
	}
	var dialog *walk.Dialog
	var closeButton *walk.PushButton
	defaultEndpoint := indexOfValue(endpoints, "/info")

	spec := decl.Dialog{
		AssignTo:           &dialog,
		Title:              dialogText(localizer, "Rayan Lamp Diagnostics"),
		Size:               decl.Size{Width: 800, Height: 650},
		MinSize:            decl.Size{Width: 700, Height: 540},
		Layout:             decl.VBox{Margins: decl.Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 8},
		Font:               decl.Font{Family: dialogFontFamily(localizer), PointSize: 10},
		RightToLeftLayout:  localizer.IsRTL(),
		RightToLeftReading: localizer.IsRTL(),
		DefaultButton:      &controller.refreshButton,
		CancelButton:       &closeButton,
		Children: []decl.Widget{
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 8},
				Children: []decl.Widget{
					decl.Composite{
						Border:        true,
						StretchFactor: 1,
						Layout:        decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 10}, Spacing: 6},
						Children: []decl.Widget{
							dialogCardHeading(
								dialogText(localizer, "Lamp and connection"),
								localizer,
							),
							decl.TextEdit{
								AssignTo:      &controller.lampSummary,
								Text:          stateSummary,
								ReadOnly:      true,
								VScroll:       true,
								MinSize:       decl.Size{Width: 300, Height: 145},
								StretchFactor: 1,
							},
						},
					},
					decl.Composite{
						Border:        true,
						StretchFactor: 1,
						Layout:        decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 10}, Spacing: 6},
						Children: []decl.Widget{
							dialogCardHeading(
								dialogText(localizer, "Windows and build"),
								localizer,
							),
							decl.TextEdit{
								Text:          osSummary,
								ReadOnly:      true,
								VScroll:       true,
								MinSize:       decl.Size{Width: 300, Height: 145},
								StretchFactor: 1,
							},
						},
					},
				},
			},
			decl.Composite{
				Border:        true,
				StretchFactor: 1,
				Layout:        decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 10}, Spacing: 7},
				Children: []decl.Widget{
					dialogCardHeading(
						dialogText(localizer, "Device endpoint"),
						localizer,
					),
					decl.Composite{
						Layout: decl.HBox{MarginsZero: true, Spacing: 8},
						Children: []decl.Widget{
							decl.Label{Text: dialogText(localizer, "Endpoint:")},
							decl.ComboBox{
								AssignTo:      &controller.endpointCombo,
								Model:         endpoints,
								CurrentIndex:  defaultEndpoint,
								StretchFactor: 1,
								OnCurrentIndexChanged: func() {
									controller.renderSelected()
								},
							},
							decl.PushButton{
								AssignTo: &controller.refreshButton,
								Text:     dialogText(localizer, "&Refresh"),
								MinSize:  decl.Size{Width: 104},
								Enabled:  source != nil,
								OnClicked: func() {
									controller.startRefresh()
								},
							},
						},
					},
					decl.Label{
						AssignTo: &controller.status,
						Text: localizedInitialDiagnosticStatus(
							localizer,
							source != nil,
							false,
						),
					},
					decl.TextEdit{
						AssignTo:      &controller.output,
						Text:          initialOutput,
						ReadOnly:      true,
						HScroll:       true,
						VScroll:       true,
						MinSize:       decl.Size{Width: 620, Height: 220},
						StretchFactor: 1,
					},
					decl.Composite{
						Layout: decl.HBox{MarginsZero: true, Spacing: 8},
						Children: []decl.Widget{
							decl.CheckBox{
								AssignTo: &controller.rawView,
								Text:     dialogText(localizer, "Show raw response"),
								OnCheckedChanged: func() {
									controller.renderSelected()
								},
							},
							decl.PushButton{
								Text:    dialogText(localizer, "&Copy output"),
								MinSize: decl.Size{Width: 122},
								OnClicked: func() {
									controller.copyOutput()
								},
							},
							decl.PushButton{
								Text:    dialogText(localizer, "&Save output…"),
								MinSize: decl.Size{Width: 132},
								OnClicked: func() {
									controller.saveOutput()
								},
							},
							decl.HSpacer{},
							decl.PushButton{
								AssignTo: &closeButton,
								Text:     dialogText(localizer, "C&lose"),
								MinSize:  decl.Size{Width: 94},
								OnClicked: func() {
									dialog.Cancel()
								},
							},
						},
					},
				},
			},
		},
	}

	if err := spec.Create(owner); err != nil {
		return fmt.Errorf("create diagnostics dialog: %w", err)
	}
	defer dialog.Dispose()
	for _, icon := range icons {
		if icon == nil {
			continue
		}
		if err := dialog.SetIcon(icon); err != nil {
			return fmt.Errorf("set diagnostics dialog icon: %w", err)
		}
		break
	}
	themeSession := newDialogThemeSession(dialog)
	defer themeSession.Dispose()

	controller.dialog = dialog
	localeFonts.apply(dialog, localizer)
	applyDialogTextDirection(
		dialog,
		localizer,
		controller.endpointCombo,
		controller.output,
	)
	dialog.Disposing().Attach(controller.close)
	themeSession.Apply(working.UI.Theme, working.UI.Animations)
	if err := themeSession.WatchSystemChanges(nil); err != nil {
		return fmt.Errorf("watch diagnostics accessibility theme: %w", err)
	}
	if windowChanged != nil {
		windowChanged(dialog, func(updated config.Config) {
			if dialog == nil || dialog.IsDisposed() {
				return
			}
			themeSession.Apply(
				updated.UI.Theme,
				updated.UI.Animations,
			)
			controller.cfg = updated
			controller.localizer = dialogLocalizer(updated)
			localeFonts.apply(dialog, controller.localizer)
			_ = dialog.SetTitle(dialogText(
				controller.localizer,
				"Rayan Lamp Diagnostics",
			))
			applyDialogTextDirection(
				dialog,
				controller.localizer,
				controller.endpointCombo,
				controller.output,
			)
			controller.renderSelected()
		})
		defer windowChanged(nil, nil)
	}
	controller.startSubscription()
	controller.renderSelected()
	if source != nil {
		if result, ok := diagnosticSnapshot.Endpoints[diagnostics.EndpointInfo]; !ok || result.LastSuccess.IsZero() || result.Stale {
			controller.startRefresh()
		}
	}
	dialog.Run()
	return nil
}

func (controller *diagnosticsController) startRefresh() {
	if controller == nil || controller.closed.Load() || controller.dialog == nil {
		return
	}
	if controller.source == nil {
		_ = controller.status.SetText(dialogText(
			controller.localizer,
			"Endpoint fetching is unavailable in this session.",
		))
		return
	}
	index := controller.endpointCombo.CurrentIndex()
	if index < 0 || index >= len(controller.endpoints) {
		showValidationError(controller.dialog, "Choose an endpoint", fmt.Errorf("select one of the listed diagnostic endpoints"))
		return
	}
	endpoint := controller.endpoints[index]
	if err := controller.cfg.Diagnostics.ValidateRequest(http.MethodGet, endpoint); err != nil {
		showValidationError(controller.dialog, "Endpoint not permitted", err)
		return
	}

	serial := controller.sequence.Add(1)
	controller.cancelActive()
	timeout := controller.cfg.Lamp.RequestTimeout.Value()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	controller.cancelMu.Lock()
	controller.cancel = cancel
	controller.cancelMu.Unlock()

	controller.endpointCombo.SetEnabled(false)
	controller.refreshButton.SetEnabled(false)
	_ = controller.status.SetText(localizedDiagnosticLoading(
		controller.localizer,
		endpoint,
	))
	_ = controller.output.SetText(localizedDiagnosticWaiting(
		controller.localizer,
		endpoint,
	))

	go func() {
		defer cancel()
		snapshot, fetchErr := controller.source.RefreshDiagnostics(ctx, endpoint)
		result := endpointFetchResult{
			serial:   serial,
			endpoint: endpoint,
			snapshot: snapshot,
			err:      fetchErr,
			finished: time.Now(),
		}
		controller.completeRefresh(result)
	}()
}

func (controller *diagnosticsController) completeRefresh(result endpointFetchResult) {
	if controller == nil || controller.closed.Load() || result.serial != controller.sequence.Load() {
		return
	}
	defer func() {
		_ = recover()
	}()
	controller.dialog.Synchronize(func() {
		if controller.closed.Load() || result.serial != controller.sequence.Load() {
			return
		}
		controller.endpointCombo.SetEnabled(true)
		controller.refreshButton.SetEnabled(controller.source != nil)
		controller.snapshot = result.snapshot
		controller.renderSelected()
		if result.err != nil {
			_ = controller.status.SetText(localizedDiagnosticFinishedStatus(
				controller.localizer,
				false,
				result.endpoint,
				result.finished,
			))
		} else {
			_ = controller.status.SetText(localizedDiagnosticFinishedStatus(
				controller.localizer,
				true,
				result.endpoint,
				result.finished,
			))
		}
	})
}

func (controller *diagnosticsController) startSubscription() {
	if controller == nil || controller.source == nil || controller.closed.Load() {
		return
	}
	updates, unsubscribe := controller.source.SubscribeDiagnostics(4)
	controller.unsub = unsubscribe
	go func() {
		for {
			select {
			case <-controller.done:
				return
			case snapshot, ok := <-updates:
				if !ok {
					return
				}
				controller.acceptSnapshot(snapshot)
			}
		}
	}()
}

func (controller *diagnosticsController) acceptSnapshot(
	snapshot diagnostics.Snapshot,
) {
	if controller == nil || controller.closed.Load() || controller.dialog == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	controller.dialog.Synchronize(func() {
		if controller.closed.Load() {
			return
		}
		controller.snapshot = snapshot
		controller.renderSelected()
	})
}

func (controller *diagnosticsController) renderSelected() {
	if controller == nil || controller.closed.Load() {
		return
	}
	if controller.lampSummary != nil {
		_ = controller.lampSummary.SetText(localizedDiagnosticStateAndDeviceSummary(
			controller.localizer,
			controller.cfg,
			controller.lampSnapshot,
			controller.snapshot,
		))
	}
	if controller.output == nil || controller.endpointCombo == nil {
		return
	}
	index := controller.endpointCombo.CurrentIndex()
	if index < 0 || index >= len(controller.endpoints) {
		_ = controller.output.SetText(localizedDiagnosticIntroduction(
			controller.localizer,
			false,
		))
		return
	}
	endpoint := controller.endpoints[index]
	result, ok := controller.snapshot.Endpoints[endpoint]
	if !ok {
		_ = controller.output.SetText(localizedDiagnosticNoCache(
			controller.localizer,
			endpoint,
		))
		return
	}
	raw := controller.rawView != nil && controller.rawView.Checked()
	_ = controller.output.SetText(localizedFormatParsedDiagnosticResult(
		controller.localizer,
		endpoint,
		result,
		raw,
	))
}

func formatParsedDiagnosticResult(
	endpoint string,
	result diagnostics.EndpointResult,
	raw bool,
) string {
	document := result.Document
	fetched := result.LastSuccess
	if fetched.IsZero() {
		fetched = result.LastAttempt
	}
	var output strings.Builder
	fmt.Fprintf(&output, "Endpoint: %s\r\n", endpoint)
	if !fetched.IsZero() {
		fmt.Fprintf(
			&output,
			"Fetched: %s\r\n",
			fetched.Local().Format("2006-01-02 15:04:05"),
		)
	}
	if document.Bytes != 0 || !result.LastSuccess.IsZero() {
		fmt.Fprintf(&output, "Response size: %d bytes\r\n", document.Bytes)
	}
	if result.Stale {
		output.WriteString("Cache: stale; an automatic refresh is pending\r\n")
	}
	if result.Error != "" {
		fmt.Fprintf(&output, "Latest request: %s\r\n", cleanDisplayText(result.Error, 1000))
	}
	output.WriteString("\r\n")

	if result.LastSuccess.IsZero() {
		output.WriteString("No successful response has been cached.")
		return output.String()
	}
	if raw {
		body := document.Raw
		truncated := len([]byte(body)) > maxDiagnosticOutput
		if truncated {
			body = truncateUTF8(body, maxDiagnosticOutput)
		}
		body = cleanDiagnosticBody(body)
		if body == "" {
			body = "(The endpoint returned an empty response.)"
		}
		output.WriteString(body)
		if truncated {
			fmt.Fprintf(
				&output,
				"\r\n\r\n(Display truncated to %d bytes.)",
				maxDiagnosticOutput,
			)
		}
		return output.String()
	}

	if len(document.Fields) == 0 {
		output.WriteString("(No structured fields were recognized. Enable raw view to inspect the response.)")
	} else {
		for _, field := range document.Fields {
			fmt.Fprintf(&output, "%s: %s\r\n", field.Label, field.Display)
		}
	}
	if len(document.Warnings) != 0 {
		output.WriteString("\r\nParser notes:\r\n")
		for _, warning := range document.Warnings {
			fmt.Fprintf(&output, "• %s\r\n", cleanDisplayText(warning, 500))
		}
	}
	return strings.TrimRight(output.String(), "\r\n")
}

func localizedFormatParsedDiagnosticResult(
	localizer i18n.Localizer,
	endpoint string,
	result diagnostics.EndpointResult,
	raw bool,
) string {
	return localizeDiagnosticReport(
		localizer,
		formatParsedDiagnosticResult(endpoint, result, raw),
	)
}

func localizedDiagnosticStateAndDeviceSummary(
	localizer i18n.Localizer,
	cfg config.Config,
	lampSnapshot model.Snapshot,
	deviceSnapshot diagnostics.Snapshot,
) string {
	return localizeDiagnosticReport(
		localizer,
		diagnosticStateAndDeviceSummary(cfg, lampSnapshot, deviceSnapshot),
	)
}

func localizedDiagnosticOSSummary(
	localizer i18n.Localizer,
	info winfx.OSInfo,
	probeErr error,
) string {
	return localizeDiagnosticReport(
		localizer,
		diagnosticOSSummary(info, probeErr),
	)
}

func localizedDiagnosticWindowsAndBuildSummary(
	localizer i18n.Localizer,
	osSummary string,
	renderingSummary string,
) string {
	return localizeDiagnosticReport(
		localizer,
		diagnosticWindowsAndBuildSummary(osSummary, renderingSummary),
	)
}

func localizedDiagnosticIntroduction(
	localizer i18n.Localizer,
	expert bool,
) string {
	if !localizer.IsRTL() {
		return diagnosticIntroduction(expert)
	}
	message := "یک نقطهٔ پایانی خواندنی و بررسی‌شده را انتخاب کنید و «تازه‌سازی» را بزنید.\r\n\r\n" +
		"فقط نقطه‌های پایانی ثابت GET که در میان‌افزار چراغ شناسایی شده‌اند در دسترس‌اند. " +
		"این پنجره هرگز مسیرهای تنظیمات، به‌روزرسانی، ورود، خرابی یا مسیر دلخواه را درخواست نمی‌کند."
	if expert {
		message += "\r\n\r\nعیب‌یابی‌های PU/debug حذف شده‌اند، چون به دستگاه دیگری تعلق دارند."
	}
	return message
}

func localizedInitialDiagnosticStatus(
	localizer i18n.Localizer,
	available bool,
	expert bool,
) string {
	if !localizer.IsRTL() {
		return initialDiagnosticStatus(available, expert)
	}
	if !available {
		return "دریافت نقطهٔ پایانی در دسترس نیست؛ نمای فوری و اطلاعات ویندوز همچنان قابل مشاهده‌اند."
	}
	return "آماده است. فرادادهٔ ذخیره‌شدهٔ دستگاه خودکار تازه می‌شود؛ برای دریافت فوری «تازه‌سازی» را بزنید."
}

func localizedDiagnosticLoading(
	localizer i18n.Localizer,
	endpoint string,
) string {
	if localizer.IsRTL() {
		return "در حال دریافت " + endpoint + "…"
	}
	return "Loading " + endpoint + "…"
}

func localizedDiagnosticWaiting(
	localizer i18n.Localizer,
	endpoint string,
) string {
	if localizer.IsRTL() {
		return "در انتظار پاسخ از " + endpoint + "…"
	}
	return "Waiting for a response from " + endpoint + "…"
}

func localizedDiagnosticFinishedStatus(
	localizer i18n.Localizer,
	ok bool,
	endpoint string,
	finished time.Time,
) string {
	at := finished.Local().Format("15:04:05")
	if localizer.IsRTL() {
		if ok {
			return endpoint + " در ساعت " + at + " دریافت شد"
		}
		return "درخواست در ساعت " + at + " ناموفق بود"
	}
	if ok {
		return "Loaded " + endpoint + " at " + at
	}
	return "Request failed at " + at
}

func localizedDiagnosticNoCache(
	localizer i18n.Localizer,
	endpoint string,
) string {
	if localizer.IsRTL() {
		return "برای " + endpoint +
			" پاسخ ذخیره‌شده‌ای وجود ندارد.\r\n\r\nبرای درخواست آن «تازه‌سازی» را انتخاب کنید."
	}
	return "No cached response is available for " + endpoint +
		".\r\n\r\nChoose Refresh to request it now."
}

var persianDiagnosticLabels = map[string]string{
	"State":                 "وضعیت",
	"HTTP":                  "HTTP",
	"WebSocket":             "WebSocket",
	"Device":                "دستگاه",
	"Last connected":        "آخرین اتصال",
	"Last changed":          "آخرین تغییر",
	"Pending command":       "فرمان در انتظار",
	"Last source":           "آخرین منبع",
	"Reconnect attempt":     "تلاش اتصال دوباره",
	"Last event":            "آخرین رویداد",
	"Last error":            "آخرین خطا",
	"Hostname":              "نام میزبان",
	"Device name":           "نام دستگاه",
	"Firmware":              "میان‌افزار",
	"Wi-Fi":                 "وای‌فای",
	"SSID":                  "SSID",
	"Lamp address":          "نشانی چراغ",
	"Signal":                "قدرت سیگنال",
	"Device uptime":         "زمان کار دستگاه",
	"Metadata updated":      "به‌روزرسانی فراداده",
	"Device metadata":       "فرادادهٔ دستگاه",
	"Rayan Lamp":            "چراغ رایان",
	"Commit":                "کامیت",
	"Build date":            "تاریخ ساخت",
	"Windows NT":            "Windows NT",
	"Windows 11":            "Windows 11",
	"DWM composition":       "ترکیب‌بندی DWM",
	"Transparency":          "شفافیت",
	"High contrast":         "کنتراست بالا",
	"App color preference":  "ترجیح رنگ برنامه",
	"System backdrop":       "پس‌زمینهٔ سیستم",
	"Accent acrylic":        "اکریلیک Accent",
	"Capability probe":      "بررسی قابلیت‌ها",
	"Endpoint":              "نقطهٔ پایانی",
	"Fetched":               "زمان دریافت",
	"Response size":         "اندازهٔ پاسخ",
	"Cache":                 "حافظهٔ نهان",
	"Latest request":        "آخرین درخواست",
	"Latest request status": "وضعیت آخرین درخواست",
}

var persianDiagnosticValues = map[string]string{
	"reachable":    "در دسترس",
	"offline":      "آفلاین",
	"connected":    "متصل",
	"disconnected": "قطع",
	"enabled":      "فعال",
	"disabled":     "غیرفعال",
	"supported":    "پشتیبانی‌شده",
	"unavailable":  "در دسترس نیست",
	"yes":          "بله",
	"no":           "خیر",
	"dark":         "تیره",
	"light":        "روشن",
	"unknown":      "نامشخص",
	"complete":     "کامل",
	"None":         "هیچ‌کدام",
	"Unknown":      "نامشخص",
}

func localizeDiagnosticReport(
	localizer i18n.Localizer,
	report string,
) string {
	if !localizer.IsRTL() {
		return report
	}
	lines := strings.Split(report, "\r\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "Rendering":
			lines[index] = "رندر"
			continue
		case "Parser notes:":
			lines[index] = "یادداشت‌های تجزیه‌گر:"
			continue
		case "No successful response has been cached.":
			lines[index] = "هیچ پاسخ موفقی ذخیره نشده است."
			continue
		case "(The endpoint returned an empty response.)":
			lines[index] = "(نقطهٔ پایانی پاسخ خالی برگرداند.)"
			continue
		case "(No structured fields were recognized. Enable raw view to inspect the response.)":
			lines[index] = "(فیلد ساخت‌یافته‌ای شناخته نشد. برای بررسی پاسخ، نمایش خام را فعال کنید.)"
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		label := strings.TrimSpace(line[:colon])
		translated, ok := persianDiagnosticLabels[label]
		if !ok {
			continue
		}
		value := strings.TrimSpace(line[colon+1:])
		if localized, ok := persianDiagnosticValues[value]; ok {
			value = localized
		}
		lines[index] = translated + ": " + value
	}
	return strings.Join(lines, "\r\n")
}

func truncateUTF8(value string, bytesLimit int) string {
	if bytesLimit <= 0 || len(value) <= bytesLimit {
		return value
	}
	end := bytesLimit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func (controller *diagnosticsController) copyOutput() {
	if controller == nil || controller.output == nil {
		return
	}
	if err := walk.Clipboard().SetText(controller.output.Text()); err != nil {
		showValidationError(controller.dialog, "Could not copy diagnostics", err)
		return
	}
	_ = controller.status.SetText(dialogText(
		controller.localizer,
		"Copied diagnostic output to the clipboard.",
	))
}

func (controller *diagnosticsController) saveOutput() {
	if controller == nil || controller.output == nil || controller.dialog == nil {
		return
	}
	dialog := walk.FileDialog{
		Title: dialogText(
			controller.localizer,
			"Save Rayan Lamp diagnostics",
		),
		FilePath: "rayanlamp-diagnostics.txt",
		Filter: dialogText(
			controller.localizer,
			"Text files (*.txt)|*.txt|All files (*.*)|*.*",
		),
		FilterIndex: 1,
	}
	accepted, err := dialog.ShowSave(controller.dialog)
	if err != nil {
		showValidationError(controller.dialog, "Could not choose a diagnostics file", err)
		return
	}
	if !accepted {
		return
	}
	path := filepath.Clean(dialog.FilePath)
	if filepath.Ext(path) == "" {
		path += ".txt"
	}
	if err := os.WriteFile(path, []byte(controller.output.Text()), 0o600); err != nil {
		showValidationError(controller.dialog, "Could not save diagnostics", err)
		return
	}
	if controller.localizer.IsRTL() {
		_ = controller.status.SetText("خروجی عیب‌یابی در این مسیر ذخیره شد: " + path)
	} else {
		_ = controller.status.SetText("Saved diagnostic output to " + path)
	}
}

func (controller *diagnosticsController) close() {
	if controller == nil || !controller.closed.CompareAndSwap(false, true) {
		return
	}
	controller.sequence.Add(1)
	controller.cancelActive()
	controller.stopOnce.Do(func() {
		close(controller.done)
		if controller.unsub != nil {
			controller.unsub()
		}
	})
}

func (controller *diagnosticsController) cancelActive() {
	controller.cancelMu.Lock()
	cancel := controller.cancel
	controller.cancel = nil
	controller.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func dialogCardHeading(
	text string,
	localizer ...i18n.Localizer,
) decl.Label {
	family := defaultUIFontFamily
	if len(localizer) > 0 {
		family = dialogFontFamily(localizer[0])
	}
	return decl.Label{
		Text: text,
		Font: decl.Font{
			Family:    family,
			PointSize: 10,
			Bold:      true,
		},
	}
}

func allowedDiagnosticEndpoints(_ config.DiagnosticsConfig) []string {
	return config.SafeDiagnosticEndpoints()
}

func callDiagnosticFetch(
	ctx context.Context,
	fetch func(context.Context, string) ([]byte, error),
	endpoint string,
) (data []byte, err error) {
	if fetch == nil {
		return nil, fmt.Errorf("diagnostic fetch function is unavailable")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			data = nil
			err = fmt.Errorf("diagnostic fetch panicked: %v", recovered)
		}
	}()
	return fetch(ctx, endpoint)
}

func formatDiagnosticPayload(endpoint string, data []byte, fetched time.Time) string {
	originalSize := len(data)
	truncated := originalSize > maxDiagnosticOutput
	if truncated {
		data = data[:maxDiagnosticOutput]
	}
	data = bytes.TrimSpace(data)

	var pretty bytes.Buffer
	jsonData := bytes.TrimSpace(bytes.TrimRight(data, "\x00"))
	trailingNUL := len(jsonData) != len(data)
	if len(jsonData) != 0 && json.Indent(&pretty, jsonData, "", "  ") == nil {
		data = pretty.Bytes()
		if trailingNUL {
			data = append(data, '\n', 0)
		}
	}
	body := cleanDiagnosticBody(string(data))
	if body == "" {
		body = "(The endpoint returned an empty response.)"
	}

	var truncation string
	if truncated {
		truncation = fmt.Sprintf(" (display truncated to %d bytes)", maxDiagnosticOutput)
	}
	return fmt.Sprintf(
		"Endpoint: %s\r\nFetched: %s\r\nResponse size: %d bytes%s\r\n\r\n%s",
		endpoint,
		fetched.Local().Format("2006-01-02 15:04:05"),
		originalSize,
		truncation,
		body,
	)
}

func formatDiagnosticError(endpoint string, err error, finished time.Time) string {
	message := cleanDisplayText(err.Error(), 2000)
	if errors.Is(err, context.DeadlineExceeded) {
		message = "The request exceeded the configured lamp request timeout."
	} else if errors.Is(err, context.Canceled) {
		message = "The request was canceled."
	}
	return fmt.Sprintf(
		"Endpoint: %s\r\nFinished: %s\r\n\r\nRequest failed:\r\n%s",
		endpoint,
		finished.Local().Format("2006-01-02 15:04:05"),
		message,
	)
}

func cleanDiagnosticBody(body string) string {
	body = strings.ToValidUTF8(body, "�")
	body = strings.ReplaceAll(body, "\x00", "␀")
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	return strings.ReplaceAll(body, "\n", "\r\n")
}

func diagnosticStateSummary(cfg config.Config, snapshot model.Snapshot) string {
	device := cleanDisplayText(snapshot.DeviceURL, 300)
	if device == "" {
		device = cfg.Lamp.BaseURL
	}
	lastEvent := cleanDisplayText(snapshot.LastEvent, 220)
	if lastEvent == "" {
		lastEvent = "None"
	}
	lastError := cleanDisplayText(snapshot.LastError, 350)
	if lastError == "" {
		lastError = "None"
	}
	pending := cleanDisplayText(snapshot.PendingCommand, 80)
	if pending == "" {
		pending = "None"
	}
	source := cleanDisplayText(snapshot.LastSource, 80)
	if source == "" {
		source = "Unknown"
	}
	return fmt.Sprintf(
		"State: %s\r\nHTTP: %s\r\nWebSocket: %s\r\nDevice: %s\r\nLast connected: %s\r\nLast changed: %s\r\nPending command: %s\r\nLast source: %s\r\nReconnect attempt: %d\r\nLast event: %s\r\nLast error: %s",
		snapshot.State.String(),
		displayBool(snapshot.HTTPReachable, "reachable", "offline"),
		displayBool(snapshot.WebSocketOnline, "connected", "disconnected"),
		device,
		compactTime(snapshot.LastConnected),
		compactTime(snapshot.LastChanged),
		pending,
		source,
		snapshot.ReconnectAttempt,
		lastEvent,
		lastError,
	)
}

func diagnosticStateAndDeviceSummary(
	cfg config.Config,
	lampSnapshot model.Snapshot,
	deviceSnapshot diagnostics.Snapshot,
) string {
	base := diagnosticStateSummary(cfg, lampSnapshot)
	info := deviceSnapshot.Device
	var metadata []string
	if info.Hostname != "" {
		metadata = append(metadata, "Hostname: "+info.Hostname)
	}
	if name := firstNonEmpty(info.DeviceName, info.Description.FriendlyName); name != "" {
		metadata = append(metadata, "Device name: "+name)
	}
	if firmware := firstNonEmpty(info.Firmware, info.WiFiVersion); firmware != "" {
		metadata = append(metadata, "Firmware: "+firmware)
	}
	if info.WiFiConnected != nil {
		network := "Wi-Fi: disconnected"
		if *info.WiFiConnected {
			network = "Wi-Fi: connected"
		}
		if info.SSID != "" {
			network += " to " + info.SSID
		}
		metadata = append(metadata, network)
	} else if info.SSID != "" {
		metadata = append(metadata, "SSID: "+info.SSID)
	}
	if info.WiFiIP != "" {
		metadata = append(metadata, "Lamp address: "+info.WiFiIP)
	}
	if info.RSSIDBm != nil {
		signal := fmt.Sprintf("Signal: %d dBm", *info.RSSIDBm)
		if info.SignalQuality != "" {
			signal += " (" + info.SignalQuality + ")"
		}
		metadata = append(metadata, signal)
	}
	if info.UptimeSeconds != nil {
		metadata = append(metadata, "Device uptime: "+compactDuration(*info.UptimeSeconds))
	}
	if !deviceSnapshot.UpdatedAt.IsZero() {
		metadata = append(
			metadata,
			"Metadata updated: "+deviceSnapshot.UpdatedAt.Local().Format("15:04:05"),
		)
	}
	if len(metadata) == 0 {
		metadata = append(metadata, "Device metadata: waiting for the first automatic refresh")
	}
	return base + "\r\n\r\n" + strings.Join(metadata, "\r\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func compactDuration(seconds int64) string {
	if seconds < 0 {
		return "unknown"
	}
	duration := time.Duration(seconds) * time.Second
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute
	duration -= minutes * time.Minute
	if days > 0 {
		return fmt.Sprintf("%dd %02d:%02d:%02d", days, hours, minutes, duration/time.Second)
	}
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, duration/time.Second)
}

func diagnosticOSSummary(info winfx.OSInfo, probeErr error) string {
	dark, darkErr := winfx.SystemDarkMode()
	darkText := displayBool(dark, "dark", "light")
	if darkErr != nil {
		darkText = "unknown"
	}
	probeText := "complete"
	if probeErr != nil {
		probeText = cleanDisplayText(probeErr.Error(), 300)
	}
	return fmt.Sprintf(
		"Rayan Lamp: %s\r\nEdition: %s\r\nDeveloper tools: %s\r\nCommit: %s\r\nBuild date: %s\r\nWindows NT: %s\r\nWindows 11: %s\r\nDWM composition: %s\r\nTransparency: %s\r\nHigh contrast: %s\r\nApp color preference: %s\r\nSystem backdrop: %s\r\nAccent acrylic: %s\r\nCapability probe: %s",
		buildinfo.Version,
		buildinfo.Edition,
		displayBool(buildinfo.DeveloperTools, "available", "not included"),
		cleanDisplayText(buildinfo.Commit, 100),
		cleanDisplayText(buildinfo.Date, 100),
		info.Version.String(),
		displayBool(info.Windows11, "yes", "no"),
		displayBool(info.DWMComposition, "enabled", "disabled"),
		displayBool(info.TransparencyEnabled, "enabled", "disabled"),
		displayBool(info.HighContrast, "enabled", "disabled"),
		darkText,
		displayBool(info.SupportsSystemBackdrop, "supported", "unavailable"),
		displayBool(info.SupportsAcrylicAccent, "supported", "unavailable"),
		probeText,
	)
}

func diagnosticWindowsAndBuildSummary(
	osSummary string,
	renderingSummary string,
) string {
	renderingSummary = cleanDisplayText(renderingSummary, 2000)
	if renderingSummary == "" {
		renderingSummary = "Not reported"
	}
	return strings.TrimSpace(osSummary) +
		"\r\n\r\nRendering\r\n" +
		cleanDiagnosticBody(renderingSummary)
}

func diagnosticIntroduction(expert bool) string {
	message := "Choose an audited, read-only endpoint and select Refresh.\r\n\r\n" +
		"Only fixed GET endpoints discovered in the lamp firmware are available. " +
		"Configuration, update, login, crash, and arbitrary paths are never sent from this dialog."
	if expert {
		message += "\r\n\r\nPU/debug diagnostics were retired because they belong to a separate device."
	}
	return message
}

func initialDiagnosticStatus(
	available bool,
	_ bool,
) string {
	if !available {
		return "Endpoint fetching is unavailable; snapshot and Windows information remain available."
	}
	return "Ready. Cached device metadata refreshes automatically; select Refresh to fetch now."
}
