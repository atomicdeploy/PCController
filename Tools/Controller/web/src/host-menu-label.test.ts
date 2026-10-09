import { describe, expect, it } from 'vitest'

import { hostMenuLabelCommand } from './advanced-workbench'

describe('HOST menu short-label commands', () => {
  it('uses the shared file-watched definition command', () => {
    expect(hostMenuLabelCommand('host', 'TIME')).toBe('host-menu set host label TIME')
    expect(hostMenuLabelCommand('0x80', 'A B')).toBe('host-menu set 0x80 label "A B"')
  })

  it('requires an explicit menu and one to four printable ASCII characters', () => {
    expect(hostMenuLabelCommand('', 'TIME')).toBeUndefined()
    expect(hostMenuLabelCommand('host', '')).toBeUndefined()
    expect(hostMenuLabelCommand('host', 'CLOCK')).toBeUndefined()
    expect(hostMenuLabelCommand('host', 'زمان')).toBeUndefined()
  })
})
