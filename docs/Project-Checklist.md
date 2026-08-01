# Project Checklist

Audit date: 2026-08-01

This is the domain-sorted successor to
former `%TEMP%\PCController-check.md`. It includes the original checklist plus the
later requests recorded in the session history. The original temp file is
being preserved until this copy is verified.

Operational instructions are consolidated in the
[Getting Started and Operations](Getting-Started-and-Operations.md); detailed implementation status
remains in this checklist.

## Status legend

- ✅ Implemented and supported by source, a build/test result, or an explicit
  user hardware confirmation.
- 🟡 Partially implemented, still being integrated, or superseded by a newer
  requirement that is not fully closed.
- ⚠️ Implemented in source but still requires a relevant live-hardware or
  external-system test.
- ❌ Missing, currently failing, or disproved by the latest validation.

## Current release-candidate status

- ✅ The full cap23 release-candidate source builds at 32,084/32,384 flash and
  1,444/2,048 static SRAM, leaving 300 flash bytes. Cap23 is the compact
  persistent local-menu visibility/order/hierarchy/layout capability.
- ✅ A complete physical cap24 overlay is **not** advertised. The measured
  incomplete lower bound added 256 flash and 26 SRAM (32,340 flash, only 44
  free) while still omitting navigation, requests/retries, failure fallback,
  relationships, visual/blink handling, read-only cue, and unsolicited events.
  Host and VirtualBoard retain cap24; the AVR uses cap19 exact front-panel push.
- ✅ The latest physically verified checkpoint was `2FD9F81C`: UART upload and
  flash verification succeeded on COM18, page 0 was Status, reset count stayed
  9 through Previous/Next/Decrease/Increase, relays were off, PWM user outputs
  were off, and the temporary test mute was restored to `silent=false`.
- 🟡 The 32,084-byte cap23 release candidate still needs its final frozen hash,
  guarded UART backup/upload/readback, HELLO/status proof, and load-safe
  acceptance pass. Do not substitute a successful source build for that live
  evidence.
- ✅ Exact compiled and factory initialization parameters are documented in
  [Hardware Initialization and Tuning](Hardware-Initialization-and-Tuning.md),
  including rationale, alternatives, and the boundary between source settings,
  retained EEPROM, and physical verification.

### Historical checkpoints still relevant

The bullets below preserve the evidence that led to the current source. Within
them, words such as “current” or “final” describe the checkpoint at the time it
was recorded; they do not supersede the release-candidate summary above.

- ✅ The application UART on COM18 is electrically working. A live native
  protocol probe returned an authenticated `PCController` HELLO, settings,
  telemetry, two temperature identities, and an I2C scan.
- ✅ Current firmware `5DF10D05` was built, UART-uploaded through current
  MiniCore Urboot/Urclock on COM18, flash-verified, and identified itself as
  `build=5DF10D05 date=Jul 31 2026 time=07:13:06`.
- ✅ The immediately preceding 50-sample AFD check had only a 4 mV supply span
  (0.16 mV mean absolute step), 3 mA current span (0.82 mA mean absolute
  step), and 40 mW power span (11.02 mW mean absolute step). The previous
  8.3/10 V problem is no longer present.
- ✅ The live I2C scan found INA219 at `0x40` and PWM at `0x41`; no address
  collision remains.
- ✅ Relays were reported all off, PWM mode/value were Off/0, and
  framing/CRC counters were zero during the latest protocol probe.
- 🟡 Two reset sources were found. MiniCore's optional `WIRE_TIMEOUT` path
  independently caused early reboot loops, and a timeout-disabled build still
  reset in the large late display dispatcher. A host-text override
  remained stable for 12 seconds, but later fixed-text and global-buffer
  experiments inside the old path also reset; the evidence does **not**
  isolate the fault to arithmetic or `showFixed`. The viable E2/AFD path
  precomputes temperature text and replaces the ordinary-page AVR jump table
  with explicit `currentMode` early returns. The fresh 5DF host-driven sweep
  covered pages 0-13 and held tLED/tBT for 12 one-second samples each, with
  reset count fixed at 2075 for approximately 40 seconds and no reset/error
  event.
- 🟡 Physical Button 1 and Button 2 previously produced Down, Up, and Click
  events on the E2 rollback image with reset count fixed at 2048; Previous
  then Next correctly returned page 13 to page 0. The 5DF key path still maps
  input bits 0-3 directly into the same state machine, but a fresh physical 5DF
  test of Buttons 1-4, double-click, hold acceleration, and editor keys remains
  pending.
- 🟡 Reset telemetry appends cause/count to status. An invalid diagnostic
  entered an early watchdog loop and drove the legacy EEPROM count to 916;
  the later out-of-line-renderer loop advanced the new 64-slot, CRC-checked,
  marker-last journal through many slots rather than hammering one cell.
  However, that live loop reported reset cause `0` instead of a watchdog flag,
  so reset-cause capture through the installed Urboot path is not yet proven
  correct.
- ⚠️ The latest unattended 5DF sweep ran with Silent enabled. Sound factory
  default remains on and earlier firmware produced the confirmed clean melody
  after the Timer1 fix. The running final host has now saved `silent=false`
  and completed the `notify` melody command, but the physical 5DF boot melody,
  one-beep-per-key behavior, and save/discard cues still require a user-listen
  check.
- ✅ The immediately preceding AFD menu and host commands exercised
  voltage/current precision 0, 1, and 2. Two decimals for each were saved, the
  board was reset, and the decoded settings plus raw extended byte `0xF0`
  remained correct. Current 5DF retained those settings.
- ✅ Current 5DF expands that local sequence to `Snd` → `diSP` → `StBr` →
  `CoLr` → `V-dP` → `A-dP`, with value blinking at approximately 300 ms.
  It is built and live; the new fields still need a physical-key pass.
- 🟡 Current live image `5DF10D05` passed the verified COM18 upload, flash
  verify, root-page sweep, and temperature soaks. The immediately preceding
  AFD image passed the 50-sample sensor stability and decimal-setting
  persistence tests on unchanged paths. `E2DCE296` remains the proven
  physical Buttons-1/2 rollback image.
- 🟡 The requested local-menu revision is only partial. 5DF provides one
  nested settings sequence under `Snd`, including display/status brightness,
  Ready color, reading precision, and blinking selection. A broader root
  category hierarchy remains missing.
- ✅ The final PC host TUI is running against COM18 through its stable CH340
  selector and primary-owner IPC. Secondary IPC-routed commands authenticated
  build `5DF10D05`, reported 12.226 V/263 mA with relays and PWM off and zero
  framing/CRC errors, confirmed `silent=false` in MCU EEPROM, and completed
  the host `notify` melody.

## Arduino toolchain and dependencies

- ✅ `arduino-cli` package indexes, installed board cores, and installed
  libraries were audited and upgraded through the configured environment/CLI
  proxy.
- ✅ The installed board cores were current at the audit point:
  Arduino AVR 1.8.8, ESP8266 3.1.2, HoodLoader2 2.0.5, MegaCore 3.0.3,
  MightyCore 3.0.3, and MiniCore 3.1.2.
- ✅ Well-supported requested libraries were installed:
  Adafruit PWM Servo Driver 3.0.3, Adafruit INA219 1.2.3, rc-switch 2.6.4,
  TM1637TinyDisplay 1.12.2, OneWire 2.3.8, and DallasTemperature 4.0.6.
- ✅ Adafruit BusIO and other declared package dependencies were installed.
- ✅ The release-candidate AVR intentionally links EEPROM 2.0 and rc-switch
  2.6.4 but no longer links MiniCore Wire. Fixed-hardware local drivers replace
  generic Wire/TM1637/Dallas/INA219/PWM/LCD/addressable-strip libraries to fit
  the ATmega328P; the compact I2C master still supports repeated START and
  cooperative raw host transfers up to 16 bytes.
- ✅ Firmata was removed from the firmware and host protocol. Its earlier
  installation is superseded; it is not included or linked by this project.
- ✅ Go module dependencies for Charm, serial access, file watching, IPC, and
  the host utilities are declared in `go.mod`/`go.sum`.
- ✅ UPX 5.2.0 is installed globally under `C:\Program Files\UPX`, and the
  machine PATH contains that directory without hard-coding the original
  extraction path.

## Project import, LocalLib merge, and structure

- ✅ The Puzzles project was used as the starting point and its reusable
  project layer was restored without reintroducing puzzle business rules.
- ✅ Puzzles, Timer, and motor/encoder/HMI LocalLib variants were compared and
  selectively merged. The differences and selected parts are documented in
  [Local Library Merge History](Local-Library-Merge-History.md).
- ✅ The useful mode/state manager was restored and expanded for menu,
  editors, motion, learning, boot, feedback, and fault states.
- ✅ Reusable boot melody, feedback melodies, buzzer, shift-register, task,
  key, TM1637, LCD, and WS2811/WS2812 layers were retained or improved.
- ✅ Firmware source directories were moved one level up to root
  `LocalLib/` and `Project/`.
- ✅ `PCControllerLocalLib.cpp` and `PCControllerProject.cpp` aggregate the
  root subdirectory implementations exactly once for the Arduino build.
