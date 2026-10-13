# PU850 non-business import ledger

Source: C:/Users/David/Desktop/PU850. Reviewed on 2026-10-13 without
modifying, building, launching or installing the PU850 project.

MainActivity.java was read in full, all 5,429 physical lines (including comments
and blank lines), SHA-256 prefix de5c45e329c15fe9. The first count of 4,262
excluded blank lines and is not the physical line count. Its complete read
covered 1–520, 521–1250, 1251–2050, 2051–2870, 2871–3720,
3721–4560 and 4561–5429.

The supporting review covers active HttpRequest, FileDownloader, SocketClient
and SocketServer; the non-business state/icon portions of CommonDefine; both
layouts; base/night/v27 style variants, colors, dimensions and localization
resources; manifest/build configuration; and the inactive StorageUtils/WebServer
prototypes. Resource variants were compared against their fully read base
files. The source inventory/digests are recorded separately in PU850-source-inventory.json.

## Findings

PU850 combines scale/receipt/access-level protocol code and generic Android UI
plumbing in one activity. Its useful details are localized-digit conversion
(987–1006), LTR monospace addresses, strict host/port admission, inline field
errors and keyboard submit (1259–1754), native network callbacks (1773–1794),
two-second availability rechecks, a TCP probe followed by application readback
(3109–3255), explicit reconnect intent and WebSocket callbacks (4430–4645).
SocketClient uses a ten-second lost-connection timeout and a cancelable connection
dialog. A successful TCP handshake alone is not PU850 identity or board readiness.

The dialog title helper (931–983) combines an icon, title and explicit Close.
Dialog/bottom-sheet resources share sixteen-pixel rounding, light/dark surfaces
and localized type. Address, transfer and receipt dialogs keep invalid input open
and route keyboard Done/Enter through submission. File transfer UI uses actual
byte totals, cancellation and throttled progress/notification updates. MediaStore
export supplies useful Open/Share actions after an actual saved file.

Important adaptation boundaries: the old address regex is IPv4/DNS-only,
requires a port and always constructs HTTP/ws. It rejects full URLs and IPv6.
The port probe excludes 65535 although the input validator accepts it. Socket
and HTTP helpers hold Activity references and manipulate dialogs from callbacks;
AsyncTask, legacy storage reflection, unbounded bulk reads, generic redirect
following and task-clearing locale restarts should not be copied.
The inactive WebServer references another app's PlayerActivity and exposes a
generic command/file server; it is not a working PU850 Android RPC layer.
The beep handler plays notifications/ringtones, not frequency/duration-driven
passive-buzzer audio. Keep the new bounded PCM mirror.

The Flutter transport already had strict origin validation, but its active
connection field exposed errors only after submission in a SnackBar, had no
localized normalization or endpoint/product detection, and accepted no bare
host in that UI. Existing reconnect backoff and owner fences must be retained;
the prior Android network callback woke discovery, not a selected peer's
delayed reconnect. Network recovery must never replay commands or change peers.

## Import batches

Status means implemented and source-verified only when checks are recorded below.
APK identity, real Android behavior and human acceptance are separate evidence.

