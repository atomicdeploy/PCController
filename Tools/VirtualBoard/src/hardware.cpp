#include "virtual_board/hardware.hpp"

#include <algorithm>
#include <fstream>
#include <stdexcept>

namespace pccontroller::virtual_board {

SensorReadings SensorBank::readings() const { return values_; }

void SensorBank::setSupplyMilliVolts(std::int32_t value) {
  values_.supplyMilliVolts = value;
  values_.busMilliVolts = value > 10 ? value - 10 : value;
  updatePower();
}

void SensorBank::setCurrentMilliAmps(std::int32_t value) {
  values_.currentMilliAmps = value;
  updatePower();
}

void SensorBank::setTLedCentiC(std::int16_t value) {
  values_.tLedCentiC = value;
}

void SensorBank::setTBtCentiC(std::int16_t value) {
  values_.tBtCentiC = value;
}

void SensorBank::setDoorOpen(bool value) { values_.doorOpen = value; }

void SensorBank::setBluetoothState(std::uint8_t value) {
  values_.bluetoothState = value;
}

void SensorBank::updatePower() {
  values_.powerMilliWatts = static_cast<std::int32_t>(
      (static_cast<std::int64_t>(values_.supplyMilliVolts) *
       values_.currentMilliAmps) /
      1000);
}

void RelayBank::setRetainDirectionOnStop(bool retain) {
  retainDirectionOnStop_ = retain;
}

bool RelayBank::set(std::uint8_t index, bool active) {
  if (index >= 8) {
    return false;
  }
  const std::uint8_t bit = static_cast<std::uint8_t>(1U << index);
  if (active) {
    mask_ |= bit;
  } else {
    mask_ &= static_cast<std::uint8_t>(~bit);
  }
  return true;
}

bool RelayBank::setSide(std::uint8_t side, std::uint8_t motion) {
  if (side > 1 || motion > 2) {
    return false;
  }
  const std::uint8_t direction = static_cast<std::uint8_t>(side * 2U);
  const std::uint8_t enable = static_cast<std::uint8_t>(direction + 1U);
  set(enable, false);
  if (motion == 0) {
    if (!retainDirectionOnStop_) {
      set(direction, false);
    }
    return true;
  }
  set(direction, motion == 2);
  set(enable, true);
  return true;
}

void RelayBank::allOff() { mask_ = 0; }

std::uint8_t RelayBank::mask() const { return mask_; }

bool PwmBank::available() const { return available_; }

bool PwmBank::set(std::uint8_t channel, std::uint16_t value) {
  if (!available_ || channel >= values_.size() || value > 4095) {
    return false;
  }
  values_[channel] = value;
  return true;
}

void PwmBank::allOff() {
  if (available_) {
    values_.fill(0);
  }
}

std::array<std::uint16_t, 16> PwmBank::values() const { return values_; }

std::uint16_t PwmBank::value(std::uint8_t channel) const {
  return channel < values_.size() ? values_[channel] : 0;
}

void PwmBank::select(std::uint8_t channel) {
  if (available_ && channel < values_.size()) {
    selected_ = channel;
  }
}

std::uint8_t PwmBank::selected() const { return selected_; }

bool AddressableLedBank::setPixel(std::uint8_t index,
                                  AddressableLedColor color) {
  if (index >= state_.pixels.size()) {
    return false;
  }
  state_.pixels[index] = color;
  return true;
}

void AddressableLedBank::fill(AddressableLedColor color) {
  state_.pixels.fill(color);
}

void AddressableLedBank::setBrightness(std::uint8_t brightness) {
  state_.brightness = brightness;
}

AddressableLedState AddressableLedBank::state() const { return state_; }

void DisplayBank::setSegments(const std::string &value) {
  state_.segments = value.substr(0, 4);
  state_.segments.resize(4, ' ');
}

void DisplayBank::setLcd(const std::string &value) {
  std::string normalized = value.substr(0, 32);
  const std::size_t newline = normalized.find('\n');
  if (newline != std::string::npos) {
    state_.lcdLine1 = normalized.substr(0, newline).substr(0, 16);
    state_.lcdLine2 = normalized.substr(newline + 1).substr(0, 16);
    return;
  }
  state_.lcdLine1 = normalized.substr(0, 16);
  state_.lcdLine2 =
      normalized.size() > 16 ? normalized.substr(16, 16) : std::string{};
}

void DisplayBank::setBuzzer(std::uint16_t frequencyHz,
                            std::uint16_t durationMs) {
  state_.buzzerFrequencyHz = frequencyHz;
  state_.buzzerDurationMs = durationMs;
}

DisplayState DisplayBank::state() const { return state_; }

FileEeprom::FileEeprom(std::filesystem::path path, std::size_t bytes)
    : path_(std::move(path)), bytes_(bytes, 0xFFU) {
  if (bytes == 0) {
    throw std::invalid_argument("EEPROM size must be positive");
  }
  load();
}

FileEeprom::~FileEeprom() {
  try {
    flush();
  } catch (...) {
  }
}

std::size_t FileEeprom::size() const { return bytes_.size(); }

std::uint8_t FileEeprom::read(std::size_t address) const {
  if (address >= bytes_.size()) {
    throw std::out_of_range("EEPROM read is outside the emulated device");
  }
  return bytes_[address];
}

void FileEeprom::update(std::size_t address, std::uint8_t value) {
  if (address >= bytes_.size()) {
    throw std::out_of_range("EEPROM write is outside the emulated device");
  }
  if (bytes_[address] != value) {
    bytes_[address] = value;
    dirty_ = true;
  }
}

void FileEeprom::fill(std::uint8_t value) {
  std::fill(bytes_.begin(), bytes_.end(), value);
  dirty_ = true;
}

void FileEeprom::flush() {
  if (!dirty_) {
    return;
  }
  const auto parent = path_.parent_path();
  if (!parent.empty()) {
    std::filesystem::create_directories(parent);
  }
  std::ofstream output(path_, std::ios::binary | std::ios::trunc);
  if (!output) {
    throw std::runtime_error("cannot open virtual EEPROM for writing: " +
                             path_.string());
  }
  output.write(reinterpret_cast<const char *>(bytes_.data()),
               static_cast<std::streamsize>(bytes_.size()));
  if (!output) {
    throw std::runtime_error("cannot write virtual EEPROM: " + path_.string());
  }
  output.flush();
  if (!output) {
    throw std::runtime_error("cannot flush virtual EEPROM: " + path_.string());
  }
  dirty_ = false;
}

std::filesystem::path FileEeprom::path() const { return path_; }

void FileEeprom::load() {
  std::ifstream input(path_, std::ios::binary);
  if (!input) {
    dirty_ = true;
    return;
  }
  input.read(reinterpret_cast<char *>(bytes_.data()),
             static_cast<std::streamsize>(bytes_.size()));
  const std::streamsize loaded = input.gcount();
  if (loaded < static_cast<std::streamsize>(bytes_.size())) {
    std::fill(bytes_.begin() + loaded, bytes_.end(), 0xFFU);
  }
}

} // namespace pccontroller::virtual_board
