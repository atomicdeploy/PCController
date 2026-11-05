package i18n

import "fmt"

// MessageID is a stable catalog key shared by all presentation layers.
type MessageID string

const (
	MsgAppName MessageID = "app.name"

	MsgLanguageAuto    MessageID = "language.auto"
	MsgLanguageEnglish MessageID = "language.en"
	MsgLanguagePersian MessageID = "language.fa"

	MsgCommonOK       MessageID = "common.ok"
	MsgCommonSave     MessageID = "common.save"
	MsgCommonApply    MessageID = "common.apply"
	MsgCommonCancel   MessageID = "common.cancel"
	MsgCommonClose    MessageID = "common.close"
	MsgCommonBack     MessageID = "common.back"
	MsgCommonReset    MessageID = "common.reset"
	MsgCommonRefresh  MessageID = "common.refresh"
	MsgCommonRetry    MessageID = "common.retry"
	MsgCommonCopy     MessageID = "common.copy"
	MsgCommonEdit     MessageID = "common.edit"
	MsgCommonEnabled  MessageID = "common.enabled"
	MsgCommonDisabled MessageID = "common.disabled"

	MsgStateOn           MessageID = "state.on"
	MsgStateOff          MessageID = "state.off"
	MsgStateOffline      MessageID = "state.offline"
	MsgStateConnected    MessageID = "state.connected"
	MsgStateDisconnected MessageID = "state.disconnected"
	MsgStateConnecting   MessageID = "state.connecting"
	MsgStateUnknown      MessageID = "state.unknown"
	MsgStateError        MessageID = "state.error"

	MsgActionToggle           MessageID = "action.toggle"
	MsgActionTurnOn           MessageID = "action.turn_on"
	MsgActionTurnOff          MessageID = "action.turn_off"
	MsgActionShowWindow       MessageID = "action.show_window"
	MsgActionHideWindow       MessageID = "action.hide_window"
	MsgActionSettings         MessageID = "action.settings"
	MsgActionDiagnostics      MessageID = "action.diagnostics"
	MsgActionTerminal         MessageID = "action.terminal"
	MsgActionQuit             MessageID = "action.quit"
	MsgActionReconnect        MessageID = "action.reconnect"
	MsgActionWebDashboard     MessageID = "action.web_dashboard"
	MsgActionDashboardView    MessageID = "action.dashboard_view"
	MsgActionCompactView      MessageID = "action.compact_view"
	MsgActionAlwaysOnTop      MessageID = "action.always_on_top"
	MsgActionStartWithWindows MessageID = "action.start_with_windows"

	MsgScreenDashboard   MessageID = "screen.dashboard"
	MsgScreenCommands    MessageID = "screen.commands"
	MsgScreenSettings    MessageID = "screen.settings"
	MsgScreenDevice      MessageID = "screen.device"
	MsgScreenDiagnostics MessageID = "screen.diagnostics"
	MsgScreenTerminal    MessageID = "screen.terminal"
	MsgScreenAbout       MessageID = "screen.about"

	MsgSettingsGeneral       MessageID = "settings.general"
	MsgSettingsAppearance    MessageID = "settings.appearance"
	MsgSettingsLamp          MessageID = "settings.lamp"
	MsgSettingsNotifications MessageID = "settings.notifications"
	MsgSettingsHotkeys       MessageID = "settings.hotkeys"
	MsgSettingsAutomation    MessageID = "settings.automation"
	MsgSettingsIntegration   MessageID = "settings.integration"
	MsgSettingsAudio         MessageID = "settings.audio"
	MsgSettingsLanguage      MessageID = "settings.language"

	MsgFieldState      MessageID = "field.state"
	MsgFieldMode       MessageID = "field.mode"
	MsgFieldAddress    MessageID = "field.address"
	MsgFieldHost       MessageID = "field.host"
	MsgFieldVersion    MessageID = "field.version"
	MsgFieldIP         MessageID = "field.ip"
	MsgFieldSSID       MessageID = "field.ssid"
	MsgFieldSignal     MessageID = "field.signal"
	MsgFieldLastUpdate MessageID = "field.last_update"

	MsgNoticeSaved         MessageID = "notice.saved"
	MsgNoticeRequestFailed MessageID = "notice.request_failed"
	MsgNoticeNoData        MessageID = "notice.no_data"

	MsgMainLightOn        MessageID = "main.light_on"
	MsgMainLightOff       MessageID = "main.light_off"
	MsgMainFindingLamp    MessageID = "main.finding_lamp"
	MsgMainUpdating       MessageID = "main.updating"
	MsgMainWorking        MessageID = "main.working"
	MsgMainInfo           MessageID = "main.info"
	MsgMainRightClickHint MessageID = "main.right_click_hint"

	MsgConnectionLive         MessageID = "connection.live"
	MsgConnectionHTTPFallback MessageID = "connection.http_fallback"
	MsgConnectionReconnecting MessageID = "connection.reconnecting"

	MsgTrayStateOn         MessageID = "tray.state_on"
	MsgTrayStateOff        MessageID = "tray.state_off"
	MsgTrayConnectionError MessageID = "tray.connection_error"
	MsgTrayStateUnknown    MessageID = "tray.state_unknown"
	MsgTrayTooltip         MessageID = "tray.tooltip"
	MsgTrayLeftClickHint   MessageID = "tray.left_click_hint"

	MsgTaskbarSuspended       MessageID = "taskbar.suspended"
	MsgTaskbarDisconnected    MessageID = "taskbar.disconnected"
	MsgTaskbarOperationFailed MessageID = "taskbar.operation_failed"
	MsgTaskbarTurningOn       MessageID = "taskbar.turning_on"
	MsgTaskbarTurningOff      MessageID = "taskbar.turning_off"
	MsgTaskbarToggling        MessageID = "taskbar.toggling"
	MsgTaskbarWorking         MessageID = "taskbar.working"

	MsgErrorFocusSettings     MessageID = "error.focus_settings"
	MsgErrorOpenSettings      MessageID = "error.open_settings"
	MsgErrorOpenDiagnostics   MessageID = "error.open_diagnostics"
	MsgErrorWebUnavailable    MessageID = "error.web_unavailable"
	MsgErrorOpenWeb           MessageID = "error.open_web"
	MsgErrorOpenMenu          MessageID = "error.open_menu"
	MsgErrorUpdateSettings    MessageID = "error.update_settings"
	MsgErrorWindowChrome      MessageID = "error.window_chrome"
	MsgErrorResizeWindow      MessageID = "error.resize_window"
	MsgErrorHotkeyUnavailable MessageID = "error.hotkey_unavailable"
	MsgErrorRunCommand        MessageID = "error.run_command"
	MsgErrorCommandFailed     MessageID = "error.command_failed"

	MsgNotificationTestTitle     MessageID = "notification.test_title"
	MsgNotificationTestBody      MessageID = "notification.test_body"
	MsgNotificationLampTurnedOn  MessageID = "notification.lamp_turned_on"
	MsgNotificationLampTurnedOff MessageID = "notification.lamp_turned_off"
)

