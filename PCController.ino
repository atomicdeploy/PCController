#include <Arduino.h>
#include <avr/wdt.h>

#include "ProjectConfig.h"

#include <RCSwitch.h>

#include "LocalLib/BoardPins.h"
#include "LocalLib/DallasTemperatureBus.h"
#include "LocalLib/Keys.h"
#include "LocalLib/ModeManager.h"
#include "LocalLib/SevenSegments.h"
#include "LocalLib/ShiftRegisters.h"
#include "LocalLib/Tasks.h"
#include "LocalLib/TonePlayer.h"
#include "Project/AddressableLeds.h"
#include "Project/BootMelody.h"
#include "Project/ControllerEvents.h"
#include "Project/CompactI2c.h"
#include "Project/IlluminationController.h"
#include "Project/Ina219Sensor.h"
#include "Project/MacroQueue.h"
#include "Project/PwmController.h"
#include "Project/PwmExpanderDriver.h"
#include "Project/RelayController.h"
#include "Project/RemoteLearningStore.h"
#include "Project/ResetTelemetry.h"
#include "Project/SafeResetController.h"
#include "Project/SettingsStore.h"
#include "Project/StatusLedController.h"
#include "Project/SystemInputs.h"
#include "Project/UartProtocol.h"
#include "LocalLib/I2cLcd.h"

// -----------------------------------------------------------------------------
// Firmware configuration
// -----------------------------------------------------------------------------

#ifndef PCCONTROLLER_BUILD_HASH
#define PCCONTROLLER_BUILD_HASH 0UL
#endif

#ifndef PCCONTROLLER_BUILD_TIMESTAMP
#define PCCONTROLLER_BUILD_TIMESTAMP 0UL
#endif

constexpr uint16_t SHIFT_POLL_MS = 5;
constexpr uint16_t DISPLAY_REFRESH_MS = 20;
constexpr uint16_t INA219_SAMPLE_MS = 500;
constexpr uint16_t INA219_DOOR_OPEN_SAMPLE_MS = 100;
constexpr uint16_t TEMPERATURE_PERIOD_MS = 1000;
constexpr uint16_t TEMPERATURE_DOOR_OPEN_PERIOD_MS = 450;
constexpr uint16_t TEMPERATURE_CONVERSION_MS = 375;
constexpr uint16_t HOST_OFFLINE_MS = 5000;
constexpr uint8_t DEFAULT_LEARNING_SECONDS = 15;
constexpr uint8_t MAX_LEARNING_SECONDS = 120;
constexpr uint8_t MAX_RC_PROTOCOL = 12;
constexpr uint8_t LEARN_MULTI = 1U << 0;
constexpr uint8_t LEARN_INDEFINITE = 1U << 1;
constexpr uint16_t PWM_MENU_STEP = 256;
constexpr uint8_t ILLUMINATION_MENU_STEP = 16;
constexpr int16_t HOT_TEMPERATURE_CENTI_C = 5000;

constexpr int32_t INVALID_I32 = (-2147483647L - 1L);
constexpr int16_t INVALID_I16 = (-32767 - 1);

enum MenuAction : uint8_t {
  MENU_PREVIOUS = 0,
  MENU_NEXT = 1,
  MENU_DECREASE = 2,
  MENU_INCREASE = 3
};

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

const __FlashStringHelper *commonText(uint8_t offset) {
  return reinterpret_cast<const __FlashStringHelper *>(CommonTexts + offset);
}

const uint8_t ModeTextOffsets[] PROGMEM = {
    TextOff, TextAuto, TextOn, TextOff, TextManual, TextAuto};

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

// -----------------------------------------------------------------------------
// Hardware state
// -----------------------------------------------------------------------------

struct SensorState {
  int32_t supplyMilliVolts = INVALID_I32;
  int32_t busMilliVolts = INVALID_I32;
  int32_t currentMilliAmps = INVALID_I32;
  int32_t powerMilliWatts = INVALID_I32;
  int16_t temperatureCentiC[2] = {INVALID_I16, INVALID_I16};
};
static_assert(sizeof(SensorState) == 20, "Sensor telemetry wire layout changed");

struct RadioState {
  uint32_t lastCode = 0;
  uint16_t lastPulseLength = 0;
  uint8_t lastBitLength = 0;
  uint8_t lastProtocol = 0;
};

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
  uint8_t pwmMode;
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

PwmExpanderDriver pwmDriver(BoardPins::PwmAddress);
PwmController pwm(pwmDriver);
Ina219Sensor ina219(BoardPins::Ina219Address);
DallasTemperatureBus temperatureBus(BoardPins::OneWireData);
Ds18b20Address temperatureAddresses[2];
RCSwitch radioReceiver;
RCSwitch radioTransmitter;
RelayController relays(shiftRegisters);
ControllerProtocol::UartProtocol appProtocol(Serial);
ControllerEvents appEvents(appProtocol);
MacroQueue macroPlayback(appProtocol);

Key menuKeys[] = {
    Key(BoardPins::KeyPrevious),
    Key(BoardPins::KeyNext),
    Key(BoardPins::KeyDecrease),
    Key(BoardPins::KeyIncrease),
};

SensorState sensors;
RadioState radioState;

int16_t smoothTemperature(int16_t filtered, int16_t sample) {
  if (filtered == INVALID_I16 || sample == INVALID_I16 ||
      sample >= HOT_TEMPERATURE_CENTI_C) {
    return sample;
  }
  // A 50/50 EMA smooths jitter without delaying first-valid or HOT indication.
  return static_cast<int16_t>(filtered + (sample - filtered) / 2);
}

bool ina219Available = false;
bool pwmAvailable = false;
uint8_t temperatureAddressCount = 0;
bool temperatureConversionPending = false;
bool learningActive = false;
uint8_t learningOptions = 0;
uint32_t learningEndsAt = 0;

uint8_t menuPage = PAGE_STATUS;
#if PCCONTROLLER_MENU_HIERARCHY
// Low bits are a category ID; bit 7 selects the category-list parent level.
uint8_t menuTreeState = 0;
#endif
uint8_t relayMenuIndex = 0;
uint8_t userPwmMenuIndex = 0;
uint8_t userRelayMenuIndex = 0;
uint8_t userRelayBehavior = 0;
uint8_t settingsMenuItem = 0;
uint8_t identifiedKey = 0;
uint32_t menuLabelEndsAt = 0;
uint32_t modeEnteredAt = 0;
uint32_t identifiedKeyEndsAt = 0;
uint32_t motionExitStartedAt = 0;
uint32_t flashMessageEndsAt = 0;
// Menu edits only touch the legacy 19-byte settings prefix. Presentation
// visibility/order is configured independently over UART and must neither be
// duplicated in SRAM nor rolled back by an unrelated front-panel edit.
constexpr size_t MenuEditSnapshotSize =
    offsetof(ControllerSettings, menuFlags) +
    sizeof(ControllerSettings::menuFlags);
uint8_t editSnapshot[MenuEditSnapshotSize]{};
ProgramMode editReturnMode = MODE_STATUS;
bool editTransactionActive = false;
bool flashMessageSaved = false;

ModeManager<ProgramMode> modeManager(MODE_BOOT);
ProgramMode modeBeforeLearning = MODE_RF;

uint16_t streamPeriodMs = 500;

uint32_t lastShiftPollAt = 0;
uint32_t lastDisplayRefreshAt = 0;
uint32_t lastIna219SampleAt = 0;
uint32_t lastTemperatureRequestAt = 0;
uint32_t lastTelemetryAt = 0;

RemoteActionKind remoteMomentaryKind = RemoteActionKind::None;
uint8_t remoteMomentaryValue = 0;
uint32_t remoteMomentaryEndsAt = 0;
uint32_t lastRemoteActionAt = 0;
uint32_t lastRemoteActionCode = 0;
uint8_t lastRelayMask = 0;
bool firmwareReady = false;

bool hostSegmentTextActive = false;
char hostSegmentText[5] = {};
char temperatureSegmentText[2][4] = {
    {'L', '-', '-', 'C'},
    {'b', '-', '-', 'C'},
};
uint32_t hostSegmentTextEndsAt = 0;
uint32_t lastHostActivityAt = 0;
char hostLcdText[32] = {};
uint8_t hostLcdFlags = 0;
uint8_t hostLcdAddress = 0;
uint16_t hostPanelMeta = 0;
uint16_t i2cLeaseUntil = 0;
uint8_t i2cLeaseAddress = 0;
// One wrap-safe application clock snapshot is refreshed at setup/loop and at
// asynchronous UART entry. Ordinary services consume it without repeatedly
// passing four timestamp bytes through the AVR call graph; ISRs use their own
// edge timing and never depend on this value.
uint32_t now = 0;
constexpr uint8_t HOST_SEEN = 1U << 0;
constexpr uint8_t HOST_LCD_OFFLINE = 1U << 1;
constexpr uint8_t HOST_PANEL_CAPTURED = 1U << 4;

// -----------------------------------------------------------------------------
// Forward declarations
// -----------------------------------------------------------------------------

void handleMenuAction(uint8_t action, bool fromRemote = false);
void setMenuPage(uint8_t page);
void sendTelemetry(uint8_t sequence);
void endLearning(uint8_t state, int8_t feedback);
void programService(uint32_t now);
void serviceSystemInputs(uint32_t now);
void serviceIlluminationSettings(uint32_t now);
void handleProtocolFrame(const ControllerProtocol::Frame &frame, void *);
void releaseHostPanel();

// -----------------------------------------------------------------------------
// Utility helpers
// -----------------------------------------------------------------------------

bool __attribute__((noinline)) timeReached(uint32_t now,
                                           uint32_t deadline) {
  return static_cast<int32_t>(now - deadline) >= 0;
}

bool i2cLeaseActive(uint32_t now) {
  if (i2cLeaseAddress != 0 &&
      static_cast<int16_t>(static_cast<uint16_t>(now) - i2cLeaseUntil) >= 0) {
    i2cLeaseAddress = 0;
  }
  return i2cLeaseAddress != 0;
}

bool motionPolicyAllows() {
  const uint8_t policy = static_cast<uint8_t>(
      settingsStore.values().motionDoorPolicy());
  return policy == static_cast<uint8_t>(MotionDoorPolicy::Always) ||
         (policy == static_cast<uint8_t>(MotionDoorPolicy::ClosedOnly) &&
          !systemInputs.doorOpen()) ||
         (policy == static_cast<uint8_t>(MotionDoorPolicy::OpenOnly) &&
          systemInputs.doorOpen());
}

int32_t scaledMilliValue(int32_t value, uint8_t decimalPlaces) {
  const uint16_t divisor =
      decimalPlaces == 0 ? 1000U : (decimalPlaces == 1 ? 100U : 10U);
  const int32_t rounding = divisor / 2U;
  return value >= 0 ? (value + rounding) / divisor
                    : (value - rounding) / divisor;
}

void loadIlluminationSettings() {
  settingsStore.begin(now);
  const ControllerSettings &settings = settingsStore.values();
  illumination.setMode(
      static_cast<IlluminationMode>(settings.illuminationMode));
  illumination.setOnBrightness(settings.illuminationOnBrightness);
  illumination.setOffBrightness(settings.illuminationOffBrightness);
}

void markIlluminationSettingsChanged(uint32_t now) {
  ControllerSettings &settings = settingsStore.values();
  settings.illuminationMode = static_cast<uint8_t>(illumination.mode());
  settings.illuminationOnBrightness = illumination.onBrightness();
  settings.illuminationOffBrightness = illumination.offBrightness();
  if (!editTransactionActive) {
    settingsStore.markDirty(now);
  }
}

void serviceIlluminationSettings(uint32_t now) {
  settingsStore.service(now, !learningActive && !editTransactionActive);
}

void releaseHostPanel() {
  hostLcdFlags &= static_cast<uint8_t>(~HOST_PANEL_CAPTURED);
  hostPanelMeta = 0;
  hostSegmentTextActive = false;
  setMenuPage(settingsStore.values().defaultMenuPage);
}

// The PC preloads a fixed offline page in hidden LCD DDRAM. On abrupt heartbeat
// loss the MCU needs only shift that page into view; no scanner, strings, or
// full HD44780 renderer are duplicated in scarce flash.
void showHostOfflineOnLcd() {
  if (hostLcdAddress == 0) {
    return;
  }
  static const uint8_t shiftCommand[] = {
      0x18, 0x1C, 0x18, 0x88, 0x8C, 0x88,
  };
  for (uint8_t batch = 0; batch < 4; ++batch) {
    i2cBus.beginTransmission(hostLcdAddress);
    const uint8_t commands = batch == 3 ? 1 : 5;
    for (uint8_t index = 0; index < commands; ++index) {
      // PCF8574 pulses for HD44780 command 0x18 (shift display left).
      i2cBus.write(shiftCommand, sizeof(shiftCommand));
    }
    (void)i2cBus.endTransmission();
  }
}

void appendU16(uint8_t *buffer, uint8_t &index, uint16_t value) {
  buffer[index++] = static_cast<uint8_t>(value);
  buffer[index++] = static_cast<uint8_t>(value >> 8);
}

