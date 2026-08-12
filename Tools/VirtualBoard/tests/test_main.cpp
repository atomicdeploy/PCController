#include "virtual_board/hardware.hpp"
#include "virtual_board/protocol.hpp"
#include "virtual_board/virtual_board.hpp"
#include "../../../Project/StatusLedMath.h"
#include "status_led_golden.hpp"

#include <algorithm>
#include <array>
#include <chrono>
#include <cstdint>
#include <filesystem>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

namespace pccontroller::virtual_board {

struct VirtualBoardStatusLedTestAccess {
  static VirtualBoard::TimePoint now(VirtualBoard &board) {
    const auto at = VirtualBoard::Clock::now();
    board.bootEndsAt_ = at;
    board.hostSeen_ = true;
    board.lastHostActivityAt_ = at;
    board.lastStatusLedPushAt_ = at;
    return at;
  }

  static VirtualBoard::TimePoint bootNow(VirtualBoard &board) {
    const auto at = VirtualBoard::Clock::now();
    board.bootEndsAt_ = at + std::chrono::milliseconds(650);
    board.lastStatusLedPushAt_ = at;
    board.restoreStatusPresentation(at);
    return at;
  }

  static bool apply(VirtualBoard &board,
                    const std::array<std::uint8_t, 12> &descriptor,
                    VirtualBoard::TimePoint at) {
    return board.applyStatusEffect(
        std::vector<std::uint8_t>(descriptor.begin(), descriptor.end()), at);
  }

  static std::vector<wire::Frame> tickAt(VirtualBoard &board,
                                         VirtualBoard::TimePoint at) {
    if (board.hostSeen_) {
      board.lastHostActivityAt_ = at;
    }
    board.serviceAutomation(at);
    board.queueMirrorChanges(at);
    std::vector<wire::Frame> frames;
    frames.swap(board.pendingEvents_);
    return frames;
  }

  static std::uint8_t effect(const VirtualBoard &board) {
    return board.statusEffect_;
  }

  static VirtualBoard::TimePoint resetDeadline(const VirtualBoard &board) {
    return board.resetDeadline_;
  }

  static bool resetPending(const VirtualBoard &board) {
    return board.resetPending_;
  }

  static void setFallbackBrightness(VirtualBoard &board,
                                    std::uint8_t brightness) {
    board.settings_.statusBrightness = brightness;
  }
};

} // namespace pccontroller::virtual_board

namespace {

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

bool timedEventEquals(const pccontroller::wire::Frame &event,
                      std::initializer_list<std::uint8_t> expected) {
  if (event.opcode != pccontroller::wire::Event ||
      event.payload.size() != expected.size() + 4U || expected.size() == 0) {
    return false;
  }
  auto actual = event.payload.begin();
  auto wanted = expected.begin();
  if ((*actual & 0x7FU) != *wanted) {
    return false;
  }
  return std::equal(expected.begin() + 1, expected.end(), actual + 1);
}

const pccontroller::wire::Frame *findOpcode(
    const std::vector<pccontroller::wire::Frame> &frames,
    std::uint8_t opcode) {
  const auto found = std::find_if(
      frames.begin(), frames.end(),
      [opcode](const auto &frame) { return frame.opcode == opcode; });
  return found == frames.end() ? nullptr : &*found;
}

std::filesystem::path temporaryEeprom() {
  const auto nonce =
      std::chrono::steady_clock::now().time_since_epoch().count();
  return std::filesystem::temp_directory_path() /
         ("pccontroller-virtual-board-" + std::to_string(nonce) + ".bin");
}

void writeVirtualResetRecord(pccontroller::virtual_board::FileEeprom &eeprom,
                             std::uint8_t slot, std::uint32_t count,
                             std::uint8_t marker = 0xA7,
                             bool corruptChecksum = false) {
  constexpr std::size_t kJournalAddress = 336;
  constexpr std::size_t kRecordBytes = 6;
  const std::size_t address =
      kJournalAddress + static_cast<std::size_t>(slot) * kRecordBytes;
  const std::uint8_t bytes[] = {
      static_cast<std::uint8_t>(count),
      static_cast<std::uint8_t>(count >> 8U),
      static_cast<std::uint8_t>(count >> 16U),
      static_cast<std::uint8_t>(count >> 24U),
  };
  for (std::size_t index = 0; index < sizeof(bytes); ++index) {
    eeprom.update(address + index, bytes[index]);
  }
  eeprom.update(address + 4U,
                static_cast<std::uint8_t>(
                    pccontroller::wire::crc8(bytes, sizeof(bytes)) ^
                    (corruptChecksum ? 0x5AU : 0U)));
  eeprom.update(address + 5U, marker);
}

std::uint32_t virtualResetCount(
    pccontroller::virtual_board::VirtualBoard &board) {
  const auto status =
      board.handle({pccontroller::wire::GetStatus, 0, {}});
  return static_cast<std::uint32_t>(status[0].payload[44]) |
         (static_cast<std::uint32_t>(status[0].payload[45]) << 8U) |
         (static_cast<std::uint32_t>(status[0].payload[46]) << 16U) |
         (static_cast<std::uint32_t>(status[0].payload[47]) << 24U);
}

void testProtocolRoundTrip() {
  pccontroller::wire::Frame source{
      pccontroller::wire::DisplayText, 73, {0, 0, 2, 0, 3, 0, 4}};
  const auto encoded = pccontroller::wire::encode(source);
  const auto decoded =
      pccontroller::wire::decode(encoded.data(), encoded.size());
  require(static_cast<bool>(decoded), "protocol round trip did not decode");
  require(decoded.frame.opcode == source.opcode, "opcode changed in transit");
  require(decoded.frame.sequence == source.sequence,
          "sequence changed in transit");
  require(decoded.frame.payload == source.payload,
          "payload changed in transit");

  std::vector<std::uint8_t> advisoryRaw;
  require(pccontroller::wire::cobsDecode(
              encoded.data(), encoded.size() - 1U, advisoryRaw),
          "could not decode advisory-revision fixture");
  advisoryRaw[1] = 0x7F;
  advisoryRaw.back() = pccontroller::wire::crc8(
      advisoryRaw.data(), advisoryRaw.size() - 1U);
  auto advisoryEncoded = pccontroller::wire::cobsEncode(
      advisoryRaw.data(), advisoryRaw.size());
  advisoryEncoded.push_back(0);
  require(static_cast<bool>(pccontroller::wire::decode(
              advisoryEncoded.data(), advisoryEncoded.size())),
          "advisory envelope revision was treated as a protocol version");

  pccontroller::wire::Frame maximum;
  maximum.opcode = pccontroller::wire::GetStatus;
  maximum.sequence = 1;
  maximum.payload.resize(pccontroller::wire::kMaximumPayload);
  for (std::size_t index = 0; index < maximum.payload.size(); ++index) {
    maximum.payload[index] = static_cast<std::uint8_t>(index);
  }
  const auto maximumEncoded = pccontroller::wire::encode(maximum);
  require(static_cast<bool>(pccontroller::wire::decode(
              maximumEncoded.data(), maximumEncoded.size())),
          "maximum payload did not round trip");

  auto corrupt = encoded;
  corrupt[corrupt.size() - 2] ^= 0x01U;
  const auto rejected =
      pccontroller::wire::decode(corrupt.data(), corrupt.size());
  require(!static_cast<bool>(rejected), "corrupt frame was accepted");
}

void testBoardAndPersistence() {
  const auto path = temporaryEeprom();
  std::error_code ignored;
  std::filesystem::remove(path, ignored);

  {
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);

    auto response = board.handle(
        {pccontroller::wire::Hello, 42, {}});
    require(response.size() == 1 &&
                response[0].opcode == pccontroller::wire::HelloResponse &&
                response[0].sequence == 42,
            "HELLO response is invalid");
    require(response[0].payload.size() == 14 &&
                response[0].payload[0] == 3 &&
                response[0].payload[1] == 1 &&
                std::any_of(response[0].payload.begin() + 6,
                            response[0].payload.begin() + 10,
                            [](std::uint8_t value) { return value != 0; }) &&
                std::any_of(response[0].payload.begin() + 10,
                            response[0].payload.end(),
                            [](std::uint8_t value) { return value != 0; }),
            "HELLO compact schema-3 identity is not production-shaped");
    const std::uint32_t capabilities =
        static_cast<std::uint32_t>(response[0].payload[2]) |
        (static_cast<std::uint32_t>(response[0].payload[3]) << 8U) |
        (static_cast<std::uint32_t>(response[0].payload[4]) << 16U) |
        (static_cast<std::uint32_t>(response[0].payload[5]) << 24U);
    require((capabilities & (1UL << 25U)) != 0 &&
                (capabilities & (1UL << 26U)) != 0 &&
                (capabilities & (1UL << 27U)) != 0,
            "HELLO omitted scheduled-display and mirror-push capabilities");

    response = board.handle({pccontroller::wire::MenuLayoutGet, 43, {}});
    require(response.size() == 1 &&
                response[0].opcode == pccontroller::wire::MenuLayoutResponse &&
                response[0].payload.size() == 11 &&
                response[0].payload[0] == 2 &&
                response[0].payload[4] == 0x10 &&
                response[0].payload[10] == 0xDC,
            "MENU_LAYOUT GET is not packed schema 2");
    const std::vector<std::uint8_t> packedLayout{
        2, 14, 0xFE, 0x3F, 0x30, 0x14, 0x52, 0x76, 0xCB, 0x8D, 0xA9};
    response = board.handle(
        {pccontroller::wire::MenuLayoutSet, 44, packedLayout});
    require(response.size() == 1 && response[0].opcode == pccontroller::wire::Ack,
            "packed MENU_LAYOUT SET was not acknowledged");
    response = board.handle({pccontroller::wire::MenuLayoutGet, 45, {}});
    require(response[0].payload == packedLayout,
            "packed MENU_LAYOUT did not round-trip exactly");
    const std::vector<std::uint8_t> legacyLayout{
        1, 14, 0xFF, 0x3F, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
        13};
    response = board.handle(
        {pccontroller::wire::MenuLayoutSet, 46, legacyLayout});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "obsolete schema-1 MENU_LAYOUT baggage was accepted");

