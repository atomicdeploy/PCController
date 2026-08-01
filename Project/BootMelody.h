#pragma once

#include "../LocalLib/TonePlayer.h"

// Queue the original Puzzles welcome melody without blocking the main loop.
// Calling this function replaces any melody currently queued in the player.
void playBootMelody(TonePlayer &player = buzzer);

// Compatibility name used by the inherited project layer.
void welcomeMelody();
