#include "virtual_board/virtual_board.hpp"

#include "Project/MacroAction.h"

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
constexpr std::size_t kSettingsValuesSize = 40;
constexpr std::size_t kSettingsRecordSize = kSettingsValuesSize + 1;
constexpr std::size_t kRemoteHeaderAddress = 80;
constexpr std::size_t kRemoteEntriesAddress = 84;
constexpr std::size_t kRemoteRecordSize = 12;
constexpr std::size_t kResetJournalAddress = 336;
constexpr std::size_t kResetJournalSlots = 64;
constexpr std::size_t kResetRecordSize = 6;
constexpr std::size_t kStatusProfileAddress =
    kResetJournalAddress + kResetJournalSlots * kResetRecordSize;
constexpr std::size_t kStatusProfileCount = 19;
constexpr std::size_t kStatusProfilePayloadSize = 12;
constexpr std::size_t kStatusProfileRecordSize = 13;
constexpr std::uint8_t kResetRecordMarker = 0xA7;
constexpr std::uint8_t kPowerOnResetCause = 1U << 0U;
constexpr std::uint8_t kWatchdogResetCause = 1U << 3U;
constexpr std::uint8_t kResetEventType = 7;
constexpr std::uint8_t kMenuPageCount = 14;
constexpr std::uint16_t kMenuAllPagesMask = 0x3FFFU;
constexpr std::uint8_t kSettingsSilent = 1U << 0U;
constexpr std::uint8_t kSettingsProgramming = 1U << 1U;
constexpr std::uint8_t kSettingsSwapTemperature = 1U << 2U;
constexpr std::uint8_t kSettingsMotionPolicy = 3U << 3U;
constexpr std::uint8_t kSettingsDoorAudioDisabled = 1U << 5U;
constexpr std::uint8_t kSettingsRelayAudioDisabled = 1U << 6U;
constexpr std::uint8_t kSettingsAllowed =
    kSettingsSilent | kSettingsProgramming | kSettingsSwapTemperature |
    kSettingsMotionPolicy | kSettingsDoorAudioDisabled |
    kSettingsRelayAudioDisabled;
constexpr std::uint8_t kPersistMotion = 1U << 0U;
constexpr std::uint8_t kPersistUserRelays = 1U << 1U;
constexpr std::uint8_t kPersistUserPwm = 1U << 2U;
constexpr std::uint8_t kRetainDirectionOnStop = 1U << 3U;
constexpr std::uint8_t kOutputPersistenceAllowed =
    kPersistMotion | kPersistUserRelays | kPersistUserPwm |
    kRetainDirectionOnStop;
constexpr std::uint8_t kSaveLastPage = 1U << 0U;
constexpr std::uint8_t kLearnModeIndefinite = 0;
constexpr std::uint8_t kLearnModeTimer = 1;
constexpr std::uint8_t kMaximumLearningSeconds = 120;
constexpr std::uint8_t kSegmentRepeatMask = 0x03U;
constexpr std::uint8_t kSegmentIntervalWaiting = 0x40U;
constexpr std::uint8_t kSegmentForceScroll = 0x80U;
constexpr std::chrono::milliseconds kHostOfflineAfter{5000};
constexpr std::int16_t kHotTemperatureCentiC = 5000;
constexpr std::array<std::uint8_t, 4> kI2cAddresses{0x27, 0x3F, 0x40,
                                                   0x41};

constexpr std::uint32_t decimal2(char tens, char ones) {
  return (tens == ' ' ? 0U : static_cast<std::uint32_t>(tens - '0') * 10U) +
         static_cast<std::uint32_t>(ones - '0');
}

constexpr std::uint32_t compileMonth(const char *date) {
  if (date[0] == 'J') {
    return date[1] == 'a' ? 1U : (date[2] == 'n' ? 6U : 7U);
  }
  if (date[0] == 'F') {
    return 2U;
  }
  if (date[0] == 'M') {
    return date[2] == 'r' ? 3U : 5U;
  }
  if (date[0] == 'A') {
    return date[1] == 'p' ? 4U : 8U;
  }
  if (date[0] == 'S') {
    return 9U;
  }
  if (date[0] == 'O') {
    return 10U;
  }
  if (date[0] == 'N') {
    return 11U;
  }
  return 12U;
}

// Mirror the AVR's DOS date/time identity using the simulator compile time.
constexpr std::uint32_t packedBuildTimestamp() {
  constexpr char date[] = __DATE__;
  constexpr char time[] = __TIME__;
  constexpr std::uint32_t year =
      static_cast<std::uint32_t>(date[7] - '0') * 1000U +
      static_cast<std::uint32_t>(date[8] - '0') * 100U +
      static_cast<std::uint32_t>(date[9] - '0') * 10U +
      static_cast<std::uint32_t>(date[10] - '0');
  return ((year - 2000U) << 25U) | (compileMonth(date) << 21U) |
         (decimal2(date[4], date[5]) << 16U) |
         (decimal2(time[0], time[1]) << 11U) |
         (decimal2(time[3], time[4]) << 5U) |
         (decimal2(time[6], time[7]) / 2U);
}

constexpr std::uint16_t kStatusIna219 = 1U << 0U;
constexpr std::uint16_t kStatusPwm = 1U << 1U;
constexpr std::uint16_t kStatusTLed = 1U << 2U;
constexpr std::uint16_t kStatusTBt = 1U << 3U;
constexpr std::uint16_t kStatusRfLearned = 1U << 4U;
constexpr std::uint16_t kStatusRfLearning = 1U << 5U;
constexpr std::uint16_t kStatusStreaming = 1U << 6U;
constexpr std::uint16_t kStatusRfReceived = 1U << 7U;
constexpr std::uint16_t kStatusSilent = 1U << 9U;
constexpr std::uint16_t kStatusRelayBusy = 1U << 10U;
constexpr std::uint16_t kStatusDoorOpen = 1U << 11U;
constexpr std::uint16_t kStatusBuzzerBusy = 1U << 12U;
constexpr std::uint16_t kStatusProgramRunning = 1U << 13U;
constexpr std::uint16_t kStatusHostOffline = 1U << 14U;
constexpr std::uint16_t kStatusHot = 1U << 15U;

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

std::uint8_t easedByte(std::uint8_t current, std::uint8_t target) {
  if (current == target) {
    return current;
  }
  const std::uint8_t distance = current > target ? current - target
                                                  : target - current;
  const std::uint8_t step =
      std::max<std::uint8_t>(1, std::min<std::uint8_t>(8, distance >> 4U));
  if (current < target) {
    return static_cast<std::uint8_t>(
        std::min<unsigned>(target, static_cast<unsigned>(current) + step));
  }
  return static_cast<std::uint8_t>(current - target > step ? current - step
                                                            : target);
}

std::uint8_t encodeSegment(char value) {
  switch (value) {
  case 'b': return 0x7C;
  case 'c': return 0x58;
  case 'd': return 0x5E;
  case 'h': return 0x74;
  case 'n': return 0x54;
  case 'o': return 0x5C;
  case 'r': return 0x50;
  case 't': return 0x78;
  case 'u': return 0x1C;
  default: break;
  }
  if (value >= 'a' && value <= 'z') {
    value = static_cast<char>(value - ('a' - 'A'));
  }
  switch (value) {
  case '0': case 'O': return 0x3F;
  case '1': case 'I': return 0x06;
  case '2': case 'Z': return 0x5B;
  case '3': return 0x4F;
  case '4': return 0x66;
  case '5': case 'S': return 0x6D;
  case '6': case 'G': return 0x7D;
  case '7': return 0x07;
  case '8': return 0x7F;
  case '9': return 0x6F;
  case 'A': return 0x77;
  case 'B': return 0x7C;
  case 'C': return 0x39;
  case 'D': return 0x5E;
  case 'E': return 0x79;
  case 'F': return 0x71;
  case 'H': case 'K': case 'X': return 0x76;
  case 'J': return 0x1E;
  case 'L': return 0x38;
  case 'M': return 0x37;
  case 'N': return 0x54;
  case 'P': return 0x73;
  case 'Q': return 0x67;
  case 'R': return 0x50;
  case 'T': return 0x78;
  case 'U': case 'V': return 0x3E;
  case 'Y': return 0x6E;
  case '-': return 0x40;
  default: return 0;
  }
}

bool validPackedMenuOrder(
    std::uint16_t visibleMask,
    const std::array<std::uint8_t, 7> &packedOrder) {
  if (visibleMask == 0 || (visibleMask & ~kMenuAllPagesMask) != 0) {
    return false;
  }
  std::uint16_t seen = 0;
  for (std::uint8_t rank = 0; rank < kMenuPageCount; ++rank) {
    const std::uint8_t packed = packedOrder[rank >> 1U];
    const std::uint8_t page = (rank & 1U) == 0
                                  ? packed & 0x0FU
                                  : packed >> 4U;
    const std::uint16_t bit = static_cast<std::uint16_t>(1U << page);
    if (page >= kMenuPageCount || (seen & bit) != 0) {
      return false;
    }
    seen |= bit;
  }
  return seen == kMenuAllPagesMask;
}

