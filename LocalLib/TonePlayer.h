#pragma once

#include <Arduino.h>

#include "BoardPins.h"
#include "pitches.h"

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

private:
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
};

extern TonePlayer buzzer;

// Compatibility wrappers retained for reusable inherited code.
void setupBuzzer();
void addTone(unsigned int frequency, unsigned int duration);
void resetTone();
void playToneSequence();
bool isPlayingTones();
