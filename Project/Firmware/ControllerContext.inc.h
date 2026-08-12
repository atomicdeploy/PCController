// Implementation fragment compiled once; owns cross-domain objects and runtime state.
// -----------------------------------------------------------------------------
// Hardware state
// -----------------------------------------------------------------------------

// Filtered readings in native integer units: mV, mA, mW, and centi-degrees C.
struct SensorState {
  int32_t supplyMilliVolts = INVALID_I32;
  int32_t busMilliVolts = INVALID_I32;
  int32_t currentMilliAmps = INVALID_I32;
  int32_t powerMilliWatts = INVALID_I32;
  int16_t temperatureCentiC[2] = {INVALID_I16, INVALID_I16};
};
static_assert(sizeof(SensorState) == 20, "Sensor telemetry wire layout changed");

// Most recent valid 433 MHz frame, retained for telemetry and repeat handling.
struct RadioState {
  uint32_t lastCode = 0;
  uint16_t lastPulseLength = 0;
  uint8_t lastBitLength = 0;
  uint8_t lastProtocol = 0;
};

// Exact 48-byte StatusResponse payload; field order is a wire compatibility ABI.
struct __attribute__((packed)) TelemetryPayload {
  uint32_t uptimeMs;
  SensorState sensors;
  uint16_t flags;
  uint8_t rawInputs;
  uint8_t activeKeys;
  uint8_t relayMask;
  uint8_t menuPage;
  uint8_t mode;
  uint8_t doorOpen;
  uint8_t bluetoothState;
  uint8_t pwmAvailable;
  uint8_t pwmChannel;
  uint16_t pwmValue;
  uint8_t lcdAddress;
  uint8_t pwmErrors;
  uint16_t framingErrors;
  uint16_t crcErrors;
  uint8_t resetCause;
  uint32_t resetCount;
};
static_assert(sizeof(TelemetryPayload) == ControllerProtocol::MaximumPayload,
              "Native telemetry wire layout changed");

// Long-lived hardware drivers and protocol-domain coordinators.
PwmExpanderDriver pwmDriver(BoardPins::PwmAddress);
PwmController pwm(pwmDriver);
Ina219Sensor ina219(BoardPins::Ina219Address);
DallasTemperatureBus temperatureBus(BoardPins::OneWireData);
Ds18b20Address temperatureAddresses[2];
RCSwitch radioReceiver;
RelayController relays(shiftRegisters);
ControllerProtocol::UartProtocol appProtocol(Serial);
ControllerEvents appEvents(appProtocol);
MacroQueue macroPlayback(appProtocol);

// Front-panel key order intentionally matches MenuAction IDs 0..3.
Key menuKeys[] = {
    Key(BoardPins::KeyPrevious),
    Key(BoardPins::KeyNext),
    Key(BoardPins::KeyDecrease),
    Key(BoardPins::KeyIncrease),
};

SensorState sensors;
RadioState radioState;

// Smooths normal readings while allowing invalid/HOT state changes immediately.
int16_t smoothTemperature(int16_t filtered, int16_t sample) {
  if (filtered == INVALID_I16 || sample == INVALID_I16 ||
      sample >= HOT_TEMPERATURE_CENTI_C) {
    return sample;
  }
  // A 50/50 EMA smooths jitter without delaying first-valid or HOT indication.
  return static_cast<int16_t>(filtered + (sample - filtered) / 2);
}

// Peripheral availability, asynchronous conversion, and RF-learning state.
bool ina219Available = false;
bool pwmAvailable = false;
uint8_t temperatureAddressCount = 0;
bool temperatureConversionPending = false;
bool learningActive = false;
uint8_t learningMode = RF_LEARN_INDEFINITE;
uint8_t learningTotalSeconds = 0;
uint8_t learningReportedRemaining = 0;
uint32_t learningEndsAt = 0;

// Active page, modal editor selection, and transient front-panel deadlines.
uint8_t menuPage = PAGE_DOOR;
#if PCCONTROLLER_MENU_HIERARCHY
// Low bits are a category ID; bit 7 selects the category-list parent level.
uint8_t menuTreeState = 0;
#endif
uint8_t relayMenuIndex = 0;
#if PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES
uint8_t userPwmMenuIndex = 0;
#endif
uint8_t userRelayMenuIndex = 0;
uint8_t userRelayBehavior = 0;
#if PCCONTROLLER_ENABLE_LOCAL_SETTINGS_EDITOR
uint8_t settingsMenuItem = 0;
#endif
uint8_t identifiedKey = 0;
uint32_t menuLabelEndsAt = 0;
uint32_t modeEnteredAt = 0;
uint32_t identifiedKeyEndsAt = 0;
uint32_t motionExitStartedAt = 0;
uint32_t flashMessageEndsAt = 0;
// Presentation visibility/order is configured independently over UART and is
// not rolled back by a local edit. The late display-options byte is captured
// separately because it owns the locally editable closed-door brightness.
constexpr size_t MenuEditSnapshotSize =
    offsetof(ControllerSettings, menuFlags) +
    sizeof(ControllerSettings::menuFlags);
