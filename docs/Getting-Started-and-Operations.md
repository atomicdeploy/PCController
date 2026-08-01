# Getting Started and Operations

This is the canonical operating guide for the physical ControllerBoardMini,
the `controller` PC host, and the native virtual board. The current live
firmware is build `5DF10D05`; the proven rollback image is `E2DCE296`. The
packaged host is under `Tools/Controller`.

For exact implementation status, including items that still need physical
validation, see the [Project Checklist](Project-Checklist.md).
For the full TUI, PC-only configuration, hotkey/toast, discovery, webhook, and
remote-bridge guide, see
[Host Configuration and Integrations](Host-Configuration-and-Integrations.md).

## Current tested baseline

The following current path has been exercised on the board attached as COM18:

- MiniCore 3.1.2 UART0 Urboot/Urclock at 115200 baud;
- official firmware `5DF10D05`, UART-uploaded and flash-verified;
- authenticated HELLO identity
  `build=5DF10D05 date=Jul 31 2026 time=07:13:06`;
- all 14 root menu pages, including 12 one-second samples on each temperature
  page, without an unexpected reset; reset count remained 2075 throughout
  that approximately 40-second sweep;
- immediately preceding AFD validation of voltage/current precision at zero,
  one, and two decimals from both the host and local settings submenu,
  including reset persistence; 5DF retained and reported the final
  two-decimal values;
- INA219 at `0x40`, PWM expander at `0x41`, and both DS18B20 ROMs;
- the immediately preceding AFD 50-sample sensor check with 4 mV supply span
  and 3 mA current span; fresh 5DF page/temperature validation had no
  framing/CRC errors;
- prior physical Buttons 1 and 2 testing on the E2 rollback image through
  Down, Up, and Click events;
- current 5DF host-driven HELLO/STATUS/menu/settings validation, plus the
  earlier packaged-host HELLO, STATUS, SETTINGS, temperature-list, and
  I2C-scan smoke test against the A7 image;
- the final TUI running through the CH340 selector and primary IPC owner;
  secondary commands reported 12.226 V, 263 mA, relays/PWM off, zero
  framing/CRC errors, `silent=false`, and a completed `notify` melody;
- the current EXE/DLL/header builds, an earlier external C-ABI smoke test,
  current host unit tests, and current host static analysis.

Current artifact identity:

```text
Flash: 32,374/32,384 application bytes (10 free)
SRAM:  1,455/2,048 static bytes (593 free)
Source set:
       6416EB92A694C4CBEE7FFFFD66BA757033E3DE0FFBADAA44F044A46306BB7783
HEX:   8BF7AE02FDCD6B10FF6B335FF49EEB55CCF59E4EE417CD27C3CE5AA5430FBC49
Merged application + Urboot:
       E9AFF099A95862E36512BA4D1343487D219E6E68F58A1F589A7E5416C8327EBE
```

Current host artifacts:

```text
controller.exe  2,799,616 bytes
  DE4E017E3FA475EE9E639FF759E3D79289D5CAF42147F4344E46811BBD67C52C
pccontroller.dll  8,792,378 bytes
  88A7CEBFFFA043176CF0307E26880546379006F4ECAF016988E076E84BB11CBA
pccontroller.h  2,108 bytes
  61BB3FAF65771BDD4000C98EA218AE1262EFA53982ABF5AF7E01EA18BEBBD8C4
```

The firmware currently leaves only 10 application-flash bytes. Treat every new
firmware feature as a measured tradeoff; do not add code and assume it still
fits.

Still pending physical validation includes Buttons 3/4 and advanced gestures,
all editor submodes, door/BT transitions, loaded relay motion, PWM/RGB/strip
visuals, a real 433 MHz handset, an attached LCD, macros on harmless outputs,
USB removal/reconnect, and listening to the restored melody/key cues.

The 5DF local UI still has a flat 14-page root menu with modal editors. The
`Snd` page now opens a nested `Snd` → `diSP` → `StBr` → `CoLr` → `V-dP` →
`A-dP` settings sequence and editing values blink at approximately 300 ms
intervals. The broader requested root category hierarchy remains pending, but
display brightness, status brightness/color, sound, and reading precision are
now locally adjustable in that submenu.

## Safety first

Before operating outputs:

- Disconnect or make safe any mechanism that could move unexpectedly.
- Use `relay side ...` for R1-R4 motion. Direct R1-R4 writes are routed through
  the same disable/break/direction/settle sequencer, but the named side command
  makes intent clearer; direct writes are primarily for R5-R8.
- The host performs a fresh fail-closed door-status preflight before starting
  R1-R4, side motion, or a motion macro; stop/off commands remain available.
  That query-plus-command pair is not atomic, so it complements rather than
  replaces firmware/local MOVE interlocks.
- Test PWM Auto only after making channels 0-10 safe. Auto does not own
  enclosure, power, or RGB channels 11-15.
- Use `pwm mode off` to stop the commissioning demo without touching the
  system-owned outputs. `pwm off` is an emergency all-16-channel clear and can
  also extinguish enclosure, power, and status-light outputs.
- Start RF learning and automation with a harmless menu action, not a relay or
  motor action.
- A macro **finish** may preserve its final output states. A macro **cancel**
  turns off only the relay/PWM outputs that macro claimed.
- Only one process can own COM18 directly. Current host instances share the
  primary owner's IPC path, but close Arduino Serial Monitor, old host builds,
  and unrelated serial utilities before opening the board.
- Do not run UART upload and ISP programming at the same time.

