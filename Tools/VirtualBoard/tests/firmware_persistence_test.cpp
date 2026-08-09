#include <EEPROM.h>

#include "Project/EepromLayout.h"
#include "Project/RemoteLearningStore.h"
#include "Project/SettingsStore.h"

#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {

void require(bool condition, const std::string &message) {
  if (!condition) {
    throw std::runtime_error(message);
  }
}

void requireAtMostOneUpdate(std::size_t before, const std::string &owner) {
  require(EEPROM.updates().size() <= before + 1,
          owner + " wrote more than one EEPROM byte in one service turn");
}

void drainSettings(SettingsStore &store, std::uint16_t maximum = 100) {
  while (!store.persisted() && maximum-- != 0) {
    const auto before = EEPROM.updates().size();
    store.service(0, true);
    requireAtMostOneUpdate(before, "settings");
  }
  require(store.persisted() && !store.dirty(),
          "settings write never reached its CRC commit point");
}

void drainRemotes(RemoteLearningStore &store, std::uint16_t maximum = 400) {
  while (store.busy() && maximum-- != 0) {
    const auto before = EEPROM.updates().size();
    require(store.service(), "busy RF store made no cooperative progress");
    requireAtMostOneUpdate(before, "learned RF");
  }
  require(!store.busy(), "learned-RF write never reached its commit point");
}

void testSettingsPublishLastAndPolling() {
  EEPROM.fill(0xFF);
  SettingsStore store;
  require(!store.begin(0) && !store.persisted(),
          "blank settings unexpectedly appeared persisted");
  store.values().flags =
      SettingsFlags::ProgrammingMode | SettingsFlags::Silent;
  const std::uint8_t name[] = {'L', 'I', 'V', 'E'};
  require(store.setBoardName(name, sizeof(name)),
          "board name fixture was rejected");
  store.saveNow();
  require(store.dirty() && !store.persisted() && EEPROM.updates().empty(),
          "saveNow blocked or falsely published persistence");

  const int checksumAddress = SettingsStore::EepromAddress +
                              SettingsRecordLayout::RecordBytes - 1;
  drainSettings(store);
  require(!EEPROM.updates().empty() &&
              EEPROM.updates().front().address == checksumAddress &&
              EEPROM.updates().back().address == checksumAddress,
          "settings CRC was not invalidated first and published last");

  SettingsStore reloaded;
  require(reloaded.begin(0) && reloaded.persisted() &&
              reloaded.values().programmingMode(),
          "preseeded Prog setting did not survive CRC-backed reload");
  std::uint8_t readName[SettingsStore::MaximumBoardNameLength + 1]{};
  require(reloaded.boardName(readName) && readName[0] == sizeof(name) &&
              readName[1] == 'L' && readName[4] == 'E',
          "persisted board name did not reload");

  reloaded.markDirty(10);
  require(!reloaded.persisted(),
          "markDirty left the old record falsely reported as persisted");
}

void testSettingsDualBankPowerLossAtEveryWriteIndex() {
  EEPROM.fill(0xFF);
  SettingsStore store;
  store.begin(0);
  store.values().streamPeriodMs = 600;
  store.saveNow();
  drainSettings(store);

  // Exercise both directions: canonical bank 32 -> staging bank 0, then the
  // staging bank -> canonical bank. A reset before checksum publication must
  // always recover the complete old bank; after checksum publication the new
  // generation is independently valid and wins modulo-16 selection.
  for (std::uint8_t direction = 0; direction < 2; ++direction) {
    const std::uint16_t oldPeriod = direction == 0 ? 600 : 700;
    const std::uint16_t newPeriod = direction == 0 ? 700 : 800;
    const EEPROMClass durableOld = EEPROM;
    for (std::uint8_t completedTurns = 0;
         completedTurns <= SettingsRecordLayout::RecordBytes + 2U;
         ++completedTurns) {
      EEPROM = durableOld;
      SettingsStore writer;
      require(writer.begin(0) &&
                  writer.values().streamPeriodMs == oldPeriod,
              "dual-bank fixture did not load its durable old generation");
      writer.values().streamPeriodMs = newPeriod;
      writer.saveNow();
      for (std::uint8_t turn = 0; turn < completedTurns; ++turn) {
        writer.service(0, true);
      }

      SettingsStore recovered;
      require(recovered.begin(0) && recovered.persisted(),
              "a torn inactive-bank write hid the complete active bank");
      const bool checksumPublished =
          completedTurns >= SettingsRecordLayout::RecordBytes + 1U;
      require(recovered.values().streamPeriodMs ==
                  (checksumPublished ? newPeriod : oldPeriod),
              "dual-bank recovery selected the wrong generation at a write cut");
    }

    // Establish the new generation as the durable starting point for the
    // reverse-direction matrix.
    EEPROM = durableOld;
    SettingsStore writer;
    require(writer.begin(0), "durable bank disappeared before reverse test");
    writer.values().streamPeriodMs = newPeriod;
    writer.saveNow();
    drainSettings(writer);
  }
}

