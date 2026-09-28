#include "Project/Core/MacroRing.h"

#include <array>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {

using ControllerCore::MacroRing;

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

MacroRing makeRing(uint8_t eventType = 6) {
  MacroRing ring{};
  ring.initialize(eventType);
  return ring;
}

void testLifecycleAndRollover() {
  MacroRing ring = makeRing();
  require(ring.status().type == 6 &&
              ring.status().report.state == MacroRing::Idle,
          "schema-2 macro status envelope drifted");

  ring.begin(9, 0, 1);
  const std::array<std::uint8_t, 8> record{{0x00, 0x02, 0x00, 0x00,
                                             0x34, 0x02, 0xAA, 0x55}};
  require(ring.append(0, 1, record.data(),
                      static_cast<std::uint8_t>(record.size()), 0),
          "valid schema-2 record was rejected");
  require(ring.status().report.state == MacroRing::Buffering &&
              ring.status().report.id == 9 &&
              ring.status().report.acceptedSteps == 1 &&
              ring.status().report.acceptedBytes == record.size() &&
              ring.status().report.fill == record.size(),
          "buffering report no longer describes accepted schema-2 bytes");

  require(ring.start(0xFFFFFF00UL), "ready macro did not start");
  MacroRing::Command command{};
  std::array<std::uint8_t, 48> payload{};
  require(ring.dequeueDue(0x000000FFUL, command, payload.data(),
                          static_cast<std::uint8_t>(payload.size())) ==
              MacroRing::NotDue,
          "32-bit microsecond rollover released a record too early");
  require(ring.dequeueDue(0x00000100UL, command, payload.data(),
                          static_cast<std::uint8_t>(payload.size())) ==
              MacroRing::Ready &&
              command.opcode == 0x34 && command.payloadLength == 2 &&
              payload[0] == 0xAA && payload[1] == 0x55,
          "due record did not preserve opcode/payload across rollover");
  require(ring.completeStep(true) &&
              ring.status().report.state == MacroRing::Completed &&
              ring.status().report.executedSteps == 1 &&
              ring.status().report.fill == 0,
          "completed macro did not publish the terminal schema-2 state");
}

void testBoundedQueueAndStartGate() {
  MacroRing ring = makeRing();
  ring.begin(1, 0, 2);
  std::array<std::uint8_t, 64> initial{};
  initial[4] = 0x40;
  initial[5] = 58;
  require(ring.append(0, 1, initial.data(),
                      static_cast<std::uint8_t>(initial.size()), 0),
          "64-byte streaming record was rejected");
  require(ring.canStart(),
          "schema-2 streaming threshold did not allow a 64-byte buffer");

  MacroRing capacity = makeRing();
  capacity.begin(2, 0, 0);
  std::array<std::uint8_t, MacroRing::Capacity> bytes{};
  require(capacity.append(0, 0, bytes.data(),
                          static_cast<std::uint8_t>(bytes.size()), 0) &&
              capacity.status().report.fill == MacroRing::Capacity,
          "128-byte ring did not retain its documented 127 usable bytes");
  const std::uint8_t extra = 0;
  require(!capacity.append(MacroRing::Capacity, 0, &extra, 1, 0),
          "macro ring accepted byte 128 and overwrote its circular head");
}

void testMalformedRecordAndSafeStop() {
  MacroRing ring = makeRing();
  ring.begin(3, 0, 1);
  std::array<std::uint8_t, 55> malformed{};
  malformed[4] = 0x41;
  malformed[5] = 49;
  require(ring.append(0, 1, malformed.data(),
                      static_cast<std::uint8_t>(malformed.size()), 0) &&
              ring.start(0),
          "malformed-record fixture could not enter playback");
  MacroRing::Command command{};
  std::array<std::uint8_t, 48> payload{};
  require(ring.dequeueDue(0, command, payload.data(),
                          static_cast<std::uint8_t>(payload.size())) ==
              MacroRing::Malformed &&
              ring.status().report.state == MacroRing::Failed &&
              ring.status().report.dispatchErrors == 1 &&
              ring.takeSafeStopRequest() && !ring.takeSafeStopRequest(),
          "malformed record did not fail closed with one safe-stop request");
}

void testOversizedHeaderCannotStarvePlayback() {
  MacroRing ring = makeRing();
  ring.begin(4, 0, 2);
  const std::array<std::uint8_t, 12> records{{
      0, 0, 0, 0, 0x40, 0,
      0, 0, 0, 0, 0x41, 0xFF,
  }};
  require(ring.append(0, 2, records.data(),
                      static_cast<std::uint8_t>(records.size()), 0) &&
              ring.start(0),
          "oversized-header fixture could not enter playback");

  MacroRing::Command command{};
  std::array<std::uint8_t, 48> payload{};
  require(ring.dequeueDue(0, command, payload.data(),
                          static_cast<std::uint8_t>(payload.size())) ==
              MacroRing::Ready &&
              !ring.completeStep(true),
          "valid record before oversized header did not complete");
  require(ring.dequeueDue(0, command, payload.data(),
                          static_cast<std::uint8_t>(payload.size())) ==
              MacroRing::Malformed &&
              ring.status().report.state == MacroRing::Failed &&
              ring.status().report.dispatchErrors == 1 &&
              ring.takeSafeStopRequest() && !ring.takeSafeStopRequest(),
          "oversized header starved playback instead of failing safe once");
}