- 🟡 The firmware is substantially modularized into domain classes, but
  `PCController.ino` is still about 2,740 lines/84 KB and contains the large
  menu and opcode dispatchers. The request to reduce it to a small skeleton
  entry point is only partially complete.
- ✅ Host/tool directory consolidation is complete: the active host is under
  `Tools/Controller`, the native simulator is under `Tools/VirtualBoard`, and
  the stale root `host/`, empty `Tool/`, and obsolete legacy host binary were
  removed.
- ✅ Root build scripts and host documentation now use the canonical
  `Tools/Controller` path.
- ✅ Source code uses `PWM` rather than PCA/pair terminology; 433 MHz records
  use `learn`.
- ✅ The Dallas implementation is named `DallasTemperatureBus`; the obsolete
  `Compact` Dallas name was removed.
- ✅ Operational firmware identity uses a build hash plus compile date/time.
  The leading `0.0.0` bytes exist only as a compatibility shape telling older
  hosts to use the appended identity.
- ✅ Constant user-facing AVR text uses `F("...")` where applicable.

## Board pins, shift registers, and input model

- ✅ Keys use the first four active-low shift-input bits:
  Key 1 Previous, Key 2 Next, Key 3 Decrease, and Key 4 Increase/Enter.
- ✅ Shift inputs 4 and 5 are reserved system-sense inputs and remain visible
  in raw status data.
- ✅ Shift input 6 is the BT-5.0-Pro audio-module LED sense input with
  Off/On/Blink classification.
- ✅ Shift input 7 is the enclosure reed input.
- ✅ Shift outputs start inactive and are committed through the relay safety
  controller.
- ✅ TM1637 uses MOSI/D11 for data and SCK/D13 for clock.
- ✅ Both DS18B20 probes share CS/D10.
- ✅ 433 MHz receive uses D2/INT0 and transmit uses D3/INT1.
- ✅ The addressable WS2811/WS2812 data line is restored on D6.
- ✅ The buzzer uses D9/Timer1 OC1A.
- ⚠️ Raw bit mapping is source-verified, but reserved sense bits 4/5 and live
  BT/reed polarity still need a complete physical transition test.

## Firmware lifecycle, persistence, and reset

- ✅ Startup restores EEPROM settings, initializes safe relay/PWM state,
  emits an early unsolicited HELLO, and enters the configured menu page.
- ✅ During development, MCU configuration is deliberately an **unversioned**
  packed 29-byte cap23 settings value plus one CRC-8 byte at EEPROM address 32.
  It uses `EEPROM.update()` with a 1.5 s deferred write and remains separate
  from host JSON/YAML/TOML configuration. There is no settings magic/version
  field or automatic firmware-side legacy migration in this development
  layout; an invalid record is safely replaced by factory defaults.
- ✅ Persistent MCU settings include Silent and door/relay audio cues,
  tLED/tBT swap, four-state motion-door policy, 1/100 ms motion break,
  illumination mode/levels, display/status brightness, persistent Ready color,
  voltage/current decimal precision, PWM boot mode, user PWM 0-7, telemetry
  period, default/save-last page, and cap23 menu visibility/order. LCD ownership
  is host-side; flags bit 1 is reserved rather than an LCD-enable setting.
- ✅ Sound factory default is on, per the later request; retained EEPROM may
  override it.
- ✅ A configurable default menu page is used after boot and when a closed
  enclosure leaves a no-change flow.
- ✅ Save-last-page can update the default page so it survives power loss.
- ✅ The host can query/change settings and directly select a menu page.
- ✅ The application reset command first disables side/general relays,
  disables PWM test mode, clears PWM channels 0-12, plays a reset RGB cue,
  then clears all PWM outputs and enters a watchdog reset.
- ⚠️ Graceful reset sequencing is source-complete but needs a live load-safe
  reset test after the physical menu-key validation.
- 🟡 Hardware reset cause and a persistent reset count are appended to status
  telemetry and emitted at boot. The wear-levelled journal advanced across a
  real reset loop, but its migration/rollover integrity still needs a bounded
  test and the observed cause remained `0`; exact reset-cause reporting is
  therefore not yet working on the installed bootloader path.
- ✅ MiniCore Wire's optional timeout path was removed because it was one
  reproducible live reset trigger. The fixed 100 kHz compact TWI master now
  bounds every wait at 25 ms and resets only the peripheral on timeout; startup
  additionally provides nine recovery clocks plus STOP, and the 2 s watchdog
  remains a final system-level bound.

## Buttons, gestures, menu, and audio

- ✅ A press performs its first action immediately.
- ✅ Key events include Down, Up, Click, DoubleClick, HoldStart, HoldRepeat,
  and HoldRelease.
- ✅ Hold begins after 600 ms, repeats every 150 ms, and accelerates to 60 ms
  after 1.8 seconds.
- ✅ Short press versus long hold semantics are preserved: a held key gets the
  initial press action, then hold/repeat actions without blocking the loop.
- ✅ Plus/minus brightness values roll over at their limits.
- ✅ Key-identification page shows 1, 2, 3, or 4.
- ✅ The mode-driven menu has voltage, current, tLED, tBT, illumination, BT,
  sound, PWM test, relay test, key identification, user PWM, R5-R8, motion,
  and RF learn pages with editor submodes.
- ✅ Illumination, sound, PWM mode, and user PWM flows provide save/discard,
  distinct audio cues, and flashing `SAVE`/`diSC`.
- ✅ The four requested keys and actions are documented in
  [Front Panel and Menus](Front-Panel-and-Menus.md).
- 🟡 The corrected image passed host-driven navigation through every root
  page. Physical Buttons 1 and 2 then produced complete Down/Up/Click events
  without changing the reset count. Buttons 3/4 and the full
  double-click/hold/editor test remain.
- ✅ The original three-note boot melody and reusable project-layer cues are
  present.
- ✅ Each physical menu press requests one brief beep.
- ✅ Silent mode is EEPROM-backed and can be changed locally or by the host.
- ✅ The user explicitly confirmed that the former glitchy buzzer was fixed.
  The fix uses Timer1 hardware pin toggling instead of an audio-rate ISR, so
  RF/sensor interrupt latency no longer modulates the tone.
- ⚠️ Key beeps, double-click, accelerated hold, and save/discard cues need to
  be re-tested on the corrected release image.

## TM1637 and optional I2C LCD

- ✅ TM1637 writes are cached at the four-segment frame and brightness level;
  unchanged data is not retransmitted every service interval.
- ✅ The display decision loop runs every 20 ms without blocking UART.
- ✅ INA219 is sampled every 100 ms while the door is open and every 500 ms
  while closed.
- ✅ INA219 configuration `0x3F7F` uses 64-sample bus averaging plus stronger
  128-sample shunt/current averaging in continuous mode. A complete conversion
  is about 102.15 ms; open-door polling is 100 ms and closed polling is 500 ms.
- ✅ DS18B20 uses nonblocking 11-bit conversions; the open-enclosure period is
  450 ms and the normal period is 1 s.
- ✅ Temperatures use a 50/50 integer EMA; a raw sample at or above
  the 50 C warning threshold bypasses smoothing so the hot cue is immediate.
- ✅ The latest 50-sample AFD check measured supply span 4 mV
  (0.16 mV mean absolute step), current span 3 mA
  (0.82 mA mean absolute step), and power span 40 mW
  (11.02 mW mean absolute step). The earlier E2 sample established absolute
  readings around 12.22 V and temperatures around 30.25/30.67 C.
- ⚠️ These rates address the previous four-updates-per-second jumpiness in
  source, but smoothness still needs visual confirmation on the corrected
  firmware.
- ✅ The AVR's full PCF8574/HD44780 renderer is disabled to save flash. The host
  probes `0x27`/`0x3F`, owns rich 16x2 rendering through the cooperative 16-byte
  I2C bridge, and can run it alongside the cached TM1637 path.
- ✅ The host can send four-character TM1637 text and up to two 16-character
  LCD rows.
- ✅ A live scan found only `0x40` and `0x41`; therefore no LCD backpack was
  present/responding during that test.
- ⚠️ LCD discovery, initialization, text output, and simultaneous TM1637 use
  need hardware validation after a backpack is connected.

## INA219, DS18B20, and I2C

- ✅ One-line source comments state `INA219 = 0x40` and `PWM = 0x41` to
  document the collision fix.
- ✅ Supply voltage is calculated as bus voltage plus shunt voltage.
- ✅ Live supply/current data now agree with the nominal 12 V system rather
  than the earlier 8.3/10 V readings.
- ✅ DS18B20 source and pin-map comments require an external 4.7 kOhm pull-up.
- ✅ Missing-pull-up/stuck-low handling checks idle-high, bounds ROM-search
  passes, and returns without locking the MCU.
- ✅ The two ROM IDs are discovered, sorted, reported by the protocol, and
  displayed by the host with the `tLED`/`tBT` roles.
- ✅ The running final host currently lists tBT
  `28-616435FB503F-E9` and tLED `28-70F275000000-8A`; the latest open-door
  snapshot was 31.39 C and 32.75 C respectively.
