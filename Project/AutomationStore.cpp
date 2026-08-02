#include "AutomationStore.h"

#include <EEPROM.h>
#include <string.h>

#include "UartProtocol.h"

namespace {
constexpr uint16_t AutomationStoreMagic = 0x4155;
constexpr uint8_t AutomationCommitMarker = 0xA5;

bool generationNewer(uint16_t candidate, uint16_t reference) {
  return static_cast<int16_t>(candidate - reference) > 0;
}
} // namespace

AutomationStore boardAutomations;

int AutomationStore::bankAddress(uint8_t bank) {
  return bank == 0 ? EepromLayout::AutomationBankAAddress
                   : EepromLayout::AutomationBankBAddress;
}

int AutomationStore::recordAddress(uint8_t bank, uint8_t id) {
  return bankAddress(bank) + EepromLayout::AutomationHeaderBytes +
         static_cast<int>(id) * EepromLayout::AutomationRecordBytes;
}

void AutomationStore::begin() {
  BankHeader first;
  BankHeader second;
  const bool firstValid = bankValid(0, first);
  const bool secondValid = bankValid(1, second);
  if (firstValid || secondValid) {
    activeBank_ = secondValid &&
                          (!firstValid ||
                           generationNewer(second.generation, first.generation))
                      ? 1
                      : 0;
    generation_ = activeBank_ == 0 ? first.generation : second.generation;
    return;
  }

  activeBank_ = 1;
  generation_ = 0;
  clear();
}

uint8_t AutomationStore::count() const {
  uint8_t result = 0;
  AutomationRecord record;
  for (uint8_t id = 0; id < Capacity; ++id) {
    if (get(id, record)) {
      ++result;
    }
  }
  return result;
}

uint16_t AutomationStore::generation() const { return generation_; }

bool AutomationStore::get(uint8_t id, AutomationRecord &record) const {
  StoredRecord stored;
  if (id >= Capacity || !readStored(activeBank_, id, stored) || empty(stored)) {
    return false;
  }
  decode(id, stored, record);
  return valid(record);
}

bool AutomationStore::put(AutomationRecord &record) {
  if (!valid(record)) {
    return false;
  }
  if (record.id == NewRecord) {
    StoredRecord stored;
    for (uint8_t id = 0; id < Capacity; ++id) {
      if (readStored(activeBank_, id, stored) && empty(stored)) {
        record.id = id;
        break;
      }
    }
  }
  if (record.id >= Capacity) {
    return false;
  }
  return commit(record.id, &record, false);
}

bool AutomationStore::remove(uint8_t id) {
  AutomationRecord existing;
  if (!get(id, existing)) {
    return false;
  }
  return commit(id, nullptr, true);
}

void AutomationStore::clear() { (void)commit(NewRecord, nullptr, false); }

bool AutomationStore::valid(const AutomationRecord &record) {
  if ((record.flags & ~AutomationFlags::Allowed) != 0 ||
      record.options != 0 ||
      record.eventKind < static_cast<uint8_t>(AutomationEventKind::Door) ||
      record.eventKind > static_cast<uint8_t>(AutomationEventKind::Boot) ||
      record.actionKind <
          static_cast<uint8_t>(AutomationActionKind::SafeStop) ||
      record.actionKind >
          static_cast<uint8_t>(AutomationActionKind::HostMacroRequest)) {
    return false;
  }

  switch (static_cast<AutomationEventKind>(record.eventKind)) {
    case AutomationEventKind::Door:
    case AutomationEventKind::Host:
      if (record.eventValue > 1) {
        return false;
      }
      break;
    case AutomationEventKind::Bluetooth:
      if (record.eventValue > 2) {
        return false;
      }
      break;
    case AutomationEventKind::LearnedRf:
      if (record.eventValue >= 20) {
        return false;
      }
      break;
    case AutomationEventKind::Key:
      if ((record.eventValue >> 4) > 3 ||
          (record.eventValue & 0x0F) > 6) {
        return false;
      }
      break;
    case AutomationEventKind::Alert:
      if ((record.eventValue >> 1) < 1 ||
          (record.eventValue >> 1) > 2) {
        return false;
      }
      break;
    case AutomationEventKind::Boot:
      if (record.eventValue != 0) {
        return false;
      }
      break;
    case AutomationEventKind::Relay:
      break;
  }

  const AutomationActionKind action =
      static_cast<AutomationActionKind>(record.actionKind);
  if (record.eventKind == static_cast<uint8_t>(AutomationEventKind::Host) &&
      (action == AutomationActionKind::Relay ||
       action == AutomationActionKind::Pwm)) {
    // A wildcard or disconnected-host rule must never restore an actuator
    // after the lifecycle's unconditional host-loss safe stop.
    return false;
  }

  switch (action) {
    case AutomationActionKind::SafeStop:
      return record.actionTarget == 0 && record.value == 0 &&
             record.extra == 0;
    case AutomationActionKind::MotionStop:
      return (record.actionTarget <= 1 || record.actionTarget == 0xFF) &&
             record.value == 0 && record.extra == 0;
    case AutomationActionKind::Relay:
      return record.actionTarget < 8 && record.value <= 2 &&
             record.extra == 0;
    case AutomationActionKind::Pwm:
      return record.actionTarget < 11 && record.value <= 4095 &&
             record.extra == 0;
    case AutomationActionKind::StatusCue:
      return record.actionTarget >= 1 && record.actionTarget <= 8 &&
             record.value != 0 && record.extra == 0;
    case AutomationActionKind::Buzzer:
      return record.value != 0 && record.extra != 0;
    case AutomationActionKind::RfTransmit:
      return record.actionTarget < 20 && record.value == 0 &&
             record.extra == 0;
    case AutomationActionKind::HostMacroRequest:
      return record.value == 0 && record.extra == 0;
  }
  return false;
}

