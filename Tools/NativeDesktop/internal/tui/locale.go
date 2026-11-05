package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/i18n"
)

type localeConfigMsg struct {
	requestID uint64
	cfg       config.Config
	err       error
}

// persianTUIText keeps TUI-only copy together while shared cross-surface
// messages remain in internal/i18n's typed catalog.
var persianTUIText = map[string]string{
	"PCController":         "PCController",
	"PCCONTROLLER_DESKTOP": "PCController",
	"Terminal UI ready":    "رابط پایانه آماده است",

	"On": "روشن", "Off": "خاموش", "Unknown": "نامشخص",
	"Offline": "آفلاین", "Connecting": "در حال اتصال",
	"Connected": "متصل", "Disconnected": "قطع‌شده",
	"Controller offline": "هماهنگ‌کننده آفلاین است",
	"HTTP fallback":      "جایگزین HTTP",
	"LAST KNOWN":         "آخرین وضعیت معلوم",
	"Connection":         "اتصال", "Transport": "مسیر ارتباطی",
	"Device": "دستگاه", "Source": "منبع", "Changed": "تغییر",
	"Activity": "فعالیت", "ready": "آماده",
	"STATUS & CONTROL": "وضعیت و کنترل",
	"DEVICE":           "دستگاه", "DEVICE CONTROL": "کنترل دستگاه",
	"RECENT ACTIVITY":                         "فعالیت‌های اخیر",
	"click or press space":                    "کلیک کنید یا Space را بزنید",
	"One device command is already in flight": "یک فرمان دستگاه هم‌اکنون در حال اجراست",

	"Toggle": "تغییر وضعیت", "Turn on": "روشن کردن",
	"Turn off": "خاموش کردن", "Refresh": "تازه‌سازی",
	"Reconnect":     "اتصال دوباره",
	"SPACE  TOGGLE": "SPACE  تغییر وضعیت",
	"O  ON":         "O  روشن", "F  OFF": "F  خاموش",
	"R  REFRESH": "R  تازه‌سازی", "C  RECONNECT": "C  اتصال دوباره",
	"T TOGGLE": "T تغییر وضعیت", "O ON": "O روشن", "F OFF": "F خاموش",
	"Reconnect requested":         "درخواست اتصال دوباره ارسال شد",
	"Main window shown":           "پنجرهٔ اصلی نمایش داده شد",
	"Main window hidden":          "پنجرهٔ اصلی پنهان شد",
	"Native settings opened":      "تنظیمات بومی باز شد",
	"Native diagnostics opened":   "عیب‌یابی بومی باز شد",
	"Web dashboard opened":        "داشبورد وب باز شد",
	"Notification test requested": "آزمایش اعلان درخواست شد",
	"Audible cue requested":       "نشانهٔ صوتی درخواست شد",
	"Primary is shutting down":    "هماهنگ‌کنندهٔ اصلی در حال خروج است",
	"Command completed":           "فرمان انجام شد",

	"COMMAND CENTER":  "مرکز فرمان",
	"Command palette": "فهرست فرمان‌ها",
	"Device · app · serving · configuration · device":                                       "دستگاه · برنامه · سرویس · پیکربندی · دستگاه",
	"Search with / · every operation is routed through the active coordinator.":             "با / جست‌وجو کنید؛ همهٔ عملیات از هماهنگ‌کنندهٔ فعال عبور می‌کنند.",
	"CONFIRM DESTRUCTIVE COMMAND":                                                           "تأیید فرمان حساس",
	"Quit the active PCControllerDesktop primary?":                                          "هماهنگ‌کنندهٔ فعال PCController بسته شود؟",
	"This stops the GUI/headless coordinator, WebSocket, local API, tray, and automations.": "این کار رابط گرافیکی یا هماهنگ‌کنندهٔ بدون رابط، WebSocket، API محلی، سینی و خودکارسازی‌ها را متوقف می‌کند.",
	"Quit primary":        "خروج از هماهنگ‌کننده",
	"Cancel":              "انصراف",
	"RUNNING":             "در حال اجرا",
	"COMMAND RESULT":      "نتیجهٔ فرمان",
	"Please wait":         "لطفاً صبر کنید",
	"Routing through %s…": "در حال ارسال از مسیر %s…",
	"↑/↓ choose · / filter · enter run · esc dashboard": "↑/↓ انتخاب · / جست‌وجو · Enter اجرا · Esc داشبورد",
	"←/→ choose · enter confirm · esc cancel":           "←/→ انتخاب · Enter تأیید · Esc انصراف",
	"enter/esc commands · q dashboard":                  "Enter/Esc فرمان‌ها · Q داشبورد",

	"💡 Device · Toggle":                  "💡 دستگاه · تغییر وضعیت",
	"☀  Device · Turn on":                "☀  دستگاه · روشن کردن",
	"🌙 Device · Turn off":                "🌙 دستگاه · خاموش کردن",
	"↻  Device · Refresh state":          "↻  دستگاه · تازه‌سازی وضعیت",
	"⟳  Device · Reconnect":              "⟳  دستگاه · اتصال دوباره",
	"▣  App · Show main window":          "▣  برنامه · نمایش پنجرهٔ اصلی",
	"—  App · Hide main window":          "—  برنامه · پنهان کردن پنجرهٔ اصلی",
	"⚙  App · Open native settings":      "⚙  برنامه · باز کردن تنظیمات بومی",
	"🩺 App · Open native diagnostics":    "🩺 برنامه · باز کردن عیب‌یابی بومی",
	"🌐 App · Open Web dashboard":         "🌐 برنامه · باز کردن داشبورد وب",
	"🔔 App · Test notification":          "🔔 برنامه · آزمایش اعلان",
	"♪  App · Test audible cue":          "♪  برنامه · آزمایش نشانهٔ صوتی",
	"🧰 Configure · Application settings": "🧰 پیکربندی · تنظیمات برنامه",
	"📡 Configure · Serving":              "📡 پیکربندی · سرویس‌دهی",
	"●  Serving · Status":                "●  سرویس‌دهی · وضعیت",
	"▶  Serving · Enable":                "▶  سرویس‌دهی · فعال‌سازی",
	"■  Serving · Disable":               "■  سرویس‌دهی · غیرفعال‌سازی",
	"🔧 Device · Parameters":              "🔧 دستگاه · پارامترها",
	"⌨  Device · Open terminal":          "⌨  دستگاه · باز کردن پایانه",
	"🔌 Device · Connect terminal":        "🔌 دستگاه · اتصال پایانه",
	"⏏  Device · Disconnect terminal":    "⏏  دستگاه · قطع پایانه",
	"🔎 Diagnostics · Fetch endpoint":     "🔎 عیب‌یابی · دریافت نقطهٔ پایانی",
	"⌂  Navigation · Dashboard":          "⌂  پیمایش · داشبورد",
	"⏻  App · Quit primary":              "⏻  برنامه · خروج از هماهنگ‌کننده",

	"APPLICATION SETTINGS":                                              "تنظیمات برنامه",
	"LOADING APPLICATION SETTINGS":                                      "در حال بارگیری تنظیمات برنامه",
	"Choose a configuration workspace":                                  "یک بخش پیکربندی را انتخاب کنید",
	"Each Apply is validated, saved atomically, and synchronized live.": "هر اعمال، اعتبارسنجی و به‌صورت اتمی ذخیره می‌شود و هم‌زمان همگام خواهد شد.",
	"💡 Device connection & recovery":                                    "💡 اتصال و بازیابی دستگاه",
	"🎨 Appearance & rendering":                                          "🎨 ظاهر و رندر",
	"🪟 Window behavior & placement":                                     "🪟 رفتار و جایگاه پنجره",
	"🔔 Notifications & sound":                                           "🔔 اعلان‌ها و صدا",
	"⌨  Global hotkeys":                                                 "⌨  میان‌برهای سراسری",
	"⚙  Automation scripts":                                             "⚙  اسکریپت‌های خودکارسازی",
	"🌐 Serving & Windows integration":                                   "🌐 سرویس‌دهی و یکپارچه‌سازی ویندوز",
	"🩺 Diagnostics policy (read only)":                                  "🩺 سیاست عیب‌یابی (فقط خواندنی)",
	"🧰 Complete JSON editor":                                            "🧰 ویرایشگر کامل JSON",
	"✓  Return to dashboard":                                            "✓  بازگشت به داشبورد",
	"TUI-native workspace":                                              "محیط بومی رابط پایانه",
	"Reading authoritative configuration…":                              "در حال خواندن پیکربندی مرجع…",
	"Validating, saving, and synchronizing…":                            "در حال اعتبارسنجی، ذخیره و همگام‌سازی…",
	"esc close":                 "Esc بستن",
	"enter/r retry · esc close": "Enter/R تلاش دوباره · Esc بستن",
	"↑/↓ choose · / filter · enter open · esc close":                  "↑/↓ انتخاب · / جست‌وجو · Enter باز کردن · Esc بستن",
	"tab/enter next · shift+tab back · ctrl+s apply · esc categories": "Tab/Enter بعدی · Shift+Tab قبلی · Ctrl+S اعمال · Esc دسته‌ها",
	"scroll/read · esc categories":                                    "پیمایش/خواندن · Esc دسته‌ها",
	"No configuration changes to apply":                               "تغییری برای اعمال وجود ندارد",
	"Configuration applied live":                                      "پیکربندی بی‌درنگ اعمال شد",
	"Windows registration updated for the current executable":         "ثبت ویندوز برای فایل اجرایی جاری به‌روز شد",

	"Device connection": "اتصال دستگاه", "Appearance": "ظاهر",
	"Window behavior": "رفتار پنجره", "Notifications": "اعلان‌ها",
	"Global hotkeys": "میان‌برهای سراسری", "Automation": "خودکارسازی",
	"Serving & integration": "سرویس‌دهی و یکپارچه‌سازی",
	"Diagnostics policy":    "سیاست عیب‌یابی", "Complete JSON": "JSON کامل",
	"Settings": "تنظیمات",
	"Ctrl+S applies this section immediately · Esc discards": "Ctrl+S این بخش را بی‌درنگ اعمال می‌کند · Esc نادیده می‌گیرد",

	"Language": "زبان",
	"Choose the language used by every PCControllerDesktop surface.": "زبان همهٔ بخش‌های PCController را انتخاب کنید.",
	"System default": "پیش‌فرض سیستم", "English": "انگلیسی", "Persian": "فارسی",
	"View": "نما", "Compact device or larger dashboard.": "دستگاه فشرده یا داشبورد بزرگ‌تر.",
	"Theme": "پوسته", "Follow Windows or force a palette.": "پیروی از ویندوز یا انتخاب اجباری رنگ‌بندی.",
	"Backdrop": "پس‌زمینه", "Windows composition material.": "جنس ترکیب‌بندی ویندوز.",
	"Rendering backend": "موتور رندر",
	"Automatic prefers D3D11/DirectComposition. Compatibility retains every UI feature while using less private memory.": "حالت خودکار D3D11/DirectComposition را ترجیح می‌دهد. حالت سازگار همهٔ امکانات رابط را با مصرف حافظهٔ خصوصی کمتر حفظ می‌کند.",
	"Rounded corners": "گوشه‌های گرد", "Use the platform corner preference.": "از تنظیم گوشه‌های سیستم استفاده شود.",
	"Layered foreground": "پیش‌زمینهٔ لایه‌ای",
	"Alpha-composited glass styling; independent of the presentation backend.": "استایل شیشه‌ای با ترکیب آلفا، مستقل از موتور نمایش.",
	"Title bar": "نوار عنوان", "None, macOS-style custom, or native.": "بدون نوار، سفارشی شبیه macOS یا بومی.",
	"Border": "حاشیه", "Automatic native frame or no border.": "قاب بومی خودکار یا بدون حاشیه.",
	"Animations": "پویانمایی‌ها", "Transitions, button feedback, and fades.": "گذارها، بازخورد دکمه و محوشدن.",

	"Device base URL": "نشانی پایهٔ دستگاه",
	"HTTP(S) origin; `.local` is resolved and cached by the coordinator.": "مبدأ HTTP(S)؛ نشانی `.local` توسط هماهنگ‌کننده حل و ذخیره می‌شود.",
	"Request timeout": "مهلت درخواست", "WebSocket events": "رویدادهای WebSocket",
	"Listen on /ws and reconnect with backoff.": "به /ws گوش دهد و با وقفهٔ افزایشی دوباره متصل شود.",
	"Reconnect minimum":                         "کمینهٔ اتصال دوباره", "Reconnect maximum": "بیشینهٔ اتصال دوباره",
	"At least the minimum; at most 15m.":    "دست‌کم برابر کمینه و حداکثر ۱۵ دقیقه.",
	"HTTP fallback poll":                    "بازبینی جایگزین HTTP",
	"0 disables polling; otherwise 1s–24h.": "صفر بازبینی را غیرفعال می‌کند؛ در غیر این صورت ۱ ثانیه تا ۲۴ ساعت.",
	"Telnet port":                           "درگاه Telnet", "Device terminal port, 1–65535.": "درگاه پایانهٔ دستگاه، از ۱ تا ۶۵۵۳۵.",

	"Always on top": "همیشه روی پنجره‌ها", "Keep the device above ordinary windows.": "دستگاه بالاتر از پنجره‌های معمولی بماند.",
	"Close to tray": "بستن به سینی", "Close hides the window while the coordinator remains active.": "بستن، پنجره را پنهان می‌کند و هماهنگ‌کننده فعال می‌ماند.",
	"Prewarm tray menu": "آماده‌سازی منوی سینی", "Keep the acrylic menu ready to avoid first-open flash.": "منوی اکریلیک آماده بماند تا بار اول چشمک نزند.",
	"Start minimized": "شروع کمینه", "Begin in the notification area.": "برنامه در ناحیهٔ اعلان آغاز شود.",
	"Window visible": "پنجره نمایان", "Restore the last visible/hidden state.": "آخرین حالت نمایان/پنهان بازیابی شود.",
	"Saved show state": "حالت نمایش ذخیره‌شده", "State restored on the next GUI launch.": "در اجرای بعدی رابط گرافیکی بازیابی می‌شود.",
	"Show in taskbar": "نمایش در نوار وظیفه", "Expose the main window in the Windows taskbar.": "پنجرهٔ اصلی در نوار وظیفهٔ ویندوز نمایش داده شود.",
	"Magnetic snap": "چسبندگی مغناطیسی", "Snap near monitor work-area edges; Ctrl bypasses it.": "نزدیک لبه‌های نمایشگر بچسبد؛ Ctrl موقتاً آن را دور می‌زند.",
	"Snap distance": "فاصلهٔ چسبندگی", "Last settings tab": "آخرین زبانهٔ تنظیمات",
	"Saved bounds": "ابعاد ذخیره‌شده",
	"Width":        "عرض", "Height": "ارتفاع",

	"Master notification switch.": "کلید اصلی اعلان‌ها.",
	"State changes":               "تغییر وضعیت", "Notify when device state changes.": "هنگام تغییر وضعیت دستگاه اعلان دهد.",
	"Initial sync": "همگام‌سازی نخستین", "Notify after the first confirmed state.": "پس از نخستین وضعیت تأییدشده اعلان دهد.",
	"Custom messages": "پیام‌های سفارشی", "Render `message:` device events.": "رویدادهای `message:` دستگاه نمایش داده شوند.",
	"Audible cues": "نشانه‌های صوتی", "Play cues independently of toast delivery.": "صدا مستقل از نمایش اعلان پخش شود.",
	"Sound source": "منبع صدا", "Import custom WAV": "وارد کردن WAV سفارشی",
	"Custom sound asset": "دارایی صدای سفارشی", "Custom sound name": "نام صدای سفارشی",
	"Delivery": "روش ارسال", "Notifications & sound": "اعلان‌ها و صدا",

	"Show / hide": "نمایش / پنهان‌سازی",
	"Native settings also offers live key recording · Ctrl+S applies": "تنظیمات بومی امکان ضبط زندهٔ کلید را هم دارد · Ctrl+S اعمال",

	"Start with Windows": "اجرا با ویندوز",
	"Local API / Web UI": "API محلی / رابط وب", "Listen address": "نشانی گوش‌دادن",
	"Rotate local API token":                      "تعویض توکن API محلی",
	"Leave blank to preserve the current secret.": "برای حفظ رمز جاری خالی بگذارید.",

	"DEVICE SETTINGS":           "تنظیمات دستگاه",
	"LOADING DEVICE PARAMETERS": "در حال بارگیری پارامترهای دستگاه",
	"Typed, sanitized projection · passwords and raw /config bodies never enter this screen": "نمایش ساخت‌یافته و پاک‌سازی‌شده؛ گذرواژه‌ها و بدنهٔ خام /config هرگز وارد این صفحه نمی‌شوند",
	"Device actions": "عملیات دستگاه",
	"↻ Refresh all audited device parameters": "↻ تازه‌سازی همهٔ پارامترهای بررسی‌شده",
	"🔌 Connect / probe firmware terminal":     "🔌 اتصال / بررسی پایانهٔ میان‌افزار",
	"⌨ Open expert device terminal":           "⌨ باز کردن پایانهٔ تخصصی دستگاه",
	"💡 Edit coordinator device connection":    "💡 ویرایش اتصال دستگاه در هماهنگ‌کننده",
	"← Return to dashboard":                   "← بازگشت به داشبورد",
	"Reading sanitized coordinator cache…":    "در حال خواندن حافظهٔ پاک‌سازی‌شدهٔ هماهنگ‌کننده…",
	"Coordinating device operation…":          "در حال هماهنگ‌سازی عملیات دستگاه…",
	"Not reported":                            "گزارش نشده", "Yes": "بله", "No": "خیر",
	"Hostname": "نام میزبان", "Model": "مدل", "Station SSID": "SSID ایستگاه",
	"Wi-Fi mode": "حالت Wi-Fi", "Station status": "وضعیت ایستگاه",
	"DHCP": "DHCP", "Station IP": "IP ایستگاه", "Gateway / DNS": "دروازه / DNS",
	"Access point": "نقطهٔ دسترسی", "AP status": "وضعیت AP", "AP address": "نشانی AP",
	"Firmware": "میان‌افزار", "Firmware hash": "هش میان‌افزار", "Build": "ساخت",
	"Uptime": "زمان کارکرد", "Protocol": "پروتکل", "Updated": "به‌روزرسانی",

	"DIAGNOSTICS · READ ONLY":                          "عیب‌یابی · فقط خواندنی",
	"DIAGNOSTICS · FETCH":                              "عیب‌یابی · دریافت",
	"DIAGNOSTICS · COULD NOT FETCH":                    "عیب‌یابی · دریافت ناموفق",
	"Endpoint":                                         "نقطهٔ پایانی",
	"Routed through %s · allow-list enforced":          "ارسال از مسیر %s · فهرست مجاز اعمال می‌شود",
	"enter fetch  ·  esc close":                        "Enter دریافت · Esc بستن",
	"Contacting active transport…":                     "در حال تماس با مسیر ارتباطی فعال…",
	"↑/↓/pgup/pgdn scroll  ·  d another  ·  esc close": "↑/↓/PgUp/PgDn پیمایش · D مورد دیگر · Esc بستن",

	"DEVICE TERMINAL": "پایانهٔ دستگاه",
	"CONNECTING":      "در حال اتصال", "DISCONNECTING": "در حال قطع",
	"DISCONNECTED": "قطع‌شده", "configured device": "دستگاه پیکربندی‌شده",
	"read only":                  "فقط خواندنی",
	"Waiting for device output…": "در انتظار خروجی دستگاه…",
	"No active session · press Ctrl+R to connect":                                        "نشست فعالی نیست · برای اتصال Ctrl+R را بزنید",
	"Raw keyboard input is sent to the device":                                           "ورودی خام صفحه‌کلید به دستگاه فرستاده می‌شود",
	"ShellOnSerial · output only":                                                        "ShellOnSerial · فقط خروجی",
	"Esc / Ctrl+] detach · Ctrl+C sends ETX · Ctrl+D disconnect · Ctrl+R / F5 reconnect": "Esc / Ctrl+] جداسازی · Ctrl+C ارسال ETX · Ctrl+D قطع · Ctrl+R / F5 اتصال دوباره",
	"Opening the app-owned terminal session…":                                            "در حال باز کردن نشست پایانهٔ برنامه…",
	"Reconnecting device terminal…":                                                      "در حال اتصال دوبارهٔ پایانهٔ دستگاه…",
	"Disconnecting device terminal…":                                                     "در حال قطع پایانهٔ دستگاه…",
	"Connection failed · Ctrl+R retries":                                                 "اتصال ناموفق بود · Ctrl+R دوباره تلاش می‌کند",
	"Fresh terminal session connected":                                                   "نشست تازهٔ پایانه متصل شد",
	"Interactive session attached":                                                       "نشست تعاملی پیوست شد",
	"Disconnect failed":                                                                  "قطع اتصال ناموفق بود",
	"Disconnected explicitly · Ctrl+R reconnects":                                        "اتصال صریحاً قطع شد · Ctrl+R دوباره متصل می‌کند",
	"Session changed · transcript fenced":                                                "نشست تغییر کرد · متن خروجی جدا شد",
	"Scrollback cursor expired · view resynchronized":                                    "نشانگر پیشینه منقضی شد · نما دوباره همگام شد",
	"Invalid terminal output received":                                                   "خروجی نامعتبر پایانه دریافت شد",
	"Local display trimmed to the newest 512 KiB":                                        "نمای محلی به تازه‌ترین ۵۱۲ KiB محدود شد",
	"Device terminal disconnected · Ctrl+R reconnects":                                   "پایانهٔ دستگاه قطع شد · Ctrl+R دوباره متصل می‌کند",
	"New terminal session · previous output cleared":                                     "نشست تازهٔ پایانه · خروجی قبلی پاک شد",

	"-1 asks Windows to center.":       "-۱ از ویندوز می‌خواهد پنجره را وسط‌چین کند.",
	": commands · s settings · q quit": ": فرمان‌ها · S تنظیمات · Q خروج",
	"%d pulse(s)":                      "%d پالس",
	"↑/↓ choose · pgup/pgdn info · enter run · r refresh · x terminal · esc": "↑/↓ انتخاب · PgUp/PgDn اطلاعات · Enter اجرا · R تازه‌سازی · X پایانه · Esc",
	"○ Not registered for this exact executable":                             "○ برای همین فایل اجرایی ثبت نشده",
	"✓ Registered for this exact executable":                                 "✓ برای همین فایل اجرایی ثبت شده",
	"1–128 physical-independent pixels.":                                     "۱ تا ۱۲۸ پیکسل مستقل از مقیاس.",
	"100ms–5m":                                                               "۱۰۰ میلی‌ثانیه تا ۵ دقیقه",
	"1s–10m per invocation.":                                                 "۱ ثانیه تا ۱۰ دقیقه برای هر اجرا.",
	"accepted; awaiting state confirmation":                                  "پذیرفته شد؛ در انتظار تأیید وضعیت",
	"Advanced JSON editor":                                                   "ویرایشگر پیشرفتهٔ JSON",
	"Applied":                                                                "اعمال شد",
	"applying":                                                               "در حال اعمال",
	"Audited device parameters refreshed":                                    "پارامترهای بررسی‌شدهٔ دستگاه تازه‌سازی شدند",
	"Automatic modern toast with legacy balloon fallback.":                   "اعلان مدرن خودکار با بازگشت به بالن قدیمی.",
	"Back to categories":                                                     "بازگشت به دسته‌ها",
	"Blank inherits the application directory.":                              "خالی، پوشهٔ برنامه را به ارث می‌برد.",
	"bytes":        "بایت",
	"bytes queued": "بایت در صف",
	"Capture and colorize child stdout/stderr.": "خروجی استاندارد و خطای فرایند فرزند دریافت و رنگی شود.",
	"Capture output":                             "دریافت خروجی",
	"Choose action":                              "انتخاب عملیات",
	"choose category":                            "انتخاب دسته",
	"COM automation":                             "خودکارسازی COM",
	"Compiled diagnostics policy":                "سیاست عیب‌یابی کامپایل‌شده",
	"Complete PCControllerDesktop configuration": "پیکربندی کامل PCController",
	"completed in":                               "انجام شد در",
	"Confirm ownership-safe unregistration":      "تأیید لغو ثبت امن از نظر مالکیت",
	"confirm quit · esc cancel":                  "تأیید خروج · Esc انصراف",
	"Ctrl+S applies all pages in this section · Esc discards": "Ctrl+S همهٔ صفحه‌های این بخش را اعمال می‌کند · Esc نادیده می‌گیرد",
	"ctrl+s apply":                    "Ctrl+S اعمال",
	"Current executable registration": "ثبت فایل اجرایی جاری",
	"Device is offline; cached sanitized values may be shown.": "دستگاه آفلاین است؛ ممکن است مقادیر پاک‌سازی‌شدهٔ ذخیره‌شده نمایش داده شوند.",
	"Device terminal did not enter the connected state":        "پایانهٔ دستگاه وارد وضعیت متصل نشد",
	"Device terminal disconnected · input queue cleared":       "پایانهٔ دستگاه قطع شد · صف ورودی پاک شد",
	"Device terminal is not connected · Ctrl+R reconnects":     "پایانهٔ دستگاه متصل نیست · Ctrl+R دوباره متصل می‌کند",
	"Disabled":                             "غیرفعال",
	"Enabled":                              "فعال",
	"error":                                "خطا",
	"Example: Ctrl+Alt+L; blank disables.": "نمونه: Ctrl+Alt+L؛ خالی یعنی غیرفعال.",
	"Example: Ctrl+Alt+Shift+F; blank disables.": "نمونه: Ctrl+Alt+Shift+F؛ خالی یعنی غیرفعال.",
	"Example: Ctrl+Alt+Shift+L; blank disables.": "نمونه: Ctrl+Alt+Shift+L؛ خالی یعنی غیرفعال.",
	"Example: Ctrl+Alt+Shift+O; blank disables.": "نمونه: Ctrl+Alt+Shift+O؛ خالی یعنی غیرفعال.",
	"Executable":                                     "فایل اجرایی",
	"Executable/script invoked after OFF.":           "فایل اجرایی/اسکریپت پس از خاموش‌شدن اجرا می‌شود.",
	"Executable/script invoked after ON.":            "فایل اجرایی/اسکریپت پس از روشن‌شدن اجرا می‌شود.",
	"Executable/script invoked for custom messages.": "فایل اجرایی/اسکریپت برای پیام‌های سفارشی اجرا می‌شود.",
	"Execution policy":                               "سیاست اجرا",
	"Expert endpoint policy":                         "سیاست نقاط پایانی تخصصی",
	"Explicit loopback host:port.":                   "میزبان:درگاه صریح روی loopback.",
	"failed":                                         "ناموفق",
	"FETCHING":                                       "در حال دریافت",
	"firmware shell is on Serial (read only over network)":                                 "پوستهٔ میان‌افزار روی Serial است (شبکه فقط خواندنی)",
	"HTTP diagnostics available. Connect the terminal to probe optional parameter writes.": "عیب‌یابی HTTP در دسترس است. برای بررسی نوشتن پارامترهای اختیاری، پایانه را متصل کنید.",
	"HTTP polling active":                                                      "بازبینی HTTP فعال",
	"I understand":                                                             "متوجه‌ام",
	"Invoked after a host network change.":                                     "پس از تغییر شبکهٔ میزبان اجرا می‌شود.",
	"Invoked after Windows resumes.":                                           "پس از ادامهٔ کار ویندوز اجرا می‌شود.",
	"Invoked when Windows is entering suspend.":                                "هنگام ورود ویندوز به تعلیق اجرا می‌شود.",
	"Invoked when Windows locks the session.":                                  "هنگام قفل‌شدن نشست ویندوز اجرا می‌شود.",
	"Invoked when Windows unlocks the session.":                                "هنگام بازشدن قفل نشست ویندوز اجرا می‌شود.",
	"JSON object · token is masked · Ctrl+S validates and atomically applies.": "شیء JSON · توکن پوشانده شده · Ctrl+S اعتبارسنجی و اتمی اعمال می‌کند.",
	"Device & message events":                                                  "رویدادهای دستگاه و پیام",
	"Device is":                                                                "دستگاه",
	"Listener changes migrate after Apply · Ctrl+S applies":                    "تغییر شنونده پس از اعمال جابه‌جا می‌شود · Ctrl+S اعمال",
	"load failed · enter retry":                                                "بارگیری ناموفق · Enter تلاش دوباره",
	"loading configuration":                                                    "در حال بارگیری پیکربندی",
	"loading parameters":                                                       "در حال بارگیری پارامترها",
	"Local API/Web UI configured at":                                           "API محلی/رابط وب پیکربندی‌شده در",
	"Local API/Web UI configured but unavailable":                              "API محلی/رابط وب پیکربندی شده ولی در دسترس نیست",
	"Local API/Web UI disabled":                                                "API محلی/رابط وب غیرفعال است",
	"Local API/Web UI disabled; active requests drain gracefully":              "API محلی/رابط وب غیرفعال شد؛ درخواست‌های فعال با آرامش پایان می‌یابند",
	"Local API/Web UI enabled at":                                              "API محلی/رابط وب فعال در",
	"Local API/Web UI enabled; listener is starting":                           "API محلی/رابط وب فعال است؛ شنونده در حال شروع است",
	"Local API/Web UI running at":                                              "API محلی/رابط وب در حال اجرا در",
	"Local API/Web UI stopped gracefully":                                      "API محلی/رابط وب به‌آرامی متوقف شد",
	"Lock script":                                                              "اسکریپت قفل",
	"Message script":                                                           "اسکریپت پیام",
	"Native settings page index, 0–5.":                                         "شمارهٔ صفحهٔ تنظیمات بومی، ۰ تا ۵.",
	"Network-change script":                                                    "اسکریپت تغییر شبکه",
	"No registration change":                                                   "بدون تغییر ثبت",
	"Not confirmed":                                                            "تأیید نشده",
	"Not reported by audited HTTP firmware":                                    "توسط میان‌افزار HTTP بررسی‌شده گزارش نشده",
	"Off script":                                                               "اسکریپت خاموش",
	"On script":                                                                "اسکریپت روشن",
	"One-time registration action":                                             "عملیات یک‌بارهٔ ثبت",
	"Optional local PCM WAV path; Apply imports it through the active GUI/headless primary.": "مسیر اختیاری WAV PCM محلی؛ اعمال آن را از طریق هماهنگ‌کنندهٔ فعال وارد می‌کند.",
	"Page 1 of 3 · Ctrl+S applies every automation page · Esc discards":                      "صفحهٔ ۱ از ۳ · Ctrl+S همهٔ صفحه‌های خودکارسازی را اعمال می‌کند · Esc نادیده می‌گیرد",
	"Page 2 of 2 · actions target the exact executable shown above":                          "صفحهٔ ۲ از ۲ · عملیات دقیقاً فایل اجرایی بالا را هدف می‌گیرد",
	"Page 2 of 3 · Blank disables an individual event · Ctrl+S applies all pages":            "صفحهٔ ۲ از ۳ · خالی هر رویداد را غیرفعال می‌کند · Ctrl+S همهٔ صفحه‌ها را اعمال می‌کند",
	"Page 3 of 3 · Ctrl+S applies every automation page · Esc discards":                      "صفحهٔ ۳ از ۳ · Ctrl+S همهٔ صفحه‌های خودکارسازی را اعمال می‌کند · Esc نادیده می‌گیرد",
	"pending": "در انتظار",
	"Read only: this firmware routes its shell to Serial. HTTP parameters remain safely viewable.": "فقط خواندنی: این میان‌افزار پوسته را به Serial می‌فرستد. پارامترهای HTTP با ایمنی قابل مشاهده‌اند.",
	"Reader-facing name shared by native and Web settings.":                                        "نام نمایشی مشترک میان تنظیمات بومی و وب.",
	"Register COM automation for this executable":                                                  "ثبت خودکارسازی COM برای این فایل اجرایی",
	"Register the unified executable for the current user.":                                        "فایل اجرایی یکپارچه برای کاربر جاری ثبت شود.",
	"Register URI scheme + Jump List for this executable":                                          "ثبت طرح URI و Jump List برای این فایل اجرایی",
	"result · enter commands":                                                                      "نتیجه · Enter فرمان‌ها",
	"Resume script":                                                                                "اسکریپت ادامه",
	"Run scripts after device and Windows lifecycle events.":                                       "پس از رویدادهای دستگاه و چرخهٔ عمر ویندوز اسکریپت اجرا شود.",
	"Safe automatic endpoints":                                                                     "نقاط پایانی خودکار امن",
	"selected":                                                                                     "انتخاب‌شده",
	"Sending input":                                                                                "در حال ارسال ورودی",
	"sending…":                                                                                     "در حال ارسال…",
	"sent · awaiting confirmed state":                                                              "ارسال شد · در انتظار وضعیت تأییدشده",
	"Serve the coordinator and bundled dashboard on loopback.":                                     "هماهنگ‌کننده و داشبورد همراه روی loopback سرویس داده شوند.",
	"session": "نشست",
	"SHA-256 ID imported by the native or Web settings surface.": "شناسهٔ SHA-256 واردشده توسط تنظیمات بومی یا وب.",
	"Shell URI + Jump List": "URI پوسته و Jump List",
	"space toggle · d info · x terminal · ? help · q quit": "Space تغییر · D اطلاعات · X پایانه · ? راهنما · Q خروج",
	"State refreshed":              "وضعیت تازه شد",
	"Status has not been queried.": "وضعیت هنوز پرس‌وجو نشده است.",
	"Status unavailable:":          "وضعیت در دسترس نیست:",
	"Suspend script":               "اسکریپت تعلیق",
	"t toggle · o on · f off":      "T تغییر · O روشن · F خاموش",
	"Terminal connected; waiting for a capability banner before allowing writes.": "پایانه متصل است؛ پیش از اجازهٔ نوشتن منتظر اعلان قابلیت است.",
	"Terminal input queue is full":                       "صف ورودی پایانه پر است",
	"Terminal theme":                                     "پوستهٔ پایانه",
	"This screen is intentionally read only":             "این صفحه عمداً فقط خواندنی است",
	"This transport does not expose diagnostics":         "این مسیر ارتباطی عیب‌یابی را ارائه نمی‌کند",
	"This transport does not expose diagnostics.":        "این مسیر ارتباطی عیب‌یابی را ارائه نمی‌کند.",
	"This transport does not expose the device terminal": "این مسیر ارتباطی پایانهٔ دستگاه را ارائه نمی‌کند",
	"Timeout": "مهلت",
	"Unclassified device frame received · %d bytes": "فریم دسته‌بندی‌نشدهٔ دستگاه دریافت شد · %d بایت",
	"Unlock script": "اسکریپت بازکردن قفل",
	"Unregister COM automation owned by this executable":                "لغو ثبت خودکارسازی COM متعلق به این فایل اجرایی",
	"Unregister URI scheme + Jump List owned by this executable":        "لغو ثبت طرح URI و Jump List متعلق به این فایل اجرایی",
	"Use the shared bundled catalog or a content-addressed custom WAV.": "از مجموعهٔ همراه مشترک یا WAV سفارشی مبتنی بر محتوا استفاده شود.",
	"Validated against the selected view mode.":                         "با حالت نمای انتخابی اعتبارسنجی می‌شود.",
	"via":                      "از مسیر",
	"Windows lifecycle events": "رویدادهای چرخهٔ عمر ویندوز",
	"Windows shell & automation registration": "ثبت پوسته و خودکارسازی ویندوز",
	"Working directory":                       "پوشهٔ کاری",
	"Writable shell detected; structured mutation remains locked until credential-safe firmware support is present.":           "پوستهٔ قابل نوشتن شناسایی شد؛ تغییر ساخت‌یافته تا پشتیبانی امن میان‌افزار از اعتبارنامه قفل می‌ماند.",
	"Portable mode stays unregistered unless you explicitly choose an action. Nothing here is replayed on startup.":            "حالت قابل‌حمل تا زمانی که صریحاً عملیاتی را انتخاب نکنید ثبت نمی‌شود. هیچ‌یک از این موارد هنگام شروع دوباره اجرا نمی‌شود.",
	"Required only when unregistering. PCControllerDesktop rechecks every owned registry view before removing shared entries.": "فقط هنگام لغو ثبت لازم است. PCController پیش از حذف ورودی‌های مشترک، همهٔ نماهای رجیستری متعلق به خود را دوباره بررسی می‌کند.",
	"Sensitive /config projections are sanitized before retention. Non-device ASA routes remain excluded by compiled policy.":  "خروجی‌های حساس /config پیش از نگه‌داری پاک‌سازی می‌شوند. مسیرهای غیردستگاهی ASA طبق سیاست کامپایل‌شده همچنان کنار گذاشته می‌شوند.",

	"Toggle using the confirmed coordinator state":       "تغییر وضعیت با استفاده از وضعیت تأییدشدهٔ هماهنگ‌کننده",
	"Request /device/on":                                 "درخواست /device/on",
	"Request /device/off":                                "درخواست /device/off",
	"Read /device/get now":                               "اکنون /device/get خوانده شود",
	"Restart WebSocket and HTTP recovery":                "راه‌اندازی دوبارهٔ WebSocket و بازیابی HTTP",
	"Bring the existing GUI forward":                     "رابط گرافیکی موجود به جلو آورده شود",
	"Keep the coordinator in the tray":                   "هماهنگ‌کننده در سینی باقی بماند",
	"Open the Win32 settings window":                     "باز کردن پنجرهٔ تنظیمات Win32",
	"Open the native diagnostics window":                 "باز کردن پنجرهٔ بومی عیب‌یابی",
	"Open the active local dashboard":                    "باز کردن داشبورد محلی فعال",
	"Exercise configured toast/balloon delivery":         "آزمایش ارسال Toast/Balloon پیکربندی‌شده",
	"Play audio independently of notification delivery":  "پخش صدا مستقل از ارسال اعلان",
	"Open the complete TUI-native editor":                "باز کردن ویرایشگر کامل بومی رابط پایانه",
	"Local API, PWA, token, and Windows integration":     "API محلی، PWA، توکن و یکپارچه‌سازی ویندوز",
	"Show observed listener state when this TUI owns it": "نمایش وضعیت مشاهده‌شدهٔ شنونده هنگامی که در اختیار این رابط است",
	"Persist and start the loopback coordinator":         "ذخیره و آغاز هماهنگ‌کنندهٔ loopback",
	"Gracefully stop the loopback coordinator":           "توقف آرام هماهنگ‌کنندهٔ loopback",
	"Sanitized device values and capability-gated edits": "مقادیر پاک‌سازی‌شدهٔ دستگاه و ویرایش‌های محدود به قابلیت",
	"Attach to the coordinator-owned Telnet session":     "پیوستن به نشست Telnet متعلق به هماهنگ‌کننده",
	"Connect without opening the terminal screen":        "اتصال بدون باز کردن صفحهٔ پایانه",
	"Close the shared Telnet session":                    "بستن نشست مشترک Telnet",
	"Open the allow-listed diagnostic reader":            "باز کردن خوانندهٔ عیب‌یابی فهرست مجاز",
	"Return to device status and activity":               "بازگشت به وضعیت و فعالیت دستگاه",
	"Stop the active PCControllerDesktop coordinator":    "توقف هماهنگ‌کنندهٔ فعال PCController",
	"Notification test from the command palette.":        "آزمایش اعلان از فهرست فرمان‌ها.",

	"compact":     "فشرده",
	"dashboard":   "داشبورد",
	"system":      "سیستم",
	"light":       "روشن",
	"dark":        "تیره",
	"auto":        "خودکار",
	"solid":       "یکدست",
	"transparent": "شفاف",
	"blur":        "محو",
	"acrylic":     "اکریلیک",
	"mica":        "میکا",
	"tabbed":      "زبانه‌ای",
	"directx":     "DirectX",
	"legacy":      "قدیمی",
	"none":        "بدون",
	"custom":      "سفارشی",
	"native":      "بومی",
	"normal":      "عادی",
	"minimized":   "کمینه",
	"maximized":   "بیشینه",
	"bundled":     "همراه",
	"toast":       "Toast",
	"balloon":     "Balloon",

	"space": "Space", "toggle": "تغییر", "on": "روشن", "off": "خاموش",
	"refresh": "تازه‌سازی", "reconnect": "اتصال دوباره", "diagnostics": "عیب‌یابی",
	"terminal": "پایانه", "commands": "فرمان‌ها", "settings": "تنظیمات",
	"device": "دستگاه", "serving": "سرویس", "theme": "پوسته",
	"more": "بیشتر", "quit": "خروج", "close": "بستن", "events": "رویدادها",
}

