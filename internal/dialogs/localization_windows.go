//go:build windows

package dialogs

import (
	"reflect"
	"strings"
	"syscall"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/i18n"
	"rayanlamp.local/app/internal/winfx"
)

const defaultUIFontFamily = "Segoe UI"

var persianDialogStrings = map[string]string{
	"Settings — Rayan Lamp": "تنظیمات — چراغ رایان",
	"Settings":              "تنظیمات",
	"Check the highlighted settings and try again.": "تنظیمات مشخص‌شده را بررسی و دوباره تلاش کنید.",
	"Settings need attention":                       "تنظیمات نیاز به بررسی دارد",
	"Hotkeys need attention":                        "میان‌برها نیاز به بررسی دارند",
	"Unable to save settings":                       "ذخیرهٔ تنظیمات ممکن نشد",
	"Changes preview immediately. Save keeps them; Cancel restores the previous setup.": "تغییرات همان لحظه پیش‌نمایش می‌شوند. «ذخیره» آن‌ها را نگه می‌دارد و «انصراف» تنظیمات قبلی را برمی‌گرداند.",
	"&Save settings":         "&ذخیرهٔ تنظیمات",
	"Saving…":                "در حال ذخیره…",
	"&Cancel":                "&انصراف",
	"&Close":                 "&بستن",
	"C&lose":                 "&بستن",
	"&Refresh":               "&تازه‌سازی",
	"&Copy output":           "&رونوشت خروجی",
	"&Save output…":          "&ذخیرهٔ خروجی…",
	"&Test toast":            "&آزمایش اعلان",
	"Test &audio":            "آزمایش &صدا",
	"&Import WAV…":           "&واردکردن WAV…",
	"Browse &on…":            "انتخاب روشن‌شدن (&N)…",
	"Browse o&ff…":           "انتخاب خاموش‌شدن (&F)…",
	"Browse &event…":         "انتخاب رویداد (&E)…",
	"Browse &lock…":          "انتخاب قفل (&L)…",
	"Browse &unlock…":        "انتخاب بازشدن قفل (&U)…",
	"Browse &suspend…":       "انتخاب تعلیق (&S)…",
	"Browse &resume…":        "انتخاب ازسرگیری (&R)…",
	"Browse &network…":       "انتخاب شبکه (&W)…",
	"Browse fol&der…":        "انتخاب پوشه (&D)…",
	"&Reveal":                "&نمایش",
	"Cop&y token":            "&رونوشت توکن",
	"Re&generate…":           "&ساخت دوباره…",
	"Register shell &URI":    "ثبت &URI پوسته",
	"Unregister shell &URI":  "لغو ثبت &URI پوسته",
	"Register &automation":   "ثبت &خودکارسازی",
	"Unregister &automation": "لغو ثبت &خودکارسازی",

	"Lamp":          "چراغ",
	"Appearance":    "ظاهر",
	"Notifications": "اعلان‌ها",
	"Hotkeys":       "میان‌برها",
	"Automation":    "خودکارسازی",
	"Integration":   "یکپارچه‌سازی",
	"Language:":     "زبان:",

	"Lamp address:":    "نشانی چراغ:",
	"Request timeout:": "مهلت درخواست:",
	"Listen continuously for WebSocket events": "به‌طور پیوسته به رویدادهای WebSocket گوش بده",
	"Reconnect minimum:":                       "کمینهٔ تلاش دوباره:",
	"Reconnect maximum:":                       "بیشینهٔ تلاش دوباره:",
	"HTTP poll interval:":                      "فاصلهٔ پایش HTTP:",
	"Device terminal port:":                    "درگاه پایانهٔ دستگاه:",
	"Manual only":                              "فقط دستی",
	"The .local address is resolved once and refreshed only after a cached connection fails.": "نشانی ‎.local‎ یک‌بار پیدا می‌شود و فقط پس از شکست اتصال ذخیره‌شده دوباره بررسی خواهد شد.",

	"Window layout:":                           "چیدمان پنجره:",
	"Color theme:":                             "پوستهٔ رنگ:",
	"Window backdrop:":                         "پس‌زمینهٔ پنجره:",
	"Rendering backend:":                       "موتور رندر:",
	"Title bar:":                               "نوار عنوان:",
	"Window border:":                           "قاب پنجره:",
	"Use rounded window corners":               "گوشه‌های پنجره گرد باشند",
	"Enable layered glass foreground effects":  "افکت شیشه‌ای لایهٔ جلویی فعال باشد",
	"Keep the lamp window above other windows": "پنجرهٔ چراغ همیشه روی پنجره‌های دیگر بماند",
	"Close button hides the app in the notification area": "دکمهٔ بستن، برنامه را در ناحیهٔ اعلان پنهان کند",
	"Preload and reuse the notification-area menu":        "منوی ناحیهٔ اعلان از پیش آماده و دوباره استفاده شود",
	"Start with the main window hidden":                   "برنامه با پنجرهٔ اصلی پنهان آغاز شود",
	"Show the main window on the taskbar":                 "پنجرهٔ اصلی در نوار وظیفه نمایش داده شود",
	"Enable APNG animation and motion":                    "پویانمایی APNG و حرکت‌ها فعال باشند",
	"Magnetic edge snap":                                  "چسبندگی مغناطیسی به لبه",
	"Distance:":                                           "فاصله:",

	"Enable desktop notifications":                   "اعلان‌های دسکتاپ فعال باشند",
	"Delivery method:":                               "روش نمایش:",
	"Notify when the lamp turns on or off":           "هنگام روشن یا خاموش‌شدن چراغ اعلان بده",
	"Notify after the initial state synchronization": "پس از همگام‌سازی نخستین وضعیت اعلان بده",
	"Show sanitized message: events from the lamp":   "پیام‌های پالایش‌شدهٔ چراغ را نمایش بده",
	"Play audible cues":                              "نشانه‌های صوتی پخش شوند",
	"Sound source:":                                  "منبع صدا:",
	"Custom WAV:":                                    "WAV سفارشی:",
	"Try notifications:":                             "آزمایش اعلان‌ها:",
	"Automatic delivery prefers a modern toast and falls back to the existing tray balloon.": "حالت خودکار ابتدا اعلان مدرن را امتحان می‌کند و در صورت نیاز به بالون ناحیهٔ اعلان برمی‌گردد.",

	"Keyboard shortcuts": "میان‌برهای صفحه‌کلید",
	"Select Record, then press the complete combination. Existing global shortcuts pause while Rayan Lamp is listening.": "«ضبط» را انتخاب کنید، سپس ترکیب کامل کلیدها را بزنید. هنگام شنود چراغ رایان، میان‌برهای سراسری موقتاً متوقف می‌شوند.",
	"Escape cancels · Backspace or Delete clears · left/right modifiers are normalized automatically":                    "Escape لغو می‌کند · Backspace یا Delete پاک می‌کند · کلیدهای تغییردهندهٔ چپ و راست خودکار یکسان می‌شوند",
	"Toggle lamp":              "تغییر وضعیت چراغ",
	"Turn on":                  "روشن کردن",
	"Turn off":                 "خاموش کردن",
	"Show window":              "نمایش پنجره",
	"Switch on ↔ off":          "جابجایی روشن ↔ خاموش",
	"Force the lamp on":        "چراغ را روشن کن",
	"Force the lamp off":       "چراغ را خاموش کن",
	"Bring Rayan Lamp forward": "چراغ رایان را به پیش‌زمینه بیاور",
	"Ready":                    "آماده",
	"Record":                   "ضبط",
	"Clear":                    "پاک‌کردن",
	"Record &toggle":           "ضبط (&T)",
	"Clear to&ggle":            "پاک‌کردن (&G)",
	"Record o&n":               "ضبط (&N)",
	"Clea&r on":                "پاک‌کردن (&R)",
	"Record of&f":              "ضبط (&F)",
	"Clear &off":               "پاک‌کردن (&O)",
	"Record sho&w":             "ضبط (&W)",
	"C&lear show":              "پاک‌کردن (&L)",
	"Cancel r&ecording":        "لغو ضبط (&E)",
	"Disabled — record a shortcut to enable":                                       "غیرفعال — برای فعال‌سازی یک میان‌بر ضبط کنید",
	"Unsupported shortcut — select Record to replace it.":                          "میان‌بر پشتیبانی نمی‌شود — برای جایگزینی «ضبط» را انتخاب کنید.",
	"F12 is reserved by Windows.":                                                  "کلید F12 در اختیار ویندوز است.",
	"System-wide single-key shortcut — use carefully.":                             "میان‌بر سراسری تک‌کلیدی — با احتیاط استفاده کنید.",
	"Unavailable — Windows or another app already owns this shortcut.":             "در دسترس نیست — ویندوز یا برنامهٔ دیگری مالک این میان‌بر است.",
	"Replace the invalid value by recording a new shortcut.":                       "مقدار نامعتبر را با ضبط یک میان‌بر تازه جایگزین کنید.",
	"Standard-key capture active — Windows system-key interception is unavailable": "ضبط کلیدهای معمولی فعال است — رهگیری کلیدهای سیستمی ویندوز در دسترس نیست",
	"Listening — press a key with any modifiers · Esc cancels":                     "در حال شنود — کلید اصلی و تغییردهنده‌ها را بزنید · Esc لغو می‌کند",
	"Listening — press the main key · Esc cancels":                                 "در حال شنود — کلید اصلی را بزنید · Esc لغو می‌کند",
	"That key cannot be registered. Try another key.":                              "این کلید قابل ثبت نیست. کلید دیگری را امتحان کنید.",

	"Run local scripts for lamp and Windows events": "برای رویدادهای چراغ و ویندوز اسکریپت محلی اجرا شود",
	"Lamp-on script:":        "اسکریپت روشن‌شدن:",
	"Lamp-off script:":       "اسکریپت خاموش‌شدن:",
	"Message script:":        "اسکریپت پیام:",
	"Windows lock script:":   "اسکریپت قفل ویندوز:",
	"Windows unlock script:": "اسکریپت بازشدن قفل ویندوز:",
	"System suspend script:": "اسکریپت تعلیق سیستم:",
	"System resume script:":  "اسکریپت ازسرگیری سیستم:",
	"Network-change script:": "اسکریپت تغییر شبکه:",
	"Working directory:":     "پوشهٔ کاری:",
	"Script timeout:":        "مهلت اسکریپت:",
	"Capture bounded script output in the application log":                                                      "خروجی محدود اسکریپت در گزارش برنامه ثبت شود",
	"Scripts run hidden with RAYANLAMP_* event variables. Only explicitly configured local files are executed.": "اسکریپت‌ها با متغیرهای رویداد ‎RAYANLAMP_*‎ و به‌صورت پنهان اجرا می‌شوند. فقط فایل‌های محلی که صریحاً تنظیم شده‌اند اجرا خواهند شد.",

	"Start Rayan Lamp when I sign in":       "چراغ رایان هنگام ورود من اجرا شود",
	"Enable the authenticated loopback API": "API احرازشدهٔ لوپ‌بک فعال باشد",
	"Listen address:":                       "نشانی شنود:",
	"Bearer token:":                         "توکن Bearer:",
	"The API remains bound to loopback. Treat the token like a password and rotate it if exposed.": "API فقط روی لوپ‌بک می‌ماند. با توکن مانند گذرواژه رفتار کنید و اگر فاش شد آن را تغییر دهید.",
	"Windows registration": "ثبت در ویندوز",
	"Portable mode does not register these automatically. Register only when you want this exact executable to own them.": "حالت قابل‌حمل این موارد را خودکار ثبت نمی‌کند. فقط زمانی ثبت کنید که می‌خواهید همین فایل اجرایی مالک آن‌ها باشد.",
	"Current executable:":                       "فایل اجرایی فعلی:",
	"Shell (URI + Jump List):":                  "پوسته (URI و Jump List):",
	"COM automation:":                           "خودکارسازی COM:",
	"Checking…":                                 "در حال بررسی…",
	"Checking URI protocol and Jump List…":      "در حال بررسی پروتکل URI و Jump List…",
	"Checking VBA and AutoHotkey registration…": "در حال بررسی ثبت VBA و AutoHotkey…",
	"Unavailable in this host":                  "در این میزبان در دسترس نیست",
	"Registration status is unavailable":        "وضعیت ثبت در دسترس نیست",
	"Not reported":                              "گزارش نشده",
	"Exact current executable was not reported": "فایل اجرایی دقیق فعلی گزارش نشده است",
	"Registered to this executable":             "برای این فایل اجرایی ثبت شده است",
	"Not registered (registration is opt-in)":   "ثبت نشده (ثبت اختیاری است)",
	"Updating URI protocol and Jump List…":      "در حال به‌روزرسانی پروتکل URI و Jump List…",
	"Updating COM automation registration…":     "در حال به‌روزرسانی ثبت خودکارسازی COM…",
	"Unregister removes only entries still owned by the executable shown above; foreign registrations are preserved.": "لغو ثبت فقط مواردی را حذف می‌کند که هنوز متعلق به فایل اجرایی بالا هستند؛ ثبت‌های متعلق به برنامه‌های دیگر دست‌نخورده می‌مانند.",

	"Compact lamp":                       "چراغ فشرده",
	"Dashboard":                          "داشبورد",
	"Follow Windows":                     "پیروی از ویندوز",
	"Light":                              "روشن",
	"Dark":                               "تیره",
	"Automatic":                          "خودکار",
	"Solid (no effect)":                  "یکدست (بدون افکت)",
	"Transparent (no backdrop)":          "شفاف (بدون پس‌زمینه)",
	"Blur":                               "محو",
	"Acrylic":                            "اکریلیک",
	"Mica":                               "میکا",
	"Tabbed":                             "زبانه‌ای",
	"Automatic (DirectX when supported)": "خودکار (در صورت پشتیبانی DirectX)",
	"DirectX 11 hardware":                "سخت‌افزاری DirectX 11",
	"Compatibility renderer (lower RAM)": "رندر سازگار (مصرف RAM کمتر)",
	"None":                               "هیچ‌کدام",
	"Custom controls":                    "کنترل‌های سفارشی",
	"Native Windows title bar":           "نوار عنوان بومی ویندوز",
	"Automatic (resizable)":              "خودکار (تغییرپذیر)",
	"None (frameless)":                   "بدون قاب",
	"Modern toast":                       "اعلان مدرن",
	"Legacy balloon":                     "بالون قدیمی",
	"Bundled Rayan Lamp sounds":          "صداهای همراه چراغ رایان",
	"Custom WAV":                         "WAV سفارشی",
	"System default":                     "پیش‌فرض سیستم",
	"English":                            "انگلیسی",
	"Persian":                            "فارسی",

	"Rayan Lamp Diagnostics": "عیب‌یابی چراغ رایان",
	"Lamp and connection":    "چراغ و اتصال",
	"Windows and build":      "ویندوز و ساخت",
	"Device endpoint":        "نقطهٔ پایانی دستگاه",
	"Endpoint:":              "نقطهٔ پایانی:",
	"Show raw response":      "نمایش پاسخ خام",
	"Endpoint fetching is unavailable in this session.": "دریافت نقطهٔ پایانی در این نشست در دسترس نیست.",
	"Copied diagnostic output to the clipboard.":        "خروجی عیب‌یابی در کلیپ‌بورد رونویسی شد.",
	"Choose an endpoint":                                "یک نقطهٔ پایانی انتخاب کنید",
	"Endpoint not permitted":                            "نقطهٔ پایانی مجاز نیست",
	"Save Rayan Lamp diagnostics":                       "ذخیرهٔ عیب‌یابی چراغ رایان",
	"Text files (*.txt)|*.txt|All files (*.*)|*.*":      "فایل‌های متنی (*.txt)|*.txt|همهٔ فایل‌ها (*.*)|*.*",
	"All files (*.*)|*.*":                               "همهٔ فایل‌ها (*.*)|*.*",
	"Import Rayan Lamp sound effect":                    "واردکردن افکت صوتی چراغ رایان",
	"PCM Wave audio (*.wav)|*.wav|All files (*.*)|*.*":  "صدای PCM Wave (*.wav)|*.wav|همهٔ فایل‌ها (*.*)|*.*",
	"Supported scripts (*.ps1;*.cmd;*.bat;*.exe;*.com)|*.ps1;*.cmd;*.bat;*.exe;*.com|All files (*.*)|*.*": "اسکریپت‌های پشتیبانی‌شده (*.ps1;*.cmd;*.bat;*.exe;*.com)|*.ps1;*.cmd;*.bat;*.exe;*.com|همهٔ فایل‌ها (*.*)|*.*",
	"Select automation working directory": "انتخاب پوشهٔ کاری خودکارسازی",
	"Select lamp-on script":               "انتخاب اسکریپت روشن‌شدن چراغ",
	"Select lamp-off script":              "انتخاب اسکریپت خاموش‌شدن چراغ",
	"Select message-event script":         "انتخاب اسکریپت رویداد پیام",
	"Select Windows lock script":          "انتخاب اسکریپت قفل ویندوز",
	"Select Windows unlock script":        "انتخاب اسکریپت بازشدن قفل ویندوز",
	"Select system suspend script":        "انتخاب اسکریپت تعلیق سیستم",
	"Select system resume script":         "انتخاب اسکریپت ازسرگیری سیستم",
	"Select network-change script":        "انتخاب اسکریپت تغییر شبکه",
}