bool AutomationStore::bankValid(uint8_t bank, BankHeader &header) const {
  EEPROM.get(bankAddress(bank), header);
  if (header.magic != AutomationStoreMagic || header.schema != Schema ||
      header.recordBytes != sizeof(StoredRecord) ||
      header.capacity != Capacity || header.reserved != 0 ||
      header.commit != AutomationCommitMarker ||
      header.checksum != ControllerProtocol::UartProtocol::crc8(
                             reinterpret_cast<const uint8_t *>(&header), 8)) {
    return false;
  }
  StoredRecord record;
  for (uint8_t id = 0; id < Capacity; ++id) {
    EEPROM.get(recordAddress(bank, id), record);
    if (!validStored(record)) {
      return false;
    }
  }
  return true;
}

bool AutomationStore::readStored(uint8_t bank, uint8_t id,
                                 StoredRecord &record) const {
  if (id >= Capacity) {
    return false;
  }
  EEPROM.get(recordAddress(bank, id), record);
  return validStored(record);
}

bool AutomationStore::commit(uint8_t replaceID,
                             const AutomationRecord *replacement,
                             bool removeOnly) {
  const uint8_t destination = static_cast<uint8_t>(activeBank_ ^ 1U);
  const int base = bankAddress(destination);
  EEPROM.update(base + offsetof(BankHeader, commit), 0);

  StoredRecord record;
  for (uint8_t id = 0; id < Capacity; ++id) {
    if (id == replaceID) {
      if (replacement != nullptr) {
        encode(*replacement, record);
      } else {
        memset(&record, 0, sizeof(record));
      }
    } else if (replaceID == NewRecord && !removeOnly) {
      memset(&record, 0, sizeof(record));
    } else if (!readStored(activeBank_, id, record)) {
      return false;
    }
    writeBytes(recordAddress(destination, id), &record, sizeof(record));
  }

  BankHeader header = {AutomationStoreMagic, Schema, sizeof(StoredRecord), Capacity, 0,
                       static_cast<uint16_t>(generation_ + 1), 0, 0};
  header.checksum = ControllerProtocol::UartProtocol::crc8(
      reinterpret_cast<const uint8_t *>(&header), 8);
  writeBytes(base, &header, static_cast<uint8_t>(offsetof(BankHeader, commit)));
  EEPROM.update(base + offsetof(BankHeader, commit), AutomationCommitMarker);
  activeBank_ = destination;
  generation_ = header.generation;
  return true;
}

bool AutomationStore::empty(const StoredRecord &record) {
  const uint8_t *bytes = reinterpret_cast<const uint8_t *>(&record);
  for (uint8_t index = 0; index < sizeof(record); ++index) {
    if (bytes[index] != 0) {
      return false;
    }
  }
  return true;
}

bool AutomationStore::validStored(const StoredRecord &record) {
  if (record.checksum != ControllerProtocol::UartProtocol::crc8(
                             reinterpret_cast<const uint8_t *>(&record),
                             sizeof(record) - 1)) {
    return false;
  }
  if (empty(record)) {
    return true;
  }
  AutomationRecord decoded;
  decode(0, record, decoded);
  return valid(decoded);
}

void AutomationStore::encode(const AutomationRecord &source,
                             StoredRecord &target) {
  memcpy(&target, &source.flags, sizeof(target) - 1);
  target.checksum = ControllerProtocol::UartProtocol::crc8(
      reinterpret_cast<const uint8_t *>(&target), sizeof(target) - 1);
}

void AutomationStore::decode(uint8_t id, const StoredRecord &source,
                             AutomationRecord &target) {
  target.id = id;
  memcpy(&target.flags, &source, sizeof(source) - 1);
}

void AutomationStore::writeBytes(int address, const void *value,
                                 uint8_t length) {
  const uint8_t *bytes = reinterpret_cast<const uint8_t *>(value);
  for (uint8_t index = 0; index < length; ++index) {
    EEPROM.update(address + index, bytes[index]);
  }
}

AutomationExecutor::AutomationExecutor(AutomationStore &store,
                                       AutomationActionHandler handler,
                                       void *context)
    : store_(store), handler_(handler), context_(context) {}

uint8_t AutomationExecutor::dispatch(AutomationEventKind kind, uint8_t value,
                                     uint32_t now) {
  if (executing_ || handler_ == nullptr) {
    return 0;
  }
  if (static_cast<uint32_t>(now - windowStartedAt_) >= 1000UL) {
    windowStartedAt_ = now;
    windowActions_ = 0;
  }
  executing_ = true;
  uint8_t executed = 0;
  uint8_t attempted = 0;
  AutomationRecord record;
  for (uint8_t id = 0; id < AutomationStore::Capacity &&
                       attempted < MaximumActionsPerEvent &&
                       windowActions_ < MaximumActionsPerSecond;
       ++id) {
    if (!store_.get(id, record) ||
        (record.flags & AutomationFlags::Enabled) == 0 ||
        record.eventKind != static_cast<uint8_t>(kind) ||
        (record.eventMask != 0 &&
         (value & record.eventMask) !=
             (record.eventValue & record.eventMask))) {
      continue;
    }
    ++attempted;
    ++windowActions_;
    if (handler_(record, context_)) {
      ++executed;
    }
  }
  executing_ = false;
  return executed;
}

void AutomationExecutor::reset() {
  executing_ = false;
  windowStartedAt_ = 0;
  windowActions_ = 0;
}
