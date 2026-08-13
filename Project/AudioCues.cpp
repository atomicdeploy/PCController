#include "AudioCues.h"

#include <EEPROM.h>
#include <avr/pgmspace.h>

#include "../LocalLib/TonePlayer.h"
#include "EepromLayout.h"
#include "UartProtocol.h"

namespace {
const uint16_t FallbackHz[] PROGMEM = {1700, 1100, 1900, 1250};
}

AudioCueStore audioCues;

void AudioCueStore::begin() {
  uint8_t record[RecordBytes];
  EEPROM.get(EepromLayout::AudioCueAddress, record);
  valid_ = record[RecordBytes - 1U] ==
           ControllerProtocol::UartProtocol::crc8(record, RecordBytes - 1U);
}

void AudioCueStore::play(AudioCue cue) {
  const uint8_t index = static_cast<uint8_t>(cue);
  uint16_t frequency = pgm_read_word(FallbackHz + index);
  uint8_t duration = index < 2 ? 45 : 35;
  if (valid_) {
    const int address = EepromLayout::AudioCueAddress + index * 3;
    frequency = static_cast<uint16_t>(EEPROM.read(address)) |
                static_cast<uint16_t>(EEPROM.read(address + 1)) << 8;
    duration = EEPROM.read(address + 2);
  }
  buzzer.beep(duration, frequency);
}