void appendI16(uint8_t *buffer, uint8_t &index, int16_t value) {
  appendU16(buffer, index, static_cast<uint16_t>(value));
}

uint16_t readU16(const uint8_t *buffer) {
  return static_cast<uint16_t>(buffer[0]) |
         (static_cast<uint16_t>(buffer[1]) << 8);
}

uint32_t readU32(const uint8_t *buffer) {
  return static_cast<uint32_t>(buffer[0]) |
         (static_cast<uint32_t>(buffer[1]) << 8) |
         (static_cast<uint32_t>(buffer[2]) << 16) |
         (static_cast<uint32_t>(buffer[3]) << 24);
}

void safeStopMacroOutputs() {
  relays.allOff(now);
  pwm.clearMask(PwmChannels::UserTestMask);
}

uint16_t statusFlags() {
  uint16_t flags = 0;
  if (ina219Available) {
    flags |= STATUS_INA219;
  }
  if (pwm.available()) {
    flags |= STATUS_PWM;
  }
  if (sensors.temperatureCentiC[0] != INVALID_I16) {
    flags |= STATUS_TLED;
  }
  if (sensors.temperatureCentiC[1] != INVALID_I16) {
    flags |= STATUS_TBT;
  }
  if (learnedRemotes.count() != 0) {
    flags |= STATUS_RF_LEARNED;
  }
  if (learningActive) {
    flags |= STATUS_RF_LEARNING;
  }
  if (streamPeriodMs != 0) {
    flags |= STATUS_STREAMING;
  }
  if (radioState.lastCode != 0) {
    flags |= STATUS_RF_RECEIVED;
  }
  if (settingsStore.values().silent()) {
    flags |= STATUS_SILENT;
  }
  if (relays.anySideBusy()) {
    flags |= STATUS_RELAY_BUSY;
  }
  if (systemInputs.doorOpen()) {
    flags |= STATUS_DOOR_OPEN;
  }
  if (buzzer.isBusy()) {
    flags |= STATUS_BUZZER_BUSY;
  }
  return flags;
}

// -----------------------------------------------------------------------------
// Learned 433 MHz remotes and RC-switch
// -----------------------------------------------------------------------------

void beginLearning(uint8_t timeoutSeconds, uint8_t options = 0) {
  if (timeoutSeconds == 0) {
    timeoutSeconds = DEFAULT_LEARNING_SECONDS;
  }
  options &= static_cast<uint8_t>(LEARN_MULTI | LEARN_INDEFINITE);
  if (timeoutSeconds > MAX_LEARNING_SECONDS) {
    timeoutSeconds = MAX_LEARNING_SECONDS;
  }

  buzzer.stop();
  const ProgramMode currentMode = modeManager.current();
  if (currentMode <= MODE_RF) {
    modeBeforeLearning = currentMode;
  } else {
    modeBeforeLearning = MODE_RF;
  }
  learningActive = true;
  learningOptions = options;
  learningEndsAt = (options & LEARN_INDEFINITE) != 0
                       ? 0
                       : now +
                             static_cast<uint32_t>(timeoutSeconds) * 1000UL;
  modeManager.transitionTo(MODE_RF_LEARNING);
  appEvents.rfLearning(3, learnedRemotes.count());
}

void endLearning(uint8_t state, int8_t feedback) {
  if (!learningActive) {
    return;
  }
  learningActive = false;
  learningOptions = 0;
  learningEndsAt = 0;
  if (modeManager.current() == MODE_RF_LEARNING) {
    modeManager.transitionTo(modeBeforeLearning);
  }
  appEvents.rfLearning(state, learnedRemotes.count());
  if (feedback > 0) {
    buzzer.success();
  } else if (feedback < 0) {
    buzzer.error();
  }
}

void stopRemoteMomentary(uint32_t now) {
  switch (remoteMomentaryKind) {
    case RemoteActionKind::Relay:
      relays.requestRelayForTest(
          static_cast<uint8_t>(remoteMomentaryValue + 1), false, now);
      break;
    case RemoteActionKind::Side:
      relays.stopSide(static_cast<::RelaySide>(remoteMomentaryValue), now);
      break;
    case RemoteActionKind::Pwm:
      pwm.setChannel(remoteMomentaryValue, now);
      pwm.setValue(0, now);
      break;
    default:
      break;
  }
  remoteMomentaryKind = RemoteActionKind::None;
  remoteMomentaryEndsAt = 0;
}

void serviceRemoteMomentary(uint32_t now) {
  if (remoteMomentaryKind != RemoteActionKind::None &&
      timeReached(now, remoteMomentaryEndsAt)) {
    stopRemoteMomentary(now);
  }
}

void executeLearnedRemote(const LearnedRemote &remote, uint32_t now) {
  const RemoteActionKind kind =
      static_cast<RemoteActionKind>(remote.actionKind);
  const RemoteBehavior behavior =
      static_cast<RemoteBehavior>(remote.behavior);
  if (remoteMomentaryKind != RemoteActionKind::None &&
      (remoteMomentaryKind != kind ||
       remoteMomentaryValue != remote.actionValue)) {
    stopRemoteMomentary(now);
  }
  switch (kind) {
    case RemoteActionKind::Key:
      appEvents.key(remote.actionValue,
                    static_cast<uint8_t>(KeyEvent::Click),
                    InputEventSource::Radio, remote.id);
      handleMenuAction(remote.actionValue, true);
      return;
    case RemoteActionKind::Menu:
      handleMenuAction(remote.actionValue, true);
      return;
    case RemoteActionKind::Relay: {
      const uint8_t mask = static_cast<uint8_t>(_BV(remote.actionValue));
      const bool active = (relays.activeRelayMask() & mask) != 0;
      const bool next = behavior == RemoteBehavior::Toggle ||
                                behavior == RemoteBehavior::Press
                            ? !active
                            : true;
      const bool accepted = relays.requestRelayForTest(
          static_cast<uint8_t>(remote.actionValue + 1), next, now);
      if (accepted && behavior == RemoteBehavior::Momentary) {
        remoteMomentaryKind = kind;
        remoteMomentaryValue = remote.actionValue;
        remoteMomentaryEndsAt = now + 350;
      }
      return;
    }
    case RemoteActionKind::Side:
      if (behavior != RemoteBehavior::Stop &&
          !relays.motionAllowed()) {
        return;
      }
      if (behavior == RemoteBehavior::Stop) {
        relays.stopSide(static_cast<::RelaySide>(remote.actionValue), now);
      } else {
        const RelayDirection direction =
            behavior == RemoteBehavior::Down ? RelayDirection::Reverse
                                               : RelayDirection::Forward;
        if (relays.requestSide(static_cast<::RelaySide>(remote.actionValue),
                               direction, true, now)) {
          remoteMomentaryKind = kind;
          remoteMomentaryValue = remote.actionValue;
          remoteMomentaryEndsAt = now + 350;
        }
      }
      return;
    case RemoteActionKind::Pwm: {
      if (pwm.mode() == PwmTestMode::Auto) {
        pwm.setMode(PwmTestMode::Manual, now);
      }
      pwm.setChannel(remote.actionValue, now);
      const bool active = pwm.logicalValue(remote.actionValue) != 0;
      pwm.setValue(behavior == RemoteBehavior::Momentary
                       ? 4095
                       : (active ? 0 : 4095),
                   now);
      if (behavior == RemoteBehavior::Momentary) {
        remoteMomentaryKind = kind;
        remoteMomentaryValue = remote.actionValue;
        remoteMomentaryEndsAt = now + 350;
      }
      return;
    }
    case RemoteActionKind::None:
      return;
  }
}

void serviceRadio() {
  if (!radioReceiver.available()) {
    return;
  }

  const uint32_t code = radioReceiver.getReceivedValue();
  const uint8_t bits = radioReceiver.getReceivedBitlength();
  const uint8_t protocol = radioReceiver.getReceivedProtocol();
  const uint16_t pulseLength = radioReceiver.getReceivedDelay();
  radioReceiver.resetAvailable();

  if (code == 0 || bits == 0) {
    return;
  }

  radioState.lastCode = code;
  radioState.lastBitLength = bits;
  radioState.lastProtocol = protocol;
  radioState.lastPulseLength = pulseLength;

  const bool repeated =
      code == lastRemoteActionCode &&
      static_cast<uint32_t>(now - lastRemoteActionAt) < 400;
  lastRemoteActionCode = code;
  lastRemoteActionAt = now;

  if (learningActive) {
    if (repeated) {
      return;
    }
    uint8_t learnedId = 0;
    const bool learned =
        learnedRemotes.learn(code, bits, protocol, pulseLength, learnedId);
    if (learned) {
      appEvents.rfLearned(learnedId);
    }
    appEvents.rfReceived(code, bits, protocol, pulseLength,
                         learned ? learnedId : 0xFF);
    statusLeds.playCue(StatusLedCue::Radio, 320);
    if (!learned) {
      endLearning(2, -1);
    } else if (learnedRemotes.count() >= RemoteLearningStore::Capacity) {
      endLearning(2, 1);
    } else if ((learningOptions & LEARN_MULTI) == 0) {
      endLearning(0, 1);
    }
    return;
  }

  LearnedRemote remote;
  const bool learned = learnedRemotes.find(code, bits, protocol, remote);
  appEvents.rfReceived(code, bits, protocol, pulseLength,
                       learned ? remote.id : 0xFF);
  statusLeds.playCue(StatusLedCue::Radio, 240);
  if (learned) {
    const RemoteBehavior behavior =
        static_cast<RemoteBehavior>(remote.behavior);
    const bool refreshable =
        behavior == RemoteBehavior::Momentary ||
        behavior == RemoteBehavior::Up ||
        behavior == RemoteBehavior::Down;
    if (refreshable || !repeated) {
      executeLearnedRemote(remote, now);
    }
  }
}

bool transmitRadio(uint32_t code, uint8_t bits, uint8_t protocol,
                   uint16_t pulseLength) {
  if (learningActive || code == 0 || bits == 0 || bits > 32 ||
      protocol == 0 || protocol > MAX_RC_PROTOCOL) {
    return false;
  }

  radioReceiver.disableReceive();
  radioTransmitter.setProtocol(protocol);
  if (pulseLength != 0) {
    radioTransmitter.setPulseLength(pulseLength);
  }
  radioTransmitter.send(code, bits);
  radioReceiver.enableReceive(digitalPinToInterrupt(BoardPins::RcReceive));
  return true;
}

// -----------------------------------------------------------------------------
// Sensors and PWM
// -----------------------------------------------------------------------------

bool prepareI2cBus() {
  constexpr uint8_t sda = _BV(PORTC4);
  constexpr uint8_t scl = _BV(PORTC5);
  // Release both open-drain lines with their weak pull-ups enabled.
  PORTC |= static_cast<uint8_t>(sda | scl);
  DDRC &= static_cast<uint8_t>(~(sda | scl));
  delayMicroseconds(10);

  if ((PINC & scl) == 0) {
    return false;
  }

  // Let a slave stranded mid-byte advance and release SDA before TWI starts.
  if ((PINC & sda) == 0) {
    for (uint8_t pulse = 0; pulse < 9; ++pulse) {
      PORTC &= static_cast<uint8_t>(~scl);
      DDRC |= scl;
      delayMicroseconds(5);
      PORTC |= scl;
      DDRC &= static_cast<uint8_t>(~scl);
      delayMicroseconds(5);
      if ((PINC & scl) == 0) {
        return false;
      }
    }

    // Generate a STOP condition after the recovery clocks.
    PORTC &= static_cast<uint8_t>(~sda);
    DDRC |= sda;
    delayMicroseconds(5);
    PORTC |= scl;
    DDRC &= static_cast<uint8_t>(~scl);
    delayMicroseconds(5);
    PORTC |= sda;
    DDRC &= static_cast<uint8_t>(~sda);
    delayMicroseconds(5);
  }

  return (PINC & static_cast<uint8_t>(sda | scl)) ==
         static_cast<uint8_t>(sda | scl);
}

bool normalizePwmMode2() {
  constexpr uint8_t expectedMode2 = PwmController::recommendedMode2();
  constexpr uint8_t PwmMode2Register = 0x01;
  i2cBus.beginTransmission(BoardPins::PwmAddress);
  i2cBus.write(PwmMode2Register);
  // Normal active-high PWM uses MODE2=0x04; active-low builds use 0x05.
  i2cBus.write(expectedMode2);
  return i2cBus.endTransmission() == 0;
}

// Low-pass a completed INA219 sample in-place without another history buffer.
__attribute__((noinline)) int32_t
smoothInaValue(int32_t previous, int32_t sample, bool quarter) {
  if (previous == INVALID_I32) {
    return sample;
  }
  int32_t step = (sample - previous) >> 1;
  if (quarter) {
    step >>= 1;
  }
  return step == 0 ? sample : previous + step;
}

