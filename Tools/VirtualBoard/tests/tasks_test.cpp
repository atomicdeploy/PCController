#include <cstdint>
#include <cstdlib>
#include <iostream>

#include "LocalLib/Tasks.h"

namespace {
void increment(void *context) {
  ++*static_cast<std::uint8_t *>(context);
}

void require(bool condition, const char *message) {
  if (!condition) {
    std::cerr << "tasks test failed: " << message << '\n';
    std::exit(1);
  }
}
} // namespace

int main() {
  Tasks tasks;
  std::uint8_t calls = 0;

  require(tasks.addTask(1000, 25, increment, &calls) >= 0,
          "could not schedule task from caller snapshot");
  tasks.update(1024);
  require(calls == 0, "task ran before its caller-derived deadline");
  tasks.update(1025);
  require(calls == 1 && tasks.count() == 0,
          "task did not run exactly once at its deadline");

  require(tasks.addTask(0xFFFFFFF0UL, 32, increment, &calls) >= 0,
          "could not schedule rollover task");
  tasks.update(0x0000000FUL);
  require(calls == 1, "rollover task ran one millisecond early");
  tasks.update(0x00000010UL);
  require(calls == 2, "rollover task missed its wrapped deadline");

  const int8_t cancelled = tasks.addTask(2000, 10, increment, &calls);
  require(cancelled >= 0, "could not schedule cancellable task");
  tasks.cancelTask(cancelled);
  tasks.update(2010);
  require(calls == 2 && tasks.count() == 0, "cancelled task still ran");

  std::cout << "tasks caller-clock and rollover checks passed\n";
  return 0;
}
