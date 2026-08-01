#pragma once

#include "../LocalLib/TonePlayer.h"

// Reusable feedback cues retained from the inherited ControllerBoardMini
// project layer. All functions replace the current queue and return
// immediately; TonePlayer::update() performs playback in the main loop.
void finishMelody(TonePlayer &player = buzzer);
void lostMelody(TonePlayer &player = buzzer);
void incorrectBeep(TonePlayer &player = buzzer);
void errorBeep(TonePlayer &player = buzzer);
void faultBeep(TonePlayer &player = buzzer);
