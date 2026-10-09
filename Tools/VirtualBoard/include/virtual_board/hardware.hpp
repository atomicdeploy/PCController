#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <filesystem>
#include <string>
#include <vector>

namespace pccontroller::virtual_board {

struct SensorReadings {
  std::int32_t supplyMilliVolts = 12000;
  std::int32_t busMilliVolts = 11990;
  std::int32_t currentMilliAmps = 250;
  std::int32_t powerMilliWatts = 3000;
  std::int16_t tLedCentiC = 2600;
  std::int16_t tBtCentiC = 2500;
  bool doorOpen = false;
  std::uint8_t bluetoothState = 0;
};

class ISensors {
public:
  virtual ~ISensors() = default;
  virtual SensorReadings readings() const = 0;
  virtual void setSupplyMilliVolts(std::int32_t value) = 0;
  virtual void setCurrentMilliAmps(std::int32_t value) = 0;
  virtual void setTLedCentiC(std::int16_t value) = 0;
  virtual void setTBtCentiC(std::int16_t value) = 0;
  virtual void setDoorOpen(bool value) = 0;
  virtual void setBluetoothState(std::uint8_t value) = 0;
};

class SensorBank final : public ISensors {
public:
  SensorReadings readings() const override;
  void setSupplyMilliVolts(std::int32_t value) override;
  void setCurrentMilliAmps(std::int32_t value) override;
  void setTLedCentiC(std::int16_t value) override;
  void setTBtCentiC(std::int16_t value) override;
  void setDoorOpen(bool value) override;
  void setBluetoothState(std::uint8_t value) override;

private:
  void updatePower();
  SensorReadings values_;
};

class IRelays {
public:
  virtual ~IRelays() = default;
  virtual void setRetainDirectionOnStop(bool retain) = 0;
  virtual bool set(std::uint8_t index, bool active) = 0;
  virtual bool setSide(std::uint8_t side, std::uint8_t motion) = 0;
  virtual void allOff() = 0;
  virtual std::uint8_t mask() const = 0;
};

class RelayBank final : public IRelays {
public:
  void setRetainDirectionOnStop(bool retain) override;
  bool set(std::uint8_t index, bool active) override;
  bool setSide(std::uint8_t side, std::uint8_t motion) override;
  void allOff() override;
  std::uint8_t mask() const override;

private:
  std::uint8_t mask_ = 0;
  bool retainDirectionOnStop_ = false;
};

class IPwm {
public:
  virtual ~IPwm() = default;
  virtual bool available() const = 0;
  virtual bool set(std::uint8_t channel, std::uint16_t value) = 0;
  virtual void allOff() = 0;
  virtual std::array<std::uint16_t, 16> values() const = 0;
  virtual std::uint16_t value(std::uint8_t channel) const = 0;
  virtual void select(std::uint8_t channel) = 0;
  virtual std::uint8_t selected() const = 0;
};

class PwmBank final : public IPwm {
public:
  explicit PwmBank(bool available = true) : available_(available) {}
  bool available() const override;
  bool set(std::uint8_t channel, std::uint16_t value) override;
  void allOff() override;
  std::array<std::uint16_t, 16> values() const override;
  std::uint16_t value(std::uint8_t channel) const override;
  void select(std::uint8_t channel) override;
  std::uint8_t selected() const override;

private:
  std::array<std::uint16_t, 16> values_{};
  std::uint8_t selected_ = 0;
  bool available_ = true;
};

constexpr std::size_t kAddressableLedPixelCount = 11;

struct AddressableLedColor {
  std::uint8_t red = 0;
  std::uint8_t green = 0;
  std::uint8_t blue = 0;
};

struct AddressableLedState {
  std::array<AddressableLedColor, kAddressableLedPixelCount> pixels{};
  std::uint8_t brightness = 255;
};

class IAddressableLeds {
public:
  virtual ~IAddressableLeds() = default;
  virtual bool setPixel(std::uint8_t index, AddressableLedColor color) = 0;
  virtual void fill(AddressableLedColor color) = 0;
  virtual void setBrightness(std::uint8_t brightness) = 0;
  virtual AddressableLedState state() const = 0;
};

class AddressableLedBank final : public IAddressableLeds {
public:
  bool setPixel(std::uint8_t index, AddressableLedColor color) override;
  void fill(AddressableLedColor color) override;
  void setBrightness(std::uint8_t brightness) override;
  AddressableLedState state() const override;

private:
  AddressableLedState state_;
};

struct DisplayState {
  std::string segments = "BOOT";
  std::string lcdLine1 = "PCController";
  std::string lcdLine2 = "Virtual board";
  std::uint16_t buzzerFrequencyHz = 0;
  std::uint16_t buzzerDurationMs = 0;
};

class IDisplays {
public:
  virtual ~IDisplays() = default;
  virtual void setSegments(const std::string &value) = 0;
  virtual void setLcd(const std::string &value) = 0;
  virtual void setBuzzer(std::uint16_t frequencyHz,
                         std::uint16_t durationMs) = 0;
  virtual DisplayState state() const = 0;
};

class DisplayBank final : public IDisplays {
public:
  void setSegments(const std::string &value) override;
  void setLcd(const std::string &value) override;
  void setBuzzer(std::uint16_t frequencyHz,
                 std::uint16_t durationMs) override;
  DisplayState state() const override;

private:
  DisplayState state_;
};

class IEeprom {
public:
  virtual ~IEeprom() = default;
  virtual std::size_t size() const = 0;
  virtual std::uint8_t read(std::size_t address) const = 0;
  virtual void update(std::size_t address, std::uint8_t value) = 0;
  virtual void fill(std::uint8_t value) = 0;
  virtual void flush() = 0;
  virtual std::filesystem::path path() const = 0;
};

class FileEeprom final : public IEeprom {
public:
  explicit FileEeprom(std::filesystem::path path,
                      std::size_t bytes = 1024);
  ~FileEeprom() override;

  std::size_t size() const override;
  std::uint8_t read(std::size_t address) const override;
  void update(std::size_t address, std::uint8_t value) override;
  void fill(std::uint8_t value) override;
  void flush() override;
  std::filesystem::path path() const override;

private:
  void load();
  std::filesystem::path path_;
  std::vector<std::uint8_t> bytes_;
  bool dirty_ = false;
};

} // namespace pccontroller::virtual_board