USBasp is no longer required for routine use. Urboot/fuses and the UART upload
path are verified, so ISP may be disconnected whenever no programmer command
is running.

## Quick start on COM18

From the project root:

```powershell
.\Tools\Controller\bin\controller.exe ports
.\Tools\Controller\bin\controller.exe exec --port COM18 hello
.\Tools\Controller\bin\controller.exe exec --port COM18 status
.\Tools\Controller\bin\controller.exe exec --port COM18 settings
.\Tools\Controller\bin\controller.exe exec --port COM18 temp list
.\Tools\Controller\bin\controller.exe exec --port COM18 i2c scan
```

Expected identity:

```text
PCController ... build=5DF10D05 date=Jul 31 2026 time=07:13:06
```

The build hash and compile date/time are the operational firmware identity;
the leading legacy `0.0.0` compatibility bytes are not a semantic release
version.

Expected I2C devices on the current hardware are `0x40` and `0x41`. An
optional LCD adds `0x27` or `0x3F`.

Start the interactive Charm TUI last:

```powershell
.\Tools\Controller\bin\controller.exe tui --port COM18
```

Auto-detection is also available:

```powershell
.\Tools\Controller\bin\controller.exe
.\Tools\Controller\bin\controller.exe tui --device "USB-SERIAL CH340"
.\Tools\Controller\bin\controller.exe tui --device 1A86:7523
.\Tools\Controller\bin\controller.exe tui --device serial:ABC123
.\Tools\Controller\bin\controller.exe tui --device instance:USB\VID_1A86...
.\Tools\Controller\bin\controller.exe tui --vid 1A86 --pid 7523 --name CH340
```

`--device` accepts a COM ID, friendly/human name, `VID:PID`, `serial:VALUE`,
or `instance:VALUE`. Filters only narrow the candidates. The host accepts a
port only after a valid native `PCController` HELLO. A unique stable match is
automatic; an interactive TUI/shell/programming command presents a numbered
chooser when several adapters remain ambiguous. After authentication, the host
stores stable identity fields and the current port in PC JSON as
`connection.last_device`; it never writes those fields to MCU EEPROM.

The currently observed adapter is:

```text
USB-SERIAL CH340 (COM18), manufacturer wch.cn
VID 1A86, PID 7523
Windows instance USB\VID_1A86&PID_7523\6&2CC1445A&0&3
```

The current serial mode explicitly opens with DTR and RTS inactive; opening the
host is not intended to reset the board. `reset_on_reconnect` also defaults
false. Treat an unexpected boot-count increment on ordinary Open as a defect
to investigate, not as normal host behavior. Explicit reset uses one DTR-only
pulse and leaves DTR inactive afterward.

## TUI and shell controls

The Charm TUI has Dashboard, Outputs, Menus/Front Panel, Board Settings, App
Settings, RF, Programming, Automation, Events/Graphs, and Console pages. It
displays connection identity, grouped measurements, door and BT Audio state,
keys/events, relays, menu/mode, and PWM values. Run `temp list` for ROM
identities and `pwm get` for every logical PWM output.

```text
1..0        open a named page directly
Left/Right  switch pages, or adjust the selected setting/output
Up/Down     navigate rows; in Console, navigate command history
Enter       activate the selected control or submit Console input
Tab         next page when input is empty; otherwise accept completion
Shift+Tab   previous page or previous completion
Ctrl+O      resume authenticated auto-connect
Ctrl+P      open/close the port picker
Ctrl+X      close the port and pause reconnect
Ctrl+R      request a graceful application reset
F1..F4      inject a press on remote front-panel Key 1..4
Shift+F1..F4 inject a hold gesture on remote front-panel Key 1..4
PgUp/PgDn   scroll Console or history where available
Ctrl+C      exit
```

Mouse clicks select tabs, rows, buttons, relay toggles, and PWM sliders.
Global hotkeys are separately configurable and work while another application
has focus; see the host-integration guide linked above.

The same command language is available without the full-screen UI:

```powershell
.\Tools\Controller\bin\controller.exe shell --port COM18
.\Tools\Controller\bin\controller.exe exec --port COM18 status
.\Tools\Controller\bin\controller.exe monitor --port COM18 --interval 500ms
.\Tools\Controller\bin\controller.exe monitor --port COM18 --json
```

Do not make two independent processes open the same serial port. Current
`controller.exe` instances avoid that by routing later clients through the
primary owner; older host binaries and unrelated serial tools do not.

Within this host, the first long-running TUI, shell, or IPC server becomes the
primary serial owner at `127.0.0.1:8787`. Later host `exec`, batch, monitor,
reset, shell, and programming invocations route commands/events through that
primary process instead of reopening COM18. A second interactive TUI becomes
an IPC-backed secondary console. External serial tools still cannot share the
port.

### Command index

These commands are accepted by the TUI prompt and plain shell; one command can
also follow `exec --port COM18`:

