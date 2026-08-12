#include <Arduino.h>
#include <avr/interrupt.h>

#include "LocalLib/DallasTemperatureBus.h"
#include "LocalLib/Keys.h"
#include "LocalLib/SevenSegments.h"
#include "LocalLib/ShiftRegisters.h"
#include "LocalLib/TonePlayer.h"
#include "Project/FrontPanelModel.h"
#include "Project/MotionDoorPolicy.h"
#include "Project/PwmController.h"
#include "Project/RelayController.h"
#include "Project/SettingsStore.h"
#include "Project/StatusLedController.h"
#include "Project/StatusLedMath.h"
#include "Project/TemperatureRoles.h"
#include "Project/TransitionMath.h"
#include "Project/UartProtocol.h"
#include "status_led_golden.hpp"

#include <algorithm>
#include <array>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

struct KeyTrace {
  std::vector<KeyEvent> events;
};

void gestured(std::uint8_t, KeyEvent event, void *context) {
  static_cast<KeyTrace *>(context)->events.push_back(event);
}

void bind(Key &key, KeyTrace &trace) {
  key.setEventCallback(gestured, &trace);
}

void testKeyGestures() {
  shiftRegisters.clearVirtualInputs();

  require(KEY_DEBOUNCE_MS <= 25,
          "front-key debounce exceeds the immediate-response budget");
  require(keyEventRunsPrimaryAction(KeyEvent::Down) &&
              keyEventRunsPrimaryAction(KeyEvent::HoldRepeat) &&
              !keyEventRunsPrimaryAction(KeyEvent::Click) &&
              !keyEventRunsPrimaryAction(KeyEvent::HoldStart),
          "primary actions must run on Down/repeat, never deferred Click");

  {
    Key key(0);
    KeyTrace trace;
    bind(key, trace);
    key.update(0);
    shiftRegisters.setVirtualInput(0, true);
    key.update(1);
    key.update(1 + KEY_DEBOUNCE_MS - 1);
    require(trace.events.empty(), "key acted before debounce completed");
    key.update(1 + KEY_DEBOUNCE_MS);
    require(trace.events == std::vector<KeyEvent>({KeyEvent::Down}),
            "key Down was not emitted at the debounce deadline");
    shiftRegisters.setVirtualInput(0, false);
    key.update(100);
    key.update(100 + KEY_DEBOUNCE_MS);
    key.update(100 + KEY_DEBOUNCE_MS + KEY_DOUBLE_CLICK_MS + 1);
    require(trace.events == std::vector<KeyEvent>({KeyEvent::Down,
                                                   KeyEvent::Up,
                                                   KeyEvent::Click}),
            "single-click event order drifted");
  }

  {
    Key key(1);
    KeyTrace trace;
    bind(key, trace);
    key.update(1000);
    shiftRegisters.setVirtualInput(1, true);
    key.update(1001);
    key.update(1051);
    shiftRegisters.setVirtualInput(1, false);
    key.update(1100);
    key.update(1150);
    shiftRegisters.setVirtualInput(1, true);
    key.update(1250);
    key.update(1300);
    shiftRegisters.setVirtualInput(1, false);
    key.update(1350);
    key.update(1400);
    require(trace.events.back() == KeyEvent::DoubleClick &&
                std::find(trace.events.begin(), trace.events.end(),
                          KeyEvent::Click) == trace.events.end(),
            "double-click also leaked a single-click event");
  }

  {
    Key key(2);
    KeyTrace trace;
    bind(key, trace);
    key.update(2000);
    shiftRegisters.setVirtualInput(2, true);
    key.update(2001);
    key.update(2051);
    key.update(2651);
    key.update(2801);
    key.update(3801);
    key.update(3861);
    shiftRegisters.setVirtualInput(2, false);
    key.update(3900);
    key.update(3950);
    require(std::count(trace.events.begin(), trace.events.end(),
                       KeyEvent::HoldStart) == 1 &&
                std::count(trace.events.begin(), trace.events.end(),
                           KeyEvent::HoldRepeat) == 3 &&
                trace.events.back() == KeyEvent::HoldRelease,
            "hold start/repeat/fast-repeat/release sequence drifted");
    require(std::find(trace.events.begin(), trace.events.end(),
                      KeyEvent::Click) == trace.events.end(),
            "held key emitted a release click");
  }

  {
    Key key(3);
    KeyTrace trace;
    bind(key, trace);
    key.update(65520UL);
    shiftRegisters.setVirtualInput(3, true);
    key.update(65530UL);
    key.update(65580UL);
    require(trace.events.size() == 1 && trace.events.front() == KeyEvent::Down,
            "16-bit key debounce failed across millis rollover");
  }

  shiftRegisters.clearVirtualInputs();
}

