#include <EEPROM.h>

#include "Project/AutomationStore.h"
#include "Project/EepromLayout.h"

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

AutomationRecord relayRule(std::uint8_t id, std::uint8_t eventValue,
                           std::uint8_t eventMask = 0xFF) {
  return AutomationRecord{
      id,
      AutomationFlags::Enabled,
      static_cast<std::uint8_t>(AutomationEventKind::Door),
      eventValue,
      eventMask,
      static_cast<std::uint8_t>(AutomationActionKind::Relay),
      4,
      1,
      0,
      0,
  };
}

void testEmptyStoreAndTransactionalCrud() {
  EEPROM.fill(0xFF);
  AutomationStore store;
  store.begin();
  require(store.generation() == 1 && store.count() == 0,
          "erased EEPROM did not initialize a valid empty generation");

  AutomationRecord first = relayRule(AutomationStore::NewRecord, 1);
  require(store.put(first) && first.id == 0 && store.generation() == 2,
          "add did not allocate slot zero and commit generation two");
  AutomationRecord readback;
  require(store.get(0, readback) && readback.value == 1,
          "added automation did not read back exactly");

  first.value = 2;
  require(store.put(first) && store.get(0, readback) && readback.value == 2,
          "edit did not replace the stable slot transactionally");
  require(store.remove(0) && store.count() == 0,
          "remove did not publish an empty replacement slot");
  require(!store.remove(0), "remove accepted a vacant slot");
}

void testTornNewBankFallsBackToPreviousGeneration() {
  EEPROM.fill(0xFF);
  AutomationStore store;
  store.begin(); // bank A generation 1
  AutomationRecord first = relayRule(AutomationStore::NewRecord, 1);
  require(store.put(first), "fixture add failed"); // bank B generation 2

  EEPROM.update(EepromLayout::AutomationBankBAddress +
                    EepromLayout::AutomationHeaderBytes - 1,
                0);
  AutomationStore recovered;
  recovered.begin();
  require(recovered.generation() == 1 && recovered.count() == 0,
          "torn destination displaced the previous committed generation");
}

struct ExecutionCapture {
  std::vector<std::uint8_t> ids;
  AutomationExecutor *executor = nullptr;
};

bool captureAction(const AutomationRecord &record, void *context) {
  auto &capture = *static_cast<ExecutionCapture *>(context);
  capture.ids.push_back(record.id);
  if (capture.executor != nullptr) {
    require(capture.executor->dispatch(AutomationEventKind::Door, 1, 10) == 0,
            "executor recursion guard allowed re-entry");
  }
  return true;
}

void testDeterministicMatchingAndBounds() {
  EEPROM.fill(0xFF);
  AutomationStore store;
  store.begin();
  for (std::uint8_t index = 0; index < 6; ++index) {
    AutomationRecord record =
        relayRule(AutomationStore::NewRecord,
                  static_cast<std::uint8_t>(index == 5 ? 0 : 1),
                  index == 4 ? 0 : 0xFF);
    record.actionTarget = index;
    require(store.put(record), "executor fixture add failed");
  }

  ExecutionCapture capture;
  AutomationExecutor executor(store, captureAction, &capture);
  capture.executor = &executor;
  require(executor.dispatch(AutomationEventKind::Door, 1, 10) == 4,
          "per-event action budget was not enforced");
  require(capture.ids == std::vector<std::uint8_t>({0, 1, 2, 3}),
          "matching records did not execute in ascending slot order");

  capture.ids.clear();
  require(executor.dispatch(AutomationEventKind::Door, 1, 20) == 4,
          "second dispatch did not consume the remaining rate budget");
  capture.ids.clear();
  require(executor.dispatch(AutomationEventKind::Door, 1, 30) == 0,
          "one-second global action budget was not enforced");
  require(executor.dispatch(AutomationEventKind::Door, 1, 1010) == 4,
          "rate window did not reset after one second");
}

void testUnsafeRecordsAreRejected() {
  AutomationRecord hostRelay = relayRule(AutomationStore::NewRecord, 0);
  hostRelay.eventKind = static_cast<std::uint8_t>(AutomationEventKind::Host);
  require(!AutomationStore::valid(hostRelay),
          "host-loss rule could re-enable a relay");
  hostRelay.actionKind =
      static_cast<std::uint8_t>(AutomationActionKind::MotionStop);
  hostRelay.actionTarget = 0xFF;
  hostRelay.value = 0;
  require(AutomationStore::valid(hostRelay),
          "safe all-motion stop rule was rejected");
}

} // namespace

int main() {
  try {
    testEmptyStoreAndTransactionalCrud();
    testTornNewBankFallsBackToPreviousGeneration();
    testDeterministicMatchingAndBounds();
    testUnsafeRecordsAreRejected();
    std::cout << "automation_store_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "automation_store_tests: " << error.what() << '\n';
    return 1;
  }
}
