#pragma once
#include "virtual_board/virtual_board.hpp"
#include <memory>
#include <string>
#include <vector>
namespace pccontroller::virtual_board {
struct HostPeripheralOptions {
  bool speaker=true;
  std::vector<std::string> relayGpio, inputGpio, relayLed, relayOptical, relayParallel;
  std::vector<std::string> relayScrollLock;
  std::string stripOpenrgb;
};
// OS sinks/sources are outside the production firmware. Only explicitly mapped
// repurposed devices are opened. The worker never owns a second board state.
class HostPeripherals {
 public:
  HostPeripherals(VirtualBoard &board,const HostPeripheralOptions &options);
  ~HostPeripherals();
  void observe(const OutputSnapshot &snapshot);
 private:
  struct Impl;
  std::unique_ptr<Impl> impl_;
};
std::string peripheralCandidates(); // Read-only inventory, not commissioning.
}
