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
#if PCCONTROLLER_ENABLE_TASK_SCHEDULER
#include "LocalLib/Tasks.h"
#endif
#include "LocalLib/TonePlayer.h"
#include "Project/AddressableLeds.h"
#if PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES
#include "Project/AudioCues.h"
#endif
#include "Project/BootMelody.h"
#include "Project/ControllerEvents.h"
#include "Project/CompactI2c.h"
#if PCCONTROLLER_ENABLE_EEPROM_MENU_LABELS
#include "Project/EepromMenuLabels.h"
#endif
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
#include "Project/Runtime/ControllerConfiguration.inc.h"
#include "Project/Runtime/ControllerContext.inc.h"
#include "Project/Runtime/ControllerUtilities.inc.h"
#include "Project/Runtime/RadioRuntime.inc.h"
#include "Project/Runtime/SensorRuntime.inc.h"
#include "Project/Runtime/FrontPanelRuntime.inc.h"
#include "Project/Runtime/ProtocolRuntime.inc.h"
#include "Project/Runtime/LifecycleRuntime.inc.h"

// Arduino entry points intentionally expose only the high-level lifecycle.
void setup() { initializeController(); }

void loop() { serviceController(); }
