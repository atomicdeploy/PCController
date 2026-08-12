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

// The production ControllerBoardMini image keeps the independent INA219,
// DS18B20 and PCA9685 drivers in one profile.  A missing module is discovered
// at runtime and reported as unavailable; a constrained or diagnostic image
// must explicitly opt out rather than silently compiling another feature set.
#ifndef PCCONTROLLER_ENABLE_INA219
#define PCCONTROLLER_ENABLE_INA219 1
#endif

#ifndef PCCONTROLLER_ENABLE_DS18B20
#define PCCONTROLLER_ENABLE_DS18B20 1
#endif

#ifndef PCCONTROLLER_ENABLE_PCA9685
#define PCCONTROLLER_ENABLE_PCA9685 1
#endif

// The compact status engine renders one host descriptor in MCU time and also
// owns the disconnected fallback.  EEPROM condition profiles remain a
// separate, larger-MCU/backlog feature; production advertises only effects.
#ifndef PCCONTROLLER_ENABLE_STATUS_LED_ENGINE
#define PCCONTROLLER_ENABLE_STATUS_LED_ENGINE 1
#endif

#ifndef PCCONTROLLER_ENABLE_STATUS_LED_PROFILES
#define PCCONTROLLER_ENABLE_STATUS_LED_PROFILES 0
#endif

#ifndef PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION
#define PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION 0
#endif

#ifndef PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES
#define PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES 0
#endif

#ifndef PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI
#define PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI 0
#endif

#ifndef PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS
#define PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS 0
#endif

// The 328P production image can publish the four-cell presentation without
// carrying the larger buzzer/status-RGB mirror. Keep that aggregate optional,
// while advertising segment push independently on the wire.
#ifndef PCCONTROLLER_ENABLE_ASYNC_SEGMENT_EVENTS
#define PCCONTROLLER_ENABLE_ASYNC_SEGMENT_EVENTS 1
#endif

#ifndef PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS
#define PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS 0
#endif

// Tone playback and the Buzzer opcode remain available.  Only unsolicited
// boot/menu/error cue policy is optional so a freshly programmed board stays
// quiet until the user deliberately enables an autonomous profile.
#ifndef PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES
#define PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES 0
#endif

// The constrained production profile retains immediate bEEP-page mute/unmute
// but keeps the extended seven-segment settings editor host-owned.  Opt in for
// commissioning builds that need the local brightness/decimal/policy editor.
#ifndef PCCONTROLLER_ENABLE_LOCAL_SETTINGS_EDITOR
#define PCCONTROLLER_ENABLE_LOCAL_SETTINGS_EDITOR 0
#endif

#ifndef PCCONTROLLER_ENABLE_TASK_SCHEDULER
#define PCCONTROLLER_ENABLE_TASK_SCHEDULER 0
#endif

// PAGE_KEYS is retained as a stable wire/EEPROM identifier but is no longer a
// second physical page.  The normal image shows motion; a diagnostic build
// changes that one page into a key identifier.
#ifndef PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS
#define PCCONTROLLER_UNIFIED_PAGE_IDENTIFIES_KEYS 0
#endif

#ifndef PCCONTROLLER_ENABLE_MACRO_CAPTURE
#define PCCONTROLLER_ENABLE_MACRO_CAPTURE 1
#endif

// On the 32 KiB production target this editor is a commissioning trade-off:
// disable board-local macro capture in that optional build, or use a larger
// MCU. The production profile keeps macro capture and direct bEEP recovery.
#if PCCONTROLLER_ENABLE_LOCAL_SETTINGS_EDITOR && PCCONTROLLER_ENABLE_MACRO_CAPTURE
#error "Local settings editor requires a reduced commissioning profile (disable macro capture)"
#endif

#ifndef PCCONTROLLER_FORCE_SILENT
#define PCCONTROLLER_FORCE_SILENT 0
#endif

#ifndef PCCONTROLLER_BLANK_EEPROM_SILENT
#define PCCONTROLLER_BLANK_EEPROM_SILENT 1
#endif

#if (PCCONTROLLER_ENABLE_INA219 != 0) && (PCCONTROLLER_ENABLE_INA219 != 1)
#error "PCCONTROLLER_ENABLE_INA219 must be 0 or 1"
#endif
#if (PCCONTROLLER_ENABLE_DS18B20 != 0) && (PCCONTROLLER_ENABLE_DS18B20 != 1)
#error "PCCONTROLLER_ENABLE_DS18B20 must be 0 or 1"
#endif
#if (PCCONTROLLER_ENABLE_PCA9685 != 0) && (PCCONTROLLER_ENABLE_PCA9685 != 1)
#error "PCCONTROLLER_ENABLE_PCA9685 must be 0 or 1"
#endif
#if PCCONTROLLER_ENABLE_STATUS_LED_ENGINE && !PCCONTROLLER_ENABLE_PCA9685
#error "PCCONTROLLER_ENABLE_STATUS_LED_ENGINE requires PCA9685"
#endif
#if PCCONTROLLER_ENABLE_STATUS_LED_PROFILES
#error "Status LED EEPROM profiles are unavailable in the compact ATmega328P build"
#endif
#if PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION && !PCCONTROLLER_ENABLE_PCA9685
#error "PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION requires PCA9685"
#endif
#if PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES && !PCCONTROLLER_ENABLE_PCA9685
#error "PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES requires PCA9685"
#endif

// Rich catalog/layout presentation is host-owned. Stable page IDs and the
// fixed EEPROM bytes remain, but the AVR does not carry duplicate directory,
// ordering, hierarchy, or layout-protocol implementations in release builds.
#ifndef PCCONTROLLER_ENABLE_MENU_DIRECTORY
#define PCCONTROLLER_ENABLE_MENU_DIRECTORY 0
#endif

#ifndef PCCONTROLLER_MENU_LAYOUT_STORAGE
#define PCCONTROLLER_MENU_LAYOUT_STORAGE 0
#endif

// The 41-byte local layout profile extends settings through EEPROM byte 72,
// while the full-peripheral profile reserves 64..79 for the learned DS18B20
// role identity. They are intentionally separate alpha feature profiles; do
// not silently overlap their persistent records.
#if PCCONTROLLER_MENU_LAYOUT_STORAGE && PCCONTROLLER_ENABLE_DS18B20
#error "PCCONTROLLER_MENU_LAYOUT_STORAGE overlaps the full-peripheral DS18B20 role record; keep production layout host-owned or disable DS18B20"
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
