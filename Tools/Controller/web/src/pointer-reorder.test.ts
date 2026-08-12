import { describe, expect, it } from 'vitest'
import { pointerReorderTargetChanged } from './pointer-reorder'

describe('pointer reorder target guard', () => {
  it('applies once per distinct target despite repeated layout-driven move samples', () => {
    let previous: string | null = null
    const accepted: string[] = []
    for (const candidate of ['b', 'b', 'a', 'b', null, 'b', 'c', 'c', 'b']) {
      if (!pointerReorderTargetChanged(previous, 'a', candidate)) continue
      previous = candidate
      accepted.push(candidate)
    }
    expect(accepted).toEqual(['b', 'c', 'b'])
  })
})
