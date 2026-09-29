#pragma once

#include <Arduino.h>

#include "BoardPins.h"
#include "pitches.h"

// TonePlayer queues nonblocking tones on the board's Timer1 buzzer output.
class TonePlayer {
public:
  TonePlayer() = default;
  // ATmega328P implementation owns Timer1 and the PB1/OC1A pin. It uses
  // hardware compare toggling (no audio-rate ISR); do not combine it with
  // Servo or analogWrite() on D9/D10.
  explicit TonePlayer(uint8_t pin);

  void begin(uint8_t pin);
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

  uint8_t pin_;
  ToneStep queue_[MAX_TONES];
  uint8_t head_;
  uint8_t tail_;
  uint8_t count_;
  uint32_t stepEndsAt_;
  bool stepActive_;
  bool muted_;
  uint8_t revision_;
  uint16_t activeFrequencyHz_;
  uint16_t activeDurationMs_;
};

// buzzer is the single board-wide feedback player.
extern TonePlayer buzzer;