void sampleIna219(uint32_t now) {
  const uint16_t samplePeriod =
      systemInputs.doorOpen() ? INA219_DOOR_OPEN_SAMPLE_MS
                              : INA219_SAMPLE_MS;
  if (!ina219Available ||
      static_cast<uint32_t>(now - lastIna219SampleAt) < samplePeriod) {
    return;
  }
  lastIna219SampleAt = now;
  Ina219Reading reading;
  if (!ina219.read(reading)) {
    ina219Available = false;
    return;
  }
  int32_t *filtered = &sensors.supplyMilliVolts;
  const int32_t *sample = &reading.supplyMilliVolts;
  for (uint8_t index = 0; index < 4; ++index) {
    filtered[index] = smoothInaValue(filtered[index], sample[index],
                                     index >= 2);
  }
}

void sortTemperatureAddresses() {
  if (temperatureAddressCount < 2 ||
      memcmp(temperatureAddresses[0], temperatureAddresses[1], 8) <= 0) {
    return;
  }
  for (uint8_t i = 0; i < 8; ++i) {
    const uint8_t temporary = temperatureAddresses[0][i];
    temperatureAddresses[0][i] = temperatureAddresses[1][i];
    temperatureAddresses[1][i] = temporary;
  }
}

void discoverTemperatureSensors() {
  temperatureAddressCount = 0;
  const uint8_t discovered = temperatureBus.getDeviceCount();
  for (uint8_t index = 0; index < discovered &&
                          temperatureAddressCount < 2;
       ++index) {
    if (temperatureBus.getAddress(
            temperatureAddresses[temperatureAddressCount], index)) {
      ++temperatureAddressCount;
    }
  }
  sortTemperatureAddresses();
  for (uint8_t index = 0; index < temperatureAddressCount; ++index) {
    // 11-bit conversion is 375 ms with 0.125 C resolution, allowing a more
    // responsive open-enclosure display without blocking UART servicing.
    temperatureBus.setResolution(temperatureAddresses[index], 11);
  }
}

uint8_t temperatureRole(uint8_t addressIndex) {
  return settingsStore.values().swapTemperatureSensors()
             ? addressIndex
             : static_cast<uint8_t>(1 - addressIndex);
}

void requestTemperatures(uint32_t now) {
  if (temperatureAddressCount == 0 || learningActive) {
    return;
  }
  temperatureBus.requestTemperatures();
  temperatureConversionPending = true;
  lastTemperatureRequestAt = now;
}

void formatTemperatureSegmentText(uint8_t index, int16_t centiC) {
  char *text = temperatureSegmentText[index];
  if (centiC < 0 || centiC >= 9950) {
    text[1] = '-';
    text[2] = '-';
    return;
  }
  const uint8_t wholeC =
      static_cast<uint8_t>((centiC + 50) / 100);
  text[1] = static_cast<char>('0' + wholeC / 10);
  text[2] = static_cast<char>('0' + wholeC % 10);
}

void serviceTemperatures(uint32_t now) {
  // OneWire briefly masks interrupts. Keep it idle during RF learning so
  // receiver pulse timing is not disturbed.
  if (learningActive || temperatureAddressCount == 0) {
    return;
  }

  if (temperatureConversionPending) {
    if (static_cast<uint32_t>(now - lastTemperatureRequestAt) <
        TEMPERATURE_CONVERSION_MS) {
      return;
    }
    for (uint8_t index = 0; index < temperatureAddressCount; ++index) {
      // On this controller's physical harness the lexicographically first ROM
      // is tLED and the second is tBT. The EEPROM swap flag remains available
      // if either probe is replaced or the harness order changes.
      const uint8_t destination = temperatureRole(index);
      const int16_t sample =
          temperatureBus.getTempCentiC(temperatureAddresses[index]);
      sensors.temperatureCentiC[destination] =
          smoothTemperature(sensors.temperatureCentiC[destination], sample);
      formatTemperatureSegmentText(
          destination, sensors.temperatureCentiC[destination]);
    }
    temperatureConversionPending = false;
    return;
  }

  const uint16_t samplePeriod =
      systemInputs.doorOpen() ? TEMPERATURE_DOOR_OPEN_PERIOD_MS
                              : TEMPERATURE_PERIOD_MS;
  if (static_cast<uint32_t>(now - lastTemperatureRequestAt) >=
      samplePeriod) {
    requestTemperatures(now);
  }
}

// -----------------------------------------------------------------------------
// Menu, keys, display, and buzzer
// -----------------------------------------------------------------------------

void showMenuLabel(uint32_t now) {
  display.showText(reinterpret_cast<const __FlashStringHelper *>(
      MenuLabels + static_cast<uint8_t>(menuPage << 2)));
  menuLabelEndsAt = now + 650;
}

void showSettingsLabel(uint32_t now) {
  display.showText(reinterpret_cast<const __FlashStringHelper *>(
      SettingsLabels + static_cast<uint8_t>(settingsMenuItem << 2)));
  menuLabelEndsAt = now + 650;
}

bool isMenuMode(ProgramMode mode) {
  return mode >= MODE_STATUS && mode <= MODE_RF;
}

ProgramMode pageToMode(uint8_t page) {
  return static_cast<ProgramMode>(
      static_cast<uint8_t>(MODE_STATUS) + page % PAGE_COUNT);
}

uint8_t modeToPage(ProgramMode mode) {
  return isMenuMode(mode)
             ? static_cast<uint8_t>(mode) -
                   static_cast<uint8_t>(MODE_STATUS)
             : menuPage;
}

#if PCCONTROLLER_MENU_VISIBILITY
uint8_t configuredMenuPageAt(uint8_t rank) {
#if PCCONTROLLER_MENU_ORDERING
  return settingsStore.values().menuPageAtRank(rank);
#else
  return rank;
#endif
}

uint8_t configuredMenuRank(uint8_t page) {
#if PCCONTROLLER_MENU_ORDERING
  for (uint8_t rank = 0; rank < PAGE_COUNT; ++rank) {
    if (configuredMenuPageAt(rank) == page) {
      return rank;
    }
  }
#endif
  return page;
}

#if PCCONTROLLER_MENU_HIERARCHY
constexpr uint8_t MenuCategoryCount = 4;
constexpr uint8_t MenuCategorySelector = 0x80;

uint8_t menuCategory(uint8_t page) {
  if (page <= PAGE_TBT) {
    return 0; // Monitoring: Status, voltage, current, tLED, tBT.
  }
  if (page <= PAGE_SOUND) {
    return 1; // Environment: illumination, BT Audio, sound.
  }
  return page == PAGE_KEYS || page == PAGE_RF
             ? 3 // Inputs/RF: physical keys and learned RF.
             : 2; // Outputs: PWM, relays, user outputs, motion.
}

bool menuCategorySelected() {
  return (menuTreeState & MenuCategorySelector) != 0;
}
#endif

uint8_t nextConfiguredMenuPage(uint8_t page, bool forward,
                               uint8_t category = 0xFF) {
  uint8_t rank = configuredMenuRank(page);
  for (uint8_t checked = 0; checked < PAGE_COUNT; ++checked) {
    rank = forward ? static_cast<uint8_t>((rank + 1U) % PAGE_COUNT)
                   : (rank == 0 ? PAGE_COUNT - 1U : rank - 1U);
    const uint8_t candidate = configuredMenuPageAt(rank);
    if (!settingsStore.values().menuPageVisible(candidate)) {
      continue;
    }
#if PCCONTROLLER_MENU_HIERARCHY
    if (category < MenuCategoryCount && menuCategory(candidate) != category) {
      continue;
    }
#else
    (void)category;
#endif
    return candidate;
  }
  for (uint8_t rank = 0; rank < PAGE_COUNT; ++rank) {
    const uint8_t fallback = configuredMenuPageAt(rank);
    if (settingsStore.values().menuPageVisible(fallback)) {
      return fallback;
    }
  }
  return PAGE_STATUS; // Valid settings always expose at least one page.
}

#if PCCONTROLLER_MENU_HIERARCHY
uint8_t firstConfiguredMenuPage(uint8_t category) {
  for (uint8_t rank = 0; rank < PAGE_COUNT; ++rank) {
    const uint8_t page = configuredMenuPageAt(rank);
    if (settingsStore.values().menuPageVisible(page) &&
        menuCategory(page) == category) {
      return page;
    }
  }
  return 0xFF;
}

void showMenuCategory(uint32_t now) {
  static const uint8_t labelPages[] PROGMEM = {
      PAGE_STATUS, PAGE_ILLUMINATION, PAGE_PWM, PAGE_KEYS};
  const uint8_t category = menuTreeState & 0x03U;
  const uint8_t labelPage = pgm_read_byte(labelPages + category);
  display.showText(reinterpret_cast<const __FlashStringHelper *>(
      MenuLabels + static_cast<uint8_t>(labelPage << 2)));
  menuLabelEndsAt = now + 650;
}

void moveMenuCategory(bool forward, uint32_t now) {
  uint8_t category = menuTreeState & 0x03U;
  for (uint8_t checked = 0; checked < MenuCategoryCount; ++checked) {
    category = forward
                   ? static_cast<uint8_t>((category + 1U) % MenuCategoryCount)
                   : (category == 0 ? MenuCategoryCount - 1U : category - 1U);
    if (firstConfiguredMenuPage(category) != 0xFF) {
      menuTreeState = static_cast<uint8_t>(MenuCategorySelector | category);
      showMenuCategory(now);
      return;
    }
  }
}
#endif
#endif

void setMenuPage(uint8_t page) {
  menuPage = static_cast<uint8_t>(page % PAGE_COUNT);
#if PCCONTROLLER_MENU_HIERARCHY
  menuTreeState = menuCategory(menuPage);
#endif
  ControllerSettings &settings = settingsStore.values();
  if (settings.saveLastMenuPage() &&
#if PCCONTROLLER_MENU_VISIBILITY
      settings.menuPageVisible(menuPage) &&
#endif
      settings.defaultMenuPage != menuPage) {
    settings.defaultMenuPage = menuPage;
    settingsStore.markDirty(now);
  }
  modeManager.transitionTo(pageToMode(menuPage));
}

void programService(uint32_t now) {
  ProgramMode previous;
  ProgramMode current;
  if (modeManager.consumeTransition(previous, current)) {
    (void)previous;
    modeEnteredAt = now;

    if (isMenuMode(current)) {
      menuPage = modeToPage(current);
      showMenuLabel(now);
      return;
    }

    if (current == MODE_SOUND_EDIT) {
      showSettingsLabel(now);
      return;
    }

    const uint8_t editLabel =
        static_cast<uint8_t>(current) -
        static_cast<uint8_t>(MODE_ILLUMINATION_MODE_EDIT);
    if (editLabel < EditLabelCount) {
      display.showText(reinterpret_cast<const __FlashStringHelper *>(
          EditLabels + static_cast<uint8_t>(editLabel << 2)));
      menuLabelEndsAt = now + 650;
      return;
    }

    switch (current) {
      case MODE_BOOT:
        display.showText(commonText(TextBoot));
        break;
      case MODE_USER_RELAY_CONTROL:
        display.showText(commonText(TextUserRelays));
        menuLabelEndsAt = now + 450;
        break;
      case MODE_MOTION_CONTROL:
        relays.allOff(now);
        display.showText(commonText(TextGo));
        menuLabelEndsAt = now + 450;
        break;
      case MODE_SAVE_PROMPT:
        display.showText(commonText(TextSave));
        break;
      case MODE_FLASH_MESSAGE:
        break;
      case MODE_RF_LEARNING:
        display.showText(commonText(TextLearn));
        break;
      case MODE_FAULT:
        display.showText(commonText(TextError));
        buzzer.error();
        break;
      default:
        break;
    }
    return;
  }

  switch (modeManager.current()) {
    case MODE_BOOT:
      if (static_cast<uint32_t>(now - modeEnteredAt) >= 650) {
        setMenuPage(settingsStore.values().defaultMenuPage);
      }
      break;

    case MODE_RF_LEARNING:
      if (!learningActive) {
        modeManager.transitionTo(modeBeforeLearning);
      } else if (learningEndsAt != 0 && timeReached(now, learningEndsAt)) {
        endLearning(0, -1);
      }
      break;

    case MODE_MOTION_CONTROL:
      if (!relays.motionAllowed()) {
        relays.allOff(now);
        modeManager.transitionTo(MODE_MOTION);
      }
      break;

    case MODE_FLASH_MESSAGE:
      if (timeReached(now, flashMessageEndsAt)) {
        modeManager.transitionTo(editReturnMode);
      }
      break;

    case MODE_FAULT:
    case MODE_STATUS:
    case MODE_VOLTAGE:
    case MODE_CURRENT:
    case MODE_TLED:
    case MODE_TBT:
    case MODE_ILLUMINATION:
    case MODE_BLUETOOTH:
    case MODE_SOUND:
    case MODE_PWM:
    case MODE_RELAY:
    case MODE_KEYS:
    case MODE_USER_PWM:
    case MODE_USER_RELAYS:
    case MODE_MOTION:
    case MODE_RF:
    case MODE_ILLUMINATION_MODE_EDIT:
    case MODE_ILLUMINATION_ON_EDIT:
    case MODE_ILLUMINATION_OFF_EDIT:
    case MODE_SOUND_EDIT:
    case MODE_PWM_MODE_EDIT:
    case MODE_PWM_CHANNEL_EDIT:
    case MODE_PWM_VALUE_EDIT:
    case MODE_RELAY_CHANNEL_EDIT:
    case MODE_RELAY_VALUE_EDIT:
    case MODE_USER_PWM_CHANNEL_EDIT:
    case MODE_USER_PWM_VALUE_EDIT:
    case MODE_USER_RELAY_CHANNEL_EDIT:
    case MODE_USER_RELAY_BEHAVIOR_EDIT:
    case MODE_USER_RELAY_CONTROL:
    case MODE_SAVE_PROMPT:
    case MODE_UNDEFINED:
      break;
  }
}

