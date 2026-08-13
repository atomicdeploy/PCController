# Firmware features and profiles

The firmware build has three related, deliberately distinct kinds of truth:

- a **compile gate** selects code in `ProjectConfig.h`;
- a **HELLO build flag** summarizes eight high-value build choices;
- a **runtime capability bit** says whether a connected board implements a
  wire behavior.

The Go compiler writes all three into `firmware-manifest.json`. `build.cmd`,
`build.sh`, `controller firmware features`, and the GitHub Actions summary
render that manifest rather than maintaining separate lists.

## Profiles

| Value | Profile | Inference |
|---:|---|---|
| 0 | `full-peripheral` | INA219, DS18B20, and PCA9685 are compiled. |
| 1 | `motion-macro` | INA219, DS18B20, PCA9685, and the MCU LCD renderer are excluded. |
| 2 | `key-diagnostic` | The unified physical page identifies keys. |
| 3 | `custom` | Any other source-selected combination. |

Profiles are descriptive: the compiler derives them from the resolved gates.
They do not weaken the dependency and incompatibility checks in
`ProjectConfig.h`. Unsupported combinations must fail compilation.

## Compile gates

| Gate | Purpose |
|---|---|
| <a id="i2c-lcd"></a>`PCCONTROLLER_ENABLE_I2C_LCD` | MCU PCF8574/HD44780 renderer; production keeps the LCD host-owned. |
| <a id="ina219"></a>`PCCONTROLLER_ENABLE_INA219` | Supply-voltage/current telemetry. |
| <a id="ds18b20"></a>`PCCONTROLLER_ENABLE_DS18B20` | Two temperature probes and learned identities. |
| <a id="pca9685"></a>`PCCONTROLLER_ENABLE_PCA9685` | Sixteen-channel PWM driver. |
| <a id="status-led-engine"></a>`PCCONTROLLER_ENABLE_STATUS_LED_ENGINE` | Compact MCU-timed RGB renderer and disconnected fallback; requires PCA9685. |
| <a id="status-led-profiles"></a>`PCCONTROLLER_ENABLE_STATUS_LED_PROFILES` | Larger EEPROM condition profiles; rejected on the compact 328P build. |
| <a id="illumination-automation"></a>`PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION` | Door-aware local illumination; requires PCA9685. |
| <a id="local-pca-pages"></a>`PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES` | Board-local PWM editor pages. |
| <a id="local-rf-learning-ui"></a>`PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI` | RF learning from the physical panel. |
| <a id="bt-led-detection"></a>`PCCONTROLLER_ENABLE_BT_LED_DETECTION` | Commissioned active-high BT audio LED input. |
| <a id="async-presentation-events"></a>`PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS` | Unsolicited buzzer and rendered-RGB frames. |
| <a id="async-segment-events"></a>`PCCONTROLLER_ENABLE_ASYNC_SEGMENT_EVENTS` | Changed-only TM1637 frames. |
| <a id="scheduled-segments"></a>`PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS` | Scheduled once/loop/interval segment messages. |
| <a id="local-audio-cues"></a>`PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES` | Autonomous door and motion feedback tones. |
| <a id="local-settings-editor"></a>`PCCONTROLLER_ENABLE_LOCAL_SETTINGS_EDITOR` | Extended local settings editor; conflicts with macro capture on the constrained image. |
| <a id="task-scheduler"></a>`PCCONTROLLER_ENABLE_TASK_SCHEDULER` | Optional cooperative scheduler. |
| <a id="key-diagnostic-page"></a>`PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS` | Key identifier on the unified physical page. |
| <a id="macro-capture"></a>`PCCONTROLLER_ENABLE_MACRO_CAPTURE` | Board-local timed capture, replay, and export. |
| <a id="force-silent"></a>`PCCONTROLLER_FORCE_SILENT` | Prevent local audio regardless of stored settings. |
| <a id="blank-eeprom-silent"></a>`PCCONTROLLER_BLANK_EEPROM_SILENT` | Audio default before valid settings exist. |
| <a id="menu-directory"></a>`PCCONTROLLER_ENABLE_MENU_DIRECTORY` | MCU-paged menu catalog. |
| <a id="menu-layout-storage"></a>`PCCONTROLLER_MENU_LAYOUT_STORAGE` | Extended local layout record. |
| <a id="menu-visibility"></a>`PCCONTROLLER_MENU_VISIBILITY` | Persistent local page visibility. |
| <a id="menu-ordering"></a>`PCCONTROLLER_MENU_ORDERING` | Persistent local page ranks; requires visibility. |
| <a id="menu-hierarchy"></a>`PCCONTROLLER_MENU_HIERARCHY` | Nested local menus; requires ordering. |
| <a id="menu-layout-protocol"></a>`PCCONTROLLER_MENU_LAYOUT_PROTOCOL` | Persistent layout operations; requires ordering. |

## Runtime capabilities

HELLO schema 4 carries a 32-bit capability mask. The linked Actions table lists
every bit by number, name, inclusion state, and owning source. Capabilities are
the correct UI/API gate after connection; compile gates are build provenance.

## Local and CI commands

```text
build.cmd --firmware-only
controller firmware features --manifest .build/firmware/firmware-manifest.json
controller firmware features --project . --format markdown
```

Local builds use the compact Unicode table. CI uses the expanded Markdown table
with links to this page and the exact revision of `ProjectConfig.h` or
`Project/Firmware/ProtocolRuntime.inc.h`.