```text
# Connection, status, and raw protocol
ports
open [PORT]
close
reconnect
hello
status
voltage
current
temp [list|scan]
stream PERIOD_MS
settings
settings decimals VOLTAGE CURRENT
settings color INDEX
settings set FLAGS LIGHT ON OFF DISPLAY STATUS PWMBOOT STREAM
  [DEFAULT_PAGE SAVE_LAST [STATUS_COLOR VOLTAGE_DECIMALS CURRENT_DECIMALS]]
event latest
event wait [KIND] [TIMEOUT]
query OPCODE RESPONSE_OPCODE [PAYLOAD_HEX]
write HEX_BYTES

# Local UI and outputs
menu prev|next|dec|inc
menu page N
relay N on|off|toggle
relay side left|right stop|up|down
relay off
relay test [MS]
pwm get
pwm off
pwm mode off|manual|auto
pwm set CHANNEL VALUE
rgb R G B [BRIGHTNESS]
rgb effect list
rgb effect play|wait NAME
rgb effect stop|status
strip pixel N R G B [BRIGHTNESS]
strip fill R G B [BRIGHTNESS]
strip clear
buzzer FREQUENCY_HZ DURATION_MS
melody list
melody play|wait NAME [REPEATS]
melody stop|status
silent status|on|off
display segments|lcd|both DURATION_MS [TEXT]

# Host workflows and RF
macro list|play NAME_OR_ID|status|cancel
automation list|run NAME
os status
os policy
os brightness get|set VALUE
os brightness-policy enable|disable|range MIN MAX
os power ACTION CONFIRMATION
os power-policy enable|disable|allow|deny [ACTION]
rf send CODE BITS PROTOCOL [PULSE_US] [REPEATS]
rf learn [SECONDS]
rf cancel
rf list
rf remove ID|all
rf map ...
i2c scan
reset lines|app|bootloader
```

`stream 0` disables periodic telemetry; any nonzero period must be at least
100 ms. `reset app` and `reset bootloader` currently request the same
firmware watchdog reset. Use `reset lines` or Urclock programming when
reliable bootloader entry is required.

The PC-owned `SYS` submenu mirrors the same guarded OS path: `BRIT` controls the
primary monitor from 0 through 100, followed by Lock, Suspend, Hibernate,
Restart, and Shut down. Power and brightness writes are policy-disabled by
default. Enabling power still permits only its allow-listed actions; Restart
and Shut down require an explicit `os power-policy allow ...`. DDC/CI support
depends on the physical monitor, dock/adapter, and display driver, so
`os brightness get` may correctly report that the active display is
unsupported.

## Physical keys and key identification

The first four active-low shift-register inputs are:

| Physical key | Root action | Typical editor action |
|---:|---|---|
| 1 | Previous page | previous field or discard |
| 2 | Next page | next field or save |
| 3 | Decrease | decrease or Off |
| 4 | Increase / Enter | increase, On, enter, or save |

A press acts immediately. Hold starts after 600 ms, repeats every 150 ms, and
accelerates to 60 ms after 1.8 seconds where repetition is meaningful.
Double-clicking Key 1 requests the configured default page.

To identify the wiring:

1. Run `menu page 9` from the host, or press Key 2 until `KEY` appears.
2. Press one physical key at a time.
3. The TM1637 should show `1`, `2`, `3`, or `4`.
4. Watch the TUI event log for Down, Up, and Click with source `physical`.
5. Hold Key 1 or Key 2 to leave the KEY page.

Buttons 1 and 2 have passed this event path. Buttons 3 and 4, double-click,
hold, repeat acceleration, and hold-release still require a controlled test.

## Root menu

Key 1/Key 2 wrap around this flat page list:

| Page | Display | Function | Root action |
|---:|---|---|---|
| 0 | `VOLT` | INA219 supply voltage | read-only |
| 1 | `CURR` | INA219 current in mA | read-only |
| 2 | `tLEd` | enclosure-light sensor, `tLED` | read-only |
| 3 | `t-bt` | Bluetooth-module sensor, `tBT` | read-only |
| 4 | `LItE` | enclosure light mode/levels | Key 4 enters editor |
| 5 | `bt` | BT LED Off/On/Blink | read-only |
| 6 | `Snd` | sound and V/A decimal precision | Key 4 enters settings submenu |
| 7 | `PWM` | 16-channel test | Key 4 enters editor |
| 8 | `rELY` | relay identification | Key 3 Auto; Key 4 manual |
| 9 | `KEY` | key identification | pressed key shows 1-4 |
| 10 | `uPWM` | saved PWM 0-7 levels | Key 4 enters editor |
| 11 | `r5-8` | general relay behavior/control | Key 4 enters |
| 12 | `MOVE` | two-side motion | Key 4 enters if door is open |
| 13 | `LErn` | 433 MHz learned records | Key 3 clears all; Key 4 learns |

The host can navigate the same state machine:

```text
menu prev
menu next
menu dec
menu inc
menu page 0
menu page 9
```

### Illumination editor

From `LItE`, press Key 4:

1. `L-Md`: Off, Auto, or On.
2. `L-on`: door-open/on brightness, 0-255 in steps of 16.
3. `L-oF`: off brightness, 0-255 in steps of 16.
4. `SAVE`: Key 2/4 saves; Key 1/3 discards.

Auto follows the door reed and fades PWM channel 11 between the saved levels.

### Sound editor

From `Snd`, press Key 4. This is the current local settings submenu:

1. `Snd`: Key 3 selects Mute and Key 4 selects Sound;
2. `diSP`: Key 3/4 changes TM1637 brightness through 0-7 with wrap-around;
3. `StBr`: Key 3/4 changes status-RGB brightness through 0-255 in steps of
   16 with wrap-around;
4. `CoLr`: Key 3/4 selects the Ready-state palette index 0-7;
5. `V-dP`: Key 3/4 decreases/increases voltage precision through 0, 1, and
   2 decimal places with wrap-around;
6. `A-dP`: Key 3/4 likewise selects current precision;
7. `SAVE`: save or discard the whole transaction.