void menuFeedback(bool fromRemote) {
  statusLeds.playCue(fromRemote ? StatusLedCue::Radio
                                : StatusLedCue::Menu,
                     260);
  if (fromRemote) {
    buzzer.success();
  } else {
    buzzer.beep();
  }
}

uint8_t adjustedBrightness(uint8_t value, bool increase) {
  if (increase) {
    return value > 255 - ILLUMINATION_MENU_STEP
               ? 0
               : static_cast<uint8_t>(value + ILLUMINATION_MENU_STEP);
  }
  return value < ILLUMINATION_MENU_STEP
             ? 255
             : static_cast<uint8_t>(value - ILLUMINATION_MENU_STEP);
}

void adjustIlluminationMode(bool increase, uint32_t now) {
  int8_t mode = static_cast<int8_t>(illumination.mode());
  mode += increase ? 1 : -1;
  if (mode < static_cast<int8_t>(IlluminationMode::Off)) {
    mode = static_cast<int8_t>(IlluminationMode::On);
  } else if (mode > static_cast<int8_t>(IlluminationMode::On)) {
    mode = static_cast<int8_t>(IlluminationMode::Off);
  }
  illumination.setMode(static_cast<IlluminationMode>(mode));
  markIlluminationSettingsChanged(now);
}

void adjustPwmMode(bool increase, uint32_t now) {
  int8_t mode = static_cast<int8_t>(pwm.mode());
  mode += increase ? 1 : -1;
  if (mode < static_cast<int8_t>(PwmTestMode::Off)) {
    mode = static_cast<int8_t>(PwmTestMode::Auto);
  } else if (mode > static_cast<int8_t>(PwmTestMode::Auto)) {
    mode = static_cast<int8_t>(PwmTestMode::Off);
  }
  pwm.setMode(static_cast<PwmTestMode>(mode), now);
  settingsStore.values().pwmBootMode = static_cast<uint8_t>(mode);
  if (!editTransactionActive) {
    settingsStore.markDirty(now);
  }
}

bool selectedRelayActive() {
  return (relays.activeRelayMask() & _BV(relayMenuIndex)) != 0;
}

void setSelectedRelay(bool active, uint32_t now) {
  relays.requestRelayForTest(static_cast<uint8_t>(relayMenuIndex + 1), active,
                             now);
}

bool selectedUserRelayActive() {
  return (relays.activeRelayMask() &
          _BV(static_cast<uint8_t>(userRelayMenuIndex + 4))) != 0;
}

void setSelectedUserRelay(bool active, uint32_t now) {
  relays.requestRelayForTest(
      static_cast<uint8_t>(userRelayMenuIndex + 5), active, now);
}

void setSilentMode(bool silent, uint32_t now) {
  settingsStore.values().setSilent(silent);
  if (!editTransactionActive) {
    settingsStore.markDirty(now);
  }
  buzzer.setMuted(silent);
}

uint16_t userPwm12(uint8_t value) {
  return static_cast<uint16_t>(
      static_cast<uint16_t>(value) * 16U + value / 16U);
}

void applyStoredSettings(uint32_t now) {
  const ControllerSettings &settings = settingsStore.values();
  buzzer.setMuted(settings.silent());
  illumination.setMode(
      static_cast<IlluminationMode>(settings.illuminationMode));
  illumination.setOnBrightness(settings.illuminationOnBrightness);
  illumination.setOffBrightness(settings.illuminationOffBrightness);
  display.setBrightness(settings.displayBrightness);
  statusLeds.setBrightness(settings.statusBrightness);
  statusLeds.setReadyColor(settings.statusColor());
  streamPeriodMs = settings.streamPeriodMs;
  relays.setMotionAllowed(motionPolicyAllows());
  relays.setBreakBeforeDirectionMs(settings.motionBreakBeforeDirectionMs());
  if (!relays.motionAllowed()) {
    relays.stopSide(::RelaySide::A, now);
    relays.stopSide(::RelaySide::B, now);
  }
  pwm.setMode(static_cast<PwmTestMode>(settings.pwmBootMode), now);
  if (settings.pwmBootMode ==
      static_cast<uint8_t>(PwmTestMode::Manual)) {
    for (uint8_t channel = 0; channel < 8; ++channel) {
      pwm.setLogical(channel, userPwm12(settings.userPwm[channel]));
    }
  }
}

void beginEditTransaction(ProgramMode returnMode) {
  if (!editTransactionActive) {
    memcpy(editSnapshot, &settingsStore.values(), sizeof(editSnapshot));
    editTransactionActive = true;
  }
  editReturnMode = returnMode;
}

void finishEditTransaction(bool save, uint32_t now) {
  if (!save) {
    memcpy(&settingsStore.values(), editSnapshot, sizeof(editSnapshot));
    applyStoredSettings(now);
  } else {
    settingsStore.markDirty(now);
    settingsStore.saveNow();
  }
  editTransactionActive = false;
  flashMessageSaved = save;
  flashMessageEndsAt = now + 900;
  if (save) {
    buzzer.success();
    statusLeds.playCue(StatusLedCue::Save, 900, now);
  } else {
    buzzer.error();
    statusLeds.playCue(StatusLedCue::Discard, 900, now);
  }
  modeManager.transitionTo(MODE_FLASH_MESSAGE);
}

void handleMenuAction(uint8_t action, bool fromRemote) {
  if (action > MENU_INCREASE) {
    return;
  }

  if (learningActive) {
    if (action == MENU_DECREASE) {
      endLearning(1, 0);
    }
    menuFeedback(fromRemote);
    return;
  }

  // Menu actions can arrive between loop snapshots (UART, RF, or key event),
  // so capture their exact event time before entering modal state logic.
  const uint32_t now = millis();
  if (modeManager.current() == MODE_BOOT ||
      modeManager.current() == MODE_FAULT) {
    menuFeedback(fromRemote);
    return;
  }

#if PCCONTROLLER_MENU_HIERARCHY
  if (menuCategorySelected() && isMenuMode(modeManager.current())) {
    const uint8_t category = menuTreeState & 0x03U;
    if (action == MENU_PREVIOUS || action == MENU_NEXT) {
      moveMenuCategory(action == MENU_NEXT, now);
    } else if (action == MENU_INCREASE) {
      const uint8_t page = firstConfiguredMenuPage(category);
      if (page != 0xFF) {
        setMenuPage(page);
        showMenuLabel(now);
      }
    } else {
      menuTreeState = menuCategory(menuPage);
      showMenuLabel(now);
    }
    menuFeedback(fromRemote);
    return;
  }

  // K3 is consistently Back at a leaf; it opens the page's parent category.
  // This replaces the older, undiscoverable K1+K2 hold gesture.
  if (isMenuMode(modeManager.current()) && action == MENU_DECREASE) {
    menuTreeState = static_cast<uint8_t>(
        MenuCategorySelector | menuCategory(menuPage));
    showMenuCategory(now);
    menuFeedback(fromRemote);
    return;
  }
#endif

  if (modeManager.current() == MODE_KEYS) {
    identifiedKey = static_cast<uint8_t>(action + 1);
    identifiedKeyEndsAt = now + 900;
    menuFeedback(fromRemote);
    return;
  }

  switch (modeManager.current()) {
    case MODE_ILLUMINATION_MODE_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_ILLUMINATION_ON_EDIT);
      } else {
        adjustIlluminationMode(action == MENU_INCREASE, now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_ILLUMINATION_ON_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_ILLUMINATION_MODE_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_ILLUMINATION_OFF_EDIT);
      } else {
        illumination.setOnBrightness(adjustedBrightness(
            illumination.onBrightness(), action == MENU_INCREASE));
        markIlluminationSettingsChanged(now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_ILLUMINATION_OFF_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_ILLUMINATION_ON_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else {
        illumination.setOffBrightness(adjustedBrightness(
            illumination.offBrightness(), action == MENU_INCREASE));
        markIlluminationSettingsChanged(now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_SOUND_EDIT:
      if (action == MENU_PREVIOUS) {
        if (settingsMenuItem == 0) {
          modeManager.transitionTo(MODE_SAVE_PROMPT);
        } else {
          --settingsMenuItem;
          showSettingsLabel(now);
        }
      } else if (action == MENU_NEXT) {
        if (++settingsMenuItem >= SettingsItemCount) {
          settingsMenuItem = static_cast<uint8_t>(SettingsItemCount - 1);
          modeManager.transitionTo(MODE_SAVE_PROMPT);
        } else {
          showSettingsLabel(now);
        }
      } else {
        const bool increase = action == MENU_INCREASE;
        ControllerSettings &settings = settingsStore.values();
        switch (settingsMenuItem) {
          case 0:
            setSilentMode(!increase, now);
            break;
          case 1:
            settings.displayBrightness = static_cast<uint8_t>(
                (settings.displayBrightness + (increase ? 1U : 7U)) & 0x07U);
            display.setBrightness(settings.displayBrightness);
            break;
          case 2:
            settings.statusBrightness =
                adjustedBrightness(settings.statusBrightness, increase);
            statusLeds.setBrightness(settings.statusBrightness);
            break;
          case 3:
            settings.setStatusColor(static_cast<uint8_t>(
                (settings.statusColor() + (increase ? 1U : 4U)) % 5U));
            statusLeds.setReadyColor(settings.statusColor());
            break;
          case 4:
            settings.setVoltageDecimals(static_cast<uint8_t>(
                (settings.voltageDecimals() + (increase ? 1U : 2U)) % 3U));
            break;
          case 5:
            settings.setCurrentDecimals(static_cast<uint8_t>(
                (settings.currentDecimals() + (increase ? 1U : 2U)) % 3U));
            break;
        }
      }
      menuFeedback(fromRemote);
      return;

    case MODE_PWM_MODE_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(pwm.mode() == PwmTestMode::Manual
                                     ? MODE_PWM_CHANNEL_EDIT
                                     : MODE_SAVE_PROMPT);
      } else {
        adjustPwmMode(action == MENU_INCREASE, now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_PWM_CHANNEL_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_PWM_MODE_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_PWM_VALUE_EDIT);
      } else {
        pwm.adjustChannel(action == MENU_INCREASE ? 1 : -1, now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_PWM_VALUE_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_PWM_CHANNEL_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else {
        pwm.adjustValue(action == MENU_INCREASE ? PWM_MENU_STEP
                                                    : -PWM_MENU_STEP,
                            now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_USER_PWM_CHANNEL_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_USER_PWM_VALUE_EDIT);
      } else if (action == MENU_INCREASE) {
        userPwmMenuIndex = static_cast<uint8_t>(
            (userPwmMenuIndex + 1) % 8);
      } else {
        userPwmMenuIndex =
            userPwmMenuIndex == 0
                ? 7
                : static_cast<uint8_t>(userPwmMenuIndex - 1);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_USER_PWM_VALUE_EDIT: {
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_USER_PWM_CHANNEL_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_SAVE_PROMPT);
      } else {
        uint8_t &value =
            settingsStore.values().userPwm[userPwmMenuIndex];
        value = adjustedBrightness(value, action == MENU_INCREASE);
        pwm.setLogical(userPwmMenuIndex, userPwm12(value));
      }
      menuFeedback(fromRemote);
      return;
    }

    case MODE_USER_RELAY_CHANNEL_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_USER_RELAYS);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_USER_RELAY_BEHAVIOR_EDIT);
      } else if (action == MENU_INCREASE) {
        userRelayMenuIndex =
            static_cast<uint8_t>((userRelayMenuIndex + 1) % 4);
      } else {
        userRelayMenuIndex =
            userRelayMenuIndex == 0
                ? 3
                : static_cast<uint8_t>(userRelayMenuIndex - 1);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_USER_RELAY_BEHAVIOR_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_USER_RELAY_CHANNEL_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_USER_RELAY_CONTROL);
      } else {
        userRelayBehavior = action == MENU_INCREASE ? 1 : 0;
      }
      menuFeedback(fromRemote);
      return;

    case MODE_USER_RELAY_CONTROL:
      if (action == MENU_PREVIOUS) {
        setSelectedUserRelay(false, now);
        modeManager.transitionTo(MODE_USER_RELAY_BEHAVIOR_EDIT);
      } else if (action == MENU_NEXT) {
        setSelectedUserRelay(false, now);
        modeManager.transitionTo(MODE_USER_RELAYS);
      } else if (action == MENU_DECREASE) {
        setSelectedUserRelay(false, now);
      } else if (userRelayBehavior == 0) {
        setSelectedUserRelay(!selectedUserRelayActive(), now);
      } else {
        setSelectedUserRelay(true, now);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_MOTION_CONTROL: {
      const uint8_t side = action >= MENU_DECREASE ? 1 : 0;
      const bool reverse =
          action == MENU_NEXT || action == MENU_INCREASE;
      const bool accepted = relays.requestSide(
          static_cast<::RelaySide>(side),
          reverse ? RelayDirection::Reverse : RelayDirection::Forward, true,
          now);
      if (accepted && fromRemote) {
        remoteMomentaryKind = RemoteActionKind::Side;
        remoteMomentaryValue = side;
        remoteMomentaryEndsAt = now + 350;
      }
      menuFeedback(fromRemote);
      return;
    }

    case MODE_SAVE_PROMPT:
      finishEditTransaction(action == MENU_NEXT ||
                                action == MENU_INCREASE,
                            now);
      return;

    case MODE_FLASH_MESSAGE:
      return;

    case MODE_RELAY_CHANNEL_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_RELAY);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_RELAY_VALUE_EDIT);
      } else if (action == MENU_INCREASE) {
        relayMenuIndex = static_cast<uint8_t>((relayMenuIndex + 1) % 8);
      } else {
        relayMenuIndex =
            relayMenuIndex == 0 ? 7 : static_cast<uint8_t>(relayMenuIndex - 1);
      }
      menuFeedback(fromRemote);
      return;

    case MODE_RELAY_VALUE_EDIT:
      if (action == MENU_PREVIOUS) {
        modeManager.transitionTo(MODE_RELAY_CHANNEL_EDIT);
      } else if (action == MENU_NEXT) {
        modeManager.transitionTo(MODE_RELAY);
      } else {
        setSelectedRelay(action == MENU_INCREASE, now);
      }
      menuFeedback(fromRemote);
      return;

    default:
      break;
  }

  switch (action) {
    case MENU_PREVIOUS:
#if PCCONTROLLER_MENU_VISIBILITY
      setMenuPage(nextConfiguredMenuPage(
          menuPage, false
#if PCCONTROLLER_MENU_HIERARCHY
          , menuTreeState
#endif
          ));
#else
      setMenuPage(menuPage == 0 ? PAGE_COUNT - 1 : menuPage - 1);
#endif
      break;
    case MENU_NEXT:
#if PCCONTROLLER_MENU_VISIBILITY
      setMenuPage(nextConfiguredMenuPage(
          menuPage, true
#if PCCONTROLLER_MENU_HIERARCHY
          , menuTreeState
#endif
          ));
#else
      setMenuPage(static_cast<uint8_t>((menuPage + 1) % PAGE_COUNT));
#endif
      break;
    case MENU_DECREASE:
      if (menuPage == PAGE_RELAY) {
        relays.allOff(now);
      }
      break;
    case MENU_INCREASE:
      if (menuPage == PAGE_ILLUMINATION) {
        beginEditTransaction(MODE_ILLUMINATION);
        modeManager.transitionTo(MODE_ILLUMINATION_MODE_EDIT);
      } else if (menuPage == PAGE_SOUND) {
        beginEditTransaction(MODE_SOUND);
        settingsMenuItem = 0;
        modeManager.transitionTo(MODE_SOUND_EDIT);
      } else if (menuPage == PAGE_PWM) {
        beginEditTransaction(MODE_PWM);
        modeManager.transitionTo(MODE_PWM_MODE_EDIT);
      } else if (menuPage == PAGE_RELAY) {
        relays.allOff(now);
        modeManager.transitionTo(MODE_RELAY_CHANNEL_EDIT);
      } else if (menuPage == PAGE_USER_PWM) {
        beginEditTransaction(MODE_USER_PWM);
        settingsStore.values().pwmBootMode =
            static_cast<uint8_t>(PwmTestMode::Manual);
        pwm.setMode(PwmTestMode::Manual, now);
        for (uint8_t channel = 0; channel < 8; ++channel) {
          pwm.setLogical(
              channel,
              userPwm12(settingsStore.values().userPwm[channel]));
        }
        modeManager.transitionTo(MODE_USER_PWM_CHANNEL_EDIT);
      } else if (menuPage == PAGE_USER_RELAYS) {
        modeManager.transitionTo(MODE_USER_RELAY_CHANNEL_EDIT);
      } else if (menuPage == PAGE_MOTION) {
        if (relays.motionAllowed()) {
          modeManager.transitionTo(MODE_MOTION_CONTROL);
        } else {
          buzzer.error();
        }
      } else if (menuPage == PAGE_RF) {
        beginLearning(DEFAULT_LEARNING_SECONDS);
      } else {
        display.showText(commonText(TextError));
        menuLabelEndsAt = now + 650;
        statusLeds.playCue(StatusLedCue::Discard, 650, now);
        buzzer.error();
        return;
      }
      break;
  }
  menuFeedback(fromRemote);
}