void testRelayInterlocks() {
  ShiftRegisters registers;
  RelayController controller(registers);
  controller.begin(0);
  controller.setBreakBeforeDirectionMs(1);
  require(controller.activeRelayMask() == 0, "relay begin was not all-off");

  require(controller.requestSide(RelaySide::A, RelayDirection::Forward, true,
                                 1),
          "Side A forward request was rejected");
  require(controller.activeRelayMask() == _BV(1),
          "R2 must be Side A output/enable");

  require(controller.requestSide(RelaySide::A, RelayDirection::Reverse, true,
                                 10),
          "Side A reversal was rejected");
  require((controller.activeRelayMask() & (_BV(0) | _BV(1))) == 0,
          "Side A enable did not break before direction");
  controller.service(11);
  require((controller.activeRelayMask() & (_BV(0) | _BV(1))) ==
              (_BV(0) | _BV(1)),
          "Side A did not enable immediately after its configured break");

  controller.setRetainDirectionOnStop(true);
  controller.stopSide(RelaySide::A, 20);
  require((controller.activeRelayMask() & (_BV(0) | _BV(1))) == _BV(0),
          "output-only stop did not retain the Side A direction relay");
  controller.setRetainDirectionOnStop(false);
  controller.stopSide(RelaySide::A, 21);
  require((controller.activeRelayMask() & (_BV(0) | _BV(1))) == 0,
          "full-off stop did not clear the Side A direction relay");
  require(controller.requestSide(RelaySide::A, RelayDirection::Reverse, true,
                                 22),
          "Side A could not restart after a full-off stop");
  controller.service(26);

  require(controller.requestSide(RelaySide::B, RelayDirection::Forward, true,
                                 70),
          "Side B request was rejected");
  require((controller.activeRelayMask() & _BV(3)) != 0 &&
              (controller.activeRelayMask() & (_BV(0) | _BV(1))) ==
                  (_BV(0) | _BV(1)),
          "Side B changed Side A state (cross-side isolation failure)");

  controller.requestSide(RelaySide::A, RelayDirection::Forward, true, 100);
  controller.requestSide(RelaySide::B, RelayDirection::Reverse, true, 100);
  controller.service(101);
  require((controller.activeRelayMask() & _BV(0)) == 0 &&
              (controller.activeRelayMask() & _BV(2)) == 0,
          "two direction relays changed in the same service window");
  controller.service(102);
  require((controller.activeRelayMask() & _BV(2)) == 0,
          "cross-side direction interlock ended before 5 ms");
  controller.service(106);
  require((controller.activeRelayMask() & _BV(2)) != 0,
          "R3 did not change after the cross-side interlock");

  controller.setMotionAllowed(false, 120);
  require((controller.activeRelayMask() & (_BV(1) | _BV(3))) == 0,
          "revoked door policy did not immediately drop both outputs");
  require(!controller.requestSide(RelaySide::A, RelayDirection::Forward, true,
                                  121) &&
              !controller.requestRelayForTest(4, true, 121),
          "motion enable bypassed the common denied policy");
  require(controller.setGeneral(0, true) &&
              (controller.activeRelayMask() & _BV(4)) != 0,
          "door motion policy incorrectly blocked general R5");

  controller.allOff(130);
  require(controller.activeRelayMask() == 0,
          "safe reset relay primitive did not leave every output off");

  ShiftRegisters preciseRegisters;
  RelayController preciseController(preciseRegisters);
  preciseController.begin(0);
  preciseController.setBreakBeforeDirectionMs(37);
  require(preciseController.requestSide(
              RelaySide::A, RelayDirection::Forward, true, 1),
          "exact-break fixture could not start Side A");
  require(preciseController.requestSide(
              RelaySide::A, RelayDirection::Reverse, true, 2),
          "exact-break fixture rejected reversal");
  preciseController.service(38);
  require((preciseController.activeRelayMask() & (_BV(0) | _BV(1))) == 0,
          "configured 37 ms break ended one millisecond early");
  preciseController.service(39);
  require((preciseController.activeRelayMask() & (_BV(0) | _BV(1))) ==
              (_BV(0) | _BV(1)),
          "configured 37 ms break did not apply exactly");
}