Key 1 moves to the previous field and Key 2 moves to the next field. After
each 650 ms field label, the selected value blinks on/off at approximately
300 ms intervals. Muting and brightness/color previews take effect while
editing; Discard restores the snapshot. The earlier AFD validation exercised
all three precision values locally and from the host, saved two decimals for
both readings, reset the board, and confirmed the settings persisted. The
expanded six-item menu is installed in current 5DF; its new physical-key
display/status fields still need a hands-on pass.

Host equivalents:

```text
silent status
silent on
silent off
buzzer 1200 80
settings decimals 2 2
```

### PWM test editor

From `PWM`, press Key 4:

1. choose Off, Manual, or Auto;
2. in Manual, choose channel 0-15;
3. choose logical value 0-4095 in steps of 256;
4. save or discard the boot mode.

Auto identifies only user channels 0-10. Host equivalents:

```text
pwm get
pwm mode off
pwm mode manual
pwm mode auto
pwm set 0 1024
```

`pwm mode off` stops the Off/Manual/Auto commissioning owner. Reserve
`pwm off` for an intentional emergency clear of all 16 channels; it also
clears enclosure channel 11, power channel 12, and status RGB channels 13-15.

PWM channel ownership is:

| Channels | Owner |
|---:|---|
| 0-7 | persistent user MOSFET/light outputs |
| 8-10 | additional user outputs |
| 11 | enclosure illumination |
| 12 | Power/On indicator |
| 13-15 | status RGB |

### Relay identification and R5-R8

At `rELY`, Key 3 toggles the automatic R1-R8 identification sequence. Key 4
opens manual relay selection and Off/On control.

At `r5-8`, choose R5-R8, then Toggle or Push. In control, Key 3 turns it off;
Key 4 toggles it or keeps it on only while held, depending on the selected
behavior.

Host equivalents:

```text
relay off
relay test 250
relay test 0
relay 5 on
relay 5 off
```

### Motion mode

`MOVE` is allowed only when the door reports open:

| Key | Motion |
|---:|---|
| 1 | Side A Up/forward |
| 2 | Side A Down/reverse |
| 3 | Side B Up/forward |
| 4 | Side B Down/reverse |

Releasing a key stops that side. Hold Keys 1+2 or Keys 3+4 together for
600 ms to stop all relays and exit. Closing the door also stops both sides
and exits immediately.

Prefer:

```text
relay side left up
relay side left down
relay side left stop
relay side right up
relay side right down
relay side right stop
```

These host commands are not currently reed-gated by firmware. Check `status`
for `door=true` before starting, stop immediately on the asynchronous
door-close event, and always send the matching `stop`; the automatic
door-close stop applies to local `MOVE` mode. Forward/Up naming still needs
confirmation against the actual motor wiring.

### Save and discard

Editors snapshot their original values. At the save prompt:

- Key 2 or 4 commits to CRC-checked MCU EEPROM.
- Key 1 or 3 restores the snapshot.
- `SAVE` or `diSC` flashes for about 900 ms.

The audible save/discard cues are implemented, but the restored-audio
acoustic test is still pending.

## Sensors and live status

Useful commands:

```text
status
voltage
current
temp list
temp scan
i2c scan
stream 500
stream 0
```

`temp list` reports the role, full 64-bit ROM, and value for both probes.
`tLED` belongs to enclosure illumination and `tBT` to the BT module. The
current harness assigns the higher sorted ROM to tLED and the lower ROM to
tBT; MCU settings can swap roles after sensor replacement.

Current live identities:

```text
tBT   28-616435FB503F-E9
tLED  28-70F275000000-8A
```

The two DS18B20s share D10/CS and require an external pull-up, normally
4.7 kOhm to the sensor logic supply. Missing/stuck-low handling is bounded and
should report unavailable rather than lock the MCU.

INA219 uses address `0x40`; the PWM expander uses `0x41`. The supply reading is
bus plus shunt voltage. The current 5DF firmware uses the INA219's 64-sample
bus/shunt averaging mode, which takes about 68 ms and fits the 100 ms
door-open sampling period. A 50-sample AFD check measured only 4 mV supply
span and 3 mA current span. Temperature values use a 50/50 integer EMA, while
a raw value at or above 50 C bypasses smoothing for immediate warning.

## Displays, RGB, and addressable LEDs

Send temporary text:

```text
display segments 2000 DEMO
display lcd 3000 Enclosure open
display both 2000 rEAd
```

TM1637 uses D11/MOSI for data and D13/SCK for clock. It is not I2C. The
optional 16x2 LCD is probed at `0x27` and `0x3F`; no LCD was detected in the
latest live scan.

Status RGB uses PWM channels 13-15:

```text
rgb 255 0 0 128
rgb 0 255 0 128
rgb effect list
rgb effect play attention
rgb effect play breathe-blue
rgb effect stop
```

Named `flash` and `breathe` effects come from the watched host JSON. Updates
are capped at 20 acknowledged native requests per second. Use `play` from a
persistent TUI/shell/IPC session; use `wait` for a finite one-shot `exec`.
The host must remain connected. Stopping cancels future frames and leaves the
effect's base color at its configured full brightness.

Host-configured melodies use the same pattern:

```text
melody list
melody play notify
melody wait attention 2
melody stop
```

The scheduler sends one acknowledged buzzer note at a time, avoiding the MCU
queue limit. Stopping prevents subsequent notes, but a note already accepted
by the current firmware must finish. MCU Silent mode still wins, so run
`silent off` when audible playback is intended.

