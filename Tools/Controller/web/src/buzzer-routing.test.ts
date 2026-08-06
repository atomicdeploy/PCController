import { describe, expect, it } from 'vitest'
import { buzzerPathFromState } from './buzzer-routing'

describe('buzzerPathFromState', () => {
  it.each([
    [false, true, 'board'],
    [true, false, 'host'],
    [false, false, 'both'],
    [true, true, 'none'],
  ] as const)('maps boardSilent=%s hostSilent=%s to %s', (boardSilent, hostSilent, path) => {
    expect(buzzerPathFromState(boardSilent, hostSilent)).toBe(path)
  })
})
