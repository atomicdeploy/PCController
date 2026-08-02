import { describe, expect, it } from 'vitest'
import { rfLearnCommand } from './workbench'

describe('RF learning command contract', () => {
  it('defaults the indefinite selection to multi-code learning', () => {
    expect(rfLearnCommand('indefinite', 30)).toBe('rf learn indefinite')
  })

  it('uses the canonical timer name and a bounded duration', () => {
    expect(rfLearnCommand('timer', 45.4)).toBe('rf learn timer 45s')
    expect(rfLearnCommand('timer', -10)).toBe('rf learn timer 1s')
    expect(rfLearnCommand('timer', 100_000)).toBe('rf learn timer 120s')
  })
})
