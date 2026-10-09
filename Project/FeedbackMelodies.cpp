#include "FeedbackMelodies.h"

void finishMelody(TonePlayer &player) {
  player.stop();
  player.enqueue(NOTE_E5, 100);
  player.enqueue(NOTE_G5, 100);
  player.enqueue(NOTE_A5, 250);
}

void lostMelody(TonePlayer &player) {
  player.stop();
  player.enqueue(NOTE_G4, 100);
  player.enqueue(NOTE_E4, 100);
  player.enqueue(NOTE_C4, 100);
  player.enqueue(NOTE_G3, 100);
}

void incorrectBeep(TonePlayer &player) {
  player.stop();
  for (uint8_t index = 0; index < 3; ++index) {
    player.beep(100);
    player.pause(100);
  }
}

void errorBeep(TonePlayer &player) {
  player.stop();
  for (uint8_t index = 0; index < 5; ++index) {
    player.beep(10);
    player.pause(10);
  }
}

void faultBeep(TonePlayer &player) {
  player.stop();
  player.enqueue(1000, 250);
  player.enqueue(500, 500);
  player.pause(5000);
}
