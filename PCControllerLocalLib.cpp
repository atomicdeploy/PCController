// Arduino compiles root translation units but not arbitrary root subfolders.
// Keep the inherited LocalLib layout while building each implementation once.
#include "LocalLib/DallasTemperatureBus.cpp"
#include "LocalLib/I2cLcd.cpp"
#include "LocalLib/Keys.cpp"
#include "LocalLib/SevenSegments.cpp"
#include "LocalLib/ShiftRegisters.cpp"
#include "LocalLib/Tasks.cpp"
#include "LocalLib/TonePlayer.cpp"
