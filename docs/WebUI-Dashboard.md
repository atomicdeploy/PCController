# 🧭 WebUI dashboard contract

> WIP delivery tracked by #159, with shared UI follow-up in #154 and
> native-feeling embedded UI acceptance in #101.

The dashboard is a live view, not an alternate board-control authority. The
host owns the serial connection and emits snapshot, status, event, and
front-panel updates to every UI surface.

## Live and stale state

- A `LIVE` badge with its Wi-Fi icon means a status sample arrived less than
  one second ago **and** the event socket is open. While it is live, the
  manual Refresh button is hidden.
- A connected but older stream is visibly `STALE`; Refresh is then available.
- On transport loss the host clears board-derived values before rendering, so
  relays, EEPROM values, front-panel segments, and telemetry never masquerade
  as current data.
- `front_panel.segment` is pushed into the snapshot immediately. The dashboard
  shows the raw seven-segment bytes without polling the board.

## Personal layout versus shared names

Card order, collapse state, and visibility are persisted only in that
browser's local storage. Drag a card by its grip to reorder it; hide or
collapse it with its controls, then use the hero action menu to restore a
hidden card. This is deliberately local and does not mutate a board or another
operator's layout.

Peripheral labels are different: relay, MOSFET/PWM, and motion-side names use
the host's canonical peripheral catalog. The dashboard action opens Settings,
where saving a label propagates through the host, CLI, TUI, IPC, bridge, and
WebUI. This prevents duplicate definitions while allowing each board setup to
be described clearly.

## Connection and notification behavior

The hero menu offers open, close, reconnect, and USB-device discovery actions.
Its output is sent through the normal command route; it does not bypass the
host's single connection owner.

Relay toggles use that same canonical command path; each cell is a labelled
button whose immediate hover/focus cue describes the next on/off action. Toasts for relay changes
are intentionally limited to physical-front-panel and RF sources. WebUI,
macro, and automation actions already have their initiating feedback and do
not produce duplicate notifications. Parsed `HELLO` and `STATUS` transport
traffic is never surfaced as a toast. Fault and door safety notifications remain
visible regardless of origin. Device and firmware values use live board
identity when it is available, otherwise they say what is pending rather than
rendering a placeholder.

The overview collects connection freshness, LCD address, buzzer/silent state,
and event-driven seven-segment state alongside board identity and diagnostics.
Its action menu links directly to the seven-segment/LCD, buzzer-routing, and
connection settings surfaces so display and connection state never becomes a
dead-end summary.
Temperature cards use an LED/audio tab instead of displaying two competing
thermal cards.

## Host preferences versus board settings

The always-visible quick-header Settings control opens a tabbed **Application
preferences** dialog. Theme, language (including the explicit language
dropdown), direction, interaction feedback, and visibility of nonessential
quick-header controls are browser/host preferences. The Settings shortcut is
not hideable, so the choices are always reversible.

Board EEPROM settings remain visibly separate on the Settings page and require
an explicit board write. The preferences dialog cannot write the controller.

## Remaining WIP acceptance

1. Exercise drag, hide/restore, and label editing in a browser connected to a
   live board.
2. Confirm every host command used by the hero menu has the expected port
   selection UX on multi-port systems.
3. Run visual desktop, narrow/mobile, RTL/LTR, and screen-reader checks before
   merging #159.
