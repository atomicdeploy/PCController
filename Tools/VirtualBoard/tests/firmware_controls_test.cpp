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
#include "Project/TemperatureRoles.h"
#include "Project/TransitionMath.h"
#include "Project/UartProtocol.h"

#include <algorithm>
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
  shiftRegisters.begin();
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

  require(PwmValueRollover::next(3840, 256) == 4095 &&
              PwmValueRollover::next(4095, 256) == 0 &&
              PwmValueRollover::next(255, -256) == 0 &&
              PwmValueRollover::next(0, -256) == 4095,
          "seven-segment PWM editor rollover skipped an endpoint");

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
  SevenSegments initiallyOff;
  initiallyOff.begin(0);
  require(initiallyOff.lastCommandForTest() == 0x80,
          "TM1637 begin(0) did not issue the display-off command");

  SevenSegments segments;
  segments.begin(5);
  const auto initialRevision = segments.revision();
  require(initialRevision != 0 &&
              std::equal(segments.rawSegments(), segments.rawSegments() + 4,
                         segments.presentationState()) &&
              segments.presentationState()[4] == segments.brightness(),
          "TM1637 push state is not contiguous segments plus brightness");
  segments.showText("test");
  const auto textRevision = segments.revision();
  require(textRevision != initialRevision,
          "TM1637 segment change did not advance its push revision");
  segments.showText("test");
  require(segments.revision() == textRevision,
          "unchanged TM1637 cells generated a duplicate push revision");
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
  require(segments.revision() != textRevision &&
              segments.presentationState()[4] == 7,
          "TM1637 brightness changes did not update the push state");
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
        current == MODE_RELAY ? LeafDecreaseAction::AllRelaysOff
                              : LeafDecreaseAction::ParentCategory;
    require(leafDecreaseAction(current) == expected,
            "leaf K3 dispatch no longer matches its page context");
  }

  require(canonicalFrontPanelPage(PAGE_KEYS) == PAGE_MOTION &&
              canonicalFrontPanelPage(PAGE_MOTION) == PAGE_MOTION &&
              !frontPanelPageCompiled(PAGE_KEYS) &&
              frontPanelPageCompiled(PAGE_MOTION),
          "retired KEY page is no longer one canonical MOVE surface");
  require(unifiedInputIntent(MENU_PREVIOUS, true) ==
                  UnifiedInputIntent::PreviousPage &&
              unifiedInputIntent(MENU_NEXT, true) ==
                  UnifiedInputIntent::NextPage,
          "diagnostic key page lost a direct exit");
  require(unifiedInputIntent(MENU_DECREASE, false) ==
                  UnifiedInputIntent::Macro &&
              unifiedInputIntent(MENU_INCREASE, false) ==
                  UnifiedInputIntent::Motion,
          "normal unified page no longer exposes macro/motion actions");
  require(unifiedMacroGesture(KeyEvent::Down, false, false) ==
                  UnifiedMacroGesture::ImmediateCapture &&
              unifiedMacroGesture(KeyEvent::HoldRepeat, false, false) ==
                  UnifiedMacroGesture::None &&
              unifiedMacroGesture(KeyEvent::Down, true, false) ==
                  UnifiedMacroGesture::None &&
              unifiedMacroGesture(KeyEvent::Click, true, false) ==
                  UnifiedMacroGesture::Replay &&
              unifiedMacroGesture(KeyEvent::HoldStart, true, false) ==
                  UnifiedMacroGesture::ReplaceCapture &&
              unifiedMacroGesture(KeyEvent::Click, true, true) ==
                  UnifiedMacroGesture::SuppressClassification,
          "unified macro key lost one-shot replay/replace classification");

  const MotionKeyBinding expectedMotion[] = {
      {0, false}, {0, true}, {1, false}, {1, true}};
  for (std::uint8_t action = MENU_PREVIOUS; action <= MENU_INCREASE;
       ++action) {
    const auto actual = motionKeyBinding(static_cast<MenuAction>(action));
    require(actual.side == expectedMotion[action].side &&
                actual.reverse == expectedMotion[action].reverse,
            "four front keys no longer map to A/B up/down immediately");
  }
}

void testPowerSignalFallbackPolicy() {
  std::uint16_t value = 0;
  const std::uint16_t first =
      PowerSignalFallback::nextValue(value, true, false);
  require(first == PowerSignalFallback::Step,
          "offline power signal did not start with one bounded fade step");
  value = first;
  for (std::uint8_t turn = 0; turn < 20; ++turn) {
    value = PowerSignalFallback::nextValue(value, true, false);
  }
  require(value == PowerSignalFallback::FullBrightness,
          "offline power signal did not saturate at full brightness");
  require(PowerSignalFallback::nextValue(731, false, false) == 731,
          "reconnected host did not retain channel-12 ownership");
  require(PowerSignalFallback::nextValue(0, true, true) == 0,
          "Prog mode allowed the fallback to re-enable channel 12");
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
    testPowerSignalFallbackPolicy();
    testKeyGestures();
    testRelayInterlocks();
    testMotionDoorPolicyMatrixAndEntryPaths();
    testTransitionsAndRollover();
    testDisplayBrightnessFade();
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
