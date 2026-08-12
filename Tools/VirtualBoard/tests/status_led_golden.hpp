#pragma once

#include <array>
#include <cstdint>

namespace status_led_golden {

// A deliberately asymmetric fixture catches /255 versus AVR /256 drift in
// both color interpolation and brightness scaling.
constexpr std::array<std::uint8_t, 12> descriptor(std::uint8_t effect,
                                                   std::uint8_t repeats = 0) {
  return {{effect, 240, 80, 20, 20, 160, 240, 200, 20,
           0x80, 0x02, repeats}}; // 640 ms, 64 rendered phases.
}

struct Frame {
  std::uint8_t phase;
  std::array<std::uint8_t, 3> breathe;
  std::array<std::uint8_t, 3> cycle;
  std::array<std::uint8_t, 3> transition;
  std::array<std::uint8_t, 3> flash;
};

// These are fixed outputs from the AVR formula, not values calculated by the
// test. Sharing them makes the production mock and VirtualBoard prove the same
// phase/color contract independently.
constexpr std::array<Frame, 5> frames{{
    {0, {{19, 6, 1}}, {{188, 62, 15}}, {{188, 62, 15}}, {{188, 62, 15}}},
    {64, {{104, 34, 8}}, {{102, 94, 102}}, {{145, 78, 58}},
     {{188, 62, 15}}},
    {128, {{187, 62, 15}}, {{17, 124, 186}}, {{102, 94, 102}},
     {{15, 125, 188}}},
    {192, {{103, 34, 8}}, {{103, 93, 100}}, {{58, 109, 145}},
     {{15, 125, 188}}},
    {252, {{23, 7, 1}}, {{184, 63, 19}}, {{18, 124, 185}},
     {{15, 125, 188}}},
}};

constexpr std::array<std::uint8_t, 3> transitionEndpoint{{15, 125, 188}};
constexpr std::array<std::uint8_t, 3> breatheFirstStep{{25, 8, 2}};
constexpr std::array<std::uint8_t, 3> cycleFirstStep{{183, 64, 20}};
constexpr std::array<std::uint8_t, 3> transitionFirstStep{{186, 63, 18}};
constexpr std::array<std::uint8_t, 3> flashFirstStep{{188, 62, 15}};
constexpr std::array<std::uint8_t, 5> directDescendingRed{{240, 185, 130, 75,
                                                           24}};
constexpr std::array<std::uint8_t, 6> interpolationPhases{{0, 1, 127, 128,
                                                           254, 255}};
constexpr std::array<std::uint8_t, 6> descending{{240, 240, 131, 130, 22, 21}};
constexpr std::array<std::uint8_t, 6> ascending{{20, 20, 129, 130, 238, 239}};

} // namespace status_led_golden
