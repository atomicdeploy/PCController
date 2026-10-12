# PakarController mobile

The Devices page now scans mDNS/DNS-SD, SSDP and MNDP and saves independent
local/remote profiles with secure credentials. Actual board type/capabilities
are detected after selection, not invented by profile labels. The app-bar
context menu opens separate Firmware and Advanced MQTT/bridge pages. Firmware
supports uploads, SHA-256-pinned coordinated remote staging/apply, stored AVR
flash/EEPROM export and digest-checked running ESP image export. The Android
bridge supports TLS MQTT (including WebSocket transport), QoS, broker subscription
ACK, availability/LWT, an outbound WebSocket and authenticated local Android
HTTP/WebSocket clients. It is explicitly foreground-only and loopback-only;
remote commands/firmware staging are opt-in, duplicate/retained/stale requests
fail closed, and no lost command is replayed. See
[the contracts and remaining acceptance](../docs/MOBILE-NETWORK-WORKSPACE.md).

The Flutter control surface targets Android phones and tablets with the same
truthful board-state contract as the native and embedded Web interfaces.

It provides the row-major 80-cell table with the physical/logical identity
editor and half-cell column stagger, game modes, front-panel marquee and live
seven-segment preview, hardware/discovery state, and persistent authenticated
connection preferences. The user-facing endpoint defaults to
`http://ledboard.local`; any valid renamed `.local` hostname is supported. An SSDP origin is used only as a matching identity-checked
transport fallback when the Android resolver cannot resolve mDNS.

The Phosphor font is vendored in `assets/fonts/Phosphor.ttf`. Only used glyphs
are retained in release builds. The visual colors follow Pealayer's Studio
palette roles. Logical-ID search supports exact numbers and ranges with
one shared selection; alternating colors are on cells, not column backgrounds,
and odd columns use +0.5. Finding or tapping selects without physical writes;
explicit bulk output stops at the first failed/uncertain command. Indicators
are optional and initially hidden. Neutral animated inside outlines do not
change cell geometry or conflict with selection.
Unknown board state is not displayed as zero or a confirmed off cell.

Passwords use `flutter_secure_storage` 11.2.0 (Android platform encrypted
storage), not SharedPreferences. A legacy plaintext password is removed only
after secure migration succeeds; no plaintext fallback is permitted.
Android app backup is disabled. HTTP Basic/Telnet over the ESP's plain HTTP
LAN services still require a trusted network; encryption at rest is not
transport encryption. Discovery is not cryptographic device identity.

Mutations are sent once, after a read-only identity check. A lost response
reports an unknown outcome and is never replayed by discovery. Marquee text
defaults to Once without forced scrolling; Loop and Interval are explicit.

```powershell
./tools/build-mobile.ps1 -Mode Test
./tools/build-mobile.ps1 -Mode ApkPreview
```

The release APK is written to
`build/app/outputs/flutter-apk/app-release.apk`.

The current release-mode APK uses the template's debug signing identity and
is a **preview**, not a production-signed Android release. A durable signing
key, physical phone/tablet verification, full preference/menu parity and
actual board round-trips remain open in GitHub master issue #2. No keystore or
device credentials are committed. Flutter/Dart versions used for this
checkpoint are 3.47.3/3.13.3; all product surfaces advertise version 0.2.7.

The wrapper runs from the repository root using the existing documented SDK,
canonical mobile/build cache, reviewed context/storage gates and locked resolved
packages. It does not install an SDK, upgrade dependencies or clean caches.
Only acknowledged capabilities are displayed; stale/changed connections revoke
controls and pending replies. Device application title and bounded inline artwork
override the shared desktop/Web PNG fallback. See docs/MOBILE-CONTEXT.md.

Device settings now include actual Wi-Fi, MQTT, outbound WebSocket, full board
identity and access editors with explicit confirmation, positive ACK and public
readback. Phone wrapping navigation and tablet rail share the real catalog.
See docs/MOBILE-SETTINGS.md for the actual Pealayer PreferencesTab mapping,
46 passing Flutter tests, inspected captures and concrete remaining omissions.
Mobile firmware now has pinned remote jobs, explicit apply/cancel, ISP/full
backups, document import/export and ESP OTA. See docs/MOBILE-FIRMWARE.md for
actual donor mappings, safety boundaries and remaining physical acceptance.
Complete localization/appearance and Android-back draft protection remain open.

```powershell
./tools/build-mobile.ps1 -Mode Test -CaptureView SettingsTablet -CapturePath C:\absolute\outputs\PakarController\mobile\new-settings.png
```

Capture paths must be new files within this task's outputs/PakarController.
SettingsPhone, FirmwareTablet, FirmwarePhone and Table are also supported; captures assert zero mutations and
do not control an installed phone or the production Windows desktop.

FrontPanelTablet and FrontPanelPhone capture the real peripheral mirroring UI
with an explicit simulated peer and a silent injected audio output. Front panel
and actual 7-segment masks are independent previews; phone-speaker mirroring
synthesizes the advertised AVR Hz and timestamp cadence and is opt-in,
foreground-only and peer/boot/AVR-session fenced. See
[MOBILE-MIRRORING](../docs/MOBILE-MIRRORING.md) for contracts and live-test gates.
