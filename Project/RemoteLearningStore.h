#pragma once

#include <Arduino.h>

#include "EepromLayout.h"

// RemoteActionKind selects the domain controlled by a learned RF record.
enum class RemoteActionKind : uint8_t {
  None = 0,
  Key = 1,
  Menu = 2,
  Relay = 3,
  Side = 4,
  Pwm = 5,
};

// RemoteBehavior selects press, toggle, momentary, or motion semantics.
enum class RemoteBehavior : uint8_t {
  Press = 0,
  Toggle = 1,
  Momentary = 2,
  Up = 3,
  Down = 4,
  Stop = 5,
};

// This packed layout is also the 12-byte native-UART list entry.
struct __attribute__((packed)) LearnedRemote {
  uint8_t id;
  uint32_t code;
  uint8_t bits;
  uint8_t protocol;
  uint16_t pulseMicros;
  uint8_t actionKind;
  uint8_t actionValue;
  uint8_t behavior;
};
static_assert(sizeof(LearnedRemote) == 12,
              "Native learned-remote entry must remain 12 bytes");

// RemoteLearningStore validates and persists the fixed-capacity learned RF table.
class RemoteLearningStore {
public:
  static constexpr uint8_t Capacity = EepromLayout::RemoteCapacity;

  void begin();
  // Advances at most one EEPROM byte. Record CRCs and the store header magic
  // are published last so reset/power loss can only expose complete records.
  bool service();
  bool busy() const;
  // True only when no cooperative mutation remains and EEPROM can be read.
  // Hosts poll list/status until this durability boundary after mutation ACK.
  bool ready() const;
  uint8_t count() const;
  bool get(uint8_t id, LearnedRemote &remote) const;
  bool find(uint32_t code, uint8_t bits, uint8_t protocol,
            LearnedRemote &remote) const;

  // Updates a known RF code or allocates the first free slot. New entries are
  // deliberately unmapped until the user assigns an action.
  bool learn(uint32_t code, uint8_t bits, uint8_t protocol,
             uint16_t pulseMicros, uint8_t &id);
  bool remove(uint8_t id);
  bool clear();
  // Atomically replaces one slot at record granularity. The existing paged
  // list response is the read-back for host-staged reordering/import.
  bool replace(const LearnedRemote &remote);
private:
  static constexpr int HeaderAddress = EepromLayout::RemoteHeaderAddress;
  static constexpr int EntriesAddress = EepromLayout::RemoteEntriesAddress;

  // Record adds validity and checksum bytes around one learned entry.
  struct __attribute__((packed)) Record {
    uint32_t code;
    uint8_t bits;
    uint8_t protocol;
    uint16_t pulseMicros;
    uint8_t actionKind;
    uint8_t actionValue;
    uint8_t behavior;
    uint8_t checksum;
  };

  static_assert(sizeof(Record) == EepromLayout::RemoteRecordBytes,
                "RF record layout changed");
  static_assert(EntriesAddress + Capacity * sizeof(Record) <=
                    EepromLayout::ResetJournalAddress,
                "RF records overlap reset journal");

  bool readRecord(uint8_t id, Record &record) const;
  bool queueRecord(uint8_t id, const Record &record);
  bool serviceRecordWrite();
  bool serviceHeaderWrite();
  static bool validMapping(RemoteActionKind kind, uint8_t value,
                           RemoteBehavior behavior);

  Record pendingRecord_{};
  uint8_t pendingId_ = Capacity;
  uint8_t writeIndex_ = 0;
  uint8_t clearId_ = 0;
  uint8_t headerWriteIndex_ = 0;
  bool writePending_ = false;
  bool clearing_ = false;
  bool headerPending_ = false;
  bool headerInvalidated_ = false;
};

// learnedRemotes is the single MCU-owned learned RF table.
extern RemoteLearningStore learnedRemotes;
