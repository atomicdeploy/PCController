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
      source: 'board-a', frequencyHz: 440, durationMS: 100,
      deviceMicros: 0xFFFF_FF00, generation: '7',
    }, 1000)
    expect(first).toEqual({ delayMS: 8, durationMS: 100, audible: true, stop: false })

    const pause = timeline.plan({
      source: 'board-a', frequencyHz: 0, durationMS: 40,
      deviceMicros: 0x0001_85A0, generation: '7',
    }, 1118)
    expect(pause).toEqual({ delayMS: 0, durationMS: 30, audible: false, stop: true })

    const late = timeline.plan({
      source: 'board-a', frequencyHz: 660, durationMS: 80,
      deviceMicros: 0x0002_21E0, generation: '7',
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

  it('reanchors a small-forward counter after the source generation changes', () => {
    const timeline = new BuzzerPlaybackTimeline()
    expect(timeline.plan({
      source: 'a', frequencyHz: 440, durationMS: 100, deviceMicros: 1_000, generation: '1',
    }, 1000)).toEqual({ delayMS: 8, durationMS: 100, audible: true, stop: false })
    expect(timeline.plan({
      source: 'a', frequencyHz: 660, durationMS: 100, deviceMicros: 2_000, generation: '2',
    }, 2000)).toEqual({ delayMS: 8, durationMS: 100, audible: true, stop: false })
  })

  it('bounds missing-generation reboot fallback and future timestamp jumps', () => {
    const timeline = new BuzzerPlaybackTimeline()
    timeline.plan({ source: 'legacy-peer', frequencyHz: 440, durationMS: 100, deviceMicros: 1_000 }, 1000)
    expect(timeline.plan({
      source: 'legacy-peer', frequencyHz: 660, durationMS: 100, deviceMicros: 2_000,
    }, 2000)).toEqual({ delayMS: 8, durationMS: 100, audible: true, stop: false })
    expect(timeline.plan({
      source: 'legacy-peer', frequencyHz: 880, durationMS: 100, deviceMicros: 240_002_000,
    }, 2100)).toEqual({ delayMS: 8, durationMS: 100, audible: true, stop: false })
  })

  it('reanchors a fresh note after one whole micros wrap of silence', () => {
    const timeline = new BuzzerPlaybackTimeline()
    timeline.plan({
      source: 'board-a', frequencyHz: 440, durationMS: 100, deviceMicros: 10_000, generation: '9',
    }, 1000)
    const afterWholeWrapAndThirtySecondsMS = 1000 + 0x1_0000_0000 / 1000 + 30_000
    expect(timeline.plan({
      source: 'board-a', frequencyHz: 660, durationMS: 100,
      deviceMicros: 30_010_000, generation: '9',
    }, afterWholeWrapAndThirtySecondsMS)).toEqual({
      delayMS: 8, durationMS: 100, audible: true, stop: false,
    })
  })
})
