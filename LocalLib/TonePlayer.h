#pragma once

#include <Arduino.h>

#include "BoardPins.h"
#include "pitches.h"

// TonePlayer queues nonblocking tones on the board's Timer1 buzzer output.
class TonePlayer {
public:
  // ATmega328P implementation owns Timer1 and the PB1/OC1A pin. It uses
  // hardware compare toggling (no audio-rate ISR); do not combine it with
  // Servo or analogWrite() on D9/D10.
  explicit TonePlayer(uint8_t pin);

  void begin();
  bool enqueue(uint16_t frequencyHz, uint16_t durationMs);
  bool pause(uint16_t durationMs);
  void beep(uint16_t durationMs = 40, uint16_t frequencyHz = 2000);
  void success();
  void error();
  void update(uint32_t now = millis());
  void stop();
  void setMuted(bool muted);
  bool isBusy() const;
  uint8_t revision() const { return revision_; }
  uint16_t activeFrequencyHz() const { return activeFrequencyHz_; }
  uint16_t activeDurationMs() const { return activeDurationMs_; }
  bool muted() const { return muted_; }

private:
  // ToneStep is one queued tone or silent pause and its duration.
  struct ToneStep {
    uint16_t frequencyHz;
    uint16_t durationMs;
  };

  static constexpr uint8_t MAX_TONES = 10;

  bool startHardwareTone(uint16_t frequencyHz);
  void stopHardwareTone();
  static bool timerSettings(uint16_t frequencyHz, uint16_t &top,
                            uint8_t &clockBits);
  static bool timerSettingForPrescaler(uint16_t frequencyHz,
                                       uint16_t divisor,
                                       uint8_t candidateClockBits,
                                       uint16_t &top, uint8_t &clockBits);

  uint8_t pin_;
  ToneStep queue_[MAX_TONES];
  uint8_t head_ = 0;
  uint8_t tail_ = 0;
  uint8_t count_ = 0;
  uint32_t stepEndsAt_ = 0;
  bool stepActive_ = false;
  bool muted_ = false;
  uint8_t revision_ = 0;
  uint16_t activeFrequencyHz_ = 0;
  uint16_t activeDurationMs_ = 0;
};

// buzzer is the single board-wide feedback player.
extern TonePlayer buzzer;
