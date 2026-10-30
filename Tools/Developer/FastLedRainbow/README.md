# FastLED rainbow hardware test

Standalone LED test for the MiniCore ATmega328P PCController board:
D6/PD6, 100 WS2811 pixels, BRG color order, brightness 64/255, animated rainbow.
The active-low 74HC595 output enable on D8/PB0 is held disabled. Other controller
features are absent while this sketch is installed. This does not modify EEPROM.
Serial at 115200 baud reports startup configuration and frame count every two seconds.
Frame reports establish firmware execution, not physical LED output.

Requires MiniCore and FastLED (verified with MiniCore 3.1.3, FastLED 3.10.5).
Compile and upload using Arduino CLI, replacing COM3 with the actual board port:

```powershell
arduino-cli compile --fqbn MiniCore:avr:328:clock=16MHz_external,variant=modelP,bootloader=uart0 --build-path work/rainbow-build Tools/Developer/FastLedRainbow
arduino-cli upload --fqbn MiniCore:avr:328:clock=16MHz_external,variant=modelP,bootloader=uart0 --port COM3 --input-dir work/rainbow-build Tools/Developer/FastLedRainbow
```

This replaces the controller application. Preserve a board-local flash/EEPROM
backup before uploading if the installed firmware must be recoverable. Keep
those device backups private. Restore normal PCController firmware when done.
A WS2812B strip instead needs WS2812B and GRB in the addLeds template.
