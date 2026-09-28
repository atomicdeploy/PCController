#include <Arduino.h>
#include <EEPROM.h>

#include "Project/PwmController.h"
#include "Project/PwmExpanderDriver.h"
#include "Project/StatusLedController.h"
#include "Project/StatusLedMath.h"

#include <array>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {

using Descriptor =
    std::array<std::uint8_t, StatusLedController::ProfilePayloadBytes>;

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

Descriptor descriptor(StatusLedEffect effect, std::uint8_t red,
                      std::uint8_t green, std::uint8_t blue,
                      std::uint8_t alternateRed,
                      std::uint8_t alternateGreen,
                      std::uint8_t alternateBlue,
                      std::uint8_t brightness, std::uint8_t minimum,
                      std::uint16_t period, std::uint8_t repeats) {
  return {static_cast<std::uint8_t>(effect), red, green, blue, alternateRed,
          alternateGreen, alternateBlue, brightness, minimum,
          static_cast<std::uint8_t>(period),
          static_cast<std::uint8_t>(period >> 8), repeats};
}

struct Fixture {
  PwmExpanderDriver driver{PwmController::PwmI2cAddress};
  PwmController pwm{driver};
  // Production owns a static singleton, so native fixtures must model its
  // guaranteed zero-initialization instead of inheriting stack garbage.
  StatusLedController leds{};

  explicit Fixture(std::uint8_t brightness = 100) {
    EEPROM.fill(0xFF);
    leds.begin(pwm, brightness, 0, true);
    leds.setMode(StatusLedMode::Ready, 0);
  }
};

void testGoldenMath() {
  constexpr std::uint8_t phases[] = {0, 1, 127, 128, 254, 255};
  for (const std::uint8_t phase : phases) {
    const std::uint8_t up = static_cast<std::uint8_t>(
        7U + (static_cast<std::uint16_t>(241U - 7U) * phase) / 256U);
    const std::uint8_t down = static_cast<std::uint8_t>(
        241U - (static_cast<std::uint16_t>(241U - 7U) * phase) / 256U);
    require(StatusLedMath::interpolate(7, 241, phase) == up,
            "ascending interpolation vector drifted");
    require(StatusLedMath::interpolate(241, 7, phase) == down,
            "descending interpolation vector drifted");
  }
  require(StatusLedMath::scale(255, 255) == 255 &&
              StatusLedMath::scale(255, 100) == 100 &&
              StatusLedMath::scale(0, 255) == 0,
          "AVR /256 brightness scaling drifted");
  require(StatusLedMath::phaseDeadline(1800, 64) == 1800 &&
              StatusLedMath::phaseDeadline(640, 1) == 10 &&
              StatusLedMath::phaseDeadline(60000, 64) == 60000,
          "64-phase deadline distribution changed configured duration");
}

void testStrictDescriptorAndIdempotence() {
  Fixture fixture(180);
  require(!fixture.leds.setEffect(nullptr, 0),
          "null STATUS_EFFECT descriptor was accepted");

  Descriptor value = descriptor(StatusLedEffect::Transition, 0, 0, 0, 255,
                                0, 0, 255, 0, 1280, 0);
  Descriptor invalid = value;
  invalid[9] = 0x7F;
  invalid[10] = 0x02; // 639 ms
  require(!fixture.leds.setEffect(invalid.data(), 0),
          "sub-minimum effect period was accepted");
  invalid = value;
  invalid[9] = 0x61;
  invalid[10] = 0xEA; // 60001 ms
  require(!fixture.leds.setEffect(invalid.data(), 0),
          "period 60001 was accepted");
  invalid[9] = 0xFF;
  invalid[10] = 0xFF;
  require(!fixture.leds.setEffect(invalid.data(), 0),
          "period 65535 was accepted");

  require(fixture.leds.setEffect(value.data(), 0),
          "valid transition descriptor was rejected");
  fixture.leds.service(640);
  const std::uint8_t midpoint = fixture.leds.renderedRed();
  require(midpoint >= 126 && midpoint <= 128,
          "transition midpoint did not use the AVR renderer");
  require(fixture.leds.setEffect(value.data(), 640),
          "identical descriptor was not acknowledged");
  fixture.leds.service(660);
  require(fixture.leds.renderedRed() > midpoint,
          "identical descriptor reset the active phase");
}