- ✅ The harness mapping in source treats the lower sorted ROM as tBT and the
  higher ROM as tLED; an EEPROM swap flag remains available.
- ⚠️ An earlier role-identification snapshot was tLED 26.50 C and tBT
  29.88 C; the later E2 stability sample was about 30.25 C and 30.66-30.68 C
  respectively. A controlled illumination-on/off test is still needed because
  neither ambient snapshot proves that tLED warms while tBT stays cool.
- ✅ The live I2C address result is:

  | Device | Address | Live result |
  |---|---:|---|
  | INA219 | `0x40` | detected |
  | PWM expander | `0x41` | detected |
  | Optional LCD | `0x27` or `0x3F` | not detected |

- ✅ There is no conflict in the current address map. TM1637 and DS18B20 are
  not I2C devices.

## PWM, enclosure light, power, RGB, and addressable LEDs

- ✅ PWM channels are named and owned as requested:
  0-7 eight MOSFET user lights, 8-10 three user outputs, 11 enclosure light,
  12 power indicator, and 13-15 status RGB.
- ✅ Logical value caching avoids redundant I2C writes and the expander is
  configured for 1 kHz output.
- ✅ Boot initialization normalizes MODE2 and uses safe logical output
  polarity instead of leaving all channels fully on.
- ✅ Manual mode can select and change any channel for wiring
  identification.
- ✅ Auto demo fades across user channels 0-10 and never takes ownership of
  enclosure, power, or RGB channels.
- ✅ PWM Auto is the factory boot mode; valid EEPROM may select another mode.
- ✅ The eight persistent 8-bit user outputs can be edited from the menu and
  by the host.
- ✅ Enclosure illumination has Off/Auto/On plus separate Off/On brightness
  levels.
- ✅ Auto mode follows the reed input and uses elapsed-time 20 ms fade steps;
  catching up missed intervals fixes the former on-to-off jitter in source.
- ✅ Power indication and status RGB controllers are implemented.
- ✅ RGB modes/cues cover boot, ready, learning, hot/warning, fault, door
  open/closed, BT activity, menu navigation, RF activity, save, discard, and
  graceful reset.
- ✅ Host commands can set a custom RGB value and brightness.
- ⚠️ `pwm off` is intentionally an all-16-channel emergency clear, not merely
  a user-output stop; it also clears enclosure, power, and RGB channels.
  Ordinary commissioning shutdown should use `pwm mode off`. Confirm whether
  system controllers should explicitly re-render after an emergency clear.
- ✅ The 11-pixel D6 WS2811/WS2812 buffer, brightness, fill, pixel, clear, and
  show operations are restored with a fixed AVR sender.
- ⚠️ The physical PWM outputs, auto fade, enclosure fade smoothness, power
  indicator, RGB colors, and addressable-strip ordering still need live
  visual/load validation after the corrected firmware is installed.

## Relays and motion safety

- ✅ Relay mapping is R1 direction/R2 enable for Side A, R3 direction/R4
  enable for Side B, and R5-R8 general outputs.
- ✅ Direction changes are sequenced disable -> break -> direction change ->
  settle -> enable, so opposing motion is not commanded simultaneously.
- 🟡 Make the break-before-direction interval an EEPROM-backed board setting
  when the final flash budget permits it. The current loads may use a 1 ms
  default/minimum, while direction-settle and cross-side interlocks remain
  independently enforced. Expose the value through menus, CLI, TUI, APIs, and
  backup/offline EEPROM decoding instead of baking a single delay into code.
- ✅ Relay all-off, individual test, and automatic identification test modes
  exist.
- ✅ R5-R8 support toggle and momentary push behavior.
- ✅ MOVE maps Keys 1/2 to Side A Up/Down and Keys 3/4 to Side B Up/Down.
- ✅ Releasing a motion key stops that side.
- ✅ Holding both direction keys for either side exits MOVE and stops all
  relays.
- ✅ Closing the enclosure stops both sides and exits MOVE immediately.
- ✅ The host can control individual relays, side motion, all-off, and relay
  tests.
- 🟡 Local MOVE and learned RF Side actions enforce the reed policy in
  firmware. The host also performs a fresh fail-closed status preflight before
  R1-R4/side/macro start commands, while allowing stop/off. The host query and
  command are not atomic, so a firmware-level gate inside every host motion
  opcode would still be stronger.
- ⚠️ Motor direction naming, interlock timing, R5-R8 wiring, door-close stop,
  and relay test mode require a load-safe physical test.

## 433 MHz RF receive, transmit, and learning

- ✅ rc-switch receive is attached to D2/INT0 and transmit to D3/INT1.
- ✅ Receive and transmit are both implemented; transmit temporarily disables
  receive, sends, then restores the receiver.
- ✅ RF learning uses `learn` terminology throughout the active code and menu.
- ✅ Eight CRC-checked EEPROM records can be learned, listed, removed
  individually, cleared, and remapped.
- ✅ Each record retains code, bit count, protocol, pulse width, action,
  action value, and behavior.
- ✅ Host mapping supports Key 1-4, menu actions, R1-R8, Side A/B
  Up/Down/Stop, PWM 0-10, and no action, with press/toggle/momentary behavior
  where applicable.
- ✅ Every received RF frame is emitted to the host with exact code, bit
  count, protocol, pulse width, and learned ID.
- ✅ Learned RF actions identify their input source and learned record ID in
  normalized key events.
- ✅ The host derives RF down, hold, repeat, and timed-up gestures from
  repeated receiver frames and exposes them to automation and IPC.
- ✅ Host automations can match raw RF code/protocol/learned ID and physical,
  RF, or host key source/gesture.
- ✅ Host commands and automation actions can transmit an RF code with
  configurable bit count, protocol, pulse width, and repeat count.
- ⚠️ A real handset still needs end-to-end learn/list/map/hold/remove tests,
  and INT1 transmission should be confirmed with another receiver.

## Native UART protocol and asynchronous events

- ✅ UART is the primary application interface at 115200 8N1, not a debug
  console.
- ✅ The lightweight binary protocol uses magic/version, opcodes, sequence
  IDs, bounded 48-byte payloads, COBS framing, a zero delimiter, and CRC-8.
- ✅ It supports request/response, unsolicited boot HELLO, status streaming,
  and asynchronous event frames.
- ✅ Commands cover identity/status/settings, temperatures, sound, all PWM,
  RGB, addressable LEDs, RF, menu, relays, reset, I2C scan, displays, and
  macros.
- ✅ Source uses bounded arrays and flash strings suitable for AVR rather than
  BSON/MessagePack heap machinery.
- ✅ Door, BT, PWM auto-channel, key, RF learned/raw-receive, and macro events
  are emitted immediately.
- 🟡 A fault event type is defined and parsed by the host, but firmware does
  not yet emit a dedicated fault event.
- 🟡 Relay-state changes and temperature alarms are visible in streamed
  status, but do not yet have dedicated immediate event types.
- ✅ The host updates cached door/BT/PWM state from event frames and publishes
  board and USB lifecycle events to TUI, scripting, automations, Go clients,
  the C ABI, and JSON-RPC IPC. The current Windows DLL/header were rebuilt and
  passed an external C-caller smoke test.
- ✅ The former application-HELLO deadline problem is addressed by early boot
  HELLO plus configurable settle/retry windows and authenticated retry.
- ✅ The packaged host previously authenticated official firmware `A7E59058`
  on COM18 and completed read-only HELLO, STATUS, SETTINGS,
  temperature-list, and I2C-scan commands without a deadline error. The later
  E2 rollback image completed its host-driven status/menu validation, and
  current 5DF completed HELLO/STATUS/menu/settings validation plus the full
  all-page sweep. The final packaged TUI launch/demonstration is still pending.

## Host application, TUI, configuration, shell, IPC, and library

- ✅ The PC host is implemented in Go.
- ✅ The TUI uses Bubble Tea, Bubbles, and Lip Gloss from the Charm stack.
- ✅ TUI/monitor state covers connection, voltage/current/power, tLED/tBT
  values, door, BT, keys/events, relays, menu/mode, and selected PWM
  channel/value. Temperature ROM identities are exposed by `temp list`, IPC,
  and the reusable APIs rather than occupying a permanent TUI card.
- ✅ Commands provide port list/open/close/reconnect, status/settings,
  temperature list/scan, stream, menu, relays, PWM, RGB, strip, sound,
  configurable melodies/status effects, displays, macros, automations, RF,
  I2C, reset, raw query/write, Arduino, bootloader, and programming operations.
- ✅ Auto-detection can filter VID/PID/name but accepts a candidate only after
  a valid `PCController` HELLO identity.
- ✅ Stable selectors also accept a COM ID, human/friendly name, `VID:PID`,
  USB `serial:VALUE`, or Windows `instance:VALUE`. Event-driven Windows device
  notifications re-arm discovery without polling delay, and an interactive
  chooser handles multiple ambiguous adapters.
- ✅ A successful HELLO persists `connection.last_device` in host JSON without
  touching MCU EEPROM. The current CH340 has no stable USB serial, so the host
  can use its PnP instance when topology is unchanged and safely falls back to
  VID/PID/name plus the chooser rather than trusting a COM number alone.
