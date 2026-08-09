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

// The production host owns the 16x2 PCF8574 LCD through the bounded generic
// I2C opcode. The physical LCD remains fully usable while its duplicated
// HD44780 presentation renderer stays off this 32 KiB MCU. The native-renderer
// flag is reserved until real lifecycle/DisplayText call sites exist; rejecting
// it prevents an image from falsely advertising capability bit 6.
#ifndef PCCONTROLLER_ENABLE_I2C_LCD
#define PCCONTROLLER_ENABLE_I2C_LCD 0
#endif

// The live ControllerBoardMini production profile includes all three native
// peripheral drivers. Constrained/diagnostic profiles must opt out explicitly
// and advertise the resulting capability bitmap truthfully in HELLO.
#ifndef PCCONTROLLER_ENABLE_INA219
#define PCCONTROLLER_ENABLE_INA219 1
#endif

#ifndef PCCONTROLLER_ENABLE_DS18B20
#define PCCONTROLLER_ENABLE_DS18B20 1
#endif

#ifndef PCCONTROLLER_ENABLE_PCA9685
#define PCCONTROLLER_ENABLE_PCA9685 1
#endif

// Keep the PCA9685's complete 16-channel output API on the MCU while the host
// owns rich animation and local configuration presentation. These optional
// engines do not affect direct PWM/status RGB commands or hardware discovery.
#ifndef PCCONTROLLER_ENABLE_STATUS_LED_ENGINE
#define PCCONTROLLER_ENABLE_STATUS_LED_ENGINE 0
#endif

#ifndef PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION
#define PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION 0
#endif

#ifndef PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES
#define PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES 0
#endif

// RF receive, learned mappings, protocol learning, and exact action evidence
// remain enabled. Only the duplicated four-digit local learning page is host
// owned in the production profile.
#ifndef PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI
#define PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI 0
#endif

// Host polling and ordinary action evidence remain available. Changed-only
// render mirrors and the MCU segment scheduler duplicate host presentation and
// can be enabled in feature builds when flash permits.
#ifndef PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS
#define PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS 0
#endif

#ifndef PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS
#define PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS 0
#endif

// Audio streaming and the native Buzzer opcode stay available. Autonomous
// boot/menu/success/error melodies are host presentation and are omitted from
// the byte-tight production image (and cannot wake a room after a reset).
#ifndef PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES
#define PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES 0
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

#if (PCCONTROLLER_ENABLE_I2C_LCD != 0) && \
    (PCCONTROLLER_ENABLE_I2C_LCD != 1)
#error "PCCONTROLLER_ENABLE_I2C_LCD must be 0 or 1"
#endif

#if PCCONTROLLER_ENABLE_I2C_LCD
#error "PCCONTROLLER_ENABLE_I2C_LCD is reserved until the MCU renderer has lifecycle and DisplayText call sites; use generic I2C capability bit 16"
#endif

#if (PCCONTROLLER_ENABLE_STATUS_LED_ENGINE != 0) && \
    (PCCONTROLLER_ENABLE_STATUS_LED_ENGINE != 1)
#error "PCCONTROLLER_ENABLE_STATUS_LED_ENGINE must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION != 0) && \
    (PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION != 1)
#error "PCCONTROLLER_ENABLE_ILLUMINATION_AUTOMATION must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES != 0) && \
    (PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES != 1)
#error "PCCONTROLLER_ENABLE_LOCAL_PCA_PAGES must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI != 0) && \
    (PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI != 1)
#error "PCCONTROLLER_ENABLE_LOCAL_RF_LEARNING_UI must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS != 0) && \
    (PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS != 1)
#error "PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS != 0) && \
    (PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS != 1)
#error "PCCONTROLLER_ENABLE_SCHEDULED_SEGMENTS must be 0 or 1"
#endif

#if (PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES != 0) && \
    (PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES != 1)
#error "PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES must be 0 or 1"
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