void testSettingsMidFlightMutationAndReadyGate() {
  EEPROM.fill(0xFF);
  SettingsStore store;
  store.begin(0);
  store.values().streamPeriodMs = 600;
  store.saveNow();
  drainSettings(store);

  store.values().streamPeriodMs = 700;
  store.saveNow();
  for (std::uint8_t turn = 0; turn < 5; ++turn) {
    store.service(0, true);
  }
  SettingsStore torn;
  require(torn.begin(0) && torn.persisted() &&
              torn.values().streamPeriodMs == 600,
          "power loss did not recover the untouched settings bank");

  // A newer host update arriving while the 700 ms snapshot is in flight must
  // queue a second coherent snapshot, never splice bytes into the first.
  store.values().streamPeriodMs = 800;
  store.saveNow();
  for (std::uint8_t turn = 5;
       turn < SettingsRecordLayout::RecordBytes + 2U; ++turn) {
    store.service(0, true);
  }
  require(!store.persisted() && store.dirty(),
          "older in-flight settings snapshot published newer RAM as durable");
  drainSettings(store);
  SettingsStore latest;
  require(latest.begin(0) && latest.values().streamPeriodMs == 800,
          "mid-flight settings update did not publish its own final snapshot");

  // EEPROM.update starts a hardware write asynchronously. Even after the CRC
  // byte has been issued, persisted must remain false until EEPE clears and a
  // later service turn reads the committed byte back.
  latest.values().streamPeriodMs = 900;
  latest.saveNow();
  for (std::uint8_t turn = 0;
       turn < SettingsRecordLayout::RecordBytes + 1U; ++turn) {
    latest.service(0, true);
  }
  require(!latest.persisted() && latest.dirty(),
          "settings claimed durability before CRC readiness/readback");
  EEPROM.setReady(false);
  const auto blockedUpdates = EEPROM.updates().size();
  require(latest.service(0, true) && !latest.persisted() &&
              EEPROM.updates().size() == blockedUpdates,
          "busy EEPROM advanced or published settings durability");
  EEPROM.setReady(true);
  require(latest.service(0, true) && latest.persisted(),
          "ready EEPROM did not finish settings CRC readback");
}

void testSettingsGenerationWrap() {
  EEPROM.fill(0xFF);
  SettingsStore store;
  store.begin(0);
  for (std::uint8_t generation = 0; generation < 20; ++generation) {
    const std::uint16_t period = static_cast<std::uint16_t>(500 + generation);
    store.values().streamPeriodMs = period;
    store.saveNow();
    drainSettings(store);
    SettingsStore reloaded;
    require(reloaded.begin(0) && reloaded.persisted() &&
                reloaded.values().streamPeriodMs == period,
            "modulo-16 settings generation wrap selected the stale bank");
  }
}

