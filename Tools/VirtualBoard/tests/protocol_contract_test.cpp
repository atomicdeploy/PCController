#include "../../../Project/ProtocolContract.h"

#include <cstdint>
#include <iostream>

namespace {

bool same(std::uint8_t left, std::uint8_t right) { return left == right; }

} // namespace

int main() {
  // This target is deliberately C++11 and includes no Arduino shim. The
  // production UART adapter is compiled separately by firmware_uart_protocol;
  // together the two tests prove the contract is portable while the adapter
  // remains source-compatible.
  static_assert(ControllerProtocol::Magic == 0xA5,
                "native envelope marker changed");
  static_assert(ControllerProtocol::MaximumPayload == 48,
                "native payload bound changed");
  static_assert(ControllerProtocol::RawFrameOverhead == 6,
                "native raw-frame shape changed");
  static_assert(ControllerProtocol::Hello == 0x01,
                "HELLO opcode changed");
  static_assert(ControllerProtocol::StatusEffect == 0x17,
                "STATUS_EFFECT opcode changed");
  static_assert(ControllerProtocol::HostMenuDirectory == 0x42,
                "host-menu directory opcode changed");
  static_assert(ControllerProtocol::HostMenuContent == 0x43,
                "host-menu content opcode changed");
  static_assert(ControllerProtocol::HostMenuStateGet == 0x44,
                "host-menu state opcode changed");
  static_assert(ControllerProtocol::StatusLedChanged == 0x9E,
                "status-led event opcode changed");
  static_assert(ControllerProtocol::Unsafe == 6,
                "protocol error value changed");

  const bool compatible =
      same(ControllerProtocol::WireContract::Magic, ControllerProtocol::Magic) &&
      same(ControllerProtocol::WireContract::StatusEffect,
           ControllerProtocol::StatusEffect) &&
      same(ControllerProtocol::WireContract::ErrorResponse,
           ControllerProtocol::ErrorResponse);
  if (!compatible) {
    std::cerr << "protocol contract aliases diverged\n";
    return 1;
  }

  std::cout << "firmware_protocol_contract_tests: canonical values compile "
               "through the AVR adapter\n";
  return 0;
}
