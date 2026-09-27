#include "RemoteLearningStore.h"
#include "UartProtocol.h"
#include "EepromAccess.h"

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
  pendingId_ = Capacity;
  writeIndex_ = 0;
  clearId_ = 0;
  headerWriteIndex_ = 0;
  writePending_ = false;
  clearing_ = false;
  headerPending_ = false;
  headerInvalidated_ = false;
  StoreHeader header;
  EEPROM.get(HeaderAddress, header);
  if (header.magic == StoreMagic &&
      header.recordBytes == EepromLayout::RemoteRecordBytes &&
      header.capacity == Capacity) {
    return;
  }

  // Treat an unknown alpha layout as empty immediately, but clear it one byte
  // per controller turn. Only after every old entry is invalid does service()
  // publish the new header magic, so a reset cannot revive stale records.
  clearing_ = true;
  headerPending_ = true;
}

bool RemoteLearningStore::service() {
  if (!controllerEepromReady()) {
    return busy();
  }
  if (headerPending_ && !headerInvalidated_) {
    return serviceHeaderWrite();
  }
  if (writePending_) {
    return serviceRecordWrite();
  }
  if (clearing_) {
    if (clearId_ < Capacity) {
      const Record empty{};
      queueRecord(clearId_, empty);
      return serviceRecordWrite();
    }
    clearing_ = false;
  }
  return headerPending_ && serviceHeaderWrite();
}

bool RemoteLearningStore::busy() const {
  return writePending_ || clearing_ || headerPending_;
}

bool RemoteLearningStore::ready() const {
  // Query/list and RF-dispatch callers need a durable table, not merely an
  // idle EEPE bit between two bytes of a larger cooperative transaction.
  return !busy() && controllerEepromReady();
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
  if (busy() || code == 0 || bits == 0 || bits > 32 || protocol == 0) {
    return false;
  }

  Record record;
  uint8_t freeId = Capacity;
  for (uint8_t candidate = 0; candidate < Capacity; ++candidate) {
    if (readRecord(candidate, record)) {
      if (record.code == code && record.bits == bits &&
          record.protocol == protocol) {
        record.pulseMicros = pulseMicros;
        if (!queueRecord(candidate, record)) {
          return false;
        }
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
  if (!queueRecord(freeId, record)) {
    return false;
  }
  id = freeId;
  return true;
}

bool RemoteLearningStore::remove(uint8_t id) {
  if (busy() || id >= Capacity) {
    return false;
  }
  Record record{};
  return queueRecord(id, record);
}

bool RemoteLearningStore::clear() {
  if (busy()) {
    return false;
  }
  clearId_ = 0;
  clearing_ = true;
  headerPending_ = true;
  headerInvalidated_ = false;
  headerWriteIndex_ = 0;
  return true;
}

bool RemoteLearningStore::replace(const LearnedRemote &remote) {
  if (busy() || remote.id >= Capacity || remote.code == 0 || remote.bits == 0 ||
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
  return queueRecord(remote.id, record);
}

bool RemoteLearningStore::readRecord(uint8_t id, Record &record) const {
  if (id >= Capacity || clearing_ || headerPending_) {
    return false;
  }
  if (writePending_ && id == pendingId_) {
    record = pendingRecord_;
  } else {
    EEPROM.get(EntriesAddress + static_cast<int>(id) *
                                    static_cast<int>(sizeof(Record)),
               record);
  }
  return record.code != 0 && record.bits != 0 && record.bits <= 32 &&
         record.checksum ==
             ControllerProtocol::UartProtocol::crc8(
                 reinterpret_cast<const uint8_t *>(&record),
                 static_cast<uint8_t>(sizeof(record) - 1));
}

bool RemoteLearningStore::queueRecord(uint8_t id, const Record &record) {
  if (writePending_ || id >= Capacity) {
    return false;
  }
  pendingRecord_ = record;
  pendingRecord_.checksum = ControllerProtocol::UartProtocol::crc8(
      reinterpret_cast<const uint8_t *>(&pendingRecord_),
      static_cast<uint8_t>(sizeof(pendingRecord_) - 1));
  pendingId_ = id;
  writeIndex_ = 0;
  writePending_ = true;
  return true;
}

bool RemoteLearningStore::serviceRecordWrite() {
  constexpr uint8_t checksumIndex = sizeof(Record) - 1U;
  const int address = EntriesAddress +
                      static_cast<int>(pendingId_) *
                          static_cast<int>(sizeof(Record));
  const uint8_t *bytes =
      reinterpret_cast<const uint8_t *>(&pendingRecord_);
  if (writeIndex_ == 0) {
    uint8_t invalid = static_cast<uint8_t>(bytes[checksumIndex] + 1U);
    if (invalid == EEPROM.read(address + checksumIndex)) {
      ++invalid;
    }
    EEPROM.update(address + checksumIndex, invalid);
  } else if (writeIndex_ <= checksumIndex) {
    const uint8_t dataIndex = static_cast<uint8_t>(writeIndex_ - 1U);
    EEPROM.update(address + dataIndex, bytes[dataIndex]);
  } else if (writeIndex_ == static_cast<uint8_t>(checksumIndex + 1U)) {
    EEPROM.update(address + checksumIndex, bytes[checksumIndex]);
  } else {
    if (EEPROM.read(address + checksumIndex) != bytes[checksumIndex]) {
      EEPROM.update(address + checksumIndex, bytes[checksumIndex]);
      return true;
    }
    writePending_ = false;
    pendingId_ = Capacity;
    writeIndex_ = 0;
    if (clearing_) {
      ++clearId_;
    }
    return true;
  }
  ++writeIndex_;
  return true;
}

bool RemoteLearningStore::serviceHeaderWrite() {
  const uint8_t magicLow = static_cast<uint8_t>(StoreMagic);
  const uint8_t magicHigh = static_cast<uint8_t>(StoreMagic >> 8U);
  switch (headerWriteIndex_) {
    case 0: {
      uint8_t invalid = static_cast<uint8_t>(magicLow + 1U);
      if (invalid == EEPROM.read(HeaderAddress)) {
        ++invalid;
      }
      EEPROM.update(HeaderAddress, invalid);
      headerInvalidated_ = true;
      ++headerWriteIndex_;
      break;
    }
    case 1:
      EEPROM.update(HeaderAddress + 2, EepromLayout::RemoteRecordBytes);
      ++headerWriteIndex_;
      break;
    case 2:
      EEPROM.update(HeaderAddress + 3, Capacity);
      ++headerWriteIndex_;
      break;
    case 3:
      EEPROM.update(HeaderAddress + 1, magicHigh);
      ++headerWriteIndex_;
      break;
    case 4:
      // Low magic is the publication point after metadata and all entries.
      EEPROM.update(HeaderAddress, magicLow);
      ++headerWriteIndex_;
      break;
    default:
      if (EEPROM.read(HeaderAddress) != magicLow) {
        EEPROM.update(HeaderAddress, magicLow);
        break;
      }
      headerWriteIndex_ = 0;
      headerPending_ = false;
      headerInvalidated_ = false;
      break;
  }
  return true;
}

bool RemoteLearningStore::validMapping(RemoteActionKind kind,
                                       uint8_t value,
                                       RemoteBehavior behavior) {
  // Exclusive limits indexed by RemoteActionKind: None, Key, Menu, Relay,
  // Side, and Pwm. None is accepted separately below and does not use its zero.
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