var englishCatalog = map[MessageID]string{
	MsgAppName: "Rayan Lamp",

	MsgLanguageAuto:    "System default",
	MsgLanguageEnglish: "English",
	MsgLanguagePersian: "Persian",

	MsgCommonOK:       "OK",
	MsgCommonSave:     "Save",
	MsgCommonApply:    "Apply",
	MsgCommonCancel:   "Cancel",
	MsgCommonClose:    "Close",
	MsgCommonBack:     "Back",
	MsgCommonReset:    "Reset",
	MsgCommonRefresh:  "Refresh",
	MsgCommonRetry:    "Retry",
	MsgCommonCopy:     "Copy",
	MsgCommonEdit:     "Edit",
	MsgCommonEnabled:  "Enabled",
	MsgCommonDisabled: "Disabled",

	MsgStateOn:           "On",
	MsgStateOff:          "Off",
	MsgStateOffline:      "Offline",
	MsgStateConnected:    "Connected",
	MsgStateDisconnected: "Disconnected",
	MsgStateConnecting:   "Connecting…",
	MsgStateUnknown:      "Unknown",
	MsgStateError:        "Error",

	MsgActionToggle:           "Toggle lamp",
	MsgActionTurnOn:           "Turn on",
	MsgActionTurnOff:          "Turn off",
	MsgActionShowWindow:       "Show main window",
	MsgActionHideWindow:       "Hide main window",
	MsgActionSettings:         "Settings",
	MsgActionDiagnostics:      "Diagnostics",
	MsgActionTerminal:         "Terminal",
	MsgActionQuit:             "Exit Rayan Lamp",
	MsgActionReconnect:        "Reconnect",
	MsgActionWebDashboard:     "Web dashboard…",
	MsgActionDashboardView:    "Use dashboard view",
	MsgActionCompactView:      "Use compact lamp view",
	MsgActionAlwaysOnTop:      "Always on top",
	MsgActionStartWithWindows: "Start with Windows",

	MsgScreenDashboard:   "Dashboard",
	MsgScreenCommands:    "Commands",
	MsgScreenSettings:    "Settings",
	MsgScreenDevice:      "Device",
	MsgScreenDiagnostics: "Diagnostics",
	MsgScreenTerminal:    "Terminal",
	MsgScreenAbout:       "About",

	MsgSettingsGeneral:       "General",
	MsgSettingsAppearance:    "Appearance",
	MsgSettingsLamp:          "Lamp",
	MsgSettingsNotifications: "Notifications",
	MsgSettingsHotkeys:       "Hotkeys",
	MsgSettingsAutomation:    "Automation",
	MsgSettingsIntegration:   "Integration",
	MsgSettingsAudio:         "Audio",
	MsgSettingsLanguage:      "Language",

	MsgFieldState:      "State",
	MsgFieldMode:       "Mode",
	MsgFieldAddress:    "Address",
	MsgFieldHost:       "Host",
	MsgFieldVersion:    "Version",
	MsgFieldIP:         "IP address",
	MsgFieldSSID:       "Wi-Fi network",
	MsgFieldSignal:     "Signal",
	MsgFieldLastUpdate: "Last update",

	MsgNoticeSaved:         "Settings saved",
	MsgNoticeRequestFailed: "Request failed: %s",
	MsgNoticeNoData:        "No data available",

	MsgMainLightOn:        "LIGHT IS ON",
	MsgMainLightOff:       "LIGHT IS OFF",
	MsgMainFindingLamp:    "FINDING LAMP",
	MsgMainUpdating:       "UPDATING",
	MsgMainWorking:        "Working…",
	MsgMainInfo:           "Info",
	MsgMainRightClickHint: "Right-click for integrations and exit",

	MsgConnectionLive:         "live",
	MsgConnectionHTTPFallback: "HTTP fallback",
	MsgConnectionReconnecting: "reconnecting",

	MsgTrayStateOn:         "on",
	MsgTrayStateOff:        "off",
	MsgTrayConnectionError: "connection error",
	MsgTrayStateUnknown:    "state unknown",
	MsgTrayTooltip:         "Rayan Lamp — %s (%s)\n%s",
	MsgTrayLeftClickHint:   "Left-click to toggle; right-click for menu",

	MsgTaskbarSuspended:       "Rayan Lamp — Suspended",
	MsgTaskbarDisconnected:    "Rayan Lamp — Disconnected",
	MsgTaskbarOperationFailed: "Rayan Lamp — Operation failed",
	MsgTaskbarTurningOn:       "Turning on…",
	MsgTaskbarTurningOff:      "Turning off…",
	MsgTaskbarToggling:        "Toggling…",
	MsgTaskbarWorking:         "Working…",

	MsgErrorFocusSettings:     "Unable to focus settings",
	MsgErrorOpenSettings:      "Unable to open settings",
	MsgErrorOpenDiagnostics:   "Unable to open diagnostics",
	MsgErrorWebUnavailable:    "Web dashboard is unavailable",
	MsgErrorOpenWeb:           "Unable to open the web dashboard",
	MsgErrorOpenMenu:          "Unable to open the app menu",
	MsgErrorUpdateSettings:    "Unable to update settings",
	MsgErrorWindowChrome:      "Unable to change the window chrome",
	MsgErrorResizeWindow:      "Unable to resize the lamp window",
	MsgErrorHotkeyUnavailable: "Global hotkey unavailable",
	MsgErrorRunCommand:        "Unable to run command",
	MsgErrorCommandFailed:     "Rayan Lamp command failed",

	MsgNotificationTestTitle:     "Rayan Lamp test",
	MsgNotificationTestBody:      "Modern notifications are ready.",
	MsgNotificationLampTurnedOn:  "Lamp turned on",
	MsgNotificationLampTurnedOff: "Lamp turned off",
}