The separate 11-pixel D6 strip starts black:

```text
strip pixel 0 255 80 0 128
strip fill 0 0 255 96
strip clear
```

RGB animations and strip commands are implemented but still need visual
validation on the connected hardware.

## 433 MHz receive, learn, map, and transmit

Receive is D2/INT0 and transmit is D3/INT1. The firmware stores eight learned
button records.

Start with a harmless menu mapping:

```text
rf learn 15
rf list
rf map 0 menu next
```

Other mappings:

```text
rf map 0 none
rf map 0 key 1 press
rf map 0 relay 5 toggle
rf map 0 relay 5 momentary
rf map 0 side left up
rf map 0 pwm 0 momentary
rf remove 0
rf remove all
```

Transmit:

```text
rf send 1193046 24 1 350 5
```

Every received frame can report exact code, bit count, rc-switch protocol,
pulse width, and learned ID. The host infers Down, click, double-click, Hold,
accelerated Repeat, and timed Up from repeated RF frames and exposes those
gestures to automation. Release/double-click are inferred from packet gaps
because typical 433 MHz codes contain no physical release bit.

Use **learn**, not pair, in commands and documentation. End-to-end handset
learning/mapping/hold/removal and INT1 transmit confirmation are still
pending.

## Host macros

Macros live in host JSON, not MCU EEPROM. Each has an ID, name, four-character
segment label, optional LCD message, and timed relay/PWM steps. Start from
`Tools/Controller/examples/config.example.json`.

Example:

```json
{
  "id": 1,
  "name": "output-demo",
  "label": "dEMO",
  "lcd_message": "Output demo running",
  "steps": [
    {"at_ms": 0, "kind": "relay", "target": 4, "value": 1},
    {"at_ms": 250, "kind": "pwm", "target": 0, "value": 1024},
    {"at_ms": 750, "kind": "pwm", "target": 0, "value": 0},
    {"at_ms": 1000, "kind": "relay", "target": 4, "value": 0}
  ]
}
```

Macro relay targets are zero-based protocol indices: target `4` is R5.
Copy or merge the example into the active file reported by `config path`, run
`config validate`, and allow the long-running host to hot-reload it before
using the example name:

```text
macro list
macro play output-demo
macro status
macro cancel
```

The host streams steps; the board displays the macro label and tracks output
ownership. Cancel switches off the outputs claimed by that macro. Normal
completion may preserve final states, so include explicit off steps when that
is the desired result.

Macro playback/cancellation on real loads remains a pending harmless-output
test.

## Persistent configuration

There are two intentionally separate configuration owners.

### MCU EEPROM

The board owns sound and door/relay audio cues, tLED/tBT swap, motion-door
policy and motion-break selection, illumination mode/levels, display/status
brightness, persistent Ready-state color, voltage/current decimal precision,
PWM boot mode, saved PWM 0-7, stream period, default/save-last menu page,
cap23 menu visibility/order, learned RF records, and the reset-count journal.
LCD rendering is PC-owned; EEPROM flags bit 1 is reserved.

Read it with:

```text
settings
```

Local editors update it after Save. Host settings updates first read the
current scalar record and preserve fields not named by the chosen command.
They do not replace saved user-PWM levels, learned RF records, or the reset
journal. Use `pwm set`/the `uPWM` editor and `rf ...` commands for those
separate stores.

The complete host form is:

```text
settings set FLAGS LIGHT ON OFF DISPLAY STATUS PWMBOOT STREAM
  [DEFAULT_PAGE SAVE_LAST [STATUS_COLOR VOLTAGE_DECIMALS CURRENT_DECIMALS]]
settings decimals VOLTAGE_DECIMALS CURRENT_DECIMALS
settings color STATUS_COLOR
```

| Field | Meaning |
|---|---|
| `FLAGS` | bit 0 Silent; bit 1 reserved; bit 2 swap tLED/tBT; bits 3-4 motion-door policy; bit 5 door audio disabled; bit 6 relay audio disabled; bit 7 selects the extended 100 ms motion break instead of 1 ms. Values may be decimal or `0x` hexadecimal |
| `LIGHT` | 0 Off, 1 Auto/reed, 2 On |
| `ON`, `OFF` | enclosure-light levels, 0-255 |
| `DISPLAY` | TM1637 brightness, 0-7 |
| `STATUS` | status-RGB brightness, 0-255 |
| `PWMBOOT` | 0 Off, 1 Manual, 2 Auto |
| `STREAM` | 0 disables streaming; otherwise at least 100 ms |
| `DEFAULT_PAGE` | persistent board page 0-14 |
| `SAVE_LAST` | `false`/`0` or `true`/`1` |
| `STATUS_COLOR` | persistent Ready color 0-7: green, blue, cyan, amber, magenta, red, white, violet |
| `VOLTAGE_DECIMALS`, `CURRENT_DECIMALS` | displayed precision, 0-2 decimal places |

The shorter forms preserve every omitted newer field. Use `silent on|off` when
sound is the only setting you intend to change, `settings decimals ...` for
precision alone, and `settings color ...` for the Ready color alone. The
extended settings byte uses bit 0 for save-last-page, bits 1-3 for Ready color,
bits 4-5 for voltage precision, and bits 6-7 for current precision. An encoded
precision value of zero is the backward-compatible two-decimal default.

Display/status brightness, Ready color, sound, and voltage/current precision
are host-configurable and locally editable through the six-item `Snd`
submenu.

