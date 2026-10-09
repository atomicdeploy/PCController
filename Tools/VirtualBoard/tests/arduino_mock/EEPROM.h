#pragma once

#include <algorithm>
#include <array>
#include <cstdint>
#include <cstring>
#include <vector>

// EEPROMClass is a byte-accurate 1 KiB ATmega328P EEPROM test double. Its
// update log lets reset-journal tests prove that validity is published last.
class EEPROMClass {
public:
  struct Update {
    int address;
    std::uint8_t value;
  };

  EEPROMClass() { fill(0xFF); }

  template <typename T> T &get(int address, T &value) const {
    std::memcpy(&value, bytes_.data() + address, sizeof(T));
    return value;
  }

  void update(int address, std::uint8_t value) {
    if (bytes_.at(static_cast<std::size_t>(address)) == value) {
      return;
    }
    bytes_[static_cast<std::size_t>(address)] = value;
    updates_.push_back({address, value});
  }

  std::uint8_t read(int address) const {
    return bytes_.at(static_cast<std::size_t>(address));
  }

  void fill(std::uint8_t value) {
    bytes_.fill(value);
    updates_.clear();
  }

  void clearUpdates() { updates_.clear(); }
  const std::vector<Update> &updates() const { return updates_; }

private:
  std::array<std::uint8_t, 1024> bytes_{};
  std::vector<Update> updates_;
};

inline EEPROMClass EEPROM;
