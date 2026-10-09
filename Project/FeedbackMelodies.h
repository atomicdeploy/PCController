#pragma once

#include "../LocalLib/TonePlayer.h"

// Reusable controller feedback cues. Each function replaces the current queue
// and returns immediately; TonePlayer::update() performs cooperative playback.
void finishMelody(TonePlayer &player = buzzer);
void lostMelody(TonePlayer &player = buzzer);
void incorrectBeep(TonePlayer &player = buzzer);
void errorBeep(TonePlayer &player = buzzer);
void faultBeep(TonePlayer &player = buzzer);