- ✅ The launched host persisted COM18, VID/PID `1A86:7523`, friendly name
  `USB-SERIAL CH340`, and current instance
  `USB\VID_1A86&PID_7523\6&2CC1445A&0&3` in
  `%AppData%\PCController\config.json`.
- ✅ Auto-reconnect has explicit connected/disconnected/reconnecting/
  reconnected events.
- ✅ `reset_on_reconnect` is a PC-side JSON option, defaults false, and pulses
  DTR only once when a disappeared USB board reappears; it does not touch RTS
  or repeat the pulse for HELLO retries.
- ✅ Explicit reset-line support remains available separately.
- ✅ PC-side configuration is persistent JSON and is kept separate from MCU
  EEPROM settings.
- ✅ fsnotify plus debounce/polling safety applies PC config changes at
  runtime.
- ✅ Interactive shell, batch scripts, CLI, continuous monitor, JSON-RPC over
  loopback/stdio, reusable Go API, and C-compatible JSON ABI source are
  present. Git's incompatible bundled GCC is rejected, and the shared-library
  builder discovers compatible standalone MinGW-w64 toolchains and prefers
  the highest version without hard-coding its WinGet path. The current
  DLL/header were built, both ABI functions were exported, and a real external
  C `ports` smoke test passed.
- ✅ The first long-running TUI/shell/IPC server becomes the exclusive serial
  owner at loopback `127.0.0.1:8787`. Later executable CLI, batch, monitor,
  reset, shell, and programmer invocations route through its JSON-RPC command/
  event stream instead of opening COM18 again. A second TUI intentionally
  becomes an IPC-backed command/event console, not a second full dashboard.
  C-ABI handles still own their own serial transport; consumers that must
  share an existing primary should use JSON-RPC.
- ✅ Ardush and AtomicDeploy portable-shell projects were researched; useful
  command/history/shell patterns were reimplemented portably and attribution
  is documented without copying incompatible project code.
- ✅ The former JavaScript WebSocket upload client/server workflow is
  incorporated as a tested Go WebSocket relay in the single host tool.
- ✅ Host macros have ID/name/segment label/LCD text and timed relay/PWM
  steps; they can play, report status, complete, or be cancelled by the host
  or board command.
- ✅ Reusable host-streamed custom RGB flash/breathe effects and configurable
  melodies are integrated through the TUI/shell, one-shot CLI, JSON-RPC IPC,
  Go API, and C JSON ABI. ACK-paced melody notes and status updates capped at
  20 Hz reuse existing native RGB/buzzer opcodes without consuming AVR flash.
  The host must remain connected while a streamed effect or melody is playing.
  Cancel stops future notes/frames, but a buzzer note already accepted by the
  current firmware must finish; MCU Silent mode still suppresses melodies.
- ✅ Individual R1-R8 and PWM/MOSF operations are already reachable through
  the TUI/shell command engine, one-shot CLI, JSON-RPC `controller.execute`,
  the reusable Go API, and the C JSON ABI. Loaded hardware operation remains a
  separate safety test.
- ✅ Host direct R1-R4, motion, and macro-start paths perform a fresh
  fail-closed door-status preflight; stop/off remains available. Relay toggle
  is exposed. This host check is not atomic with the following command, so the
  firmware interlock remains stronger.
- ✅ Live RF receive events, learn/cancel/list/remove/map commands, and
  asynchronous event delivery provide the requested capture-to-mapping
  workflow; a real handset remains the pending end-to-end test.
- ✅ Host RF gesture synthesis now reports inferred click, double-click, hold,
  accelerated repeat, and up events. Because common 433 MHz handsets do not
  transmit a physical release bit, release/double-click are necessarily
  inferred from packet gaps.
- ✅ Host automation can react to device, RF gesture/raw RF, and USB lifecycle
  events and run board commands, shell commands, RF sends, or macro actions.
- ✅ A complete backup workflow can read flash, EEPROM, and Urboot/Urclock
  metadata into a unique timestamped directory with raw programmer log and an
  atomic SHA-256 manifest. Partial reads are marked `incomplete`. Unit/code
  tests passed; this new workflow was deliberately not executed against
  COM18/AVRDUDE during the host-only follow-up.
- ✅ `go test ./...` passed all 13 host packages and `go vet ./...` passed,
  including the RF/gesture, runtime, command, transport, configuration, and
  programmer tests after the newest melody/effect/API, stable-identity,
  primary-owner IPC, and full-backup changes. The Windows race detector was
  unavailable because the configured CGO tool failed before compilation; it
  is not counted as a pass.
- ✅ Current packaged artifacts are:

  | Artifact | Bytes | SHA-256 |
  |---|---:|---|
  | `controller.exe` | 2,799,616 | `DE4E017E3FA475EE9E639FF759E3D79289D5CAF42147F4344E46811BBD67C52C` |
  | `pccontroller.dll` | 8,792,378 | `88A7CEBFFFA043176CF0307E26880546379006F4ECAF016988E076E84BB11CBA` |
  | `pccontroller.h` | 2,108 | `61BB3FAF65771BDD4000C98EA218AE1262EFA53982ABF5AF7E01EA18BEBBD8C4` |

- 🟡 The Windows resource source now reports `DRSDavidSoft`, `PCController
  Host`, and `PCController Tool`, with numeric version `0.0.0.0` and string
  version `development`. The Go build injects a source hash and UTC build
  time. Regenerate and inspect the final EXE before marking packaging complete;
  the artifact hashes in the preceding table predate this identity change.
- ⚠️ The newest stable-identity/primary-owner build now authenticates COM18
  and serves live secondary IPC commands; the earlier external DLL smoke test
  also passed. Physical USB removal/reappearance, opt-in DTR
  reset-on-reconnect, macro playback, the current DLL against an external
  caller, and the AVRDUDE backup path still require validation.
- ✅ The completed newest TUI was launched and left running for the user.
  Its primary IPC owner authenticated `5DF10D05`; separate `exec` commands
  successfully shared that connection for HELLO, status, settings, sound, and
  melody operations without a second COM18 owner.

## Bootloader, programming, build scripts, and packaging

- ✅ MiniCore 3.1.2 UART0 Urboot is configured for ATmega328P model P,
  16 MHz external clock, 115200 baud, EEPROM keep, and 2.7 V BOD.
- ✅ The Urboot image/fuses were provisioned and verified through USBasp.
- ✅ UART upload uses AVRDUDE's `urclock` programmer through
  `arduino-cli`; UART remains the preferred routine programming path.
- ✅ Host `boot`/`program` commands provide probe/info/metadata/read/write/
  verify/start workflows, release exclusive UART ownership to AVRDUDE or
  Arduino CLI, then authenticate application HELLO after programming.
- ✅ Urboot/Urclock wire details are intentionally delegated to the maintained
  current MiniCore AVRDUDE/Arduino CLI backend rather than inaccurately
  described as a reimplemented native Go bootloader protocol. The custom
  native opcodes apply to the running application, not to Urboot.
- ✅ Host tooling owns Arduino CLI compile/update, Urclock, and guarded
  USBasp/AVRDUDE recovery workflows plus EEPROM-preservation safety checks.
  Direct Arduino upload is intentionally disabled.
- ✅ Host and scripts do not hard-code the supplied UPX extraction directory;
  they resolve tools from PATH/configured tool discovery.
- ✅ Root `build.cmd` and `build.sh` are thin argv-preserving launchers for one
  project-owned Node implementation. It retains VT-100/emoji stages, Go
  tests/vet, Win32 resources, deterministic identity, C ABI smoke testing,
  dependency notices, host/firmware manifests, and UPX pack/test. Neither
  active launcher invokes PowerShell; the obsolete `build.ps1` was removed.
- ✅ Firmware staging/cache state now lives in the user-local cache rather
  than below the sketch. This fixes the Arduino builder recursively copying
  its generated sketch tree back into itself, which caused the intermittent
  official-build wedge.
- ✅ `Tools/Firmware/firmware.mjs` and the root `firmware.cmd`/`firmware.sh`
  wrappers provide dependency-free build, explicit upload, content-watched
  build, HEX check, atomic manifest, backup, verify, probe, and metadata
  workflows with VT-100/emoji output.
- ✅ The firmware studio validates Intel HEX checksums, record/address/size
  boundaries, SHA-256, atomic manifests/backups, and never opens a port in
  dry-run. Its current hardware-free suite passes 19/19, including retention
  of the Controller compiler's matching deterministic manifest identity.
- ✅ Firmware-studio Urclock, guarded USBasp, and watched plans perform a
  hardware-free Controller compile plus strict method-specific HEX/SHA
  validation before starting a programmer subprocess. USBasp requires explicit
  troubleshooting authorization and the complete merged application-plus-
  Urboot image; generated `.eep` data is never written implicitly. Root
  compile/update/program actions now delegate to the same Controller surfaces.

### Tooling entry-point consolidation audit

