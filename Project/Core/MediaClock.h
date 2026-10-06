#pragma once
#include <stdint.h>

// Virtual-board model of the physical firmware's leased host-display presenter.
// PCController formats authoritative samples; boards only retain the exact cells.
class MediaClock {
public:
  static constexpr uint8_t PayloadBytes = 14;
  bool update(const uint8_t *p, uint8_t length, uint32_t now) {
    if (length != PayloadBytes || p[0] != 3 || p[1] > 3 || p[1] == 2) return false;
    for (uint8_t i = 0; i < 4; ++i) cells_[i] = p[10 + i];
    anchor_ = now;
    flags_ = p[1];
    return true;
  }
  bool active(uint32_t now) const {
    return (flags_ & 1) != 0 && static_cast<uint32_t>(now - anchor_) < 3000;
  }
  void segments(uint32_t now, uint8_t out[4]) const {
    (void)now;
    for (uint8_t i = 0; i < 4; ++i) out[i] = cells_[i];
  }
  const uint8_t *segments() const { return cells_; }
private:
  uint32_t anchor_ = 0;
  uint8_t flags_ = 0, cells_[4] = {};
};