var englishDialogStrings = func() map[string]string {
	result := make(map[string]string, len(persianDialogStrings))
	for english, persian := range persianDialogStrings {
		result[persian] = english
	}
	return result
}()

func dialogLocalizer(cfg config.Config) i18n.Localizer {
	preference, err := i18n.ParsePreference(cfg.UI.Locale)
	if err != nil {
		preference = i18n.Auto
	}
	return i18n.New(preference)
}

func dialogLocalizerForForm(owner walk.Form) i18n.Localizer {
	if owner != nil && owner.RightToLeftLayout() {
		return i18n.New(i18n.Persian)
	}
	return i18n.New(i18n.English)
}

func dialogFontFamily(localizer i18n.Localizer) string {
	if localizer.IsRTL() {
		return winfx.PersianFontFamily(false)
	}
	return defaultUIFontFamily
}

type dialogFontKey struct {
	family string
	size   int
	style  walk.FontStyle
}

type dialogFontCache map[dialogFontKey]*walk.Font

// Apply also replaces explicit child fonts (headings, recorder labels), which
// Walk deliberately excludes from inherited parent-font changes. Capture all
// sizes/styles before changing the root so language switches preserve hierarchy.
func (cache dialogFontCache) apply(dialog *walk.Dialog, localizer i18n.Localizer) {
	type target struct {
		window walk.Window
		size   int
		style  walk.FontStyle
	}
	var targets []target
	collect := func(window walk.Window) {
		if nilWalkWindow(window) {
			return
		}
		font := window.Font()
		targets = append(targets, target{window, font.PointSize(), font.Style()})
	}
	collect(dialog)
	walkDialogWidgets(dialog, func(widget walk.Widget) { collect(widget) })
	for _, item := range targets {
		family := defaultUIFontFamily
		if localizer.IsRTL() {
			family = winfx.PersianFontFamily(item.style&walk.FontBold != 0)
		}
		key := dialogFontKey{family, item.size, item.style}
		font := cache[key]
		if font == nil {
			created, err := walk.NewFont(key.family, key.size, key.style)
			if err != nil {
				continue
			}
			font, cache[key] = created, created
		}
		item.window.SetFont(font)
	}
}

