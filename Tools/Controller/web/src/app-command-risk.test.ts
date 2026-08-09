import { describe, expect, it } from 'vitest'
import { commandWarning } from './app'

describe('web command confirmation boundary', () => {
  it('catches externally consequential command families', () => {
    for (const command of [
      'reset lines',
      'rf send 0x1234 24 1 350 5',
      'macro play evening',
      'automation run arrival',
      'bridge call lab controller.status',
      'boot info',
      'program flash firmware.hex',
      'write A50135010000',
      'os power shutdown CONFIRM',
      'quit',
    ]) {
      expect(commandWarning(command, 'en'), command).not.toBeNull()
    }
  })

  it('does not interrupt read-only or safety-stop commands', () => {
    for (const command of [
      'status',
      'toolchain profile',
      'rf list',
      'macro cancel',
      'relay off',
      'program-state status',
      'hotkeys status',
    ]) {
      expect(commandWarning(command, 'en'), command).toBeNull()
    }
  })

  it('localizes the browser confirmation copy', () => {
    expect(commandWarning('reset lines', 'fa')?.title).toMatch(/[\u0600-\u06ff]/)
  })
})
