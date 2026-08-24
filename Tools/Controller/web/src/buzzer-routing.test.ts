import { describe, expect, it } from 'vitest'
import { BuzzerPlaybackTimeline, buzzerPathFromState } from './buzzer-routing'

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

describe('BuzzerPlaybackTimeline', () => {
  it('preserves timed note and pause cadence across arrival jitter', () => {
    const timeline = new BuzzerPlaybackTimeline()
    const first = timeline.plan({
      source: 'board-a', frequencyHz: 440, durationMS: 100, deviceMicros: 0xFFFF_FF00,
    }, 1000)
    expect(first).toEqual({ delayMS: 8, durationMS: 100, audible: true, stop: false })

    const pause = timeline.plan({
      source: 'board-a', frequencyHz: 0, durationMS: 40, deviceMicros: 0x0001_85A0,
    }, 1118)
    expect(pause).toEqual({ delayMS: 0, durationMS: 30, audible: false, stop: true })

    const late = timeline.plan({
      source: 'board-a', frequencyHz: 660, durationMS: 80, deviceMicros: 0x0002_21E0,
    }, 1165)
    expect(late).toEqual({ delayMS: 0, durationMS: 63, audible: true, stop: false })
  })

  it('accepts explicit stop markers and rejects zero-duration tones', () => {
    const timeline = new BuzzerPlaybackTimeline()
    expect(timeline.plan({
      source: 'board-a', frequencyHz: 0, durationMS: 0, deviceMicros: 42,
    }, 1000)).toEqual({ delayMS: 8, durationMS: 0, audible: false, stop: true })
    expect(timeline.plan({ source: 'board-a', frequencyHz: 440, durationMS: 0 }, 1000)).toBeNull()
  })

  it('reanchors a restarted source and keeps independent boards separate', () => {
    const timeline = new BuzzerPlaybackTimeline()
    expect(timeline.plan({ source: 'a', frequencyHz: 440, durationMS: 10, deviceMicros: 9_000_000 }, 10)?.delayMS).toBe(8)
    expect(timeline.plan({ source: 'a', frequencyHz: 440, durationMS: 10, deviceMicros: 1_000 }, 20)?.delayMS).toBe(8)
    expect(timeline.plan({ source: 'b', frequencyHz: 440, durationMS: 10, deviceMicros: 2_000 }, 30)?.delayMS).toBe(8)
  })

  it('reanchors after a whole MCU micros wrap of silence', () => {
    const timeline = new BuzzerPlaybackTimeline()
    const baseMS = 1000
    timeline.plan({ source: 'a', frequencyHz: 440, durationMS: 100, deviceMicros: 1_000 }, baseMS)
    const observedMS = baseMS + 0x1_0000_0000 / 1000 + 30_000
    expect(timeline.plan({
      source: 'a', frequencyHz: 440, durationMS: 100, deviceMicros: 30_001_000,
    }, observedMS)).toEqual({ delayMS: 8, durationMS: 100, audible: true, stop: false })
  })

  it('uses observation time for compact state and breaks a stale device-clock chain', () => {
    const timeline = new BuzzerPlaybackTimeline()
    timeline.plan({ source: 'a', frequencyHz: 440, durationMS: 100, deviceMicros: 1_000 }, 1000)
    expect(timeline.plan({ source: 'a', frequencyHz: 660, durationMS: 80 }, 3000))
      .toEqual({ delayMS: 8, durationMS: 80, audible: true, stop: false })
    expect(timeline.plan({ source: 'a', frequencyHz: 880, durationMS: 60, deviceMicros: 2_000 }, 4000))
      .toEqual({ delayMS: 8, durationMS: 60, audible: true, stop: false })
  })
})
