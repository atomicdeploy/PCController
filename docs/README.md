# Documentation

PCController combines ATmega328P firmware, a Windows-first Go controller host,
programming tools, and a native desktop simulator. Read the documents in this
order when the project is new to you:

1. [Getting Started and Operations](Getting-Started-and-Operations.md) — wire
   safety, first connection, the TUI and shell, ordinary control, backups, and
   recovery.
2. [Hardware Initialization and Tuning](Hardware-Initialization-and-Tuning.md)
   — exact INA219, PWM, DS18B20, I2C, display, RF, relay, LED, timer, and UART
   parameters, why they were chosen, and safe tuning alternatives.
3. [Front Panel and Menus](Front-Panel-and-Menus.md) — the four physical or
   remote keys, seven-segment/LCD behavior, edit/confirm flows, relays, motion,
   PWM, and RF learning.
4. [Host Configuration and Integrations](Host-Configuration-and-Integrations.md)
   — the Charm TUI, PC-only JSON/YAML/TOML configuration, USB ownership,
   hotkeys, notifications, discovery, webhooks, and remote bridges.
5. [Protocol and Network API](../Tools/Controller/docs/Protocol-and-Network-API.md)
   — COBS/CRC application opcodes, JSON-RPC, REST, WebSocket, events, and the
   application/Urboot ownership boundary.
6. [Control-Surface Capability Matrix](../Tools/Controller/docs/Control-Surface-Capability-Matrix.md)
   — per-domain reachability through CLI, libraries, IPC, network APIs,
   subscriptions, and remote authorization.
7. [C Library API](../Tools/Controller/docs/C-Library-API.md) — embedding the Go
   controller through its C-compatible DLL.
8. [Memory and Feature Tradeoffs](Memory-and-Feature-Tradeoffs.md) — measured
   AVR flash/SRAM use and the exact capability lost by each optional cut.
9. [Urboot-Custom](../Tools/Bootloader/Urboot-Custom/README.md) — the
   reproducible upstream patch, exact stock hashes, optional progress backend,
   byte/profile matrix, and ISP-first installation constraints.
10. [Local Library Merge History](Local-Library-Merge-History.md) — what differed
   between Puzzles, Timer, and motor-encoder-HMI LocalLib variants, and which
   implementation was retained.
11. [Upstream Source Audit](../Tools/Controller/docs/Upstream-Source-Audit.md) —
   reused ideas, external dependencies, licenses, and provenance.
12. [Toolchain Bootstrap and Safe Programming](Toolchain-and-Safe-Programming.md)
    — reproducible clean-machine firmware dependencies, proxy behavior,
    guarded backup/flash/restore, and crash recovery.
13. [CI/CD and Releases](CI-CD-and-Releases.md) — GitHub build matrices,
   artifacts, checksums, dependency automation, and draft/published release
   behavior.
14. [Project Checklist](Project-Checklist.md) — the durable acceptance record.
   A green item requires source plus proportionate build, simulation, live-board,
   or user-observation evidence; yellow and warning items remain work or physical
   validation.
15. [Completion Recovery Audit](Completion-Recovery-Audit.md) — the explicit
    boundary between implemented-and-verified, implemented-but-unverified,
    missing, and human-dependent work after the TUI/macro handoff gap.

The root [README](../README.md) is the hardware/architecture overview. Host-only
commands and packaging are also summarized in the
[controller-tool README](../Tools/Controller/README.md), while the virtual AVR
substitute has its own [simulation guide](../Tools/VirtualBoard/README.md).

## Configuration ownership

Do not mix the two persistence domains:

- board settings, RF records, reset telemetry, and board-resident automation
  belong to MCU EEPROM;
- device-selection preferences, TUI presentation, network endpoints, hotkeys,
  notifications, webhooks, histories, and host automations belong to the host
  configuration files.

Changing or deleting the host configuration never rewrites MCU EEPROM unless a
specific board command is issued. Conversely, erasing EEPROM does not erase the
host's remembered USB identity or UI/network settings.

## Development-state warning

The EEPROM layout may be cleanly reinitialized during this development phase;
the project deliberately avoids spending scarce AVR flash on whole-history
migrations until the layout is declared stable. Back up flash and EEPROM before
any commissioning image whose notes say the layout changed.
