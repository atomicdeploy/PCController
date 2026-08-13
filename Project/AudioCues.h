#pragma once

#include <Arduino.h>

enum class AudioCue : uint8_t {
  DoorOpen = 0,
  DoorClosed,
  OutputOn,
  OutputOff,
  Count,
};

class AudioCueStore {
public:
  static constexpr uint8_t RecordBytes = 13;
  void begin();
  void play(AudioCue cue);

private:
  bool valid_;
};

extern AudioCueStore audioCues;
