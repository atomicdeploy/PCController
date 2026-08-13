import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  audioCueCooldown,
  audioCueNames,
  clampVolume,
  createAudioEngine,
  isAudioCue,
  isWebAudioSupported,
  vibrationPattern,
} from './audio-engine'

describe('procedural audio engine', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('is SSR-safe and does not create audio resources during construction', async () => {
    expect(isWebAudioSupported()).toBe(false)
    const audio = createAudioEngine({ muted: false, volume: 0.4 })
    expect(audio.status).toBe('idle')
    expect(audio.cue('select')).toBe(false)
    expect(await audio.start()).toBe(false)
    expect(audio.status).toBe('unsupported')
    await audio.dispose()
    expect(audio.status).toBe('disposed')
  })

  it('clamps persisted user volume without NaN leaking into Web Audio', () => {
    expect(clampVolume(-1)).toBe(0)
    expect(clampVolume(0.42)).toBe(0.42)
    expect(clampVolume(9)).toBe(1)
    expect(clampVolume(Number.NaN)).toBe(0)
  })

  it('exposes a stable, complete semantic vocabulary with restrained cooldowns', () => {
    expect(audioCueNames).toEqual([
      'focus', 'select', 'navigation', 'success', 'warning', 'error', 'connect', 'disconnect',
    ])
    expect(new Set(audioCueNames).size).toBe(audioCueNames.length)
    for (const cue of audioCueNames) {
      expect(isAudioCue(cue)).toBe(true)
      expect(audioCueCooldown(cue)).toBeGreaterThanOrEqual(0.06)
      expect(audioCueCooldown(cue)).toBeLessThanOrEqual(0.5)
    }
    expect(isAudioCue('')).toBe(false)
    expect(isAudioCue('alarm')).toBe(false)
  })

  it('uses bounded feature-detected haptic patterns only for meaningful mobile cues', () => {
    expect(vibrationPattern('focus')).toBeNull()
    expect(vibrationPattern('navigation')).toBeNull()
    expect(vibrationPattern('select')).toBe(8)
    expect(vibrationPattern('success')).toEqual([10, 22, 14])
    expect(vibrationPattern('disconnect')).toEqual([12, 24, 10])
  })

  it('schedules every semantic cue only after gesture start and honors mute', async () => {
    let oscillatorCount = 0
    let lastOscillatorStart = 0
    const oscillatorStops: number[] = []
    const parameter = () => ({
      value: 0,
      cancelScheduledValues: vi.fn(),
      setValueAtTime(value: number) { this.value = value },
      linearRampToValueAtTime(value: number) { this.value = value },
      exponentialRampToValueAtTime(value: number) { this.value = value },
    })
    class FakeAudioContext {
      currentTime = 1
      state: AudioContextState = 'running'
      destination = { connect: vi.fn(), disconnect: vi.fn() }
      onstatechange: (() => void) | null = null
      createGain() {
        return { gain: parameter(), connect: vi.fn(), disconnect: vi.fn() }
      }
      createDynamicsCompressor() {
        return {
          threshold: parameter(), knee: parameter(), ratio: parameter(),
          attack: parameter(), release: parameter(), connect: vi.fn(), disconnect: vi.fn(),
        }
      }
      createOscillator() {
        oscillatorCount += 1
        return {
          type: 'sine', frequency: parameter(), connect: vi.fn(), disconnect: vi.fn(),
          addEventListener: vi.fn(), start: vi.fn((at: number) => { lastOscillatorStart = at }),
          stop: vi.fn((at: number) => { oscillatorStops.push(at) }),
        }
      }
      createStereoPanner() {
        return { pan: parameter(), connect: vi.fn(), disconnect: vi.fn() }
      }
      async resume() { this.state = 'running' }
      async suspend() { this.state = 'suspended' }
      async close() { this.state = 'closed' }
    }
    vi.stubGlobal('window', { AudioContext: FakeAudioContext })

    const audio = createAudioEngine({ volume: 0.35 })
    expect(oscillatorCount).toBe(0)
    expect(await audio.start()).toBe(true)
    expect(oscillatorCount).toBe(0)
    expect(audio.cue('focus')).toBe(true)
    expect(audio.cue('select')).toBe(true)
    expect(audio.cue('navigation', 'left')).toBe(true)
    expect(audio.cue('success')).toBe(true)
    expect(audio.cue('warning')).toBe(true)
    expect(audio.cue('error')).toBe(true)
    expect(audio.cue('connect')).toBe(true)
    expect(audio.cue('disconnect')).toBe(true)
    expect(oscillatorCount).toBe(15)
		expect(audio.playTone(440, 220)).toBe(true)
		expect(audio.playTone(440, 220, 125, 'board-a')).toBe(true)
		expect(lastOscillatorStart).toBeCloseTo(1.125)
		expect(audio.stopTone('board-a', 25)).toBe(true)
		expect(oscillatorStops.at(-1)).toBeCloseTo(1.025)
		expect(audio.stopTone('board-a')).toBe(false)
		expect(audio.playTone(0, 220)).toBe(false)
		expect(oscillatorCount).toBe(17)
    audio.setMuted(true)
    expect(audio.cue('select')).toBe(false)
		expect(audio.playTone(440, 220)).toBe(false)
		expect(oscillatorCount).toBe(17)
    await audio.dispose()
  })
})