| ID | PU850 source/pattern | Flutter destination/adaptation | Batch | Status / acceptance |
|---|---|---|---|---|
| U01 | MainActivity arabicToDecimal; address edit filter | Shared localized digit/separator normalization, caret-safe formatter, inline errors | 1 | Source implemented; invalid/partial input, Persian/Arabic paste, selection and IME checks passed |
| U02 | MainActivity address/port validation | IPv4, DNS, IPv6, HTTP(S)/WS(S), explicit ports 1–65535, canonical origin preview and known service URL admission | 1 | Source implemented; explicit port/TLS, invalid paths and credential guards passed |
| U03 | checkAddressServer TCP + /pu proof | Bounded same-host candidate ports and read-only PCController/Pealayer identity verification; optional saved offline address | 1 | Source implemented; bounded fixture identity/redirect/body checks passed; live LAN acceptance open |
| U04 | getDialogTitle, rounded_dialog, BottomSheetStyle, IME listeners | Shared responsive modal shell, 44px Close, icon/title/footer, keyboard-safe scrolling, inline connection result/cancel | 1 | Source implemented for connection and root confirmations; phone/tablet/light/dark/keyboard/RTL source checks passed; other surfaces U10 |
| U05 | native NetworkCallback, retry intent, SocketClient lost timeout | Immediate selected-peer network recovery, coalesced opens, WebSocket keepalive, explicit disconnect and owner fencing | 1 | Source implemented; loopback keepalive, delayed/canceled opens, duplicate wakeups, network recovery and explicit-disconnect checks passed |
| U06 | state-driven connection icon/action and selected address | Preserve Connect/Cancel lifecycle and Disconnect More; show actual candidate/product and saved target | 1 | Existing lifecycle preserved; verified canonical target shown, no inferred board readiness |
| U07 | download byte totals, throttled progress, cancel confirmation, notifications | Existing bounded APK/firmware transfers, actual progress receipts and optional Android transfer notifications | 2 | Planned; preserve integrity, installation consent and uncertain outcomes |
| U08 | MediaStore export and afterImageSavedDialog Open/Share | Contextual saved screenshot/document actions via existing SAF/capture path | 2 | Planned; only offer actions after a confirmed local file/URI |
| U09 | applyViewDirection/getTypeface/locale resources | English/Persian/Arabic UI resources, LTR endpoints/measurements inside RTL, localized numbers | 2 | Partial: endpoint direction/digits and original Droid Sans Mono ported; complete localization remains open |
| U10 | contextual question/receipt/login dialogs | Migrate remaining channel/cue/effect/settings dialogs to shared shell and consistent validation | 2 | Planned by surface; no unsolicited authentication or discard confirmations |
| U11 | semantic errors/icons and disabled action state | Actual availability, field errors, outcome-unknown receipts and accessible focus/keyboard behavior | 1–2 | Partial: field/cancel/edit-during-probe and ownership guards passed; extend each remaining modal |
| U12 | click haptics and user-visible long-task state | Optional native haptics for explicit user actions and real operation notifications | 3 | Planned; no constant vibration, filler or repaint loops |
| U13 | fixed status/measurement panels and subdued palette | Retain existing Pastel/Vibrant configuration; consistent dialog surfaces and centered text | 1 | Source implemented for shared dialog theme; Pastel/Vibrant palettes preserved and rendered |
| U14 | wake-lock/screen-on methods and portrait layout | User-controlled monitoring screen policy and adaptive tablet layout | 3 | Evaluate; never force wake/unlock, portrait or media interruption by default |
| X01 | scale, tare, receipt/report, access-level and printer-font protocol | Excluded business/device logic | — | Do not import into PCController/Pealayer |
| X02 | ringtone/notification beep handler | Excluded audio approximation | — | Keep bounded frame-counted PCM buzzer mirror |
| X03 | inactive WebServer/StorageUtils, generic socket demo | Excluded direct implementation; modern API concepts only if needed | — | Keep protected local RPC, SAF and current permission/owner contracts |
| X04 | AsyncTask, Activity-held helpers, arbitrary redirects, task-clearing locale restart, unbounded bulk reads | Excluded legacy lifecycle/transport mechanics | — | Use bounded asynchronous ownership-aware Flutter/native adapters |

## Batch 1 verification

The first batch targets 0.1.10+11. Twenty-five focused checks passed:
six address/detection checks, seven dialog/input/cancel/race/geometry checks,
four peer-recovery checks, six existing API credential/uncertain-write guards
and both phone/tablet front-panel loading/scroll regressions. The analyzer is
clean. One additional opt-in renderer passed for actual application connection
modals in phone Vibrant, tablet dark Pastel and narrow keyboard/error layouts.
Those three PNGs are controlled source fixtures, not actual Android screenshots
or proof of endpoint/network behavior.

Two concurrent detection workers inspect at most six same-host candidates,
using bounded read-only health/product responses without redirects or tokens.
Explicit ports are honored; explicit HTTPS never downgrades. A failed probe
can save a validated offline target through an explicit Connect when available
action; wrong-product results cannot take that shortcut. Native network changes
wake the selected enabled session, coalescing opens and preserving explicit
disconnect, request/response-only relay routing and command non-replay.

Read-only two-second LAN checks to the previously recorded Cafe addresses
192.168.100.155:8787 and :8080 failed to connect on 2026-10-13. Live Android
endpoint detection and reconnect behavior remain unverified. No PU850 business
operation or physical PCController action was sent. Device/package receipts
are separate from these source checks and are appended after delivery.

The original PU850 res/font/monospace.ttf is ported unchanged as
assets/fonts/DroidSansMono.ttf, SHA-256
`db19a1fdaba41cc4a2fec0330e5c15e71c6dd68a3ef074f4f28268828b45c862`.
Its embedded metadata identifies Droid Sans Mono Regular 1.00, Google
Corporation copyright 2007 and Apache License 2.0. The full license and source
notice are bundled and registered with Flutter LicenseRegistry, following the
[Apache redistribution conditions](https://www.apache.org/licenses/LICENSE-2.0.txt).
No commercial/localized PU850 fonts or business assets are imported.

Next batches are U07/U08 transfer and saved-file actions, U09 localization,
U10 remaining channel/cue/effect/settings dialog bodies and U11 their validation.
U12 haptics and U14 explicit screen policy require product decisions and actual
Android checks. They remain open; first-batch delivery is not full UI acceptance.