func (cache dialogFontCache) dispose() {
	for key, font := range cache {
		font.Dispose()
		delete(cache, key)
	}
}

func dialogText(localizer i18n.Localizer, value string) string {
	canonical := value
	if english, ok := englishDialogStrings[canonical]; ok {
		canonical = english
	}
	if localizer.IsRTL() {
		if translated, ok := persianDialogStrings[canonical]; ok {
			return translated
		}
	}
	return canonical
}

func dialogLabels(localizer i18n.Localizer, labels []string) []string {
	result := make([]string, len(labels))
	for index, label := range labels {
		result[index] = dialogText(localizer, label)
	}
	return result
}

func applySettingsLocalization(
	session *settingsDialogSession,
	cfg config.Config,
) {
	if session == nil || session.dialog == nil ||
		session.dialog.IsDisposed() || session.controls == nil {
		return
	}
	localizer := dialogLocalizer(cfg)
	session.controls.localePreference = cfg.UI.Locale
	wasSyncing := session.syncing.Swap(true)
	defer session.syncing.Store(wasSyncing)

	_ = session.dialog.SetTitle(
		dialogText(localizer, "Settings — Rayan Lamp"),
	)
	if session.localeFonts == nil {
		session.localeFonts = make(dialogFontCache)
	}
	session.localeFonts.apply(session.dialog, localizer)

	controls := session.controls
	setModel := func(control *walk.ComboBox, labels []string) {
		if control == nil {
			return
		}
		index := control.CurrentIndex()
		if err := control.SetModel(dialogLabels(localizer, labels)); err != nil {
			return
		}
		if index >= 0 && index < len(labels) {
			_ = control.SetCurrentIndex(index)
		}
	}
	setModel(controls.viewMode, viewModeLabels)
	setModel(controls.theme, themeLabels)
	setModel(controls.locale, localeLabels)
	setModel(controls.backdrop, backdropLabels)
	setModel(controls.renderBackend, renderBackendLabels)
	setModel(controls.titleBar, titleBarLabels)
	setModel(controls.borderMode, borderModeLabels)
	setModel(controls.delivery, deliveryLabels)
	setModel(controls.soundSource, soundSourceLabels)

	if controls.tabs != nil {
		titles := []string{
			"Lamp",
			"Appearance",
			"Notifications",
			"Hotkeys",
			"Automation",
			"Integration",
		}
		pages := controls.tabs.Pages()
		for index := 0; index < pages.Len() && index < len(titles); index++ {
			_ = pages.At(index).SetTitle(dialogText(localizer, titles[index]))
		}
	}
	if controls.hotkeyRecorders != nil {
		controls.hotkeyRecorders.SetLocalizer(localizer)
	}

	technical := []walk.Window{
		controls.baseURL,
		controls.requestTimeout,
		controls.reconnectMin,
		controls.reconnectMax,
		controls.httpPoll,
		controls.telnetPort,
		controls.snapDistance,
		controls.soundName,
		controls.onScript,
		controls.offScript,
		controls.messageScript,
		controls.lockScript,
		controls.unlockScript,
		controls.suspendScript,
		controls.resumeScript,
		controls.networkScript,
		controls.workingDirectory,
		controls.automationTimeout,
		controls.localAPIListen,
		controls.localAPIToken,
		controls.registrationExecutable,
		hotkeySurface(controls.hotkeyToggle),
		hotkeySurface(controls.hotkeyOn),
		hotkeySurface(controls.hotkeyOff),
		hotkeySurface(controls.hotkeyShow),
	}
	applyDialogTextDirection(session.dialog, localizer, technical...)
	session.dialog.RequestLayout()
	_ = session.dialog.Invalidate()
}