void testPriorityRetentionAndRelease() {
  Fixture fixture(100);
  const Descriptor first = descriptor(StatusLedEffect::Breathe, 10, 20, 30,
                                      0, 0, 0, 220, 12, 1280, 0);
  const Descriptor latest = descriptor(StatusLedEffect::Cycle, 1, 2, 3, 200,
                                       100, 50, 180, 0, 1800, 0);
  require(fixture.leds.setEffect(first.data(), 0),
          "manual owner could not claim renderer");
  require(fixture.leds.condition() == StatusLedController::ManualCondition,
          "manual descriptor did not publish manual condition");

  fixture.leds.setMode(StatusLedMode::Warning, 100);
  require(fixture.leds.condition() ==
              static_cast<std::uint8_t>(StatusLedMode::Warning),
          "Warning did not preempt manual owner");
  require(fixture.leds.setEffect(latest.data(), 200),
          "changed descriptor during Warning was rejected");
  require(fixture.leds.condition() ==
              static_cast<std::uint8_t>(StatusLedMode::Warning),
          "changed descriptor stole Warning priority");

  fixture.leds.setMode(StatusLedMode::Learning, 250);
  require(fixture.leds.condition() ==
              static_cast<std::uint8_t>(StatusLedMode::Learning),
          "Learning did not retain priority");
  fixture.leds.setMode(StatusLedMode::Custom, 300);
  require(fixture.leds.condition() == StatusLedController::ManualCondition &&
              fixture.leds.effect() == StatusLedEffect::Cycle,
          "latest retained descriptor did not restore after safety");

  fixture.leds.playCue(StatusLedCue::Menu, 100, 310);
  require(fixture.leds.condition() == StatusLedController::ManualCondition,
          "routine cue stole a manual owner");
  fixture.leds.playCue(StatusLedCue::Reset, 240, 320);
  require(fixture.leds.condition() == 18,
          "Reset cue did not preempt a manual owner");
  require(fixture.leds.setEffect(latest.data(), 330) &&
              fixture.leds.condition() == 18,
          "identical request during Reset did not retain cue priority");
  fixture.leds.service(559);
  require(fixture.leds.condition() == 18,
          "Reset cue ended before its watchdog interval");
  fixture.leds.service(560);
  require(fixture.leds.condition() == StatusLedController::ManualCondition &&
              fixture.leds.effect() == StatusLedEffect::Cycle,
          "Reset cue expiry did not restore retained owner");

  const std::uint8_t retainedRed = fixture.leds.renderedRed();
  fixture.leds.cancelEffect();
  require(fixture.leds.effect() == StatusLedEffect::None &&
              fixture.leds.renderedRed() == retainedRed,
          "release did not retain the terminal physical frame");
  fixture.leds.setMode(StatusLedMode::Ready, 600);
  fixture.leds.setMode(StatusLedMode::Custom, 601);
  require(fixture.leds.condition() ==
              static_cast<std::uint8_t>(StatusLedMode::Custom),
          "released owner was resurrected");
}

