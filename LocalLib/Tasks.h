#pragma once

#include <Arduino.h>

using TaskCallback = void (*)(void *context);

// Tiny fixed-capacity one-shot scheduler retained for inherited project logic.
class Tasks {
public:
  Tasks();

  // Runs every due callback once using rollover-safe millisecond deadlines.
  void update(uint32_t now = millis());
  // Returns a reusable slot index, or -1 when all eight slots are active.
  int8_t addTask(uint32_t delayMs, TaskCallback callback,
                 void *context = nullptr);
  void cancelTask(int8_t taskIndex);
  void clear();
  uint8_t count() const;

private:
  // One scheduled callback and its caller-owned opaque context.
  struct TaskItem {
    uint32_t dueAt;
    TaskCallback callback;
    void *context;
    bool active;
  };

  static constexpr uint8_t MAX_TASKS = 8;
  TaskItem tasks_[MAX_TASKS];
  uint8_t itemCount_ = 0;
};

extern Tasks taskManager;

void serviceTasks();