func hotkeySurface(binding *hotkeyRecorderBinding) walk.Window {
	if binding == nil {
		return nil
	}
	return binding.surface
}

func settingsControlText(controls *settingsControls, value string) string {
	if controls == nil {
		return value
	}
	preference, err := i18n.ParsePreference(controls.localePreference)
	if err != nil {
		preference = i18n.Auto
	}
	return dialogText(i18n.New(preference), value)
}

func applyDialogTextDirection(
	dialog *walk.Dialog,
	localizer i18n.Localizer,
	ltrWindows ...walk.Window,
) {
	if dialog == nil || dialog.IsDisposed() {
		return
	}
	rtl := localizer.IsRTL()
	_ = dialog.SetRightToLeftReading(rtl)
	_ = dialog.SetRightToLeftLayout(rtl)

	visit := func(window walk.Window) {
		if nilWalkWindow(window) {
			return
		}
		_ = window.SetRightToLeftReading(rtl)
		if textWindow, ok := window.(interface {
			Text() string
			SetText(string) error
		}); ok {
			current := textWindow.Text()
			translated := dialogText(localizer, current)
			if translated != current {
				_ = textWindow.SetText(translated)
			}
		}
	}
	visit(dialog)
	walkDialogWidgets(dialog, func(widget walk.Widget) { visit(widget) })

	// TabWidget's real SysTabControl32 child is not represented in Walk's
	// widget tree. Propagate only RTL reading order to every native child; the
	// top-level Walk layout owns coordinate mirroring.
	setNativeChildRTLReading(uintptr(dialog.Handle()), rtl)

	for _, window := range ltrWindows {
		if nilWalkWindow(window) {
			continue
		}
		_ = window.SetRightToLeftReading(false)
		setNativeWindowRTLReading(window.Handle(), false)
		setNativeChildRTLReading(uintptr(window.Handle()), false)
		switch control := window.(type) {
		case *walk.LineEdit:
			_ = control.SetTextAlignment(walk.AlignNear)
		case *walk.TextEdit:
			_ = control.SetTextAlignment(walk.AlignNear)
		}
	}
}

func nilWalkWindow(window walk.Window) bool {
	if window == nil {
		return true
	}
	value := reflect.ValueOf(window)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func setNativeChildRTLReading(root uintptr, rtl bool) {
	if root == 0 {
		return
	}
	callback := syscall.NewCallback(func(hwnd win.HWND, _ uintptr) uintptr {
		setNativeWindowRTLReading(hwnd, rtl)
		return 1
	})
	win.EnumChildWindows(win.HWND(root), callback, 0)
}

func setNativeWindowRTLReading(hwnd win.HWND, rtl bool) {
	if hwnd == 0 {
		return
	}
	style := win.GetWindowLong(hwnd, win.GWL_EXSTYLE)
	if rtl {
		style |= win.WS_EX_RTLREADING
	} else {
		style &^= win.WS_EX_RTLREADING
	}
	win.SetWindowLong(hwnd, win.GWL_EXSTYLE, style)
}

func canonicalDialogText(value string) string {
	if english, ok := englishDialogStrings[strings.TrimSpace(value)]; ok {
		return english
	}
	return strings.TrimSpace(value)
}