void testMotionDoorPolicyMatrixAndEntryPaths() {
  struct PolicyCase {
    MotionDoorPolicy policy;
    bool doorOpen;
    bool allowed;
  };
  const PolicyCase cases[] = {
      {MotionDoorPolicy::Always, false, true},
      {MotionDoorPolicy::Always, true, true},
      {MotionDoorPolicy::ClosedOnly, false, true},
      {MotionDoorPolicy::ClosedOnly, true, false},
      {MotionDoorPolicy::OpenOnly, false, false},
      {MotionDoorPolicy::OpenOnly, true, true},
      {MotionDoorPolicy::Never, false, false},
      {MotionDoorPolicy::Never, true, false},
  };

  enum class EntryPath : std::uint8_t {
    PhysicalMotionMenu,
    RadioSideMapping,
    RadioRelayMapping,
    HostSideCommand,
    HostRelayTest,
    BufferedMacroCommand,
  };
  const EntryPath paths[] = {
      EntryPath::PhysicalMotionMenu, EntryPath::RadioSideMapping,
      EntryPath::RadioRelayMapping,  EntryPath::HostSideCommand,
      EntryPath::HostRelayTest,      EntryPath::BufferedMacroCommand,
  };

  for (const PolicyCase &policyCase : cases) {
    require(motionDoorPolicyAllows(policyCase.policy,
                                   policyCase.doorOpen) ==
                policyCase.allowed,
            "four-mode motion-door policy matrix drifted");

    for (EntryPath path : paths) {
      ShiftRegisters registers;
      RelayController controller(registers);
      controller.begin(0);
      controller.setBreakBeforeDirectionMs(1);
      controller.setMotionAllowed(policyCase.allowed, 1);

      bool accepted = false;
      switch (path) {
        // These deliberately mirror the exact two RelayController APIs used by
        // the physical menu, RF dispatcher, UART dispatcher, and macro replay.
        case EntryPath::PhysicalMotionMenu:
        case EntryPath::RadioSideMapping:
        case EntryPath::HostSideCommand:
          accepted = controller.requestSide(
              RelaySide::A, RelayDirection::Forward, true, 2);
          break;
        case EntryPath::RadioRelayMapping:
        case EntryPath::HostRelayTest:
        case EntryPath::BufferedMacroCommand:
          accepted = controller.requestRelayForTest(2, true, 2);
          break;
      }
      require(accepted == policyCase.allowed,
              "a firmware motion entry path bypassed the common door policy");
      require(((controller.activeRelayMask() & _BV(1)) != 0) ==
                  policyCase.allowed,
              "motion enable state disagreed with its policy decision");
      controller.stopSide(RelaySide::A, 3);
      require((controller.activeRelayMask() & _BV(1)) == 0,
              "stop was blocked by a denied motion-door policy");
      require(controller.setGeneral(0, true),
              "motion-door policy leaked into general relay control");
    }
  }

  ShiftRegisters retainedRegisters;
  RelayController retained(retainedRegisters);
  retained.begin(0);
  retained.setBreakBeforeDirectionMs(1);
  require(retained.requestSide(
              RelaySide::A, RelayDirection::Reverse, true, 1),
          "policy-revocation stop fixture could not start motion");
  retained.setRetainDirectionOnStop(true);
  retained.setMotionAllowed(false, 2);
  require((retained.activeRelayMask() & (_BV(0) | _BV(1))) == _BV(0),
          "policy revocation did not preserve output-only stop semantics");
  retained.setRetainDirectionOnStop(false);
  retained.stopSide(RelaySide::A, 3);
  retained.service(6);
  require((retained.activeRelayMask() & (_BV(0) | _BV(1))) == 0,
          "full-off stop could not clear direction after policy revocation");

  ControllerSettings editor{};
  editor.flags = SettingsFlags::Silent | SettingsFlags::DoorAudioDisabled;
  editor.adjustMotionDoorPolicy(true);
  require(editor.motionDoorPolicy() == MotionDoorPolicy::ClosedOnly,
          "policy editor skipped Closed Only");
  editor.adjustMotionDoorPolicy(true);
  require(editor.motionDoorPolicy() == MotionDoorPolicy::OpenOnly,
          "policy editor skipped Open Only");
  editor.adjustMotionDoorPolicy(true);
  require(editor.motionDoorPolicy() == MotionDoorPolicy::Never,
          "policy editor skipped Never");
  editor.adjustMotionDoorPolicy(true);
  require(editor.motionDoorPolicy() == MotionDoorPolicy::Always,
          "policy editor did not wrap forward to Always");
  editor.adjustMotionDoorPolicy(false);
  require(editor.motionDoorPolicy() == MotionDoorPolicy::Never,
          "policy editor did not wrap backward to Never");
  require((editor.flags & ~SettingsFlags::MotionDoorPolicyMask) ==
              (SettingsFlags::Silent | SettingsFlags::DoorAudioDisabled),
          "policy editor corrupted adjacent settings flags");

}

void testTransitionsAndRollover() {
  using namespace TransitionMath;
  require(rollByte(240, 16, true) == 255 &&
              rollByte(255, 16, true) == 0 &&
              rollByte(16, 16, false) == 0 &&
              rollByte(0, 16, false) == 255,
          "front-panel byte rollover skipped or trapped an endpoint");

  std::uint8_t value = 128;
  std::uint8_t previous = value;
  unsigned frames = 0;
  while (value != 0 && frames++ < 256) {
    value = easedByte(value, 0);
    require(value < previous, "illumination off fade stalled or reversed");
    previous = value;
  }
  require(value == 0 && frames > 16,
          "illumination ease did not converge smoothly to configured off");

  value = 32;
  previous = value;
  frames = 0;
  while (value != 200 && frames++ < 256) {
    value = easedByte(value, 200);
    require(value > previous && value <= 200,
            "illumination on fade overshot or reversed");
    previous = value;
  }
  require(value == 200, "illumination ease did not reach configured on");

  std::uint16_t channel = 0;
  for (unsigned frame = 0; frame < 64 && channel != 4095; ++frame) {
    const auto next = easedChannel(channel, 4095);
    require(next > channel && next <= 4095,
            "RGB informational transition was not monotonic");
    channel = next;
  }
  require(channel > 4000,
          "RGB damping did not approach its target at display cadence");

  require(smoothSample(1000, 1001, 2, 1) == 1000 &&
              smoothSample(1000, 1008, 2, 1) == 1002,
          "voltage EMA/deadband no longer filters small input jitter");
  require(smoothSample(0, 2, 3, 2) == 0 &&
              smoothSample(0, 80, 3, 2) == 10 &&
              smoothSample(0, -80, 3, 2) == -10,
          "current/power 1/8 EMA is not symmetric and noise-stable");
}

