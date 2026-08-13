#include <Arduino.h>
#include <EEPROM.h>
#include <avr/interrupt.h>

#include "LocalLib/TonePlayer.h"
#include "Project/AudioCues.h"
#include "Project/EepromLayout.h"
#include "Project/UartProtocol.h"

#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

std::uint16_t timerTop(std::uint16_t frequencyHz) {
  const std::uint32_t denominator = 16UL * frequencyHz;
  return static_cast<std::uint16_t>(
      (F_CPU + denominator / 2UL) / denominator - 1UL);
}

void start(AudioCue cue, std::uint32_t now) {
  buzzer.stop();
  buzzer.begin();
  audioCues.begin();
  audioCues.play(cue);
  buzzer.update(now);
}

void requireTone(std::uint16_t frequencyHz, const std::string &message) {
  require((TCCR1B & (_BV(CS12) | _BV(CS11) | _BV(CS10))) == _BV(CS11) &&
              OCR1A == timerTop(frequencyHz),
          message);
}

void testBlankAndCorruptEEPROMUseHistoricalFallbacks() {
  EEPROM.fill(0xFF);
  start(AudioCue::DoorOpen, 100);
  requireTone(1700, "blank EEPROM lost the 1700 Hz door-open fallback");
  buzzer.update(144);
  require(buzzer.isBusy(), "door-open fallback ended before 45 ms");
  buzzer.update(145);
  require(!buzzer.isBusy(), "door-open fallback exceeded 45 ms");

  start(AudioCue::DoorClosed, 200);
  requireTone(1100, "blank EEPROM lost the 1100 Hz door-close fallback");

  start(AudioCue::OutputOn, 300);
  requireTone(1900, "blank EEPROM lost the motion/output-on tone");
  buzzer.update(334);
  require(buzzer.isBusy(), "motion/output-on fallback ended before 35 ms");
  buzzer.update(335);
  require(!buzzer.isBusy(), "motion/output-on fallback exceeded 35 ms");

  start(AudioCue::OutputOff, 400);
  requireTone(1250, "blank EEPROM lost the motion/output-off tone");

  EEPROM.update(EepromLayout::AudioCueAddress, 0x00);
  start(AudioCue::DoorOpen, 500);
  requireTone(1700, "corrupt EEPROM did not fall back atomically");
}

void testValidEEPROMOverridesCoreCue() {
  std::uint8_t record[AudioCueStore::RecordBytes] = {
      0xA0, 0x05, 55, // 1440 Hz, 55 ms
      0x4C, 0x04, 45, // 1100 Hz, 45 ms
      0x6C, 0x07, 35, // 1900 Hz, 35 ms
      0xE2, 0x04, 35, // 1250 Hz, 35 ms
      0,
  };
  record[AudioCueStore::RecordBytes - 1] =
      ControllerProtocol::UartProtocol::crc8(
          record, AudioCueStore::RecordBytes - 1);
  for (std::uint8_t index = 0; index < AudioCueStore::RecordBytes; ++index) {
    EEPROM.update(EepromLayout::AudioCueAddress + index, record[index]);
  }

  start(AudioCue::DoorOpen, 600);
  requireTone(1440, "valid EEPROM cue was not applied");
  buzzer.update(654);
  require(buzzer.isBusy(), "EEPROM duration ended before 55 ms");
  buzzer.update(655);
  require(!buzzer.isBusy(), "EEPROM duration exceeded 55 ms");
}

void testEntirePublicFrequencyRangeUsesHardwareTimer() {
  TonePlayer player(PIN_PB1);
  player.begin();
  player.beep(1, 20);
  player.update(700);
  requireTone(20, "20 Hz lower contract boundary did not fit Timer1 /8");
  player.stop();
  player.beep(1, 20000);
  player.update(800);
  requireTone(20000,
              "20 kHz upper contract boundary did not fit Timer1 /8");
}

} // namespace

int main() {
  try {
    testBlankAndCorruptEEPROMUseHistoricalFallbacks();
    testValidEEPROMOverridesCoreCue();
    testEntirePublicFrequencyRangeUsesHardwareTimer();
    std::cout << "firmware_audio_cue_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_audio_cue_tests: " << error.what() << '\n';
    return 1;
  }
}
