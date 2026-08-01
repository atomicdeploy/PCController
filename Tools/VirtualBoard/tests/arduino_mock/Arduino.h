#pragma once

#include <cstddef>
#include <cstdint>
#include <deque>
#include <vector>

using std::size_t;

// Minimal serial double used to compile the production AVR framing code on
// the host. It deliberately models only the HardwareSerial surface consumed
// by UartProtocol.
class HardwareSerial {
public:
  void begin(std::uint32_t baud) { baud_ = baud; }

  int available() const { return static_cast<int>(rx_.size()); }

  int read() {
    if (rx_.empty()) {
      return -1;
    }
    const auto value = rx_.front();
    rx_.pop_front();
    return value;
  }

  size_t write(std::uint8_t value) {
    tx_.push_back(value);
    return 1;
  }

  size_t write(const std::uint8_t *values, size_t length) {
    tx_.insert(tx_.end(), values, values + length);
    return length;
  }

  void feed(const std::vector<std::uint8_t> &values) {
    rx_.insert(rx_.end(), values.begin(), values.end());
  }

  std::uint32_t baud() const { return baud_; }
  const std::vector<std::uint8_t> &written() const { return tx_; }
  void clearWritten() { tx_.clear(); }

private:
  std::deque<std::uint8_t> rx_;
  std::vector<std::uint8_t> tx_;
  std::uint32_t baud_ = 0;
};

inline std::uint32_t micros() {
  static std::uint32_t now = 1000;
  return ++now;
}