    response = board.handle(
        {pccontroller::wire::HostMenuDirectory, 47, {1, 7, 0}});
    require(response[0].opcode == pccontroller::wire::ErrorResponse &&
                response[0].payload[1] == pccontroller::wire::Unsupported,
            "unadvertised host-menu directory extension was not rejected");

    response = board.handle({pccontroller::wire::MenuList, 48, {0, 0xAA}});
    require(response[0].opcode == pccontroller::wire::MenuListResponse &&
                response[0].payload.size() == 46 &&
                response[0].payload[0] == 1 &&
                response[0].payload[1] == 14 &&
                response[0].payload[2] == 7 &&
                response[0].payload[3] == 7,
            "MENU_LIST does not match the paged production schema");

    std::vector<std::uint8_t> capture{3, 0xBC, 0x2A, 36,
                                      'T', 'I', 'M', 'E'};
    const auto appendFixed = [&capture](const std::string &value,
                                       std::size_t width) {
      capture.insert(capture.end(), value.begin(), value.end());
      capture.insert(capture.end(), width - value.size(), ' ');
    };
    appendFixed("Current time", 16);
    appendFixed("15:04:05", 16);
    response = board.handle(
        {pccontroller::wire::DisplayText, 49, capture});
    require(response[0].opcode == pccontroller::wire::Ack &&
                displays.state().segments == "TIME" &&
                displays.state().lcdLine1 == "Current time    " &&
                displays.state().lcdLine2 == "15:04:05        ",
            "host time and short label were not applied to the front panel");
    capture.assign({3, 0xBC, 0x2A, 36, 'D', 'A', 'T', 'E'});
    appendFixed("Current date", 16);
    appendFixed("2026-08-02", 16);
    response = board.handle(
        {pccontroller::wire::DisplayText, 49, capture});
    require(response[0].opcode == pccontroller::wire::Ack &&
                displays.state().segments == "DATE" &&
                displays.state().lcdLine1 == "Current date    " &&
                displays.state().lcdLine2 == "2026-08-02      ",
            "host date and short label were not applied to the front panel");
    response = board.handle(
        {pccontroller::wire::FrontPanelGet, 50, {0xAA}});
    require(response[0].opcode == pccontroller::wire::FrontPanelResponse &&
                response[0].payload.size() == 47 &&
                (response[0].payload[44] & 0x80U) != 0 &&
                (response[0].payload[44] & 0x0FU) == 2 &&
                response[0].payload[45] == 0xBC &&
                response[0].payload[46] == 0x0A,
            "front-panel snapshot omitted capture metadata");
    response = board.handle(
        {pccontroller::wire::DisplayText, 51, {4, 0, 0, 0, 0xAA}});
    require(response[0].opcode == pccontroller::wire::Ack &&
                displays.state().segments == "tLED",
            "target-4 release did not restore the local default page");

    std::vector<std::uint8_t> oversizedLcd{1, 0, 0, 33};
    oversizedLcd.insert(oversizedLcd.end(), 33, 'X');
    response = board.handle(
        {pccontroller::wire::DisplayText, 52, oversizedLcd});
    require(response[0].opcode == pccontroller::wire::Ack &&
                displays.state().lcdLine1 == std::string(16, 'X') &&
                displays.state().lcdLine2 == std::string(16, 'X'),
            "oversized LCD text was not safely truncated to 2x16");

    const std::vector<std::uint8_t> scheduledOnce{
        5, 80, 0, 5, 0, 50, 0, 0, 'H', 'E', 'L', 'L', 'O'};
    response = board.handle(
        {pccontroller::wire::DisplayText, 53, scheduledOnce});
    require(response[0].opcode == pccontroller::wire::Ack &&
                displays.state().segments == "HELL",
            "scheduled marquee did not show its first window");
    auto pushed = board.tick();
    const auto *segmentPush =
        findOpcode(pushed, pccontroller::wire::SegmentChanged);
    require(segmentPush != nullptr && segmentPush->sequence == 0 &&
                segmentPush->payload.size() == 5 &&
                std::any_of(segmentPush->payload.begin(),
                            segmentPush->payload.begin() + 4,
                            [](std::uint8_t value) { return value != 0; }),
            "changed segment state was not pushed without polling");
    for (std::size_t step = 0; step < 5; ++step) {
      std::this_thread::sleep_for(std::chrono::milliseconds(85));
      pushed = board.tick();
    }
    segmentPush = findOpcode(pushed, pccontroller::wire::SegmentChanged);
    require(displays.state().segments == "    " && segmentPush != nullptr &&
                segmentPush->payload.size() == 5 &&
                std::all_of(segmentPush->payload.begin(),
                            segmentPush->payload.begin() + 4,
                            [](std::uint8_t value) { return value == 0; }),
            "marquee omitted the complete terminal blank frame");
    std::this_thread::sleep_for(std::chrono::milliseconds(85));
    pushed = board.tick();
    require(displays.state().segments == "tLED" &&
                findOpcode(pushed, pccontroller::wire::SegmentChanged) !=
                    nullptr,
            "once marquee did not release the local page after its blank frame");