### Host JSON

The host owns connection filters/timeouts, UI limits, paths, programming-tool
selection, named scripts, macros, automations, and the last authenticated
device identity.

Default Windows path:

```text
%AppData%\PCController\config.json
```

Commands:

```powershell
.\Tools\Controller\bin\controller.exe config path
.\Tools\Controller\bin\controller.exe config show
.\Tools\Controller\bin\controller.exe --config .\lab.json config validate
```

Override the path with `--config FILE` or `PCCONTROLLER_CONFIG`.

Long-running TUI, shell, monitor, and IPC sessions watch the configuration
directory with `fsnotify`. Valid replacements apply after debounce; invalid
replacements leave the last-known-good configuration active. A five-second
poll covers file systems that miss replacement events.

`connection.last_device` is maintained after a successful HELLO. This CH340
does not expose a stable USB serial number; its remembered Windows PnP instance
can change with USB topology. The host therefore falls back to VID/PID/name
filtering and the interactive chooser rather than trusting a stale COM number.

`connection.reset_on_reconnect` defaults false. When enabled, it pulses DTR
once for a genuinely reappeared physical serial device, not for every HELLO
retry. It never applies to TCP virtual-board endpoints.

## Automation

Host automations can match device lifecycle, door/BT changes, messages,
physical/RF/host key source and gesture, learned RF ID, raw RF code, or RF
protocol. Actions can:

- run a board command;
- transmit RF;
- launch a bounded host command or named script;
- run/cancel a macro;
- emit a host event.

The default host configuration contains no named automations. After defining a
rule such as `door-open-notify` in the active JSON file and validating/reloading
it, inspect and invoke:

```text
automation list
automation run door-open-notify
```

Rules are hot-reloaded from host JSON, have cooldowns and bounded output, and
cannot recursively match their own result events. Test every output-producing
rule with harmless hardware first.

## Batch scripts and monitoring

Create `commissioning.pc`:

```text
# Read-only commissioning
hello
status
settings
temp list
i2c scan
repeat 4 status
```

Run it:

```powershell
.\Tools\Controller\bin\controller.exe batch --port COM18 --file .\commissioning.pc
Get-Content .\commissioning.pc |
  .\Tools\Controller\bin\controller.exe batch --port COM18 --file -
```

Script directives include `set`, `unset`, `sleep`, and `repeat`; `${NAME}`
expands script variables.

For telemetry logging:

```powershell
.\Tools\Controller\bin\controller.exe monitor `
  --port COM18 --json > .\telemetry.jsonl
```

## JSON-RPC IPC, Go API, and DLL

Start a loopback-only JSON-RPC server:

```powershell
.\Tools\Controller\bin\controller.exe ipc serve --port COM18
```

From another terminal:

```powershell
.\Tools\Controller\bin\controller.exe ipc call `
  --method controller.snapshot
.\Tools\Controller\bin\controller.exe ipc call `
  --method controller.execute `
  --params '{"command":"rf list"}'
```

Methods include `controller.ping`, `connect`, `close`, `snapshot`, `status`,
`temperatures`, `execute`, and `rf.list`. `ipc serve --stdio` provides
newline-delimited JSON-RPC over pipes. TCP IPC rejects non-loopback listeners.

The executable routes secondary CLI/console clients through the primary
JSON-RPC owner automatically. The C ABI is different: each handle owns its
serial transport and does not attach to an existing primary process. A native
consumer that must share the already-open board should use JSON-RPC instead.

Go clients import the module-root package. Typed public calls cover individual
relays/toggle, side motion, PWM, RGB, tones/melodies/effects, RF
send/learn/list/map, and generic command execution. Other languages can load:

```text
Tools\Controller\bin\pccontroller.dll
Tools\Controller\bin\pccontroller.h
```

The C ABI exports:

```c
char *PCControllerInvoke(char *request_json);
void PCControllerFree(char *response_json);
```

Every returned UTF-8 JSON string must be released exactly once with
`PCControllerFree`. See
[C Library API](../Tools/Controller/docs/C-Library-API.md) for handles and operations.

## Native protocol summary

The application UART is 115200 8N1. Each decoded frame is:

```text
magic A5 | version 01 | opcode | sequence | payload_length | payload | CRC8
```

The decoded bytes are COBS-encoded and terminated by `00`. Payloads are at
most 48 bytes. CRC-8/ATM uses polynomial `0x07` and initial value `0x00`.
Multi-byte values are little-endian.

Host requests use sequence 1-255. Their response echoes the sequence.
Unsolicited boot HELLO, streamed STATUS, and EVENT frames use sequence 0.

Request opcodes:

| Range | Operations |
|---|---|
| `01-06` | HELLO, status/stream, settings, temperature list |
| `10-16` | buzzer, PWM, RGB, addressable strip |
| `20-26` | RF transmit/learn/list/remove/map |
| `30-37` | menu, relay, reset, I2C scan, direct page |
| `38-3B` | display text and host-streamed macro control |

Responses are ACK `80`, HELLO `81`, ERROR `82`, STATUS `90`, SETTINGS `91`,
PWM values `92`, I2C result `93`, RF entries `94`, temperatures `95`, and
EVENT `A0`.

Current event types:

| Type | Event |
|---:|---|
| 1 | key: key, gesture, source, source ID |
| 2 | door open/closed |
| 3 | Bluetooth Off/On/Blink |
| 4 | PWM Auto channel |
| 5 | RF learned |
| 6 | macro started/step/cancelled/completed |
| 7 | boot/reset cause and persistent count |
| 8 | raw RF receive fields and learned ID |

