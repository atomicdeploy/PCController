import { describe, expect, it } from 'vitest'
import { keyboardReorderOffset, pointerReorderTargetChanged } from './pointer-reorder'

describe('card reorder input guards', () => {
  it('applies once per distinct pointer target despite layout-driven move samples', () => {
    let previous: string | null = null
    const accepted: string[] = []
    for (const candidate of ['b', 'b', 'a', 'b', null, 'b', 'c', 'c', 'b']) {
      if (!pointerReorderTargetChanged(previous, 'a', candidate)) continue
      previous = candidate
      accepted.push(candidate)
    }
    expect(accepted).toEqual(['b', 'c', 'b'])
  })

  it('maps physical arrows to logical order in both text directions', () => {
    expect(keyboardReorderOffset('ArrowUp', 'ltr')).toBe(-1)
    expect(keyboardReorderOffset('ArrowDown', 'rtl')).toBe(1)
    expect(keyboardReorderOffset('ArrowLeft', 'ltr')).toBe(-1)
    expect(keyboardReorderOffset('ArrowLeft', 'rtl')).toBe(1)
    expect(keyboardReorderOffset('Enter', 'ltr')).toBeNull()
  })
})