void testDisplayBrightnessFade() {
  SevenSegments segments;
  segments.begin(5);
  segments.serviceBrightness(0, 69);
  require(segments.brightness() == 5,
          "TM1637 brightness moved before its quiet fade interval");
  for (std::uint32_t tick = 70; tick <= 350; tick += 70) {
    segments.serviceBrightness(0, tick);
  }
  require(segments.brightness() == 0,
          "door-close TM1637 fade did not reach display-off");
  for (std::uint32_t tick = 420; tick <= 910; tick += 70) {
    segments.serviceBrightness(9, tick);
  }
  require(segments.brightness() == 7,
          "door-open TM1637 fade did not clamp/reach full brightness");
}

void testStatusEffectOwnershipAndIdempotence() {
  // The native mock cannot acknowledge I2C, but StatusLedController records
  // the exact composited RGB values even when the physical driver is absent.
  // That lets this test exercise the AVR animation engine without hardware.
  PwmExpanderDriver driver(PwmController::PwmI2cAddress);
  PwmController pwm(driver);
  pwm.begin(true, 0);
  StatusLedController led{};
  led.begin(pwm, 255, 0);
  const std::uint8_t bootFirst[] =
      {1, 255, 0, 0, 0, 0, 0, 180, 20, 0x00, 0x05, 0};
  const std::uint8_t bootLatest[] =
      {1, 0, 0, 255, 0, 0, 0, 210, 25, 0x00, 0x05, 0};
  require(led.setEffect(bootFirst, 1) && led.setEffect(bootLatest, 2) &&
              led.condition() == static_cast<std::uint8_t>(StatusLedMode::Boot),
          "manual descriptor stole AVR Boot priority");
  led.playCue(StatusLedCue::Menu, 100, 3);
  require(led.condition() == static_cast<std::uint8_t>(StatusLedMode::Boot),
          "informational cue obscured AVR Boot priority");
  led.setMode(StatusLedMode::Custom, 650);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.renderedBlue() > 20 && led.renderedRed() == 0,
          "AVR Boot exit did not restore the latest retained request");
  led.cancelEffect();
  led.setMode(StatusLedMode::Ready, 0);
  const std::uint8_t breathe[] =
      {1, 0, 0, 255, 0, 0, 0, 255, 0, 0x80, 0x02, 0};
  require(led.setEffect(breathe, 0) &&
              led.mode() == StatusLedMode::Custom,
          "STATUS_EFFECT did not take board compositor ownership");

  for (std::uint32_t tick = 10; tick <= 320; tick += 10) {
    led.service(tick);
  }
  const std::uint8_t peak = led.renderedBlue();
  require(peak > 240,
          "board-native breathe did not reach the high point before refresh");

  require(led.setEffect(breathe, 320) &&
              led.renderedBlue() == peak,
          "identical STATUS_EFFECT refresh restarted the breathe phase");
  led.service(330);
  require(led.renderedBlue() < peak && led.renderedBlue() > 0,
          "board-native breathe did not begin its single smooth fall");

  for (std::uint32_t tick = 340; tick <= 640; tick += 10) {
    led.service(tick);
  }
  require(led.renderedBlue() < 20,
          "board-native breathe did not complete one fall without reset");

  const std::uint8_t changed[] =
      {1, 255, 0, 0, 0, 0, 0, 200, 40, 0x00, 0x05, 0};
  require(led.setEffect(changed, 640) && led.renderedRed() > 30 &&
              led.renderedBlue() == 0,
          "same-kind changed descriptor was not atomically applied");
  led.setMode(StatusLedMode::Learning, 650);
  const std::uint8_t learningLatest[] =
      {1, 0, 255, 0, 0, 0, 0, 205, 30, 0x00, 0x05, 0};
  require(led.setEffect(learningLatest, 660) &&
              led.condition() ==
                  static_cast<std::uint8_t>(StatusLedMode::Learning),
          "changed request stole AVR Learning priority");
  led.playCue(StatusLedCue::Radio, 100, 665);
  require(led.condition() ==
              static_cast<std::uint8_t>(StatusLedMode::Learning),
          "informational cue obscured AVR Learning priority");
  led.setMode(StatusLedMode::Custom, 670);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.renderedGreen() > 20 && led.renderedRed() == 0,
          "AVR Learning exit did not restore the latest retained request");
  require(led.setEffect(changed, 680),
          "post-Learning descriptor replacement was rejected");
  led.service(800);
  const std::uint8_t beforeCue = led.renderedRed();
  led.playCue(StatusLedCue::Menu, 40, 800);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.renderedRed() == beforeCue,
          "informational cue interrupted a board-owned effect");
  led.service(841);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.effect() == StatusLedEffect::Breathe,
          "suppressed cue changed the requested descriptor");

  led.setMode(StatusLedMode::Fault, 850);
  require(led.condition() == static_cast<std::uint8_t>(StatusLedMode::Fault),
          "safety state did not preempt the requested descriptor");
  led.setMode(StatusLedMode::Custom, 900);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.effect() == StatusLedEffect::Breathe,
          "safety release did not restore the requested descriptor");
  const std::uint8_t beforeCancel = led.renderedRed();
  led.cancelEffect();
  require(led.renderedRed() == beforeCancel,
          "ownership handoff flashed the Custom fallback frame");

  led.setMode(StatusLedMode::Warning, 1000);
  const std::uint8_t warningRed = led.renderedRed();
  const std::uint8_t replacement[] =
      {1, 0, 0, 255, 0, 0, 0, 210, 45, 0x00, 0x05, 0};
  require(led.setEffect(replacement, 1010),
          "changed request was rejected during Warning");
  led.playCue(StatusLedCue::Menu, 100, 1010);
  require(led.condition() == static_cast<std::uint8_t>(StatusLedMode::Warning) &&
              led.renderedRed() == warningRed && led.renderedBlue() == 0,
          "manual request or cue obscured Warning priority");
  require(led.setEffect(replacement, 1020) &&
              led.renderedRed() == warningRed,
          "identical request changed a preempted safety frame");
  led.setMode(StatusLedMode::Custom, 1030);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.effect() == StatusLedEffect::Breathe &&
              led.renderedBlue() > 20 && led.renderedRed() == 0,
          "Warning release did not restore the retained changed descriptor");

  led.setMode(StatusLedMode::Fault, 1040);
  const std::uint8_t faultRed = led.renderedRed();
  led.cancelEffect();
  require(led.condition() == static_cast<std::uint8_t>(StatusLedMode::Fault) &&
              led.renderedRed() == faultRed,
          "explicit release disrupted an active Fault presentation");
  led.setMode(StatusLedMode::Connected, 1050);
  require(led.condition() ==
              static_cast<std::uint8_t>(StatusLedMode::Connected),
          "released owner did not return to native presentation");

  led.playCue(StatusLedCue::Menu, 100, 1060);
  require(led.condition() !=
              static_cast<std::uint8_t>(StatusLedMode::Connected),
          "native presentation did not accept an informational cue");
  led.setCustom(255, 0, 0, 128, 1070);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.effect() == StatusLedEffect::None &&
              led.renderedRed() >= 127 && led.renderedGreen() == 0,
          "steady manual owner did not preempt an informational cue");
  led.service(1200);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.effect() == StatusLedEffect::None &&
              led.renderedRed() >= 127,
          "canceled cue expiry destroyed the retained manual request");
  led.playCue(StatusLedCue::Reset, 240, 1210);
  require(led.condition() == 18 &&
              led.condition() != StatusLedController::ManualCondition,
          "system Reset cue did not preempt a manual owner");
  led.service(1449);
  require(led.condition() == 18,
          "system Reset cue ended before its 240 ms watchdog interval");
  const std::uint8_t resetReplacement[] =
      {1, 0, 0, 255, 0, 0, 0, 220, 25, 0x00, 0x05, 0};
  require(led.setEffect(resetReplacement, 1449) && led.condition() == 18,
          "changed manual descriptor stole the active Reset cue");
  led.service(1450);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.renderedBlue() > 20 && led.renderedRed() == 0,
          "system Reset cue did not restore the latest manual owner");
  led.setMode(StatusLedMode::Warning, 1460);
  led.setMode(StatusLedMode::Custom, 1470);
  require(led.condition() == StatusLedController::ManualCondition &&
              led.effect() == StatusLedEffect::Breathe &&
              led.renderedBlue() > 20 && led.renderedRed() == 0,
          "safety release did not restore the changed manual owner");

  const std::uint8_t transition[] =
      {4, 255, 0, 0, 0, 255, 0, 200, 0, 0x80, 0x02, 1};
  require(led.setEffect(transition, 2000),
          "finite Transition request was rejected");
  for (std::uint32_t tick = 2010; tick <= 2640; tick += 10) {
    led.service(tick);
  }
  require(led.effect() == StatusLedEffect::None &&
              led.condition() == StatusLedController::ManualCondition &&
              led.renderedGreen() > 190 && led.renderedRed() == 0,
          "finite Transition did not settle as retained steady output");
  led.setMode(StatusLedMode::Warning, 2650);
  led.setMode(StatusLedMode::Custom, 2660);
  require(led.effect() == StatusLedEffect::None &&
              led.renderedGreen() > 190 && led.renderedRed() == 0,
          "safety restore restarted a completed finite Transition");

  // A corrupt blank safety profile must use the configured board brightness,
  // not the most recent manual descriptor's local level.
  StatusLedController fallback{};
  fallback.begin(pwm, 100, 3000);
  fallback.setMode(StatusLedMode::Ready, 3001);
  std::uint8_t manualBright[] =
      {1, 0, 0, 255, 0, 0, 0, 220, 20, 0x80, 0x02, 0};
  require(fallback.setEffect(manualBright, 3002),
          "fallback-brightness manual descriptor was rejected");
  fallback.setMode(StatusLedMode::Warning, 3003);
  require(fallback.brightness() == 100,
          "blank AVR safety fallback inherited manual brightness");
  fallback.setMode(StatusLedMode::Custom, 3004);
  require(fallback.brightness() == 220 && fallback.renderedBlue() > 0,
          "AVR safety clear did not restore manual descriptor brightness");
  require(!fallback.setEffect(nullptr, 3005),
          "AVR accepted a null STATUS_EFFECT descriptor");
}

