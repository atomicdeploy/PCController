#include "virtual_board/hardware.hpp"
#include "virtual_board/protocol.hpp"
#include "virtual_board/virtual_board.hpp"

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <filesystem>
#include <iostream>
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

std::filesystem::path temporaryEeprom() {
  const auto nonce =
      std::chrono::steady_clock::now().time_since_epoch().count();
  return std::filesystem::temp_directory_path() /
         ("pccontroller-virtual-board-" + std::to_string(nonce) + ".bin");
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

    response = board.handle({pccontroller::wire::MenuLayoutGet, 43, {}});
    require(response.size() == 1 &&
                response[0].opcode == pccontroller::wire::MenuLayoutResponse &&
                response[0].payload.size() == 12 &&
                response[0].payload[0] == 2 &&
                response[0].payload[4] == 0x10 &&
                response[0].payload[11] == 0xFE,
            "MENU_LAYOUT GET is not packed schema 2");
    const std::vector<std::uint8_t> packedLayout{
        2, 15, 0xFE, 0x7F, 0x30, 0x14, 0x52, 0x76, 0xCB, 0x8D, 0xA9,
        0xFE};
    response = board.handle(
        {pccontroller::wire::MenuLayoutSet, 44, packedLayout});
    require(response.size() == 1 && response[0].opcode == pccontroller::wire::Ack,
            "packed MENU_LAYOUT SET was not acknowledged");
    response = board.handle({pccontroller::wire::MenuLayoutGet, 45, {}});
    require(response[0].payload == packedLayout,
            "packed MENU_LAYOUT did not round-trip exactly");
    const std::vector<std::uint8_t> legacyLayout{
        1, 15, 0xFF, 0x7F, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
        13, 14};
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
                response[0].payload[1] == 15 &&
                response[0].payload[2] == 7 &&
                response[0].payload[3] == 7,
            "MENU_LIST does not match the paged production schema");

    std::vector<std::uint8_t> capture{3, 0xBC, 0x2A, 36,
                                      'H', 'O', 'S', 'T'};
    const auto appendFixed = [&capture](const std::string &value,
                                       std::size_t width) {
      capture.insert(capture.end(), value.begin(), value.end());
      capture.insert(capture.end(), width - value.size(), ' ');
    };
    appendFixed("Host controls", 16);
    appendFixed("Ready", 16);
    response = board.handle(
        {pccontroller::wire::DisplayText, 49, capture});
    require(response[0].opcode == pccontroller::wire::Ack &&
                displays.state().segments == "HOST",
            "host front-panel capture was not applied");
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

    response = board.handle(
        {pccontroller::wire::I2cTransfer, 53,
         {0x41, 2, 3, 0, 0x10, 0xAB, 0xCD}});
    require(response[0].opcode == pccontroller::wire::I2cTransferResponse &&
                response[0].payload ==
                    std::vector<std::uint8_t>({0, 0x41, 0}),
            "cooperative I2C write response is invalid");
    response = board.handle(
        {pccontroller::wire::I2cTransfer, 54, {0x41, 0, 1, 2, 0x10}});
    require(response[0].payload ==
                std::vector<std::uint8_t>({0, 0x41, 2, 0xAB, 0xCD}),
            "cooperative I2C repeated-start read did not preserve bytes");
    response = board.handle(
        {pccontroller::wire::I2cTransfer, 55, {0x55, 0, 0, 1}});
    require(response[0].payload ==
                std::vector<std::uint8_t>({2, 0x55, 0}),
            "unconnected I2C address did not return address-NACK status");
    response = board.handle(
        {pccontroller::wire::I2cTransfer, 56, {0, 0, 0, 0}});
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
        2, 0x02, 1, 200, 7, 6, 140, 1, 250, 0, 5, 1};
    response =
        board.handle({pccontroller::wire::SetSettings, 9, settings});
    require(response.size() == 1 &&
                response[0].opcode == pccontroller::wire::Ack,
            "SET_SETTINGS was not acknowledged");

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
        board.handle({pccontroller::wire::RadioLearnStart, 19, {10, 0}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "RF learning did not start");
    require(board.console("rflearn 0x11223344 24 1 350")
                    .message.find("entry 0") != std::string::npos,
            "virtual RF entry was not learned");
    static_cast<void>(board.tick());

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
        {pccontroller::wire::RadioLearnMap, 190, {0, 3, 4, 1}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "RF relay mapping was not acknowledged");
    static_cast<void>(board.console("rfrecv 0x11223344 24 1 350"));
    static_cast<void>(board.tick());
    require((relays.mask() & (1U << 4U)) != 0,
            "mapped RF toggle did not execute through the relay path");
    static_cast<void>(board.console("rfrecv 0x11223344 24 1 350"));
    static_cast<void>(board.tick());
    require((relays.mask() & (1U << 4U)) != 0,
            "repeated RF frame retriggered a non-refreshable mapping");

    response = board.handle(
        {pccontroller::wire::RadioLearnMap, 190, {0, 3, 5, 2}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "RF momentary mapping was not acknowledged");
    static_cast<void>(board.console("rfrecv 0x11223344 24 1 350"));
    static_cast<void>(board.tick());
    require((relays.mask() & (1U << 5U)) != 0,
            "mapped RF momentary action did not turn its relay on");
    std::this_thread::sleep_for(std::chrono::milliseconds(370));
    static_cast<void>(board.tick());
    require((relays.mask() & (1U << 5U)) == 0,
            "mapped RF momentary action did not expire locally");

    response = board.handle(
        {pccontroller::wire::RadioLearnStart, 191, {0, 3, 0xAA}});
    require(response[0].opcode == pccontroller::wire::Ack,
            "multi/indefinite RF-learning prefix was not accepted");
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
                          return timedEventEquals(event, {9, 2, 20});
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
    require(response[0].payload.size() == 12 &&
                response[0].payload[3] == 200 &&
                response[0].payload[10] == 5 &&
                response[0].payload[11] == 1,
            "virtual MCU EEPROM did not retain settings");

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
                                     {2, 15, 0xFE, 0x7F, 0x30, 0x14,
                                      0x52, 0x76, 0xCB, 0x8D, 0xA9, 0xFE}),
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

} // namespace

int main() {
  try {
    testProtocolRoundTrip();
    testBoardAndPersistence();
    std::cout << "virtual_board_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "virtual_board_tests: " << error.what() << '\n';
    return 1;
  }
}
