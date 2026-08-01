#include "virtual_board/virtual_board.hpp"

#include <algorithm>
#include <array>
#include <cctype>
#include <cmath>
#include <iomanip>
#include <limits>
#include <sstream>
#include <stdexcept>
#include <utility>

namespace pccontroller::virtual_board {
namespace {

constexpr std::size_t kSettingsAddress = 32;
constexpr std::size_t kSettingsRecordSize = 15;
constexpr std::uint8_t kSettingsVersion = 1;
constexpr std::size_t kLegacyResetCountAddress = 56;
constexpr std::size_t kResetJournalAddress = 192;
constexpr std::size_t kResetJournalSlots = 64;
constexpr std::size_t kResetRecordSize = 6;
constexpr std::uint8_t kResetRecordMarker = 0xA7;
constexpr std::uint8_t kPowerOnResetCause = 1U << 0U;
constexpr std::uint8_t kWatchdogResetCause = 1U << 3U;
constexpr std::uint8_t kResetEventType = 7;
constexpr std::uint8_t kMenuPageCount = 15;
constexpr std::uint8_t kSettingsSilent = 1U << 0U;
constexpr std::uint8_t kSettingsLcd = 1U << 1U;
constexpr std::uint8_t kSettingsSwapTemperature = 1U << 2U;
constexpr std::uint8_t kSaveLastPage = 1U << 0U;

constexpr std::uint16_t kStatusIna219 = 1U << 0U;
constexpr std::uint16_t kStatusPwm = 1U << 1U;
constexpr std::uint16_t kStatusTLed = 1U << 2U;
constexpr std::uint16_t kStatusTBt = 1U << 3U;
constexpr std::uint16_t kStatusRfLearned = 1U << 4U;
constexpr std::uint16_t kStatusRfLearning = 1U << 5U;
constexpr std::uint16_t kStatusStreaming = 1U << 6U;
constexpr std::uint16_t kStatusLcd = 1U << 8U;
constexpr std::uint16_t kStatusSilent = 1U << 9U;
constexpr std::uint16_t kStatusRelayBusy = 1U << 10U;
constexpr std::uint16_t kStatusDoorOpen = 1U << 11U;
constexpr std::uint16_t kStatusMacro = 1U << 12U;

constexpr std::array<std::uint8_t, 8> kTLedRom{
    0x28, 0x4A, 0x11, 0x7C, 0x93, 0x16, 0x03, 0xB2};
constexpr std::array<std::uint8_t, 8> kTBtRom{
    0x28, 0x9D, 0x42, 0x61, 0x74, 0x16, 0x03, 0x6C};

std::uint16_t readU16(const std::vector<std::uint8_t> &payload,
                      std::size_t offset = 0) {
  return static_cast<std::uint16_t>(
      payload[offset] |
      (static_cast<std::uint16_t>(payload[offset + 1]) << 8U));
}

std::uint32_t readU32(const std::vector<std::uint8_t> &payload,
                      std::size_t offset = 0) {
  return static_cast<std::uint32_t>(payload[offset]) |
         (static_cast<std::uint32_t>(payload[offset + 1]) << 8U) |
         (static_cast<std::uint32_t>(payload[offset + 2]) << 16U) |
         (static_cast<std::uint32_t>(payload[offset + 3]) << 24U);
}

void appendU16(std::vector<std::uint8_t> &payload, std::uint16_t value) {
  payload.push_back(static_cast<std::uint8_t>(value));
  payload.push_back(static_cast<std::uint8_t>(value >> 8U));
}

void appendI16(std::vector<std::uint8_t> &payload, std::int16_t value) {
  appendU16(payload, static_cast<std::uint16_t>(value));
}

void appendU32(std::vector<std::uint8_t> &payload, std::uint32_t value) {
  payload.push_back(static_cast<std::uint8_t>(value));
  payload.push_back(static_cast<std::uint8_t>(value >> 8U));
  payload.push_back(static_cast<std::uint8_t>(value >> 16U));
  payload.push_back(static_cast<std::uint8_t>(value >> 24U));
}

void appendI32(std::vector<std::uint8_t> &payload, std::int32_t value) {
  appendU32(payload, static_cast<std::uint32_t>(value));
}

std::uint32_t buildHash() {
  constexpr char identity[] = "VirtualBoard|" __DATE__ "|" __TIME__;
  std::uint32_t hash = 2166136261U;
  for (const char value : identity) {
    if (value == '\0') {
      break;
    }
    hash ^= static_cast<std::uint8_t>(value);
    hash *= 16777619U;
  }
  return hash;
}

std::vector<std::string> fields(const std::string &line) {
  std::istringstream input(line);
  std::vector<std::string> result;
  for (std::string value; input >> value;) {
    result.push_back(std::move(value));
  }
  return result;
}

std::string lower(std::string value) {
  std::transform(value.begin(), value.end(), value.begin(),
                 [](unsigned char character) {
                   return static_cast<char>(std::tolower(character));
                 });
  return value;
}

std::uint64_t parseUnsigned(const std::string &value, std::uint64_t maximum) {
  std::size_t used = 0;
  const std::uint64_t result = std::stoull(value, &used, 0);
  if (used != value.size() || result > maximum) {
    throw std::invalid_argument("numeric value is outside its valid range");
  }
  return result;
}

double parseDecimal(const std::string &value, double minimum,
                    double maximum) {
  std::size_t used = 0;
  const double result = std::stod(value, &used);
  if (used != value.size() || !std::isfinite(result) || result < minimum ||
      result > maximum) {
    throw std::invalid_argument("decimal value is outside its valid range");
  }
  return result;
}

std::string tailAfterCommand(const std::string &line) {
  const std::size_t first = line.find_first_of(" \t");
  if (first == std::string::npos) {
    return {};
  }
  const std::size_t start = line.find_first_not_of(" \t", first);
  return start == std::string::npos ? std::string{} : line.substr(start);
}

std::uint16_t scale8(std::uint8_t value) {
  return static_cast<std::uint16_t>(
      static_cast<unsigned int>(value) * 16U + value / 16U);
}

} // namespace

VirtualBoard::VirtualBoard(ISensors &sensors, IRelays &relays, IPwm &pwm,
                           IAddressableLeds &addressableLeds,
                           IDisplays &displays, IEeprom &eeprom)
    : sensors_(sensors), relays_(relays), pwm_(pwm),
      addressableLeds_(addressableLeds), displays_(displays), eeprom_(eeprom) {
  const TimePoint now = Clock::now();
  startedAt_ = now;
  lastStreamAt_ = now;
  lastPwmStepAt_ = now;
  lastFadeAt_ = now;
  lastRelayTestAt_ = now;
  loadSettings();
  recordReset(kPowerOnResetCause, false);
  menuPage_ = settings_.defaultMenuPage;
  pwm_.setMode(settings_.pwmBootMode);
  pwm_.allOff();
  pwm_.set(12, 4095);
  pwm_.set(14, scale8(settings_.statusBrightness));
  enclosureBrightness_ = settings_.illuminationOffBrightness;
  pwm_.set(11, scale8(enclosureBrightness_));
  updateMenuDisplay();
}

std::vector<wire::Frame> VirtualBoard::connectedFrames() {
  std::lock_guard<std::mutex> lock(mutex_);
  const TimePoint now = Clock::now();
  return {helloFrame(0), statusFrame(0, now)};
}

std::vector<wire::Frame> VirtualBoard::handle(const wire::Frame &request) {
  std::lock_guard<std::mutex> lock(mutex_);
  const auto &payload = request.payload;
  const TimePoint now = Clock::now();
  const auto bad = [&]() {
    return std::vector<wire::Frame>{errorFrame(
        request.sequence, request.opcode, wire::BadPayload, now)};
  };
  const auto ack = [&]() {
    return std::vector<wire::Frame>{
        ackFrame(request.sequence, request.opcode, now)};
  };

  switch (request.opcode) {
  case wire::Hello:
    return payload.empty()
               ? std::vector<wire::Frame>{helloFrame(request.sequence)}
               : bad();
  case wire::GetStatus:
    return payload.empty()
               ? std::vector<wire::Frame>{statusFrame(request.sequence, now)}
               : bad();
  case wire::SetStreamPeriod:
    if (payload.size() != 2 ||
        (readU16(payload) != 0 && readU16(payload) < 100)) {
      return bad();
    }
    settings_.streamPeriodMs = readU16(payload);
    saveSettings();
    lastStreamAt_ = now;
    return ack();
  case wire::GetSettings:
    return payload.empty()
               ? std::vector<wire::Frame>{settingsFrame(request.sequence)}
               : bad();
  case wire::SetSettings:
    if (!applySettings(payload)) {
      return bad();
    }
    saveSettings();
    return ack();
  case wire::TemperatureList:
    if (payload.size() > 1 ||
        (payload.size() == 1 && payload[0] != 1)) {
      return bad();
    }
    return {temperaturesFrame(request.sequence)};

  case wire::Buzzer:
    if (payload.size() != 4) {
      return bad();
    }
    displays_.setBuzzer(readU16(payload), readU16(payload, 2));
    return ack();
  case wire::PwmSet:
    if (payload.size() != 3 || payload[0] >= 16 ||
        readU16(payload, 1) > 4095) {
      return bad();
    }
    if (payload[0] < 11 && pwm_.mode() == 2) {
      pwm_.setMode(1);
    }
    pwm_.select(payload[0]);
    pwm_.set(payload[0], readU16(payload, 1));
    return ack();
  case wire::PwmAllOff:
    if (!payload.empty()) {
      return bad();
    }
    pwm_.allOff();
    return ack();
  case wire::PwmMode:
    if (payload.size() != 1 || payload[0] > 2) {
      return bad();
    }
    pwm_.setMode(payload[0]);
    settings_.pwmBootMode = payload[0];
    saveSettings();
    return ack();
  case wire::StatusRgb:
    if (payload.size() != 4) {
      return bad();
    }
    pwm_.set(13, static_cast<std::uint16_t>(
                     scale8(payload[0]) * payload[3] / 255U));
    pwm_.set(14, static_cast<std::uint16_t>(
                     scale8(payload[1]) * payload[3] / 255U));
    pwm_.set(15, static_cast<std::uint16_t>(
                     scale8(payload[2]) * payload[3] / 255U));
    return ack();
  case wire::PwmGet:
    return payload.empty()
               ? std::vector<wire::Frame>{pwmFrame(request.sequence)}
               : bad();
  case wire::AddressableLed: {
    if (payload.size() != 5 ||
        (payload[0] != 0xFFU &&
         payload[0] >= kAddressableLedPixelCount)) {
      return bad();
    }
    addressableLeds_.setBrightness(payload[4]);
    const AddressableLedColor color{payload[1], payload[2], payload[3]};
    if (payload[0] == 0xFFU) {
      addressableLeds_.fill(color);
    } else {
      addressableLeds_.setPixel(payload[0], color);
    }
    return ack();
  }

  case wire::RadioTransmit:
    if (payload.size() != 8 || readU32(payload) == 0 || payload[4] == 0 ||
        payload[4] > 32 || payload[5] == 0 || payload[5] > 12) {
      return bad();
    }
    return ack();
  case wire::RadioLearnStart:
    if (payload.size() != 1 || payload[0] == 0 || payload[0] > 120) {
      return bad();
    }
    learningActive_ = true;
    learningDeadline_ = now + std::chrono::seconds(payload[0]);
    return ack();
  case wire::RadioLearnCancel:
    if (!payload.empty()) {
      return bad();
    }
    learningActive_ = false;
    return ack();
  case wire::RadioLearnClear:
    if (!payload.empty()) {
      return bad();
    }
    learningActive_ = false;
    remotes_.fill(RemoteEntry{});
    return ack();
  case wire::RadioLearnList:
    if (payload.size() != 1 || payload[0] >= remotes_.size()) {
      return bad();
    }
    return {remotesFrame(request.sequence, payload[0])};
  case wire::RadioLearnRemove:
    if (payload.size() != 1 || payload[0] >= remotes_.size() ||
        !remotes_[payload[0]].used) {
      return bad();
    }
    remotes_[payload[0]] = RemoteEntry{};
    return ack();
  case wire::RadioLearnMap:
    if (payload.size() != 4 || payload[0] >= remotes_.size() ||
        !remotes_[payload[0]].used || payload[1] > 5 || payload[3] > 5) {
      return bad();
    }
    remotes_[payload[0]].actionKind = payload[1];
    remotes_[payload[0]].actionValue = payload[2];
    remotes_[payload[0]].behavior = payload[3];
    return ack();

  case wire::MenuAction:
    if (payload.size() != 1 || payload[0] > 3) {
      return bad();
    }
    if (payload[0] == 0) {
      setMenuPage(menuPage_ == 0 ? kMenuPageCount - 1
                                 : static_cast<std::uint8_t>(menuPage_ - 1));
    } else if (payload[0] == 1) {
      setMenuPage(static_cast<std::uint8_t>((menuPage_ + 1) %
                                            kMenuPageCount));
    }
    return ack();
  case wire::RelaySet:
    if (payload.size() != 2 || payload[0] > 7 || payload[1] > 1) {
      return bad();
    }
    relayTestPeriodMs_ = 0;
    relays_.set(payload[0], payload[1] != 0);
    return ack();
  case wire::RelaySide:
    if (payload.size() != 2 || !relays_.setSide(payload[0], payload[1])) {
      return bad();
    }
    relayTestPeriodMs_ = 0;
    return ack();
  case wire::RelayAllOff:
    if (!payload.empty()) {
      return bad();
    }
    relayTestPeriodMs_ = 0;
    relays_.allOff();
    return ack();
  case wire::RelayTest:
    if (payload.size() != 2 ||
        (readU16(payload) != 0 && readU16(payload) < 250)) {
      return bad();
    }
    relays_.allOff();
    relayTestPeriodMs_ = readU16(payload);
    relayTestIndex_ = 0;
    relayTestOn_ = false;
    lastRelayTestAt_ = now;
    return ack();
  case wire::Reset:
    if (payload.size() != 1 || payload[0] > 1) {
      return bad();
    }
    resetRuntime(now);
    recordReset(kWatchdogResetCause, true);
    return ack();
  case wire::I2cScan:
    return payload.empty()
               ? std::vector<wire::Frame>{i2cFrame(request.sequence)}
               : bad();
  case wire::MenuSetPage:
    if (payload.size() != 1 || payload[0] >= kMenuPageCount) {
      return bad();
    }
    setMenuPage(payload[0]);
    return ack();
  case wire::DisplayText: {
    if (payload.size() < 4 || payload[0] > 2 || payload[3] > 40 ||
        payload.size() != static_cast<std::size_t>(4U + payload[3])) {
      return bad();
    }
    for (std::size_t index = 4; index < payload.size(); ++index) {
      if (payload[index] < 0x20 || payload[index] > 0x7E) {
        return bad();
      }
    }
    const std::string text(payload.begin() + 4, payload.end());
    const std::uint16_t duration = readU16(payload, 1);
    if (payload[0] == 0 || payload[0] == 2) {
      if (text.empty()) {
        updateMenuDisplay();
        segmentDeadlineActive_ = false;
      } else {
        displays_.setSegments(text);
        segmentDeadlineActive_ = duration != 0;
        segmentDeadline_ = now + std::chrono::milliseconds(duration);
      }
    }
    if (payload[0] == 1 || payload[0] == 2) {
      displays_.setLcd(text);
      lcdDeadlineActive_ = duration != 0 && !text.empty();
      lcdDeadline_ = now + std::chrono::milliseconds(duration);
    }
    return ack();
  }
  case wire::MacroStart:
    if (payload.size() != 5 || payload[0] != 2 || readU16(payload, 3) == 0) {
      return bad();
    }
    if (macroState_ == 1 || macroState_ == 2) {
      return {errorFrame(request.sequence, request.opcode, wire::Busy, now)};
    }
    macroState_ = 1;
    macroId_ = payload[1];
    macroOptions_ = payload[2];
    macroTotalSteps_ = readU16(payload, 3);
    macroAcceptedSteps_ = 0;
    macroExecutedSteps_ = 0;
    macroAcceptedBytes_ = 0;
    macroUnderruns_ = 0;
    macroDispatchErrors_ = 0;
    macroStartedAtUs_ = 0;
    macroQueue_.clear();
    macroLastHostActivity_ = now;
    queueMacroEvent();
    return ack();
  case wire::MacroCancel:
    if (payload.size() > 1 ||
        (payload.size() == 1 && payload[0] > 1)) {
      return bad();
    }
    cancelMacro(payload.size() == 1 ? payload[0] != 0
                                    : (macroOptions_ & 1U) != 0,
                true);
    return ack();
  case wire::MacroStep: {
    macroLastHostActivity_ = now;
    if (payload.size() == 1 && payload[0] == 2) {
      return {macroStatusFrame(wire::MacroStatusResponse,
                               request.sequence)};
    }
    if (payload.size() == 1 && payload[0] == 1) {
      if (macroState_ != 1 || !macroRecordReady() ||
          (macroAcceptedSteps_ < macroTotalSteps_ &&
           macroQueue_.size() < 64U)) {
        return bad();
      }
      macroState_ = 2;
      macroStartedAt_ = now;
      macroStartedAtUs_ = deviceMicros(now);
      queueMacroEvent();
      return ack();
    }
    if (payload.size() < 6 || payload[0] != 0 ||
        (macroState_ != 1 && macroState_ != 2) ||
        readU16(payload, 1) != macroAcceptedBytes_ ||
        readU16(payload, 3) < macroAcceptedSteps_ ||
        readU16(payload, 3) > macroTotalSteps_ ||
        macroQueue_.size() + payload.size() - 5U > 127U) {
      return bad();
    }
    const bool wasStarved = !macroRecordReady();
    macroQueue_.insert(macroQueue_.end(), payload.begin() + 5,
                       payload.end());
    macroAcceptedBytes_ = static_cast<std::uint16_t>(
        macroAcceptedBytes_ + payload.size() - 5U);
    macroAcceptedSteps_ = readU16(payload, 3);
    if (wasStarved && macroState_ == 2 && macroRecordReady()) {
      const std::uint32_t due = readU32(macroQueue_);
      if (static_cast<std::int32_t>(deviceMicros(now) -
                                    (macroStartedAtUs_ + due)) >= 0) {
        ++macroUnderruns_;
      }
    }
    return ack();
  }
  case wire::MenuLayoutGet:
    return payload.empty()
               ? std::vector<wire::Frame>{menuLayoutFrame(request.sequence)}
               : bad();
  case wire::MenuLayoutSet:
    if (!applyMenuLayout(payload)) {
      return bad();
    }
    return ack();
  case wire::HostMenuDirectory:
    if (!applyHostMenuDirectory(payload)) {
      return bad();
    }
    return ack();
  case wire::HostMenuContent:
    if (!applyHostMenuContent(payload)) {
      return bad();
    }
    pendingEvents_.push_back(hostMenuStateFrame(0));
    return ack();
  case wire::HostMenuStateGet:
    return payload.empty()
               ? std::vector<wire::Frame>{hostMenuStateFrame(request.sequence)}
               : bad();
  default:
    return {wire::makeError(request.sequence, request.opcode,
                            wire::Unsupported)};
  }
}

std::vector<wire::Frame> VirtualBoard::tick() {
  std::lock_guard<std::mutex> lock(mutex_);
  const TimePoint now = Clock::now();
  serviceAutomation(now);
  std::vector<wire::Frame> output;
  serviceMacro(now, pendingEvents_);
  output.swap(pendingEvents_);
  if (settings_.streamPeriodMs != 0 &&
      now - lastStreamAt_ >=
          std::chrono::milliseconds(settings_.streamPeriodMs)) {
    lastStreamAt_ = now;
    output.push_back(statusFrame(0, now));
  }
  return output;
}

ConsoleResult VirtualBoard::console(const std::string &line) {
  std::lock_guard<std::mutex> lock(mutex_);
  const auto args = fields(line);
  if (args.empty()) {
    return {};
  }
  const std::string command = lower(args[0]);
  try {
    if (command == "help" || command == "?") {
      return {
          "Commands: show | door open|closed|toggle | tled C | tbt C | "
          "bt off|on|blink | voltage V | current mA | key 1..4 "
          "[click|double|hold|repeat|release|down|up] | "
          "relay 1..8 on|off | "
          "pwm 0..15 0..4095 | strip pixel N R G B [BRIGHTNESS] | "
          "strip fill R G B [BRIGHTNESS] | strip clear | "
          "menu 0..14 | hostmenu 0x80..0xEF | segments TEXT | lcd TEXT | "
          "reset [CAUSE] | rflearn CODE BITS PROTOCOL PULSE_US | "
          "rfrecv CODE BITS PROTOCOL PULSE_US | "
          "eeprom path|flush|reset | quit"};
    }
    if (command == "quit" || command == "exit") {
      return {"stopping virtual board", true};
    }
    if (command == "show") {
      return {describeLocked()};
    }
    if (command == "door") {
      if (args.size() != 2) {
        throw std::invalid_argument("usage: door open|closed|toggle");
      }
      const bool previous = sensors_.readings().doorOpen;
      const std::string value = lower(args[1]);
      bool next = previous;
      if (value == "open" || value == "on" || value == "1") {
        next = true;
      } else if (value == "closed" || value == "close" || value == "off" ||
                 value == "0") {
        next = false;
      } else if (value == "toggle") {
        next = !previous;
      } else {
        throw std::invalid_argument("usage: door open|closed|toggle");
      }
      sensors_.setDoorOpen(next);
      if (next != previous) {
        queueEvent({2, static_cast<std::uint8_t>(next)});
        if (!next) {
          setMenuPage(settings_.defaultMenuPage);
        }
      }
      return {std::string("door ") + (next ? "OPEN" : "CLOSED")};
    }
    if (command == "tled" || command == "tbt") {
      if (args.size() != 2) {
        throw std::invalid_argument("usage: tled C | tbt C");
      }
      const auto centi = static_cast<std::int16_t>(
          std::lround(parseDecimal(args[1], -55.0, 125.0) * 100.0));
      if (command == "tled") {
        sensors_.setTLedCentiC(centi);
      } else {
        sensors_.setTBtCentiC(centi);
      }
      std::ostringstream result;
      result << command << '=' << std::fixed << std::setprecision(2)
             << static_cast<double>(centi) / 100.0 << " C";
      return {result.str()};
    }
    if (command == "bt") {
      if (args.size() != 2) {
        throw std::invalid_argument("usage: bt off|on|blink");
      }
      const std::string value = lower(args[1]);
      std::uint8_t state = 0;
      if (value == "on" || value == "1") {
        state = 1;
      } else if (value == "blink" || value == "blinking" || value == "2") {
        state = 2;
      } else if (value != "off" && value != "0") {
        throw std::invalid_argument("usage: bt off|on|blink");
      }
      if (state != sensors_.readings().bluetoothState) {
        sensors_.setBluetoothState(state);
        queueEvent({3, state});
      }
      return {"Bluetooth state=" + std::to_string(state)};
    }
    if (command == "voltage") {
      if (args.size() != 2) {
        throw std::invalid_argument("usage: voltage VOLTS");
      }
      sensors_.setSupplyMilliVolts(static_cast<std::int32_t>(
          std::lround(parseDecimal(args[1], 0.0, 32.0) * 1000.0)));
      return {"supply voltage updated"};
    }
    if (command == "current") {
      if (args.size() != 2) {
        throw std::invalid_argument("usage: current MILLIAMPS");
      }
      sensors_.setCurrentMilliAmps(static_cast<std::int32_t>(
          std::lround(parseDecimal(args[1], -10000.0, 10000.0))));
      return {"current updated"};
    }
    if (command == "key") {
      if (args.size() < 2 || args.size() > 3) {
        throw std::invalid_argument(
            "usage: key 1..4 "
            "[click|double|hold|repeat|release|down|up]");
      }
      const auto key =
          static_cast<std::uint8_t>(parseUnsigned(args[1], 4));
      if (key == 0) {
        throw std::invalid_argument("key number must be 1..4");
      }
      std::uint8_t gesture = 0;
      if (args.size() == 3) {
        const std::string value = lower(args[2]);
        if (value == "double") {
          gesture = 1;
        } else if (value == "hold") {
          gesture = 2;
        } else if (value == "repeat") {
          gesture = 3;
        } else if (value == "release") {
          gesture = 4;
        } else if (value == "down") {
          gesture = 5;
        } else if (value == "up") {
          gesture = 6;
        } else if (value != "click") {
          throw std::invalid_argument("unknown key gesture");
        }
      }
      const std::uint8_t keyMask =
          static_cast<std::uint8_t>(1U << (key - 1U));
      if (gesture == 5) {
        activeKeys_ |= keyMask;
      } else if (gesture == 6) {
        activeKeys_ &= static_cast<std::uint8_t>(~keyMask);
      }
      queueEvent({1, static_cast<std::uint8_t>(key - 1), gesture});
      return {"key event queued"};
    }
    if (command == "relay") {
      if (args.size() != 3) {
        throw std::invalid_argument("usage: relay 1..8 on|off");
      }
      const auto relay =
          static_cast<std::uint8_t>(parseUnsigned(args[1], 8));
      if (relay == 0) {
        throw std::invalid_argument("relay number must be 1..8");
      }
      const std::string value = lower(args[2]);
      if (value != "on" && value != "off" && value != "1" &&
          value != "0") {
        throw std::invalid_argument("usage: relay 1..8 on|off");
      }
      relays_.set(static_cast<std::uint8_t>(relay - 1),
                  value == "on" || value == "1");
      return {"relay mask updated"};
    }
    if (command == "pwm") {
      if (args.size() != 3) {
        throw std::invalid_argument("usage: pwm CHANNEL VALUE");
      }
      const auto channel =
          static_cast<std::uint8_t>(parseUnsigned(args[1], 15));
      const auto value =
          static_cast<std::uint16_t>(parseUnsigned(args[2], 4095));
      pwm_.setMode(1);
      pwm_.select(channel);
      pwm_.set(channel, value);
      return {"PWM value updated"};
    }
    if (command == "strip") {
      constexpr const char *usage =
          "usage: strip pixel N R G B [BRIGHTNESS] | "
          "fill R G B [BRIGHTNESS] | clear";
      if (args.size() == 2 && lower(args[1]) == "clear") {
        addressableLeds_.setBrightness(0);
        addressableLeds_.fill({});
        return {"addressable LED strip cleared"};
      }
      if (args.size() < 5 || args.size() > 7) {
        throw std::invalid_argument(usage);
      }

      const std::string operation = lower(args[1]);
      std::uint8_t pixel = 0xFFU;
      std::size_t colorOffset = 2;
      if (operation == "pixel") {
        if (args.size() != 6 && args.size() != 7) {
          throw std::invalid_argument(usage);
        }
        pixel = static_cast<std::uint8_t>(
            parseUnsigned(args[2], kAddressableLedPixelCount - 1));
        colorOffset = 3;
      } else if (operation == "fill") {
        if (args.size() != 5 && args.size() != 6) {
          throw std::invalid_argument(usage);
        }
      } else {
        throw std::invalid_argument(usage);
      }

      const AddressableLedColor color{
          static_cast<std::uint8_t>(parseUnsigned(args[colorOffset], 255)),
          static_cast<std::uint8_t>(
              parseUnsigned(args[colorOffset + 1], 255)),
          static_cast<std::uint8_t>(
              parseUnsigned(args[colorOffset + 2], 255))};
      const std::uint8_t brightness =
          args.size() == colorOffset + 4
              ? static_cast<std::uint8_t>(
                    parseUnsigned(args[colorOffset + 3], 255))
              : 255;
      addressableLeds_.setBrightness(brightness);
      if (pixel == 0xFFU) {
        addressableLeds_.fill(color);
      } else {
        addressableLeds_.setPixel(pixel, color);
      }

      std::ostringstream result;
      result << "strip "
             << (pixel == 0xFFU
                     ? std::string("fill")
                     : "pixel " + std::to_string(pixel))
             << " RGB=" << static_cast<unsigned>(color.red) << ','
             << static_cast<unsigned>(color.green) << ','
             << static_cast<unsigned>(color.blue)
             << " brightness=" << static_cast<unsigned>(brightness);
      return {result.str()};
    }
    if (command == "menu") {
      if (args.size() != 2) {
        throw std::invalid_argument("usage: menu PAGE");
      }
      setMenuPage(
          static_cast<std::uint8_t>(parseUnsigned(args[1], 14)));
      return {"menu page=" + std::to_string(menuPage_)};
    }
    if (command == "hostmenu") {
      if (args.size() != 2) {
        throw std::invalid_argument("usage: hostmenu NODE_ID");
      }
      const auto id = static_cast<std::uint8_t>(parseUnsigned(args[1], 255));
      bool present = false;
      for (std::uint8_t index = 0; index < hostMenuCount_; ++index) {
        present = present || hostMenuDirectory_[index].id == id;
      }
      if (!present) {
        throw std::invalid_argument("host-menu node is not in the active directory");
      }
      requestHostMenuContent(id, 0, Clock::now());
      return {"host-menu loading node=" + std::to_string(id)};
    }
    if (command == "segments" || command == "lcd") {
      const std::string text = tailAfterCommand(line);
      if (command == "segments") {
        displays_.setSegments(text);
        segmentDeadlineActive_ = false;
      } else {
        displays_.setLcd(text);
        lcdDeadlineActive_ = false;
      }
      return {command + " text updated"};
    }
    if (command == "reset") {
      if (args.size() > 2) {
        throw std::invalid_argument("usage: reset [CAUSE]");
      }
      const std::uint8_t cause =
          args.size() == 2
              ? static_cast<std::uint8_t>(parseUnsigned(args[1], 255))
              : kWatchdogResetCause;
      resetRuntime(Clock::now());
      recordReset(cause, true);
      std::ostringstream result;
      result << "reset event queued; cause=0x" << std::hex
             << static_cast<unsigned>(cause) << std::dec
             << " count=" << resetCount_;
      return {result.str()};
    }
    if (command == "rflearn") {
      if (args.size() != 5) {
        throw std::invalid_argument(
            "usage: rflearn CODE BITS PROTOCOL PULSE_US");
      }
      if (!learningActive_) {
        throw std::invalid_argument(
            "RF learning is not active; send RF_LEARN_START first");
      }
      auto slot = std::find_if(remotes_.begin(), remotes_.end(),
                               [](const RemoteEntry &entry) {
                                 return !entry.used;
                               });
      if (slot == remotes_.end()) {
        throw std::invalid_argument("RF learning store is full");
      }
      const std::uint8_t id =
          static_cast<std::uint8_t>(slot - remotes_.begin());
      slot->used = true;
      slot->id = id;
      slot->code =
          static_cast<std::uint32_t>(parseUnsigned(args[1], 0xFFFFFFFFU));
      slot->bits =
          static_cast<std::uint8_t>(parseUnsigned(args[2], 32));
      slot->protocol =
          static_cast<std::uint8_t>(parseUnsigned(args[3], 12));
      slot->pulseUs =
          static_cast<std::uint16_t>(parseUnsigned(args[4], 65535));
      if (slot->code == 0 || slot->bits == 0 || slot->protocol == 0) {
        throw std::invalid_argument("RF code, bits, and protocol must be nonzero");
      }
      learningActive_ = false;
      queueEvent({5, id});
      return {"learned virtual RF entry " + std::to_string(id)};
    }
    if (command == "rfrecv") {
      if (args.size() != 5) {
        throw std::invalid_argument(
            "usage: rfrecv CODE BITS PROTOCOL PULSE_US");
      }
      const auto code =
          static_cast<std::uint32_t>(parseUnsigned(args[1], 0xFFFFFFFFU));
      const auto bits =
          static_cast<std::uint8_t>(parseUnsigned(args[2], 32));
      const auto protocol =
          static_cast<std::uint8_t>(parseUnsigned(args[3], 12));
      const auto pulseUs =
          static_cast<std::uint16_t>(parseUnsigned(args[4], 65535));
      if (code == 0 || bits == 0 || protocol == 0) {
        throw std::invalid_argument(
            "RF code, bits, and protocol must be nonzero");
      }

      const auto learned =
          std::find_if(remotes_.begin(), remotes_.end(),
                       [code, bits, protocol](const RemoteEntry &entry) {
                         return entry.used && entry.code == code &&
                                entry.bits == bits &&
                                entry.protocol == protocol;
                       });
      const std::uint8_t learnedId =
          learned == remotes_.end() ? 0xFFU : learned->id;
      queueEvent(
          {8,
           static_cast<std::uint8_t>(code),
           static_cast<std::uint8_t>(code >> 8U),
           static_cast<std::uint8_t>(code >> 16U),
           static_cast<std::uint8_t>(code >> 24U),
           bits,
           protocol,
           static_cast<std::uint8_t>(pulseUs),
           static_cast<std::uint8_t>(pulseUs >> 8U),
           learnedId});
      return {"raw RF receive event queued; learned_id=" +
              (learnedId == 0xFFU ? std::string("none")
                                  : std::to_string(learnedId))};
    }
    if (command == "eeprom") {
      if (args.size() != 2) {
        throw std::invalid_argument("usage: eeprom path|flush|reset");
      }
      const std::string action = lower(args[1]);
      if (action == "path") {
        return {eeprom_.path().string()};
      }
      if (action == "flush") {
        eeprom_.flush();
        return {"virtual MCU EEPROM flushed"};
      }
      if (action == "reset") {
        eeprom_.fill(0xFF);
        resetSettings();
        saveSettings();
        return {"virtual MCU EEPROM reset to firmware defaults"};
      }
      throw std::invalid_argument("usage: eeprom path|flush|reset");
    }
    throw std::invalid_argument("unknown command; type help");
  } catch (const std::exception &error) {
    return {std::string("error: ") + error.what()};
  }
}

std::string VirtualBoard::describe() const {
  std::lock_guard<std::mutex> lock(mutex_);
  return describeLocked();
}

void VirtualBoard::noteProtocolErrors(std::size_t framing, std::size_t crc) {
  std::lock_guard<std::mutex> lock(mutex_);
  framingErrors_ = static_cast<std::uint16_t>(std::min<std::size_t>(
      std::numeric_limits<std::uint16_t>::max(),
      static_cast<std::size_t>(framingErrors_) + framing));
  crcErrors_ = static_cast<std::uint16_t>(std::min<std::size_t>(
      std::numeric_limits<std::uint16_t>::max(),
      static_cast<std::size_t>(crcErrors_) + crc));
}

std::uint32_t VirtualBoard::deviceMicros(TimePoint now) const {
  return static_cast<std::uint32_t>(
      std::chrono::duration_cast<std::chrono::microseconds>(now - startedAt_)
          .count());
}

wire::Frame VirtualBoard::ackFrame(std::uint8_t sequence,
                                   std::uint8_t opcode,
                                   TimePoint now) const {
  std::vector<std::uint8_t> payload{opcode, wire::NoError};
  appendU32(payload, deviceMicros(now));
  return {wire::Ack, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::errorFrame(std::uint8_t sequence,
                                     std::uint8_t opcode,
                                     wire::Error error,
                                     TimePoint now) const {
  std::vector<std::uint8_t> payload{opcode,
                                    static_cast<std::uint8_t>(error)};
  if (sequence == 0xFEU) {
    appendU32(payload, deviceMicros(now));
  }
  return {wire::ErrorResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::macroStatusFrame(std::uint8_t opcode,
                                           std::uint8_t sequence) const {
  std::vector<std::uint8_t> payload{6, 2, macroState_, macroId_};
  appendU16(payload, macroAcceptedSteps_);
  appendU16(payload, macroExecutedSteps_);
  appendU16(payload, macroAcceptedBytes_);
  payload.push_back(static_cast<std::uint8_t>(macroQueue_.size()));
  payload.push_back(macroUnderruns_);
  payload.push_back(macroDispatchErrors_);
  appendU32(payload, macroStartedAtUs_);
  appendU16(payload, macroTotalSteps_);
  return {opcode, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::menuLayoutFrame(std::uint8_t sequence) const {
  // Schema 2 packs two stable page IDs per byte; the unused odd tail is 0xF.
  std::vector<std::uint8_t> payload{
      2, kMenuPageCount, static_cast<std::uint8_t>(menuVisibleMask_),
      static_cast<std::uint8_t>(menuVisibleMask_ >> 8U)};
  payload.resize(4U + (kMenuPageCount + 1U) / 2U, 0);
  for (std::size_t rank = 0; rank < menuOrder_.size(); ++rank) {
    const std::size_t index = 4U + rank / 2U;
    if ((rank & 1U) == 0) {
      payload[index] = static_cast<std::uint8_t>(menuOrder_[rank] & 0x0FU);
    } else {
      payload[index] = static_cast<std::uint8_t>(
          payload[index] | static_cast<std::uint8_t>(menuOrder_[rank] << 4U));
    }
  }
  payload.back() = static_cast<std::uint8_t>(payload.back() | 0xF0U);
  return {wire::MenuLayoutResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::hostMenuStateFrame(std::uint8_t sequence) const {
  return {wire::HostMenuStateResponse, sequence,
          {1, hostMenuGeneration_, hostMenuActiveId_, hostMenuPhase_,
           hostMenuAttempt_, hostMenuRevision_}};
}

wire::Frame VirtualBoard::helloFrame(std::uint8_t sequence) const {
  constexpr char name[] = "PCController";
  constexpr char date[] = __DATE__;
  constexpr char time[] = __TIME__;
  constexpr std::uint32_t capabilities =
      0x1FFFU | (1UL << 22U) | (1UL << 23U) | (1UL << 24U);
  std::vector<std::uint8_t> payload{0, 0, 0, 1};
  appendU32(payload, capabilities);
  payload.push_back(static_cast<std::uint8_t>(sizeof(name) - 1U));
  payload.insert(payload.end(), name, name + sizeof(name) - 1U);
  payload.push_back(1);
  appendU32(payload, buildHash());
  payload.insert(payload.end(), date, date + 11);
  payload.insert(payload.end(), time, time + 8);
  return {wire::HelloResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::statusFrame(std::uint8_t sequence,
                                      TimePoint now) const {
  const SensorReadings sensors = sensors_.readings();
  std::uint16_t flags =
      kStatusIna219 | kStatusPwm | kStatusTLed | kStatusTBt | kStatusLcd;
  if (std::any_of(remotes_.begin(), remotes_.end(),
                  [](const RemoteEntry &entry) { return entry.used; })) {
    flags |= kStatusRfLearned;
  }
  if (learningActive_) {
    flags |= kStatusRfLearning;
  }
  if (settings_.streamPeriodMs != 0) {
    flags |= kStatusStreaming;
  }
  if ((settings_.flags & kSettingsSilent) != 0) {
    flags |= kStatusSilent;
  }
  if (relayTestPeriodMs_ != 0) {
    flags |= kStatusRelayBusy;
  }
  if (sensors.doorOpen) {
    flags |= kStatusDoorOpen;
  }
  if (macroState_ == 1 || macroState_ == 2) {
    flags |= kStatusMacro;
  }

  const auto uptime = std::chrono::duration_cast<std::chrono::milliseconds>(
                          now - startedAt_)
                          .count();
  std::uint8_t rawInputs = 0x3FU;
  if (sensors.bluetoothState == 0) {
    rawInputs |= 1U << 6U;
  }
  if (sensors.doorOpen) {
    rawInputs |= 1U << 7U;
  }

  std::vector<std::uint8_t> payload;
  payload.reserve(48);
  appendU32(payload, static_cast<std::uint32_t>(uptime));
  appendI32(payload, sensors.supplyMilliVolts);
  appendI32(payload, sensors.busMilliVolts);
  appendI32(payload, sensors.currentMilliAmps);
  appendI32(payload, sensors.powerMilliWatts);
  appendI16(payload, sensors.tLedCentiC);
  appendI16(payload, sensors.tBtCentiC);
  appendU16(payload, flags);
  payload.push_back(rawInputs);
  payload.push_back(activeKeys_);
  payload.push_back(relays_.mask());
  payload.push_back(menuPage_);
  payload.push_back(static_cast<std::uint8_t>(menuPage_ + 1U));
  payload.push_back(static_cast<std::uint8_t>(sensors.doorOpen));
  payload.push_back(sensors.bluetoothState);
  payload.push_back(pwm_.mode());
  payload.push_back(pwm_.selected());
  appendU16(payload, pwm_.value(pwm_.selected()));
  payload.push_back(0x27);
  payload.push_back(pwmErrors_);
  appendU16(payload, framingErrors_);
  appendU16(payload, crcErrors_);
  payload.push_back(resetCause_);
  appendU32(payload, resetCount_);
  return {wire::StatusResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::settingsFrame(std::uint8_t sequence) const {
  std::vector<std::uint8_t> payload{
      2,
      settings_.flags,
      settings_.illuminationMode,
      settings_.illuminationOnBrightness,
      settings_.illuminationOffBrightness,
      settings_.displayBrightness,
      settings_.statusBrightness,
      settings_.pwmBootMode,
  };
  appendU16(payload, settings_.streamPeriodMs);
  payload.push_back(settings_.defaultMenuPage);
  payload.push_back(settings_.menuFlags);
  return {wire::SettingsResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::pwmFrame(std::uint8_t sequence) const {
  std::vector<std::uint8_t> payload{pwm_.mode(), pwm_.selected()};
  for (const std::uint16_t value : pwm_.values()) {
    appendU16(payload, value);
  }
  return {wire::PwmValuesResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::temperaturesFrame(std::uint8_t sequence) const {
  const SensorReadings values = sensors_.readings();
  std::vector<std::uint8_t> payload{1, 2};
  payload.push_back(0);
  payload.insert(payload.end(), kTLedRom.begin(), kTLedRom.end());
  appendI16(payload, values.tLedCentiC);
  payload.push_back(1);
  payload.insert(payload.end(), kTBtRom.begin(), kTBtRom.end());
  appendI16(payload, values.tBtCentiC);
  return {wire::TemperatureListResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::i2cFrame(std::uint8_t sequence) const {
  return {wire::I2cScanResponse, sequence, {3, 0x27, 0x40, 0x41}};
}

wire::Frame VirtualBoard::remotesFrame(std::uint8_t sequence,
                                       std::uint8_t cursor) const {
  const std::uint8_t total =
      static_cast<std::uint8_t>(std::count_if(
          remotes_.begin(), remotes_.end(),
          [](const RemoteEntry &entry) { return entry.used; }));
  std::vector<std::uint8_t> payload{1, total, 0xFF, 0};
  std::uint8_t scan = cursor;
  while (scan < remotes_.size() && payload[3] < 3) {
    const RemoteEntry &entry = remotes_[scan];
    if (entry.used) {
      payload.push_back(entry.id);
      appendU32(payload, entry.code);
      payload.push_back(entry.bits);
      payload.push_back(entry.protocol);
      appendU16(payload, entry.pulseUs);
      payload.push_back(entry.actionKind);
      payload.push_back(entry.actionValue);
      payload.push_back(entry.behavior);
      ++payload[3];
    }
    ++scan;
  }
  while (scan < remotes_.size()) {
    if (remotes_[scan].used) {
      payload[2] = scan;
      break;
    }
    ++scan;
  }
  return {wire::RadioLearnListResponse, sequence, std::move(payload)};
}

bool VirtualBoard::applySettings(
    const std::vector<std::uint8_t> &payload) {
  const bool schema1 = payload.size() == 10 && payload[0] == 1;
  const bool schema2 = payload.size() == 12 && payload[0] == 2;
  if ((!schema1 && !schema2) || payload[2] > 2 || payload[5] > 7 ||
      payload[7] > 2) {
    return false;
  }
  const std::uint16_t stream = readU16(payload, 8);
  if (stream != 0 && stream < 100) {
    return false;
  }
  if (schema2 &&
      (payload[10] >= kMenuPageCount ||
       (payload[11] & static_cast<std::uint8_t>(~kSaveLastPage)) != 0)) {
    return false;
  }

  settings_.flags =
      payload[1] &
      (kSettingsSilent | kSettingsLcd | kSettingsSwapTemperature);
  settings_.illuminationMode = payload[2];
  settings_.illuminationOnBrightness = payload[3];
  settings_.illuminationOffBrightness = payload[4];
  settings_.displayBrightness = payload[5];
  settings_.statusBrightness = payload[6];
  settings_.pwmBootMode = payload[7];
  settings_.streamPeriodMs = stream;
  if (schema2) {
    settings_.defaultMenuPage = payload[10];
    settings_.menuFlags = payload[11];
  }
  pwm_.setMode(settings_.pwmBootMode);
  return true;
}

bool VirtualBoard::applyMenuLayout(
    const std::vector<std::uint8_t> &payload) {
  if (payload.size() < 4U || payload[1] != kMenuPageCount ||
      (payload[0] != 1 && payload[0] != 2)) {
    return false;
  }
  const std::size_t expected =
      payload[0] == 1 ? 4U + kMenuPageCount
                      : 4U + (kMenuPageCount + 1U) / 2U;
  if (payload.size() != expected ||
      (payload[0] == 2 && (payload.back() >> 4U) != 0x0FU)) {
    return false;
  }
  const std::uint16_t mask = readU16(payload, 2);
  if (mask == 0 || (mask & static_cast<std::uint16_t>(~0x7FFFU)) != 0) {
    return false;
  }
  std::array<bool, kMenuPageCount> seen{};
  std::array<std::uint8_t, kMenuPageCount> order{};
  for (std::size_t rank = 0; rank < order.size(); ++rank) {
    std::uint8_t id = 0;
    if (payload[0] == 1) {
      id = payload[4U + rank];
    } else {
      const std::uint8_t packed = payload[4U + rank / 2U];
      id = (rank & 1U) == 0 ? static_cast<std::uint8_t>(packed & 0x0FU)
                            : static_cast<std::uint8_t>(packed >> 4U);
    }
    if (id >= kMenuPageCount || seen[id]) {
      return false;
    }
    seen[id] = true;
    order[rank] = id;
  }
  menuVisibleMask_ = mask;
  menuOrder_ = order;
  const auto firstVisible = [&]() {
    for (const std::uint8_t id : menuOrder_) {
      if ((menuVisibleMask_ & (std::uint16_t{1} << id)) != 0) {
        return id;
      }
    }
    return std::uint8_t{0};
  };
  const std::uint8_t fallback = firstVisible();
  if ((menuVisibleMask_ & (std::uint16_t{1} << menuPage_)) == 0) {
    menuPage_ = fallback;
  }
  if ((menuVisibleMask_ &
       (std::uint16_t{1} << settings_.defaultMenuPage)) == 0) {
    settings_.defaultMenuPage = fallback;
    saveSettings();
  }
  updateMenuDisplay();
  return true;
}

bool VirtualBoard::applyHostMenuDirectory(
    const std::vector<std::uint8_t> &payload) {
  if (payload.size() < 3 || payload[0] != 1 || payload[2] > 8 ||
      payload.size() != 3U + static_cast<std::size_t>(payload[2]) * 3U) {
    return false;
  }
  const std::uint8_t count = payload[2];
  std::array<HostMenuEntry, 8> entries{};
  std::array<bool, 256> present{};
  for (std::uint8_t index = 0; index < count; ++index) {
    const std::size_t offset = 3U + static_cast<std::size_t>(index) * 3U;
    const HostMenuEntry entry{payload[offset], payload[offset + 1U],
                              payload[offset + 2U]};
    const bool builtin = entry.id <= 0x0EU;
    const bool host = entry.id >= 0x80U && entry.id <= 0xEFU;
    if ((!builtin && !host) || present[entry.id] ||
        (entry.flags & 0x01U) == 0 ||
        ((entry.flags & 0x10U) != 0 &&
         (entry.flags & (0x04U | 0x08U)) != 0) ||
        (builtin && (entry.flags & 0x20U) == 0) ||
        (host && ((entry.flags & 0x40U) == 0 ||
                  (entry.flags & 0x20U) != 0))) {
      return false;
    }
    present[entry.id] = true;
    entries[index] = entry;
  }
  for (std::uint8_t index = 0; index < count; ++index) {
    std::array<bool, 256> seen{};
    seen[entries[index].id] = true;
    std::uint8_t parent = entries[index].parent;
    while (parent != 0xFFU && !(parent >= 0x70U && parent <= 0x73U)) {
      if (!present[parent] || seen[parent]) {
        return false;
      }
      seen[parent] = true;
      bool found = false;
      for (std::uint8_t candidate = 0; candidate < count; ++candidate) {
        if (entries[candidate].id == parent) {
          parent = entries[candidate].parent;
          found = true;
          break;
        }
      }
      if (!found) {
        return false;
      }
    }
  }
  hostMenuDirectory_ = entries;
  hostMenuCount_ = count;
  hostMenuGeneration_ = payload[1];
  bool activeExists = hostMenuActiveId_ == 0xFFU;
  for (std::uint8_t index = 0; index < count; ++index) {
    activeExists = activeExists || entries[index].id == hostMenuActiveId_;
  }
  if (!activeExists) {
    hostMenuActiveId_ = 0xFFU;
    hostMenuPhase_ = 0;
    hostMenuAttempt_ = 0;
    hostMenuRequestActive_ = false;
    updateMenuDisplay();
  }
  return true;
}

bool VirtualBoard::applyHostMenuContent(
    const std::vector<std::uint8_t> &payload) {
  if (payload.size() != 43 || payload[0] != 1 ||
      payload[1] != hostMenuGeneration_ ||
      (payload[5] > 7 && payload[5] != 0xFFU) || payload[6] > 3) {
    return false;
  }
  bool present = false;
  for (std::uint8_t index = 0; index < hostMenuCount_; ++index) {
    present = present || hostMenuDirectory_[index].id == payload[2];
  }
  if (!present) {
    return false;
  }
  for (std::size_t index = 7; index < payload.size(); ++index) {
    if (payload[index] < 0x20U || payload[index] > 0x7EU) {
      return false;
    }
  }
  hostMenuActiveId_ = payload[2];
  hostMenuPhase_ = 2;
  hostMenuAttempt_ = 0;
  hostMenuRevision_ = payload[3];
  hostMenuRequestActive_ = false;
  displays_.setSegments(std::string(payload.begin() + 7,
                                    payload.begin() + 11));
  displays_.setLcd(std::string(payload.begin() + 11, payload.end()));
  return true;
}

void VirtualBoard::requestHostMenuContent(std::uint8_t id,
                                          std::uint8_t reason,
                                          TimePoint now) {
  hostMenuActiveId_ = id;
  hostMenuPhase_ = 1;
  hostMenuAttempt_ = 0;
  hostMenuRequestedAt_ = now;
  hostMenuRequestActive_ = true;
  if (id <= 0x0EU) {
    // Built-ins retain their flash-resident rendering while an override loads.
    menuPage_ = id;
    updateMenuDisplay();
  } else {
    displays_.setSegments("----");
    displays_.setLcd("Loading menu...\nPlease wait");
  }
  pendingEvents_.push_back(
      {wire::HostMenuContentRequest,
       0,
       {1, hostMenuGeneration_, id, reason, hostMenuAttempt_}});
}

void VirtualBoard::loadSettings() {
  if (eeprom_.size() < kSettingsAddress + kSettingsRecordSize) {
    throw std::runtime_error("virtual EEPROM is too small for settings");
  }
  std::array<std::uint8_t, kSettingsRecordSize> record{};
  for (std::size_t index = 0; index < record.size(); ++index) {
    record[index] = eeprom_.read(kSettingsAddress + index);
  }
  if (record[0] != 0x43 || record[1] != 0x50 ||
      record[2] != kSettingsVersion ||
      record.back() != wire::crc8(record.data(), record.size() - 1U)) {
    resetSettings();
    saveSettings();
    return;
  }
  std::vector<std::uint8_t> payload{
      2, record[3], record[4], record[5], record[6], record[7], record[8],
      record[9], record[10], record[11], record[12], record[13]};
  if (!applySettings(payload)) {
    resetSettings();
    saveSettings();
  }
}

void VirtualBoard::saveSettings() {
  std::array<std::uint8_t, kSettingsRecordSize> record{
      0x43,
      0x50,
      kSettingsVersion,
      settings_.flags,
      settings_.illuminationMode,
      settings_.illuminationOnBrightness,
      settings_.illuminationOffBrightness,
      settings_.displayBrightness,
      settings_.statusBrightness,
      settings_.pwmBootMode,
      static_cast<std::uint8_t>(settings_.streamPeriodMs),
      static_cast<std::uint8_t>(settings_.streamPeriodMs >> 8U),
      settings_.defaultMenuPage,
      settings_.menuFlags,
      0,
  };
  record.back() = wire::crc8(record.data(), record.size() - 1U);
  for (std::size_t index = 0; index < record.size(); ++index) {
    eeprom_.update(kSettingsAddress + index, record[index]);
  }
  eeprom_.flush();
}

void VirtualBoard::resetSettings() { settings_ = Settings{}; }

void VirtualBoard::recordReset(std::uint8_t cause, bool emitEvent) {
  if (eeprom_.size() <
      kResetJournalAddress + kResetJournalSlots * kResetRecordSize) {
    throw std::runtime_error(
        "virtual EEPROM is too small for the reset journal");
  }

  const auto recordChecksum = [](std::uint32_t count) {
    const std::array<std::uint8_t, 5> input{
        0x1F,
        static_cast<std::uint8_t>(count),
        static_cast<std::uint8_t>(count >> 8U),
        static_cast<std::uint8_t>(count >> 16U),
        static_cast<std::uint8_t>(count >> 24U),
    };
    return wire::crc8(input.data(), input.size());
  };
  const auto readCount = [this](std::size_t address) {
    return static_cast<std::uint32_t>(eeprom_.read(address)) |
           (static_cast<std::uint32_t>(eeprom_.read(address + 1U)) << 8U) |
           (static_cast<std::uint32_t>(eeprom_.read(address + 2U)) << 16U) |
           (static_cast<std::uint32_t>(eeprom_.read(address + 3U)) << 24U);
  };

  bool found = false;
  std::size_t newestSlot = 0;
  for (std::size_t slot = 0; slot < kResetJournalSlots; ++slot) {
    const std::size_t address =
        kResetJournalAddress + slot * kResetRecordSize;
    const std::uint32_t count = readCount(address);
    const std::uint8_t checksum = eeprom_.read(address + 4U);
    const std::uint8_t marker = eeprom_.read(address + 5U);
    const bool valid =
        marker == kResetRecordMarker && count != 0 &&
        count != std::numeric_limits<std::uint32_t>::max() &&
        checksum == recordChecksum(count);
    const std::uint32_t difference = count - resetCount_;
    if (valid &&
        (!found ||
         (difference != 0 && difference < (std::uint32_t{1} << 31U)))) {
      found = true;
      newestSlot = slot;
      resetCount_ = count;
    }
  }

  if (!found) {
    resetCount_ = readCount(kLegacyResetCountAddress);
    if (resetCount_ == std::numeric_limits<std::uint32_t>::max()) {
      resetCount_ = 0;
    }
  }
  resetCount_ =
      resetCount_ == std::numeric_limits<std::uint32_t>::max()
          ? 1
          : resetCount_ + 1U;
  resetCause_ = cause;

  const std::size_t nextSlot =
      found ? (newestSlot + 1U) % kResetJournalSlots : 0;
  const std::size_t address =
      kResetJournalAddress + nextSlot * kResetRecordSize;
  eeprom_.update(address + 5U, 0);
  eeprom_.update(address, static_cast<std::uint8_t>(resetCount_));
  eeprom_.update(address + 1U,
                 static_cast<std::uint8_t>(resetCount_ >> 8U));
  eeprom_.update(address + 2U,
                 static_cast<std::uint8_t>(resetCount_ >> 16U));
  eeprom_.update(address + 3U,
                 static_cast<std::uint8_t>(resetCount_ >> 24U));
  eeprom_.update(address + 4U, recordChecksum(resetCount_));
  eeprom_.update(address + 5U, kResetRecordMarker);
  eeprom_.flush();

  if (emitEvent) {
    queueEvent({
        kResetEventType,
        resetCause_,
        static_cast<std::uint8_t>(resetCount_),
        static_cast<std::uint8_t>(resetCount_ >> 8U),
        static_cast<std::uint8_t>(resetCount_ >> 16U),
        static_cast<std::uint8_t>(resetCount_ >> 24U),
    });
  }
}

void VirtualBoard::resetRuntime(TimePoint now) {
  relays_.allOff();
  pwm_.allOff();
  pwm_.setMode(settings_.pwmBootMode);
  pwm_.set(12, 4095);
  learningActive_ = false;
  relayTestPeriodMs_ = 0;
  macroState_ = 0;
  macroQueue_.clear();
  macroAcceptedSteps_ = 0;
  macroExecutedSteps_ = 0;
  macroAcceptedBytes_ = 0;
  menuPage_ = settings_.defaultMenuPage;
  startedAt_ = now;
  lastStreamAt_ = now;
  lastPwmStepAt_ = now;
  lastFadeAt_ = now;
  segmentDeadlineActive_ = false;
  lcdDeadlineActive_ = false;
  updateMenuDisplay();
}

void VirtualBoard::setMenuPage(std::uint8_t page) {
  menuPage_ = static_cast<std::uint8_t>(page % kMenuPageCount);
  if ((settings_.menuFlags & kSaveLastPage) != 0 &&
      settings_.defaultMenuPage != menuPage_) {
    settings_.defaultMenuPage = menuPage_;
    saveSettings();
  }
  if (macroState_ != 1 && macroState_ != 2 && !segmentDeadlineActive_) {
    updateMenuDisplay();
  }
}

void VirtualBoard::updateMenuDisplay() {
  static constexpr std::array<const char *, kMenuPageCount> labels{
      "STAT", "VOLT", "CURR", "tLED", "t-bt", "LItE", "bt  ",
      "Snd ", "PWM ", "rELY", "KEY ", "uPWM", "r5-8", "Go  ",
      "LErn"};
  displays_.setSegments(labels[menuPage_]);
}

void VirtualBoard::cancelMacro(bool keepOutputs, bool emitEvent) {
  if (macroState_ != 1 && macroState_ != 2) {
    return;
  }
  if (!keepOutputs) {
    relays_.allOff();
    for (std::uint8_t channel = 0; channel < 11; ++channel) {
      pwm_.set(channel, 0);
    }
    queueEvent({10, relays_.mask()});
  }
  macroState_ = 3;
  macroQueue_.clear();
  updateMenuDisplay();
  if (emitEvent) {
    queueMacroEvent();
  }
}

bool VirtualBoard::macroRecordReady() const {
  return macroQueue_.size() >= 6U &&
         macroQueue_.size() >=
             static_cast<std::size_t>(6U + macroQueue_[5]);
}

void VirtualBoard::queueMacroEvent() {
  queueEvent(macroStatusFrame(wire::Event, 0).payload);
}

bool VirtualBoard::executeQueuedCommand(
    std::uint8_t opcode, const std::vector<std::uint8_t> &payload,
    TimePoint now) {
  switch (opcode) {
  case wire::Buzzer:
    if (payload.size() != 4) {
      return false;
    }
    displays_.setBuzzer(readU16(payload), readU16(payload, 2));
    return true;
  case wire::PwmSet:
    if (payload.size() != 3 || payload[0] >= 16 ||
        readU16(payload, 1) > 4095) {
      return false;
    }
    if (payload[0] < 11) {
      pwm_.setMode(1);
    }
    pwm_.select(payload[0]);
    pwm_.set(payload[0], readU16(payload, 1));
    return true;
  case wire::PwmAllOff:
    if (!payload.empty()) {
      return false;
    }
    for (std::uint8_t channel = 0; channel < 11; ++channel) {
      pwm_.set(channel, 0);
    }
    return true;
  case wire::PwmMode:
    if (payload.size() != 1 || payload[0] > 2) {
      return false;
    }
    pwm_.setMode(payload[0]);
    return true;
  case wire::StatusRgb:
    if (payload.size() != 4) {
      return false;
    }
    pwm_.set(13, static_cast<std::uint16_t>(
                     scale8(payload[0]) * payload[3] / 255U));
    pwm_.set(14, static_cast<std::uint16_t>(
                     scale8(payload[1]) * payload[3] / 255U));
    pwm_.set(15, static_cast<std::uint16_t>(
                     scale8(payload[2]) * payload[3] / 255U));
    return true;
  case wire::AddressableLed: {
    if (payload.size() != 5 ||
        (payload[0] != 0xFFU && payload[0] >= kAddressableLedPixelCount)) {
      return false;
    }
    addressableLeds_.setBrightness(payload[4]);
    const AddressableLedColor color{payload[1], payload[2], payload[3]};
    if (payload[0] == 0xFFU) {
      addressableLeds_.fill(color);
    } else {
      addressableLeds_.setPixel(payload[0], color);
    }
    return true;
  }
  case wire::RadioTransmit:
    return payload.size() == 8 && readU32(payload) != 0 &&
           payload[4] != 0 && payload[4] <= 32 && payload[5] != 0 &&
           payload[5] <= 12;
  case wire::MenuAction:
    if (payload.size() != 1 || payload[0] > 3) {
      return false;
    }
    if (payload[0] == 0) {
      setMenuPage(static_cast<std::uint8_t>(
          (menuPage_ + kMenuPageCount - 1U) % kMenuPageCount));
    } else if (payload[0] == 1) {
      setMenuPage(static_cast<std::uint8_t>(
          (menuPage_ + 1U) % kMenuPageCount));
    }
    return true;
  case wire::RelaySet:
    if (payload.size() != 2 || payload[0] >= 8 || payload[1] > 1) {
      return false;
    }
    relays_.set(payload[0], payload[1] != 0);
    queueEvent({10, relays_.mask()});
    return true;
  case wire::RelaySide: {
    if (payload.size() != 2 || payload[0] > 1 || payload[1] > 2) {
      return false;
    }
    const std::uint8_t direction = static_cast<std::uint8_t>(payload[0] * 2U);
    const std::uint8_t output = static_cast<std::uint8_t>(direction + 1U);
    if (payload[1] == 0) {
      relays_.set(output, false);
    } else {
      relays_.set(output, false);
      relays_.set(direction, payload[1] == 2);
      relays_.set(output, true);
    }
    queueEvent({10, relays_.mask()});
    return true;
  }
  case wire::RelayAllOff:
    if (!payload.empty()) {
      return false;
    }
    relays_.allOff();
    queueEvent({10, relays_.mask()});
    return true;
  case wire::MenuSetPage:
    if (payload.size() != 1 || payload[0] >= kMenuPageCount) {
      return false;
    }
    setMenuPage(payload[0]);
    return true;
  case wire::DisplayText: {
    if (payload.size() < 4 || payload[0] > 2 || payload[3] > 40 ||
        payload.size() != static_cast<std::size_t>(4U + payload[3])) {
      return false;
    }
    const std::string text(payload.begin() + 4, payload.end());
    const std::uint16_t duration = readU16(payload, 1);
    if (payload[0] == 0 || payload[0] == 2) {
      displays_.setSegments(text);
      segmentDeadlineActive_ = duration != 0;
      segmentDeadline_ = now + std::chrono::milliseconds(duration);
    }
    if (payload[0] == 1 || payload[0] == 2) {
      displays_.setLcd(text);
      lcdDeadlineActive_ = duration != 0;
      lcdDeadline_ = now + std::chrono::milliseconds(duration);
    }
    return true;
  }
  default:
    return false;
  }
}

void VirtualBoard::serviceMacro(TimePoint now,
                                std::vector<wire::Frame> &output) {
  if ((macroState_ == 1 || macroState_ == 2) &&
      now - macroLastHostActivity_ > std::chrono::seconds(5)) {
    cancelMacro(false, true);
  }
  while (macroState_ == 2 && macroExecutedSteps_ < macroTotalSteps_) {
    if (!macroRecordReady()) {
      return;
    }
    const std::uint32_t due = readU32(macroQueue_);
    if (static_cast<std::int32_t>(deviceMicros(now) -
                                  (macroStartedAtUs_ + due)) < 0) {
      return;
    }
    const std::uint8_t opcode = macroQueue_[4];
    const std::size_t payloadLength = macroQueue_[5];
    std::vector<std::uint8_t> payload(
        macroQueue_.begin() + 6,
        macroQueue_.begin() + static_cast<std::ptrdiff_t>(6U + payloadLength));
    macroQueue_.erase(
        macroQueue_.begin(),
        macroQueue_.begin() + static_cast<std::ptrdiff_t>(6U + payloadLength));
    ++macroExecutedSteps_;
    const bool succeeded = executeQueuedCommand(opcode, payload, now);
    if (succeeded) {
      output.push_back(ackFrame(0xFEU, opcode, now));
    } else {
      ++macroDispatchErrors_;
      output.push_back(
          errorFrame(0xFEU, opcode, wire::BadPayload, now));
    }
    if (macroExecutedSteps_ == macroTotalSteps_) {
      macroState_ = macroQueue_.empty() ? 4 : 5;
      if (macroState_ == 5) {
        relays_.allOff();
        for (std::uint8_t channel = 0; channel < 11; ++channel) {
          pwm_.set(channel, 0);
        }
      }
      macroQueue_.clear();
      queueMacroEvent();
      return;
    }
    now = Clock::now();
  }
}

void VirtualBoard::queueEvent(
    std::initializer_list<std::uint8_t> payload) {
  queueEvent(std::vector<std::uint8_t>(payload));
}

void VirtualBoard::queueEvent(std::vector<std::uint8_t> payload) {
  if (payload.empty()) {
    return;
  }
  payload[0] |= 0x80U;
  appendU32(payload, deviceMicros(Clock::now()));
  pendingEvents_.push_back({wire::Event, 0, std::move(payload)});
}

void VirtualBoard::serviceAutomation(TimePoint now) {
  if (hostMenuRequestActive_) {
    const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(
                             now - hostMenuRequestedAt_)
                             .count();
    if (elapsed >= 1500) {
      hostMenuRequestActive_ = false;
      hostMenuPhase_ = 3;
      hostMenuAttempt_ = 3;
      if (hostMenuActiveId_ <= 0x0EU) {
        updateMenuDisplay();
      } else {
        displays_.setSegments("Err ");
        displays_.setLcd("Menu unavailable\nHost timeout");
      }
      pendingEvents_.push_back(hostMenuStateFrame(0));
    } else {
      const auto queueRetry = [&]() {
        pendingEvents_.push_back(
            {wire::HostMenuContentRequest,
             0,
             {1, hostMenuGeneration_, hostMenuActiveId_, 3,
              hostMenuAttempt_}});
      };
      if (elapsed >= 250 && hostMenuAttempt_ < 1) {
        hostMenuAttempt_ = 1;
        queueRetry();
      }
      if (elapsed >= 750 && hostMenuAttempt_ < 2) {
        hostMenuAttempt_ = 2;
        queueRetry();
      }
    }
  }
  if (learningActive_ && now >= learningDeadline_) {
    learningActive_ = false;
  }
  if (segmentDeadlineActive_ && now >= segmentDeadline_) {
    segmentDeadlineActive_ = false;
    if (macroState_ != 1 && macroState_ != 2) {
      updateMenuDisplay();
    }
  }
  if (lcdDeadlineActive_ && now >= lcdDeadline_) {
    lcdDeadlineActive_ = false;
    displays_.setLcd("PCController\nVirtual board");
  }

  if (relayTestPeriodMs_ != 0 &&
      now - lastRelayTestAt_ >=
          std::chrono::milliseconds(relayTestPeriodMs_)) {
    lastRelayTestAt_ = now;
    if (relayTestOn_) {
      relays_.set(relayTestIndex_, false);
      relayTestIndex_ = static_cast<std::uint8_t>((relayTestIndex_ + 1U) % 8U);
      relayTestOn_ = false;
    } else {
      relays_.set(relayTestIndex_, true);
      relayTestOn_ = true;
    }
  }

  if (pwm_.mode() == 2 &&
      now - lastPwmStepAt_ >= std::chrono::milliseconds(20)) {
    const auto intervals = std::min<std::int64_t>(
        16, std::chrono::duration_cast<std::chrono::milliseconds>(
                now - lastPwmStepAt_)
                    .count() /
                20);
    lastPwmStepAt_ += std::chrono::milliseconds(intervals * 20);
    const std::uint16_t step =
        static_cast<std::uint16_t>(std::min<std::int64_t>(4095, 128 * intervals));
    const std::uint8_t channel = pwm_.selected() > 10 ? 0 : pwm_.selected();
    pwm_.select(channel);
    const std::uint16_t current = pwm_.value(channel);
    if (pwmRising_) {
      const std::uint16_t next =
          static_cast<std::uint16_t>(std::min<unsigned>(4095, current + step));
      pwm_.set(channel, next);
      if (next == 4095) {
        pwmRising_ = false;
      }
    } else {
      const std::uint16_t next = current > step ? current - step : 0;
      pwm_.set(channel, next);
      if (next == 0) {
        const std::uint8_t nextChannel =
            static_cast<std::uint8_t>((channel + 1U) % 11U);
        pwm_.select(nextChannel);
        pwmRising_ = true;
        queueEvent({4, nextChannel});
      }
    }
  }

  if (now - lastFadeAt_ >= std::chrono::milliseconds(20)) {
    const auto elapsed =
        std::chrono::duration_cast<std::chrono::milliseconds>(now - lastFadeAt_)
            .count();
    const std::uint8_t intervals = static_cast<std::uint8_t>(
        std::min<std::int64_t>(16, elapsed / 20));
    lastFadeAt_ += std::chrono::milliseconds(intervals * 20);
    const SensorReadings sensor = sensors_.readings();
    std::uint8_t target = settings_.illuminationOffBrightness;
    if (settings_.illuminationMode == 2 ||
        (settings_.illuminationMode == 1 && sensor.doorOpen)) {
      target = settings_.illuminationOnBrightness;
    }
    const std::uint16_t distance =
        static_cast<std::uint16_t>(4U * intervals);
    if (enclosureBrightness_ < target) {
      enclosureBrightness_ = static_cast<std::uint8_t>(
          std::min<std::uint16_t>(target, enclosureBrightness_ + distance));
    } else if (enclosureBrightness_ > target) {
      enclosureBrightness_ = static_cast<std::uint8_t>(
          enclosureBrightness_ - target > distance
              ? enclosureBrightness_ - distance
              : target);
    }
    pwm_.set(11, scale8(enclosureBrightness_));
  }
  pwm_.set(12, 4095);
}

std::string VirtualBoard::describeLocked() const {
  const SensorReadings sensors = sensors_.readings();
  const DisplayState displays = displays_.state();
  const AddressableLedState strip = addressableLeds_.state();
  const auto litPixels =
      std::count_if(strip.pixels.begin(), strip.pixels.end(),
                    [](const AddressableLedColor &color) {
                      return color.red != 0 || color.green != 0 ||
                             color.blue != 0;
                    });
  std::ostringstream result;
  result << std::fixed << std::setprecision(2)
         << "door=" << (sensors.doorOpen ? "OPEN" : "CLOSED")
         << " bt=" << static_cast<unsigned>(sensors.bluetoothState)
         << " supply=" << sensors.supplyMilliVolts / 1000.0 << "V"
         << " current=" << sensors.currentMilliAmps << "mA"
         << " tLED=" << sensors.tLedCentiC / 100.0 << "C"
         << " tBT=" << sensors.tBtCentiC / 100.0 << "C"
         << " relays=0x" << std::hex << std::setw(2) << std::setfill('0')
         << static_cast<unsigned>(relays_.mask()) << std::dec
         << " pwm=" << static_cast<unsigned>(pwm_.selected()) << ':'
         << pwm_.value(pwm_.selected())
         << " strip=" << static_cast<unsigned>(strip.brightness)
         << "/255:" << litPixels << '/' << strip.pixels.size()
         << " menu=" << static_cast<unsigned>(menuPage_)
         << " reset=0x" << std::hex
         << static_cast<unsigned>(resetCause_) << std::dec << ':'
         << resetCount_
         << " segments=\"" << displays.segments << "\""
         << " lcd=\"" << displays.lcdLine1 << "|" << displays.lcdLine2 << "\""
         << " eeprom=\"" << eeprom_.path().string() << "\"";
  return result.str();
}

} // namespace pccontroller::virtual_board
