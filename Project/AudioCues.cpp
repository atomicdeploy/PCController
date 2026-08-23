#include "AudioCues.h"

#include <EEPROM.h>

#include "../LocalLib/TonePlayer.h"
#include "../ProjectConfig.h"
#include "EepromLayout.h"
#include "UartProtocol.h"

namespace {

void fallbackCue(AudioCue cue, uint16_t &frequency, uint8_t &duration) {
  switch (cue) {
    case AudioCue::DoorOpen:
      frequency = 1700;
      duration = 45;
      return;
    case AudioCue::DoorClosed:
      frequency = 1100;
      duration = 45;
      return;
    case AudioCue::OutputOn:
      frequency = 1900;
      duration = 35;
      return;
    case AudioCue::OutputOff:
      frequency = 1250;
      duration = 35;
      return;
    case AudioCue::Count:
      break;
  }
  frequency = 0;
  duration = 0;
}

} // namespace

AudioCueStore audioCues;

static_assert(AudioCueStore::RecordBytes == EepromLayout::AudioCueBytes,
              "audio cue controller and EEPROM layout disagree");

void AudioCueStore::begin() {
  valid_ = false;
#if PCCONTROLLER_ENABLE_EEPROM_AUDIO_CUES
  uint8_t record[RecordBytes];
  EEPROM.get(EepromLayout::AudioCueAddress, record);
  if (record[RecordBytes - 1U] !=
      ControllerProtocol::UartProtocol::crc8(record, RecordBytes - 1U)) {
    return;
  }
  for (uint8_t offset = 0; offset < RecordBytes - 1U; offset += 3U) {
    const uint16_t frequency = static_cast<uint16_t>(record[offset]) |
                               static_cast<uint16_t>(record[offset + 1U]) << 8;
    if (frequency < 20U || frequency > 20000U || record[offset + 2U] == 0) {
      return;
    }
  }
  valid_ = true;
#endif
}

void AudioCueStore::play(AudioCue cue) {
  const uint8_t index = static_cast<uint8_t>(cue);
  if (index >= static_cast<uint8_t>(AudioCue::Count)) {
    return;
  }

  uint16_t frequency;
  uint8_t duration;
  fallbackCue(cue, frequency, duration);
#if PCCONTROLLER_ENABLE_EEPROM_AUDIO_CUES
  if (valid_) {
    const int address = EepromLayout::AudioCueAddress + index * 3U;
    frequency = static_cast<uint16_t>(EEPROM.read(address)) |
                static_cast<uint16_t>(EEPROM.read(address + 1)) << 8;
    duration = EEPROM.read(address + 2);
  }
#endif

  // A physical-state cue supersedes any queued generic navigation beep. The
  // muted bit lives in TonePlayer and survives stop(), so global Silent still
  // suppresses the replacement cue electrically.
  buzzer.stop();
  buzzer.beep(duration, frequency);
}
