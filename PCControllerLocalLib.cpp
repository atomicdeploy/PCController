// Arduino compiles root translation units but not arbitrary root subfolders.
// Keep the LocalLib domain layout while building each implementation once.
#include "ProjectConfig.h"

#if PCCONTROLLER_ENABLE_DS18B20
#include "LocalLib/DallasTemperatureBus.cpp"
#endif
#include "LocalLib/I2cLcd.cpp"
#include "LocalLib/Keys.cpp"
#include "LocalLib/SevenSegments.cpp"
#include "LocalLib/ShiftRegisters.cpp"
#if PCCONTROLLER_ENABLE_TASK_SCHEDULER
#include "LocalLib/Tasks.cpp"
#endif
#include "LocalLib/TonePlayer.cpp"
