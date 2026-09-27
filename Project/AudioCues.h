#pragma once

#include <Arduino.h>

// AudioCue identifies the four short physical-state cues that must remain
// distinct and available without a host. Rich named melodies remain host-owned.
enum class AudioCue : uint8_t {
  DoorOpen = 0,
  DoorClosed,
  OutputOn,
  OutputOff,
  Count,
};

// AudioCueStore is the compact policy/controller layered over TonePlayer. It
// validates one atomic EEPROM record at boot and otherwise uses exact built-in
// fallbacks; it never owns Timer1 or duplicates the playback engine.
class AudioCueStore {
public:
  static constexpr uint8_t RecordBytes = 13;

  void begin();
  void play(AudioCue cue);
  bool usingEEPROM() const { return valid_; }

private:
  // Global static storage zero-initializes this before begin(), avoiding a
  // constructor solely to write the same value on the constrained AVR.
  bool valid_;
};

extern AudioCueStore audioCues;