- ❌ There is not yet one canonical generated `controller` executable. The
  audit found five ignored/generated Windows copies: `.build/host` reported
  version `0.2.0`; `Tools/Controller/.build-test-bin`,
  `Tools/Controller/.build-upx-bin`, and `Tools/Controller/bin` reported
  `0.4.0`; and
  `Tools/Controller/.cache/identity-build` reported a development build.
  These are build artifacts rather than five intended products, but their
  coexistence makes manual invocation and script resolution ambiguous.
- ✅ Active project-owned build and firmware entry points resolve exactly
  `Tools/Controller/bin`; the Node publisher writes only that canonical host
  package and removes audited legacy output locations after validation. The
  old root PowerShell implementation remains deprecated and is not invoked.
- ❌ None of the discovered executables proves it matches the current Go
  source. The current 114-file host source identity is
  `3AB8618F6D1AEF9922F732C0FD282DF05D01E3E600436135E71C5514C45829CD`;
  the newest executable that exposed an embedded source identity reported
  `503DE8209BA060BB6AC6295BF8E00CC3957EC212E551BDD7BA8F95C154D75692`.
  Release-version-only output such as `0.4.0` is insufficient evidence of a
  source match.
- ✅ Every active USBasp caller passes the source-required
  `--usbasp-troubleshooting` authorization; tests reject an unguarded ISP
  request and reject direct Arduino upload before any programmer subprocess.
- ✅ `build.sh` is now a 10-line launcher and `build.cmd` a small native CMD
  launcher. Both delegate policy to `Tools/Build/build.mjs`; frozen-identity
  tests prove their JSON command plans are identical. The former large
  `build.ps1` and duplicated Controller PowerShell build scripts were removed.
- ✅ The executable MiniCore build/program policy is owned by the Go programmer
  and consumed by the shared Node/CMD/Bash plan; the obsolete independent
  PowerShell copy was removed.
- ✅ The duplicate VirtualBoard PowerShell implementation was removed; its
  remaining Bash launcher configures the canonical CMake project directly.
- ✅ `build.cmd`, `build.sh`, `firmware.cmd`, and `firmware.sh` are thin
  launchers over their shared Node entry points; build/programming policy is
  not copied into platform wrappers.
- ✅ The host `boot` and `arduino` commands translate into the central
  `runProgram` implementation; they are convenience command forms rather than
  duplicated programmer engines. Document one canonical form and retain
  aliases only where they improve usability or compatibility.
- 🟡 Consolidation implementation is complete in source: one canonical host
  location, verified embedded source/build identity, audited legacy-output
  cleanup, guarded Controller programming, and hardware-free CMD/Bash plan
  tests. Final acceptance remains pending a fresh full host package run after
  concurrent Controller source work settles, with manifest/hash/UPX/resource/
  C ABI evidence recorded below.
- 🟡 A previous smaller boot-stable diagnostic build used source hash
  `E5109CA1`:
  32,056/32,384 flash bytes and 1,444/2,048 static SRAM bytes, leaving
  328 flash bytes and 604 static-RAM bytes.
- ✅ The current live build is `5DF10D05`: 32,374/32,384 application bytes
  and 1,455/2,048 static SRAM bytes, leaving 10 flash bytes and 593
  static-RAM bytes. Firmware source-set SHA-256 is
  `6416EB92A694C4CBEE7FFFFD66BA757033E3DE0FFBADAA44F044A46306BB7783`.
  Artifact SHA-256: application HEX
  `8BF7AE02FDCD6B10FF6B335FF49EEB55CCF59E4EE417CD27C3CE5AA5430FBC49`
  and application plus Urboot
  `E9AFF099A95862E36512BA4D1343487D219E6E68F58A1F589A7E5416C8327EBE`;
  the generated EEPROM image remains
  `788E5FFC44AE4EE912FE01F495951F96B0ACCD5705E184DCAEA197D8B64856A6`.
  The source-keyed build, verified UART upload/flash verify, and fresh all-page
  sweep passed. The immediately preceding image also passed the sensor
  stability and decimal-setting persistence tests on the unchanged paths.
- ✅ A wire-compatible size refactor recovered 214 flash bytes: 162 from
  block-copying the fixed telemetry fields and 52 from sharing the settings
  prefix. Compile-time size/offset guards preserve protocol and EEPROM layout;
  EEPROM output stayed byte-identical and static SRAM stayed 1,455 bytes.
- ✅ The proven rollback build is `E2DCE296`:
  32,382/32,384 application bytes and 1,452/2,048 static SRAM bytes, leaving
  2 flash bytes and 596 static-RAM bytes. SHA-256:
  application HEX
  `30E79054D5DF1BA359217066CBCE0E0138DAB6A74EFCE14E8898247EACCBF7A4`,
  application plus Urboot
  `588A2DBEB37723C064324432C8D48C5F270BDFDADA80D3CB88A09EA44FFF140E`,
  and EEPROM image
  `788E5FFC44AE4EE912FE01F495951F96B0ACCD5705E184DCAEA197D8B64856A6`.
  Its source-keyed build, verified UART upload, post-upload root-page sweep,
  and physical Buttons-1/2 test passed.
- ✅ `-mcall-prologues` and linker relaxation free flash without removing a
  requested capability. The tradeoff is only a small deterministic function
  entry/exit cycle cost.
- ⚠️ LCD, addressable LEDs, RF, full menus, reset telemetry, and other
  requested features remain included. The optional Wire timeout improvement
  is the one deliberate exception because it caused the real resets.
- ✅ Exact optional tradeoffs are documented: disabling LCD would recover
  1,328 flash/49 SRAM but lose LCD discovery/output/host messages; a
  no-bootloader profile exposes 384 more application bytes but loses the
  requested UART Urboot path. Neither cut was applied to the production
  feature set.
- ⚠️ The current normal build leaves only 10 application-flash bytes. Any
  further growth requires another measured optimization or a
  feature-profile/MCU choice.
- ✅ The current resource-stamped EXE was compressed and checked with UPX,
  and the DLL/header were rebuilt and smoke-tested with an external C caller.
- ✅ USBasp/ISP is no longer needed for normal operation: Urboot/fuses were
  previously verified, and the corrected official image was built, uploaded,
  flash-verified, and exercised through UART. It is safe to disconnect ISP
  while no programmer command is running.

## Native virtual board

- ✅ `Tools/VirtualBoard` provides a native C++17/CMake mock board that builds
  with desktop GCC-compatible tooling.
- ✅ It speaks the same COBS/CRC/opcode protocol over TCP at
  `127.0.0.1:8765`.
- ✅ It models settings, separate virtual MCU EEPROM, sensors and ROM IDs,
  door/BT/keys/RF events, relays, PWM, displays, macros, and the 11-pixel
  addressable strip.
- ✅ Virtual STATUS is the same 48-byte shape with reset cause at byte 43 and
  little-endian count at bytes 44-47; event type 7 uses the same six-byte
  payload and virtual application/bootloader resets advance a wear-levelled
  MCU-EEPROM journal separate from PC configuration.
- ✅ It provides interactive fault/input/sensor injection and portable
  PowerShell/Bash build scripts.
- ✅ Native unit tests and raw protocol smoke tests passed, including HELLO,
  addressable-strip ACK, and reset count `1 -> 2` with watchdog cause `0x08`.
- ✅ Host TCP tests cover fragmented/delayed transport behavior.
- 🟡 This is a behavioral simulator, not the same AVR translation unit
  compiled on the PC. Shared domain logic could be extracted further if
  cycle-accurate firmware parity becomes necessary.

## Remaining peripheral exposure

- ✅ Safe named APIs expose all requested functional peripherals: raw shift
  inputs, relays, PWM, INA219, temperatures/ROMs, door, BT, buzzer, TM1637,
  LCD, RGB, addressable strip, RF RX/TX, menu, reset, and I2C discovery.
- 🟡 There is no unrestricted arbitrary GPIO/register/I2C-write console.
  Reserved bits and the spare D12 pin therefore are observable/documented but
  not exposed as unsafe generic host writes. Clarify any additional intended
  peripheral before adding a bypass around the safety owners.

## 2026-08-01 host UX, bridge, automation, and firmware acceptance pass

This section records the newest requirements as independently verifiable
acceptance criteria. A status may be promoted to ✅ only after implementation
and the relevant automated, mock-TUI, screenshot, protocol, or live-hardware
check has passed.

### TUI structure and interaction

- 🟡 Provide a polished first-run setup animation and persist completion in
  PC-side configuration. Keep this page synchronized with the physical board:
  wait for authenticated HELLO/ready, start or observe the welcome melody,
  show initialization progress, and leave only after initialization and the
  melody finish or a bounded timeout gives a clear offline/error explanation.
- 🟡 Replace the bare monitor/prompt layout with navigable pages/tabs for the
  dashboard, measurements, outputs, board settings, app settings, menus, RF,
  programming/Urclock, automations, history/graphs, events, and console.
- 🟡 Make tables and submenus navigable with arrows and make controls usable
  with mouse clicks as well as the keyboard.
- 🟡 Add visible Open, Close, Reset, Refresh/Select-port, relay, motion, RF,
  PWM-slider, RGB, buzzer/melody, menu, and programming controls. Reflect
  externally initiated relay/PWM changes immediately.
