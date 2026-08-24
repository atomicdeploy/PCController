// Project-layer implementation aggregator for the root-level Project folder.
#include "Project/AddressableLeds.cpp"
#include "Project/BootMelody.cpp"
#include "Project/CompactI2c.cpp"
#include "Project/ControllerEvents.cpp"
#include "Project/FeedbackMelodies.cpp"
#include "Project/IlluminationController.cpp"
#if PCCONTROLLER_ENABLE_INA219
#include "Project/Ina219Sensor.cpp"
#endif
#include "Project/MacroQueue.cpp"
#if PCCONTROLLER_ENABLE_PCA9685
#include "Project/PwmController.cpp"
#include "Project/PwmExpanderDriver.cpp"
#endif
#include "Project/RelayController.cpp"
#include "Project/RemoteLearningStore.cpp"
#include "Project/ResetTelemetry.cpp"
#include "Project/SafeResetController.cpp"
#include "Project/SettingsStore.cpp"
#include "Project/StatusLedController.cpp"
#include "Project/SystemInputs.cpp"
#include "Project/UartProtocol.cpp"
