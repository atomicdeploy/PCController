import { describe, expect, it } from 'vitest'
import { redactSensitiveCommand, settingsSetCommand, shellArgument } from './command-line'
import type { ControllerSettings } from './types'

const settings: ControllerSettings = {
  flags: 2,
  light_mode: 1,
  on_brightness: 220,
  off_brightness: 18,
  display_brightness: 5,
  display_closed_brightness: 0,
  motion_exit_hold_seconds: 2,
  status_brightness: 160,
  output_persistence: 6,
  relay_restore_mask: 240,
  stream_period_ms: 200,
  default_page: 4,
  extended_flags: 1,
  motion_break_ms: 1,
}

describe('shared command builders', () => {
  it('quotes shell arguments without JSON-only escape semantics', () => {
    expect(shellArgument('plain')).toBe('plain')
    expect(shellArgument('two words')).toBe('"two words"')
    expect(shellArgument("O'Reilly")).toBe('"O\'Reilly"')
    expect(shellArgument('a\\b"c')).toBe('"a\\\\b\\"c"')
    expect(shellArgument('')).toBe('""')
  })

  it('uses the supported settings form with default-page and save-last together', () => {
    const command = settingsSetCommand(settings, 6, 1, 170, 500, 9, 7, 160)
    expect(command).toBe('settings set 2 1 220 18 6 1 170 7 500 4 1 0 2 2 9 160')
    expect(command.split(' ')).toHaveLength(18)
  })

  it('redacts host power confirmation tokens without hiding ordinary commands', () => {
    expect(redactSensitiveCommand('os power restart secret-token')).toBe('os power restart [REDACTED]')
    expect(redactSensitiveCommand('OS   POWER lock " token with spaces "')).toBe('OS   POWER lock [REDACTED]')
    expect(redactSensitiveCommand('os brightness get')).toBe('os brightness get')
  })
})