void keyPressed(uint8_t bit, void *) {
  if ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0) {
    return;
  }
  handleMenuAction(bit);
}

void keyReleased(uint8_t bit, void *) {
  if ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0) {
    return;
  }
  const uint32_t now = millis();
  if (modeManager.current() == MODE_USER_RELAY_CONTROL &&
      userRelayBehavior != 0 && bit == BoardPins::KeyIncrease) {
    setSelectedUserRelay(false, now);
  }
  if (modeManager.current() == MODE_MOTION_CONTROL) {
    const uint8_t side =
        bit <= BoardPins::KeyNext ? 0 : 1;
    relays.stopSide(static_cast<::RelaySide>(side), now);
  }
}

void keyGesture(uint8_t bit, KeyEvent event, void *) {
  static uint8_t suppressedHoldBit = 0xFF;
  if ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0) {
    appEvents.key(bit, static_cast<uint8_t>(event));
    return;
  }
  // A press already performs the first action immediately. Holding repeats it
  // without delaying ordinary clicks; double-click Previous is a quick Home.
  if (event == KeyEvent::HoldStart &&
      modeManager.current() == MODE_KEYS &&
      (bit == BoardPins::KeyPrevious ||
       bit == BoardPins::KeyNext)) {
    suppressedHoldBit = bit;
#if PCCONTROLLER_MENU_VISIBILITY
    setMenuPage(nextConfiguredMenuPage(
        menuPage, bit == BoardPins::KeyNext
#if PCCONTROLLER_MENU_HIERARCHY
        , menuTreeState
#endif
        ));
#else
    setMenuPage(
        bit == BoardPins::KeyPrevious
            ? (menuPage == 0 ? PAGE_COUNT - 1 : menuPage - 1)
            : static_cast<uint8_t>((menuPage + 1) % PAGE_COUNT));
#endif
  } else if (event == KeyEvent::HoldRelease &&
             bit == suppressedHoldBit) {
    suppressedHoldBit = 0xFF;
  } else if (event == KeyEvent::HoldRepeat &&
             bit != suppressedHoldBit &&
             modeManager.current() != MODE_MOTION_CONTROL &&
             modeManager.current() != MODE_KEYS &&
             !(modeManager.current() == MODE_USER_RELAY_CONTROL &&
               userRelayBehavior != 0)) {
    keyPressed(bit, nullptr);
  } else if (event == KeyEvent::DoubleClick &&
             bit == BoardPins::KeyPrevious) {
    setMenuPage(settingsStore.values().defaultMenuPage);
  }

  appEvents.key(bit, static_cast<uint8_t>(event));
}

void serviceMotionExit(uint32_t now) {
  const bool motionControl = modeManager.current() == MODE_MOTION_CONTROL;
  const bool sideAExit =
      menuKeys[0].isPressed() && menuKeys[1].isPressed();
  const bool sideBExit =
      menuKeys[2].isPressed() && menuKeys[3].isPressed();
  if (!motionControl || (!sideAExit && !sideBExit)) {
    motionExitStartedAt = 0;
    return;
  }
  if (motionControl) {
    relays.allOff(now);
  }
  if (motionExitStartedAt == 0xFFFFFFFFUL) {
    return;
  }
  if (motionExitStartedAt == 0) {
    motionExitStartedAt = now;
  } else if (static_cast<uint32_t>(now - motionExitStartedAt) >=
             KEY_HOLD_START_MS) {
    setMenuPage(PAGE_MOTION);
    buzzer.success();
    motionExitStartedAt = 0xFFFFFFFFUL;
  }
}

void serviceSystemInputs(uint32_t now) {
  systemInputs.update(shiftRegisters.rawInputs(), now);

  bool value;
  if (systemInputs.consumeDoorChange(value)) {
    appEvents.door(value);
    if (settingsStore.values().doorAudioEnabled()) {
      buzzer.beep(45, value ? 1700 : 1100);
    }
    statusLeds.playCue(value ? StatusLedCue::DoorOpen
                             : StatusLedCue::DoorClosed,
                       720, now);
    relays.setMotionAllowed(motionPolicyAllows());
    if (!relays.motionAllowed()) {
      relays.stopSide(::RelaySide::A, now);
      relays.stopSide(::RelaySide::B, now);
      if (modeManager.current() == MODE_MOTION_CONTROL) {
        modeManager.transitionTo(MODE_MOTION);
      }
    }
    if (!value && !editTransactionActive &&
        modeManager.current() != MODE_BOOT &&
        modeManager.current() != MODE_FAULT) {
      if (modeManager.current() == MODE_MOTION_CONTROL) {
        relays.allOff(now);
      }
      setMenuPage(settingsStore.values().defaultMenuPage);
      if (settingsStore.values().saveLastMenuPage()) {
        // A door close is a natural commit point and avoids losing the most
        // recent page if power is removed immediately afterwards.
        settingsStore.saveNow();
      }
    }
  }
  // Clear the per-edge notification; the categorized state below is reported
  // only when it changes, so a blinking LED cannot flood the UART.
  systemInputs.consumeBluetoothEdge(value);

  static bool initialized = false;
  static BluetoothIndicatorState reported = BluetoothIndicatorState::Off;
  const BluetoothIndicatorState current = systemInputs.bluetoothState(now);
  if (!initialized) {
    initialized = true;
    reported = current;
  } else if (reported != current) {
    reported = current;
    appEvents.bluetooth(static_cast<uint8_t>(current));
    statusLeds.playCue(StatusLedCue::Bluetooth, 600, now);
  }
}

void serviceShiftRegisterAndKeys(uint32_t now) {
  if (static_cast<uint32_t>(now - lastShiftPollAt) < SHIFT_POLL_MS) {
    return;
  }
  lastShiftPollAt = now;
  shiftRegisters.service();
  serviceSystemInputs(now);
  for (Key &key : menuKeys) {
    key.update(now);
  }
  serviceMotionExit(now);
}

void showIlluminationMode() {
  display.showText(commonText(pgm_read_byte(
      ModeTextOffsets + static_cast<uint8_t>(illumination.mode()))));
}

void showPwmMode() {
  display.showText(commonText(pgm_read_byte(
      ModeTextOffsets + 3U + static_cast<uint8_t>(pwm.mode()))));
}

void showBluetoothState(uint32_t now) {
  display.showText(commonText(
      TextBluetoothOff +
      (static_cast<uint8_t>(systemInputs.bluetoothState(now)) << 2)));
}

void showPwmChannel() {
  const uint8_t channel = pwm.channel();
  const char modeCharacter =
      pwm.mode() == PwmTestMode::Auto
          ? 'A'
          : (pwm.mode() == PwmTestMode::Manual ? 'M' : 'O');
  char label[5] = {
      modeCharacter,
      '-',
      static_cast<char>('0' + channel / 10),
      static_cast<char>('0' + channel % 10),
      '\0',
  };
  display.showText(label);
}

