#pragma once

#include <Arduino.h>

using TaskCallback = void (*)(void *context);

class Tasks {
public:
  Tasks();

  void update(uint32_t now = millis());
  int8_t addTask(uint32_t delayMs, TaskCallback callback,
                 void *context = nullptr);
  void cancelTask(int8_t taskIndex);
  void clear();
  uint8_t count() const;

private:
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
