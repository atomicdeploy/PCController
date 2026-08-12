import { describe, expect, it } from 'vitest'
import { movePeripheralControl, peripheralPresentationPayload } from './peripheral-presentation'
import type { PeripheralControlDescriptor } from './types'

const controls: PeripheralControlDescriptor[] = [
  { key: 'relay.5', kind: 'relay', role: 'user-output', index: 5, order: 0, name: ' Bench ', description: ' Lamp ', default_name: 'Relay 5', default_description: 'Relay', control: 'relay' },
  { key: 'relay.6', kind: 'relay', role: 'user-output', index: 6, order: 1, name: 'Relay 6', description: 'Spare', default_name: 'Relay 6', default_description: 'Relay', control: 'relay' },
  { key: 'motion.a', kind: 'side', role: 'motion-side', index: 1, order: 2, name: 'Left', description: 'Left lift', default_name: 'Side A', default_description: 'Motion', control: 'motion' },
]

describe('peripheral presentation', () => {
  it('commits normalized names, descriptions, and contiguous ranges without a save step', () => {
    expect(peripheralPresentationPayload(controls)).toEqual({
      'relay.5': { name: 'Bench', description: 'Lamp', order: 0 },
      'relay.6': { name: 'Relay 6', description: 'Spare', order: 1 },
      'motion.a': { name: 'Left', description: 'Left lift', order: 2 },
    })
  })

  it('supports same-kind drag reorder and rejects cross-kind drops', () => {
    expect(movePeripheralControl(controls, 'relay.6', 'relay.5').map((control) => control.key))
      .toEqual(['relay.6', 'relay.5', 'motion.a'])
    expect(movePeripheralControl(controls, 'relay.5', 'motion.a').map((control) => control.key))
      .toEqual(controls.map((control) => control.key))
  })
})
