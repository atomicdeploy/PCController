#pragma once

#include <Arduino.h>

// Front-key debounce, multi-click, and accelerated-hold timing in milliseconds.
// Keep debounce short: the primary action runs on the debounced Down edge and
// must remain perceptibly immediate even though Click classification is later.
constexpr uint16_t KEY_DEBOUNCE_MS = 20;
constexpr uint16_t KEY_DOUBLE_CLICK_MS = 300;
constexpr uint16_t KEY_HOLD_START_MS = 600;
constexpr uint16_t KEY_HOLD_REPEAT_MS = 150;
constexpr uint16_t KEY_HOLD_FAST_AFTER_MS = 1800;
constexpr uint16_t KEY_HOLD_REPEAT_FAST_MS = 60;

// KeyEvent is the debounced, classified lifecycle emitted for one front key.
enum class KeyEvent : uint8_t {
  Click = 0,
  DoubleClick,
  HoldStart,
  HoldRepeat,
  HoldRelease,
  Down,
  Up,
};

// Local and injected key lifecycles share one latency contract: the initial
// action belongs to Down, repeats belong to HoldRepeat, and Click/HoldStart are
// classification/telemetry only. This prevents the double-click window from
// ever becoming input latency.
constexpr bool keyEventRunsPrimaryAction(KeyEvent event) {
  return event == KeyEvent::Down || event == KeyEvent::HoldRepeat;
}

// Numeric front-panel editors advance one unit on the initial press, ten
// units during an ordinary hold, and one hundred units after the fast-repeat
// threshold. Navigation and binary selectors deliberately ignore this value.
constexpr uint8_t keyAdjustmentStep(KeyEvent event, uint16_t heldMs) {
  if (event != KeyEvent::HoldRepeat) {
    return 1;
  }
  return heldMs >= KEY_HOLD_FAST_AFTER_MS ? 100 : 10;
}

using KeyEventCallback =
    void (*)(uint8_t bit, KeyEvent event, void *context);

// Key classifies active-low samples into click, double-click, and hold events.
class Key {
public:
  explicit Key(uint8_t bit);

  void update(uint32_t now = millis());

  bool isPressed() const;
  bool isHeld() const;
  uint16_t heldForMs(uint32_t now = millis()) const;
  uint8_t inputBit() const;

  // Down/Up remain immediate after debounce; Click is deferred for telemetry
  // until the double-click window closes and a hold never leaks a Click event.
  void setEventCallback(KeyEventCallback callback, void *context = nullptr);

  explicit operator bool() const { return isPressed(); }

private:
  bool readCurrentState() const;
  void emitEvent(KeyEvent event);
  void handlePressed(uint16_t now);
  void handleReleased(uint16_t now);
  void serviceGestures(uint16_t now);

  uint8_t bit_;
  // All gesture intervals are far below 65.536 seconds, so 16-bit millisecond
  // timestamps preserve rollover safety while saving scarce ATmega328P SRAM.
  uint16_t rawChangedAt_ = 0;
  uint16_t lastRepeatAt_ = 0;
  uint16_t pendingClickAt_ = 0;
  uint8_t initialized_ : 1;
  uint8_t rawState_ : 1;
  uint8_t stableState_ : 1;
  uint8_t holdActive_ : 1;
  uint8_t clickPending_ : 1;
  KeyEventCallback eventCallback_ = nullptr;
  void *eventContext_ = nullptr;
};