STATUS is 48 bytes. It includes uptime, electrical measurements,
temperatures, flags, raw inputs, keys, relays, page/mode, door/BT, PWM, LCD,
error counters, reset cause, and reset count.

The protocol exposes the reset-cause byte, but live Urboot boots currently
report `0x00`; the bootloader path appears to clear the original MCUSR flags
before the application captures them. The persistent reset count is working.
Do not yet use cause `0x00` as proof of a particular electrical reset source.

For exact opcode values and payload schemas, use the
[Protocol and Network API](../Tools/Controller/docs/Protocol-and-Network-API.md).

## Build and programming

Build everything without touching hardware. The CMD and Bash launchers both
delegate to the same project-owned Node plan and never invoke PowerShell:

```bat
build.cmd --dry-run
build.cmd --host-only
build.cmd --firmware-only
build.cmd --upload --port COM18
```

```bash
./build.sh --firmware-only
./build.sh --upload --port COM18
```

The canonical outputs are `Tools/Controller/bin` and `.build/firmware`.
`build.cmd --arduino-update` explicitly routes index/core/library maintenance
through `controller arduino update`; updates never occur during an ordinary
build. The former PowerShell build copies were removed so only the shared
CMD/Bash/Node/Controller path remains.

Routine upload uses Urboot/Urclock:

```console
Tools\Controller\bin\controller.exe program --method urclock --device COM18 --operation write-flash --hex .build\firmware\PCController.ino.hex
```

Hardware-free compile uses the Controller interface:

```console
Tools\Controller\bin\controller.exe program --method compile --sketch . --output-dir .build\firmware
```

Direct Arduino upload is disabled; Controller owns backup, programming,
verification, and application reauthentication.

USBasp recovery/provisioning:

```console
Tools\Controller\bin\controller.exe program --method usbasp --operation write-flash --programmer usbasp --usbasp-troubleshooting --hex .build\firmware\PCController.ino.with_bootloader.hex
```

Programming closes the application session first, runs the external tool,
then requires a fresh authenticated application HELLO. Close any other COM18
owner before starting.

The running application protocol is the project's native COBS/CRC opcode
protocol. Bootloader operations are different: the host deliberately delegates
current MiniCore Urboot/Urclock wire handling to maintained AVRDUDE or
`arduino-cli`; it does not claim to reimplement that boot protocol.

The current 5DF firmware and the proven E2 rollback use this profile:

```text
MiniCore:avr:328:bootloader=uart0,eeprom=keep,baudrate=115200,variant=modelP,BOD=2v7,LTO=Os_flto,clock=16MHz_external
```

### Standalone firmware studio

The dependency-free Node tool under `Tools/Firmware` adds validated
build/upload/watch/manifest/backup workflows:

```console
firmware.cmd build
firmware.cmd check
firmware.cmd manifest
firmware.cmd upload --port COM18 --dry-run
firmware.cmd watch
firmware.cmd watch --upload --method urclock --port COM18
firmware.cmd probe --port COM18
firmware.cmd metadata --port COM18
firmware.cmd backup --port COM18 --output .\backups\flash.hex
firmware.cmd verify --port COM18
```

`build` is the safe default. Watch mode hashes contents every 250 ms, waits
for a 500 ms stable window, skips byte-identical changes, serializes builds,
and coalesces edits made during a build. It never uploads unless `--upload` is
explicit. UART actions require an explicit port. Intel HEX records,
checksums, address ranges, application/bootloader boundaries, artifact
SHA-256 values, and atomic manifests are validated before Controller starts
Urclock or USBasp. The USBasp path additionally requires
`--usbasp-troubleshooting`, a complete merged application-plus-Urboot image,
and preserved EEPROM; generated `.eep` data is never written implicitly.
Direct Arduino upload is disabled.

### Complete controller backup

Read application flash, EEPROM, and Urboot/Urclock metadata into a new
timestamped directory:

```console
Tools\Controller\bin\controller.exe boot backup .\backups --device "USB-SERIAL CH340"
```

From the persistent TUI/shell or IPC command engine:

```text
boot backup .\backups
program backup urclock .\backups COM18
```

The generic programmer form is:

```console
Tools\Controller\bin\controller.exe program --operation backup --method urclock --output .\backups --device 1A86:7523
```

Each run creates `pccontroller-YYYYMMDD-HHMMSS` containing `flash.hex`,
`eeprom.hex`, raw `programmer.txt`, and an atomically written `manifest.json`.
The manifest records completion status, method/port/MCU/programmer, sizes,
SHA-256 hashes, and application build hash/date/time when the native app was
reachable before bootloader handoff. Any partial read remains explicitly
`incomplete`. This workflow passed code/unit tests but was deliberately not
executed against COM18/AVRDUDE during the host-only follow-up.

The requested production workflow is stricter than this current explicit
backup command: every ordinary flash write must first create a successful
flash+EEPROM backup in the host data directory, normally through Urclock, and
block the write on backup failure unless the operator gives a logged override.
USBasp is an advanced fallback, not the normal path. Automatic pre-flash
backup, live-settings export versus offline EEPROM parsing, named-region HEX
inspection/patching with before/after hashes, and graceful-exit diagnostic
snapshots remain 🟡 acceptance work; do not assume they happen implicitly yet.