var persianCatalog = map[MessageID]string{
	MsgAppName: "چراغ رایان",

	MsgLanguageAuto:    "پیش‌فرض سیستم",
	MsgLanguageEnglish: "انگلیسی",
	MsgLanguagePersian: "فارسی",

	MsgCommonOK:       "تأیید",
	MsgCommonSave:     "ذخیره",
	MsgCommonApply:    "اعمال",
	MsgCommonCancel:   "انصراف",
	MsgCommonClose:    "بستن",
	MsgCommonBack:     "بازگشت",
	MsgCommonReset:    "بازنشانی",
	MsgCommonRefresh:  "تازه‌سازی",
	MsgCommonRetry:    "تلاش دوباره",
	MsgCommonCopy:     "رونوشت",
	MsgCommonEdit:     "ویرایش",
	MsgCommonEnabled:  "فعال",
	MsgCommonDisabled: "غیرفعال",

	MsgStateOn:           "روشن",
	MsgStateOff:          "خاموش",
	MsgStateOffline:      "آفلاین",
	MsgStateConnected:    "متصل",
	MsgStateDisconnected: "قطع‌شده",
	MsgStateConnecting:   "در حال اتصال…",
	MsgStateUnknown:      "نامشخص",
	MsgStateError:        "خطا",

	MsgActionToggle:           "تغییر وضعیت چراغ",
	MsgActionTurnOn:           "روشن کردن",
	MsgActionTurnOff:          "خاموش کردن",
	MsgActionShowWindow:       "نمایش پنجرهٔ اصلی",
	MsgActionHideWindow:       "پنهان کردن پنجرهٔ اصلی",
	MsgActionSettings:         "تنظیمات",
	MsgActionDiagnostics:      "عیب‌یابی",
	MsgActionTerminal:         "پایانه",
	MsgActionQuit:             "خروج از چراغ رایان",
	MsgActionReconnect:        "اتصال دوباره",
	MsgActionWebDashboard:     "داشبورد وب…",
	MsgActionDashboardView:    "استفاده از نمای داشبورد",
	MsgActionCompactView:      "استفاده از نمای فشردهٔ چراغ",
	MsgActionAlwaysOnTop:      "همیشه روی پنجره‌های دیگر",
	MsgActionStartWithWindows: "اجرای خودکار با ویندوز",

	MsgScreenDashboard:   "داشبورد",
	MsgScreenCommands:    "فرمان‌ها",
	MsgScreenSettings:    "تنظیمات",
	MsgScreenDevice:      "دستگاه",
	MsgScreenDiagnostics: "عیب‌یابی",
	MsgScreenTerminal:    "پایانه",
	MsgScreenAbout:       "درباره",

	MsgSettingsGeneral:       "عمومی",
	MsgSettingsAppearance:    "ظاهر",
	MsgSettingsLamp:          "چراغ",
	MsgSettingsNotifications: "اعلان‌ها",
	MsgSettingsHotkeys:       "میان‌برها",
	MsgSettingsAutomation:    "خودکارسازی",
	MsgSettingsIntegration:   "یکپارچه‌سازی",
	MsgSettingsAudio:         "صدا",
	MsgSettingsLanguage:      "زبان",

	MsgFieldState:      "وضعیت",
	MsgFieldMode:       "حالت",
	MsgFieldAddress:    "نشانی",
	MsgFieldHost:       "میزبان",
	MsgFieldVersion:    "نسخه",
	MsgFieldIP:         "نشانی IP",
	MsgFieldSSID:       "شبکهٔ وای‌فای",
	MsgFieldSignal:     "قدرت سیگنال",
	MsgFieldLastUpdate: "آخرین به‌روزرسانی",

	MsgNoticeSaved:         "تنظیمات ذخیره شد",
	MsgNoticeRequestFailed: "درخواست ناموفق بود: %s",
	MsgNoticeNoData:        "داده‌ای موجود نیست",

	MsgMainLightOn:        "چراغ روشن است",
	MsgMainLightOff:       "چراغ خاموش است",
	MsgMainFindingLamp:    "در حال یافتن چراغ",
	MsgMainUpdating:       "در حال به‌روزرسانی",
	MsgMainWorking:        "در حال انجام…",
	MsgMainInfo:           "اطلاعات",
	MsgMainRightClickHint: "برای یکپارچه‌سازی‌ها و خروج، راست‌کلیک کنید",

	MsgConnectionLive:         "برخط",
	MsgConnectionHTTPFallback: "جایگزین HTTP",
	MsgConnectionReconnecting: "در حال اتصال دوباره",

	MsgTrayStateOn:         "روشن",
	MsgTrayStateOff:        "خاموش",
	MsgTrayConnectionError: "خطای اتصال",
	MsgTrayStateUnknown:    "وضعیت نامشخص",
	MsgTrayTooltip:         "چراغ رایان — %s (%s)\n%s",
	MsgTrayLeftClickHint:   "کلیک چپ: تغییر وضعیت؛ کلیک راست: نمایش منو",

	MsgTaskbarSuspended:       "چراغ رایان — در حالت تعلیق",
	MsgTaskbarDisconnected:    "چراغ رایان — قطع ارتباط",
	MsgTaskbarOperationFailed: "چراغ رایان — عملیات ناموفق بود",
	MsgTaskbarTurningOn:       "در حال روشن کردن…",
	MsgTaskbarTurningOff:      "در حال خاموش کردن…",
	MsgTaskbarToggling:        "در حال تغییر وضعیت…",
	MsgTaskbarWorking:         "در حال انجام…",

	MsgErrorFocusSettings:     "تمرکز روی پنجرهٔ تنظیمات ممکن نشد",
	MsgErrorOpenSettings:      "باز کردن تنظیمات ممکن نشد",
	MsgErrorOpenDiagnostics:   "باز کردن عیب‌یابی ممکن نشد",
	MsgErrorWebUnavailable:    "داشبورد وب در دسترس نیست",
	MsgErrorOpenWeb:           "باز کردن داشبورد وب ممکن نشد",
	MsgErrorOpenMenu:          "باز کردن منوی برنامه ممکن نشد",
	MsgErrorUpdateSettings:    "به‌روزرسانی تنظیمات ممکن نشد",
	MsgErrorWindowChrome:      "تغییر قاب پنجره ممکن نشد",
	MsgErrorResizeWindow:      "تغییر اندازهٔ پنجرهٔ چراغ ممکن نشد",
	MsgErrorHotkeyUnavailable: "میان‌بر سراسری در دسترس نیست",
	MsgErrorRunCommand:        "اجرای فرمان ممکن نشد",
	MsgErrorCommandFailed:     "فرمان چراغ رایان ناموفق بود",

	MsgNotificationTestTitle:     "آزمایش چراغ رایان",
	MsgNotificationTestBody:      "اعلان‌های نوین آماده‌اند.",
	MsgNotificationLampTurnedOn:  "چراغ روشن شد",
	MsgNotificationLampTurnedOff: "چراغ خاموش شد",
}

