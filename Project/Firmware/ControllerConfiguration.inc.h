// Implementation fragment compiled once; owns stable IDs, timing, and flash text.
// -----------------------------------------------------------------------------
// Firmware configuration
// -----------------------------------------------------------------------------

#ifndef PCCONTROLLER_BUILD_HASH
#define PCCONTROLLER_BUILD_HASH 0UL
#endif

#ifndef PCCONTROLLER_BUILD_TIMESTAMP
#define PCCONTROLLER_BUILD_TIMESTAMP 0UL
#endif

// Cooperative service periods are milliseconds; all comparisons are rollover-safe.
constexpr uint16_t SHIFT_POLL_MS = 5;
constexpr uint16_t DISPLAY_REFRESH_MS = 20;
constexpr uint16_t INA219_SAMPLE_MS = 500;
constexpr uint16_t INA219_DOOR_OPEN_SAMPLE_MS = 100;
constexpr uint16_t TEMPERATURE_PERIOD_MS = 1000;
constexpr uint16_t TEMPERATURE_DOOR_OPEN_PERIOD_MS = 450;
constexpr uint16_t TEMPERATURE_CONVERSION_MS = 375;
constexpr uint16_t HOST_OFFLINE_MS = 5000;

// RF learning limits/options and front-panel adjustment steps.
constexpr uint8_t DEFAULT_LEARNING_SECONDS = 15;
constexpr uint8_t MAX_LEARNING_SECONDS = 120;
constexpr uint8_t MAX_RC_PROTOCOL = 12;
constexpr uint8_t LEARN_MULTI = 1U << 0;
constexpr uint8_t LEARN_INDEFINITE = 1U << 1;
constexpr uint16_t PWM_MENU_STEP = 256;
constexpr uint8_t ILLUMINATION_MENU_STEP = 16;
constexpr int16_t HOT_TEMPERATURE_CENTI_C = 5000;

// Invalid sensor sentinels survive packed native telemetry without floating point.
constexpr int32_t INVALID_I32 = (-2147483647L - 1L);
constexpr int16_t INVALID_I16 = (-32767 - 1);

// Physical, RF, and host keys share these stable four action IDs.
enum MenuAction : uint8_t {
  MENU_PREVIOUS = 0,
  MENU_NEXT = 1,
  MENU_DECREASE = 2,
  MENU_INCREASE = 3
};

// Stable EEPROM/protocol page IDs; presentation order is stored separately.
enum MenuPage : uint8_t {
  PAGE_STATUS = 0,
  PAGE_VOLTAGE,
  PAGE_CURRENT,
  PAGE_TLED,
  PAGE_TBT,
  PAGE_ILLUMINATION,
  PAGE_BLUETOOTH,
  PAGE_SOUND,
  PAGE_PWM,
  PAGE_RELAY,
  PAGE_KEYS,
  PAGE_USER_PWM,
  PAGE_USER_RELAYS,
  PAGE_MOTION,
  PAGE_RF,
  PAGE_COUNT
};
static_assert(PAGE_COUNT == PersistentMenuPageCount,
              "Persistent menu catalog no longer matches stable page IDs");

// Top-level pages and modal editors consumed by ModeManager.
enum ProgramMode : uint8_t {
  MODE_BOOT = 0,
  MODE_STATUS,
  MODE_VOLTAGE,
  MODE_CURRENT,
  MODE_TLED,
  MODE_TBT,
  MODE_ILLUMINATION,
  MODE_BLUETOOTH,
  MODE_SOUND,
  MODE_PWM,
  MODE_RELAY,
  MODE_KEYS,
  MODE_USER_PWM,
  MODE_USER_RELAYS,
  MODE_MOTION,
  MODE_RF,
  MODE_ILLUMINATION_MODE_EDIT,
  MODE_ILLUMINATION_ON_EDIT,
  MODE_ILLUMINATION_OFF_EDIT,
  MODE_SOUND_EDIT,
  MODE_PWM_MODE_EDIT,
  MODE_PWM_CHANNEL_EDIT,
  MODE_PWM_VALUE_EDIT,
  MODE_RELAY_CHANNEL_EDIT,
  MODE_RELAY_VALUE_EDIT,
  MODE_USER_PWM_CHANNEL_EDIT,
  MODE_USER_PWM_VALUE_EDIT,
  MODE_USER_RELAY_CHANNEL_EDIT,
  MODE_USER_RELAY_BEHAVIOR_EDIT,
  MODE_USER_RELAY_CONTROL,
  MODE_MOTION_CONTROL,
  MODE_SAVE_PROMPT,
  MODE_FLASH_MESSAGE,
  MODE_RF_LEARNING,
  MODE_FAULT,
  MODE_UNDEFINED = 0xFF
};

// Four-character labels are packed contiguously to avoid pointer tables in SRAM.
const char MenuLabels[] PROGMEM =
    "STATVOLTCURRtLEDt-btLItEbt  Snd PWM rELYKEY uPWMr5-8MOVELErn";
const char EditLabels[] PROGMEM =
    "L-MdL-onL-oFS-MdP-MdP-ChP-u r-Chr-onuP-CuP-uur-Cur-M";
constexpr uint8_t EditLabelCount = 13;
const char SettingsLabels[] PROGMEM = "Snd diSPStBrCoLrV-dPA-dP";
constexpr uint8_t SettingsItemCount = 6;
const char CommonTexts[] PROGMEM =
    "oFF  on AutoSAVEOPENMuteSnd LErnBOOTdiSC"
    "r5-8Go  Err Man boFFb-onbLnkKEY CLSdtoGLPuSH";
enum CommonTextOffset : uint8_t {
  TextOff = 0,
  TextOn = 4,
  TextAuto = 8,
  TextSave = 12,
  TextOpen = 16,
  TextMute = 20,
  TextSound = 24,
  TextLearn = 28,
  TextBoot = 32,
  TextDiscard = 36,
  TextUserRelays = 40,
  TextGo = 44,
  TextError = 48,
  TextManual = 52,
  TextBluetoothOff = 56,
  TextBluetoothOn = 60,
  TextBluetoothBlink = 64,
  TextKey = 68,
  TextClosed = 72,
  TextToggle = 76,
  TextPush = 80,
};

// Returns a flash-string view at a known four-byte CommonTexts offset.
const __FlashStringHelper *commonText(uint8_t offset) {
  return reinterpret_cast<const __FlashStringHelper *>(CommonTexts + offset);
}

const uint8_t ModeTextOffsets[] PROGMEM = {
    TextOff, TextAuto, TextOn, TextOff, TextManual, TextAuto};

// Telemetry availability/activity bits shared with the native host decoder.
enum StatusFlag : uint16_t {
  STATUS_INA219 = 1U << 0,
  STATUS_PWM = 1U << 1,
  STATUS_TLED = 1U << 2,
  STATUS_TBT = 1U << 3,
  STATUS_RF_LEARNED = 1U << 4,
  STATUS_RF_LEARNING = 1U << 5,
  STATUS_STREAMING = 1U << 6,
  STATUS_RF_RECEIVED = 1U << 7,
  STATUS_LCD = 1U << 8,
  STATUS_SILENT = 1U << 9,
  STATUS_RELAY_BUSY = 1U << 10,
  STATUS_DOOR_OPEN = 1U << 11,
  STATUS_BUZZER_BUSY = 1U << 12
};
