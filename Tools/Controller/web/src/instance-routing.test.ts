import { describe, expect, it } from 'vitest'
import { matchesAppTarget } from './instance-routing'

describe('matchesAppTarget', () => {
  it('accepts broadcasts, a surface, and an exact instance', () => {
    expect(matchesAppTarget('*', 'tab:one', 'webui')).toBe(true)
    expect(matchesAppTarget('WEBUI', 'tab:one', 'webui')).toBe(true)
    expect(matchesAppTarget('tab:one', 'tab:one', 'webui')).toBe(true)
    expect(matchesAppTarget('tab:two', 'tab:one', 'webui')).toBe(false)
    expect(matchesAppTarget('tui', 'tab:one', 'webui')).toBe(false)
  })
})
