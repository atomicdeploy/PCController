#include "RemoteLearningStore.h"
#include "UartProtocol.h"

#include <EEPROM.h>
#include <string.h>

namespace {

constexpr uint16_t StoreMagic = 0x4C52; // "RL"

struct __attribute__((packed)) StoreHeader {
  uint16_t magic;
  uint8_t recordBytes;
  uint8_t capacity;
};

} // namespace

RemoteLearningStore learnedRemotes;

void RemoteLearningStore::begin() {
  StoreHeader header;
  EEPROM.get(HeaderAddress, header);
  if (header.magic == StoreMagic &&
      header.recordBytes == EepromLayout::RemoteRecordBytes &&
      header.capacity == Capacity) {
    return;
  }

  header = {StoreMagic, EepromLayout::RemoteRecordBytes, Capacity};
  EEPROM.put(HeaderAddress, header);
  clear();
}

uint8_t RemoteLearningStore::count() const {
  uint8_t result = 0;
  Record record;
  for (uint8_t id = 0; id < Capacity; ++id) {
    if (readRecord(id, record)) {
      ++result;
    }
  }
  return result;
}

bool RemoteLearningStore::get(uint8_t id, LearnedRemote &remote) const {
  Record record;
  if (!readRecord(id, record)) {
    return false;
  }
  remote = {id,
            record.code,
            record.bits,
            record.protocol,
            record.pulseMicros,
            record.actionKind,
            record.actionValue,
            record.behavior};
  return true;
}

bool RemoteLearningStore::find(uint32_t code, uint8_t bits,
                               uint8_t protocol,
                               LearnedRemote &remote) const {
  for (uint8_t id = 0; id < Capacity; ++id) {
    if (get(id, remote) && remote.code == code &&
        (remote.bits == 0 || remote.bits == bits) &&
        (remote.protocol == 0 || remote.protocol == protocol)) {
      return true;
    }
  }
  return false;
}

bool RemoteLearningStore::learn(uint32_t code, uint8_t bits,
                                uint8_t protocol, uint16_t pulseMicros,
                                uint8_t &id) {
  if (code == 0 || bits == 0 || bits > 32 || protocol == 0) {
    return false;
  }

  Record record;
  uint8_t freeId = Capacity;
  for (uint8_t candidate = 0; candidate < Capacity; ++candidate) {
    if (readRecord(candidate, record)) {
      if (record.code == code && record.bits == bits &&
          record.protocol == protocol) {
        record.pulseMicros = pulseMicros;
        writeRecord(candidate, record);
        id = candidate;
        return true;
      }
    } else if (freeId == Capacity) {
      freeId = candidate;
    }
  }
  if (freeId == Capacity) {
    return false;
  }

  record = {code,
            bits,
            protocol,
            pulseMicros,
            static_cast<uint8_t>(RemoteActionKind::None),
            0,
            static_cast<uint8_t>(RemoteBehavior::Press),
            0};
  writeRecord(freeId, record);
  id = freeId;
  return true;
}

bool RemoteLearningStore::remove(uint8_t id) {
  if (id >= Capacity) {
    return false;
  }
  Record record{};
  writeRecord(id, record);
  return true;
}

void RemoteLearningStore::clear() {
  for (uint8_t id = 0; id < Capacity; ++id) {
    remove(id);
  }
}

bool RemoteLearningStore::replace(const LearnedRemote &remote) {
  if (remote.id >= Capacity || remote.code == 0 || remote.bits == 0 ||
      remote.bits > 32 || remote.protocol == 0 ||
      !validMapping(static_cast<RemoteActionKind>(remote.actionKind),
                    remote.actionValue,
                    static_cast<RemoteBehavior>(remote.behavior))) {
    return false;
  }
  Record record = {remote.code,
                   remote.bits,
                   remote.protocol,
                   remote.pulseMicros,
                   remote.actionKind,
                   remote.actionValue,
                   remote.behavior,
                   0};
  writeRecord(remote.id, record);
  return true;
}

bool RemoteLearningStore::map(uint8_t id, RemoteActionKind kind,
                              uint8_t value, RemoteBehavior behavior) {
  if (!validMapping(kind, value, behavior)) {
    return false;
  }
  Record record;
  if (!readRecord(id, record)) {
    return false;
  }
  record.actionKind = static_cast<uint8_t>(kind);
  record.actionValue = value;
  record.behavior = static_cast<uint8_t>(behavior);
  writeRecord(id, record);
  return true;
}

bool RemoteLearningStore::readRecord(uint8_t id, Record &record) const {
  if (id >= Capacity) {
    return false;
  }
  EEPROM.get(EntriesAddress + id * sizeof(Record), record);
  return record.code != 0 && record.bits != 0 && record.bits <= 32 &&
         record.checksum ==
             ControllerProtocol::UartProtocol::crc8(
                 reinterpret_cast<const uint8_t *>(&record),
                 static_cast<uint8_t>(sizeof(record) - 1));
}

void RemoteLearningStore::writeRecord(uint8_t id, Record &record) {
  record.checksum = ControllerProtocol::UartProtocol::crc8(
      reinterpret_cast<const uint8_t *>(&record),
      static_cast<uint8_t>(sizeof(record) - 1));
  const uint8_t *bytes = reinterpret_cast<const uint8_t *>(&record);
  const int address = EntriesAddress + id * sizeof(Record);
  for (uint8_t index = 0; index < sizeof(Record); ++index) {
    EEPROM.update(address + index, bytes[index]);
  }
}

bool RemoteLearningStore::validMapping(RemoteActionKind kind,
                                       uint8_t value,
                                       RemoteBehavior behavior) {
  static const uint8_t ValueLimits[] = {0, 4, 4, 8, 2, 11};
  const uint8_t kindValue = static_cast<uint8_t>(kind);
  if (kindValue > static_cast<uint8_t>(RemoteActionKind::Pwm) ||
      static_cast<uint8_t>(behavior) >
          static_cast<uint8_t>(RemoteBehavior::Stop)) {
    return false;
  }
  return kindValue == static_cast<uint8_t>(RemoteActionKind::None) ||
         value < ValueLimits[kindValue];
}