std::array<std::uint8_t, 3> rendered(const StatusLedController &led) {
  return {{led.renderedRed(), led.renderedGreen(), led.renderedBlue()}};
}

void testStatusEffectGoldenVectorsAndCadence() {
  PwmExpanderDriver driver(PwmController::PwmI2cAddress);
  PwmController pwm(driver);
  pwm.begin(true, 0);

  const auto verifyEffect = [&](std::uint8_t kind,
                                const auto &expectedMember) {
    StatusLedController led{};
    led.begin(pwm, 255, 0);
    led.setMode(StatusLedMode::Ready, 0);
    const auto descriptor = status_led_golden::descriptor(kind);
    require(led.setEffect(descriptor.data(), 0),
            "AVR golden descriptor was rejected");
    std::uint8_t phase = 0;
    std::uint32_t now = 0;
    for (const auto &golden : status_led_golden::frames) {
      while (phase != golden.phase) {
        phase = static_cast<std::uint8_t>(phase + 4U);
        now += 10;
        led.service(now);
      }
      require(rendered(led) == golden.*expectedMember,
              "AVR status effect diverged from a golden phase vector");
    }
  };

  verifyEffect(1, &status_led_golden::Frame::breathe);
  verifyEffect(2, &status_led_golden::Frame::flash);
  verifyEffect(3, &status_led_golden::Frame::cycle);
  verifyEffect(4, &status_led_golden::Frame::transition);

  for (std::uint8_t kind = 1; kind <= 4; ++kind) {
    StatusLedController boundary{};
    boundary.begin(pwm, 255, 0);
    boundary.setMode(StatusLedMode::Ready, 0);
    const auto descriptor = status_led_golden::descriptor(kind);
    require(boundary.setEffect(descriptor.data(), 0),
            "AVR boundary descriptor was rejected");
    boundary.service(639);
    const auto terminal =
        kind == 1 ? status_led_golden::frames[4].breathe
                  : (kind == 2 ? status_led_golden::frames[4].flash
                               : (kind == 3
                                      ? status_led_golden::frames[4].cycle
                                      : status_led_golden::frames[4].transition));
    require(rendered(boundary) == terminal,
            "AVR effect missed its period-1 terminal phase");
    boundary.service(640);
    const auto primary =
        kind == 1 ? status_led_golden::frames[0].breathe
                  : (kind == 2 ? status_led_golden::frames[0].flash
                               : (kind == 3
                                      ? status_led_golden::frames[0].cycle
                                      : status_led_golden::frames[0].transition));
    require(rendered(boundary) == primary,
            "AVR effect did not render phase zero at exact period");
    boundary.service(650);
    const auto first =
        kind == 1 ? status_led_golden::breatheFirstStep
                  : (kind == 2 ? status_led_golden::flashFirstStep
                               : (kind == 3
                                      ? status_led_golden::cycleFirstStep
                                      : status_led_golden::transitionFirstStep));
    const auto firstActual = rendered(boundary);
    require(firstActual == first,
            "AVR effect missed first post-wrap deadline kind=" +
                std::to_string(kind) + " actual=" +
                std::to_string(firstActual[0]) + "," +
                std::to_string(firstActual[1]) + "," +
                std::to_string(firstActual[2]));
    boundary.service(1930);
    require(rendered(boundary) == first,
            "AVR delayed multi-cycle service lost its absolute phase");
  }

  for (std::size_t index = 0;
       index < status_led_golden::interpolationPhases.size(); ++index) {
    const auto phase = status_led_golden::interpolationPhases[index];
    require(StatusLedMath::interpolate(240, 20, phase) ==
                    status_led_golden::descending[index] &&
                StatusLedMath::interpolate(20, 240, phase) ==
                    status_led_golden::ascending[index],
            "AVR bidirectional interpolation diverged from golden vector");
  }

  // Full-brightness transition samples expose descending interpolation by one
  // byte at the high phases where /255 or signed-16 math would otherwise hide.
  StatusLedController descending{};
  descending.begin(pwm, 255, 0);
  descending.setMode(StatusLedMode::Ready, 0);
  auto direct = status_led_golden::descriptor(4);
  direct[7] = 255;
  require(descending.setEffect(direct.data(), 0),
          "descending interpolation fixture was rejected");
  std::uint8_t directPhase = 0;
  std::uint32_t directNow = 0;
  for (std::size_t index = 0; index < status_led_golden::frames.size();
       ++index) {
    const auto target = status_led_golden::frames[index].phase;
    while (directPhase != target) {
      directPhase = static_cast<std::uint8_t>(directPhase + 4U);
      directNow += 10;
      descending.service(directNow);
    }
    require(descending.renderedRed() ==
                status_led_golden::directDescendingRed[index],
            "AVR descending interpolation diverged from golden vector");
  }

  struct ShippedEffect {
    const char *name;
    std::array<std::uint8_t, 12> descriptor;
  };
  const std::array<ShippedEffect, 5> shipped{{
      {"rf-breathe", {{1, 190, 0, 255, 0, 0, 0, 190, 20,
                        0x84, 0x03, 0}}},
      {"hot-breathe", {{1, 255, 0, 0, 0, 0, 0, 255, 72,
                         0xE8, 0x03, 0}}},
      {"factory-breathe", {{1, 16, 72, 255, 0, 0, 0, 145, 18,
                             0x40, 0x06, 0}}},
      {"named-breathe-blue", {{1, 30, 120, 255, 0, 0, 0, 200, 8,
                                0x08, 0x07, 0}}},
      {"factory-bt-off-cycle", {{3, 0, 255, 80, 255, 0, 0, 128, 0,
                                  0xD0, 0x07, 0}}},
  }};
  for (const auto &fixture : shipped) {
    StatusLedController led{};
    led.begin(pwm, 255, 0);
    led.setMode(StatusLedMode::Ready, 0);
    require(led.setEffect(fixture.descriptor.data(), 0),
            std::string(fixture.name) + " descriptor was rejected");
    auto previous = rendered(led);
    unsigned changes = 0;
    for (std::uint32_t now = 17; now <= 1000; now += 17) {
      led.service(now);
      const auto current = rendered(led);
      if (current != previous) {
        ++changes;
        previous = current;
      }
    }
    require(changes >= 20 && changes <= 60,
            std::string(fixture.name) +
                " smooth changed-frame cadence escaped 20..60 Hz");
  }

  // Finite effects preserve the exact descriptor duration across the complete
  // accepted 640..60000 ms range, including a delayed cooperative service.
  const std::array<std::uint16_t, 4> periods{{640, 1280, 3200, 60000}};
  for (const std::uint16_t period : periods) {
    StatusLedController led{};
    led.begin(pwm, 255, 0);
    led.setMode(StatusLedMode::Ready, 0);
    auto descriptor = status_led_golden::descriptor(4, 1);
    descriptor[9] = static_cast<std::uint8_t>(period);
    descriptor[10] = static_cast<std::uint8_t>(period >> 8U);
    require(led.setEffect(descriptor.data(), 0),
            "finite duration descriptor was rejected");
    led.service(static_cast<std::uint32_t>(period) - 1U);
    require(led.effect() == StatusLedEffect::Transition,
            "finite status effect completed before its exact period");
    led.service(period);
    require(led.effect() == StatusLedEffect::None &&
                rendered(led) == status_led_golden::transitionEndpoint,
            "finite status effect missed its exact terminal deadline");
    require(led.setEffect(descriptor.data(),
                          static_cast<std::uint32_t>(period) + 1U) &&
                led.effect() == StatusLedEffect::Transition &&
                rendered(led) ==
                    status_led_golden::frames[0].transition,
            "completed AVR descriptor did not restart at phase zero");

    StatusLedController delayed{};
    delayed.begin(pwm, 255, 0);
    delayed.setMode(StatusLedMode::Ready, 0);
    require(delayed.setEffect(descriptor.data(), 0),
            "delayed finite duration descriptor was rejected");
    delayed.service(static_cast<std::uint32_t>(period) + 9U);
    require(delayed.effect() == StatusLedEffect::None &&
                rendered(delayed) == status_led_golden::transitionEndpoint,
            "delayed scheduler tick did not finish at the retained endpoint");
  }
  StatusLedController tenMillisecond{};
  tenMillisecond.begin(pwm, 255, 0);
  tenMillisecond.setMode(StatusLedMode::Ready, 0);
  auto period1800 = status_led_golden::descriptor(4, 1);
  period1800[9] = 0x08;
  period1800[10] = 0x07;
  require(tenMillisecond.setEffect(period1800.data(), 0),
          "1800 ms AVR duration fixture was rejected");
  for (std::uint32_t tick = 10; tick < 1800; tick += 10) {
    tenMillisecond.service(tick);
  }
  require(tenMillisecond.effect() == StatusLedEffect::Transition,
          "10 ms scheduler drift completed the 1800 ms effect early");
  tenMillisecond.service(1800);
  require(tenMillisecond.effect() == StatusLedEffect::None &&
              rendered(tenMillisecond) ==
                  status_led_golden::transitionEndpoint,
          "10 ms scheduler drift missed the exact 1800 ms endpoint");
  auto tooLong = status_led_golden::descriptor(1);
  tooLong[9] = 0x61;
  tooLong[10] = 0xEA; // 60001 ms exceeds the documented wire maximum.
  StatusLedController bounded{};
  bounded.begin(pwm, 255, 0);
  bounded.setMode(StatusLedMode::Ready, 0);
  require(!bounded.setEffect(tooLong.data(), 0),
          "AVR accepted a STATUS_EFFECT period above 60000 ms");
}

