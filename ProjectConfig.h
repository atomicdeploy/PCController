#pragma once

// The UART is the primary COBS/opcode application link, not a debug console.
#ifndef PCCONTROLLER_UART_BAUD
#define PCCONTROLLER_UART_BAUD 115200UL
#endif

// The configured board uses 11 WS2811 pixels in BRG order on D6. Set this to 1
// for a WS2812B/GRB strip when the final hardware is confirmed.
#ifndef PCCONTROLLER_USE_WS2812B
#define PCCONTROLLER_USE_WS2812B 0
#endif

// Logical PWM 0 is off and 4095 is fully on. The MOSFET modules are active
// high; change this only if a later output stage is electrically inverted.
#ifndef PCCONTROLLER_PWM_ACTIVE_LOW
#define PCCONTROLLER_PWM_ACTIVE_LOW 0
#endif

// Raw 74HC165 levels for the two monitored inputs. A conventional reed switch
// wired to ground is low while the door magnet is present/closed, so an open
// door is normally read high. BT audio modules commonly sink the indicator
// LED cathode while it is illuminated, so that input defaults active-low.
// Flip either value after electrical verification if the board buffers it
// differently.
#ifndef PCCONTROLLER_DOOR_OPEN_RAW_HIGH
#define PCCONTROLLER_DOOR_OPEN_RAW_HIGH 1
#endif

#ifndef PCCONTROLLER_BT_LED_ON_RAW_HIGH
#define PCCONTROLLER_BT_LED_ON_RAW_HIGH 0
#endif

// The host owns the 16x2 PCF8574 LCD through the generic I2C opcode. Keeping
// the full HD44780 renderer off the 328P recovers flash for the timed macro
// queue; the host still scans 0x27/0x3F and mirrors all text/front-panel state.
#ifndef PCCONTROLLER_ENABLE_I2C_LCD
#define PCCONTROLLER_ENABLE_I2C_LCD 0
#endif

// The current motion/macro boards intentionally omit these optional I2C and
// OneWire modules. Keeping them as independent build features lets a later
// full-peripheral FQBN/profile restore the exact drivers without source edits,
// while the default deployment image spends its 328P flash on input latency,
// relay/RF safety, and timed macro capture.
#ifndef PCCONTROLLER_ENABLE_INA219
#define PCCONTROLLER_ENABLE_INA219 0
#endif

#ifndef PCCONTROLLER_ENABLE_DS18B20
#define PCCONTROLLER_ENABLE_DS18B20 0
#endif

#ifndef PCCONTROLLER_ENABLE_PCA9685
#define PCCONTROLLER_ENABLE_PCA9685 0
#endif

// The production front panel has one input/output page. In the normal build it
// is the four-key motion surface (Side A up/down, Side B up/down). A diagnostic
// image may compile the same page as a key identifier without adding a second
// page or carrying its flash cost in the normal image.
#ifndef PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS
#define PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS 0
#endif

// Capture ordinary accepted actions into the same bounded byte ring used for
// MCU-timed playback. Disable only for an exceptionally constrained profile.
#ifndef PCCONTROLLER_ENABLE_MACRO_CAPTURE
#define PCCONTROLLER_ENABLE_MACRO_CAPTURE 1
#endif

// A validation/development image can make every audio path electrically quiet
// without mutating the user's EEPROM Silent preference. Blank/corrupt EEPROM
// also starts silent by default so a newly programmed board cannot surprise a
// sleeping household before the host provisions it.
#ifndef PCCONTROLLER_FORCE_SILENT
#define PCCONTROLLER_FORCE_SILENT 0
#endif

#ifndef PCCONTROLLER_BLANK_EEPROM_SILENT
#define PCCONTROLLER_BLANK_EEPROM_SILENT 1
#endif

#if (PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS != 0) && \
    (PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS != 1)
#error "PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_MACRO_CAPTURE != 0) && \
    (PCCONTROLLER_ENABLE_MACRO_CAPTURE != 1)
#error "PCCONTROLLER_ENABLE_MACRO_CAPTURE must be 0 or 1"
#endif

#if (PCCONTROLLER_FORCE_SILENT != 0) && (PCCONTROLLER_FORCE_SILENT != 1)
#error "PCCONTROLLER_FORCE_SILENT must be 0 or 1"
#endif

#if (PCCONTROLLER_BLANK_EEPROM_SILENT != 0) && \
    (PCCONTROLLER_BLANK_EEPROM_SILENT != 1)
#error "PCCONTROLLER_BLANK_EEPROM_SILENT must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_INA219 != 0) && \
    (PCCONTROLLER_ENABLE_INA219 != 1)
#error "PCCONTROLLER_ENABLE_INA219 must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_DS18B20 != 0) && \
    (PCCONTROLLER_ENABLE_DS18B20 != 1)
#error "PCCONTROLLER_ENABLE_DS18B20 must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_PCA9685 != 0) && \
    (PCCONTROLLER_ENABLE_PCA9685 != 1)
#error "PCCONTROLLER_ENABLE_PCA9685 must be 0 or 1"
#endif

// Rich catalog/layout presentation is host-owned. Stable page IDs and the
// fixed EEPROM bytes remain, but the AVR does not carry duplicate directory,
// ordering, hierarchy, or layout-protocol implementations in release builds.
#ifndef PCCONTROLLER_ENABLE_MENU_DIRECTORY
#define PCCONTROLLER_ENABLE_MENU_DIRECTORY 0
#endif

#ifndef PCCONTROLLER_MENU_LAYOUT_STORAGE
#define PCCONTROLLER_MENU_LAYOUT_STORAGE 1
#endif

// AVR-owned persistent front-panel catalog. Stable page IDs remain protocol
// identities while EEPROM stores a separate visibility mask and packed rank.
#ifndef PCCONTROLLER_MENU_VISIBILITY
#define PCCONTROLLER_MENU_VISIBILITY 0
#endif

#ifndef PCCONTROLLER_MENU_ORDERING
#define PCCONTROLLER_MENU_ORDERING 0
#endif

#ifndef PCCONTROLLER_MENU_HIERARCHY
#define PCCONTROLLER_MENU_HIERARCHY 0
#endif

#ifndef PCCONTROLLER_MENU_LAYOUT_PROTOCOL
#define PCCONTROLLER_MENU_LAYOUT_PROTOCOL 0
#endif

#if PCCONTROLLER_MENU_ORDERING && !PCCONTROLLER_MENU_VISIBILITY
#error "Menu ordering requires persistent visibility"
#endif

#if PCCONTROLLER_MENU_HIERARCHY && !PCCONTROLLER_MENU_ORDERING
#error "Nested menus require persistent ordering"
#endif

#if PCCONTROLLER_MENU_LAYOUT_PROTOCOL && !PCCONTROLLER_MENU_ORDERING
#error "Menu-layout protocol requires persistent ordering"
#endif
