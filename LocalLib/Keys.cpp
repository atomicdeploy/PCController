#include "Keys.h"
#include "ShiftRegisters.h"

Key::Key(uint8_t bit)
    : bit_(bit), initialized_(false), rawState_(false), stableState_(false),
      holdActive_(false), clickPending_(false) {}

void Key::update(uint32_t now) {
  const uint16_t tick = static_cast<uint16_t>(now);
  const bool currentRawState = readCurrentState();

  if (!initialized_) {
    initialized_ = true;
    rawState_ = currentRawState;
    stableState_ = currentRawState;
    rawChangedAt_ = tick;
    if (stableState_) {
      lastRepeatAt_ = tick;
    }
    return;
  }

  if (currentRawState != rawState_) {
    rawState_ = currentRawState;
    rawChangedAt_ = tick;
  } else if (stableState_ != rawState_ &&
             static_cast<uint16_t>(tick - rawChangedAt_) >=
                 KEY_DEBOUNCE_MS) {
    stableState_ = rawState_;
    if (stableState_) {
      handlePressed(tick);
    } else {
      handleReleased(tick);
    }
  }

  serviceGestures(tick);
}

bool Key::isPressed() const { return stableState_; }

bool Key::isHeld() const { return holdActive_; }

uint8_t Key::inputBit() const { return bit_; }

void Key::setEventCallback(KeyEventCallback callback, void *context) {
  eventCallback_ = callback;
  eventContext_ = context;
}

bool Key::readCurrentState() const {
  return shiftRegisters.inputActive(bit_);
}

void Key::emitEvent(KeyEvent event) {
  if (eventCallback_ != nullptr) {
    eventCallback_(bit_, event, eventContext_);
  }
}

void Key::handlePressed(uint16_t now) {
  lastRepeatAt_ = now;
  holdActive_ = false;
  emitEvent(KeyEvent::Down);
}

void Key::handleReleased(uint16_t now) {
  emitEvent(KeyEvent::Up);

  if (holdActive_) {
    holdActive_ = false;
    clickPending_ = false;
    emitEvent(KeyEvent::HoldRelease);
    return;
  }

  if (clickPending_) {
    if (static_cast<uint16_t>(now - pendingClickAt_) <=
        KEY_DOUBLE_CLICK_MS) {
      clickPending_ = false;
      emitEvent(KeyEvent::DoubleClick);
      return;
    }
    emitEvent(KeyEvent::Click);
  }

  clickPending_ = true;
  pendingClickAt_ = now;
}

void Key::serviceGestures(uint16_t now) {
  if (stableState_) {
    if (!holdActive_ &&
        static_cast<uint16_t>(now - lastRepeatAt_) >= KEY_HOLD_START_MS) {
      if (clickPending_) {
        clickPending_ = false;
        emitEvent(KeyEvent::Click);
      }
      holdActive_ = true;
      lastRepeatAt_ = now;
      emitEvent(KeyEvent::HoldStart);
    } else if (holdActive_) {
      // The raw edge timestamp remains the original press edge while held.
      // After a deliberate long hold, accelerate value editing without
      // allocating another per-key timestamp on the small AVR.
      const uint16_t repeatInterval =
          static_cast<uint16_t>(now - rawChangedAt_) >=
                  KEY_HOLD_FAST_AFTER_MS
              ? KEY_HOLD_REPEAT_FAST_MS
              : KEY_HOLD_REPEAT_MS;
      if (static_cast<uint16_t>(now - lastRepeatAt_) < repeatInterval) {
        return;
      }
      lastRepeatAt_ = now;
      emitEvent(KeyEvent::HoldRepeat);
    }
    return;
  }

  if (clickPending_ &&
      static_cast<uint16_t>(now - pendingClickAt_) >
          KEY_DOUBLE_CLICK_MS) {
    clickPending_ = false;
    emitEvent(KeyEvent::Click);
  }
}