// Localizer translates messages and applies locale-aware digit shaping.
type Localizer struct {
	locale Locale
}

// New returns a localizer for a preference. Auto follows the system locale.
func New(preference Locale) Localizer {
	return Localizer{locale: Resolve(preference)}
}

// NewForSystem returns a localizer resolved against an explicit system locale
// name. It is useful when multiple surfaces must share one resolved locale.
func NewForSystem(preference Locale, systemLocaleName string) Localizer {
	return Localizer{locale: ResolveName(preference, systemLocaleName)}
}

// Locale returns the resolved en or fa locale.
func (l Localizer) Locale() Locale {
	if l.locale == Persian {
		return Persian
	}
	return English
}

// Metadata returns layout and formatting metadata without another system query.
func (l Localizer) Metadata() Metadata {
	if l.Locale() == Persian {
		return Metadata{
			Locale:      Persian,
			LanguageTag: "fa-IR",
			Direction:   RTL,
			RTL:         true,
			Digits:      PersianDigits,
		}
	}
	return Metadata{
		Locale:      English,
		LanguageTag: "en-US",
		Direction:   LTR,
		RTL:         false,
		Digits:      LatinDigits,
	}
}

// IsRTL reports whether this localizer uses a right-to-left base direction.
func (l Localizer) IsRTL() bool {
	return l.Locale() == Persian
}