void serviceDisplay(uint32_t now) {
  if (static_cast<uint32_t>(now - lastDisplayRefreshAt) <
      DISPLAY_REFRESH_MS) {
    return;
  }
  lastDisplayRefreshAt = now;

  const ProgramMode currentMode = modeManager.current();
  if (currentMode == MODE_BOOT || currentMode == MODE_FAULT) {
    return;
  }
  if (learningActive) {
    display.showText(commonText(TextLearn));
    return;
  }
  if (hostSegmentTextActive && hostSegmentTextEndsAt != 0 &&
      timeReached(now, hostSegmentTextEndsAt)) {
    hostSegmentTextActive = false;
  }
  if (hostSegmentTextActive) {
    display.showText(hostSegmentText);
    return;
  }
  if (currentMode == MODE_FLASH_MESSAGE) {
    if (((now / 150U) & 1U) != 0) {
      display.showText(commonText(flashMessageSaved ? TextSave
                                                   : TextDiscard));
    } else {
      display.clear();
    }
    return;
  }
  if (currentMode == MODE_SAVE_PROMPT) {
    display.showText(commonText(((now / 600U) & 1U) != 0
                                    ? TextSave
                                    : TextDiscard));
    return;
  }
#if PCCONTROLLER_MENU_HIERARCHY
  if (menuCategorySelected() && isMenuMode(currentMode)) {
    showMenuCategory(now);
    return;
  }
#endif
  if (!timeReached(now, menuLabelEndsAt)) {
    return;
  }
  if (currentMode >= MODE_ILLUMINATION_MODE_EDIT &&
      currentMode <= MODE_USER_RELAY_BEHAVIOR_EDIT &&
      ((now / 300U) & 1U) == 0) {
    display.clear();
    return;
  }
  // Render ordinary menu pages as explicit early returns. Keeping them out of
  // a second dense AVR jump table prevents the layout-sensitive reset that was
  // reproduced when navigating among otherwise healthy pages.
  if (currentMode == MODE_STATUS) {
    display.showText(commonText(systemInputs.doorOpen() ? TextOpen
                                                       : TextClosed));
    return;
  }
  if (currentMode == MODE_VOLTAGE) {
    if (sensors.supplyMilliVolts == INVALID_I32) {
      display.showUnavailable();
    } else {
      const uint8_t decimals = settingsStore.values().voltageDecimals();
      display.showFixed(
          scaledMilliValue(sensors.supplyMilliVolts, decimals), decimals);
    }
    return;
  }
  if (currentMode == MODE_CURRENT) {
    if (sensors.currentMilliAmps == INVALID_I32) {
      display.showUnavailable();
    } else {
      const uint8_t decimals = settingsStore.values().currentDecimals();
      display.showFixed(
          scaledMilliValue(sensors.currentMilliAmps, decimals), decimals);
    }
    return;
  }
  if (currentMode == MODE_TLED || currentMode == MODE_TBT) {
    const uint8_t index = currentMode == MODE_TLED ? 0 : 1;
    // Temperature text is prepared only when a conversion completes. Keep this
    // path identical to the proven host-text early return and keep arithmetic
    // out of the time-critical TM1637 renderer.
    display.showText(temperatureSegmentText[index]);
    return;
  }
  if (currentMode == MODE_ILLUMINATION) {
    showIlluminationMode();
    return;
  }
  if (currentMode == MODE_BLUETOOTH) {
    showBluetoothState(now);
    return;
  }
  if (currentMode == MODE_SOUND) {
    display.showText(commonText(settingsStore.values().silent()
                                    ? TextMute
                                    : TextSound));
    return;
  }
  if (currentMode == MODE_PWM) {
    if (!pwm.available()) {
      display.showUnavailable();
    } else if (((now / 900UL) & 1U) == 0) {
      showPwmChannel();
    } else {
      display.showInteger(pwm.value());
    }
    return;
  }
  if (currentMode == MODE_RELAY) {
    if (((now / 900UL) & 1U) == 0) {
      const char relayLabel[4] = {
          'r',
          '-',
          static_cast<char>('0' + (relayMenuIndex + 1) / 10),
          static_cast<char>('0' + (relayMenuIndex + 1) % 10),
      };
      display.showText(relayLabel);
    } else {
      display.showText(commonText(selectedRelayActive() ? TextOn : TextOff));
    }
    return;
  }
  if (currentMode == MODE_KEYS) {
    if (!timeReached(now, identifiedKeyEndsAt) && identifiedKey != 0) {
      display.showInteger(identifiedKey);
    } else {
      display.showText(commonText(TextKey));
    }
    return;
  }
  if (currentMode == MODE_USER_PWM) {
    if (((now / 900UL) & 1U) == 0) {
      display.showInteger(static_cast<int32_t>(userPwmMenuIndex + 1));
    } else {
      display.showInteger(settingsStore.values().userPwm[userPwmMenuIndex]);
    }
    return;
  }
  if (currentMode == MODE_USER_RELAYS) {
    display.showInteger(static_cast<int32_t>(
        relays.activeRelayMask() >> 4));
    return;
  }
  if (currentMode == MODE_MOTION) {
    display.showText(commonText(systemInputs.doorOpen() ? TextOpen
                                                       : TextClosed));
    return;
  }
  if (currentMode == MODE_RF) {
    if (learnedRemotes.count() == 0) {
      display.showUnavailable();
    } else {
      display.showInteger(learnedRemotes.count());
    }
    return;
  }

  switch (currentMode) {
    case MODE_ILLUMINATION_MODE_EDIT:
      showIlluminationMode();
      return;
    case MODE_ILLUMINATION_ON_EDIT:
      display.showInteger(illumination.onBrightness());
      return;
    case MODE_ILLUMINATION_OFF_EDIT:
      display.showInteger(illumination.offBrightness());
      return;
    case MODE_SOUND_EDIT:
      if (settingsMenuItem == 0) {
        display.showText(commonText(settingsStore.values().silent()
                                        ? TextMute
                                        : TextSound));
      } else {
        const ControllerSettings &settings = settingsStore.values();
        uint8_t value;
        switch (settingsMenuItem) {
          case 1:
            value = settings.displayBrightness;
            break;
          case 2:
            value = settings.statusBrightness;
            break;
          case 3:
            value = settings.statusColor();
            break;
          case 4:
            value = settings.voltageDecimals();
            break;
          default:
            value = settings.currentDecimals();
            break;
        }
        display.showInteger(value);
      }
      return;
    case MODE_PWM_MODE_EDIT:
      showPwmMode();
      return;
    case MODE_PWM_CHANNEL_EDIT:
      display.showInteger(pwm.channel());
      return;
    case MODE_PWM_VALUE_EDIT:
      display.showInteger(pwm.value());
      return;
    case MODE_RELAY_CHANNEL_EDIT:
      display.showInteger(static_cast<int32_t>(relayMenuIndex + 1));
      return;
    case MODE_RELAY_VALUE_EDIT:
      display.showText(commonText(selectedRelayActive() ? TextOn : TextOff));
      return;
    case MODE_USER_PWM_CHANNEL_EDIT:
      display.showInteger(static_cast<int32_t>(userPwmMenuIndex + 1));
      return;
    case MODE_USER_PWM_VALUE_EDIT:
      display.showInteger(
          settingsStore.values().userPwm[userPwmMenuIndex]);
      return;
    case MODE_USER_RELAY_CHANNEL_EDIT:
      display.showInteger(static_cast<int32_t>(userRelayMenuIndex + 5));
      return;
    case MODE_USER_RELAY_BEHAVIOR_EDIT:
      display.showText(commonText(userRelayBehavior == 0 ? TextToggle
                                                        : TextPush));
      return;
    case MODE_USER_RELAY_CONTROL:
      display.showText(
          commonText(selectedUserRelayActive() ? TextOn : TextOff));
      return;
    case MODE_MOTION_CONTROL:
      display.showInteger(relays.activeRelayMask());
      return;
    default:
      break;
  }

}

// -----------------------------------------------------------------------------
// Native UART protocol
// -----------------------------------------------------------------------------

void sendHello(uint8_t sequence) {
  constexpr uint32_t capabilities =
      (1UL << 0) |  // INA219
      (1UL << 1) |  // two DS18B20 sensors
      (1UL << 2) |  // 16-channel PWM
      (1UL << 3) |  // relay safety controller
      (1UL << 4) |  // 433 MHz RX/TX, learning, and action mapping
      (1UL << 5) |  // TM1637
      (1UL << 6) |  // I2C LCD
      (1UL << 7) |  // addressable LEDs
      (1UL << 8) |  // persistent settings
      (1UL << 9) |  // menu remote control
      (1UL << 10) | // named temperature identities
      (1UL << 12) | // host display text and asynchronous events
      (1UL << 13) | // exact front-panel snapshot
      (1UL << 14) | // host-injected key gestures
      (1UL << 15) | // multi/indefinite RF learning
      (1UL << 16) | // bounded generic I2C transaction lease
#if PCCONTROLLER_ENABLE_MENU_DIRECTORY
      (1UL << 17) | // board-authoritative paged menu directory
#endif
      (1UL << 18) | // host-staged learned-RF record replacement (opcode 0x3F)
      (1UL << 19) | // host-captured front-panel session (DisplayText targets 3/4)
      (1UL << 20) | // status bit 12 means buzzer queue/voice is busy
      (1UL << 21) | // EEPROM-selectable 1/100 ms motion break time
      (1UL << 22) | // MCU-timed events/ACKs and queued macro schema 2
#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
      (1UL << 23) | // persistent visible-mask and stable-ID rank permutation
#endif
      0;
  struct __attribute__((packed)) HelloPayload {
    uint8_t schema;
    uint8_t boardKind;
    uint32_t capabilities;
    uint32_t buildHash;
    uint32_t buildTimestamp;
  } payload = {3, 1, capabilities,
               static_cast<uint32_t>(PCCONTROLLER_BUILD_HASH),
               static_cast<uint32_t>(PCCONTROLLER_BUILD_TIMESTAMP)};
  appProtocol.send(ControllerProtocol::HelloResponse, sequence,
                   reinterpret_cast<const uint8_t *>(&payload),
                   sizeof(payload));
}

void sendTelemetry(uint8_t sequence) {
  TelemetryPayload payload;
  payload.uptimeMs = now;
  payload.sensors = sensors;
  payload.flags = statusFlags();
  payload.rawInputs = systemInputs.rawInputs();
  payload.activeKeys =
      static_cast<uint8_t>(shiftRegisters.activeInputs() & 0x0F);
  payload.relayMask = relays.activeRelayMask();
  payload.menuPage = menuPage;
  payload.mode = static_cast<uint8_t>(modeManager.current());
  payload.doorOpen = systemInputs.doorOpen();
  payload.bluetoothState =
      static_cast<uint8_t>(systemInputs.bluetoothState(payload.uptimeMs));
  payload.pwmMode = static_cast<uint8_t>(pwm.mode());
  payload.pwmChannel = pwm.channel();
  payload.pwmValue = pwm.value();
  payload.lcdAddress = 0; // The host publishes its PC-owned LCD address.
  payload.pwmErrors = pwm.errorCount();
  payload.framingErrors = appProtocol.framingErrors();
  payload.crcErrors = appProtocol.crcErrors();
  payload.resetCause = resetTelemetry.cause();
  payload.resetCount = resetTelemetry.count();
  appProtocol.send(ControllerProtocol::StatusResponse, sequence,
                   reinterpret_cast<const uint8_t *>(&payload),
                   sizeof(payload));
}

void sendSettings(uint8_t sequence) {
  const ControllerSettings &settings = settingsStore.values();
  uint8_t payload[12];
  payload[0] = 2;
  memcpy(payload + 1, &settings, ControllerSettingsPrefixSize);
  payload[8] = static_cast<uint8_t>(settings.streamPeriodMs);
  payload[9] = static_cast<uint8_t>(settings.streamPeriodMs >> 8);
  payload[10] = settings.defaultMenuPage;
  payload[11] = settings.menuFlags;
  appProtocol.send(ControllerProtocol::SettingsResponse, sequence, payload,
                   sizeof(payload));
}

void sendPwmValues(uint8_t sequence) {
  uint8_t payload[34];
  uint8_t index = 0;
  payload[index++] = static_cast<uint8_t>(pwm.mode());
  payload[index++] = pwm.channel();
  for (uint8_t channel = 0; channel < PwmChannels::Count; ++channel) {
    appendU16(payload, index, pwm.logicalValue(channel));
  }
  appProtocol.send(ControllerProtocol::PwmValuesResponse, sequence, payload,
                   index);
}

void sendTemperatureList(uint8_t sequence) {
  uint8_t payload[24] = {1, temperatureAddressCount};
  uint8_t index = 2;
  for (uint8_t addressIndex = 0;
       addressIndex < temperatureAddressCount; ++addressIndex) {
    const uint8_t role = temperatureRole(addressIndex);
    payload[index++] = role; // 0=tLED, 1=tBT
    memcpy(payload + index, temperatureAddresses[addressIndex], 8);
    index += 8;
    appendI16(payload, index, sensors.temperatureCentiC[role]);
  }
  appProtocol.send(ControllerProtocol::TemperatureListResponse, sequence,
                   payload, index);
}

void sendFrontPanel(uint8_t sequence) {
  uint8_t payload[47];
  payload[0] = 2;
  memcpy(payload + 1, display.rawSegments(), 4);
  payload[5] = display.brightness();
  const ProgramMode mode = modeManager.current();
  const bool blinking =
      mode == MODE_FLASH_MESSAGE || mode == MODE_SAVE_PROMPT ||
      (mode >= MODE_ILLUMINATION_MODE_EDIT &&
       mode <= MODE_USER_RELAY_BEHAVIOR_EDIT);
  payload[6] = static_cast<uint8_t>((blinking ? 1U : 0U) |
                                    ((payload[1] | payload[2] | payload[3] |
                                      payload[4]) != 0
                                         ? 2U
                                         : 0U)
#if PCCONTROLLER_MENU_HIERARCHY
                                    | (menuCategorySelected() ? 4U : 0U)
#endif
                                    );
  payload[7] = 0;
  payload[8] = 0;
  memcpy(payload + 9, hostLcdText, sizeof(hostLcdText));
  payload[41] =
      static_cast<uint8_t>(shiftRegisters.activeInputs() & 0x0FU);
  payload[42] = menuPage;
  payload[43] = static_cast<uint8_t>(mode);
  payload[44] = static_cast<uint8_t>(
      ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0 ? 0x80U : 0U) |
      ((hostPanelMeta >> 12) & 0x0FU));
  payload[45] = static_cast<uint8_t>(hostPanelMeta);
  payload[46] = static_cast<uint8_t>((hostPanelMeta >> 8) & 0x0FU);
  appProtocol.send(ControllerProtocol::FrontPanelResponse, sequence, payload,
                   sizeof(payload));
}

