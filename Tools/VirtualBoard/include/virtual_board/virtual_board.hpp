#pragma once
#include <array>
#include <cstdint>
#include <filesystem>
#include <mutex>
#include <string>
#include <vector>

namespace pccontroller::virtual_board {
std::string nativeEepromSnapshot();
void releaseNativeEeprom();
struct ConsoleResult { std::string message; bool stopRequested=false; };
struct OutputSnapshot {
  uint8_t relayMask=0;
  std::array<uint16_t,16> pwm{};
  std::array<uint8_t,4> segments{};
  uint8_t brightness=0;
  bool tm1637Detected=false;
  uint16_t buzzerHz=0;
  std::vector<uint8_t> strip;
};
// One firmware instance per process, exactly like one MCU. Multiple boards
// require isolated processes and EEPROM paths. This never dispatches opcodes.
class VirtualBoard {
public:
  explicit VirtualBoard(const std::filesystem::path &eeprom={},bool deterministic=false);
  ~VirtualBoard();
  VirtualBoard(const VirtualBoard &)=delete;
  VirtualBoard &operator=(const VirtualBoard &)=delete;
  void feed(const uint8_t *data,size_t length);
  std::vector<uint8_t> tick(uint64_t advanceUs=0);
  std::vector<uint8_t> connectedBytes();
  void disconnected();
  void flush();
  ConsoleResult console(const std::string &line);
  std::string describe() const;
  OutputSnapshot outputSnapshot() const;
  bool resetRequested() const;
private:
  mutable std::mutex mutex_;
  bool resetRequested_=false;
};
}
