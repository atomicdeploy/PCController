#include <Arduino.h>
#include <avr/wdt.h>

#include "ProjectConfig.h"

#include <RCSwitch.h>

#include "LocalLib/BoardPins.h"
#include "LocalLib/DallasTemperatureBus.h"
#include "LocalLib/Keys.h"
#include "LocalLib/ModeManager.h"
#include "LocalLib/SevenSegments.h"
#include "LocalLib/ShiftRegisters.h"
#include "LocalLib/Tasks.h"
#include "LocalLib/TonePlayer.h"
#include "Project/AddressableLeds.h"
#include "Project/BootMelody.h"
#if PCCONTROLLER_ENABLE_EEPROM_BOOT_OPCODES
#include "Project/BootOpcodeSequence.h"
#endif
#include "Project/ControllerEvents.h"
#include "Project/CompactI2c.h"
#include "Project/FrontPanelModel.h"
#include "Project/IlluminationController.h"
#include "Project/Ina219Sensor.h"
#include "Project/MacroQueue.h"
#include "Project/PwmController.h"
#include "Project/PwmExpanderDriver.h"
#include "Project/RelayController.h"
#include "Project/RemoteLearningStore.h"
#include "Project/ResetTelemetry.h"
#include "Project/SafeResetController.h"
#include "Project/SettingsStore.h"
#include "Project/StatusLedController.h"
#include "Project/SystemInputs.h"
#include "Project/TemperatureRoles.h"
#include "Project/TransitionMath.h"
#include "Project/UartProtocol.h"
#include "LocalLib/I2cLcd.h"

// Domain implementation fragments remain in this single translation unit so
// AVR LTO, stack use, and byte-tight layout stay equivalent to the proven build.
#include "Project/Firmware/ControllerConfiguration.inc.h"
#include "Project/Firmware/ControllerContext.inc.h"
#include "Project/Firmware/ControllerUtilities.inc.h"
#include "Project/Firmware/RadioRuntime.inc.h"
#include "Project/Firmware/SensorRuntime.inc.h"
#include "Project/Firmware/FrontPanelRuntime.inc.h"
#include "Project/Firmware/ProtocolRuntime.inc.h"
#include "Project/Firmware/LifecycleRuntime.inc.h"

// Arduino entry points intentionally expose only the high-level lifecycle.
void setup() { initializeController(); }

void loop() { serviceController(); }
