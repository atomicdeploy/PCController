#pragma once
// Named capability bundles share production implementation and storage rules.
// 0 constrained/default; 1 expanded front panel; 2 expanded autonomous board.
#ifndef PCCONTROLLER_FEATURE_PROFILE
#define PCCONTROLLER_FEATURE_PROFILE 0
#endif
#if PCCONTROLLER_FEATURE_PROFILE < 0 || PCCONTROLLER_FEATURE_PROFILE > 2
#error "Unknown PCController feature profile"
#endif
#if PCCONTROLLER_FEATURE_PROFILE != 0
#if defined(__AVR_ATmega328P__)
#error "Expanded profiles require a larger MCU or OS-native firmware target"
#endif
#define PCCONTROLLER_ENABLE_MENU_DIRECTORY 1
#define PCCONTROLLER_MENU_VISIBILITY 1
#define PCCONTROLLER_MENU_ORDERING 1
#define PCCONTROLLER_MENU_HIERARCHY 1
#define PCCONTROLLER_MENU_LAYOUT_PROTOCOL 1
#define PCCONTROLLER_ENABLE_I2C_LCD 1
#define PCCONTROLLER_ENABLE_TASK_SCHEDULER 1
#define PCCONTROLLER_ENABLE_EEPROM_AUDIO_CUES 1
#define PCCONTROLLER_ENABLE_EEPROM_BOOT_OPCODES 1
#endif
#if PCCONTROLLER_FEATURE_PROFILE == 2
// Automation banks and status-profile/menu-label storage have deliberate
// ownership conflicts. The existing production gates/layout enforce them.
#define PCCONTROLLER_ENABLE_BOARD_AUTOMATIONS 1
#endif
