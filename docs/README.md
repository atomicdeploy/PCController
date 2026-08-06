<div align="center">
  <a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a>
</div>

# 📚 Documentation hub

This index contains the maintained product documentation. Start with the path
that matches what you are trying to do; board-owned EEPROM settings and
host-owned JSON configuration are documented separately on purpose.

## 🚀 Start here

| Goal | Recommended guide |
|---|---|
| Build or run PCController for the first time | [Getting Started and Operations](Getting-Started-and-Operations.md) |
| Understand the physical panel | [Front Panel and Menus](Front-Panel-and-Menus.md) |
| Configure the host, WebUI, hotkeys, notifications, or integrations | [Host Configuration and Integrations](Host-Configuration-and-Integrations.md) |
| Integrate through UART, JSON-RPC, REST, WebSocket, Go, or C | [Protocol and Network API](../Tools/Controller/docs/Protocol-and-Network-API.md), [machine-readable contracts](../Tools/Controller/api/reference.html), and [C Library API](../Tools/Controller/docs/C-Library-API.md) |
| Export or host the WebUI from a separate origin | [Portable WebUI](../Tools/Controller/docs/Portable-WebUI.md) |
| Build, back up, program, or recover a board | [Toolchain and Safe Programming](Toolchain-and-Safe-Programming.md) |
| Check wiring and electrical assumptions | [Hardware Initialization and Tuning](Hardware-Initialization-and-Tuning.md) |
| Verify what each UI/API can reach | [Control-Surface Capability Matrix](../Tools/Controller/docs/Control-Surface-Capability-Matrix.md) |
| Review current acceptance boundaries | [Project Acceptance](Project-Checklist.md) |

## 🧭 Product references

1. [Getting Started and Operations](Getting-Started-and-Operations.md) — build,
   launch, connect, monitor, operate, automate, and troubleshoot.
2. [Front Panel and Menus](Front-Panel-and-Menus.md) — keys, gestures,
   TM1637 pages, editors, menu persistence, hosted menus, and safety behavior.
3. [Host Configuration and Integrations](Host-Configuration-and-Integrations.md)
   — configuration schema, UI settings, hotkeys, local device integration,
   notifications, discovery, webhooks, and bridges.
4. [Hardware Initialization and Tuning](Hardware-Initialization-and-Tuning.md)
   — pin ownership, buses, addresses, timing, polarity, calibration, and test
   order.
5. [Protocol and Network API](../Tools/Controller/docs/Protocol-and-Network-API.md)
   — native framing, opcodes, JSON-RPC, REST, WebSocket, security, and events.
6. [Machine-readable API contracts](../Tools/Controller/api/reference.html) —
   offline OpenAPI 3.1, AsyncAPI 3.0, and JSON-RPC method/error catalogs.
7. [C Library API](../Tools/Controller/docs/C-Library-API.md) — ABI lifecycle,
   JSON payload ownership, callbacks, and examples.
8. [Control-Surface Capability Matrix](../Tools/Controller/docs/Control-Surface-Capability-Matrix.md)
   — feature reachability across firmware, WebUI, TUI, CLI, IPC, and libraries.
9. [Portable WebUI](../Tools/Controller/docs/Portable-WebUI.md) — deterministic
   export, controller-origin validation, CORS, tokens, and static-host rules.

## 🔐 Runtime ownership

| Domain | Authoritative storage and owner |
|---|---|
| Board behavior | MCU EEPROM owns board settings, learned RF records and mappings, and reset telemetry; richer automations remain host-owned. |
| Host behavior | The host configuration owns device-selection preferences, UI/network settings, hotkeys, notifications, webhooks, history, scripts, and host automation. |

Changing host configuration does not rewrite MCU EEPROM unless an explicit
board command is issued. Erasing board EEPROM does not remove the host's saved
device identity or interface settings.

## 🛠️ Delivery and engineering

- [Build Tool](../Tools/Build/README.md) — canonical build entry points and
  package locations.
- [Firmware Tool](../Tools/Firmware/README.md) — content-watched builds,
  validation, upload, and bootloader operations.
- [CI/CD and Releases](CI-CD-and-Releases.md) — workflow artifacts,
  checksums, provenance, and pre-release policy.
- [Toolchain and Safe Programming](Toolchain-and-Safe-Programming.md) — managed
  dependencies and recoverable programming lifecycle.
- [Memory and Feature Tradeoffs](Memory-and-Feature-Tradeoffs.md) — current AVR
  resource constraints and deliberate host/firmware ownership choices.
- [Local Library Variant Comparison](Local-Library-Variant-Comparison.md) —
  privacy-safe comparison of the three reviewed helper variants and the parts
  retained, improved, or deliberately excluded.
- [Host Controller Tool](../Tools/Controller/README.md) — WebUI, TUI, CLI,
  native integration, programming, and packaging reference.
- [Virtual Board](../Tools/VirtualBoard/README.md) — hardware-free native
  simulator setup and protocol testing.
- [Requirements Backlog](Requirements-Backlog.md) — issue-linked open work.
- [Project Acceptance](Project-Checklist.md) — current software gates and
  hardware work that remains deliberately unclaimed.

## ✍️ Documentation rules

- Repository Markdown is canonical; the wiki is a published mirror.
- Commands describe the current source tree, not an earlier artifact.
- A successful build is not a physical-device safety claim.
- Superseded implementation notes are intentionally excluded from the
  maintained documentation.
- When behavior changes, update the implementation, tests, capability matrix,
  and relevant operating guide together.
- Physical board-output mirrors are push-first. Do not introduce steady-state
  polling as an implementation shortcut when an unsolicited change opcode is
  available. Snapshot reads are only for connection sync, explicit refresh,
  detected-gap recovery, or a visible bounded fallback for legacy firmware.

<p align="center"><a href="../README.md">← Return to the PCController main page</a></p>
