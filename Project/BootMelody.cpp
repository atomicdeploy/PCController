#include "BootMelody.h"

void playBootMelody(TonePlayer &player) {
  player.stop();

  player.enqueue(1032, 70);
  player.pause(60);
  player.enqueue(2010, 70);
  player.pause(60);
  player.enqueue(2400, 120);
  player.pause(150);
}
