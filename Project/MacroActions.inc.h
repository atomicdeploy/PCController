// name, fixed board-capture payload length (0xFF = playback-only/variable)
//
// This deliberately remains a tiny X-macro data source.  Firmware capture,
// playback validation, and later host/VirtualBoard generators consume the
// same allow-list instead of growing parallel controller definitions.
PCCONTROLLER_MACRO_ACTION(Buzzer, 4)
PCCONTROLLER_MACRO_ACTION(PwmSet, 3)
PCCONTROLLER_MACRO_ACTION(PwmAllOff, 0)
PCCONTROLLER_MACRO_ACTION(StatusRgb, 4)
PCCONTROLLER_MACRO_ACTION(StatusEffect, 0xFF)
PCCONTROLLER_MACRO_ACTION(AddressableLed, 5)
PCCONTROLLER_MACRO_ACTION(RadioTransmit, 8)
PCCONTROLLER_MACRO_ACTION(MenuAction, 1)
PCCONTROLLER_MACRO_ACTION(RelaySet, 2)
PCCONTROLLER_MACRO_ACTION(RelaySide, 2)
PCCONTROLLER_MACRO_ACTION(RelayAllOff, 0)
PCCONTROLLER_MACRO_ACTION(MenuSetPage, 1)
PCCONTROLLER_MACRO_ACTION(DisplayText, 0xFF)
PCCONTROLLER_MACRO_ACTION(RemoteKeyGesture, 2)
