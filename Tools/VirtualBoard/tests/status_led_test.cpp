#include <Arduino.h>

#include "Project/PwmController.h"
#include "Project/PwmExpanderDriver.h"
#include "Project/StatusLedController.h"

#include <algorithm>
#include <cstdint>
#include <cstring>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {
std::uint32_t pwmWrites;

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

std::uint16_t from8(std::uint8_t value) {
  return static_cast<std::uint16_t>(value * 16U + value / 16U);
}

struct Fixture {
  PwmExpanderDriver driver{PwmController::PwmI2cAddress};
  PwmController pwm{driver};
  StatusLedController led{};

  explicit Fixture(std::uint8_t localBrightness = 200) {
    pwm.begin(true, 0);
    led.begin(pwm, localBrightness, 0, true);
    pwmWrites = 0;
  }
};

void testCustomBrightnessAndCadence() {
  Fixture fixture;
  const std::uint8_t custom[] = {255, 0, 0, 64};
  const auto beforeFrames = fixture.led.renderedFrames();
  fixture.led.setCustom(custom, 0);
  require(fixture.led.renderedFrames() == beforeFrames && pwmWrites == 0,
          "STATUS_RGB wrote PCA output before the service gate");
  fixture.led.service(0);
  require(fixture.pwm.logicalValue(PwmChannels::StatusRed) == from8(64) &&
              fixture.pwm.logicalValue(PwmChannels::StatusGreen) == 0 &&
              fixture.pwm.logicalValue(PwmChannels::StatusBlue) == 0,
          "STATUS_RGB did not preserve its fourth-byte brightness");

  const std::uint8_t breathe[] = {
      1, 255, 0, 0, 0, 0, 0, 255, 0, 0x40, 0x06, 0}; // 1600 ms
  fixture.led.setEffect(breathe, 0);
  const auto start = fixture.led.renderedFrames();
  for (std::uint32_t now = 0; now <= 1000; ++now) {
    const auto prior = fixture.led.renderedFrames();
    fixture.led.service(now);
    require(fixture.led.renderedFrames() - prior <= 1,
            "one service call rendered more than one frame");
  }
  const auto frames = fixture.led.renderedFrames() - start;
  require(frames >= 60 && frames <= 64,
          "native status renderer missed its >=50 FPS / target-60 cadence");
}

void testLeasePauseAndRollover() {
  Fixture fixture;
  const std::uint8_t effect[] = {
      1, 255, 0, 0, 0, 0, 0, 255, 0, 0x40, 0x06, 0};
  fixture.led.setEffect(effect, 0);
  fixture.led.service(0);
  fixture.led.service(16);
  const auto beforeResume = fixture.led.renderedFrames();
  fixture.led.service(316); // service was withheld during a 300 ms I2C lease
  require(fixture.led.renderedFrames() == beforeResume + 1,
          "lease resume did not render exactly one frame");
  for (std::uint32_t now = 317; now < 332; ++now) {
    fixture.led.service(now);
  }
  require(fixture.led.renderedFrames() == beforeResume + 1,
          "lease resume caused a loop-speed catch-up burst");
  fixture.led.service(332);
  require(fixture.led.renderedFrames() == beforeResume + 2,
          "lease resume did not restore the 16 ms deadline");

  fixture.led.setEffect(effect, 0xFFFFFFF0UL);
  fixture.led.service(0xFFFFFFF0UL);
  const auto beforeWrap = fixture.led.renderedFrames();
  fixture.led.service(0);
  require(fixture.led.renderedFrames() == beforeWrap + 1,
          "frame deadline failed across millis rollover");
}

void runFinite(StatusLedController &led, std::uint32_t periodMs) {
  led.service(0);
  for (std::uint32_t now = 16; now <= periodMs; now += 16) {
    led.service(now);
  }
}

