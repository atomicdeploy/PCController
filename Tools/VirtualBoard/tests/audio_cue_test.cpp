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
  return static_cast<std::uint16_t>(
      (1000000UL + frequencyHz / 2UL) / frequencyHz - 1UL);
}

void start(AudioCue cue, std::uint32_t now) {
  buzzer.stop();
  buzzer.setMuted(false);
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

void testBlankAndCorruptEEPROMUseExactFallbacks() {
  EEPROM.fill(0xFF);
  start(AudioCue::DoorOpen, 100);
  require(!audioCues.usingEEPROM(), "blank EEPROM was accepted");
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
  require(buzzer.isBusy(), "output-on fallback ended before 35 ms");
  buzzer.update(335);
  require(!buzzer.isBusy(), "output-on fallback exceeded 35 ms");

  start(AudioCue::OutputOff, 400);
  requireTone(1250, "blank EEPROM lost the motion/output-off tone");

  EEPROM.update(EepromLayout::AudioCueAddress, 0x00);
  start(AudioCue::DoorOpen, 500);
  requireTone(1700, "corrupt EEPROM did not fall back atomically");
}

void testValidEEPROMOverridesAndInvalidDescriptorRejectsWholeRecord() {
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
  require(audioCues.usingEEPROM(), "valid EEPROM cue record was rejected");
  requireTone(1440, "valid EEPROM cue was not applied");
  buzzer.update(654);
  require(buzzer.isBusy(), "EEPROM duration ended before 55 ms");
  buzzer.update(655);
  require(!buzzer.isBusy(), "EEPROM duration exceeded 55 ms");

  record[3] = 0;
  record[4] = 0;
  record[AudioCueStore::RecordBytes - 1] =
      ControllerProtocol::UartProtocol::crc8(
          record, AudioCueStore::RecordBytes - 1);
  for (std::uint8_t index = 0; index < AudioCueStore::RecordBytes; ++index) {
    EEPROM.update(EepromLayout::AudioCueAddress + index, record[index]);
  }
  start(AudioCue::DoorOpen, 700);
  require(!audioCues.usingEEPROM(),
          "one invalid descriptor did not reject the record atomically");
  requireTone(1700, "invalid descriptor did not restore exact fallback");
}

void testSpecificCuePreemptsGenericAndSilentSuppressesHardware() {
  EEPROM.fill(0xFF);
  buzzer.begin();
  buzzer.setMuted(false);
  buzzer.beep(40, 2000);
  audioCues.begin();
  audioCues.play(AudioCue::OutputOn);
  buzzer.update(800);
  requireTone(1900, "specific output cue was queued behind generic feedback");

  buzzer.stop();
  buzzer.setMuted(true);
  audioCues.play(AudioCue::DoorClosed);
  buzzer.update(900);
  require((TCCR1B & (_BV(CS12) | _BV(CS11) | _BV(CS10))) == 0,
          "global Silent allowed autonomous hardware tone output");
  buzzer.setMuted(false);
  buzzer.stop();
}

void testEntirePublicFrequencyRangeUsesFixedHardwareDivider() {
  TonePlayer player(PIN_PB1);
  player.begin();
  player.beep(1, 20);
  player.update(1000);
  requireTone(20, "20 Hz lower contract boundary did not fit Timer1 /8");
  player.stop();
  player.beep(1, 20000);
  player.update(1100);
  requireTone(20000, "20 kHz upper contract boundary did not fit Timer1 /8");
}

} // namespace

int main() {
  try {
    testBlankAndCorruptEEPROMUseExactFallbacks();
    testValidEEPROMOverridesAndInvalidDescriptorRejectsWholeRecord();
    testSpecificCuePreemptsGenericAndSilentSuppressesHardware();
    testEntirePublicFrequencyRangeUsesFixedHardwareDivider();
    std::cout << "firmware_audio_cue_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_audio_cue_tests: " << error.what() << '\n';
    return 1;
  }
}
