import { describe, it, expect } from 'vitest'
import { searchChannels } from './channel-search'
import type { ControlDescriptor } from './types'

describe('profile-resolved channel search', () => {
  const channels: ControlDescriptor[] = [
    { key: 'pwm.0', kind: 'mosfet', order: 0, name: 'Window', group: 'Cinema lighting', control: 'pwm-user' },
    { key: 'seat.a', kind: 'seat', order: 1, name: 'Seat Left', control: 'seat' },
    { key: 'pwm.1', kind: 'mosfet', order: 2, name: 'Hidden lamp', control: 'pwm-user', hidden: true },
  ]
  it('finds current names, IDs and groups without changing or actuating channels', () => {
    expect(searchChannels(channels, 'window').map(item => item.key)).toEqual(['pwm.0'])
    expect(searchChannels(channels, 'pwm.0').map(item => item.name)).toEqual(['Window'])
    expect(searchChannels(channels, 'cinema').map(item => item.key)).toEqual(['pwm.0'])
    expect(searchChannels(channels, 'seat left').map(item => item.key)).toEqual(['seat.a'])
    expect(searchChannels(channels, 'hidden')).toEqual([])
    expect(searchChannels(channels, '')).toEqual([])
  })
})
