# Shared channel names and icons

## Ownership

The active board profile on the PCController host owns channel presentation.
Pealayer, browser sessions and local/remote terminal clients consume the same
`controller.peripherals.get` catalog. Terminal appearance preferences are not a
second channel-name store. Independent boards retain independent profiles;
connecting to a host does not copy its names into the client's local settings.

Edits use `controller.peripheral.presentation.update` with the selected semantic
key and `expected_revision`. The host persists the change and emits
`peripherals.changed`, including board identity, revision and changed fields.
Clients refresh on that event and on reconnect. Reads never actuate outputs.

## Regression and repair

- The TUI read the old UI names map rather than resolved profile descriptors.
  Its remote host settings were only captured at startup. Control rows now read
  the authoritative catalog; rename writes use the same revision-fenced API as
  Pealayer. Cinema `seat.a/b` rows use the advertised seat keys, not generic
  registry `motion.a/b` keys. Raw diagnostic controls follow profile advertising.
- The remote terminal's WebSocket handler discarded catalog state events while
  its activity reader deliberately subscribed only to activity. Catalog state
  edges now reach the terminal refresh path, with replay deduplication.
- The Web activity filter dropped `peripherals.changed` because it travels on
  the state stream. That also prevented cross-tab delivery and view refresh.
  Catalog edits now survive that filter; routine telemetry still does not.
- Dashboard and control views refresh metadata on catalog events, connection
  generation/host changes and stream recovery. Superseded requests are aborted;
  failed same-owner reads retain the last good presentation.
- Web relay, motion and PWM headings use profile icons. The bundled regular
  Phosphor font matches Pealayer's complete catalog, including non-preset icons.
  No CDN or active Pealayer process is needed to render them. Terminal fonts
  cannot load that private-use font reliably: common symbols use portable
  Unicode equivalents, while unknown IDs stay readable in brackets.
- The browser's channel-name settings editor also reads resolved profile names,
  includes seat channels, and saves through the shared presentation API. Each
  acknowledged row advances the expected revision; failures preserve the
  remaining draft. Incoming changes retain unsaved edits and require an explicit
  reload on conflicts, rather than silently overwriting either user's work.
- Terminal refreshes are coalesced and ordered. An event during an in-flight
  read causes one trailing refresh instead of accepting a stale response.
  Profile refresh does not replace a user's rename draft. Conflicting saves
  preserve the draft and report the host's revision error.

## Verification and remaining live gate

Focused stable-path Go tests pass for `internal/tui`, `cmd/controller` and
`internal/ipcjson` (ChannelCatalog, Presentation, Peripheral and ControlTable).
They cover cinema seat binding, names/icons, event forwarding/replay, concurrent
refreshes, last-good state, board fencing and revision-fenced rename ACKs.
The focused Web suite passes 31 tests and TypeScript passes.

The integration owner is building/publishing the shared checkout; no separate
generated Web bundle or executable was substituted during this fix. Live
deployment and simultaneous terminal/browser/Pealayer visual acceptance remain
separate gates. Verify using a safe presentation-only rename/icon edit, observe
each connected client without reopening its view, then restore the original
presentation. Do not toggle relays, motion or PWM merely to verify labels.