// Choose selects one of two literal strings and applies locale digit shaping.
// It is intended for compact, surface-specific copy that does not warrant a
// stable cross-surface MessageID.
func (l Localizer) Choose(english, persian string) string {
	if l.Locale() == Persian && persian != "" {
		return l.ShapeDigits(persian)
	}
	return l.ShapeDigits(english)
}

// Text returns a translated message, falling back to English and finally the
// stable message ID. Persian results use Persian digit glyphs.
func (l Localizer) Text(id MessageID) string {
	message := ""
	if l.Locale() == Persian {
		message = persianCatalog[id]
	}
	if message == "" {
		message = englishCatalog[id]
	}
	if message == "" {
		message = string(id)
	}
	return l.ShapeDigits(message)
}

// Format translates a printf-style message and formats it with args.
func (l Localizer) Format(id MessageID, args ...any) string {
	return l.ShapeDigits(fmt.Sprintf(l.Text(id), args...))
}

// ShapeDigits applies the preferred digit glyphs for this localizer.
func (l Localizer) ShapeDigits(value string) string {
	if l.Locale() == Persian {
		return ShapePersianDigits(value)
	}
	return ShapeLatinDigits(value)
}

// Messages returns a copy of the resolved locale's complete catalog.
func Messages(locale Locale) map[MessageID]string {
	source := englishCatalog
	if Resolve(locale) == Persian {
		source = persianCatalog
	}
	result := make(map[MessageID]string, len(source))
	for id, message := range source {
		result[id] = message
	}
	return result
}
