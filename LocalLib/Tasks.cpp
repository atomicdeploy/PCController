#include "Tasks.h"

#if PCCONTROLLER_ENABLE_TASK_SCHEDULER
Tasks taskManager;
#endif

Tasks::Tasks() {
  clear();
}

void Tasks::update(uint32_t now) {
  if (itemCount_ == 0) {
    return;
  }

  for (uint8_t i = 0; i < MAX_TASKS; ++i) {
    TaskItem &task = tasks_[i];
    if (!task.active || static_cast<int32_t>(now - task.dueAt) < 0) {
      continue;
    }

    const TaskCallback callback = task.callback;
    void *const context = task.context;
    task.active = false;
    task.callback = nullptr;
    task.context = nullptr;
    --itemCount_;

    if (callback != nullptr) {
      callback(context);
    }
  }
}

#if PCCONTROLLER_ENABLE_TASK_SCHEDULER
void serviceTasks() { taskManager.update(); }
#endif

int8_t Tasks::addTask(uint32_t delayMs, TaskCallback callback,
                      void *context) {
  if (callback == nullptr || itemCount_ >= MAX_TASKS) {
    return -1;
  }

  for (uint8_t i = 0; i < MAX_TASKS; ++i) {
    if (tasks_[i].active) {
      continue;
    }
    tasks_[i].dueAt = millis() + delayMs;
    tasks_[i].callback = callback;
    tasks_[i].context = context;
    tasks_[i].active = true;
    ++itemCount_;
    return static_cast<int8_t>(i);
  }

  return -1;
}

void Tasks::cancelTask(int8_t taskIndex) {
  if (taskIndex < 0 || taskIndex >= static_cast<int8_t>(MAX_TASKS) ||
      !tasks_[taskIndex].active) {
    return;
  }

  tasks_[taskIndex].active = false;
  tasks_[taskIndex].callback = nullptr;
  tasks_[taskIndex].context = nullptr;
  --itemCount_;
}

void Tasks::clear() {
  for (uint8_t i = 0; i < MAX_TASKS; ++i) {
    tasks_[i].dueAt = 0;
    tasks_[i].callback = nullptr;
    tasks_[i].context = nullptr;
    tasks_[i].active = false;
  }
  itemCount_ = 0;
}

uint8_t Tasks::count() const { return itemCount_; }
