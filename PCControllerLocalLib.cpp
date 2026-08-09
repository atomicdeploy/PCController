// Arduino compiles root translation units but not arbitrary root subfolders.
// Keep the LocalLib domain layout while building each implementation once.
#if PCCONTROLLER_ENABLE_DS18B20
#include "LocalLib/DallasTemperatureBus.cpp"
#endif
#include "LocalLib/I2cLcd.cpp"
#include "LocalLib/Keys.cpp"
#include "LocalLib/SevenSegments.cpp"
#include "LocalLib/ShiftRegisters.cpp"
#include "LocalLib/Tasks.cpp"
#include "LocalLib/TonePlayer.cpp"