void testCancelOptions() {
  MacroRing keepOutputs = makeRing();
  keepOutputs.begin(4, MacroRing::KeepOutputsOnCancel, 1);
  require(keepOutputs.defaultKeepOutputsOnCancel() &&
              keepOutputs.cancel(keepOutputs.defaultKeepOutputsOnCancel()) &&
              keepOutputs.status().report.state == MacroRing::Cancelled &&
              !keepOutputs.takeSafeStopRequest(),
          "keep-output cancellation changed the safe-stop contract");

  MacroRing safeStop = makeRing();
  safeStop.begin(5, 0, 1);
  require(safeStop.cancel(false) && safeStop.takeSafeStopRequest(),
          "ordinary cancellation no longer requests the dispatcher safe-stop");
}

uint32_t read32(const uint8_t *bytes) {
  return uint32_t(bytes[0]) | uint32_t(bytes[1]) << 8 |
         uint32_t(bytes[2]) << 16 | uint32_t(bytes[3]) << 24;
}

void testRecordingCircularRetentionAndRollover() {
  MacroRing ring = makeRing();
  ring.beginRecording(42, 0x10, 0xFFFFFF00UL);
  require(ring.active() && !ring.append(5, 1, nullptr, 0, 0),
          "stream APPEND corrupted an active recording");
  require(!ring.recordRelay(0x10, 0xFFFFFF10UL), "unchanged mask was recorded");
  for (uint32_t index = 1; index <= 40; ++index) {
    require(ring.recordRelay(static_cast<uint8_t>(index),
                             static_cast<uint32_t>(0xFFFFFF00UL + index * 137)),
            "applied edge was lost at micros rollover");
  }
  require(ring.stopRecording() && !ring.active() && ring.hasRecording(),
          "record STOP discarded retained RAM");
  require(ring.claimSharedWorkspace() == nullptr,
          "strip workspace claim silently destroyed retained recording");
  const auto &report = ring.status().report;
  require(report.fill == 125 && report.totalSteps == 25 && report.underruns == 16,
          "bounded snapshot ring did not evict whole oldest snapshots");
  std::array<uint8_t, 125> bytes{};
  require(ring.readRecording(0, bytes.data(), 125) == 125 &&
              read32(bytes.data()) == 16 * 137 && bytes[4] == 16 &&
              read32(bytes.data() + 120) == 40 * 137 && bytes[124] == 40,
          "ring wrap changed masks or raw MCU edge timestamps");
  std::array<uint8_t, 44> page{};
  require(ring.readRecording(40, page.data(), 44) == 40 &&
              read32(page.data()) == 24 * 137 && page[4] == 24 &&
              ring.readRecording(1, page.data(), 44) == 0,
          "paged recording read split a snapshot or misread circular position");
}

void testRetainedReplayExactDeadlinesAndCancellation() {
  MacroRing ring = makeRing();
  ring.beginRecording(9, 0, 900);
  ring.recordRelay(0x10, 1037);
  ring.recordRelay(0, 1642);
  ring.stopRecording();
  require(ring.startRecorded(0xFFFFFFA0UL, 0x34), "retained replay did not start");
  MacroRing::Command command{};
  uint8_t payload[2]{};
  for (uint8_t snapshot = 0; snapshot < 3; ++snapshot) {
    const uint32_t delta = snapshot == 0 ? 0 : snapshot == 1 ? 137 : 742;
    const uint32_t deadline = static_cast<uint32_t>(0xFFFFFFA0UL + delta);
    if (snapshot != 0) {
      require(ring.dequeueDue(deadline - 1, command, payload, 2) == MacroRing::NotDue,
              "retained relay edge was replayed before its microsecond deadline");
    }
    require(ring.dequeueDue(deadline, command, payload, 2) == MacroRing::Ready &&
                command.opcode == 0x34 && command.payloadLength == 1 &&
                payload[0] == (snapshot == 1 ? 0x10 : 0),
            "retained replay bypassed aggregate relay opcode or changed mask");
    ring.completeStep(true);
  }
  require(ring.hasRecording() && ring.status().report.executedSteps == 3 &&
              ring.stopRecording() &&
              ring.startRecorded(1200, 0x34) && ring.cancel(false) &&
              ring.takeSafeStopRequest() && ring.hasRecording(),
          "replay/cancel destroyed RAM recording or omitted safe stop");
  uint8_t bytes[15]{};
  require(ring.readRecording(0, bytes, 15) == 15 && read32(bytes + 5) == 137 &&
              read32(bytes + 10) == 742,
          "repeated replay changed the stored profile");
  ring.beginRecording(1, 0, 0);
  require(!ring.recordRelay(1, 0x80000000UL) && ring.hasRecording(),
          "oversized recording duration silently wrapped deadline ordering");
  require(ring.clearRecording() && ring.claimSharedWorkspace() != nullptr &&
              !ring.hasRecording(), "explicit clear did not release shared workspace");
  ring.claimSharedWorkspace()[299] = 0xA5;
  ring.beginRecording(3, 0, 123);
  require(!ring.clearRecording() && ring.claimSharedWorkspace() == nullptr,
          "shared workspace was writable while recorder owned it");
}

} // namespace

int main() {
  try {
    testLifecycleAndRollover();
    testBoundedQueueAndStartGate();
    testMalformedRecordAndSafeStop();
    testOversizedHeaderCannotStarvePlayback();
    testCancelOptions();
    testRecordingCircularRetentionAndRollover();
    testRetainedReplayExactDeadlinesAndCancellation();
    std::cout << "firmware_macro_ring_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_macro_ring_tests: " << error.what() << '\n';
    return 1;
  }
}
