#pragma once

#include <Arduino.h>

#include "EepromLayout.h"

enum class RemoteActionKind : uint8_t {
  None = 0,
  Key = 1,
  Menu = 2,
  Relay = 3,
  Side = 4,
  Pwm = 5,
};

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

class RemoteLearningStore {
public:
  static constexpr uint8_t Capacity = EepromLayout::RemoteCapacity;

  void begin();
  uint8_t count() const;
  bool get(uint8_t id, LearnedRemote &remote) const;
  bool find(uint32_t code, uint8_t bits, uint8_t protocol,
            LearnedRemote &remote) const;

  // Updates a known RF code or allocates the first free slot. New entries are
  // deliberately unmapped until the user assigns an action.
  bool learn(uint32_t code, uint8_t bits, uint8_t protocol,
             uint16_t pulseMicros, uint8_t &id);
  bool remove(uint8_t id);
  void clear();
  // Atomically replaces one slot at record granularity. The existing paged
  // list response is the read-back for host-staged reordering/import.
  bool replace(const LearnedRemote &remote);
  bool map(uint8_t id, RemoteActionKind kind, uint8_t value,
           RemoteBehavior behavior);

private:
  static constexpr int HeaderAddress = EepromLayout::RemoteHeaderAddress;
  static constexpr int EntriesAddress = EepromLayout::RemoteEntriesAddress;

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
  void writeRecord(uint8_t id, Record &record);
  static bool validMapping(RemoteActionKind kind, uint8_t value,
                           RemoteBehavior behavior);
};

extern RemoteLearningStore learnedRemotes;
