import { describe, expect, it } from 'vitest'
import { clampVolume, createAudioEngine, isWebAudioSupported } from './audio-engine'

describe('procedural audio engine', () => {
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
})
