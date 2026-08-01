#include "TonePlayer.h"

#include <avr/interrupt.h>

static_assert(BoardPins::Buzzer == PIN_PB1,
              "Timer1 OC1A buzzer output must remain on PB1 / Arduino D9");

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

  uint16_t top;
  uint8_t clockBits;
  if (!timerSettings(frequencyHz, top, clockBits)) {
    stopHardwareTone();
    return false;
  }

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
  TCCR1B = static_cast<uint8_t>(_BV(WGM12) | clockBits);

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

bool TonePlayer::timerSettings(uint16_t frequencyHz, uint16_t &top,
                               uint8_t &clockBits) {
  if (frequencyHz == 0) {
    return false;
  }

  return timerSettingForPrescaler(frequencyHz, 1, _BV(CS10), top,
                                  clockBits) ||
         timerSettingForPrescaler(frequencyHz, 8, _BV(CS11), top,
                                  clockBits) ||
         timerSettingForPrescaler(
             frequencyHz, 64,
             static_cast<uint8_t>(_BV(CS11) | _BV(CS10)), top, clockBits) ||
         timerSettingForPrescaler(frequencyHz, 256, _BV(CS12), top,
                                  clockBits) ||
         timerSettingForPrescaler(
             frequencyHz, 1024,
             static_cast<uint8_t>(_BV(CS12) | _BV(CS10)), top, clockBits);
}

bool TonePlayer::timerSettingForPrescaler(uint16_t frequencyHz,
                                          uint16_t divisor,
                                          uint8_t candidateClockBits,
                                          uint16_t &top,
                                          uint8_t &clockBits) {
  const uint32_t denominator =
      2UL * divisor * static_cast<uint32_t>(frequencyHz);
  uint32_t ticks = (F_CPU + denominator / 2UL) / denominator;
  if (ticks == 0) {
    ticks = 1;
  }
  if (ticks > 65536UL) {
    return false;
  }
  top = static_cast<uint16_t>(ticks - 1UL);
  clockBits = candidateClockBits;
  return true;
}

void setupBuzzer() { buzzer.begin(); }

void addTone(unsigned int frequency, unsigned int duration) {
  buzzer.enqueue(static_cast<uint16_t>(frequency),
                 static_cast<uint16_t>(duration));
}

void resetTone() { buzzer.stop(); }
void playToneSequence() { buzzer.update(); }
bool isPlayingTones() { return buzzer.isBusy(); }
