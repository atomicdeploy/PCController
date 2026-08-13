#include "SystemInputs.h"

#include "../LocalLib/BoardPins.h"
#include "../ProjectConfig.h"

namespace {

// Input debounce and BT Audio blink-retention windows in milliseconds.
constexpr uint16_t SenseDebounceMs = 40;
#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
constexpr uint16_t BluetoothBlinkHoldMs = 2500;
#endif

bool interpretedLevel(bool rawHigh, bool activeWhenRawHigh) {
  return activeWhenRawHigh ? rawHigh : !rawHigh;
}

} // namespace

SystemInputs systemInputs;

void SystemInputs::begin(uint8_t rawInputs, uint32_t now) {
  rawInputs_ = rawInputs;

  const bool doorSample =
      interpretedLevel(bitHigh(rawInputs_, BoardPins::DoorReedSense),
                       PCCONTROLLER_DOOR_OPEN_RAW_HIGH != 0);
  door_.sample = doorSample;
  door_.stable = doorSample;
  door_.initialized = true;
  door_.sampleChangedAt = now;
  door_.stableChangedAt = now;
  door_.changedPending = false;

#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
  const bool bluetoothSample =
      interpretedLevel(bitHigh(rawInputs_, BoardPins::BluetoothLedSense),
                       PCCONTROLLER_BT_LED_ON_RAW_HIGH != 0);
  bluetooth_.sample = bluetoothSample;
  bluetooth_.stable = bluetoothSample;
  bluetooth_.initialized = true;
  bluetooth_.sampleChangedAt = now;
  bluetooth_.stableChangedAt = now;
  bluetooth_.changedPending = false;

  bluetoothHasTransitioned_ = false;
  bluetoothBlinkObserved_ = false;
  lastBluetoothTransitionAt_ = now;
  lastBluetoothOnMs_ = 0;
  lastBluetoothOffMs_ = 0;
#endif
}

void SystemInputs::update(uint8_t rawInputs, uint32_t now) {
  rawInputs_ = rawInputs;

  const bool doorSample =
      interpretedLevel(bitHigh(rawInputs_, BoardPins::DoorReedSense),
                       PCCONTROLLER_DOOR_OPEN_RAW_HIGH != 0);
  updateInput(door_, doorSample, now);

#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
  const bool bluetoothSample =
      interpretedLevel(bitHigh(rawInputs_, BoardPins::BluetoothLedSense),
                       PCCONTROLLER_BT_LED_ON_RAW_HIGH != 0);
  const bool previousBluetoothState = bluetooth_.stable;
  const uint32_t previousBluetoothChangedAt = bluetooth_.stableChangedAt;
  if (updateInput(bluetooth_, bluetoothSample, now)) {
    const uint32_t elapsed = now - previousBluetoothChangedAt;
    if (previousBluetoothState) {
      lastBluetoothOnMs_ = elapsed;
    } else {
      lastBluetoothOffMs_ = elapsed;
    }
    const uint32_t transitionGap = now - lastBluetoothTransitionAt_;
    bluetoothBlinkObserved_ =
        bluetoothHasTransitioned_ && transitionGap <= BluetoothBlinkHoldMs;
    bluetoothHasTransitioned_ = true;
    lastBluetoothTransitionAt_ = now;
  }
#endif
}

bool SystemInputs::doorOpen() const { return door_.stable; }

bool SystemInputs::bluetoothLedOn() const {
#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
  return bluetooth_.stable;
#else
  return false;
#endif
}

BluetoothIndicatorState
SystemInputs::bluetoothState(uint32_t now) const {
#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
  if (bluetoothBlinkObserved_ &&
      static_cast<uint32_t>(now - lastBluetoothTransitionAt_) <=
          BluetoothBlinkHoldMs) {
    return BluetoothIndicatorState::Blinking;
  }
  return bluetooth_.stable ? BluetoothIndicatorState::On
                           : BluetoothIndicatorState::Off;
#else
  (void)now;
  return BluetoothIndicatorState::Off;
#endif
}

bool SystemInputs::doorRawHigh() const {
  return bitHigh(rawInputs_, BoardPins::DoorReedSense);
}

bool SystemInputs::bluetoothRawHigh() const {
#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
  return bitHigh(rawInputs_, BoardPins::BluetoothLedSense);
#else
  return false;
#endif
}

uint8_t SystemInputs::rawInputs() const { return rawInputs_; }

bool SystemInputs::consumeDoorChange(bool &doorOpenValue) {
  if (!door_.changedPending) {
    return false;
  }
  door_.changedPending = false;
  doorOpenValue = door_.stable;
  return true;
}

bool SystemInputs::consumeBluetoothEdge(bool &ledOnValue) {
#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
  if (!bluetooth_.changedPending) {
    return false;
  }
  bluetooth_.changedPending = false;
  ledOnValue = bluetooth_.stable;
  return true;
#else
  (void)ledOnValue;
  return false;
#endif
}

uint32_t SystemInputs::lastBluetoothOnMs() const {
#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
  return lastBluetoothOnMs_;
#else
  return 0;
#endif
}

uint32_t SystemInputs::lastBluetoothOffMs() const {
#if PCCONTROLLER_ENABLE_BT_LED_DETECTION
  return lastBluetoothOffMs_;
#else
  return 0;
#endif
}

bool SystemInputs::updateInput(DebouncedInput &input, bool sample,
                               uint32_t now) {
  if (!input.initialized) {
    input.sample = sample;
    input.stable = sample;
    input.initialized = true;
    input.sampleChangedAt = now;
    input.stableChangedAt = now;
    return false;
  }

  if (sample != input.sample) {
    input.sample = sample;
    input.sampleChangedAt = now;
    return false;
  }

  if (input.stable == input.sample ||
      static_cast<uint32_t>(now - input.sampleChangedAt) < SenseDebounceMs) {
    return false;
  }

  input.stable = input.sample;
  input.stableChangedAt = now;
  input.changedPending = true;
  return true;
}

bool SystemInputs::bitHigh(uint8_t value, uint8_t bit) {
  return (value & _BV(bit)) != 0;
}