void testExactDurationAndWrap() {
  constexpr std::uint16_t periods[] = {640, 1280, 1800, 3200, 60000};
  for (const std::uint16_t period : periods) {
    Fixture fixture(255);
    const Descriptor finite = descriptor(StatusLedEffect::Transition, 0, 0, 0,
                                         255, 64, 32, 255, 0, period, 1);
    require(fixture.leds.setEffect(finite.data(), 10),
            "finite descriptor was rejected");
    fixture.leds.service(static_cast<std::uint16_t>(10U + period - 1U));
    require(fixture.leds.effect() == StatusLedEffect::Transition,
            "finite effect completed before configured duration");
    fixture.leds.service(static_cast<std::uint16_t>(10U + period));
    require(fixture.leds.effect() == StatusLedEffect::None &&
                fixture.leds.renderedRed() == 255 &&
                fixture.leds.renderedGreen() == 64 &&
                fixture.leds.renderedBlue() == 32,
            "finite effect did not complete at its exact endpoint");
    require(fixture.leds.setEffect(
                finite.data(), static_cast<std::uint16_t>(100000U)),
            "completed descriptor could not restart");
    require(fixture.leds.effect() == StatusLedEffect::Transition &&
                fixture.leds.renderedRed() == 0,
            "reissued finite descriptor did not restart at phase zero");
  }

  constexpr StatusLedEffect effects[] = {
      StatusLedEffect::Breathe, StatusLedEffect::Flash,
      StatusLedEffect::Cycle, StatusLedEffect::Transition};
  for (const StatusLedEffect effect : effects) {
    Fixture fixture(255);
    const std::uint8_t primaryColor =
        effect == StatusLedEffect::Breathe || effect == StatusLedEffect::Flash
            ? 255
            : 0;
    const std::uint8_t alternateColor =
        effect == StatusLedEffect::Breathe ||
                effect == StatusLedEffect::Flash
            ? 0
            : 255;
    const Descriptor looping =
        descriptor(effect, primaryColor, 0, 0, alternateColor, 0, 0, 255, 0,
                   1800, 0);
    require(fixture.leds.setEffect(looping.data(), 0),
            "looping descriptor was rejected");
    fixture.leds.service(1799);
    if (effect == StatusLedEffect::Transition) {
      require(fixture.leds.renderedRed() > 240,
              "transition period-1 missed its terminal phase");
    } else if (effect != StatusLedEffect::Flash) {
      require(fixture.leds.renderedRed() > 0,
              "animated period-1 frame collapsed to phase zero");
    }
    fixture.leds.service(1800);
    const std::uint8_t phaseZero =
        effect == StatusLedEffect::Breathe ? 0 : primaryColor;
    require(fixture.leds.renderedRed() == phaseZero,
            "exact-period wrap held the previous endpoint");
    fixture.leds.service(1828);
    if (effect == StatusLedEffect::Flash) {
      require(fixture.leds.renderedRed() == primaryColor,
              "first flash hold deadline changed color");
    } else {
      require(fixture.leds.renderedRed() != phaseZero,
              "first post-wrap deadline did not advance");
    }
  }

  Fixture delayed(255);
  const Descriptor looping = descriptor(StatusLedEffect::Transition, 0, 0, 0,
                                        255, 0, 0, 255, 0, 1800, 0);
  require(delayed.leds.setEffect(looping.data(), 0),
          "delayed-tick descriptor was rejected");
  delayed.leds.service(6300); // Three full cycles plus half a cycle.
  require(delayed.leds.renderedRed() >= 126 &&
              delayed.leds.renderedRed() <= 128,
          "delayed multi-cycle tick lost the latest physical phase: red=" +
              std::to_string(delayed.leds.renderedRed()));

  Fixture wrapped(255);
  const Descriptor wrappedFinite = descriptor(
      StatusLedEffect::Transition, 0, 0, 0, 255, 0, 0, 255, 0, 1280, 1);
  constexpr std::uint16_t wrapStart = 65000;
  require(wrapped.leds.setEffect(wrappedFinite.data(), wrapStart),
          "wrap descriptor was rejected");
  wrapped.leds.service(static_cast<std::uint16_t>(wrapStart + 1279U));
  require(wrapped.leds.effect() == StatusLedEffect::Transition,
          "modulo clock completed before the wrapped deadline");
  wrapped.leds.service(static_cast<std::uint16_t>(wrapStart + 1280U));
  require(wrapped.leds.effect() == StatusLedEffect::None &&
              wrapped.leds.renderedRed() == 255,
          "modulo clock missed the exact wrapped endpoint");
}

void testFallbackBrightnessIsStable() {
  Fixture fixture(100);
  fixture.leds.setCustom(255, 0, 0, 220, 0);
  require(fixture.leds.renderedRed() == 220,
          "manual brightness was not applied");
  fixture.leds.setMode(StatusLedMode::Warning, 10);
  require(fixture.leds.renderedRed() == 100,
          "blank Warning fallback inherited manual brightness");
  fixture.leds.setMode(StatusLedMode::Custom, 20);
  require(fixture.leds.renderedRed() == 220,
          "manual brightness was not restored after safety");
}

} // namespace

int main() {
  try {
    testGoldenMath();
    testStrictDescriptorAndIdempotence();
    testPriorityRetentionAndRelease();
    testExactDurationAndWrap();
    testFallbackBrightnessIsStable();
    std::cout << "status_led_controller_test: ok\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "status_led_controller_test: " << error.what() << '\n';
    return 1;
  }
}