std::size_t i2cDeviceIndex(std::uint8_t address) {
  const auto found =
      std::find(kI2cAddresses.begin(), kI2cAddresses.end(), address);
  return found == kI2cAddresses.end()
             ? kI2cAddresses.size()
             : static_cast<std::size_t>(found - kI2cAddresses.begin());
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
  lastFadeAt_ = now;
  lastStatusEffectAt_ = now;
  lastRelayTestAt_ = now;
  lastHostActivityAt_ = now;
  buzzerDeadline_ = now;
  i2cLeaseDeadline_ = now;
  lastRemoteActionAt_ = now;
  remoteMomentaryDeadline_ = now;
  loadSettings();
  loadRemotes();
  recordReset(kPowerOnResetCause, false);
  menuVisibleMask_ = settings_.visibleMenuMask;
  for (std::uint8_t rank = 0; rank < kMenuPageCount; ++rank) {
    const std::uint8_t packed = settings_.menuOrder[rank >> 1U];
    menuOrder_[rank] = (rank & 1U) == 0 ? packed & 0x0FU : packed >> 4U;
  }
  menuPage_ = settings_.defaultMenuPage;
  applyStoredSettings();
  restoreStoredOutputs();
  enclosureBrightness_ = settings_.illuminationOffBrightness;
  statusEffectBrightness_ = settings_.statusBrightness;
  if ((settings_.flags & kSettingsProgramming) == 0) {
    pwm_.set(12, 4095);
    pwm_.set(14, scale8(settings_.statusBrightness));
    pwm_.set(11, scale8(enclosureBrightness_));
  }
  updateMenuDisplay();
  const DisplayState display = displays_.state();
  for (std::size_t index = 0; index < lastPushedSegments_.size(); ++index) {
    lastPushedSegments_[index] = encodeSegment(display.segments[index]);
  }
  lastPushedSegmentBrightness_ = settings_.displayBrightness;
  lastPushedBuzzerFrequencyHz_ = display.buzzerFrequencyHz;
  lastPushedBuzzerDurationMs_ = display.buzzerDurationMs;
  lastPushedBuzzerMuted_ = (settings_.flags & kSettingsSilent) != 0;
  lastPushedStatusLed_ = {
      static_cast<std::uint8_t>(pwm_.value(13) >> 4U),
      static_cast<std::uint8_t>(pwm_.value(14) >> 4U),
      static_cast<std::uint8_t>(pwm_.value(15) >> 4U),
      settings_.statusBrightness, 0, statusCondition_};
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
  hostSeen_ = true;
  lastHostActivityAt_ = now;
  const auto bad = [&]() {
    return std::vector<wire::Frame>{errorFrame(
        request.sequence, request.opcode, wire::BadPayload, now)};
  };
  const auto ack = [&]() {
    queueActionEvent(2, request.opcode, payload);
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
    if (payload.size() < 2 ||
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
    return {temperaturesFrame(request.sequence)};

  case wire::Buzzer:
    if (payload.size() < 4) {
      return bad();
    }
    displays_.setBuzzer((settings_.flags & kSettingsSilent) != 0
                            ? 0
                            : readU16(payload, 2),
                        readU16(payload));
    buzzerDeadlineActive_ = readU16(payload) != 0;
    buzzerDeadline_ = now + std::chrono::milliseconds(readU16(payload));
    queueMirrorChanges();
    return ack();
  case wire::PwmSet:
    if (payload.size() < 3 || payload[0] >= 16 ||
        readU16(payload, 1) > 4095) {
      return bad();
    }
    pwm_.select(payload[0]);
    if (!pwm_.set(payload[0], readU16(payload, 1))) {
      return {errorFrame(request.sequence, request.opcode,
                         wire::HardwareUnavailable, now)};
    }
    storeUserPwmValue(payload[0], readU16(payload, 1));
    return ack();
  case wire::PwmAllOff:
    if (!pwm_.available()) {
      return {errorFrame(request.sequence, request.opcode,
                         wire::HardwareUnavailable, now)};
    }
    pwm_.allOff();
    return ack();
  case wire::StatusRgb:
    if (payload.size() < 4) {
      return bad();
    }
    setStatusRgb(payload[0], payload[1], payload[2], payload[3]);
    statusOverride_ = true;
    statusEffect_ = 0;
    statusCondition_ = 0xFF;
    return ack();
  case wire::StatusEffect:
    if (!applyStatusEffect(payload, now)) {
      return bad();
    }
    return ack();
  case wire::StatusProfileGet: {
    if (payload.empty() || payload[0] >= kStatusProfileCount) {
      return bad();
    }
    std::array<std::uint8_t, kStatusProfilePayloadSize> profile{};
    const bool persisted = statusProfile(payload[0], profile);
    std::vector<std::uint8_t> response{payload[0]};
    response.insert(response.end(), profile.begin(), profile.end());
    response.push_back(persisted ? 1U : 0U);
    return {{wire::StatusProfileResponse, request.sequence,
             std::move(response)}};
  }
  case wire::StatusProfileSet:
    if (payload.size() < 1 + kStatusProfilePayloadSize ||
        !setStatusProfile(payload[0], payload.data() + 1, now)) {
      return bad();
    }
    return ack();
  case wire::ProgramState:
    if (payload.empty() || payload[0] > 1) {
      return bad();
    }
    programRunning_ = payload[0] != 0;
    statusOverride_ = false;
    statusEffect_ = 0;
    return ack();
  case wire::PwmGet:
    return {pwmFrame(request.sequence)};
  case wire::AddressableLed: {
    if (payload.size() < 5 ||
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
    if (payload.size() < 8 || readU32(payload) == 0 || payload[4] == 0 ||
        payload[4] > 32 || payload[5] == 0 || payload[5] > 12) {
      return bad();
    }
    return ack();
  case wire::RadioLearnStart:
    if (payload.size() != 2 || payload[0] > kLearnModeTimer ||
        (payload[0] == kLearnModeIndefinite && payload[1] != 0) ||
        (payload[0] == kLearnModeTimer &&
         (payload[1] == 0 || payload[1] > kMaximumLearningSeconds))) {
      return bad();
    }
    learningActive_ = true;
    learningMode_ = payload[0];
    learningTotalSeconds_ = payload[1];
    learningReportedRemaining_ = payload[1];
    if (learningMode_ == kLearnModeTimer) {
      learningDeadline_ = now + std::chrono::seconds(payload[1]);
    }
    queueEvent({9, 3, static_cast<std::uint8_t>(std::count_if(
                          remotes_.begin(), remotes_.end(),
                          [](const RemoteEntry &entry) {
                            return entry.used;
                          })),
                learningMode_, learningTotalSeconds_,
                learningReportedRemaining_});
    return ack();
  case wire::RadioLearnCancel:
    endLearning(1);
    return ack();
  case wire::RadioLearnClear:
    endLearning(1);
    clearRemotes();
    return ack();
  case wire::RadioLearnList:
    if (payload.empty() || payload[0] >= remotes_.size()) {
      return bad();
    }
    return {remotesFrame(request.sequence, payload[0])};
  case wire::RadioLearnRemove:
    if (payload.empty() || payload[0] >= remotes_.size()) {
      return bad();
    }
    remotes_[payload[0]] = RemoteEntry{};
    saveRemote(payload[0]);
    return ack();
  case wire::RadioLearnReplace:
    if (payload.size() < 12 || payload[0] >= remotes_.size() ||
        readU32(payload, 1) == 0 || payload[5] == 0 || payload[5] > 32 ||
        payload[6] == 0 ||
        !validRemoteMapping(payload[9], payload[10], payload[11])) {
      return bad();
    }
    remotes_[payload[0]] =
        RemoteEntry{true, payload[0], readU32(payload, 1), payload[5],
                    payload[6], readU16(payload, 7), payload[9], payload[10],
                    payload[11]};
    saveRemote(payload[0]);
    return ack();

  case wire::MenuAction:
    if (payload.empty() || payload[0] > 3) {
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
    if (payload.size() < 2 || payload[0] > 7 || payload[1] > 1) {
      return bad();
    }
    if (payload[1] != 0 && payload[0] < 4 && !motionAllowed()) {
      return {errorFrame(request.sequence, request.opcode, wire::Unsafe,
                         now)};
    }
    relayTestPeriodMs_ = 0;
    if (payload[0] < 4) {
      const std::uint8_t side = payload[0] >> 1U;
      const std::uint8_t direction = side * 2U;
      const std::uint8_t enable = direction + 1U;
      if ((payload[0] & 1U) == 0) {
        const bool wasEnabled = (relays_.mask() & (1U << enable)) != 0;
        relays_.set(enable, false);
        relays_.set(direction, payload[1] != 0);
        relays_.set(enable, wasEnabled);
      } else {
        relays_.set(enable, payload[1] != 0);
      }
    } else {
      relays_.set(payload[0], payload[1] != 0);
    }
    queueEvent({10, relays_.mask()});
    captureRelayState();
    return ack();
  case wire::RelaySide:
    if (payload.size() < 2 || payload[0] > 1 || payload[1] > 2) {
      return bad();
    }
    if (payload[1] != 0 && !motionAllowed()) {
      return {errorFrame(request.sequence, request.opcode, wire::Unsafe,
                         now)};
    }
    relays_.setSide(payload[0], payload[1]);
    relayTestPeriodMs_ = 0;
    queueEvent({10, relays_.mask()});
    captureRelayState();
    return ack();
  case wire::RelayAllOff:
    relayTestPeriodMs_ = 0;
    relays_.allOff();
    queueEvent({10, relays_.mask()});
    captureRelayState();
    return ack();
  case wire::RelayTest:
    return {wire::makeError(request.sequence, request.opcode,
                            wire::Unsupported)};
  case wire::Reset:
    if (payload.empty() || payload[0] > 1) {
      return bad();
    }
    endLearning(1);
    resetRuntime(now);
    recordReset(kWatchdogResetCause, true);
    return ack();
  case wire::I2cTransfer:
    if (payload.size() < 4 || payload[1] > 10 || payload[2] > 16 ||
        payload[3] > 16 || payload.size() < 4U + payload[2]) {
      return bad();
    }
    if (payload[0] == 0) {
      i2cLeaseAddress_ = 0;
      return ack();
    }
    return {i2cTransferFrame(request.sequence, payload, now)};
  case wire::MenuSetPage:
    if (payload.empty() || payload[0] >= kMenuPageCount) {
      return bad();
    }
    setMenuPage(payload[0]);
    return ack();
  case wire::DisplayText: {
    if (!applyDisplayText(payload, now)) {
      return bad();
    }
    queueMirrorChanges();
    return ack();
  }
  case wire::MacroStart:
    if (payload.size() < 5 || payload[0] != 3) {
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
    macroDroppedSteps_ = 0;
    macroStartedAtUs_ = 0;
    macroQueue_.clear();
    macroLastHostActivity_ = now;
    queueMacroEvent();
    return ack();
  case wire::MacroCancel:
    if (!payload.empty() && payload[0] > 1) {
      return bad();
    }
    cancelMacro(payload.size() == 1 ? payload[0] != 0
                                    : (macroOptions_ & 1U) != 0,
                true);
    return ack();
  case wire::MacroStep: {
    macroLastHostActivity_ = now;
    if (!payload.empty() && payload[0] == 2) {
      return {macroStatusFrame(wire::MacroStatusResponse,
                               request.sequence)};
    }
    if (!payload.empty() && payload[0] == 1) {
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
  case wire::FrontPanelGet:
    return {frontPanelFrame(request.sequence)};
  case wire::RemoteKeyGesture:
    if (payload.size() < 2 || payload[0] > 3 || payload[1] > 6) {
      return bad();
    }
    if (payload[1] == 5) {
      activeKeys_ |= static_cast<std::uint8_t>(1U << payload[0]);
    } else if (payload[1] == 6) {
      activeKeys_ &= static_cast<std::uint8_t>(~(1U << payload[0]));
    }
    if (payload[1] == 5 || payload[1] == 3) {
      if (payload[0] == 0) {
        setMenuPage(static_cast<std::uint8_t>(
            (menuPage_ + kMenuPageCount - 1U) % kMenuPageCount));
      } else if (payload[0] == 1) {
        setMenuPage(static_cast<std::uint8_t>(
            (menuPage_ + 1U) % kMenuPageCount));
      }
    } else if (payload[1] == 1 && payload[0] == 0) {
      setMenuPage(settings_.defaultMenuPage);
    }
    queueEvent({1, payload[0], payload[1], 2, 0xFF});
    return ack();
  case wire::MenuList:
    if (payload.empty() || payload[0] >= kMenuPageCount) {
      return bad();
    }
    return {menuListFrame(request.sequence, payload[0])};
  case wire::MenuLayoutGet:
    return {menuLayoutFrame(request.sequence)};
  case wire::MenuLayoutSet:
    if (!applyMenuLayout(payload)) {
      return bad();
    }
    return ack();
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
  queueMirrorChanges();
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
          "menu 0..13 | segments TEXT | lcd TEXT | "
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
        const std::uint8_t relayMask = relays_.mask();
        if (!motionAllowed()) {
          stopMotion();
        }
        if (relayMask != relays_.mask()) {
          queueEvent({10, relays_.mask()});
        }
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
      queueEvent(
          {1, static_cast<std::uint8_t>(key - 1), gesture, 0, 0xFF});
      queueActionEvent(0, wire::RemoteKeyGesture,
                       {static_cast<std::uint8_t>(key - 1), gesture});
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
      const bool active = value == "on" || value == "1";
      if (!executeQueuedCommand(
              wire::RelaySet,
              {static_cast<std::uint8_t>(relay - 1),
               static_cast<std::uint8_t>(active)},
              Clock::now())) {
        throw std::runtime_error(
            relay <= 4 && active
                ? "motion relay denied by the enclosure-door policy"
                : "relay command was rejected");
      }
      queueActionEvent(0, wire::RelaySet,
                       {static_cast<std::uint8_t>(relay - 1),
                        static_cast<std::uint8_t>(active)});
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
      pwm_.select(channel);
      if (!pwm_.set(channel, value)) {
        throw std::runtime_error("PWM controller is unavailable");
      }
      storeUserPwmValue(channel, value);
      queueActionEvent(0, wire::PwmSet,
                       {channel, static_cast<std::uint8_t>(value),
                        static_cast<std::uint8_t>(value >> 8U)});
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
    if (command == "segments" || command == "lcd") {
      const std::string text = tailAfterCommand(line);
      if (command == "segments") {
        displays_.setSegments(text);
        segmentDeadlineActive_ = false;
      } else {
        displays_.setLcd(text);
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
      const auto code =
          static_cast<std::uint32_t>(parseUnsigned(args[1], 0xFFFFFFFFU));
      const auto bits =
          static_cast<std::uint8_t>(parseUnsigned(args[2], 32));
      const auto protocol =
          static_cast<std::uint8_t>(parseUnsigned(args[3], 12));
      const auto pulseUs =
          static_cast<std::uint16_t>(parseUnsigned(args[4], 65535));
      if (code == 0 || bits == 0 || protocol == 0) {
        throw std::invalid_argument("RF code, bits, and protocol must be nonzero");
      }
      auto slot = std::find_if(
          remotes_.begin(), remotes_.end(),
          [code, bits, protocol](const RemoteEntry &entry) {
            return entry.used && entry.code == code && entry.bits == bits &&
                   entry.protocol == protocol;
          });
      if (slot == remotes_.end()) {
        slot = std::find_if(remotes_.begin(), remotes_.end(),
                            [](const RemoteEntry &entry) {
                              return !entry.used;
                            });
      }
      if (slot == remotes_.end()) {
        queueEvent({8, static_cast<std::uint8_t>(code),
                    static_cast<std::uint8_t>(code >> 8U),
                    static_cast<std::uint8_t>(code >> 16U),
                    static_cast<std::uint8_t>(code >> 24U), bits, protocol,
                    static_cast<std::uint8_t>(pulseUs),
                    static_cast<std::uint8_t>(pulseUs >> 8U), 0xFF});
        endLearning(2);
        return {"RF learning store is full"};
      }
      const std::uint8_t id =
          static_cast<std::uint8_t>(slot - remotes_.begin());
      if (!slot->used) {
        *slot = RemoteEntry{};
        slot->used = true;
        slot->id = id;
        slot->code = code;
        slot->bits = bits;
        slot->protocol = protocol;
      }
      slot->pulseUs = pulseUs;
      saveRemote(id);
      lastRadioCode_ = code;
      queueEvent({5, id});
      queueEvent({8, static_cast<std::uint8_t>(code),
                  static_cast<std::uint8_t>(code >> 8U),
                  static_cast<std::uint8_t>(code >> 16U),
                  static_cast<std::uint8_t>(code >> 24U), bits, protocol,
                  static_cast<std::uint8_t>(pulseUs),
                  static_cast<std::uint8_t>(pulseUs >> 8U), id});
      const std::uint8_t count = static_cast<std::uint8_t>(std::count_if(
          remotes_.begin(), remotes_.end(),
          [](const RemoteEntry &entry) { return entry.used; }));
      if (count >= remotes_.size()) {
        endLearning(2);
      }
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
      const TimePoint now = Clock::now();
      const bool repeated =
          lastRemoteActionValid_ && code == lastRemoteActionCode_ &&
          now - lastRemoteActionAt_ < std::chrono::milliseconds(400);
      lastRemoteActionValid_ = true;
      lastRemoteActionCode_ = code;
      lastRemoteActionAt_ = now;
      lastRadioCode_ = code;
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
      if (learned != remotes_.end()) {
        const bool refreshable = learned->behavior == 2 ||
                                 learned->behavior == 3 ||
                                 learned->behavior == 4;
        if (refreshable || !repeated) {
          executeLearnedRemote(*learned, now);
        }
      }
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
        clearRemotes();
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
  std::vector<std::uint8_t> payload{6, 3, macroState_, macroId_};
  appendU16(payload, macroAcceptedSteps_);
  appendU16(payload, macroExecutedSteps_);
  appendU16(payload, macroAcceptedBytes_);
  payload.push_back(static_cast<std::uint8_t>(macroQueue_.size()));
  payload.push_back(macroUnderruns_);
  payload.push_back(macroDispatchErrors_);
  appendU32(payload, macroStartedAtUs_);
  appendU16(payload, macroTotalSteps_);
  appendU16(payload, macroDroppedSteps_);
  return {opcode, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::menuLayoutFrame(std::uint8_t sequence) const {
  // Schema 2 packs the 14 stable page IDs into exactly seven bytes.
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
  return {wire::MenuLayoutResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::helloFrame(std::uint8_t sequence) const {
  constexpr std::uint32_t capabilities =
      0x01FFF7FFU | (1UL << 25U) | (1UL << 26U) | (1UL << 27U) |
      (1UL << 28U) | (1UL << 29U) | (1UL << 30U) | (1UL << 31U);
  std::vector<std::uint8_t> payload{3, 1};
  appendU32(payload, capabilities);
  appendU32(payload, buildHash());
  appendU32(payload, packedBuildTimestamp());
  return {wire::HelloResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::statusFrame(std::uint8_t sequence,
                                      TimePoint now) const {
  const SensorReadings sensors = sensors_.readings();
  std::uint16_t flags = kStatusIna219 | kStatusTLed | kStatusTBt;
  if (pwm_.available()) {
    flags |= kStatusPwm;
  }
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
  if (lastRadioCode_ != 0) {
    flags |= kStatusRfReceived;
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
  if (buzzerDeadlineActive_ && now < buzzerDeadline_) {
    flags |= kStatusBuzzerBusy;
  }
  if (programRunning_) {
    flags |= kStatusProgramRunning;
  }
  if (!hostSeen_ || now - lastHostActivityAt_ > kHostOfflineAfter) {
    flags |= kStatusHostOffline;
  }
  if (sensors.tLedCentiC >= kHotTemperatureCentiC ||
      sensors.tBtCentiC >= kHotTemperatureCentiC) {
    flags |= kStatusHot;
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
  payload.push_back(static_cast<std::uint8_t>(pwm_.available()));
  payload.push_back(pwm_.selected());
  appendU16(payload, pwm_.value(pwm_.selected()));
  payload.push_back(0); // The LCD is host-driven, matching physical firmware.
  payload.push_back(pwmErrors_);
  appendU16(payload, framingErrors_);
  appendU16(payload, crcErrors_);
  payload.push_back(resetCause_);
  appendU32(payload, resetCount_);
  return {wire::StatusResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::settingsFrame(std::uint8_t sequence) const {
  std::vector<std::uint8_t> payload{
      3,
      settings_.flags,
      settings_.illuminationMode,
      settings_.illuminationOnBrightness,
      settings_.illuminationOffBrightness,
      settings_.displayBrightness,
      settings_.statusBrightness,
      settings_.outputPersistence,
  };
  appendU16(payload, settings_.streamPeriodMs);
  payload.push_back(settings_.defaultMenuPage);
  payload.push_back(settings_.menuFlags);
  payload.push_back(settings_.displayOptions);
  payload.push_back(settings_.relayRestoreMask);
  payload.push_back(settings_.motionBreakMs);
  payload.push_back(1); // Virtual EEPROM settings are initialized/persisted.
  payload.push_back(1); // Board name shares the persisted settings record.
  payload.push_back(static_cast<std::uint8_t>(settings_.boardName.size()));
  payload.insert(payload.end(), settings_.boardName.begin(), settings_.boardName.end());
  return {wire::SettingsResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::pwmFrame(std::uint8_t sequence) const {
  std::vector<std::uint8_t> payload{
      static_cast<std::uint8_t>(pwm_.available()), pwm_.selected()};
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

wire::Frame VirtualBoard::frontPanelFrame(std::uint8_t sequence) const {
  const DisplayState state = displays_.state();
  std::vector<std::uint8_t> payload(47, 0);
  payload[0] = 2;
  bool active = false;
  for (std::size_t index = 0; index < 4; ++index) {
    const char value = index < state.segments.size() ? state.segments[index]
                                                      : ' ';
    payload[1 + index] = encodeSegment(value);
    active = active || payload[1 + index] != 0;
  }
  payload[5] = settings_.displayBrightness;
  payload[6] = active ? 2 : 0;
  std::string lcd = state.lcdLine1.substr(0, 16);
  lcd.resize(16, ' ');
  std::string line2 = state.lcdLine2.substr(0, 16);
  line2.resize(16, ' ');
  lcd += line2;
  std::copy(lcd.begin(), lcd.end(), payload.begin() + 9);
  payload[41] = activeKeys_;
  payload[42] = menuPage_;
  payload[43] = static_cast<std::uint8_t>(menuPage_ + 1U);
  payload[44] = static_cast<std::uint8_t>(
      (hostPanelCaptured_ ? 0x80U : 0U) | ((hostPanelMeta_ >> 12U) & 0x0FU));
  payload[45] = static_cast<std::uint8_t>(hostPanelMeta_);
  payload[46] = static_cast<std::uint8_t>((hostPanelMeta_ >> 8U) & 0x0FU);
  return {wire::FrontPanelResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::menuListFrame(std::uint8_t sequence,
                                        std::uint8_t cursor) const {
  static constexpr std::array<const char *, kMenuPageCount> labels{
      "door", "VOLT", "CURR", "tLED", "t-bt", "LItE", "bEEP",
      "PWM ", "rELY", "KEY ", "uPWM", "r5-8", "MOVE", "LErn"};
  std::vector<std::uint8_t> payload{1, kMenuPageCount, 0xFF, 0};
  while (cursor < kMenuPageCount && payload[3] < 7) {
    payload.push_back(cursor);
    payload.push_back(static_cast<std::uint8_t>(cursor + 1U));
    payload.insert(payload.end(), labels[cursor], labels[cursor] + 4);
    ++cursor;
    ++payload[3];
  }
  if (cursor < kMenuPageCount) {
    payload[2] = cursor;
  }
  return {wire::MenuListResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::i2cTransferFrame(
    std::uint8_t sequence, const std::vector<std::uint8_t> &request,
    TimePoint now) {
  const std::uint8_t address = request[0];
  const std::uint8_t writeLength = request[2];
  const std::uint8_t readLength = request[3];
  if (request[1] != 0) {
    i2cLeaseAddress_ = address;
    i2cLeaseDeadline_ = now + std::chrono::seconds(request[1]);
  }
  const std::size_t device = i2cDeviceIndex(address);
  std::vector<std::uint8_t> payload{
      static_cast<std::uint8_t>(device == kI2cAddresses.size() ? 2 : 0),
      address, 0};
  if (device == kI2cAddresses.size()) {
    return {wire::I2cTransferResponse, sequence, std::move(payload)};
  }
  if (writeLength != 0) {
    i2cRegisterPointers_[device] = request[4];
    for (std::uint8_t index = 1; index < writeLength; ++index) {
      i2cRegisters_[device][i2cRegisterPointers_[device]++] =
          request[4U + index];
    }
  }
  for (std::uint8_t index = 0; index < readLength; ++index) {
    payload.push_back(i2cRegisters_[device][i2cRegisterPointers_[device]++]);
    ++payload[2];
  }
  return {wire::I2cTransferResponse, sequence, std::move(payload)};
}

wire::Frame VirtualBoard::remotesFrame(std::uint8_t sequence,
                                       std::uint8_t cursor) const {
  const std::uint8_t total =
      static_cast<std::uint8_t>(std::count_if(
          remotes_.begin(), remotes_.end(),
          [](const RemoteEntry &entry) { return entry.used; }));
  std::vector<std::uint8_t> payload{1, total, 0xFF, 0};
  std::size_t scan = cursor;
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
      payload[2] = static_cast<std::uint8_t>(scan);
      break;
    }
    ++scan;
  }
  return {wire::RadioLearnListResponse, sequence, std::move(payload)};
}

bool VirtualBoard::applySettings(
    const std::vector<std::uint8_t> &payload) {
  const bool hasBoardName = payload.size() != 15;
  if ((hasBoardName &&
       (payload.size() < 16 || payload.size() != 16U + payload[15])) ||
      payload[0] != 3 || payload[2] > 2 ||
      payload[5] > 7 ||
      (payload[7] & ~kOutputPersistenceAllowed) != 0 || payload[14] == 0) {
    return false;
  }
  const std::uint16_t stream = readU16(payload, 8);
  if (stream != 0 && stream < 100) {
    return false;
  }
  if (payload[10] >= kMenuPageCount ||
      (menuVisibleMask_ & (std::uint16_t{1} << payload[10])) == 0) {
    return false;
  }
  std::string boardName = settings_.boardName;
  if (hasBoardName) {
    boardName.assign(payload.begin() + 16, payload.end());
    if (boardName.size() > 8U ||
        std::any_of(boardName.begin(), boardName.end(), [](unsigned char value) {
          return value < 0x20U || value > 0x7EU;
        }) ||
        (!boardName.empty() &&
         (boardName.front() == ' ' || boardName.back() == ' '))) {
      return false;
    }
  }

  settings_.flags = payload[1] & kSettingsAllowed;
  settings_.illuminationMode = payload[2];
  settings_.illuminationOnBrightness = payload[3];
  settings_.illuminationOffBrightness = payload[4];
  settings_.displayBrightness = payload[5];
  settings_.statusBrightness = payload[6];
  settings_.outputPersistence = payload[7];
  settings_.streamPeriodMs = stream;
  settings_.defaultMenuPage = payload[10];
  settings_.menuFlags = payload[11];
  settings_.displayOptions = payload[12];
  settings_.relayRestoreMask = payload[13];
  settings_.motionBreakMs = payload[14];
  settings_.boardName = std::move(boardName);
  applyStoredSettings();
  return true;
}

bool VirtualBoard::applyMenuLayout(
    const std::vector<std::uint8_t> &payload) {
  constexpr std::size_t kMenuLayoutPayloadSize =
      4U + (kMenuPageCount + 1U) / 2U;
  if (payload.size() < kMenuLayoutPayloadSize || payload[0] != 2 ||
      payload[1] != kMenuPageCount) {
    return false;
  }
  const std::uint16_t mask = readU16(payload, 2);
  std::array<std::uint8_t, 7> packed{};
  std::copy(payload.begin() + 4,
            payload.begin() + static_cast<std::ptrdiff_t>(
                                    kMenuLayoutPayloadSize),
            packed.begin());
  if (!validPackedMenuOrder(mask, packed)) {
    return false;
  }
  std::array<std::uint8_t, kMenuPageCount> order{};
  for (std::size_t rank = 0; rank < order.size(); ++rank) {
    const std::uint8_t value = packed[rank / 2U];
    order[rank] = (rank & 1U) == 0 ? value & 0x0FU : value >> 4U;
  }
  menuVisibleMask_ = mask;
  menuOrder_ = order;
  settings_.visibleMenuMask = mask;
  settings_.menuOrder = packed;
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
  saveSettings();
  return true;
}

bool VirtualBoard::applyDisplayText(
    const std::vector<std::uint8_t> &payload, TimePoint now) {
  if (payload.size() < 4 || payload[0] > 5 || payload[3] > 40 ||
      (payload[0] != 5 && payload.size() < 4U + payload[3]) ||
      (payload[0] == 5 &&
       (payload.size() < 8 || payload.size() < 8U + payload[3] ||
        (payload[4] & kSegmentRepeatMask) > 2 ||
        (payload[4] & 0x7CU) != 0 ||
        ((payload[4] & kSegmentRepeatMask) == 2 && payload[7] == 0))) ||
      (payload[0] == 3 && (payload[3] < 4 || payload[3] > 36)) ||
      (payload[0] == 4 && payload[3] != 0)) {
    return false;
  }
  const std::uint8_t target = payload[0];
  const std::uint16_t duration = readU16(payload, 1);
  const std::size_t textLength = payload[3];
  if (target == 4) {
    releaseHostPanel();
    return true;
  }
  if (target == 5) {
    clearScheduledSegments(false);
    if (textLength == 0) {
      updateMenuDisplay();
      return true;
    }
    scheduledSegmentText_.assign(payload.begin() + 8,
                                 payload.begin() + 8 + textLength);
    scheduledSegmentOptions_ = payload[4];
    scheduledSegmentHoldMs_ = readU16(payload, 5);
    scheduledSegmentIntervalSeconds_ = payload[7];
    scheduledSegmentStepMs_ = duration == 0
                                  ? 260
                                  : std::max<std::uint16_t>(80, duration);
    scheduledSegmentIndex_ = 0;
    scheduledSegmentActive_ = true;
    scheduledSegmentWaiting_ = false;
    showScheduledSegmentWindow();
    const bool scrolling = scheduledSegmentText_.size() > 4 ||
                           (scheduledSegmentOptions_ &
                            kSegmentForceScroll) != 0;
    const std::uint8_t repeat =
        scheduledSegmentOptions_ & kSegmentRepeatMask;
    segmentDeadlineActive_ = scrolling ||
                             (scheduledSegmentHoldMs_ != 0 && repeat != 1);
    if (segmentDeadlineActive_) {
      segmentDeadline_ = now + std::chrono::milliseconds(
                                   scrolling ? scheduledSegmentStepMs_
                                             : scheduledSegmentHoldMs_);
    }
    return true;
  }
  clearScheduledSegments(false);
  if (target == 3) {
    hostPanelCaptured_ = true;
    hostPanelMeta_ = duration;
  }
  if (target == 0 || target == 2 || target == 3) {
    const std::size_t segmentLength = std::min<std::size_t>(4, textLength);
    if (segmentLength == 0) {
      segmentDeadlineActive_ = false;
      updateMenuDisplay();
    } else {
      displays_.setSegments(std::string(payload.begin() + 4,
                                        payload.begin() + 4 + segmentLength));
      segmentDeadlineActive_ = target != 3 && duration != 0;
      segmentDeadline_ = now + std::chrono::milliseconds(duration);
    }
  }
  if (target == 1 || target == 2 || target == 3) {
    const std::size_t offset = target == 3 ? 8 : 4;
    const std::size_t lcdLength =
        std::min<std::size_t>(32, target == 3 ? textLength - 4 : textLength);
    displays_.setLcd(std::string(payload.begin() + offset,
                                 payload.begin() + offset + lcdLength));
  }
  return true;
}

bool VirtualBoard::validRemoteMapping(std::uint8_t kind,
                                      std::uint8_t value,
                                      std::uint8_t behavior) const {
  static constexpr std::array<std::uint8_t, 6> limits{0, 4, 4, 8, 2, 11};
  return kind < limits.size() && behavior <= 5 &&
         (kind == 0 || value < limits[kind]);
}

void VirtualBoard::executeLearnedRemote(const RemoteEntry &remote,
                                        TimePoint now) {
  if (remoteMomentaryKind_ != 0 &&
      (remoteMomentaryKind_ != remote.actionKind ||
       remoteMomentaryValue_ != remote.actionValue)) {
    stopRemoteMomentary(now);
  }

  switch (remote.actionKind) {
  case 1: // Key: emit an RF-sourced click and apply the matching menu action.
    queueEvent({1, remote.actionValue, 0, 1, remote.id});
    [[fallthrough]];
  case 2: // Menu: Keys 1/2 are previous/next in the virtual root menu.
    if (remote.actionValue == 0) {
      setMenuPage(static_cast<std::uint8_t>(
          (menuPage_ + kMenuPageCount - 1U) % kMenuPageCount));
    } else if (remote.actionValue == 1) {
      setMenuPage(static_cast<std::uint8_t>(
          (menuPage_ + 1U) % kMenuPageCount));
    }
    queueActionEvent(1, wire::MenuAction, {remote.actionValue});
    return;
  case 3: { // Relay: Press and Toggle invert; Momentary expires after 350 ms.
    const bool active = (relays_.mask() & (1U << remote.actionValue)) != 0;
    const bool next = remote.behavior <= 1 ? !active : true;
    const std::vector<std::uint8_t> action{
        remote.actionValue, static_cast<std::uint8_t>(next)};
    const bool accepted = executeQueuedCommand(wire::RelaySet, action, now);
    if (accepted) {
      queueActionEvent(1, wire::RelaySet, action);
    }
    if (accepted && remote.behavior == 2) {
      remoteMomentaryKind_ = remote.actionKind;
      remoteMomentaryValue_ = remote.actionValue;
      remoteMomentaryDeadline_ = now + std::chrono::milliseconds(350);
    }
    return;
  }
  case 4: { // Side: Up/Down refresh a 350 ms hold; Stop is immediate.
    const std::uint8_t motion =
        remote.behavior == 5 ? 0 : (remote.behavior == 4 ? 2 : 1);
    const std::vector<std::uint8_t> action{remote.actionValue, motion};
    const bool accepted = executeQueuedCommand(wire::RelaySide, action, now);
    if (accepted) {
      queueActionEvent(1, wire::RelaySide, action);
    }
    if (accepted && motion != 0) {
      remoteMomentaryKind_ = remote.actionKind;
      remoteMomentaryValue_ = remote.actionValue;
      remoteMomentaryDeadline_ = now + std::chrono::milliseconds(350);
    }
    return;
  }
  case 5: { // PWM: Momentary goes full-on; other behaviors toggle 0/4095.
    const bool active = pwm_.value(remote.actionValue) != 0;
    const std::uint16_t value =
        remote.behavior == 2 ? 4095 : (active ? 0 : 4095);
    const std::vector<std::uint8_t> action{
        remote.actionValue, static_cast<std::uint8_t>(value),
        static_cast<std::uint8_t>(value >> 8U)};
    const bool accepted = executeQueuedCommand(wire::PwmSet, action, now);
    if (accepted) {
      queueActionEvent(1, wire::PwmSet, action);
    }
    if (accepted && remote.behavior == 2) {
      remoteMomentaryKind_ = remote.actionKind;
      remoteMomentaryValue_ = remote.actionValue;
      remoteMomentaryDeadline_ = now + std::chrono::milliseconds(350);
    }
    return;
  }
  default:
    return;
  }
}

void VirtualBoard::stopRemoteMomentary(TimePoint now) {
  switch (remoteMomentaryKind_) {
  case 3: {
    const std::vector<std::uint8_t> action{remoteMomentaryValue_, 0};
    if (executeQueuedCommand(wire::RelaySet, action, now)) {
      queueActionEvent(1, wire::RelaySet, action);
    }
    break;
  }
  case 4: {
    const std::vector<std::uint8_t> action{remoteMomentaryValue_, 0};
    if (executeQueuedCommand(wire::RelaySide, action, now)) {
      queueActionEvent(1, wire::RelaySide, action);
    }
    break;
  }
  case 5: {
    const std::vector<std::uint8_t> action{remoteMomentaryValue_, 0, 0};
    if (executeQueuedCommand(wire::PwmSet, action, now)) {
      queueActionEvent(1, wire::PwmSet, action);
    }
    break;
  }
  default:
    break;
  }
  remoteMomentaryKind_ = 0;
}

bool VirtualBoard::motionAllowed() const {
  const std::uint8_t policy = (settings_.flags & kSettingsMotionPolicy) >> 3U;
  const bool doorOpen = sensors_.readings().doorOpen;
  return policy == 0 || (policy == 1 && !doorOpen) ||
         (policy == 2 && doorOpen);
}

void VirtualBoard::stopMotion() {
  relays_.setSide(0, 0);
  relays_.setSide(1, 0);
  captureRelayState();
}

void VirtualBoard::applyStoredSettings() {
  relays_.setRetainDirectionOnStop(
      (settings_.outputPersistence & kRetainDirectionOnStop) != 0);
  if ((settings_.flags & kSettingsProgramming) != 0) {
    buzzerDeadlineActive_ = false;
    displays_.setBuzzer(0, 0);
    relayTestPeriodMs_ = 0;
    relays_.allOff();
    pwm_.allOff();
    captureRelayState();
    return;
  }
  if ((settings_.flags & kSettingsSilent) != 0) {
    const DisplayState display = displays_.state();
    displays_.setBuzzer(0, display.buzzerDurationMs);
  }
  if (!motionAllowed()) {
    stopMotion();
  }
  pwm_.set(14, scale8(settings_.statusBrightness));
}

void VirtualBoard::restoreStoredOutputs() {
  relays_.allOff();
  pwm_.allOff();
  if ((settings_.flags & kSettingsProgramming) != 0) {
    return;
  }
  if ((settings_.outputPersistence & kPersistMotion) != 0 &&
      motionAllowed()) {
    for (std::uint8_t index = 0; index < 4; ++index) {
      relays_.set(index,
                  (settings_.relayRestoreMask & (1U << index)) != 0);
    }
  }
  if ((settings_.outputPersistence & kPersistUserRelays) != 0) {
    for (std::uint8_t index = 4; index < 8; ++index) {
      relays_.set(index,
                  (settings_.relayRestoreMask & (1U << index)) != 0);
    }
  }
  if ((settings_.outputPersistence & kPersistUserPwm) != 0) {
    for (std::size_t channel = 0; channel < settings_.userPwm.size();
         ++channel) {
      pwm_.set(static_cast<std::uint8_t>(channel),
               scale8(settings_.userPwm[channel]));
    }
  }
}

void VirtualBoard::storeUserPwmValue(std::uint8_t channel,
                                     std::uint16_t value) {
  if (channel >= settings_.userPwm.size()) {
    return;
  }
  const std::uint8_t stored = static_cast<std::uint8_t>(
      value >= 4080 ? 255 : (value + 8U) / 16U);
  if (settings_.userPwm[channel] != stored) {
    settings_.userPwm[channel] = stored;
    saveSettings();
  }
}

void VirtualBoard::captureRelayState() {
  const std::uint8_t mask = relays_.mask();
  if (settings_.relayRestoreMask != mask) {
    settings_.relayRestoreMask = mask;
    saveSettings();
  }
}

std::uint8_t VirtualBoard::learningRemainingSeconds(TimePoint now) const {
  if (learningMode_ != kLearnModeTimer || now >= learningDeadline_) {
    return 0;
  }
  const auto remaining = std::chrono::duration_cast<std::chrono::milliseconds>(
      learningDeadline_ - now);
  return static_cast<std::uint8_t>((remaining.count() + 999) / 1000);
}

void VirtualBoard::endLearning(std::uint8_t state) {
  if (!learningActive_) {
    return;
  }
  const std::uint8_t remaining = learningRemainingSeconds(Clock::now());
  learningActive_ = false;
  const std::uint8_t count = static_cast<std::uint8_t>(std::count_if(
      remotes_.begin(), remotes_.end(),
      [](const RemoteEntry &entry) { return entry.used; }));
  queueEvent({9, state, count, learningMode_, learningTotalSeconds_,
              remaining});
}

void VirtualBoard::releaseHostPanel() {
  hostPanelCaptured_ = false;
  hostPanelMeta_ = 0;
  clearScheduledSegments(false);
  setMenuPage(settings_.defaultMenuPage);
}

void VirtualBoard::loadSettings() {
  if (eeprom_.size() < kSettingsAddress + kSettingsRecordSize) {
    throw std::runtime_error("virtual EEPROM is too small for settings");
  }
  std::array<std::uint8_t, kSettingsRecordSize> record{};
  for (std::size_t index = 0; index < record.size(); ++index) {
    record[index] = eeprom_.read(kSettingsAddress + index);
  }
  if (record.back() != wire::crc8(record.data(), record.size() - 1U)) {
    resetSettings();
    saveSettings();
    return;
  }
  Settings loaded;
  loaded.flags = record[0];
  loaded.illuminationMode = record[1];
  loaded.illuminationOnBrightness = record[2];
  loaded.illuminationOffBrightness = record[3];
  loaded.displayBrightness = record[4];
  loaded.statusBrightness = record[5];
  loaded.outputPersistence = record[6];
  loaded.streamPeriodMs = static_cast<std::uint16_t>(
      record[7] | (static_cast<std::uint16_t>(record[8]) << 8U));
  std::copy(record.begin() + 9, record.begin() + 17,
            loaded.userPwm.begin());
  loaded.defaultMenuPage = record[17];
  loaded.menuFlags = record[18];
  loaded.visibleMenuMask = static_cast<std::uint16_t>(
      record[19] | (static_cast<std::uint16_t>(record[20]) << 8U));
  std::copy(record.begin() + 21, record.begin() + 28,
            loaded.menuOrder.begin());
  loaded.displayOptions = record[28];
  loaded.relayRestoreMask = record[29];
  loaded.motionBreakMs = record[30];
  const std::uint8_t boardNameLength = record[31];
  if (loaded.illuminationMode > 2 || loaded.displayBrightness > 7 ||
      (loaded.outputPersistence & ~kOutputPersistenceAllowed) != 0 ||
      loaded.motionBreakMs == 0 ||
      (loaded.streamPeriodMs != 0 && loaded.streamPeriodMs < 100) ||
      boardNameLength > 8U || loaded.defaultMenuPage >= kMenuPageCount ||
      !validPackedMenuOrder(loaded.visibleMenuMask, loaded.menuOrder) ||
      (loaded.visibleMenuMask &
       (std::uint16_t{1} << loaded.defaultMenuPage)) == 0) {
    resetSettings();
    saveSettings();
    return;
  }
  settings_ = loaded;
  settings_.boardName.assign(record.begin() + 32,
                             record.begin() + 32 + boardNameLength);
}

void VirtualBoard::saveSettings() {
  std::array<std::uint8_t, kSettingsRecordSize> record{};
  record[0] = settings_.flags;
  record[1] = settings_.illuminationMode;
  record[2] = settings_.illuminationOnBrightness;
  record[3] = settings_.illuminationOffBrightness;
  record[4] = settings_.displayBrightness;
  record[5] = settings_.statusBrightness;
  record[6] = settings_.outputPersistence;
  record[7] = static_cast<std::uint8_t>(settings_.streamPeriodMs);
  record[8] = static_cast<std::uint8_t>(settings_.streamPeriodMs >> 8U);
  std::copy(settings_.userPwm.begin(), settings_.userPwm.end(),
            record.begin() + 9);
  record[17] = settings_.defaultMenuPage;
  record[18] = settings_.menuFlags;
  record[19] = static_cast<std::uint8_t>(settings_.visibleMenuMask);
  record[20] = static_cast<std::uint8_t>(settings_.visibleMenuMask >> 8U);
  std::copy(settings_.menuOrder.begin(), settings_.menuOrder.end(),
            record.begin() + 21);
  record[28] = settings_.displayOptions;
  record[29] = settings_.relayRestoreMask;
  record[30] = settings_.motionBreakMs;
  record[31] = static_cast<std::uint8_t>(settings_.boardName.size());
  std::copy(settings_.boardName.begin(), settings_.boardName.end(),
            record.begin() + 32);
  record.back() = wire::crc8(record.data(), record.size() - 1U);
  for (std::size_t index = 0; index < record.size(); ++index) {
    eeprom_.update(kSettingsAddress + index, record[index]);
  }
  eeprom_.flush();
}

void VirtualBoard::resetSettings() { settings_ = Settings{}; }

void VirtualBoard::loadRemotes() {
  if (eeprom_.size() < kRemoteEntriesAddress +
                           remotes_.size() * kRemoteRecordSize) {
    throw std::runtime_error("virtual EEPROM is too small for learned RF");
  }
  if (eeprom_.read(kRemoteHeaderAddress) != 0x52 ||
      eeprom_.read(kRemoteHeaderAddress + 1U) != 0x4C ||
      eeprom_.read(kRemoteHeaderAddress + 2U) != kRemoteRecordSize ||
      eeprom_.read(kRemoteHeaderAddress + 3U) != remotes_.size()) {
    clearRemotes();
    return;
  }
  remotes_.fill(RemoteEntry{});
  for (std::size_t id = 0; id < remotes_.size(); ++id) {
    std::array<std::uint8_t, kRemoteRecordSize> record{};
    const std::size_t address =
        kRemoteEntriesAddress + id * record.size();
    for (std::size_t index = 0; index < record.size(); ++index) {
      record[index] = eeprom_.read(address + index);
    }
    const std::uint32_t code =
        static_cast<std::uint32_t>(record[0]) |
        (static_cast<std::uint32_t>(record[1]) << 8U) |
        (static_cast<std::uint32_t>(record[2]) << 16U) |
        (static_cast<std::uint32_t>(record[3]) << 24U);
    if (code == 0 || record[4] == 0 || record[4] > 32 ||
        record.back() != wire::crc8(record.data(), record.size() - 1U)) {
      continue;
    }
    remotes_[id] = RemoteEntry{
        true,
        static_cast<std::uint8_t>(id),
        code,
        record[4],
        record[5],
        static_cast<std::uint16_t>(
            record[6] | (static_cast<std::uint16_t>(record[7]) << 8U)),
        record[8],
        record[9],
        record[10],
    };
  }
}

void VirtualBoard::saveRemote(std::uint8_t id) {
  if (id >= remotes_.size()) {
    return;
  }
  std::array<std::uint8_t, kRemoteRecordSize> record{};
  const RemoteEntry &remote = remotes_[id];
  if (remote.used) {
    record[0] = static_cast<std::uint8_t>(remote.code);
    record[1] = static_cast<std::uint8_t>(remote.code >> 8U);
    record[2] = static_cast<std::uint8_t>(remote.code >> 16U);
    record[3] = static_cast<std::uint8_t>(remote.code >> 24U);
    record[4] = remote.bits;
    record[5] = remote.protocol;
    record[6] = static_cast<std::uint8_t>(remote.pulseUs);
    record[7] = static_cast<std::uint8_t>(remote.pulseUs >> 8U);
    record[8] = remote.actionKind;
    record[9] = remote.actionValue;
    record[10] = remote.behavior;
  }
  record.back() = wire::crc8(record.data(), record.size() - 1U);
  const std::size_t address =
      kRemoteEntriesAddress + static_cast<std::size_t>(id) * record.size();
  for (std::size_t index = 0; index < record.size(); ++index) {
    eeprom_.update(address + index, record[index]);
  }
  eeprom_.flush();
}

void VirtualBoard::clearRemotes() {
  remotes_.fill(RemoteEntry{});
  eeprom_.update(kRemoteHeaderAddress, 0x52);
  eeprom_.update(kRemoteHeaderAddress + 1U, 0x4C);
  eeprom_.update(kRemoteHeaderAddress + 2U, kRemoteRecordSize);
  eeprom_.update(kRemoteHeaderAddress + 3U,
                 static_cast<std::uint8_t>(remotes_.size()));
  std::array<std::uint8_t, kRemoteRecordSize> record{};
  record.back() = wire::crc8(record.data(), record.size() - 1U);
  for (std::size_t id = 0; id < remotes_.size(); ++id) {
    const std::size_t address = kRemoteEntriesAddress + id * record.size();
    for (std::size_t index = 0; index < record.size(); ++index) {
      eeprom_.update(address + index, record[index]);
    }
  }
  eeprom_.flush();
}

void VirtualBoard::recordReset(std::uint8_t cause, bool emitEvent) {
  if (eeprom_.size() <
      kResetJournalAddress + kResetJournalSlots * kResetRecordSize) {
    throw std::runtime_error(
        "virtual EEPROM is too small for the reset journal");
  }

  const auto recordChecksum = [](std::uint32_t count) {
    const std::array<std::uint8_t, 4> input{
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
    const bool valid = marker == kResetRecordMarker && count != 0 &&
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
  applyStoredSettings();
  restoreStoredOutputs();
  if ((settings_.flags & kSettingsProgramming) == 0) {
    pwm_.set(12, 4095);
    pwm_.set(14, scale8(settings_.statusBrightness));
    pwm_.set(11, scale8(settings_.illuminationOffBrightness));
  }
  learningActive_ = false;
  learningMode_ = kLearnModeIndefinite;
  learningTotalSeconds_ = 0;
  learningReportedRemaining_ = 0;
  relayTestPeriodMs_ = 0;
  macroState_ = 0;
  macroQueue_.clear();
  macroAcceptedSteps_ = 0;
  macroExecutedSteps_ = 0;
  macroAcceptedBytes_ = 0;
  menuPage_ = settings_.defaultMenuPage;
  startedAt_ = now;
  lastStreamAt_ = now;
  lastFadeAt_ = now;
  lastStatusEffectAt_ = now;
  clearScheduledSegments(false);
  buzzerDeadlineActive_ = false;
  statusEffect_ = 0;
  statusEffectBrightness_ = settings_.statusBrightness;
  displays_.setBuzzer(0, 0);
  i2cLeaseAddress_ = 0;
  activeKeys_ = 0;
  hostSeen_ = false;
  programRunning_ = false;
  statusOverride_ = false;
  lastRemoteActionValid_ = false;
  remoteMomentaryKind_ = 0;
  hostPanelCaptured_ = false;
  hostPanelMeta_ = 0;
  updateMenuDisplay();
}

void VirtualBoard::setMenuPage(std::uint8_t page) {
  menuPage_ = static_cast<std::uint8_t>(page % kMenuPageCount);
  if ((settings_.menuFlags & kSaveLastPage) != 0 &&
      settings_.defaultMenuPage != menuPage_) {
    settings_.defaultMenuPage = menuPage_;
    saveSettings();
  }
  if (macroState_ != 1 && macroState_ != 2 && !segmentDeadlineActive_ &&
      (!scheduledSegmentActive_ || scheduledSegmentWaiting_)) {
    updateMenuDisplay();
  }
}

void VirtualBoard::updateMenuDisplay() {
  if (scheduledSegmentActive_ && !scheduledSegmentWaiting_) {
    return;
  }
  static constexpr std::array<const char *, kMenuPageCount> labels{
      "door", "VOLT", "CURR", "tLED", "t-bt", "LItE", "bEEP",
      "PWM ", "rELY", "KEY ", "uPWM", "r5-8", "MOVE", "LErn"};
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
    captureRelayState();
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
    if (payload.size() < 4) {
      return false;
    }
    displays_.setBuzzer((settings_.flags & kSettingsSilent) != 0
                            ? 0
                            : readU16(payload, 2),
                        readU16(payload));
    buzzerDeadlineActive_ = readU16(payload) != 0;
    buzzerDeadline_ = now + std::chrono::milliseconds(readU16(payload));
    return true;
  case wire::PwmSet:
    if (payload.size() < 3 || payload[0] >= 16 ||
        readU16(payload, 1) > 4095) {
      return false;
    }
    pwm_.select(payload[0]);
    if (!pwm_.set(payload[0], readU16(payload, 1))) {
      return false;
    }
    storeUserPwmValue(payload[0], readU16(payload, 1));
    return true;
  case wire::PwmAllOff:
    if (!pwm_.available()) {
      return false;
    }
    for (std::uint8_t channel = 0; channel < 11; ++channel) {
      pwm_.set(channel, 0);
    }
    return true;
  case wire::StatusRgb:
    if (payload.size() < 4) {
      return false;
    }
    setStatusRgb(payload[0], payload[1], payload[2], payload[3]);
    statusOverride_ = true;
    statusEffect_ = 0;
    statusCondition_ = 0xFF;
    return true;
  case wire::StatusEffect:
    return applyStatusEffect(payload, now);
  case wire::StatusProfileSet:
    return payload.size() >= 1 + kStatusProfilePayloadSize &&
           setStatusProfile(payload[0], payload.data() + 1, now);
  case wire::ProgramState:
    if (payload.empty() || payload[0] > 1) {
      return false;
    }
    programRunning_ = payload[0] != 0;
    statusOverride_ = false;
    statusEffect_ = 0;
    return true;
  case wire::AddressableLed: {
    if (payload.size() < 5 ||
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
    return payload.size() >= 8 && readU32(payload) != 0 &&
           payload[4] != 0 && payload[4] <= 32 && payload[5] != 0 &&
           payload[5] <= 12;
  case wire::MenuAction:
    if (payload.empty() || payload[0] > 3) {
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
    if (payload.size() < 2 || payload[0] >= 8 || payload[1] > 1 ||
        (payload[1] != 0 && payload[0] < 4 && !motionAllowed())) {
      return false;
    }
    if (payload[0] < 4) {
      const std::uint8_t side = payload[0] >> 1U;
      const std::uint8_t direction = side * 2U;
      const std::uint8_t enable = direction + 1U;
      if ((payload[0] & 1U) == 0) {
        const bool wasEnabled = (relays_.mask() & (1U << enable)) != 0;
        relays_.set(enable, false);
        relays_.set(direction, payload[1] != 0);
        relays_.set(enable, wasEnabled);
      } else {
        relays_.set(enable, payload[1] != 0);
      }
    } else {
      relays_.set(payload[0], payload[1] != 0);
    }
    queueEvent({10, relays_.mask()});
    captureRelayState();
    return true;
  case wire::RelaySide: {
    if (payload.size() < 2 || payload[0] > 1 || payload[1] > 2 ||
        (payload[1] != 0 && !motionAllowed())) {
      return false;
    }
    relays_.setSide(payload[0], payload[1]);
    queueEvent({10, relays_.mask()});
    captureRelayState();
    return true;
  }
  case wire::RelayAllOff:
    relays_.allOff();
    queueEvent({10, relays_.mask()});
    captureRelayState();
    return true;
  case wire::MenuSetPage:
    if (payload.empty() || payload[0] >= kMenuPageCount) {
      return false;
    }
    setMenuPage(payload[0]);
    return true;
  case wire::DisplayText: {
    return applyDisplayText(payload, now);
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
        captureRelayState();
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

void VirtualBoard::queueActionEvent(
    std::uint8_t source, std::uint8_t opcode,
    const std::vector<std::uint8_t> &payload) {
  if (!MacroAction::recordable(
          opcode, static_cast<std::uint8_t>(
                      std::min<std::size_t>(payload.size(), 0xFFU)))) {
    return;
  }
  const std::size_t length = MacroAction::payloadLength(opcode);
  std::vector<std::uint8_t> event{13, source, opcode,
                                  static_cast<std::uint8_t>(length)};
  event.insert(event.end(), payload.begin(),
               payload.begin() + static_cast<std::ptrdiff_t>(length));
  queueEvent(std::move(event));
}

void VirtualBoard::queueMirrorChanges() {
  const DisplayState display = displays_.state();
  std::array<std::uint8_t, 4> segments{};
  for (std::size_t index = 0; index < segments.size(); ++index) {
    segments[index] = encodeSegment(display.segments[index]);
  }
  if (segments != lastPushedSegments_ ||
      settings_.displayBrightness != lastPushedSegmentBrightness_) {
    std::vector<std::uint8_t> payload(segments.begin(), segments.end());
    payload.push_back(settings_.displayBrightness);
    pendingEvents_.push_back({wire::SegmentChanged, 0, std::move(payload)});
    lastPushedSegments_ = segments;
    lastPushedSegmentBrightness_ = settings_.displayBrightness;
  }

  const bool muted = (settings_.flags & kSettingsSilent) != 0;
  if (display.buzzerFrequencyHz != lastPushedBuzzerFrequencyHz_ ||
      display.buzzerDurationMs != lastPushedBuzzerDurationMs_ ||
      muted != lastPushedBuzzerMuted_) {
    std::vector<std::uint8_t> payload;
    appendU16(payload, display.buzzerFrequencyHz);
    appendU16(payload, display.buzzerDurationMs);
    payload.push_back(static_cast<std::uint8_t>(muted));
    pendingEvents_.push_back({wire::BuzzerChanged, 0, std::move(payload)});
    lastPushedBuzzerFrequencyHz_ = display.buzzerFrequencyHz;
    lastPushedBuzzerDurationMs_ = display.buzzerDurationMs;
    lastPushedBuzzerMuted_ = muted;
  }

  const std::array<std::uint8_t, 6> statusLed{{
      static_cast<std::uint8_t>(pwm_.value(13) >> 4U),
      static_cast<std::uint8_t>(pwm_.value(14) >> 4U),
      static_cast<std::uint8_t>(pwm_.value(15) >> 4U),
      statusEffectBrightness_, statusEffect_, statusCondition_}};
  if (statusLed != lastPushedStatusLed_) {
    pendingEvents_.push_back(
        {wire::StatusLedChanged, 0,
         std::vector<std::uint8_t>(statusLed.begin(), statusLed.end())});
    lastPushedStatusLed_ = statusLed;
  }
}

void VirtualBoard::setStatusRgb(std::uint8_t red, std::uint8_t green,
                                std::uint8_t blue,
                                std::uint8_t brightness) {
  pwm_.set(13, static_cast<std::uint16_t>(scale8(red) * brightness / 255U));
  pwm_.set(14, static_cast<std::uint16_t>(scale8(green) * brightness / 255U));
  pwm_.set(15, static_cast<std::uint16_t>(scale8(blue) * brightness / 255U));
  statusEffectBrightness_ = brightness;
}

bool VirtualBoard::applyStatusEffect(
    const std::vector<std::uint8_t> &payload, TimePoint now) {
  if (payload.size() == 1 && payload[0] == 0) {
    statusEffect_ = 0;
    statusOverride_ = false;
    statusCondition_ = 0;
    setStatusRgb(0, 255, 0, settings_.statusBrightness);
    return true;
  }
  if (payload.size() < 12 || payload[0] == 0 || payload[0] > 4 ||
      payload[8] > payload[7] || readU16(payload, 9) < 640) {
    return false;
  }
  statusEffect_ = payload[0];
  statusCondition_ = 0xFF;
  std::copy_n(payload.begin() + 1, 3, statusEffectColor_.begin());
  std::copy_n(payload.begin() + 4, 3, statusEffectAlternate_.begin());
  statusEffectBrightness_ = payload[7];
  statusEffectMinimum_ = payload[8];
  statusEffectPhase_ = 0;
  statusEffectRepeats_ = payload[11];
  const std::uint16_t period = readU16(payload, 9);
  statusEffectStepMs_ = static_cast<std::uint16_t>(period >> 5U);
  lastStatusEffectAt_ = now;
  statusOverride_ = true;
  renderStatusEffect();
  return true;
}

bool VirtualBoard::statusProfile(
    std::uint8_t condition,
    std::array<std::uint8_t, kStatusProfilePayloadSize> &payload) const {
  if (condition >= kStatusProfileCount) {
    return false;
  }
  const std::size_t address =
      kStatusProfileAddress + condition * kStatusProfileRecordSize;
  for (std::size_t index = 0; index < payload.size(); ++index) {
    payload[index] = eeprom_.read(address + index);
  }
  const std::uint16_t period = readU16(
      std::vector<std::uint8_t>(payload.begin(), payload.end()), 9);
  const bool valid =
      eeprom_.read(address + payload.size()) ==
          wire::crc8(payload.data(), payload.size()) &&
      payload[0] <= 4 && payload[8] <= payload[7] &&
      (payload[0] == 0 || period >= 640);
  if (valid) {
    return true;
  }
  payload.fill(0);
  payload[7] = settings_.statusBrightness;
  if (condition == 0 || condition == 6) {
    return false;
  }
  if (condition == 4 || condition == 5) {
    payload[1] = 255;
  } else {
    payload[3] = 255;
  }
  return false;
}

bool VirtualBoard::setStatusProfile(std::uint8_t condition,
                                    const std::uint8_t *payload,
                                    TimePoint now) {
  if (condition >= kStatusProfileCount || payload == nullptr ||
      payload[0] > 4 || payload[8] > payload[7]) {
    return false;
  }
  const std::uint16_t period = static_cast<std::uint16_t>(payload[9]) |
                               static_cast<std::uint16_t>(payload[10]) << 8U;
  if (payload[0] != 0 && period < 640) {
    return false;
  }
  const std::size_t address =
      kStatusProfileAddress + condition * kStatusProfileRecordSize;
  for (std::size_t index = 0; index < kStatusProfilePayloadSize; ++index) {
    eeprom_.update(address + index, payload[index]);
  }
  eeprom_.update(address + kStatusProfilePayloadSize,
                 wire::crc8(payload, kStatusProfilePayloadSize));
  eeprom_.flush();
  if (statusCondition_ == condition) {
    std::vector<std::uint8_t> effect(payload,
                                     payload + kStatusProfilePayloadSize);
    if (effect[0] == 0) {
      setStatusRgb(effect[1], effect[2], effect[3], effect[7]);
      statusEffect_ = 0;
    } else {
      applyStatusEffect(effect, now);
      statusCondition_ = condition;
    }
  }
  return true;
}

void VirtualBoard::renderStatusEffect() {
  std::array<std::uint8_t, 3> color = statusEffectColor_;
  std::uint8_t brightness = statusEffectBrightness_;
  const auto interpolate = [this](std::size_t channel) {
    const int delta = static_cast<int>(statusEffectAlternate_[channel]) -
                      statusEffectColor_[channel];
    return static_cast<std::uint8_t>(
        static_cast<int>(statusEffectColor_[channel]) +
        delta * statusEffectPhase_ / 255);
  };
  if (statusEffect_ == 1) {
    const std::uint8_t triangle =
        statusEffectPhase_ < 128
            ? static_cast<std::uint8_t>(statusEffectPhase_ * 2U)
            : static_cast<std::uint8_t>((255U - statusEffectPhase_) * 2U);
    brightness = static_cast<std::uint8_t>(
        statusEffectMinimum_ +
        (static_cast<unsigned>(statusEffectBrightness_ - statusEffectMinimum_) *
         triangle) /
            255U);
  } else if (statusEffect_ == 2 && statusEffectPhase_ >= 128) {
    color = statusEffectAlternate_;
  } else if (statusEffect_ == 3) {
    const std::uint8_t triangle =
        statusEffectPhase_ < 128
            ? static_cast<std::uint8_t>(statusEffectPhase_ * 2U)
            : static_cast<std::uint8_t>((255U - statusEffectPhase_) * 2U);
    const std::uint8_t savedPhase = statusEffectPhase_;
    statusEffectPhase_ = triangle;
    color = {{interpolate(0), interpolate(1), interpolate(2)}};
    statusEffectPhase_ = savedPhase;
  } else if (statusEffect_ == 4) {
    color = {{interpolate(0), interpolate(1), interpolate(2)}};
  }
  setStatusRgb(color[0], color[1], color[2], brightness);
}

void VirtualBoard::finishStatusEffect() {
  if (statusEffect_ == 4) {
    statusEffectColor_ = statusEffectAlternate_;
  }
  statusEffect_ = 0;
  setStatusRgb(statusEffectColor_[0], statusEffectColor_[1],
               statusEffectColor_[2], statusEffectBrightness_);
}

void VirtualBoard::serviceStatusEffect(TimePoint now) {
  if (statusEffect_ == 0 ||
      now - lastStatusEffectAt_ <
          std::chrono::milliseconds(statusEffectStepMs_)) {
    return;
  }
  lastStatusEffectAt_ = now;
  const std::uint8_t next =
      static_cast<std::uint8_t>(statusEffectPhase_ + 8U);
  if (next < statusEffectPhase_ && statusEffectRepeats_ != 0 &&
      --statusEffectRepeats_ == 0) {
    finishStatusEffect();
    return;
  }
  statusEffectPhase_ = next;
  renderStatusEffect();
}

void VirtualBoard::showScheduledSegmentWindow() {
  if (!scheduledSegmentActive_ || scheduledSegmentWaiting_) {
    return;
  }
  const bool scrolling = scheduledSegmentText_.size() > 4 ||
                         (scheduledSegmentOptions_ &
                          kSegmentForceScroll) != 0;
  if (!scrolling) {
    displays_.setSegments(scheduledSegmentText_);
    return;
  }
  std::string window(4, ' ');
  for (std::size_t index = 0; index < window.size(); ++index) {
    const std::size_t source = scheduledSegmentIndex_ + index;
    if (source < scheduledSegmentText_.size()) {
      window[index] = scheduledSegmentText_[source];
    }
  }
  displays_.setSegments(window);
}

void VirtualBoard::clearScheduledSegments(bool restoreMenu) {
  scheduledSegmentText_.clear();
  scheduledSegmentIndex_ = 0;
  scheduledSegmentOptions_ = 0;
  scheduledSegmentActive_ = false;
  scheduledSegmentWaiting_ = false;
  segmentDeadlineActive_ = false;
  if (restoreMenu && macroState_ != 1 && macroState_ != 2) {
    updateMenuDisplay();
  }
}

void VirtualBoard::serviceAutomation(TimePoint now) {
  serviceStatusEffect(now);
  if (learningActive_ && learningMode_ == kLearnModeTimer) {
    const std::uint8_t remaining = learningRemainingSeconds(now);
    if (remaining == 0) {
      endLearning(0);
    } else if (remaining != learningReportedRemaining_) {
      learningReportedRemaining_ = remaining;
      const std::uint8_t count = static_cast<std::uint8_t>(std::count_if(
          remotes_.begin(), remotes_.end(),
          [](const RemoteEntry &entry) { return entry.used; }));
      queueEvent({9, 4, count, learningMode_, learningTotalSeconds_,
                  remaining});
    }
  }
  if (scheduledSegmentActive_ && segmentDeadlineActive_ &&
      now >= segmentDeadline_) {
    const bool scrolling = scheduledSegmentText_.size() > 4 ||
                           (scheduledSegmentOptions_ &
                            kSegmentForceScroll) != 0;
    const std::uint8_t repeat =
        scheduledSegmentOptions_ & kSegmentRepeatMask;
    if (scheduledSegmentWaiting_) {
      scheduledSegmentWaiting_ = false;
      scheduledSegmentOptions_ &=
          static_cast<std::uint8_t>(~kSegmentIntervalWaiting);
      scheduledSegmentIndex_ = 0;
      showScheduledSegmentWindow();
      segmentDeadlineActive_ = scrolling || scheduledSegmentHoldMs_ != 0;
      if (segmentDeadlineActive_) {
        segmentDeadline_ = now + std::chrono::milliseconds(
                                     scrolling ? scheduledSegmentStepMs_
                                               : scheduledSegmentHoldMs_);
      }
    } else if (scrolling) {
      if (scheduledSegmentIndex_ < scheduledSegmentText_.size()) {
        ++scheduledSegmentIndex_;
        showScheduledSegmentWindow();
        segmentDeadline_ =
            now + std::chrono::milliseconds(scheduledSegmentStepMs_);
      } else if (repeat == 1) {
        scheduledSegmentIndex_ = 0;
        showScheduledSegmentWindow();
        segmentDeadline_ =
            now + std::chrono::milliseconds(scheduledSegmentStepMs_);
      } else if (repeat == 2) {
        scheduledSegmentWaiting_ = true;
        scheduledSegmentOptions_ |= kSegmentIntervalWaiting;
        updateMenuDisplay();
        segmentDeadline_ =
            now + std::chrono::seconds(scheduledSegmentIntervalSeconds_);
      } else {
        clearScheduledSegments(true);
      }
    } else if (repeat == 2) {
      scheduledSegmentWaiting_ = true;
      scheduledSegmentOptions_ |= kSegmentIntervalWaiting;
      updateMenuDisplay();
      segmentDeadline_ =
          now + std::chrono::seconds(scheduledSegmentIntervalSeconds_);
    } else {
      clearScheduledSegments(true);
    }
  } else if (segmentDeadlineActive_ && now >= segmentDeadline_) {
    segmentDeadlineActive_ = false;
    if (macroState_ != 1 && macroState_ != 2) {
      updateMenuDisplay();
    }
  }
  if (buzzerDeadlineActive_ && now >= buzzerDeadline_) {
    buzzerDeadlineActive_ = false;
    displays_.setBuzzer(0, 0);
  }
  if (i2cLeaseAddress_ != 0 && now >= i2cLeaseDeadline_) {
    i2cLeaseAddress_ = 0;
  }
  if (remoteMomentaryKind_ != 0 && now >= remoteMomentaryDeadline_) {
    stopRemoteMomentary(now);
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
    captureRelayState();
  }

  if ((settings_.flags & kSettingsProgramming) == 0 &&
      now - lastFadeAt_ >= std::chrono::milliseconds(20)) {
    lastFadeAt_ = now;
    const SensorReadings sensor = sensors_.readings();
    std::uint8_t target = settings_.illuminationOffBrightness;
    if (settings_.illuminationMode == 2 ||
        (settings_.illuminationMode == 1 && sensor.doorOpen)) {
      target = settings_.illuminationOnBrightness;
    }
    enclosureBrightness_ = easedByte(enclosureBrightness_, target);
    pwm_.set(11, scale8(enclosureBrightness_));
  }
  if ((settings_.flags & kSettingsProgramming) == 0) {
    pwm_.set(12, 4095);
  }
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
