# Effect presentation colors

Effect definitions and recording metadata accept the existing semantic presets
or an opaque six-digit `#RRGGBB` value. The controller normalizes hexadecimal
colors to uppercase and preserves them through persistence, catalog readback and
the Control Center editor. Other CSS strings, short hex and alpha hex are rejected.
This is presentation metadata: it does not alter firmware or output commands.

Paired Pealayer work uses one preset/Custom dropdown for Preferences and the
Effects Designer, including session-only recent colors. UI drafts do not replace
the authoritative catalog until the existing save acknowledgement succeeds.

Verification: stable-path Windows appconfig/control suites, `go vet ./...`,
Control Center TypeScript and 363 tests across 72 files. Live custom-effect save
is deliberately not performed against production authored effects during UI QA.
