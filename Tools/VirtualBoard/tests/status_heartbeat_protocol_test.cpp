// Exercises the actual production dispatcher through its UART framing. This
// is not another LED renderer or a test-only PROGRAM_STATE implementation.
#include "virtual_board/virtual_board.hpp"
#include "virtual_board/protocol.hpp"
#include <algorithm>
#include <array>
#include <iostream>
#include <stdexcept>

using namespace pccontroller;
namespace {
void require(bool value, const char *message) {
  if (!value) throw std::runtime_error(message);
}
}

int main() {
  try {
    virtual_board::VirtualBoard board({}, true);
    wire::StreamDecoder decoder;
    std::array<uint8_t, 6> led{};
    bool haveLed = false;
    uint8_t sequence = 0;
    auto receive = [&](uint64_t us) {
      const auto bytes = board.tick(us);
      const auto frames = decoder.feed(bytes.data(), bytes.size()).frames;
      for (const auto &frame : frames) {
        if (frame.opcode == wire::StatusLedChanged && frame.payload.size() == 6) {
          std::copy(frame.payload.begin(), frame.payload.end(), led.begin());
          haveLed = true;
        }
      }
      return frames;
    };
    auto request = [&](uint8_t opcode, const std::vector<uint8_t> &payload) {
      const auto id = ++sequence;
      const auto bytes = wire::encode({opcode, id, payload});
      board.feed(bytes.data(), bytes.size());
      const auto frames = receive(1000);
      if (opcode != wire::GetStatus && opcode != wire::Hello) {
        require(std::any_of(frames.begin(), frames.end(), [id](const auto &f) {
          return f.opcode == wire::Ack && f.sequence == id && f.payload.size() >= 2 && f.payload[1] == 0;
        }), "production status command was not acknowledged");
      }
    };
    // Keep real liveness traffic while advancing deterministic MCU time.
    auto advance = [&](unsigned milliseconds) {
      for (unsigned elapsed = 0; elapsed < milliseconds; elapsed += 10) {
        receive(10000);
        if (elapsed % 100 == 0) request(wire::GetStatus, {});
      }
    };
    board.console("door closed");
    request(wire::Hello, {});
    advance(3000);
    request(wire::ProgramState, {0});
    request(wire::StatusRgb, {120, 30, 10, 128});
    advance(60);
    require(haveLed && led[0] == 60 && led[1] == 15 && led[2] == 5 && led[5] == 0xFF,
            "static production override not observed");
    for (unsigned beat = 0; beat < 6; ++beat) {
      request(wire::ProgramState, {static_cast<uint8_t>(beat % 2)});
      advance(1900);
      require(led[0] == 60 && led[1] == 15 && led[2] == 5 && led[5] == 0xFF,
              "program-state heartbeat released static LED ownership");
    }
    // Looping transition: a heartbeat inside the rising segment must neither
    // release it nor restart its phase. Existing controller tests cover strict
    // descriptor replay/change/rollover and local safety precedence.
    request(wire::StatusEffect, {4, 0, 0, 0, 200, 100, 50, 255, 0, 0xE8, 0x03, 0});
    advance(200);
    const auto before = led[0];
    require(led[4] == 4 && led[5] == 0xFF && before > 0, "transition not active before heartbeat");
    request(wire::ProgramState, {1});
    advance(100);
    require(led[4] == 4 && led[5] == 0xFF && led[0] >= before,
            "program-state heartbeat cancelled/restarted native effect");
    request(wire::StatusEffect, {0});
    advance(60);
    require(led[5] != 0xFF, "explicit LED release no longer restores autonomous state");
    require(board.outputSnapshot().relayMask == 0, "LED test touched relay outputs");
    std::cout << "production heartbeat/static/effect ownership regression passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