uint8_t editSnapshot[MenuEditSnapshotSize]{};
uint8_t editDisplayOptionsSnapshot = 0;
ProgramMode editReturnMode = MODE_DOOR;
bool editTransactionActive = false;
bool flashMessageSaved = false;
#if PCCONTROLLER_ENABLE_MACRO_CAPTURE && \
    !PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS
uint8_t nextLocalMacroId = 1;
bool suppressLocalMacroClassification = false;
#endif
uint8_t motionPressedMask = 0;

ModeManager<ProgramMode> modeManager(MODE_BOOT);
#if PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI
ProgramMode modeBeforeLearning = MODE_RF;
#endif

// Zero disables periodic telemetry; the live EEPROM default is 500 ms.
uint16_t streamPeriodMs = 0;

// Cooperative task timestamps; each service owns one cadence/deadline.
uint32_t lastShiftPollAt = 0;
uint32_t lastDisplayRefreshAt = 0;
uint32_t lastIna219SampleAt = 0;
uint32_t lastTemperatureRequestAt = 0;
uint32_t lastTelemetryAt = 0;
uint32_t lastPowerSignalFallbackAt = 0;
bool macroPwmSafeStopPending = false;

// RF momentary actions expire locally even if repeats or the host disappear.
RemoteActionKind remoteMomentaryKind = RemoteActionKind::None;
uint8_t remoteMomentaryValue = 0;
uint32_t remoteMomentaryEndsAt = 0;
uint32_t lastRemoteActionAt = 0;
uint32_t lastRemoteActionCode = 0;
uint8_t lastRelayMask = 0;
bool firmwareReady = false;
#if PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS
uint8_t lastPushedSegments[4] = {};
uint8_t lastPushedSegmentBrightness = 0;
uint8_t lastPushedBuzzerRevision = 0;
#if PCCONTROLLER_ENABLE_PCA9685 && PCCONTROLLER_ENABLE_STATUS_LED_ENGINE
uint8_t lastPushedStatusLed[6] = {};
#endif
#endif

// Host-captured panel text, LCD fallback metadata, and cooperative I2C lease.
bool hostSegmentTextActive = false;
// DISPLAY_TEXT reuses one 40-byte buffer for static text or a scheduled scroll.
char hostSegmentText[41] = {};
uint8_t hostSegmentTextLength = 0;
uint8_t hostSegmentScrollIndex = 0;
uint16_t hostSegmentStepMs = 0;
#if PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS
// Low two bits select once/loop/interval, bit 6 marks the interval wait, and
// bit 7 forces a marquee even when the text fits the four-cell display.
uint8_t hostSegmentOptions = 0;
uint16_t hostSegmentHoldMs = 0;
uint8_t hostSegmentIntervalSeconds = 0;
#endif
char temperatureSegmentText[2][4] = {
    {'L', '-', '-', 'C'},
    {'b', '-', '-', 'C'},
};
// Static text uses this as an expiry; scrolling text uses it as the next step.
uint32_t hostSegmentTextEndsAt = 0;
uint32_t lastHostActivityAt = 0;
char hostLcdText[32] = {};
uint8_t hostLcdFlags = 0;
uint8_t hostLcdAddress = 0;
uint16_t hostPanelMeta = 0;
uint16_t i2cLeaseUntil = 0;
uint8_t i2cLeaseAddress = 0;
// One wrap-safe application clock is refreshed at setup/loop and asynchronous
// UART or key entry. AVR-hot paths cache this value in registers before calling
// reusable drivers; ISRs keep independent edge timing and never depend on it.
static uint32_t now = 0;
// Host state bits are intentionally sparse to preserve the existing wire value.
constexpr uint8_t HOST_SEEN = 1U << 0;
constexpr uint8_t HOST_LCD_OFFLINE = 1U << 1;
constexpr uint8_t HOST_PROGRAM_RUNNING = 1U << 2;
constexpr uint8_t HOST_STATUS_OVERRIDE = 1U << 3;
constexpr uint8_t HOST_PANEL_CAPTURED = 1U << 4;

// -----------------------------------------------------------------------------
// Forward declarations
// -----------------------------------------------------------------------------

// Cross-domain entry points required before their ordered implementation fragments.
void handleMenuAction(uint8_t action, bool fromRemote = false);
void applyKeyGesture(uint8_t bit, KeyEvent event, InputEventSource source,
                     bool emitEvidence);
void setMenuPage(uint8_t page);
void sendTelemetry(uint8_t sequence);
void endLearning(uint8_t state, int8_t feedback);
void programService(uint32_t at);
void serviceSystemInputs(uint32_t at);
void serviceIlluminationSettings(uint32_t at);
void handleProtocolFrame(const ControllerProtocol::Frame &frame, void *);
void releaseHostPanel();
