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

// Rich catalog/layout presentation is host-owned. Stable page IDs and the
// fixed EEPROM bytes remain, but the AVR does not carry duplicate directory,
// ordering, hierarchy, or layout-protocol implementations in release builds.
#ifndef PCCONTROLLER_ENABLE_MENU_DIRECTORY
#define PCCONTROLLER_ENABLE_MENU_DIRECTORY 0
#endif

#ifndef PCCONTROLLER_MENU_LAYOUT_STORAGE
#define PCCONTROLLER_MENU_LAYOUT_STORAGE 1
#endif

// Keep the fixed 4-character front-panel menu labels in the factory EEPROM
// image instead of program flash. The feature is deliberately opt-in because
// an unprovisioned/corrupt EEPROM must fall back to the built-in labels.
#ifndef PCCONTROLLER_ENABLE_EEPROM_MENU_LABELS
#define PCCONTROLLER_ENABLE_EEPROM_MENU_LABELS 0
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
