#include "TonePlayer.h"

#include <avr/interrupt.h>

static_assert(BoardPins::Buzzer == PIN_PB1,
              "Timer1 OC1A buzzer output must remain on PB1 / Arduino D9");
static_assert(F_CPU == 16000000UL,
              "TonePlayer's compact Timer1 divider assumes the 16 MHz profile");

TonePlayer buzzer(BoardPins::Buzzer);

TonePlayer::TonePlayer(uint8_t pin) : pin_(pin) {}

void TonePlayer::begin() {
  pinMode(pin_, OUTPUT);
  stop();
}

bool TonePlayer::enqueue(uint16_t frequencyHz, uint16_t durationMs) {
  if (durationMs == 0 || count_ >= MAX_TONES) {
    return false;
  }

  queue_[tail_] = {frequencyHz, durationMs};
  tail_ = static_cast<uint8_t>((tail_ + 1) % MAX_TONES);
  ++count_;
  return true;
}

bool TonePlayer::pause(uint16_t durationMs) {
  return enqueue(0, durationMs);
}

void TonePlayer::beep(uint16_t durationMs, uint16_t frequencyHz) {
  enqueue(frequencyHz, durationMs);
}

void TonePlayer::success() {
  stop();
  enqueue(NOTE_C6, 70);
  pause(30);
  enqueue(NOTE_E6, 110);
}

void TonePlayer::error() {
  stop();
  enqueue(NOTE_E4, 90);
  pause(50);
  enqueue(NOTE_C4, 160);
}

void TonePlayer::update(uint32_t now) {
  if (stepActive_) {
    if (static_cast<int32_t>(now - stepEndsAt_) < 0) {
      return;
    }
    stopHardwareTone();
    stepActive_ = false;
  }

  if (count_ == 0) {
    return;
  }

  const ToneStep step = queue_[head_];
  head_ = static_cast<uint8_t>((head_ + 1) % MAX_TONES);
  --count_;

  activeFrequencyHz_ = step.frequencyHz;
  activeDurationMs_ = step.durationMs;
  ++revision_;

  if (!muted_ && step.frequencyHz != 0) {
    startHardwareTone(step.frequencyHz);
  } else {
    stopHardwareTone();
  }
  stepEndsAt_ = now + step.durationMs;
  stepActive_ = true;
}

void TonePlayer::stop() {
  stopHardwareTone();
  head_ = 0;
  tail_ = 0;
  count_ = 0;
  stepEndsAt_ = 0;
  stepActive_ = false;
  if (activeFrequencyHz_ != 0 || activeDurationMs_ != 0) {
    activeFrequencyHz_ = 0;
    activeDurationMs_ = 0;
    ++revision_;
  }
}

void TonePlayer::setMuted(bool muted) {
  muted_ = muted;
  if (muted_) {
    stopHardwareTone();
  }
}

bool TonePlayer::isBusy() const { return stepActive_ || count_ != 0; }

bool TonePlayer::startHardwareTone(uint16_t frequencyHz) {
  if (pin_ != BoardPins::Buzzer || frequencyHz == 0) {
    stopHardwareTone();
    return false;
  }

  // The native/host contract is 20..20,000 Hz. Every legal value fits Timer1
  // CTC with the /8 prescaler at 16 MHz, so a multi-prescaler search only
  // duplicated 32-bit division paths on the constrained image.
  if (frequencyHz < 20U || frequencyHz > 20000U) {
    stopHardwareTone();
    return false;
  }
  const uint16_t top = static_cast<uint16_t>(
      (1000000UL + frequencyHz / 2U) / frequencyHz - 1UL);

  const uint8_t savedStatus = SREG;
  cli();

  // Timer1 CTC with OCR1A as TOP and OC1A toggled by hardware. No Timer1 ISR
  // is enabled, so audio output cannot delay the INT0/INT1 RC edge handlers.
  TCCR1A = 0;
  TCCR1B = 0;
  TIMSK1 &= static_cast<uint8_t>(~_BV(OCIE1A));
  TCNT1 = 0;
  OCR1A = top;
  TIFR1 = _BV(OCF1A);
  TCCR1A = _BV(COM1A0);
  TCCR1B = static_cast<uint8_t>(_BV(WGM12) | _BV(CS11));

  SREG = savedStatus;
  return true;
}

void TonePlayer::stopHardwareTone() {
  const uint8_t savedStatus = SREG;
  cli();

  // Disconnect OC1A first, then stop Timer1's clock. Timer0 (millis/micros)
  // and Timer2 are deliberately untouched.
  TCCR1A &= static_cast<uint8_t>(~_BV(COM1A0));
  TCCR1B &= static_cast<uint8_t>(
      ~(_BV(CS12) | _BV(CS11) | _BV(CS10)));

  SREG = savedStatus;
  digitalWrite(pin_, LOW);
}