    response = board.handle(
        {pccontroller::wire::DisplayText, 54,
         {5, 80, 0, 1, 4, 50, 0, 0, 'X'}});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "scheduled marquee accepted reserved option bits");

    response = board.handle(
        {pccontroller::wire::Buzzer, 55, {40, 0, 0xB8, 0x01}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "buzzer note was not acknowledged");
    pushed = board.tick();
    const auto *buzzerPush =
        findOpcode(pushed, pccontroller::wire::BuzzerChanged);
    require(buzzerPush != nullptr &&
                buzzerPush->payload ==
                    std::vector<std::uint8_t>({0xB8, 0x01, 40, 0, 0}),
            "buzzer frequency and duration were not pushed to the host");

    response = board.handle(
        {pccontroller::wire::I2cTransfer, 56,
         {0x41, 2, 3, 0, 0x10, 0xAB, 0xCD}});
    require(response[0].opcode == pccontroller::wire::I2cTransferResponse &&
                response[0].payload ==
                    std::vector<std::uint8_t>({0, 0x41, 0}),
            "cooperative I2C write response is invalid");
    response = board.handle(
        {pccontroller::wire::I2cTransfer, 57, {0x41, 0, 1, 2, 0x10}});
    require(response[0].payload ==
                std::vector<std::uint8_t>({0, 0x41, 2, 0xAB, 0xCD}),
            "cooperative I2C repeated-start read did not preserve bytes");
    response = board.handle(
        {pccontroller::wire::I2cTransfer, 58, {0x55, 0, 0, 1}});
    require(response[0].payload ==
                std::vector<std::uint8_t>({2, 0x55, 0}),
            "unconnected I2C address did not return address-NACK status");
    response = board.handle(
        {pccontroller::wire::I2cTransfer, 59, {0, 0, 0, 0}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "I2C lease release was not acknowledged");

    response = board.handle(
        {pccontroller::wire::ProgramState, 57, {1, 0xAA}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "semantic PROGRAM_STATE prefix was not accepted");
    response = board.handle({pccontroller::wire::GetStatus, 58, {}});
    require((static_cast<std::uint16_t>(response[0].payload[24]) |
             (static_cast<std::uint16_t>(response[0].payload[25]) << 8U)) &
                (1U << 13U),
            "running program state was absent from STATUS bit 13");
    response = board.handle(
        {pccontroller::wire::ProgramState, 59, {0}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "idle PROGRAM_STATE was not accepted");

    response =
        board.handle({pccontroller::wire::GetStatus, 8, {}});
    require(response.size() == 1 &&
                response[0].payload.size() == 48 &&
                response[0].payload[43] == 0x01 &&
                response[0].payload[44] == 1 &&
                response[0].payload[45] == 0 &&
                response[0].payload[46] == 0 &&
                response[0].payload[47] == 0,
            "initial reset telemetry is not the 48-byte firmware shape");

    const std::vector<std::uint8_t> settings{
        3, 0x04, 1, 200, 7, 6, 140, 0x0E, 250, 0, 5, 1, 0x4D,
        0xF0, 37};
    response =
        board.handle({pccontroller::wire::SetSettings, 9, settings});
    require(response.size() == 1 &&
                response[0].opcode == pccontroller::wire::Ack,
            "SET_SETTINGS was not acknowledged");
    std::array<std::uint8_t, 40> storedSettings{};
    for (std::size_t index = 0; index < storedSettings.size(); ++index) {
      storedSettings[index] = eeprom.read(32U + index);
    }
    require(storedSettings[6] == 0x0E && storedSettings[28] == 0x4D &&
                storedSettings[29] == 0xF0 && storedSettings[30] == 37 &&
                storedSettings[31] == 0 &&
                eeprom.read(72) == pccontroller::wire::crc8(
                                       storedSettings.data(),
                                       storedSettings.size()),
            "virtual EEPROM is not the current 40-value settings/name record");

    auto namedSettings = settings;
    namedSettings.push_back(7);
    namedSettings.insert(namedSettings.end(), {'E', 'D', 'G', 'E', '-', '0', '1'});
    response = board.handle(
        {pccontroller::wire::SetSettings, 10, namedSettings});
    require(response[0].opcode == pccontroller::wire::Ack,
            "SET_SETTINGS did not persist the board name");
    response = board.handle(
        {pccontroller::wire::SetSettings, 11, settings});
    require(response[0].opcode == pccontroller::wire::Ack,
            "ordinary settings update after board name failed");
    response = board.handle(
        {pccontroller::wire::GetSettings, 12, {}});
    require(response[0].payload.size() == 25 &&
                response[0].payload[16] == 1 &&
                response[0].payload[17] == 7 &&
                std::equal(response[0].payload.begin() + 18,
                           response[0].payload.end(), "EDGE-01"),
            "board name did not survive an ordinary settings update");

    auto invalidSettings = settings;
    invalidSettings.pop_back();
    response = board.handle(
        {pccontroller::wire::SetSettings, 90, invalidSettings});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "truncated SETTINGS payload was accepted");
    invalidSettings = settings;
    invalidSettings[7] = 0x10;
    response = board.handle(
        {pccontroller::wire::SetSettings, 91, invalidSettings});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "reserved output-persistence bits were accepted");
    invalidSettings = settings;
    invalidSettings[0] = 2;
    response = board.handle(
        {pccontroller::wire::SetSettings, 92, invalidSettings});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "alternate SETTINGS shape was accepted");
    invalidSettings = settings;
    invalidSettings[14] = 0;
    response = board.handle(
        {pccontroller::wire::SetSettings, 92, invalidSettings});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "zero motion break was accepted");

    response = board.handle(
        {pccontroller::wire::PwmSet, 93, {0, 0x00, 0x08}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "direct PWM channel control was not acknowledged");
    response = board.handle({pccontroller::wire::PwmGet, 94, {}});
    require(response[0].opcode == pccontroller::wire::PwmValuesResponse &&
                response[0].payload.size() == 34 &&
                response[0].payload[0] == 1 &&
                response[0].payload[1] == 0 &&
                response[0].payload[2] == 0x00 &&
                response[0].payload[3] == 0x08,
            "PWM_VALUES does not report availability and direct values");

    response = board.handle(
        {pccontroller::wire::RelaySet, 10, {4, 1}});
    require(response[0].opcode == pccontroller::wire::Ack &&
                (relays.mask() & (1U << 4U)) != 0,
            "relay interface did not update");

    response = board.handle(
        {pccontroller::wire::AddressableLed, 11, {10, 1, 2, 3, 4}});
    auto strip = addressableLeds.state();
    require(response[0].opcode == pccontroller::wire::Ack &&
                strip.brightness == 4 &&
                strip.pixels[10].red == 1 &&
                strip.pixels[10].green == 2 &&
                strip.pixels[10].blue == 3,
            "addressable LED pixel command was not applied");
    response = board.handle(
        {pccontroller::wire::AddressableLed, 12, {11, 9, 8, 7, 6}});
    require(response[0].opcode == pccontroller::wire::ErrorResponse &&
                addressableLeds.state().brightness == 4,
            "invalid addressable LED pixel was accepted");
    response = board.handle(
        {pccontroller::wire::AddressableLed, 13, {0xFF, 9, 8, 7, 6}});
    strip = addressableLeds.state();
    require(
        response[0].opcode == pccontroller::wire::Ack &&
            strip.brightness == 6 &&
            std::all_of(strip.pixels.begin(), strip.pixels.end(),
                        [](const auto &color) {
                          return color.red == 9 && color.green == 8 &&
                                 color.blue == 7;
                        }),
        "addressable LED fill command was not applied");
    const auto stripConsole = board.console("strip pixel 3 10 20 30 40");
    strip = addressableLeds.state();
    require(stripConsole.message.find("pixel 3") != std::string::npos &&
                strip.brightness == 40 &&
                strip.pixels[3].red == 10 &&
                strip.pixels[3].green == 20 &&
                strip.pixels[3].blue == 30,
            "interactive strip command was not applied");

    response = board.handle(
        {pccontroller::wire::MacroStart, 14, {2, 7, 0, 2, 0}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "MACRO_START was not acknowledged");
    response = board.handle(
        {pccontroller::wire::MacroStep, 12,
         {0, 0, 0, 2, 0,
          0, 0, 0, 0, pccontroller::wire::RelaySet, 2, 5, 1,
          0x40, 0x42, 0x0F, 0, pccontroller::wire::PwmSet, 3, 0, 0xFF,
          0x0F}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "macro stream fragment was not acknowledged");
    response = board.handle(
        {pccontroller::wire::MacroStep, 12, {1}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "macro run was not acknowledged");
    const auto macroEvents = board.tick();
    require((relays.mask() & (1U << 5U)) != 0 &&
                std::any_of(macroEvents.begin(), macroEvents.end(),
                            [](const auto &event) {
                              return event.sequence == 0xFEU &&
                                     event.opcode == pccontroller::wire::Ack;
                            }),
            "timed macro relay step was not applied/evidenced");
    response =
        board.handle({pccontroller::wire::MacroCancel, 13, {0}});
    require(response[0].opcode == pccontroller::wire::Ack &&
                (relays.mask() & (1U << 5U)) == 0,
            "macro cancellation did not release its relay");

    const auto console = board.console("door open");
    require(console.message == "door OPEN", "door console control failed");
    const auto events = board.tick();
    bool sawDoor = false;
    for (const auto &event : events) {
      sawDoor |= event.opcode == pccontroller::wire::Event &&
                 event.payload.size() >= 2 &&
                 (event.payload[0] & 0x7FU) == 2 &&
                 event.payload[1] == 1;
    }
    require(sawDoor, "door change did not emit an asynchronous event");

    require(board.console("key 2 down").message == "key event queued",
            "key-down console control failed");
    const auto keyDownEvents = board.tick();
    require(
        std::any_of(keyDownEvents.begin(), keyDownEvents.end(),
                    [](const auto &event) {
                      return timedEventEquals(event, {1, 1, 5, 0, 0xFF});
                    }),
        "key-down gesture event was not emitted");
    auto keyStatus =
        board.handle({pccontroller::wire::GetStatus, 17, {}});
    require((keyStatus[0].payload[27] & (1U << 1U)) != 0,
            "key-down did not update the active-key status mask");

    require(board.console("key 2 up").message == "key event queued",
            "key-up console control failed");
    const auto keyUpEvents = board.tick();
    require(
        std::any_of(keyUpEvents.begin(), keyUpEvents.end(),
                    [](const auto &event) {
                      return timedEventEquals(event, {1, 1, 6, 0, 0xFF});
                    }),
        "key-up gesture event was not emitted");
    keyStatus = board.handle({pccontroller::wire::GetStatus, 18, {}});
    require((keyStatus[0].payload[27] & (1U << 1U)) == 0,
            "key-up did not clear the active-key status mask");

    response =
        board.handle({pccontroller::wire::RadioLearnStart, 19, {1}});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "obsolete one-byte RF learn request was accepted");
    response =
        board.handle({pccontroller::wire::RadioLearnStart, 19, {1, 10}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "timed multi-code RF learning did not start");
    const auto learningStartedEvents = board.tick();
    require(std::any_of(learningStartedEvents.begin(),
                        learningStartedEvents.end(), [](const auto &event) {
              return timedEventEquals(event, {9, 3, 0, 1, 10, 10});
            }),
            "timed RF learning did not emit its exact start lifecycle event");
    require(board.console("rflearn 0x11223344 24 1 350")
                    .message.find("entry 0") != std::string::npos,
            "virtual RF entry was not learned");
    static_cast<void>(board.tick());
    response = board.handle(
        {pccontroller::wire::RadioLearnCancel, 20, {}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "timed RF learning did not cancel after capture");
    const auto learningCanceledEvents = board.tick();
    require(std::any_of(learningCanceledEvents.begin(),
                        learningCanceledEvents.end(), [](const auto &event) {
              return timedEventEquals(event, {9, 1, 1, 1, 10, 10});
            }),
            "timed RF learning did not emit its exact cancel lifecycle event");

    require(board.console("rfrecv 0x11223344 24 1 350")
                    .message.find("learned_id=0") != std::string::npos,
            "matched raw RF console control failed");
    const auto learnedRfEvents = board.tick();
    require(
        std::any_of(
            learnedRfEvents.begin(), learnedRfEvents.end(),
            [](const auto &event) {
              return timedEventEquals(
                  event,
                  {8, 0x44, 0x33, 0x22, 0x11, 24, 1, 0x5E, 0x01, 0});
            }),
        "matched raw RF receive event payload is invalid");

    require(board.console("rfrecv 0x55667788 24 1 400")
                    .message.find("learned_id=none") != std::string::npos,
            "unmatched raw RF console control failed");
    const auto unmatchedRfEvents = board.tick();
    require(
        std::any_of(
            unmatchedRfEvents.begin(), unmatchedRfEvents.end(),
            [](const auto &event) {
              return timedEventEquals(
                  event,
                  {8, 0x88, 0x77, 0x66, 0x55, 24, 1, 0x90, 0x01, 0xFF});
            }),
        "unmatched raw RF receive event payload is invalid");

    response = board.handle(
        {pccontroller::wire::RadioLearnList, 190, {0}});
    require(response[0].payload.size() >= 16 &&
                response[0].payload[13] == 0 &&
                response[0].payload[14] == 0 &&
                response[0].payload[15] == 0,
            "new learned RF entry was not left deliberately unmapped");
    response = board.handle(
        {pccontroller::wire::RadioLearnReplace, 190,
         {0, 0x44, 0x33, 0x22, 0x11, 24, 1, 0x5E, 0x01, 3, 4, 1}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "complete RF relay-record replacement was not acknowledged");
    static_cast<void>(board.console("rfrecv 0x11223344 24 1 350"));
    static_cast<void>(board.tick());
    require((relays.mask() & (1U << 4U)) != 0,
            "mapped RF toggle did not execute through the relay path");
    static_cast<void>(board.console("rfrecv 0x11223344 24 1 350"));
    static_cast<void>(board.tick());
    require((relays.mask() & (1U << 4U)) != 0,
            "repeated RF frame retriggered a non-refreshable mapping");

    response = board.handle(
        {pccontroller::wire::RadioLearnReplace, 190,
         {0, 0x44, 0x33, 0x22, 0x11, 24, 1, 0x5E, 0x01, 3, 5, 2}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "complete RF momentary record replacement was not acknowledged");
    static_cast<void>(board.console("rfrecv 0x11223344 24 1 350"));
    static_cast<void>(board.tick());
    require((relays.mask() & (1U << 5U)) != 0,
            "mapped RF momentary action did not turn its relay on");
    std::this_thread::sleep_for(std::chrono::milliseconds(370));
    static_cast<void>(board.tick());
    require((relays.mask() & (1U << 5U)) == 0,
            "mapped RF momentary action did not expire locally");

    response = board.handle(
        {pccontroller::wire::RadioLearnStart, 191, {0, 0}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "indefinite multi-code RF learning was not accepted");
    response = board.handle(
        {pccontroller::wire::RadioLearnStart, 191, {0, 0, 0xAA}});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "obsolete RF-learning positional tail was accepted");
    for (std::uint8_t id = 1; id < 20; ++id) {
      const std::uint32_t code = 0x12000000U + id;
      const auto learned = board.console(
          "rflearn " + std::to_string(code) + " 24 1 350");
      require(learned.message.find("entry " + std::to_string(id)) !=
                  std::string::npos,
              "20-slot RF store did not allocate stable ID " +
                  std::to_string(id));
    }
    const auto fullRfEvents = board.tick();
    require(std::any_of(fullRfEvents.begin(), fullRfEvents.end(),
                        [](const auto &event) {
                          return timedEventEquals(event,
                                                  {9, 2, 20, 0, 0, 0});
                        }),
            "RF learning did not emit a clear full/end event at 20 entries");
    response = board.handle(
        {pccontroller::wire::RadioLearnList, 192, {0}});
    require(response[0].payload.size() >= 16 &&
                response[0].payload[1] == 20,
            "RF list did not retain all 20 entries after mapping");
    response = board.handle(
        {pccontroller::wire::RadioLearnList, 193, {19}});
    require(response[0].payload[1] == 20 && response[0].payload[2] == 0xFF &&
                response[0].payload[3] == 1 &&
                response[0].payload[4] == 19 &&
                response[0].payload[13] == 0 &&
                response[0].payload[14] == 0 &&
                response[0].payload[15] == 0,
            "RF slot 19 is absent or was not left deliberately unmapped");

    response = board.handle(
        {pccontroller::wire::Reset, 20, {0}});
    require(response.size() == 1 &&
                response[0].opcode == pccontroller::wire::Ack,
            "application reset was not acknowledged");
    const auto applicationResetDeadline = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::resetDeadline(board);
    auto resetEvents = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(
            board, applicationResetDeadline - std::chrono::milliseconds(220));
    const auto *resetCue =
        findOpcode(resetEvents, pccontroller::wire::StatusLedChanged);
    require(resetCue != nullptr && resetCue->payload.size() == 6 &&
                resetCue->payload[5] == 18 &&
                pccontroller::virtual_board::
                    VirtualBoardStatusLedTestAccess::resetPending(board),
            "application reset did not render its 240 ms condition-18 cue");
    resetEvents = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(
            board, applicationResetDeadline - std::chrono::milliseconds(1));
    require(std::none_of(resetEvents.begin(), resetEvents.end(),
                         [](const auto &event) {
                           return timedEventEquals(event,
                                                   {7, 0x08, 2, 0, 0, 0});
                         }),
            "application reset completed before its cue deadline");
    resetEvents = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(board,
                                                 applicationResetDeadline);
    require(
        std::any_of(
            resetEvents.begin(), resetEvents.end(),
            [](const auto &event) {
              return timedEventEquals(event, {7, 0x08, 2, 0, 0, 0});
            }),
        "reset event is not [7, cause, persistent count LE u32]");
    const auto bootFrames = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(
            board, applicationResetDeadline + std::chrono::milliseconds(17));
    const auto *bootFrame =
        findOpcode(bootFrames, pccontroller::wire::StatusLedChanged);
    require(bootFrame != nullptr && bootFrame->payload.size() == 6 &&
                bootFrame->payload[5] == 1 &&
                !pccontroller::virtual_board::
                     VirtualBoardStatusLedTestAccess::resetPending(board),
            "simulated reboot did not clear reset ownership and begin Boot");
    response =
        board.handle({pccontroller::wire::GetStatus, 21, {}});
    require(response[0].payload.size() == 48 &&
                response[0].payload[43] == 0x08 &&
                response[0].payload[44] == 2,
            "reset command did not update STATUS reset telemetry");

    response = board.handle(
        {pccontroller::wire::Reset, 22, {1}});
    require(response.size() == 1 &&
                response[0].opcode == pccontroller::wire::Ack,
            "bootloader reset was not acknowledged");
    const auto bootloaderResetDeadline = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::resetDeadline(board);
    const auto bootloaderResetEvents = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(board,
                                                bootloaderResetDeadline);
    require(
        std::any_of(
            bootloaderResetEvents.begin(), bootloaderResetEvents.end(),
            [](const auto &event) {
              return timedEventEquals(event, {7, 0x08, 3, 0, 0, 0});
            }),
        "bootloader reset did not advance persistent reset telemetry");
  }

  {
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    const auto response =
        board.handle({pccontroller::wire::GetSettings, 14, {}});
    require(response.size() == 1 &&
                response[0].opcode == pccontroller::wire::SettingsResponse,
            "GET_SETTINGS did not return settings");
    require(response[0].payload.size() == 25 &&
                response[0].payload[3] == 200 &&
                response[0].payload[10] == 5 &&
                response[0].payload[11] == 1 &&
                response[0].payload[12] == 0x4D &&
                response[0].payload[13] == relays.mask() &&
                response[0].payload[14] == 37 &&
                response[0].payload[15] == 1 &&
                response[0].payload[16] == 1 &&
                response[0].payload[17] == 7 &&
                std::equal(response[0].payload.begin() + 18,
                           response[0].payload.end(), "EDGE-01"),
            "virtual MCU EEPROM did not retain settings");
    require((relays.mask() & (1U << 4U)) != 0 &&
                pwm.value(0) == 2056,
            "output-persistence policies did not restore relay/PWM state");

    const auto status =
        board.handle({pccontroller::wire::GetStatus, 15, {}});
    require(status.size() == 1 && status[0].payload.size() == 48 &&
                status[0].payload[43] == 0x01 &&
                status[0].payload[44] == 4 &&
                status[0].payload[45] == 0 &&
                status[0].payload[46] == 0 &&
                status[0].payload[47] == 0,
            "STATUS/reset-count persistence is not wire-compatible");
    const auto temperatures =
        board.handle({pccontroller::wire::TemperatureList, 16, {}});
    require(temperatures.size() == 1 &&
                temperatures[0].payload.size() == 24 &&
                temperatures[0].payload[0] == 1 &&
                temperatures[0].payload[1] == 2,
            "temperature identity list is invalid");
    const auto layout =
        board.handle({pccontroller::wire::MenuLayoutGet, 17, {}});
    require(layout[0].payload == std::vector<std::uint8_t>(
                                     {2, 14, 0xFE, 0x3F, 0x30, 0x14,
                                      0x52, 0x76, 0xCB, 0x8D, 0xA9}),
            "MCU-owned menu visibility/order did not persist in EEPROM");
    const auto remotes =
        board.handle({pccontroller::wire::RadioLearnList, 18, {19}});
    require(remotes[0].payload[1] == 20 &&
                remotes[0].payload[3] == 1 &&
                remotes[0].payload[4] == 19,
            "20 learned RF records did not persist in virtual MCU EEPROM");
  }

  std::filesystem::remove(path, ignored);
}

void testPwmAvailabilityReporting() {
  const auto path = temporaryEeprom();
  std::error_code ignored;
  std::filesystem::remove(path, ignored);
  {
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm(false);
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);

    auto response = board.handle({pccontroller::wire::GetStatus, 1, {}});
    const std::uint16_t flags = static_cast<std::uint16_t>(
        response[0].payload[24] |
        (static_cast<std::uint16_t>(response[0].payload[25]) << 8U));
    require((flags & (1U << 1U)) == 0 && response[0].payload[33] == 0,
            "STATUS claimed an unavailable PWM controller");
    response = board.handle({pccontroller::wire::PwmGet, 2, {}});
    require(response[0].payload.size() == 34 &&
                response[0].payload[0] == 0,
            "PWM_VALUES did not publish unavailable state");
    response = board.handle(
        {pccontroller::wire::PwmSet, 3, {0, 1, 0}});
    require(response[0].opcode == pccontroller::wire::ErrorResponse &&
                response[0].payload[1] ==
                    pccontroller::wire::HardwareUnavailable,
            "direct PWM write did not report unavailable hardware");
  }
  std::filesystem::remove(path, ignored);
}

void testStatusLedOwnerFramesAndIdempotentBreathe() {
  const auto path = temporaryEeprom();
  std::error_code ignored;
  std::filesystem::remove(path, ignored);
  {
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    static_cast<void>(
        pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::now(
            board));

    // Manual condition (0xFF) plus a non-zero effect is the stable six-byte
    // wire contract for an MCU-owned compositor; steady host preview uses
    // the same condition with effect 0. This preserves existing parsers.
    const std::array<std::uint8_t, 12> breathe{{
        1, 0, 0, 255, 0, 0, 0, 255, 0, 0x80, 0x02, 0}};
    const auto base =
        pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::now(
            board);
    require(pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::apply(
                board, breathe, base),
            "VirtualBoard rejected a native STATUS_EFFECT descriptor");

    std::vector<std::uint8_t> blueFrames;
    for (unsigned frame = 0; frame < 32; ++frame) {
      if ((frame % 2U) == 0U) {
        const auto response = board.handle(
            {pccontroller::wire::StatusEffect,
             static_cast<std::uint8_t>(2U + frame),
             std::vector<std::uint8_t>(breathe.begin(), breathe.end())});
        require(response[0].opcode == pccontroller::wire::Ack,
                "VirtualBoard rejected an identical STATUS_EFFECT refresh");
      }
      const auto frames = pccontroller::virtual_board::
          VirtualBoardStatusLedTestAccess::tickAt(
              board, base + std::chrono::milliseconds((frame + 1U) * 20U));
      const auto *changed =
          findOpcode(frames, pccontroller::wire::StatusLedChanged);
      const bool validFrame = changed != nullptr && changed->payload.size() == 6 &&
                              changed->payload[4] == 1 &&
                              changed->payload[5] == 0xFF;
      require(validFrame,
              "VirtualBoard did not return rendered MCU-owned LED frame " +
                  std::to_string(frame));
      blueFrames.push_back(changed->payload[2]);
    }

    const auto peak = std::max_element(blueFrames.begin(), blueFrames.end());
    require(peak != blueFrames.end() && *peak > 240U &&
                blueFrames.back() < 25U,
            "repeated STATUS_EFFECT refresh did not complete one rise/fall");
    require(std::is_sorted(peak, blueFrames.end(), std::greater_equal<>()),
            "repeated STATUS_EFFECT reset the fall phase");

    const std::array<std::uint8_t, 12> changed{{
        1, 255, 0, 0, 0, 0, 0, 200, 50, 0x00, 0x05, 0}};
    require(pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::apply(
                board, changed, base + std::chrono::milliseconds(650)),
            "same-kind changed descriptor was rejected");
    const auto replacementFrames = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(
            board, base + std::chrono::milliseconds(670));
    const auto *replacement =
        findOpcode(replacementFrames, pccontroller::wire::StatusLedChanged);
    require(replacement != nullptr && replacement->payload.size() == 6 &&
                replacement->payload[0] > 20 && replacement->payload[2] == 0 &&
                replacement->payload[4] == 1 &&
                replacement->payload[5] == 0xFF,
            "same-kind changed descriptor did not replace atomically");

    const auto redBeforeRelease = pwm.value(13);
    const auto response = board.handle(
        {pccontroller::wire::StatusEffect, 41, {0}});
    require(response[0].opcode == pccontroller::wire::Ack &&
                pwm.value(13) == redBeforeRelease,
            "explicit release changed the rendered frame before handoff");
  }
  std::filesystem::remove(path, ignored);
}

std::array<std::uint8_t, 3> renderedStatus(
    const pccontroller::virtual_board::PwmBank &pwm) {
  return {{static_cast<std::uint8_t>(pwm.value(13) >> 4U),
           static_cast<std::uint8_t>(pwm.value(14) >> 4U),
           static_cast<std::uint8_t>(pwm.value(15) >> 4U)}};
}

void testStatusLedGoldenCadenceAndDurationParity() {
  for (std::size_t index = 0;
       index < status_led_golden::interpolationPhases.size(); ++index) {
    const auto phase = status_led_golden::interpolationPhases[index];
    require(StatusLedMath::interpolate(240, 20, phase) ==
                    status_led_golden::descending[index] &&
                StatusLedMath::interpolate(20, 240, phase) ==
                    status_led_golden::ascending[index],
            "VirtualBoard bidirectional interpolation diverged from golden "
            "vector");
  }

  for (std::uint8_t kind = 1; kind <= 4; ++kind) {
    const auto path = temporaryEeprom();
    std::error_code ignored;
    std::filesystem::remove(path, ignored);
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    const auto base =
        pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::now(
            board);
    const auto descriptor = status_led_golden::descriptor(kind);
    require(pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::apply(
                board, descriptor, base),
            "VirtualBoard golden descriptor was rejected");
    for (const auto &golden : status_led_golden::frames) {
      const auto at = base + std::chrono::milliseconds(
                                 StatusLedMath::phaseDeadline(
                                     640, static_cast<std::uint8_t>(
                                              golden.phase >> 2U)));
      static_cast<void>(pccontroller::virtual_board::
                            VirtualBoardStatusLedTestAccess::tickAt(board, at));
      const auto expected =
          kind == 1 ? golden.breathe
                    : (kind == 2 ? golden.flash
                                 : (kind == 3 ? golden.cycle
                                              : golden.transition));
      const auto actual = renderedStatus(pwm);
      require(actual == expected,
              "VirtualBoard diverged from AVR golden phase vector kind=" +
                  std::to_string(kind) + " phase=" +
                  std::to_string(golden.phase) + " actual=" +
                  std::to_string(actual[0]) + "," +
                  std::to_string(actual[1]) + "," +
                  std::to_string(actual[2]) + " expected=" +
                  std::to_string(expected[0]) + "," +
                  std::to_string(expected[1]) + "," +
                  std::to_string(expected[2]));
    }
    static_cast<void>(pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::tickAt(
                              board, base + std::chrono::milliseconds(639)));
    const auto terminal =
        kind == 1 ? status_led_golden::frames[4].breathe
                  : (kind == 2 ? status_led_golden::frames[4].flash
                               : (kind == 3
                                      ? status_led_golden::frames[4].cycle
                                      : status_led_golden::frames[4].transition));
    require(renderedStatus(pwm) == terminal,
            "VirtualBoard effect missed its period-1 terminal phase");
    static_cast<void>(pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::tickAt(
                              board, base + std::chrono::milliseconds(640)));
    const auto primary =
        kind == 1 ? status_led_golden::frames[0].breathe
                  : (kind == 2 ? status_led_golden::frames[0].flash
                               : (kind == 3
                                      ? status_led_golden::frames[0].cycle
                                      : status_led_golden::frames[0].transition));
    require(renderedStatus(pwm) == primary,
            "VirtualBoard effect did not render phase zero at exact period");
    static_cast<void>(pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::tickAt(
                              board, base + std::chrono::milliseconds(650)));
    const auto first =
        kind == 1 ? status_led_golden::breatheFirstStep
                  : (kind == 2 ? status_led_golden::flashFirstStep
                               : (kind == 3
                                      ? status_led_golden::cycleFirstStep
                                      : status_led_golden::transitionFirstStep));
    const auto firstActual = renderedStatus(pwm);
    require(firstActual == first,
            "VirtualBoard effect missed first post-wrap deadline kind=" +
                std::to_string(kind) + " actual=" +
                std::to_string(firstActual[0]) + "," +
                std::to_string(firstActual[1]) + "," +
                std::to_string(firstActual[2]));
    static_cast<void>(pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::tickAt(
                              board, base + std::chrono::milliseconds(1930)));
    require(renderedStatus(pwm) == first,
            "VirtualBoard delayed multi-cycle service lost absolute phase");
    std::filesystem::remove(path, ignored);
  }

  const std::array<std::uint16_t, 4> periods{{640, 1280, 3200, 60000}};
  for (const auto period : periods) {
    const auto run = [&](bool delayed) {
      const auto path = temporaryEeprom();
      std::error_code ignored;
      std::filesystem::remove(path, ignored);
      pccontroller::virtual_board::SensorBank sensors;
      pccontroller::virtual_board::RelayBank relays;
      pccontroller::virtual_board::PwmBank pwm;
      pccontroller::virtual_board::AddressableLedBank addressableLeds;
      pccontroller::virtual_board::DisplayBank displays;
      pccontroller::virtual_board::FileEeprom eeprom(path);
      pccontroller::virtual_board::VirtualBoard board(
          sensors, relays, pwm, addressableLeds, displays, eeprom);
      const auto base =
          pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::now(
              board);
      auto descriptor = status_led_golden::descriptor(4, 1);
      descriptor[9] = static_cast<std::uint8_t>(period);
      descriptor[10] = static_cast<std::uint8_t>(period >> 8U);
      require(pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::
                  apply(board, descriptor, base),
              "VirtualBoard finite descriptor was rejected");
      if (!delayed) {
        static_cast<void>(pccontroller::virtual_board::
                              VirtualBoardStatusLedTestAccess::tickAt(
                                  board, base +
                                             std::chrono::milliseconds(
                                                 period - 1U)));
        require(pccontroller::virtual_board::
                    VirtualBoardStatusLedTestAccess::effect(board) == 4,
                "VirtualBoard finite effect completed before exact period");
      }
      static_cast<void>(pccontroller::virtual_board::
                            VirtualBoardStatusLedTestAccess::tickAt(
                                board, base + std::chrono::milliseconds(
                                                   period +
                                                   (delayed ? 9U : 0U))));
      require(pccontroller::virtual_board::
                      VirtualBoardStatusLedTestAccess::effect(board) == 0 &&
                  renderedStatus(pwm) ==
                      status_led_golden::transitionEndpoint,
              "VirtualBoard finite effect missed its terminal endpoint");
      const auto response = board.handle(
          {pccontroller::wire::StatusEffect, 77,
           std::vector<std::uint8_t>(descriptor.begin(), descriptor.end())});
      require(response.size() == 1 &&
                  response[0].opcode == pccontroller::wire::Ack &&
                  pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::effect(board) == 4 &&
                  renderedStatus(pwm) ==
                      status_led_golden::frames[0].transition,
              "completed VirtualBoard descriptor did not ACK and restart at "
              "phase zero");
      std::filesystem::remove(path, ignored);
    };
    run(false);
    run(true);
  }

  {
    const auto path = temporaryEeprom();
    std::error_code ignored;
    std::filesystem::remove(path, ignored);
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    const auto base =
        pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::now(
            board);
    auto descriptor = status_led_golden::descriptor(4, 1);
    descriptor[9] = 0x08;
    descriptor[10] = 0x07;
    require(pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::apply(
                board, descriptor, base),
            "1800 ms VirtualBoard duration fixture was rejected");
    for (unsigned millisecond = 10; millisecond < 1800;
         millisecond += 10) {
      static_cast<void>(pccontroller::virtual_board::
                            VirtualBoardStatusLedTestAccess::tickAt(
                                board, base + std::chrono::milliseconds(
                                                   millisecond)));
    }
    require(pccontroller::virtual_board::
                VirtualBoardStatusLedTestAccess::effect(board) == 4,
            "10 ms VirtualBoard scheduler completed 1800 ms effect early");
    static_cast<void>(pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::tickAt(
                              board, base + std::chrono::milliseconds(1800)));
    require(pccontroller::virtual_board::
                    VirtualBoardStatusLedTestAccess::effect(board) == 0 &&
                renderedStatus(pwm) ==
                    status_led_golden::transitionEndpoint,
            "10 ms VirtualBoard scheduler missed exact 1800 ms endpoint");
    std::filesystem::remove(path, ignored);
  }

  struct SmoothFixture {
    const char *name;
    std::array<std::uint8_t, 12> descriptor;
  };
  const std::array<SmoothFixture, 5> shipped{{
      {"rf-breathe", {{1, 190, 0, 255, 0, 0, 0, 190, 20,
                        0x84, 0x03, 0}}},
      {"hot-breathe", {{1, 255, 0, 0, 0, 0, 0, 255, 72,
                         0xE8, 0x03, 0}}},
      {"factory-breathe", {{1, 16, 72, 255, 0, 0, 0, 145, 18,
                             0x40, 0x06, 0}}},
      {"named-breathe-blue", {{1, 30, 120, 255, 0, 0, 0, 200, 8,
                                0x08, 0x07, 0}}},
      {"factory-bt-off-cycle", {{3, 0, 255, 80, 255, 0, 0, 128, 0,
                                  0xD0, 0x07, 0}}},
  }};
  for (const auto &fixture : shipped) {
    const auto path = temporaryEeprom();
    std::error_code ignored;
    std::filesystem::remove(path, ignored);
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    const auto base =
        pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::now(
            board);
    require(pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::apply(
                board, fixture.descriptor, base),
            std::string(fixture.name) + " VirtualBoard fixture was rejected");
    unsigned changes = 0;
    unsigned lastEmission = 0;
    for (unsigned millisecond = 1; millisecond <= 1000; ++millisecond) {
      const auto frames = pccontroller::virtual_board::
          VirtualBoardStatusLedTestAccess::tickAt(
              board, base + std::chrono::milliseconds(millisecond));
      const auto *changed =
          findOpcode(frames, pccontroller::wire::StatusLedChanged);
      if (changed == nullptr) {
        continue;
      }
      require(lastEmission == 0 || millisecond - lastEmission >= 17,
              "VirtualBoard emitted status frames above 60 Hz");
      lastEmission = millisecond;
      ++changes;
      require(changed->payload.size() == 6 &&
                  std::equal(changed->payload.begin(),
                             changed->payload.begin() + 3,
                             renderedStatus(pwm).begin()),
              "VirtualBoard emission was not the latest physical frame");
    }
    require(changes >= 20 && changes <= 60,
            std::string(fixture.name) +
                " VirtualBoard cadence escaped 20..60 Hz");
    std::filesystem::remove(path, ignored);
  }

  {
    const auto path = temporaryEeprom();
    std::error_code ignored;
    std::filesystem::remove(path, ignored);
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    auto valid = status_led_golden::descriptor(1);
    std::vector<std::uint8_t> shortPayload(valid.begin(), valid.end() - 1);
    std::vector<std::uint8_t> longPayload(valid.begin(), valid.end());
    longPayload.push_back(0);
    auto aboveMaximum = valid;
    aboveMaximum[9] = 0x61;
    aboveMaximum[10] = 0xEA;
    auto absoluteMaximum = valid;
    absoluteMaximum[9] = 0xFF;
    absoluteMaximum[10] = 0xFF;
    const std::array<std::vector<std::uint8_t>, 4> rejected{{
        shortPayload, longPayload,
        std::vector<std::uint8_t>(aboveMaximum.begin(), aboveMaximum.end()),
        std::vector<std::uint8_t>(absoluteMaximum.begin(),
                                  absoluteMaximum.end())}};
    std::uint8_t sequence = 1;
    for (const auto &payload : rejected) {
      const auto response = board.handle(
          {pccontroller::wire::StatusEffect, sequence++, payload});
      require(response.size() == 1 &&
                  response[0].opcode == pccontroller::wire::ErrorResponse,
              "VirtualBoard accepted a non-canonical STATUS_EFFECT payload");
    }
    std::filesystem::remove(path, ignored);
  }

  // Hard Flash deliberately emits only its two state edges; it is the warning
  // exception to the smooth changed-frame cadence contract.
  {
    const auto path = temporaryEeprom();
    std::error_code ignored;
    std::filesystem::remove(path, ignored);
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    const auto base =
        pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::now(
            board);
    const auto flash = status_led_golden::descriptor(2);
    require(pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::apply(
                board, flash, base),
            "VirtualBoard hard Flash fixture was rejected");
    static_cast<void>(pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::tickAt(
                              board, base + std::chrono::milliseconds(319)));
    require(renderedStatus(pwm) == status_led_golden::frames[0].flash,
            "hard Flash changed before its half-cycle edge");
    static_cast<void>(pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::tickAt(
                              board, base + std::chrono::milliseconds(320)));
    require(renderedStatus(pwm) == status_led_golden::frames[2].flash,
            "hard Flash missed its alternate half-cycle edge");
    static_cast<void>(pccontroller::virtual_board::
                          VirtualBoardStatusLedTestAccess::tickAt(
                              board, base + std::chrono::milliseconds(640)));
    require(renderedStatus(pwm) == status_led_golden::frames[0].flash,
            "hard Flash missed its primary cycle edge");
    std::filesystem::remove(path, ignored);
  }
}

void testStatusLedPriorityRestoreAndProfileOwnership() {
  const auto path = temporaryEeprom();
  std::error_code ignored;
  std::filesystem::remove(path, ignored);
  {
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(path);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    const auto bootBase = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::bootNow(board);
    static_cast<void>(board.handle({pccontroller::wire::Hello, 1, {}}));
    const auto bootFirst = status_led_golden::descriptor(1);
    auto bootLatest = bootFirst;
    bootLatest[1] = 0;
    bootLatest[3] = 255;
    require(pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::apply(
                board, bootFirst, bootBase + std::chrono::milliseconds(1)) &&
                pccontroller::virtual_board::
                    VirtualBoardStatusLedTestAccess::apply(
                        board, bootLatest,
                        bootBase + std::chrono::milliseconds(2)),
            "VirtualBoard rejected a retained request during Boot");
    auto bootFrames = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(
            board, bootBase + std::chrono::milliseconds(20));
    const auto *boot =
        findOpcode(bootFrames, pccontroller::wire::StatusLedChanged);
    require(boot != nullptr && boot->payload.size() == 6 &&
                boot->payload[5] == 1,
            "VirtualBoard manual request stole Boot priority");
    static_cast<void>(board.console("door open"));
    bootFrames = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(
            board, bootBase + std::chrono::milliseconds(40));
    const auto *bootCue =
        findOpcode(bootFrames, pccontroller::wire::StatusLedChanged);
    require(bootCue == nullptr || bootCue->payload[5] == 1,
            "VirtualBoard informational cue obscured Boot priority");
    sensors.setDoorOpen(false);
    bootFrames = pccontroller::virtual_board::
        VirtualBoardStatusLedTestAccess::tickAt(
            board, bootBase + std::chrono::milliseconds(651));
    const auto *bootRestored =
        findOpcode(bootFrames, pccontroller::wire::StatusLedChanged);
    require(bootRestored != nullptr && bootRestored->payload.size() == 6 &&
                bootRestored->payload[5] == 0xFF &&
                bootRestored->payload[2] > 0,
            "VirtualBoard Boot exit did not restore the latest request");

    auto simulatedNow =
        pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::now(
            board);
    const auto nextFrame = [&]() {
      simulatedNow += std::chrono::milliseconds(20);
      return pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::
          tickAt(board, simulatedNow);
    };

    static_cast<void>(board.handle({pccontroller::wire::Hello, 1, {}}));
    static_cast<void>(nextFrame());
    const std::vector<std::uint8_t> red{
        1, 255, 0, 0, 0, 0, 0, 220, 40, 0x80, 0x02, 0};
    auto response =
        board.handle({pccontroller::wire::StatusEffect, 2, red});
    require(response[0].opcode == pccontroller::wire::Ack,
            "manual effect setup failed");

    sensors.setTLedCentiC(6000);
    auto frames = nextFrame();
    const auto *warning =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(warning != nullptr && warning->payload.size() == 6 &&
                warning->payload[5] == 4 && warning->payload[0] > 0 &&
                warning->payload[2] == 0,
            "hot Warning did not preempt the manual effect");

    const std::vector<std::uint8_t> blue{
        1, 0, 0, 255, 0, 0, 0, 210, 45, 0x00, 0x05, 0};
    response = board.handle({pccontroller::wire::StatusEffect, 3, blue});
    require(response[0].opcode == pccontroller::wire::Ack,
            "changed descriptor was rejected during Warning");
    static_cast<void>(board.console("door open"));
    static_cast<void>(nextFrame());
    require(pwm.value(13) > 0 && pwm.value(15) == 0,
            "manual replacement or door cue obscured Warning");

    sensors.setTLedCentiC(2500);
    frames = nextFrame();
    const auto *restored =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(restored != nullptr && restored->payload.size() == 6 &&
                restored->payload[2] > 20 && restored->payload[0] == 0 &&
                restored->payload[4] == 1 && restored->payload[5] == 0xFF,
            "Warning release did not restore the changed retained request");

    static_cast<void>(board.handle(
        {pccontroller::wire::ProgramState, 4, {1}}));
    frames = nextFrame();
    const auto *fault =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(fault != nullptr && fault->payload.size() == 6 &&
                fault->payload[5] == 5,
            "running with an open door did not preempt with Fault");

    const std::vector<std::uint8_t> green{
        1, 0, 255, 0, 0, 0, 0, 180, 35, 0x80, 0x02, 0};
    response = board.handle({pccontroller::wire::StatusEffect, 5, green});
    require(response[0].opcode == pccontroller::wire::Ack,
            "changed descriptor was rejected during Fault");
    sensors.setDoorOpen(false);
    frames = nextFrame();
    restored = findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(restored != nullptr && restored->payload.size() == 6 &&
                restored->payload[1] > 20 && restored->payload[0] == 0 &&
                restored->payload[4] == 1 && restored->payload[5] == 0xFF,
            "Fault release did not restore the latest retained request");

    static_cast<void>(board.handle(
        {pccontroller::wire::StatusEffect, 6, {0}}));
    frames = nextFrame();
    const auto *released =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(released != nullptr && released->payload.size() == 6 &&
                released->payload[5] == 10,
            "explicit release did not clear the retained manual owner");

    static_cast<void>(board.handle(
        {pccontroller::wire::ProgramState, 7, {0}}));
    sensors.setBluetoothState(0);
    static_cast<void>(nextFrame());
    const std::vector<std::uint8_t> doorCue{
        0, 0, 255, 0, 0, 0, 0, 180, 0, 0, 0, 0};
    std::vector<std::uint8_t> cueSet{11};
    cueSet.insert(cueSet.end(), doorCue.begin(), doorCue.end());
    response = board.handle(
        {pccontroller::wire::StatusProfileSet, 8, cueSet});
    require(response[0].opcode == pccontroller::wire::Ack,
            "door cue profile setup was rejected");
    static_cast<void>(board.console("door open"));
    frames = nextFrame();
    const auto *cue =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(cue != nullptr && cue->payload.size() == 6 &&
                cue->payload[5] == 11 && cue->payload[1] > 0,
            "native door event did not render its cue layer");
    sensors.setTLedCentiC(6000);
    frames = nextFrame();
    warning = findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(warning != nullptr && warning->payload.size() == 6 &&
                warning->payload[5] == 4,
            "Warning did not preempt an active informational cue");
    sensors.setTLedCentiC(2500);
    frames = nextFrame();
    const auto *native =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(native != nullptr && native->payload.size() == 6 &&
                native->payload[5] == 8,
            "cleared Warning resumed a canceled informational cue");
    sensors.setDoorOpen(false);

    const std::vector<std::uint8_t> nativeProfile{
        1, 10, 20, 200, 0, 0, 0, 180, 20, 0x80, 0x02, 0};
    std::vector<std::uint8_t> profileSet{8};
    profileSet.insert(profileSet.end(), nativeProfile.begin(),
                      nativeProfile.end());
    response = board.handle(
        {pccontroller::wire::StatusProfileSet, 9, profileSet});
    require(response[0].opcode == pccontroller::wire::Ack,
            "active native profile edit was rejected");
    sensors.setBluetoothState(1);
    frames = nextFrame();
    const auto *connected =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(connected != nullptr && connected->payload.size() == 6 &&
                connected->payload[5] == 7,
            "active native profile edit leaked manual ownership");

    auto trailingProfile = profileSet;
    trailingProfile.push_back(0xAA);
    response = board.handle(
        {pccontroller::wire::StatusProfileSet, 91, trailingProfile});
    require(response[0].opcode == pccontroller::wire::ErrorResponse,
            "VirtualBoard accepted trailing STATUS_PROFILE_SET bytes");

    pccontroller::virtual_board::VirtualBoardStatusLedTestAccess::
        setFallbackBrightness(board, 100);
    const std::vector<std::uint8_t> fallbackManual{
        1, 0, 0, 255, 0, 0, 0, 220, 20, 0x80, 0x02, 0};
    response = board.handle(
        {pccontroller::wire::StatusEffect, 92, fallbackManual});
    require(response[0].opcode == pccontroller::wire::Ack,
            "VirtualBoard fallback-brightness manual setup failed");
    sensors.setTLedCentiC(6000);
    frames = nextFrame();
    const auto *fallbackWarning =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(fallbackWarning != nullptr &&
                fallbackWarning->payload.size() == 6 &&
                fallbackWarning->payload[3] == 100,
            "VirtualBoard blank safety fallback inherited manual brightness");
    sensors.setTLedCentiC(2500);
    frames = nextFrame();
    restored = findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(restored != nullptr && restored->payload.size() == 6 &&
                restored->payload[3] == fallbackManual[7],
            "VirtualBoard safety clear did not restore manual brightness");

    const std::vector<std::uint8_t> learningRed{
        1, 255, 0, 0, 0, 0, 0, 190, 30, 0x80, 0x02, 0};
    response = board.handle(
        {pccontroller::wire::StatusEffect, 10, learningRed});
    require(response[0].opcode == pccontroller::wire::Ack,
            "manual setup before RF Learning was rejected");
    response = board.handle(
        {pccontroller::wire::RadioLearnStart, 11, {0, 0}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "indefinite RF Learning start was rejected");
    frames = nextFrame();
    const auto *learning =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(learning != nullptr && learning->payload.size() == 6 &&
                learning->payload[5] == 3,
            "RF Learning did not preempt a manual board-owned effect");
    const auto learningFrame =
        std::array<std::uint16_t, 3>{pwm.value(13), pwm.value(14),
                                     pwm.value(15)};

    const std::vector<std::uint8_t> learningBlue{
        1, 0, 0, 255, 0, 0, 0, 205, 35, 0x00, 0x05, 0};
    response = board.handle(
        {pccontroller::wire::StatusEffect, 12, learningBlue});
    require(response[0].opcode == pccontroller::wire::Ack &&
                pwm.value(13) == learningFrame[0] &&
                pwm.value(14) == learningFrame[1] &&
                pwm.value(15) == learningFrame[2],
            "changed manual descriptor stole the RF Learning layer");
    static_cast<void>(board.console("door open"));
    frames = nextFrame();
    const auto *learningCueFrame =
        findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(learningCueFrame == nullptr ||
                (learningCueFrame->payload.size() == 6 &&
                 learningCueFrame->payload[5] == 3),
            "informational cue obscured RF Learning priority");

    response = board.handle(
        {pccontroller::wire::RadioLearnCancel, 13, {}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "RF Learning cancel was rejected");
    frames = nextFrame();
    restored = findOpcode(frames, pccontroller::wire::StatusLedChanged);
    require(restored != nullptr && restored->payload.size() == 6 &&
                restored->payload[2] > 20 && restored->payload[0] == 0 &&
                restored->payload[4] == 1 && restored->payload[5] == 0xFF,
            "RF Learning exit did not restore the latest manual descriptor");
  }
  std::filesystem::remove(path, ignored);
}

void testMotionDoorPolicyAcrossVirtualCommandSources() {
  struct PolicyCase {
    std::uint8_t policy;
    bool doorOpen;
    bool allowed;
  };
  const PolicyCase cases[] = {
      {0, false, true}, {0, true, true},  {1, false, true},
      {1, true, false}, {2, false, false}, {2, true, true},
      {3, false, false}, {3, true, false},
  };

  for (const PolicyCase &policyCase : cases) {
    const auto path = temporaryEeprom();
    std::error_code ignored;
    std::filesystem::remove(path, ignored);
    {
      pccontroller::virtual_board::SensorBank sensors;
      sensors.setDoorOpen(policyCase.doorOpen);
      pccontroller::virtual_board::RelayBank relays;
      pccontroller::virtual_board::PwmBank pwm;
      pccontroller::virtual_board::AddressableLedBank addressableLeds;
      pccontroller::virtual_board::DisplayBank displays;
      pccontroller::virtual_board::FileEeprom eeprom(path);
      pccontroller::virtual_board::VirtualBoard board(
          sensors, relays, pwm, addressableLeds, displays, eeprom);

      const std::vector<std::uint8_t> settings{
          3, static_cast<std::uint8_t>(policyCase.policy << 3U),
          1, 200, 0, 5, 140, 0, 250, 0, 0, 0, 0, 0, 1};
      auto response = board.handle(
          {pccontroller::wire::SetSettings, 1, settings});
      require(response[0].opcode == pccontroller::wire::Ack,
              "virtual motion-policy fixture rejected valid settings");

      const auto expectMotionResponse = [&](const pccontroller::wire::Frame &frame,
                                            const std::string &source) {
        const auto result = board.handle(frame);
        const bool accepted = result[0].opcode == pccontroller::wire::Ack;
        require(accepted == policyCase.allowed,
                source + " disagreed with the four-mode door policy");
        require(((relays.mask() & (1U << 1U)) != 0) == policyCase.allowed,
                source + " left the wrong Side A enable state");
        const auto stopped = board.handle(
            {pccontroller::wire::RelaySide, 99, {0, 0}});
        require(stopped[0].opcode == pccontroller::wire::Ack &&
                    (relays.mask() & (1U << 1U)) == 0,
                source + " prevented an unconditional motion stop");
      };

      expectMotionResponse(
          {pccontroller::wire::RelaySide, 2, {0, 1}}, "host side command");
      expectMotionResponse(
          {pccontroller::wire::RelaySet, 3, {1, 1}}, "host relay-test command");

      const auto console = board.console("relay 2 on");
      require((console.message.rfind("error:", 0) != 0) == policyCase.allowed &&
                  ((relays.mask() & (1U << 1U)) != 0) == policyCase.allowed,
              "interactive direct-relay path disagreed with door policy");
      static_cast<void>(board.handle(
          {pccontroller::wire::RelaySide, 98, {0, 0}}));

      response = board.handle(
          {pccontroller::wire::RadioLearnReplace, 4,
           {0, 0x44, 0x33, 0x22, 0x11, 24, 1, 0x5E, 0x01,
            4, 0, 3}});
      require(response[0].opcode == pccontroller::wire::Ack,
              "RF side-policy fixture could not install its mapping");
      static_cast<void>(board.console("rfrecv 0x11223344 24 1 350"));
      static_cast<void>(board.tick());
      require(((relays.mask() & (1U << 1U)) != 0) == policyCase.allowed,
              "RF side mapping disagreed with the four-mode door policy");
      static_cast<void>(board.handle(
          {pccontroller::wire::RelaySide, 97, {0, 0}}));

      response = board.handle(
          {pccontroller::wire::MacroStart, 5, {2, 1, 0, 1, 0}});
      require(response[0].opcode == pccontroller::wire::Ack,
              "motion-policy macro fixture did not start");
      response = board.handle(
          {pccontroller::wire::MacroStep, 6,
           {0, 0, 0, 1, 0,
            0, 0, 0, 0, pccontroller::wire::RelaySide, 2, 0, 1}});
      require(response[0].opcode == pccontroller::wire::Ack,
              "motion-policy macro fixture did not buffer its step");
      response = board.handle(
          {pccontroller::wire::MacroStep, 7, {1}});
      require(response[0].opcode == pccontroller::wire::Ack,
              "motion-policy macro fixture did not enter playback");
      const auto macroEvents = board.tick();
      require(((relays.mask() & (1U << 1U)) != 0) == policyCase.allowed,
              "buffered macro motion disagreed with the door policy");
      require(std::any_of(
                  macroEvents.begin(), macroEvents.end(),
                  [&](const auto &event) {
                    return event.sequence == 0xFEU &&
                           event.opcode ==
                               (policyCase.allowed
                                    ? pccontroller::wire::Ack
                                    : pccontroller::wire::ErrorResponse);
                  }),
              "macro playback did not report its policy decision");
      static_cast<void>(board.handle(
          {pccontroller::wire::RelayAllOff, 96, {}}));

      response = board.handle(
          {pccontroller::wire::RelaySet, 8, {4, 1}});
      require(response[0].opcode == pccontroller::wire::Ack &&
                  (relays.mask() & (1U << 4U)) != 0,
              "motion policy incorrectly blocked general relay R5");
    }
    std::filesystem::remove(path, ignored);
  }
}

void testVirtualResetJournalRecoveryAndRollover() {
  const auto corruptPath = temporaryEeprom();
  std::error_code ignored;
  std::filesystem::remove(corruptPath, ignored);
  {
    pccontroller::virtual_board::FileEeprom eeprom(corruptPath);
    eeprom.fill(0xFF);
    writeVirtualResetRecord(eeprom, 5, 41);
    writeVirtualResetRecord(eeprom, 6, 42, 0xA7, true);
    writeVirtualResetRecord(eeprom, 7, 43, 0);
    eeprom.flush();

    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    require(virtualResetCount(board) == 42,
            "virtual board did not ignore corrupt/torn reset slots");
  }
  std::filesystem::remove(corruptPath, ignored);

  const auto wrapPath = temporaryEeprom();
  std::filesystem::remove(wrapPath, ignored);
  {
    pccontroller::virtual_board::FileEeprom eeprom(wrapPath);
    eeprom.fill(0xFF);
    writeVirtualResetRecord(
        eeprom, 62, std::numeric_limits<std::uint32_t>::max() - 1U);
    eeprom.flush();
  }
  {
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(wrapPath);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    require(virtualResetCount(board) ==
                std::numeric_limits<std::uint32_t>::max(),
            "virtual reset journal did not retain UINT32_MAX");
  }
  {
    pccontroller::virtual_board::SensorBank sensors;
    pccontroller::virtual_board::RelayBank relays;
    pccontroller::virtual_board::PwmBank pwm;
    pccontroller::virtual_board::AddressableLedBank addressableLeds;
    pccontroller::virtual_board::DisplayBank displays;
    pccontroller::virtual_board::FileEeprom eeprom(wrapPath);
    pccontroller::virtual_board::VirtualBoard board(
        sensors, relays, pwm, addressableLeds, displays, eeprom);
    require(virtualResetCount(board) == 1,
            "virtual reset journal did not wrap UINT32_MAX to 1");
  }
  std::filesystem::remove(wrapPath, ignored);
}

} // namespace

int main() {
  try {
    testProtocolRoundTrip();
    testBoardAndPersistence();
    testStatusLedOwnerFramesAndIdempotentBreathe();
    testStatusLedGoldenCadenceAndDurationParity();
    testStatusLedPriorityRestoreAndProfileOwnership();
    testPwmAvailabilityReporting();
    testMotionDoorPolicyAcrossVirtualCommandSources();
    testVirtualResetJournalRecoveryAndRollover();
    std::cout << "virtual_board_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "virtual_board_tests: " << error.what() << '\n';
    return 1;
  }
}