void testPeriodsFiniteCompletionAndOwnership() {
  {
    Fixture fixture;
    const std::uint8_t transition[] = {
        4, 255, 0, 0, 0, 255, 0, 180, 0, 0x80, 0x02, 1}; // 640 ms
    fixture.led.setEffect(transition, 0);
    runFinite(fixture.led, 640);
    require(fixture.led.effect() == StatusLedEffect::None &&
                fixture.pwm.logicalValue(PwmChannels::StatusGreen) ==
                    from8(180),
            "640 ms finite transition did not finish at its static endpoint");
    fixture.led.setMode(StatusLedMode::Fault, 656);
    fixture.led.service(656);
    fixture.led.setMode(StatusLedMode::Custom, 672);
    fixture.led.service(672);
    require(fixture.led.effect() == StatusLedEffect::None &&
                fixture.pwm.logicalValue(PwmChannels::StatusGreen) ==
                    from8(180),
            "completed finite effect resurrected after a safety overlay");
  }

  {
    Fixture fixture;
    const std::uint8_t effect[] = {
        3, 21, 42, 84, 90, 60, 30, 170, 0, 0x40, 0x06, 0};
    fixture.led.setEffect(effect, 0);
    fixture.led.service(0);
    const auto writesBeforeFault = pwmWrites;
    fixture.led.setMode(StatusLedMode::Fault, 16);
    require(pwmWrites == writesBeforeFault,
            "setMode touched PCA output outside service");
    fixture.led.service(16);
    fixture.led.setMode(StatusLedMode::Custom, 32);
    fixture.led.service(32);
    require(std::memcmp(fixture.led.descriptorForTest(), effect,
                        sizeof(effect)) == 0,
            "fault clear did not restore the exact retained host descriptor");
  }

  for (const std::uint32_t period : {1600UL, 60000UL}) {
    Fixture fixture;
    std::uint8_t effect[] = {
        1, 255, 0, 0, 0, 0, 0, 255, 0,
        static_cast<std::uint8_t>(period),
        static_cast<std::uint8_t>(period >> 8), 1};
    fixture.led.setEffect(effect, 0);
    runFinite(fixture.led, period);
    require(fixture.led.effect() == StatusLedEffect::None,
            "descriptor period did not complete on MCU time");
  }
}

void testLocalBrightnessAndContinuity() {
  Fixture fixture(160);
  const std::uint8_t darkHost[] = {255, 255, 255, 0};
  fixture.led.setCustom(darkHost, 0);
  fixture.led.service(0);
  fixture.led.setMode(StatusLedMode::Fault, 16);
  require(fixture.led.brightness() == 160,
          "host brightness leaked into autonomous fault ownership");
  fixture.led.service(16);
  require(fixture.pwm.logicalValue(PwmChannels::StatusRed) > 0,
          "host brightness zero made the local fault invisible");
  fixture.led.setBrightness(8);
  fixture.led.service(32);
  require(fixture.led.localMinimumForTest() <= fixture.led.brightness() &&
              fixture.pwm.logicalValue(PwmChannels::StatusRed) <= from8(8),
          "lowered local brightness underflowed its breathe minimum");

  const std::uint8_t breathe[] = {
      1, 255, 0, 0, 0, 0, 0, 255, 0, 0x40, 0x06, 0};
  fixture.led.setEffect(breathe, 100);
  fixture.led.service(100);
  std::uint16_t previous = fixture.pwm.logicalValue(PwmChannels::StatusRed);
  std::uint16_t maximumDelta = 0;
  for (std::uint32_t now = 116; now <= 1700; now += 16) {
    fixture.led.service(now);
    const auto current = fixture.pwm.logicalValue(PwmChannels::StatusRed);
    maximumDelta = std::max<std::uint16_t>(
        maximumDelta, previous > current ? previous - current
                                         : current - previous);
    previous = current;
  }
  require(maximumDelta <= from8(16),
          "breathe easing has a visible adjacent-frame discontinuity");
}
} // namespace

bool PwmExpanderDriver::begin() { return true; }
bool PwmExpanderDriver::setFrequency(std::uint16_t) { return true; }
std::uint8_t PwmExpanderDriver::setPWM(std::uint8_t, std::uint16_t,
                                      std::uint16_t) {
  ++pwmWrites;
  return 0;
}
std::uint8_t PwmExpanderDriver::setAllPWM(std::uint16_t, std::uint16_t) {
  return 0;
}

int main() {
  try {
    testCustomBrightnessAndCadence();
    testLeasePauseAndRollover();
    testPeriodsFiniteCompletionAndOwnership();
    testLocalBrightnessAndContinuity();
    std::cout << "firmware_status_led_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_status_led_tests: " << error.what() << '\n';
    return 1;
  }
}
