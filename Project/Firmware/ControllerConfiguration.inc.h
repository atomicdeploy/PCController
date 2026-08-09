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

#ifndef PCCONTROLLER_IDENTITY_ADDRESS
#define PCCONTROLLER_IDENTITY_ADDRESS 0x7E74UL
#endif

// Final-application record patched only by the guarded host workflow. The Go
// compiler places it in the last 12 bytes below the selected core bootloader.
// "PCI1" identifies schema 1: little-endian source hash and packed timestamp.
constexpr uint16_t FirmwareIdentityAddress =
    static_cast<uint16_t>(PCCONTROLLER_IDENTITY_ADDRESS);
// FirmwareIdentityRecord occupies the guarded fixed-location patch region.
struct __attribute__((packed)) FirmwareIdentityRecord {
  uint32_t magic;
  uint32_t sourceHash;
  uint32_t packedTimestamp;
};
const FirmwareIdentityRecord firmwareIdentity
    __attribute__((section(".firmware_identity"), used)) = {
        0x31494350UL, static_cast<uint32_t>(PCCONTROLLER_BUILD_HASH),
        static_cast<uint32_t>(PCCONTROLLER_BUILD_TIMESTAMP)};
static_assert(sizeof(FirmwareIdentityRecord) == 12,
              "Firmware identity patch record changed shape");

// Cooperative service periods are milliseconds; all comparisons are rollover-safe.
constexpr uint16_t SHIFT_POLL_MS = 5;
constexpr uint16_t DISPLAY_REFRESH_MS = 20;
constexpr uint16_t INA219_SAMPLE_MS = 500;
constexpr uint16_t INA219_DOOR_OPEN_SAMPLE_MS = 100;
constexpr uint16_t TEMPERATURE_PERIOD_MS = 1000;
constexpr uint16_t TEMPERATURE_DOOR_OPEN_PERIOD_MS = 450;
constexpr uint16_t TEMPERATURE_CONVERSION_MS = 375;
constexpr uint16_t HOST_OFFLINE_MS = 5000;

// A physical menu binding is dispatched on the first debounced Down sample.
// Guard the complete scan + debounce path, not merely the debounce constant:
// shortening a later Click timer does not satisfy this response contract.
constexpr uint16_t KEY_PRIMARY_ACTION_BUDGET_MS = 25;
static_assert(SHIFT_POLL_MS + KEY_DEBOUNCE_MS <=
                  KEY_PRIMARY_ACTION_BUDGET_MS,
              "front-key Down dispatch exceeds the physical response budget");

// RF learning has one default indefinite/multi mode and one bounded timer mode.
constexpr uint8_t DEFAULT_LEARNING_SECONDS = 15;
constexpr uint8_t MAX_LEARNING_SECONDS = 120;
constexpr uint8_t MAX_RC_PROTOCOL = 12;
// RfLearningMode distinguishes indefinite multi-code and bounded timer sessions.
enum RfLearningMode : uint8_t {
  RF_LEARN_INDEFINITE = 0,
  RF_LEARN_TIMER = 1,
};
constexpr uint16_t PWM_MENU_STEP = 256;
constexpr uint8_t ILLUMINATION_MENU_STEP = 16;
constexpr int16_t HOT_TEMPERATURE_CENTI_C = 5000;

// Invalid sensor sentinels survive packed native telemetry without floating point.
constexpr int32_t INVALID_I32 = (-2147483647L - 1L);
constexpr int16_t INVALID_I16 = (-32767 - 1);

static_assert(PAGE_COUNT == PersistentMenuPageCount,
              "Persistent menu catalog no longer matches stable page IDs");

constexpr bool BuildForcesSilent = PCCONTROLLER_FORCE_SILENT != 0;

// Four-character labels are packed contiguously to avoid pointer tables in SRAM.
// PAGE_KEYS is retained as an ID but is not rendered as an independent page.
#if PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS
#define PCCONTROLLER_UNIFIED_PAGE_LABEL "KEY "
#else
#define PCCONTROLLER_UNIFIED_PAGE_LABEL "MOVE"
#endif
const char MenuLabels[] PROGMEM =
    "doorVOLTCURRtLEDtBT LItEbEEPPWM rELY    uPWMr5-8"
    PCCONTROLLER_UNIFIED_PAGE_LABEL "LErn";
#undef PCCONTROLLER_UNIFIED_PAGE_LABEL
static_assert(sizeof(MenuLabels) == PAGE_COUNT * 4U + 1U,
              "packed menu labels no longer match stable page IDs");
const char EditLabels[] PROGMEM =
    "L-MdL-onL-oFS-MdP-ChP-u r-Chr-onuP-CuP-uur-Cur-M";
constexpr uint8_t EditLabelCount = 12;
const char SettingsLabels[] PROGMEM = "bEEPdiSPdCLSStBrV-dPA-dPSAFE";
constexpr uint8_t SettingsPolicyItem = 6;
constexpr uint8_t SettingsItemCount = 7;
const char CommonTexts[] PROGMEM =
    "oFF  on AutoSAVEOPENMutebEEPLErnBOOTdiSC"
    "r5-8Go  Err KEY CLSdtoGLPuSHProgrEC PLAYSIDE";
// CommonTextOffset names four-byte cells within the packed CommonTexts table.
enum CommonTextOffset : uint8_t {
  TextOff = 0,
  TextOn = 4,
  TextAuto = 8,
  TextSave = 12,
  TextOpen = 16,
  TextMute = 20,
  TextBeep = 24,
  TextLearn = 28,
  TextBoot = 32,
  TextDiscard = 36,
  TextUserRelays = 40,
  TextGo = 44,
  TextError = 48,
  TextKey = 52,
  TextClosed = 56,
  TextToggle = 60,
  TextPush = 64,
  TextProgram = 68,
  TextRecord = 72,
  TextPlay = 76,
  TextSide = 80,
};

// Returns a flash-string view at a known four-byte CommonTexts offset. These
// packed cells are fixed-width and are not individually NUL-terminated: pass
// them only to readers bounded to four bytes (such as SevenSegments::showText),
// never to longer or unbounded readers such as I2cLcd::showLine.
const __FlashStringHelper *commonText(uint8_t offset) {
  return reinterpret_cast<const __FlashStringHelper *>(CommonTexts + offset);
}

// Maps persisted illumination modes Off/Auto/On to packed display labels.
const uint8_t ModeTextOffsets[] PROGMEM = {
    TextOff, TextAuto, TextOn};

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
  STATUS_BUZZER_BUSY = 1U << 12,
  STATUS_PROGRAM_RUNNING = 1U << 13,
  STATUS_HOST_OFFLINE = 1U << 14,
  STATUS_HOT = 1U << 15
};
