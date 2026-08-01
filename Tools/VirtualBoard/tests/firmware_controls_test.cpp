#include <Arduino.h>
#include <avr/interrupt.h>

#include "LocalLib/DallasTemperatureBus.h"
#include "LocalLib/Keys.h"
#include "LocalLib/ShiftRegisters.h"
#include "LocalLib/TonePlayer.h"
#include "Project/RelayController.h"
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
  unsigned presses = 0;
  unsigned releases = 0;
};

void pressed(std::uint8_t, void *context) {
  ++static_cast<KeyTrace *>(context)->presses;
}

void released(std::uint8_t, void *context) {
  ++static_cast<KeyTrace *>(context)->releases;
}

void gestured(std::uint8_t, KeyEvent event, void *context) {
  static_cast<KeyTrace *>(context)->events.push_back(event);
}

void bind(Key &key, KeyTrace &trace) {
  key.setPressCallback(pressed, &trace);
  key.setReleaseCallback(released, &trace);
  key.setEventCallback(gestured, &trace);
}

void testKeyGestures() {
  shiftRegisters.clearVirtualInputs();

  {
    Key key(0);
    KeyTrace trace;
    bind(key, trace);
    key.update(0);
    shiftRegisters.setVirtualInput(0, true);
    key.update(1);
    key.update(51);
    shiftRegisters.setVirtualInput(0, false);
    key.update(100);
    key.update(150);
    key.update(451);
    require(trace.presses == 1 && trace.releases == 1,
            "debounced click did not invoke one press/release");
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
    require(trace.presses == 2 && trace.releases == 2,
            "double-click did not retain two immediate press actions");
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
    require(trace.presses == 1 && trace.releases == 1,
            "hold invoked an extra immediate press");
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
    require(trace.presses == 1 && trace.events.front() == KeyEvent::Down,
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
  require((controller.activeRelayMask() & _BV(0)) != 0 &&
              (controller.activeRelayMask() & _BV(1)) == 0,
          "R1 direction changed before R2 output remained safely off");
  controller.service(60);
  require((controller.activeRelayMask() & _BV(1)) == 0,
          "Side A enabled before its 50 ms direction settle");
  controller.service(61);
  require((controller.activeRelayMask() & (_BV(0) | _BV(1))) ==
              (_BV(0) | _BV(1)),
          "Side A did not enable after direction settling");

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
  controller.service(106);
  require((controller.activeRelayMask() & _BV(2)) != 0,
          "R3 did not become Side B direction after cross-side stagger");

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
    testTransitionsAndRollover();
    testSemanticProtocolAndTemperatureRoles();
    testDallasAbsentPullupBound();
    testBuzzerTimerAndQueue();
    std::cout << "firmware_controls_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_controls_tests: " << error.what() << '\n';
    return 1;
  }
}
