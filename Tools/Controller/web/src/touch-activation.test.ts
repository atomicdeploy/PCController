import { describe, expect, it } from 'vitest'
import { TouchActivationGate, touchActivationGraceMS } from './touch-activation'

describe('immediate touch activation gate', () => {
  it('activates only a primary touch press and suppresses its delayed native click once', () => {
    const gate = new TouchActivationGate<object>()
    const target = {}
    expect(gate.begin(target, 'mouse', true, 1_000)).toBe(false)
    expect(gate.begin(target, 'touch', false, 1_000)).toBe(false)
    expect(gate.begin(target, 'touch', true, 1_000)).toBe(true)
    expect(gate.shouldSuppressNativeClick(target, 1, 1_001)).toBe(true)
    expect(gate.shouldSuppressNativeClick(target, 1, 1_002)).toBe(false)
  })

  it('never suppresses keyboard/programmatic activation and expires safely', () => {
    const gate = new TouchActivationGate<object>()
    const target = {}
    gate.begin(target, 'touch', true, 1_000)
    expect(gate.shouldSuppressNativeClick(target, 0, 1_001)).toBe(false)
    expect(gate.shouldSuppressNativeClick(target, 1, 1_000 + touchActivationGraceMS + 1)).toBe(false)
  })
})
