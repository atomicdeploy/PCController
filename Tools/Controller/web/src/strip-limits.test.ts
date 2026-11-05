import { expect, it } from 'vitest'
import { DEFAULT_STRIP_MAXIMUM_FPS } from './strip-limits'

it('allows 60 and 120 FPS in offline authoring', () => {
  expect(DEFAULT_STRIP_MAXIMUM_FPS).toBe(120)
})