#if PCCONTROLLER_ENABLE_MENU_DIRECTORY
void sendMenuList(uint8_t sequence, uint8_t cursor) {
  uint8_t payload[46] = {1, PAGE_COUNT, 0xFF, 0};
  uint8_t index = 4;
  while (cursor < PAGE_COUNT && payload[3] < 7) {
    payload[index++] = cursor;
    payload[index++] = static_cast<uint8_t>(pageToMode(cursor));
    for (uint8_t character = 0; character < 4; ++character) {
      payload[index++] = pgm_read_byte(
          MenuLabels + static_cast<uint8_t>(cursor * 4U + character));
    }
    ++cursor;
    ++payload[3];
  }
  if (cursor < PAGE_COUNT) {
    payload[2] = cursor;
  }
  appProtocol.send(ControllerProtocol::MenuListResponse, sequence, payload,
                   index);
}
#endif

#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
void sendMenuLayout(uint8_t sequence) {
  const ControllerSettings &settings = settingsStore.values();
  uint8_t payload[12] = {
      2, PAGE_COUNT, static_cast<uint8_t>(settings.visibleMenuMask),
      static_cast<uint8_t>(settings.visibleMenuMask >> 8)};
  memcpy(payload + 4, settings.menuOrder, sizeof(settings.menuOrder));
  appProtocol.send(ControllerProtocol::MenuLayoutResponse, sequence, payload,
                   sizeof(payload));
}

bool applyMenuLayout(const uint8_t *payload, uint8_t length, uint32_t now) {
  if (length != 12 || payload[0] != 2 ||
      payload[1] != PAGE_COUNT) {
    return false;
  }
  const uint16_t visibleMask = readU16(payload + 2);
  const uint8_t firstVisible =
      firstVisiblePersistentMenuPage(visibleMask, payload + 4);
  if (firstVisible == 0xFF) {
    return false;
  }

  ControllerSettings &settings = settingsStore.values();
  settings.visibleMenuMask = visibleMask;
  memcpy(settings.menuOrder, payload + 4, sizeof(settings.menuOrder));
  if (!settings.menuPageVisible(settings.defaultMenuPage)) {
    settings.defaultMenuPage = firstVisible;
  }
  if (!settings.menuPageVisible(menuPage)) {
    if (modeManager.current() == MODE_MOTION_CONTROL) {
      relays.allOff(now);
    }
    setMenuPage(firstVisible);
  }
  settingsStore.markDirty(now);
  return true;
}
#endif

// Generic I2C access is deliberately bounded. A short cooperative lease
// pauses firmware-owned polling while the PC reads or writes any bus address.
void transferI2c(uint8_t sequence, const uint8_t *request, uint8_t length,
                 uint32_t now) {
  if (length < 4) {
    appProtocol.sendError(sequence, ControllerProtocol::I2cTransfer,
                          ControllerProtocol::BadPayload);
    return;
  }
  const uint8_t address = request[0];
  const uint8_t leaseSeconds = request[1];
  const uint8_t writeLength = request[2];
  const uint8_t readLength = request[3];
  if (address == 0) {
    i2cLeaseAddress = 0;
    appProtocol.sendAck(sequence, ControllerProtocol::I2cTransfer);
    return;
  }
  if (leaseSeconds > 10 || writeLength > 16 || readLength > 16 ||
      length != static_cast<uint8_t>(4 + writeLength)) {
    appProtocol.sendError(sequence, ControllerProtocol::I2cTransfer,
                          ControllerProtocol::BadPayload);
    return;
  }
  if (leaseSeconds != 0) {
    i2cLeaseAddress = address;
    i2cLeaseUntil = static_cast<uint16_t>(
        now + static_cast<uint16_t>(leaseSeconds) * 1000U);
  }

  uint8_t response[19];
  response[0] = 0;
  response[1] = address;
  response[2] = 0;
  if (writeLength != 0 || readLength == 0) {
    i2cBus.beginTransmission(address);
    i2cBus.write(request + 4, writeLength);
    response[0] = i2cBus.endTransmission(readLength == 0);
    if (response[0] == 0 && writeLength != 0 &&
        (address == 0x27 || address == 0x3F)) {
      hostLcdAddress = address;
    }
  }
  if (response[0] == 0 && readLength != 0) {
    (void)i2cBus.requestFrom(address, readLength);
    while (response[2] < readLength && i2cBus.available() > 0) {
      response[3 + response[2]++] = i2cBus.read();
    }
  }
  appProtocol.send(ControllerProtocol::I2cTransferResponse, sequence, response,
                   static_cast<uint8_t>(3 + response[2]));
}

void sendLearnedRemotes(uint8_t sequence, uint8_t cursor) {
  uint8_t payload[40] = {1, learnedRemotes.count(), 0xFF, 0};
  uint8_t scan = cursor;
  uint8_t index = 4;
  LearnedRemote remote;
  while (scan < RemoteLearningStore::Capacity && payload[3] < 3) {
    if (learnedRemotes.get(scan, remote)) {
      memcpy(payload + index, &remote, sizeof(remote));
      index = static_cast<uint8_t>(index + sizeof(remote));
      ++payload[3];
    }
    ++scan;
  }
  while (scan < RemoteLearningStore::Capacity) {
    if (learnedRemotes.get(scan, remote)) {
      payload[2] = scan;
      break;
    }
    ++scan;
  }
  appProtocol.send(ControllerProtocol::RadioLearnListResponse, sequence,
                   payload, index);
}

bool applySettings(const uint8_t *payload, uint8_t length, uint32_t now) {
  if (length != 12 || payload[0] != 2 || payload[2] > 2 ||
      payload[5] > 7 || payload[7] > 2 || payload[10] >= PAGE_COUNT
#if PCCONTROLLER_MENU_VISIBILITY
      || !settingsStore.values().menuPageVisible(payload[10])
#endif
      ) {
    return false;
  }
  const uint16_t newStreamPeriod = readU16(payload + 8);
  if (newStreamPeriod != 0 && newStreamPeriod < 100) {
    return false;
  }

  ControllerSettings &settings = settingsStore.values();
  memcpy(&settings, payload + 1, ControllerSettingsPrefixSize);
  settings.flags &=
      SettingsFlags::Silent |
      SettingsFlags::SwapTemperatureSensors |
      SettingsFlags::MotionDoorPolicyMask |
      SettingsFlags::DoorAudioDisabled |
      SettingsFlags::RelayAudioDisabled |
      SettingsFlags::ExtendedMotionBreak;
  settings.streamPeriodMs = newStreamPeriod;
  settings.defaultMenuPage = payload[10];
  settings.menuFlags = payload[11];

  applyStoredSettings(now);
  settingsStore.markDirty(now);
  return true;
}

