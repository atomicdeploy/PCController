# Cafe temperature sensor commissioning

Commissioned on 2026-10-12 through the primary controller backend on Cafe
`COM3`. The board was running merged firmware build `0xBFFC6BCA` from PR #642
(merge `f3e6c4ebc7f2913acc2ebb0e6cbcc288817f907e`).

## Authoritative sensor roles

| Role | DS18B20 ROM |
|---|---|
| Enclosure lighting (`tLED`, role 0) | `28-70-F2-75-00-00-00-8A` |
| Bluetooth/audio (`tBT`, role 1) | `28-61-64-35-FB-50-3F-E9` |

These roles were established by full-ROM identity, not by discovery order or a
swap flag. If the board EEPROM is erased, recommission the lighting ROM shown
above rather than assuming the probes will be rediscovered in the same order.

## Physical correlation evidence

The enclosure light was driven through this repeating sequence while both ROMs
were sampled directly: off for 60 seconds, full PWM for 120 seconds, off for
90 seconds, then full PWM for 120 seconds.

- During the first full phase, `28-70-F2-75-00-00-00-8A` rose from `20.00 C`
  to `22.81 C`, then reached `23.22 C` from thermal lag just after switch-off
  and cooled to `22.39 C`.
- During the second full phase, the same ROM rose again from `22.39 C` to
  `24.49 C`.
- `28-61-64-35-FB-50-3F-E9` remained approximately `29.99-30.18 C` across
  both light cycles.

The repeatable rise/cool/rise response identifies `28-70-F2-75-00-00-00-8A`
as the enclosure-light probe. Temperature-level comparison alone would have
assigned the probes incorrectly.

## EEPROM recovery record

The commissioned record at EEPROM `0x0144..0x014E` is:

```text
28 70 F2 75 00 00 00 8A 7C 15 C0
```

The first eight bytes are the full lighting-probe ROM, `7C 15` is little-endian
`5500` centi-C (`55.00 C`), and `C0` is the record CRC-8. A fresh post-write
backend capture decoded to this exact record. This is the captured commissioning
value, not a firmware default; current firmware exposes it as an adjustable
EEPROM setting. Preserve other current settings:
capture the complete EEPROM first, patch only this record with
`controller eeprom temperature`, then use the guarded EEPROM update endpoint
and verify a fresh readback.

The pre-change full EEPROM artifact SHA-256 was
`92ccad8c3aee663f16c6f7a6d032d73d277bbfb05cf19e96ddb68b58d522a30f`.
The post-write capture SHA-256 was
`153d7aa94602b09fc6213bfbc18a084a2d536e1bb7691c1b57b5e9df3c83046d`.
Whole-image hashes can differ when unrelated persisted settings or counters
change; validate the record bytes above.