func (m *Model) text(english string) string {
	persian := persianTUIText[english]
	return m.localizer.Choose(english, persian)
}

func (m *Model) textf(english string, args ...any) string {
	template := m.text(english)
	return m.localizer.ShapeDigits(fmt.Sprintf(template, args...))
}

func (m *Model) rtl() bool {
	return m.localizer.IsRTL()
}

func (m *Model) textPosition() lipgloss.Position {
	if m.rtl() {
		return lipgloss.Right
	}
	return lipgloss.Left
}

func (m *Model) alignLine(value string, width int) string {
	value = fitText(value, width)
	if m.rtl() {
		return padLeft(value, width)
	}
	return padRight(value, width)
}

func padLeft(value string, width int) string {
	missing := width - lipgloss.Width(value)
	if missing <= 0 {
		return value
	}
	return strings.Repeat(" ", missing) + value
}

func (m *Model) applyLocalePreference(value string) {
	preference, err := i18n.ParsePreference(value)
	if err != nil {
		preference = i18n.Auto
	}
	m.localePreference = preference
	m.localizer = i18n.New(preference)
	m.keys = defaultKeyMap(m.localizer)
	m.refreshActivityView()
}

func (m *Model) beginLocaleLoad() tea.Cmd {
	transport, ok := m.transport.(ConfigTransport)
	if !ok {
		return nil
	}
	m.localeRequestID++
	requestID := m.localeRequestID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, m.options.Timeout)
		defer cancel()
		cfg, err := transport.GetConfig(ctx)
		return localeConfigMsg{requestID: requestID, cfg: cfg, err: err}
	}
}

func (m *Model) acceptLocaleConfig(message localeConfigMsg) tea.Cmd {
	if message.requestID != m.localeRequestID || message.err != nil {
		return nil
	}
	m.applyLocalePreference(message.cfg.UI.Locale)
	return m.rebuildLocalizedOverlay()
}

func (m *Model) rebuildLocalizedOverlay() tea.Cmd {
	switch {
	case m.commandsActive():
		return m.openCommands()
	case m.deviceActive():
		return m.buildDeviceActions()
	case m.diagnostics.mode == diagnosticForm:
		return m.openDiagnostics()
	case m.settings.mode == settingsMenu:
		return m.openSettingsMenu()
	case m.settings.mode == settingsEditing:
		return m.openSettingsEditorReady(m.settings.section)
	}
	return nil
}
