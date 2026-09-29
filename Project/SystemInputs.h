#pragma once

#include <Arduino.h>

// BluetoothIndicatorState classifies the BT Audio indicator's steady/blink signal.
enum class BluetoothIndicatorState : uint8_t {
  Off = 0,
  On,
  Blinking,
};

// Debounces reed/BT Audio sense bits and categorizes the BT LED blink pattern.
class SystemInputs {
public:
  // Seeds stable values from the first shift-register sample.
  void begin(uint8_t rawInputs, uint32_t now = millis());
  // Advances debouncing and edge/pattern tracking from one raw input byte.
  void update(uint8_t rawInputs, uint32_t now = millis());

  bool doorOpen() const;
  bool bluetoothLedOn() const;
  BluetoothIndicatorState bluetoothState(uint32_t now = millis()) const;

  bool doorRawHigh() const;
  bool bluetoothRawHigh() const;
  uint8_t rawInputs() const;

  // Edge consumers return true once per debounced transition.
  bool consumeDoorChange(bool &doorOpen);
  bool consumeBluetoothEdge(bool &ledOn);
  uint32_t lastBluetoothOnMs() const;
  uint32_t lastBluetoothOffMs() const;

private:
  // One input's raw sample, debounced state, pending edge, and timestamps.
  struct DebouncedInput {
    bool sample;
    bool stable;
    bool initialized;
    bool changedPending;
    uint32_t sampleChangedAt;
    uint32_t stableChangedAt;
  };

  static bool updateInput(DebouncedInput &input, bool sample, uint32_t now);
  static bool bitHigh(uint8_t value, uint8_t bit);

  DebouncedInput door_;
  DebouncedInput bluetooth_;
  // begin() seeds the real active-low sample before any consumer runs. Keeping
  // the static object all-zero avoids copying its entire state from flash.
  uint8_t rawInputs_;
  bool bluetoothHasTransitioned_;
  bool bluetoothBlinkObserved_;
  uint32_t lastBluetoothTransitionAt_;
  uint32_t lastBluetoothOnMs_;
  uint32_t lastBluetoothOffMs_;
};

// systemInputs is the single debounced door and BT Audio input service.
extern SystemInputs systemInputs;