void handleProtocolFrame(const ControllerProtocol::Frame &frame, void *) {
  using namespace ControllerProtocol;
  const uint8_t *payload = frame.payload;
  const uint8_t length = frame.payloadLength;
  const uint32_t now = millis();
  ::now = now;
  lastHostActivityAt = now;
  hostLcdFlags = static_cast<uint8_t>(
      (hostLcdFlags | HOST_SEEN) & ~HOST_LCD_OFFLINE);

  if (!firmwareReady && frame.opcode != Hello) {
    goto busy;
  }
  if (editTransactionActive &&
      (frame.opcode == SetStreamPeriod ||
       frame.opcode == SetSettings ||
#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
       frame.opcode == MenuLayoutSet ||
#endif
       frame.opcode == PwmSet ||
       frame.opcode == PwmMode)) {
    goto busy;
  }
  switch (frame.opcode) {
    case Hello:
      sendHello(frame.sequence);
      return;

    case GetStatus:
      sendTelemetry(frame.sequence);
      return;

    case SetStreamPeriod: {
      const uint16_t period = length == 2 ? readU16(payload) : 0;
      if (length != 2 || (period != 0 && period < 100)) {
        goto badPayload;
      }
      streamPeriodMs = period;
      settingsStore.values().streamPeriodMs = streamPeriodMs;
      settingsStore.markDirty(now);
      goto acknowledged;
    }

    case GetSettings:
      sendSettings(frame.sequence);
      return;

    case SetSettings:
      if (!applySettings(payload, length, now)) {
        goto badPayload;
      }
      goto acknowledged;

    case TemperatureList:
      sendTemperatureList(frame.sequence);
      return;

    case FrontPanelGet:
      sendFrontPanel(frame.sequence);
      return;

    case MenuList:
#if PCCONTROLLER_ENABLE_MENU_DIRECTORY
      if (length != 1 || payload[0] >= PAGE_COUNT) {
        goto badPayload;
      }
      sendMenuList(frame.sequence, payload[0]);
      return;
#else
      goto unsupported;
#endif

    case MenuLayoutGet:
#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
      if (length != 0) {
        goto badPayload;
      }
      sendMenuLayout(frame.sequence);
      return;
#else
      goto unsupported;
#endif

    case MenuLayoutSet:
#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL
      if (!applyMenuLayout(payload, length, now)) {
        goto badPayload;
      }
      goto acknowledged;
#else
      goto unsupported;
#endif

    case I2cTransfer:
      transferI2c(frame.sequence, payload, length, now);
      return;

    case Buzzer:
      if (length != 4) {
        goto badPayload;
      }
      buzzer.beep(readU16(payload + 2), readU16(payload));
      goto acknowledged;

    case PwmSet: {
      const uint16_t value = length == 3 ? readU16(payload + 1) : 0;
      if (length != 3 || payload[0] >= PwmChannels::Count || value > 4095) {
        goto badPayload;
      }
      if (payload[0] < 11 && pwm.mode() == PwmTestMode::Auto) {
        pwm.setMode(PwmTestMode::Manual, now);
      }
      if (!pwm.setLogical(payload[0], value)) {
        goto hardwareUnavailable;
      }
      if (payload[0] < 8) {
        settingsStore.values().userPwm[payload[0]] =
            static_cast<uint8_t>(value >= 4080 ? 255 : (value + 8) / 16);
        settingsStore.values().pwmBootMode =
            static_cast<uint8_t>(PwmTestMode::Manual);
        settingsStore.markDirty(now);
      }
      goto acknowledged;
    }

    case PwmAllOff:
      if (!pwm.tryAllOff()) {
        goto hardwareUnavailable;
      }
      goto acknowledged;

    case PwmMode:
      if (length != 1 || payload[0] > 2) {
        goto badPayload;
      }
      pwm.setMode(static_cast<PwmTestMode>(payload[0]), now);
      settingsStore.values().pwmBootMode = payload[0];
      settingsStore.markDirty(now);
      goto acknowledged;

    case StatusRgb:
      if (length != 4) {
        goto badPayload;
      }
      statusLeds.setBrightness(payload[3]);
      statusLeds.setCustom(payload[0], payload[1], payload[2]);
      goto acknowledged;

    case PwmGet:
      sendPwmValues(frame.sequence);
      return;

    case AddressableLed: {
      // [pixel 0..10, or 0xFF=fill][R][G][B][brightness].
      if (length != 5 ||
          (payload[0] != 0xFF &&
           payload[0] >= AddressableLeds::PixelCount)) {
        goto badPayload;
      }
      const RgbColor color(payload[1], payload[2], payload[3]);
      if (payload[0] == 0xFF) {
        AddressableLeds::fill(color);
      } else {
        AddressableLeds::buffer()[payload[0]] = color;
      }
      AddressableLeds::show();
      goto acknowledged;
    }

    case RadioTransmit:
      if (length != 8 ||
          !transmitRadio(readU32(payload), payload[4], payload[5],
                         readU16(payload + 6))) {
        goto badPayload;
      }
      goto acknowledged;

    case RadioLearnStart:
      if (length != 2) {
        goto badPayload;
      }
      beginLearning(payload[0], payload[1]);
      goto acknowledged;

    case RadioLearnCancel:
      endLearning(1, 0);
      goto acknowledged;

    case RadioLearnClear:
      endLearning(1, 0);
      learnedRemotes.clear();
      goto acknowledged;

    case RadioLearnList:
      if (length != 1 || payload[0] >= RemoteLearningStore::Capacity) {
        goto badPayload;
      }
      sendLearnedRemotes(frame.sequence, payload[0]);
      return;

    case RadioLearnRemove:
      if (length != 1 || !learnedRemotes.remove(payload[0])) {
        goto badPayload;
      }
      goto acknowledged;

    case RadioLearnMap:
      if (length != 4 ||
          !learnedRemotes.map(
              payload[0], static_cast<RemoteActionKind>(payload[1]),
              payload[2], static_cast<RemoteBehavior>(payload[3]))) {
        goto badPayload;
      }
      goto acknowledged;

    case RadioLearnReplace: {
      if (length != sizeof(LearnedRemote)) {
        goto badPayload;
      }
      LearnedRemote remote;
      memcpy(&remote, payload, sizeof(remote));
      if (!learnedRemotes.replace(remote)) {
        goto badPayload;
      }
      goto acknowledged;
    }

    case ControllerProtocol::MenuAction:
      if (length != 1 || payload[0] > MENU_INCREASE) {
        goto badPayload;
      }
      handleMenuAction(payload[0], true);
      goto acknowledged;

    case RemoteKeyGesture:
      if (length != 2 || payload[0] > MENU_INCREASE ||
          payload[1] > static_cast<uint8_t>(KeyEvent::Up)) {
        goto badPayload;
      }
      if (payload[1] == static_cast<uint8_t>(KeyEvent::Down) ||
          payload[1] == static_cast<uint8_t>(KeyEvent::HoldRepeat)) {
        handleMenuAction(payload[0], true);
      } else if (payload[1] == static_cast<uint8_t>(KeyEvent::Up)) {
        keyReleased(payload[0], nullptr);
      } else if (payload[1] == static_cast<uint8_t>(KeyEvent::DoubleClick) &&
                 payload[0] == MENU_PREVIOUS) {
        setMenuPage(settingsStore.values().defaultMenuPage);
      }
      appEvents.key(payload[0], payload[1], InputEventSource::Host);
      goto acknowledged;

    case MenuSetPage:
      if (length != 1 || payload[0] >= PAGE_COUNT) {
        goto badPayload;
      }
      if (modeManager.current() == MODE_MOTION_CONTROL) {
        relays.allOff(now);
      }
      if (editTransactionActive) {
        memcpy(&settingsStore.values(), editSnapshot, sizeof(editSnapshot));
        editTransactionActive = false;
        applyStoredSettings(now);
      }
      setMenuPage(payload[0]);
      goto acknowledged;

    case DisplayText: {
      if (length < 4 || payload[0] > 4 || payload[3] > 40 ||
          length != static_cast<uint8_t>(4 + payload[3]) ||
          (payload[0] == 3 && (payload[3] < 4 || payload[3] > 36)) ||
          (payload[0] == 4 && payload[3] != 0)) {
        goto badPayload;
      }
      const uint8_t target = payload[0];
      const uint16_t duration = readU16(payload + 1);
      const uint8_t textLength = payload[3];
      if (target == 4) {
        releaseHostPanel();
        goto acknowledged;
      }
      if (target == 3) {
        hostLcdFlags |= HOST_PANEL_CAPTURED;
        hostPanelMeta = duration; // high nibble=state, low 12 bits=value
      }
      const uint32_t endsAt = duration == 0 ? 0 : now + duration;
      if (target == 0 || target == 2 || target == 3) {
        hostSegmentTextActive = textLength != 0;
        memset(hostSegmentText, ' ', 4);
        hostSegmentText[4] = '\0';
        const uint8_t copyLength = textLength > 4 ? 4 : textLength;
        if (copyLength != 0) {
          memcpy(hostSegmentText, payload + 4, copyLength);
        }
        hostSegmentTextEndsAt = target == 3 ? 0 : endsAt;
      }
      if (target == 1 || target == 2 || target == 3) {
        memset(hostLcdText, ' ', sizeof(hostLcdText));
        const uint8_t lcdLength =
            target == 3 ? static_cast<uint8_t>(textLength - 4) : textLength;
        memcpy(hostLcdText, payload + (target == 3 ? 8 : 4), lcdLength);
      }
      goto acknowledged;
    }

    case MacroStart:
    case MacroCancel:
    case MacroStep:
      macroPlayback.handle(frame);
      if (macroPlayback.takeSafeStopRequest()) {
        safeStopMacroOutputs();
      }
      return;

    case RelaySet:
      if (length != 2 || payload[0] > 7 || payload[1] > 1) {
        goto badPayload;
      }
      if (!relays.requestRelayForTest(static_cast<uint8_t>(payload[0] + 1),
                                      payload[1] != 0, now)) {
        goto unsafe;
      }
      goto acknowledged;

    case ControllerProtocol::RelaySide:
      if (length != 2 || payload[0] > 1 || payload[1] > 2) {
        goto badPayload;
      }
      if (payload[1] == 0) {
        relays.stopSide(static_cast<::RelaySide>(payload[0]), now);
      } else {
        if (!relays.requestSide(
                static_cast<::RelaySide>(payload[0]),
                payload[1] == 1 ? RelayDirection::Forward
                                : RelayDirection::Reverse,
                true, now)) {
          goto unsafe;
        }
      }
      goto acknowledged;

    case RelayAllOff:
      relays.allOff(now);
      goto acknowledged;

    case Reset:
      if (length != 1 || payload[0] > 1) {
        goto badPayload;
      }
      endLearning(1, 0);
      stopRemoteMomentary(now);
      buzzer.stop();
      safeReset.request(relays, pwm, statusLeds, now);
      goto acknowledged;

    default:
      goto unsupported;
  }

acknowledged:
  appProtocol.sendAck(frame.sequence, frame.opcode);
  return;
badPayload:
  appProtocol.sendError(frame.sequence, frame.opcode, BadPayload);
  return;
hardwareUnavailable:
  appProtocol.sendError(frame.sequence, frame.opcode, HardwareUnavailable);
  return;
unsafe:
  appProtocol.sendError(frame.sequence, frame.opcode, Unsafe);
  return;
busy:
  appProtocol.sendError(frame.sequence, frame.opcode, Busy);
  return;
unsupported:
  appProtocol.sendError(frame.sequence, frame.opcode, Unsupported);
}

// -----------------------------------------------------------------------------
// Arduino lifecycle
// -----------------------------------------------------------------------------

void setup() {
  // The former Wire timeout path caused reproducible live-menu resets. The
  // compact master, startup bus recovery, and this watchdog bound every path.
  wdt_enable(WDTO_2S);
  wdt_reset();
  ::now = millis();
  appProtocol.begin(PCCONTROLLER_UART_BAUD, handleProtocolFrame);
  resetTelemetry.begin();
  // Announce as soon as UART0 is ready. Opening a USB serial adapter often
  // resets the MCU; serving HELLO during initialization prevents the host's
  // first request from being lost behind sensor/LCD setup.
  sendHello(0);
  appProtocol.service();
  wdt_reset();
  const uint32_t now = millis();
  ::now = now;
  shiftRegisters.begin();
  relays.begin(now);
  systemInputs.begin(shiftRegisters.rawInputs(), now);
  buzzer.begin();
  AddressableLeds::begin();
  loadIlluminationSettings();
  const ControllerSettings &settings = settingsStore.values();
  menuPage = settings.defaultMenuPage;
  buzzer.setMuted(settings.silent());
  streamPeriodMs = settings.streamPeriodMs;
  display.begin(settings.displayBrightness);
  display.showText(commonText(TextBoot));

  for (Key &key : menuKeys) {
    key.setPressCallback(keyPressed);
    key.setReleaseCallback(keyReleased);
    key.setEventCallback(keyGesture);
  }
  appProtocol.service();
  wdt_reset();

  const bool i2cReady = prepareI2cBus();
  if (i2cReady) {
    i2cBus.begin();
    // Reset the TWI peripheral if any state remains blocked for 25 ms.
    i2cBus.setWireTimeout(25000UL, true);
    pwmAvailable = pwmDriver.begin();
    if (pwmAvailable) {
      pwmAvailable =
          pwmDriver.setFrequency(
              static_cast<uint16_t>(BoardPins::PwmFrequencyHz)) &&
          normalizePwmMode2();
    }

    ina219Available = ina219.begin();
  }
  pwm.begin(pwmAvailable, now);
  illumination.begin(pwm, systemInputs.doorOpen(), now);
  statusLeds.begin(pwm, settings.statusBrightness, now);
  applyStoredSettings(now);
  appProtocol.service();
  wdt_reset();

  temperatureBus.begin();
  temperatureBus.setWaitForConversion(false);
  discoverTemperatureSensors();
  requestTemperatures(now);
  appProtocol.service();
  wdt_reset();

  learnedRemotes.begin();
  radioTransmitter.enableTransmit(BoardPins::RcTransmit);
  radioReceiver.setReceiveTolerance(70);
  radioReceiver.enableReceive(digitalPinToInterrupt(BoardPins::RcReceive));

  playBootMelody();
  firmwareReady = true;
  appEvents.reset(resetTelemetry.cause(), resetTelemetry.count());
  sendHello(0);
  sendTelemetry(0);
}

void loop() {
  const uint32_t now = millis();
  ::now = now;
  const bool i2cReserved = i2cLeaseActive(now);
  wdt_reset();

  serviceRadio();
  appProtocol.service();
  ControllerProtocol::Frame queuedMacroFrame;
  while (macroPlayback.dequeueDue(queuedMacroFrame)) {
    const uint16_t errors = appProtocol.responseErrors();
    handleProtocolFrame(queuedMacroFrame, nullptr);
    macroPlayback.completeStep(errors == appProtocol.responseErrors());
    wdt_reset();
  }
  if (macroPlayback.takeSafeStopRequest()) {
    safeStopMacroOutputs();
  }
  const bool hostOffline = (hostLcdFlags & HOST_SEEN) != 0 &&
                           static_cast<uint32_t>(now - lastHostActivityAt) >
                               HOST_OFFLINE_MS;
  if (hostOffline && (hostLcdFlags & HOST_LCD_OFFLINE) == 0) {
    if ((hostLcdFlags & HOST_PANEL_CAPTURED) != 0) {
      releaseHostPanel();
    }
    showHostOfflineOnLcd();
    hostLcdFlags |= HOST_LCD_OFFLINE;
  }
  if (macroPlayback.active() &&
      hostOffline) {
    macroPlayback.cancel(false);
    if (macroPlayback.takeSafeStopRequest()) {
      safeStopMacroOutputs();
    }
  }
  serviceShiftRegisterAndKeys(now);
  serviceRemoteMomentary(now);
  serviceTemperatures(now);
  if (!i2cReserved) {
    sampleIna219(now);
  }
  programService(now);

  const bool hot =
      sensors.temperatureCentiC[0] >= HOT_TEMPERATURE_CENTI_C ||
      sensors.temperatureCentiC[1] >= HOT_TEMPERATURE_CENTI_C;
  if (modeManager.current() == MODE_BOOT) {
    if (statusLeds.mode() != StatusLedMode::Boot) {
      statusLeds.setMode(StatusLedMode::Boot, now);
    }
  } else if (modeManager.current() == MODE_FAULT || hostOffline) {
    if (statusLeds.mode() != StatusLedMode::Fault) {
      statusLeds.setMode(StatusLedMode::Fault, now);
    }
  } else if (learningActive) {
    if (statusLeds.mode() != StatusLedMode::Learning) {
      statusLeds.setMode(StatusLedMode::Learning, now);
    }
  } else if (hot) {
    if (statusLeds.mode() != StatusLedMode::Warning) {
      statusLeds.setMode(StatusLedMode::Warning, now);
    }
  } else if (statusLeds.mode() == StatusLedMode::Boot ||
             statusLeds.mode() == StatusLedMode::Learning ||
             statusLeds.mode() == StatusLedMode::Warning ||
             statusLeds.mode() == StatusLedMode::Fault) {
    statusLeds.setMode(StatusLedMode::Ready, now);
  }

  illumination.service(systemInputs.doorOpen(),
                       !i2cReserved && !learningActive, now);
  serviceIlluminationSettings(now);
  if (!i2cReserved && modeManager.current() != MODE_BOOT) {
    pwm.service(now);
  }
  uint8_t pwmChannel;
  if (pwm.consumeAutoChannelChange(pwmChannel)) {
    appEvents.pwmChannel(pwmChannel);
  }
  relays.service(now);
  const uint8_t relayMask = relays.activeRelayMask();
  if (relayMask != lastRelayMask) {
    appEvents.relay(relayMask);
    if (settingsStore.values().relayAudioEnabled() &&
        ((relayMask ^ lastRelayMask) & 0xFAU) != 0) {
      buzzer.beep(35, (relayMask & ~lastRelayMask) != 0 ? 1900 : 1250);
    }
    lastRelayMask = relayMask;
  }
  serviceDisplay(now);
  if (!i2cReserved) {
    statusLeds.service(now);
  }
  taskManager.update(now);
  buzzer.update(now);

  if (streamPeriodMs != 0 &&
      static_cast<uint32_t>(now - lastTelemetryAt) >= streamPeriodMs) {
    lastTelemetryAt = now;
    sendTelemetry(0);
  }

  safeReset.service(relays, pwm, now);
}
