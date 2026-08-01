#pragma once

#include <Arduino.h>

enum class BluetoothIndicatorState : uint8_t {
  Off = 0,
  On,
  Blinking,
};

class SystemInputs {
public:
  void begin(uint8_t rawInputs, uint32_t now = millis());
  void update(uint8_t rawInputs, uint32_t now = millis());

  bool doorOpen() const;
  bool bluetoothLedOn() const;
  BluetoothIndicatorState bluetoothState(uint32_t now = millis()) const;

  bool doorRawHigh() const;
  bool bluetoothRawHigh() const;
  uint8_t rawInputs() const;

  bool consumeDoorChange(bool &doorOpen);
  bool consumeBluetoothEdge(bool &ledOn);
  uint32_t lastBluetoothOnMs() const;
  uint32_t lastBluetoothOffMs() const;

private:
  struct DebouncedInput {
    bool sample = false;
    bool stable = false;
    bool initialized = false;
    bool changedPending = false;
    uint32_t sampleChangedAt = 0;
    uint32_t stableChangedAt = 0;
  };

  static bool updateInput(DebouncedInput &input, bool sample, uint32_t now);
  static bool bitHigh(uint8_t value, uint8_t bit);

  DebouncedInput door_;
  DebouncedInput bluetooth_;
  uint8_t rawInputs_ = 0xFF;
  bool bluetoothHasTransitioned_ = false;
  bool bluetoothBlinkObserved_ = false;
  uint32_t lastBluetoothTransitionAt_ = 0;
  uint32_t lastBluetoothOnMs_ = 0;
  uint32_t lastBluetoothOffMs_ = 0;
};

extern SystemInputs systemInputs;