void testSemanticProtocolAndTemperatureRoles() {
  require(ControllerProtocol::ProgramState == 0x45,
          "semantic PROGRAM_STATE opcode moved");
  require(TemperatureRoles::fromSortedIndex(0, false) ==
              TemperatureRoles::Led &&
              TemperatureRoles::fromSortedIndex(1, false) ==
                  TemperatureRoles::BluetoothAudio,
          "factory sorted-ROM temperature roles are reversed");
  require(TemperatureRoles::fromSortedIndex(0, true) ==
              TemperatureRoles::BluetoothAudio &&
              TemperatureRoles::fromSortedIndex(1, true) ==
                  TemperatureRoles::Led,
          "EEPROM temperature swap did not reverse both roles");
}

void testFrontPanelLeafDecreaseDispatch() {
  for (std::uint8_t mode = MODE_DOOR; mode <= MODE_RF; ++mode) {
    const auto current = static_cast<ProgramMode>(mode);
    const LeafDecreaseAction expected =
        current == MODE_KEYS
            ? LeafDecreaseAction::IdentifyKey3
            : (current == MODE_RELAY
                   ? LeafDecreaseAction::AllRelaysOff
                   : LeafDecreaseAction::ParentCategory);
    require(leafDecreaseAction(current) == expected,
            "leaf K3 dispatch no longer matches its page context");
  }
  require(static_cast<std::uint8_t>(MENU_DECREASE) + 1U == 3U,
          "KEY-page K3 identification no longer resolves to key 3");
}