- 🟡 Add configurable PC keyboard action bindings with true key-down/key-up
  hold semantics. Factory defaults are `A`/`S` for Side B Up/Down and `K`/`L`
  for Side A Up/Down. Digits `1`-`9` must be configurable action mappings that
  can target relays or PWM outputs rather than fixed relay numbers. Each binding
  supports momentary and toggle/latch behavior, with Ctrl selecting the
  configured alternate behavior. The host view must reconcile to authoritative
  live relay, PWM, and motion state after actions from any source, including RF,
  physical controls, automations, IPC, and network bridges.
- 🟡 Provide a configurable application title instead of fixing
  `PCController Tool` in source.
- 🟡 Style monitoring key/value pairs and groups distinctly; expand the names
  to `LED Temperature` and `BT Audio Temperature`; label BT Audio states as
  disconnected/blinking, connected/solid, off, or unknown.
- 🟡 Format measurements with adaptive SI units (`3.5 W`, not `3500 mW`) and
  independently configurable field visibility and display precision.
- 🟡 Do not flicker measurement age through `0/100/200 ms`; suppress age text
  while a sample is under 500 ms old and show it only when useful.
- 🟡 Provide configurable sample/poll rates and stop telemetry polling when
  no TUI, script, automation, TCP, IPC, or WebSocket subscriber requires it.
- 🟡 Keep configurable measurement history (24 hours by default), graph it,
  and show important events in a navigable timeline.
- 🟡 Implement empty-prompt history recall with Right Arrow, nested
  subcommand completion with Tab or Right Arrow, and never print a literal
  `completion:` diagnostic into the ordinary console.
- 🟡 Organize commands by task, use semantic color styling, add `clear`,
  `quit`, and `exit`, and hide raw HELLO bytes unless debug logging is enabled.
- 🟡 Exercise the real TUI through Windows automation, inspect screenshots at
  representative sizes/pages, and fix visual or focus/navigation defects
  before marking this section complete.

### Configuration, menus, melodies, and programming surfaces

- 🟡 App Settings must expose all watched PC JSON settings. Board Settings
  must query/edit/save the independent MCU EEPROM settings and distinguish
  live values from persisted values.
- 🟡 Query a live menu catalog from the board, show ID, short display label,
  human description, current page/submode, and seven-segment/LCD preview;
  support `menu list`, direct jump, and Previous/Next/Dec/Inc.
- 🟡 Surface native opcode operations and Urboot/Urclock probe, metadata,
  flash, verify, flash/EEPROM read-backup, Arduino CLI, AVRDUDE, reset, and
  reconnect controls in the TUI—not only in the text shell.
- 🟡 Support board-resident melodies where firmware capacity allows and
  PC-streamed melodies with custom notes/durations/gaps plus `melody create`,
  save, preview, play, stop, and configuration hot reload.
- 🟡 Add PC-side macro recording with precise monotonic relative timing.
  Board-originated activation events include MCU monotonic timestamps/deltas
  where supported; USB/network arrival time is never authoritative. Durable
  names, IDs, categories, user colors, and the macro library remain PC-side.
  During playback the host streams allow-listed opcode/payload records in chunks
  into a bounded AVR SRAM circular buffer; the MCU schedules them locally while
  the host refills ahead. Queue ordinary native-protocol opcode/payload records
  for relay, motion, PWM/MOSFET, buzzer/melody, seven-segment/LCD messages, RF
  transmit, menu/front-panel, and extensible commands. Do not spend flash on a
  second MCU macro allow-list or duplicate policy layer: dispatch through the
  existing firmware command paths so their payload validation and inherent
  motion/output interlocks remain authoritative. The host/bridge owns rich
  permission and security policy; the MCU adds only minimal self-recursion and
  queue-integrity protection. Telemetry reports accepted/executed
  indexes, fill/free space, underruns, late count, maximum timing error,
  completion/cancel/error, and a final faithful flag. Mirror macro name/ID,
  elapsed time, duration, and current step on TM1637/LCD/TUI. Physical keys,
  host, and every API can cancel. Emit synchronized lifecycle/health events
  through CLI, TUI, IPC, REST, RPC, WebSocket, and bridges. Expose list, record,
  stop/save/discard, play, progress, and cancel through hosted custom-menu
  capability 19. Automations can start by name/ID, cancel, or replace according
  to an explicit concurrency policy and can trigger on started/progress/
  completed/cancelled/error/faithful/underrun. The active queue exists only in
  AVR RAM, the same command-path safety and queue-health gates apply to every
  source, and the
  final report records exact flash/SRAM costs and tradeoffs. This remains a
  release blocker until live timing and safety behavior are verified.
- 🟡 Ensure opening the app does not reset the board by default. DTR reset on
  reconnect remains an explicit, default-disabled PC setting.
- 🟡 Add a synchronized Front Panel view and native snapshot: exact four
  TM1637 segment bytes/decimal or colon mask/brightness/blink state, 2x16 LCD
  cells/address/backlight, active-key mask, current menu page, and submenu or
  program mode. Render it in TUI/API clients and update it from physical board
  changes.
- 🟡 Expose four remote front-panel buttons with mouse and keyboard down/up/
  hold/gesture semantics. Route injected input through the same board menu
  state machine and source-tagged event path as physical keys. Poll front-panel
  snapshots only while at least one TUI/API/IPC/WebSocket subscriber needs
  them; serial itself remains connected.
- 🟡 Add host-defined front-panel menus whose definitions live in PC-side
  JSON/YAML/TOML rather than AVR flash. Support nested pages, labels, typed
  values, ranges/steps/options, read/write callbacks, confirmation, and board-
  requested or host-initiated sessions. While captured, physical keys navigate
  the PC-owned menu and every seven-segment/LCD change is mirrored in the
  TUI/API preview; host loss releases capture and restores the EEPROM default.
- ✅ The persistent definition/core-routing portion now has table-driven
  JSON/YAML/TOML acceptance through atomic parse/write, filesystem watch,
  `Store.Update`, subscription, and `Manager.UpdateConfig`. An active definition
  edit pushes the exact updated TM1637 label and both LCD rows; an inactive edit
  emits only its normalized event; hiding or deleting the active node releases
  capture. The test exposed and fixed a cap19 drift where the bridge re-read the
  selected item instead of using the exact `DefinitionChange` preview.
- ✅ Host menu node IDs/parents, labels, titles/content, brightness, visual
  style, flags, items, and built-in overrides persist in PC config. Read-only or
  disabled K4 is a true no-op with a short denied cue/event, and cap19 provides
  bounded host-side blink/dim/alternate/pulse approximation without EEPROM
  writes. Per-page brightness cannot override the global MCU brightness on
  cap19 and is documented as a compatibility limit.
- ✅ Compact local menu layout schema 2 packs 15 page ranks into eight nibbled
  bytes and retains schema-1 host decoding. Host/mock cap24 directory/content/
  state and retry behavior remain available for future hardware; current AVR
  capability and size accounting remain honestly cap23 plus cap19 fallback.
- 🟡 Include host-menu providers for PC/device status, IP/network/API state,
  host and board settings, commands, and guarded operating-system actions.
  Expose the same menu model through CLI, IPC, REST, JSON-RPC, WebSocket,
  Socket.IO, scripting, and the physical front panel.
- 🟡 When the host owns LCD presentation, optionally mirror the active Console
  prompt viewport and completion/result context to the 2x16 LCD. A priority
  arbiter temporarily displays error/HOT/door/motion/relay/RF events, then
  restores the prompt; routine telemetry must not cause LCD flicker. When the
  host heartbeat expires, the fallback is line 1 `PC OFFLINE`, line 2
  `CONNECT USB` (the 17-character full phrase may scroll slowly).

### IPC, WebSocket, USB lifecycle, and primary ownership

- 🟡 A single primary process must own the serial port. Secondary CLI/TUI/API
  processes forward commands and consume events through IPC instead of
  competing for COM18.
- 🟡 Add a WebSocket server on the IPC service, with authenticated or safely
  local command requests and subscriptions for status, USB, RF, door, BT,
  keys, outputs, programming, reset, automation, and shutdown events.
- 🟡 IPC/WebSocket clients must be able to request open, close, reconnect,
  reset, quit/exit, programming operations, and every ordinary controller
  command with correlated results.
- 🟡 Accept explicit COM names, friendly product/name matches, and VID/PID
  filters. Use stable CH340 identity defaults with configuration/flag
  overrides, persist the last successful PC-side device, auto-select a unique
  match, and prompt when several devices match.
- 🟡 Use Windows device-arrival/removal notification as the reconnect trigger
  rather than periodic full-port enumeration; emit precise disconnect and
  reconnect events to TUI, scripts, IPC, and WebSocket consumers.
- 🟡 Treat controller-owned discovery as authoritative for development and
  deployment. Investigate and regression-test why a direct WMI/CIM query saw
  only COM1 while `controller.exe ports` correctly found COM18 and COM19 via
  its live Windows device path; never block programming solely on WMI/CIM.
