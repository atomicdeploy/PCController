// Arduino compiles root translation units but not arbitrary root subfolders.
// Keep the LocalLib domain layout while building each implementation once.
#include "ProjectConfig.h"

#include "LocalLib/DallasTemperatureBus.cpp"
#include "LocalLib/I2cLcd.cpp"
#include "LocalLib/Keys.cpp"
#include "LocalLib/SevenSegments.cpp"
#include "LocalLib/ShiftRegisters.cpp"
#if PCCONTROLLER_ENABLE_TASK_SCHEDULER
#include "LocalLib/Tasks.cpp"
#endif
#include "LocalLib/TonePlayer.cpp"