MCU EEPROM and PC configuration remain separate throughout. A parsed/exported
EEPROM file is a backup or proposed board image, never a replacement host
config. The future migration workflow is backup → identify layout/hash →
preserve unknown bytes → propose changes → write deliberately → read back and
verify.

See [firmware studio guide](../Tools/Firmware/README.md).

## WebSocket firmware relay

On the build workstation:

```console
Tools\Controller\bin\controller.exe ws serve --file .build\firmware\PCController.ino.hex --listen 127.0.0.1:3000
```

On the programming workstation:

```console
Tools\Controller\bin\controller.exe ws client --url ws://BUILD-PC:3000/firmware --method urclock --port COM18
```

Messages include filename, modification time, SHA-256, and base64 bytes. The
client validates size/name/hash, uses a temporary file, invokes the selected
programmer, then cleans up. The relay is standard WebSocket, not compatible
with the old Socket.IO framing. Non-loopback use has no TLS/authentication and
must be limited to a trusted network.

## Virtual board

Build and run:

```console
cd Tools\VirtualBoard
bash.exe build.sh
.build\bin\virtual_board.exe
```

Connect with the same host:

```console
..\Controller\bin\controller.exe exec --port tcp://127.0.0.1:8765 hello
..\Controller\bin\controller.exe tui --port tcp://127.0.0.1:8765
```

The simulator models observable protocol behavior, a separate virtual MCU
EEPROM, sensors, relays, PWM, displays, RF, macros, and reset telemetry. It is
not the same AVR translation unit and does not emulate electrical faults or
instruction timing.

## Troubleshooting

### `PCController HELLO: context deadline exceeded`

1. Confirm no other process owns COM18.
2. Use the current packaged host, not an old binary from the removed `host/`
   tree.
3. Run `ports`, then explicit `exec --port COM18 hello`.
4. Allow the host's startup wait/retries; Urboot and board initialization can
   make the first non-HELLO request return Busy.
5. Verify UART D0/D1 and common ground. A successful Urclock upload proves both
   UART directions.

The packaged host authenticated the earlier A7 image, the E2 rollback image
passed later host-driven status/menu checks, and current 5DF passed the
HELLO/STATUS/menu/settings validations without the former deadline error.

### Port busy

Current host clients should attach to the primary owner automatically. If the
port is still busy, close Arduino Serial Monitor, old host versions, and any
non-PCController serial/programming process. Use
`controller.exe ipc call --method controller.ping` to check whether a primary
host is already listening.

### Reset count increments

A new serial open may pulse reset on this adapter. One increment at open is
expected; repeated increments during one steady session are not. Watch
`reset_count`, `framing`, and `crc` in STATUS.

`reset_cause=0x00` is presently inconclusive on physical Urboot boots.

### Voltage/current is wrong or jumpy

- Expect INA219 at `0x40` and shared ground.
- Supply voltage is bus plus shunt; compare VIN+ to ground with a meter.
- Confirm shunt orientation and wiring.
- Current 5DF uses the same averaging path; the immediately preceding AFD
  live run produced a 4 mV supply span in a 50-sample
  live test.

### Temperature missing or labels reversed

- Fit the required 4.7 kOhm D10 pull-up.
- Run `temp scan` then `temp list`.
- Compare full ROM IDs, not discovery order alone.
- Use the MCU swap flag only after physically identifying the probes.

### PWM is all on, absent, or flickery

- `i2c scan` must show PWM at `0x41`, not `0x40`.
- Start with `pwm mode off`; use `pwm off` only when an all-16-channel
  emergency clear is intended.
- Verify active-high MOSFET polarity and common ground.
- Use Manual one channel at a time before Auto.

### LCD is unavailable

The latest board scan found no LCD. Connect a common PCF8574 backpack at
`0x27` or `0x3F`, enable LCD in MCU settings, then rescan. A different backpack
bit mapping needs a driver change.

### Buzzer is silent

Run `silent status`, then `silent off`. The buzzer is D9/Timer1 OC1A; Servo or
other Timer1 ownership conflicts are unsupported.

### Configuration changes do not apply

Run `config validate`. Invalid replacements deliberately preserve the
last-known-good configuration. Confirm the edited file is the one printed by
`config path`.

### Upload fails

- Close every COM18 client.
- Use the application HEX for Urclock, not the merged bootloader HEX.
- Use the merged image only for intentional USBasp provisioning/recovery.
- Do not hold UART and USBasp programming active together.

## Recommended next physical checks

1. Keep `E2DCE296` available as the proven rollback image while validating
   current `5DF10D05`.
2. Physically test all four buttons on 5DF, including double-click, hold,
   accelerated repeat, release, and all six nested `Snd` settings.
3. Listen for the next boot melody and one short beep per key. The latest
   unattended 5DF sweep deliberately ran muted.
4. Exercise every editor with Save and Discard while outputs are harmless.
5. Decide whether the flat root plus six-item settings submenu is sufficient;
   a broader root category hierarchy is the remaining unimplemented menu
   structure. The local brightness/color controls and value blink now exist.
6. Toggle the door and verify immediate host event, channel-11 fade, default
   page return, RGB cue, and MOVE emergency stop.
7. Toggle the BT module and verify Off/On/Blink events.
8. Heat only the illumination area and confirm tLED changes more than tBT.
9. Identify PWM/relay wiring one output at a time.
10. Learn/map/remove a harmless RF button, test hold/repeat/up, then verify TX.
11. Connect and validate an LCD if it is required.
12. Run/cancel a harmless macro and test USB unplug/replug reconnect.