- 🟡 Keep the serial application protocol enabled, open, and auto-reconnecting
  by default even with no measurement subscribers. Subscription accounting may
  stop status polling, but must not close UART or suppress asynchronous board
  events. DTR reset is independent and default-disabled; explicit Close pauses
  until the user resumes Open.
- 🟡 Provide durable, versioned JSON-RPC and REST request/response APIs plus a
  WebSocket client/server event and command channel. Use JSON on the wire and
  support human-editable JSON, YAML, or TOML configuration where that format
  is selected explicitly.
- 🟡 Let one controller host bridge to another over the network for remote
  programming, diagnostics/monitoring, configuration, commands, queries, and
  event subscriptions. Preserve the single serial owner and expose remote
  lifecycle/errors exactly as local IPC events.
- 🟡 Add configurable global hotkeys that can invoke board commands, inject
  front-panel key gestures, or control the primary host application. Registration
  conflicts and unsupported platforms must be reported without preventing the
  controller from starting.
- 🟡 Add an explicit, default-disabled virtual-key automation action. Physical
  key gestures, stable learned-RF tuples, door/BT/device events, scripts, IPC,
  and APIs may emit a configured Windows key such as an arrow or F13. Validate
  named/numeric keys, pair down/up safely, rate-limit repeats, release held
  keys on disconnect/exit, and emit an audit event for every injection.
- ⚠️ Guarded host OS information/actions are source-complete for custom menus,
  shell, and the shared API command path: inspect IP/system state; request Lock,
  Suspend, Hibernate, Restart, or Shut down; and read/set primary-monitor
  brightness from 0..100 through Win32 DDC/CI. Power and brightness policies
  are independently disabled by default, file-watched, audited, and tested with
  an injectable backend; power additionally requires allow-list plus explicit
  confirmation. Unsupported monitors fail gracefully. A harmless live DDC/CI
  read/menu preview remains to be checked, while disruptive power actions must
  not be exercised merely to claim acceptance.
- 🟡 Show configurable actionable desktop notifications for important
  door/HOT/motion/RF/USB/programming/error events. Toast buttons must return
  through the authenticated primary process and the same safety-checked command
  path as TUI, IPC, and scripts.
- 🟡 Advertise and discover controller-host instances with mDNS and SSDP
  where the platform/network allows it. Publish only non-secret service metadata;
  discovery never grants control authority.
- 🟡 Provide an inbound HTTP service and configurable outbound webhooks for
  GET/POST/PUT/PATCH/DELETE as appropriate, plus bidirectional WebSocket client
  and server modes. Add genuine Socket.IO protocol compatibility separately
  rather than relabeling an ordinary WebSocket endpoint.
- 🟡 Add a typed, source-tagged text-message envelope that can be sent and
  received by a local client, HTTP/WebSocket/Socket.IO server, remote bridge, the
  board, and its LCD. Messages may expose configured actions, but those actions
  must be authenticated, authorized, logged, and routed through board safety
  guards.

### Motion, enclosure, RGB, audio, and board automation

- 🟡 Correct the motion wiring model: R1/R3 select Direction for Sides A/B;
  R2/R4 control the corresponding Output/enable. Preserve break-before-make,
  safe reset, stop, and side isolation.
- 🟡 Add a persisted motion-door policy with `closed`, `open`, `always`, and
  `never`; factory default is `always`. Apply it consistently to local keys,
  learned RF, host commands, macros, and automations.
- 🟡 Restore the nonblocking enclosure-light fade between configured Off and
  On brightness on every door transition, without jitter or an initial jump,
  and add a regression test for both directions.
- 🟡 Configure coherent door-open/closed RGB base colors and transition
  animations without unrelated intermediate colors.
- 🟡 Replace independent RGB overrides with one priority arbiter. Required
  states are: HOT red/orange breathing plus buzzer; Running with door open hard
  red warning flash; PC disconnected red; RF receive violet/purple breathing;
  Running with door closed orange/yellow; Idle with BT Audio connected blue;
  BT Audio powered off green/red blink; otherwise Idle green. ProgramState is
  PC-owned and must be mirrored to the board rather than inferred from door.
  Ease/damp transitions into and out of informational states and restore the
  prior/base state smoothly; warning/critical states may jump and hard-blink.
- 🟡 Add configurable door-open, door-close, relay-on, and relay-off audio
  cues. Persist board-owned cue choices/mappings in MCU EEPROM and retain
  Silent-mode authority.
- 🟡 Allow EEPROM-backed board automations to bind door, BT Audio,
  host-connected/disconnected, relay, and other control events to safe relay,
  motion-stop, PWM, RGB/audio, or 433 MHz transmit actions.
- 🟡 Add equivalent optional PC-side scripting/automation triggers for door
  and BT Audio events; no such action is enabled by default.
- 🟡 Add a PC-owned operational `Idle`/`Running` ProgramState with readable
  source/reason text and change events. The embedding application, TUI, CLI,
  IPC/API, or an activity lease such as macro playback can set it; explicit
  consumer ownership remains authoritative and the door must never determine
  this state. Mirror the state/event text to the board/front-panel displays.
- 🟡 If the enclosure opens while ProgramState is `Running`, immediately emit
  a source-tagged warning for TUI, scripting, IPC/API, and bridges and invoke
  configurable PC-host beep and actionable-toast cues. This warning is
  PC-owned/configured, not stored in AVR EEPROM, and ends or clears coherently
  when the door closes or operation returns to `Idle`.
- 🟡 Make Status the default board page and briefly display incoming actions
  such as `R5` then flashing `On`/`Off`; add the Puzzle seven-segment animation
  only if measured flash savings make it safe.
- 🟡 Let the host provide date/time and optional display/mapping labels. The
  board must expose host-connected/disconnected state and be able to apply a
  configured safe automation when the host disappears.

### RF learning, mapping, latency, and capacity

- ✅ Remote A has been learned as ID 1, raw code `0x00F30BC8`, 24 bits,
  protocol 1, approximately 341 us pulse, and mapped to Side A Up. Live host
  events demonstrated Down, Hold, Repeat, Up, and raw receive events. A stale
  earlier learned record ID 0 (`0x00F40BC8`) still needs review/removal.
- 🟡 Resume guided B/C/D capture only after this UX pass. Ask for one labeled
  button at a time, show its exact raw identity, request/confirm the intended
  action, then persist the mapping.
- 🟡 Remove the firmware's implicit default Key-2 mapping. A newly learned RF
  record is Unmapped until the local UI or host explicitly assigns it.
- 🟡 Add finite and indefinite learn sessions, explicit start/end/cancel/full
  notifications, and an optional multi-learn session that accepts several
  distinct buttons before it ends.
- 🟡 Raise learned-record capacity from eight to at least twenty if EEPROM
  layout and endurance permit it; retain CRC/version migration and individual
  list/remove/remap operations.
- 🟡 Reduce receive-to-action latency and make a short single burst reliably
  invoke its mapped action. Validate repeat-gap/hold behavior against the real
  A/B/C/D handset after implementation.
- 🟡 Let board menus show an action catalog before learning where feasible;
  compact IDs may be resolved to human labels supplied by the connected host.
- 🟡 Add one PC-side RF number-format setting and apply it uniformly to TUI,
  Console, CLI, JSON/API presentation, exports, logs, and mapping dialogs.
  Supported views must include hexadecimal and decimal without changing the
  raw `u32` value sent on the protocol.
- 🟡 Store PC-only RF names, categories, colors, notes, and other presentation
  metadata by the stable `(code,bits,protocol)` identity rather than the
  mutable EEPROM record ID. Provide a searchable/filterable action picker
  instead of forcing users to remember numeric action IDs. Categories are
  named by the user and manually assigned one of these choices in this exact
  UI order: red, blue, violet/purple, green, white.
- 🟡 Add an explicit reorder/renumber workflow for learned EEPROM records.
  It must read the current board list, show the proposed order, write it
  transactionally where the firmware permits, read it back, and update PC
  metadata references without confusing record ID with RF identity.

### Development EEPROM, repository, licensing, and documentation

- 🟡 Development firmware may erase/reinitialize the current MCU EEPROM and
  adopt a clean layout; do not spend flash on whole-project EEPROM migration
  until the layout is declared stable. Retain validation/CRC and document that
  a development flash can reset saved settings, RF records, automations, and
  reset telemetry.
- 🟡 Initialize Git, audit generated/private files, add safe ignore rules, and
  publish the project through the already-authenticated `gh` CLI/API. Use an
  `MIT OR BSD-2-Clause` dual-license for original project code unless an
  incorporated dependency requires different treatment; preserve all
  third-party notices and do not relicense dependencies.
- 🟡 Canonically rename and organize Markdown documents into a starter-friendly
  reading order. Repair every relative link and provide complete architecture,
  hardware, firmware, host/TUI, protocol/API/RPC/WebSocket, configuration,
  build/upload/Urclock, simulation, troubleshooting, safety, and licensing
  coverage.
- 🟡 **Finalization-only code documentation gate:** after behavior, opcodes,
  EEPROM, and flash layout are stable, give public/domain functions, state,
  configuration fields, hardware assumptions, and non-obvious invariants short
  descriptive one-line or compact multi-line comments. Prefer comments that
  explain purpose, ownership, timing, units, safety, or constraints; do not add
  noise that merely repeats the syntax.