void testDallasAbsentPullupBound() {
  arduino_mock::resetHardware();
  arduino_mock::portInput = 0; // Missing pull-up/stuck-low bus.
  DallasTemperatureBus bus(PIN_PB2);
  bus.begin();
  require(bus.getDeviceCount() == 0,
          "stuck-low DS18B20 bus produced a phantom device");
  require(!bus.requestTemperatures(),
          "absent DS18B20 bus accepted a conversion request");
}

void testBuzzerTimerAndQueue() {
  TonePlayer player(PIN_PB1);
  player.begin();
  TIMSK1 = 0xFF;
  player.beep(40, 2000);
  player.update(100);
  require(player.isBusy() && (TCCR1A & _BV(COM1A0)) != 0 &&
              (TIMSK1 & _BV(OCIE1A)) == 0,
          "buzzer did not use hardware OC1A with its ISR disabled");
  player.update(140);
  require(!player.isBusy() &&
              (TCCR1B & (_BV(CS12) | _BV(CS11) | _BV(CS10))) == 0,
          "buzzer deadline did not stop Timer1 cleanly");

  player.setMuted(true);
  player.success();
  player.update(200);
  require(player.isBusy() && (TCCR1A & _BV(COM1A0)) == 0,
          "muted cue drove the physical buzzer output");
  player.update(270);
  player.update(300);
  player.update(410);
  require(!player.isBusy(), "muted cue did not drain cooperatively");

  player.stop();
  for (unsigned index = 0; index < 10; ++index) {
    require(player.enqueue(1000, 1), "tone queue rejected a valid slot");
  }
  require(!player.enqueue(1000, 1), "tone queue exceeded its static bound");
  player.stop();
}

} // namespace

int main() {
  try {
    testKeyGestures();
    testRelayInterlocks();
    testMotionDoorPolicyMatrixAndEntryPaths();
    testTransitionsAndRollover();
    testDisplayBrightnessFade();
    testStatusEffectOwnershipAndIdempotence();
    testStatusEffectGoldenVectorsAndCadence();
    testSemanticProtocolAndTemperatureRoles();
    testFrontPanelLeafDecreaseDispatch();
    testDallasAbsentPullupBound();
    testBuzzerTimerAndQueue();
    std::cout << "firmware_controls_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_controls_tests: " << error.what() << '\n';
    return 1;
  }
}
