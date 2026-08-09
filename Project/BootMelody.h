#pragma once

#include "../LocalLib/TonePlayer.h"

// Queue the project welcome melody without blocking the main loop.
// Calling this function replaces any melody currently queued in the player.
void playBootMelody(TonePlayer &player = buzzer);