- 🟡 **Finalization-only firmware structure gate:** reduce `PCController.ino`
  to high-level composition, `setup()`, and a short service-oriented `loop()`.
  Move domain state and implementation functions into focused project classes
  or modules with clear interfaces, without changing behavior or increasing
  AVR flash beyond the final measured budget. Perform this only after the
  active safety/protocol/size work is frozen, then rebuild and regression-test.
- 🟡 **Finalization-only conversation audit:** extract the complete user-message
  sequence from the project session JSONL turns, normalize spelling only for
  search, map every distinct requirement to this checklist and implementation
  evidence, and perform one final missing/regression/contradiction review. Do
  not mark an item complete merely because it appeared in a plan or comment.
- 🟡 Measure a host-driven LCD profile: MCU exposes bounded generic I2C
  probe/read/write/write-read operations, the PC owns rich device descriptions
  and LCD text/layout, and a tiny MCU fallback displays line 1 `PC OFFLINE` and
  line 2 `CONNECT USB` (or slowly scrolls `CONNECT USB TO PC`) when possible.
  Ownership is deliberately cooperative rather than restricted:
  a short expiring host lease pauses local I2C service, then known local drivers
  refresh after release. Raw access may intentionally alter INA219 `0x40`, PWM
  `0x41`, LCDs, or future devices. Record exact flash/SRAM savings, cache/reset
  risks, and offline functionality lost before making this the production
  profile.
- 🟡 **Final-report menu migration decision:** inventory every current
  firmware-resident board menu and every PC-hosted custom menu. For each
  sensible migration in either direction, state why it may be desirable,
  offline/host-dependency and protocol consequences, functionality gained or
  lost, and measured flash/SRAM saving or cost where a comparison build is
  available (otherwise label the number as an estimate). Offer the choices to
  the user and do not silently change menu ownership before a decision.
- 🟡 **Final-report peripheral initialization catalog:** list the effective
  startup/runtime parameters for INA219, PWM/PCA9685, both DS18B20 sensors,
  TM1637, shift registers, RF RX/TX, LCD/cooperative I2C, buzzer/timer,
  relays/interlocks, status RGB/WS281x, application UART, and Urboot/Urclock.
  Include addresses/pins, bus/sample/conversion rates, resolution/averaging,
  polarity, timing, boot mode, rationale, and safe alternative values the user
  may request; distinguish compiled constants from EEPROM/PC-configurable data.
  The source-backed peripheral portion is complete in
  [Hardware Initialization and Tuning](Hardware-Initialization-and-Tuning.md);
  the final report must still attach the frozen build hash and observed
  electrical results rather than presenting configured values as measurements.
- 🟡 Make the host the normal entry point for every compile, upload, verify,
  backup, and recovery workflow. Before any flash write, automatically capture
  flash and EEPROM into the host data directory through Urclock; allow a
  hidden/advanced USBasp fallback only when the UART bootloader path cannot
  work. Store flash blobs by SHA-256, reference the hash in backup names and
  manifests, and never duplicate an identical previously stored flash image.
  A failed backup must block the write unless the user gives an explicit,
  logged override.
- 🟡 Replace the firmware identity's bulky compiler date/time strings with a
  backward-decodable 32-bit packed build timestamp: year offset, month, day,
  hour, minute, and seconds divided by two. The host must render it as
  `YYMMDDHHMMSS`, retain the raw value, and pair it with the source/application
  hash in telemetry, backups, diagnostics, and programming verification.
  Schema-2 encode/decode, schema-1 compatibility, build injection, JSON/TUI
  display, and backup metadata are implemented and covered by exact-vector
  tests; final firmware rebuild and live HELLO verification remain pending.
- 🟡 Distinguish a live settings export (native protocol plus board identity)
  from an offline EEPROM-image parser. Both must preserve unknown bytes,
  identify the EEPROM layout/hash they understand, and never merge MCU EEPROM
  state into the PC configuration file.
- 🟡 Add named-region Intel HEX inspection and carefully bounded patching for
  application flash, bootloader, EEPROM images, and known metadata regions.
  Validate record checksums/address bounds, show before/after SHA-256 hashes,
  retain the unmodified source, and require a verify/readback after writing.
  The generic guarded patch engine exists, but firmware identity is not yet a
  declared region: the 32,162-byte pre-upsert image had only 222 bytes free,
  so a duplicate PROGMEM magic record was intentionally rejected. HELLO and
  manifests are canonical; identity changes require rebuilds for now. Recheck
  the exact final byte count after RF upsert lands.
- 🟡 On graceful host exit, write an atomic PC-side snapshot containing board
  identity, last status/settings/menu, connection/reset metadata, active
  programming operation, and artifact hashes. This is diagnostic host data,
  not an EEPROM mirror and not proof that an interrupted write completed.
- 🟡 Keep raw device uptime in the live status/snapshot API and show a readable
  uptime alongside the other monitoring parameters in the TUI, CLI, REST,
  WebSocket, Socket.IO, history, and scripting surfaces.
- 🟡 Consolidate the firmware's ordinary service time into one global loop
  snapshot updated once at the beginning of `loop()`, and let domains consume
  that consistent tick instead of passing many independent `millis()`/`now`
  values where practical. Interrupt handlers must remain independent, and the
  refactor must not weaken wrap-safe arithmetic or deterministic tests.
- 🟡 Define the future migration architecture now: version/hash each board
  settings and RF/automation layout, parse old images off-device, generate a
  proposed new image or explicit protocol changes, back up first, and verify
  readback. Do not spend current AVR flash on whole-history migration code
  during development.

## Final hardware validation and handoff

- ✅ Earlier hardware confirmation: firmware loaded, the board started, the
  boot melody played, menu/TM1637 worked at that stage, UART electrical wiring
  was corrected, about 12 V became visible, and the buzzer glitch was fixed.
- ✅ The root-page menu-reset regression is closed for the current
  host-driven path: official `5DF10D05` swept pages 0-13 and held tLED/tBT for
  12 one-second samples each with reset count 2075 unchanged for approximately
  40 seconds. The failed out-of-line experiment was rejected.
- 🟡 5DF implements a nested Sound/display-brightness/status-brightness/
  Ready-color/voltage-decimals/current-decimals sequence and edit-value
  blinking. It still uses the flat root page list, so the requested broader
  category hierarchy remains missing. The six fields need physical-key
  validation.
- 🟡 Buttons 1 and 2 produced physical Down/Up/Click events on E2 and reset
  count stayed 2048. Repeat those checks on 5DF and expand to Buttons 3/4,
  double-click, and hold tests while monitoring reset count.
- ⚠️ Earlier firmware's boot melody and the post-fix buzzer were user
  confirmed. The final host has now persisted `silent=false` and completed a
  streamed `notify` melody. Re-check the 5DF boot melody, one clean beep per
  press, and save/discard cues by listening to the board.
- ⚠️ Toggle the door and verify instant open/close events, default-page
  return, channel-11 fade, RGB cues, and motion emergency exit.
- ⚠️ Toggle the BT module and verify Off/On/Blink plus RGB/TUI/IPC events.
- ⚠️ Turn illumination on/off while logging both ROMs; verify tLED warms and
  tBT stays comparatively cool.
- ⚠️ Visually validate smooth TM1637 values, PWM auto demo, enclosure fade,
  power indicator, RGB animations, and the D6 strip.
- ⚠️ With mechanisms made safe, validate relay identification, both side
  directions, break/settle interlock, release-to-stop, R5-R8
  toggle/momentary, and door-close stop.
- ⚠️ Learn a harmless RF button, list/map/remove it from the host, test short
  press and hold/repeat/up events, then verify INT1 transmit with a receiver.
- ⚠️ Connect an LCD if required and validate `0x27`/`0x3F`, both rows, host
  text, and concurrent TM1637 operation.
- ⚠️ Unplug/replug USB and verify lifecycle events, authenticated reconnect,
  default-disabled DTR reset, opt-in reset-on-reconnect, TUI update, and IPC
  notification.
- ⚠️ Run a harmless named macro that labels TM1637, writes LCD text, changes
  one PWM channel/one general relay, and then cancel it.
- 🟡 Perform a final end-to-end interaction pass over every implemented host
  TUI page/control, shell/CLI, persistent config reload, primary IPC/API,
  network bridge where locally testable, programming/backup path, virtual
  board, and every load-safe live board peripheral/opcode. The final report
  must list every project area, the exact test/evidence for it, and any item
  still requiring human observation rather than collapsing these into a
  single overall pass/fail claim.
- 🟡 When a human RF/button/visual test is actually needed, play a distinctive
  repeating host-streamed ringtone melody and a visible attention LED effect;
  stop/cancel both after acknowledgement and never use the notification loop
  merely as background noise.
- 🟡 The resource-stamped/UPX-verified TUI is now running against COM18 and
  secondary commands successfully use its primary IPC connection. The
  operating guide is complete. This item remains partial only because the
  load-safe hardware exercises listed above still require the user's physical
  operation/observation.
