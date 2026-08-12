#include "virtual_board/hardware.hpp"
#include "virtual_board/protocol.hpp"
#include "virtual_board/virtual_board.hpp"

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
    require(response.size() == 4 &&
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
    const auto *initialSegments =
        findOpcode(response, pccontroller::wire::SegmentChanged);
    const auto *initialBuzzer =
        findOpcode(response, pccontroller::wire::BuzzerChanged);
    const auto *initialStatusLed =
        findOpcode(response, pccontroller::wire::StatusLedChanged);
    require(initialSegments != nullptr && initialSegments->sequence == 0 &&
                initialSegments->payload.size() == 5 &&
                initialBuzzer != nullptr && initialBuzzer->sequence == 0 &&
                initialBuzzer->payload.size() == 5 &&
                initialStatusLed != nullptr && initialStatusLed->sequence == 0 &&
                initialStatusLed->payload.size() == 6,
            "authenticated HELLO did not force exact output-state frames");
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
    const auto resetEvents = board.tick();
    require(
        std::any_of(
            resetEvents.begin(), resetEvents.end(),
            [](const auto &event) {
              return timedEventEquals(event, {7, 0x08, 2, 0, 0, 0});
            }),
        "reset event is not [7, cause, persistent count LE u32]");
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
    const auto bootloaderResetEvents = board.tick();
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
