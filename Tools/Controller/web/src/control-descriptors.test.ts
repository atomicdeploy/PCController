import { describe, expect, it } from 'vitest'
import { resolvedControlName } from './control-descriptors'
import type { ControlDescriptor } from './types'

describe('resolved control descriptors', () => {
  const controls: ControlDescriptor[] = [
    { key: 'relay.5', kind: 'relay', order: 5, name: 'Bench lamp', control: 'relay' },
    { key: 'motion.a', kind: 'side', order: 1, name: 'Left lift', control: 'motion' },
    { key: 'pwm.0', kind: 'mosfet', order: 0, name: 'Work light', control: 'pwm-user' },
  ]

  it('uses the resolved host name without reinterpreting order or kind', () => {
    expect(resolvedControlName(controls, 'relay.5', 'User relay 5')).toBe('Bench lamp')
    expect(controls.map(({ key, kind, order }) => [key, kind, order])).toEqual([
      ['relay.5', 'relay', 5],
      ['motion.a', 'side', 1],
      ['pwm.0', 'mosfet', 0],
    ])
  })

  it('keeps the localized fallback while the contract is unavailable', () => {
    expect(resolvedControlName(undefined, 'motion.b', 'حرکت سمت B')).toBe('حرکت سمت B')
  })
})