void testLearnedRemoteCooperativeRecordsAndClear() {
  EEPROM.fill(0xFF);
  RemoteLearningStore store;
  store.begin();
  require(store.busy() && store.count() == 0,
          "unknown RF layout was not treated as empty while initializing");
  drainRemotes(store);
  require(!EEPROM.updates().empty() &&
              EEPROM.updates().back().address ==
                  EepromLayout::RemoteHeaderAddress &&
              EEPROM.updates().back().value == 0x52,
          "RF header magic was not published after clearing old records");

  EEPROM.clearUpdates();
  std::uint8_t id = 0xFF;
  require(store.learn(0x123456UL, 24, 1, 350, id) && id == 0,
          "learned RF record could not be queued");
  require(store.busy() && !store.ready(),
          "queued RF mutation was exposed as durably queryable");
  LearnedRemote pending{};
  require(store.get(id, pending) && pending.code == 0x123456UL,
          "queued learned RF record was not visible to the live controller");
  // The record CRC is the thirteenth update turn; not-busy is withheld until
  // hardware readiness and a later readback turn.
  for (std::uint8_t turn = 0;
       turn < EepromLayout::RemoteRecordBytes + 1U; ++turn) {
    store.service();
  }
  EEPROM.setReady(false);
  const auto blockedUpdates = EEPROM.updates().size();
  require(store.busy() && store.service() && store.busy() &&
              EEPROM.updates().size() == blockedUpdates,
          "busy EEPROM advanced or published learned-RF durability");
  EEPROM.setReady(true);
  require(store.service() && !store.busy() && store.ready(),
          "ready EEPROM did not finish learned-RF CRC readback");
  const int checksumAddress = EepromLayout::RemoteEntriesAddress +
                              id * EepromLayout::RemoteRecordBytes +
                              EepromLayout::RemoteRecordBytes - 1;
  require(EEPROM.updates().front().address == checksumAddress &&
              EEPROM.updates().back().address == checksumAddress,
          "learned RF CRC was not invalidated first and committed last");

  // Multi/indefinite learning remains active between captures in firmware.
  // Once the first cooperative record is durable, a second distinct code must
  // queue successfully instead of failing because Busy was never drained.
  std::uint8_t secondId = 0xFF;
  require(store.learn(0x654321UL, 24, 1, 350, secondId) && secondId == 1,
          "second code in a multi-learning session was rejected");
  drainRemotes(store);
  LearnedRemote secondDurable{};
  require(store.get(secondId, secondDurable) &&
              secondDurable.code == 0x654321UL,
          "second multi-learning code did not become durable");
  RemoteLearningStore reloaded;
  reloaded.begin();
  LearnedRemote durable{};
  require(!reloaded.busy() && reloaded.get(id, durable) &&
              durable.code == pending.code,
          "committed learned RF record did not reload");

  durable.actionKind = static_cast<std::uint8_t>(RemoteActionKind::Key);
  durable.actionValue = 2;
  durable.behavior = static_cast<std::uint8_t>(RemoteBehavior::Press);
  require(reloaded.replace(durable),
          "learned RF replacement could not be queued");
  for (std::uint8_t turn = 0; turn < 5; ++turn) {
    reloaded.service();
  }
  RemoteLearningStore torn;
  torn.begin();
  require(!torn.busy() && !torn.get(id, durable),
          "power loss during learned RF write exposed a torn record");
  drainRemotes(reloaded);

  require(reloaded.clear() && reloaded.count() == 0,
          "queued RF clear did not become immediately visible");
  reloaded.service(); // Invalidate the store header before clearing entries.
  RemoteLearningStore clearRecovery;
  clearRecovery.begin();
  require(clearRecovery.busy() && clearRecovery.count() == 0,
          "power loss during RF clear revived a partially cleared table");
}

} // namespace

int main() {
  try {
    testSettingsPublishLastAndPolling();
    testSettingsDualBankPowerLossAtEveryWriteIndex();
    testSettingsMidFlightMutationAndReadyGate();
    testSettingsGenerationWrap();
    testLearnedRemoteCooperativeRecordsAndClear();
    std::cout << "firmware_persistence_tests: all checks passed\n";
    return 0;
  } catch (const std::exception &error) {
    std::cerr << "firmware_persistence_tests: " << error.what() << '\n';
    return 1;
  }
}
