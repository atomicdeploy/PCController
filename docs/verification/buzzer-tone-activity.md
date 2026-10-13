# Bounded tone activity

The explicit `buzzer FREQUENCY_HZ DURATION_MS` command now uses the same
exclusive output lane as melodies. It still waits for its command ACK before
returning; a failed ACK clears the lane. The caller's cancellation applies until
acceptance, not to the accepted finite tone's remaining lifetime.

Existing `outputs.melody_id` and `outputs.melody_name` describe owned buzzer
activity, including a transient tone named by frequency. No configured melody,
EEPROM write, firmware/protocol change or additional completion opcode is added.
Start/finish output events refresh connected clients; a mid-tone snapshot also
reports the owned activity. Zero-frequency stop remains immediate and skips
silent-settings preflight. UI clients can disable Stop while the owned lane is
idle without making their tone tester impossible to stop.

Verification: the stable-path `internal/control` suite and `go vet ./...` pass.
Regression fixtures cover expiry, invalid inputs, failed ACKs and cancellation
of a 60-second tone. Tests use a recording target, not a physical buzzer.

This is coordinator-owned activity, not proof of acoustic output or all board
autonomous beeps. Native firmware-origin notes outside this owned lane remain
separate events. Runtime delivery, audible cancellation and reconnect acceptance
are distinct from source verification; no tone was played for this change.
