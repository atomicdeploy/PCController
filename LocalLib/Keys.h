#pragma once

#include <Arduino.h>

constexpr uint16_t KEY_DEBOUNCE_MS = 50;
constexpr uint16_t KEY_DOUBLE_CLICK_MS = 300;
constexpr uint16_t KEY_HOLD_START_MS = 600;
constexpr uint16_t KEY_HOLD_REPEAT_MS = 150;
constexpr uint16_t KEY_HOLD_FAST_AFTER_MS = 1800;
constexpr uint16_t KEY_HOLD_REPEAT_FAST_MS = 60;

using KeyCallback = void (*)(uint8_t bit, void *context);
using SimpleKeyCallback = void (*)();

enum class KeyEvent : uint8_t {
  Click = 0,
  DoubleClick,
  HoldStart,
  HoldRepeat,
  HoldRelease,
  Down,
  Up,
};

using KeyEventCallback =
    void (*)(uint8_t bit, KeyEvent event, void *context);

class Key {
public:
  explicit Key(uint8_t bit, SimpleKeyCallback callback = nullptr);

  void update(uint32_t now = millis());

  bool isPressed() const;
  bool isHeld() const;
  bool getCurrentState() const { return isPressed(); }
  uint8_t inputBit() const;

  // The press/release callbacks retain the immediate debounced behavior used
  // by existing sketches. Click events are delayed briefly so a second click
  // can be recognized without also emitting a single click.
  void setPressCallback(KeyCallback callback, void *context = nullptr);
  void setReleaseCallback(KeyCallback callback, void *context = nullptr);
  void setEventCallback(KeyEventCallback callback, void *context = nullptr);

  // Compatibility with the original LocalLib callback shape.
  void setCallback(SimpleKeyCallback callback);

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
  SimpleKeyCallback simpleCallback_ = nullptr;
  KeyCallback pressCallback_ = nullptr;
  KeyCallback releaseCallback_ = nullptr;
  KeyEventCallback eventCallback_ = nullptr;
  void *pressContext_ = nullptr;
  void *releaseContext_ = nullptr;
  void *eventContext_ = nullptr;
};
