# Board channel folders

Channel folders are presentation records scoped to a board profile and section
(`motion`, `relay`, `pwm`, `board`). They are independent of effect groups.
`BoardProfile.folders` persists empty folders and their icons. The required
`folders` array in `controller.peripherals.get` combines explicit records and
actual named control assignments, including hidden members.

`controller.peripheral.folder.update` accepts a strict JSON object:

```json
{"operation":"create","kind":"pwm","name":"House lights","icon":"lightbulb","expected_revision":"CURRENT_PROFILE_REVISION"}
```

- `create`: adds an empty folder; no placeholder channel or output command.
- `update`: identifies the current `kind`/`name`; optional `next_name` and `icon`
  rename all members (including hidden channels) and update presentation only.
- `move`: stable `keys` must all belong to the section. `name` must identify an
  existing folder, or be empty for Ungrouped. The whole batch is atomic.
- `delete`: removes the folder and ungroups its members; never deletes channels.

An attached, configured board and `expected_revision` are required. Persistence
compares the revision inside the configuration transaction. A stale request is
rejected, not replayed against newer configuration. The response is the complete
authoritative peripheral catalog and new profile revision; a `peripherals.changed`
event follows. Presentation updates also return the folder catalog and preserve
former folders when their last member is moved out.

Names are 1–64 printable characters, icons at most 64, and there are at most 96
folders per profile. Case-insensitive duplicates within a section are rejected;
the same name in separate sections is allowed. Raw relays is a reserved diagnostic
heading, not an editable operator folder. Cinema seat wiring is advertised as
`seat-internal`; it cannot be moved into an operator folder. Neither the folder
catalog nor clients infer membership from channel names or lighting output type.

Creating, moving, renaming or deleting folders preserves channel names, visibility,
locks, colors, wiring, roles, actions and live output values. A new presentation
entry and kind-local reorder retain the channel's default hidden flag.

Pealayer native and Web UI use this API for section context menus, folder management
and folder drop targets. An explicit Ungrouped heading prevents following cards
from appearing to belong to a preceding named folder.

## Verification

The stable-path Windows runner passed the appconfig/ipcjson focused folder,
profile, presentation and capability tests on 2026-10-10. Domain regressions cover
hidden members, empty-folder JSON roundtrips, rename/delete/move, wrong-kind and
raw-relay rejection, unchanged state after invalid mutations, clone isolation and
revision changes. All 44 stable-path Go package suites, Go vet and the 357 embedded
Web tests passed. The menu-layout encoder now uses fixed twelve-byte storage;
roundtrips for every supported count 1–16 and rejection of 0/17 are tested, avoiding
allocation-size false positives from an already bounded expression.

Runtime deployment and live folder create/drag/rename/icon/stale-revision/delete
evidence is coordinated in Pealayer's
[channel-folders receipt](https://github.com/ToghrolTP/pealayer/blob/main/docs/verification/channel-folders.md).
The final controller executable SHA-256 is
`6a8482a8c2eecee41619a30b091fe7e4339db0446a8b5ffb3fd45fc81f1a043b`,
built from `6537f1f56982408aec2b9395ed40126c87da22cd`. It is verified on Cafe port
8787 and local port 8788. Both Cafe applications remain paused/connected with no
hardware error, saved board wiring and original presentation are retained, and the
temporary acceptance folder is removed. No firmware or output action was required.
