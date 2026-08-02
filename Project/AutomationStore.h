#pragma once

#include <Arduino.h>

#include "EepromLayout.h"

enum class AutomationEventKind : uint8_t {
  Door = 1,
  Bluetooth = 2,
  Host = 3,
  Relay = 4,
  LearnedRf = 5,
  Key = 6,
  Alert = 7,
  Boot = 8,
};

enum class AutomationActionKind : uint8_t {
  SafeStop = 1,
  MotionStop = 2,
  Relay = 3,
  Pwm = 4,
  StatusCue = 5,
  Buzzer = 6,
  RfTransmit = 7,
  HostMacroRequest = 8,
};

namespace AutomationFlags {
constexpr uint8_t Enabled = 1U << 0;
constexpr uint8_t Allowed = Enabled;
} // namespace AutomationFlags

// AutomationRecord is the stable twelve-byte UART representation. EEPROM
// replaces the slot ID with a per-record CRC-8, retaining eleven semantic
// bytes per rule.
#if defined(_MSC_VER)
#pragma pack(push, 1)
struct AutomationRecord {
#else
struct __attribute__((packed)) AutomationRecord {
#endif
  uint8_t id;
  uint8_t flags;
  uint8_t eventKind;
  uint8_t eventValue;
  uint8_t eventMask;
  uint8_t actionKind;
  uint8_t actionTarget;
  uint16_t value;
  uint16_t extra;
  uint8_t options;
};
#if defined(_MSC_VER)
#pragma pack(pop)
#endif
static_assert(sizeof(AutomationRecord) == 12,
              "automation UART record must remain twelve bytes");

// AutomationStore owns the compact dual-bank table. Every mutation is written
// to the inactive bank and its commit marker is published last, so torn writes
// leave the previous generation readable without a migration chain.
class AutomationStore {
public:
  static constexpr uint8_t Schema = 1;
  static constexpr uint8_t Capacity = EepromLayout::AutomationCapacity;
  static constexpr uint8_t NewRecord = 0xFF;

  void begin();
  uint8_t count() const;
  uint16_t generation() const;
  bool get(uint8_t id, AutomationRecord &record) const;
  bool put(AutomationRecord &record);
  bool remove(uint8_t id);
  void clear();

  static bool valid(const AutomationRecord &record);

private:
#if defined(_MSC_VER)
#pragma pack(push, 1)
  struct StoredRecord {
#else
  struct __attribute__((packed)) StoredRecord {
#endif
    uint8_t flags;
    uint8_t eventKind;
    uint8_t eventValue;
    uint8_t eventMask;
    uint8_t actionKind;
    uint8_t actionTarget;
    uint16_t value;
    uint16_t extra;
    uint8_t options;
    uint8_t checksum;
  };

  struct BankHeader {
    uint16_t magic;
    uint8_t schema;
    uint8_t recordBytes;
    uint8_t capacity;
    uint8_t reserved;
    uint16_t generation;
    uint8_t checksum;
    uint8_t commit;
  };
#if defined(_MSC_VER)
#pragma pack(pop)
#endif

  static_assert(sizeof(StoredRecord) == EepromLayout::AutomationRecordBytes,
                "automation EEPROM record layout changed");
  static_assert(sizeof(BankHeader) == EepromLayout::AutomationHeaderBytes,
                "automation EEPROM bank header layout changed");

  static int bankAddress(uint8_t bank);
  static int recordAddress(uint8_t bank, uint8_t id);
  bool bankValid(uint8_t bank, BankHeader &header) const;
  bool readStored(uint8_t bank, uint8_t id, StoredRecord &record) const;
  bool commit(uint8_t replaceID, const AutomationRecord *replacement,
              bool removeOnly);
  static bool empty(const StoredRecord &record);
  static bool validStored(const StoredRecord &record);
  static void encode(const AutomationRecord &source, StoredRecord &target);
  static void decode(uint8_t id, const StoredRecord &source,
                     AutomationRecord &target);
  static void writeBytes(int address, const void *value, uint8_t length);

  uint16_t generation_ = 0;
  uint8_t activeBank_ = 0;
};

using AutomationActionHandler =
    bool (*)(const AutomationRecord &record, void *context);

// AutomationExecutor scans slots in stable ascending order and applies strict
// per-event and per-second action budgets. Its recursion guard prevents an
// action callback from re-entering the same engine.
class AutomationExecutor {
public:
  static constexpr uint8_t MaximumActionsPerEvent = 4;
  static constexpr uint8_t MaximumActionsPerSecond = 8;

  AutomationExecutor(AutomationStore &store, AutomationActionHandler handler,
                     void *context = nullptr);
  uint8_t dispatch(AutomationEventKind kind, uint8_t value, uint32_t now);
  void reset();

private:
  AutomationStore &store_;
  AutomationActionHandler handler_;
  void *context_;
  uint32_t windowStartedAt_ = 0;
  uint8_t windowActions_ = 0;
  bool executing_ = false;
};

extern AutomationStore boardAutomations;
