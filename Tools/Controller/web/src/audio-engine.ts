/**
 * Gesture-gated procedural UI cues for the controller interface. Construction
 * is side-effect free: an AudioContext is created only
 * by start(), which the app calls from an explicit user gesture.
 */

export type AudioStatus = 'idle' | 'starting' | 'running' | 'paused' | 'unsupported' | 'disposed'
/**
 * Stable semantic cue vocabulary shared with native host feedback.  These are
 * intent names rather than filenames so each platform can use its most
 * respectful output (short Web Audio envelopes here, system sounds natively).
 */
export const audioCueNames = [
  'focus',
  'select',
  'navigation',
  'success',
  'warning',
  'error',
  'connect',
  'disconnect',
] as const

export type AudioCue = typeof audioCueNames[number]
export type NavigationDirection = 'forward' | 'backward' | 'left' | 'right' | 'up' | 'down' | 'neutral'

type AudioContextConstructor = new (options?: AudioContextOptions) => AudioContext

export interface AudioEngineOptions {
  muted?: boolean
  volume?: number
  pauseWhenHidden?: boolean
}

export interface AudioEngine {
  readonly supported: boolean
  readonly muted: boolean
  readonly status: AudioStatus
  readonly volume: number
  start(options?: { muted?: boolean }): Promise<boolean>
  setMuted(value: boolean): void
  toggleMuted(): boolean
  setVolume(value: number): void
  cue(name: AudioCue, direction?: NavigationDirection): boolean
  suspend(): Promise<void>
  resume(): Promise<boolean>
  dispose(): Promise<void>
}

const floorGain = 0.0001

export function clampVolume(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(1, value)) : 0
}

export function isAudioCue(value: unknown): value is AudioCue {
  return typeof value === 'string' && (audioCueNames as readonly string[]).includes(value)
}

export function audioCueCooldown(name: AudioCue): number {
  switch (name) {
    case 'navigation': return 0.065
    case 'focus': return 0.11
    case 'select': return 0.16
    case 'success': return 0.24
    case 'warning': return 0.3
    case 'error': return 0.34
    case 'connect':
    case 'disconnect': return 0.5
  }
}

function audioContextConstructor(): AudioContextConstructor | null {
  if (typeof window === 'undefined') return null
  const audioWindow = window as unknown as {
    AudioContext?: AudioContextConstructor
    webkitAudioContext?: AudioContextConstructor
  }
  return audioWindow.AudioContext ?? audioWindow.webkitAudioContext ?? null
}

export function isWebAudioSupported(): boolean {
  return audioContextConstructor() !== null
}

class ProceduralAudioEngine implements AudioEngine {
  private context: AudioContext | null = null
  private master: GainNode | null = null
  private state: AudioStatus = 'idle'
  private isMuted: boolean
  private masterVolume: number
  private readonly pauseWhenHidden: boolean
  private manuallySuspended = false
  private hiddenSuspended = false
  private listenerAttached = false
  private startPromise: Promise<boolean> | null = null
  private readonly sources = new Set<AudioScheduledSourceNode>()
  private readonly lastCueAt = new Map<AudioCue, number>()

  constructor(options: AudioEngineOptions = {}) {
    this.isMuted = options.muted ?? false
    this.masterVolume = clampVolume(options.volume ?? 0.42)
    this.pauseWhenHidden = options.pauseWhenHidden ?? true
  }

  get supported(): boolean { return this.context !== null || isWebAudioSupported() }
  get muted(): boolean { return this.isMuted }
  get status(): AudioStatus { return this.state }
  get volume(): number { return this.masterVolume }

  start(options?: { muted?: boolean }): Promise<boolean> {
    if (options?.muted !== undefined) this.setMuted(options.muted)
    if (this.state === 'disposed') return Promise.resolve(false)
    if (this.context) return this.resume()
    if (this.startPromise) return this.startPromise
    this.startPromise = this.initialize().finally(() => { this.startPromise = null })
    return this.startPromise
  }

  setMuted(value: boolean): void {
    if (this.state === 'disposed') return
    this.isMuted = value
    this.applyMasterGain(0.055)
  }

  toggleMuted(): boolean {
    this.setMuted(!this.isMuted)
    return this.isMuted
  }

  setVolume(value: number): void {
    if (this.state === 'disposed') return
    this.masterVolume = clampVolume(value)
    this.applyMasterGain(0.07)
  }

  cue(name: AudioCue, direction: NavigationDirection = 'neutral'): boolean {
    const context = this.playableContext(name)
    if (!context || !this.master) return false
    vibrateCue(name)
    const now = context.currentTime + 0.005
    switch (name) {
      case 'focus':
        this.tone(context, now, 0.16, 523.25, 587.33, 0.022, 0, 'sine')
        break
      case 'select':
        ;[392, 523.25, 659.25].forEach((frequency, index) => {
          this.tone(context, now + index * 0.027, 0.3 + index * 0.04, frequency, frequency * 1.006, 0.018 - index * 0.002, (index - 1) * 0.14, index === 1 ? 'triangle' : 'sine')
        })
        break
      case 'navigation': {
        const note = navigationTone(direction)
        this.tone(context, now, 0.11, note.start, note.end, 0.02, note.pan, 'sine')
        break
      }
      case 'warning':
        this.tone(context, now, 0.21, 246.94, 196, 0.027, -0.08, 'triangle')
        this.tone(context, now + 0.09, 0.25, 220, 174.61, 0.022, 0.08, 'sine')
        break
      case 'success':
        this.tone(context, now, 0.26, 329.63, 493.88, 0.021, -0.08, 'sine')
        this.tone(context, now + 0.06, 0.32, 493.88, 659.25, 0.017, 0.08, 'triangle')
        break
      case 'error':
        this.tone(context, now, 0.28, 196, 146.83, 0.026, -0.08, 'triangle')
        this.tone(context, now + 0.075, 0.3, 164.81, 123.47, 0.02, 0.08, 'sine')
        break
      case 'connect':
        this.tone(context, now, 0.24, 293.66, 392, 0.018, -0.08, 'sine')
        this.tone(context, now + 0.055, 0.3, 440, 587.33, 0.014, 0.08, 'triangle')
        break
      case 'disconnect':
        this.tone(context, now, 0.24, 392, 293.66, 0.018, -0.08, 'triangle')
        this.tone(context, now + 0.055, 0.3, 261.63, 196, 0.014, 0.08, 'sine')
        break
    }
    return true
  }

  async suspend(): Promise<void> {
    const context = this.context
    if (!context || this.state === 'disposed') return
    this.manuallySuspended = true
    this.hiddenSuspended = false
    if (context.state === 'running') {
      try { await context.suspend() } catch { /* closed between check and call */ }
    }
    if (context === this.context) this.state = 'paused'
  }

  async resume(): Promise<boolean> {
    const context = this.context
    if (!context || context.state === 'closed' || this.state === 'disposed') return false
    if (typeof document !== 'undefined' && document.hidden) {
      this.state = 'paused'
      return false
    }
    this.manuallySuspended = false
    this.hiddenSuspended = false
    try {
      if (context.state !== 'running') await context.resume()
    } catch {
      this.state = 'paused'
      return false
    }
    const running = context.state === 'running'
    this.state = running ? 'running' : 'paused'
    if (running) this.applyMasterGain(0.12)
    return running
  }

  async dispose(): Promise<void> {
    if (this.state === 'disposed') return
    this.state = 'disposed'
    this.detachVisibilityListener()
    for (const source of this.sources) {
      try { source.stop() } catch { /* source already ended */ }
      try { source.disconnect() } catch { /* source already released */ }
    }
    this.sources.clear()
    const context = this.context
    this.context = null
    this.master = null
    if (context && context.state !== 'closed') {
      try { await context.close() } catch { /* best-effort teardown */ }
    }
  }

  private async initialize(): Promise<boolean> {
    const AudioContextClass = audioContextConstructor()
    if (!AudioContextClass) {
      this.state = 'unsupported'
      return false
    }
    this.state = 'starting'
    let context: AudioContext
    try {
      context = new AudioContextClass({ latencyHint: 'interactive' })
    } catch {
      try { context = new AudioContextClass() } catch {
        this.state = 'unsupported'
        return false
      }
    }
    this.context = context
    const master = context.createGain()
    const compressor = context.createDynamicsCompressor()
    master.gain.setValueAtTime(0, context.currentTime)
    compressor.threshold.setValueAtTime(-22, context.currentTime)
    compressor.knee.setValueAtTime(16, context.currentTime)
    compressor.ratio.setValueAtTime(4, context.currentTime)
    compressor.attack.setValueAtTime(0.012, context.currentTime)
    compressor.release.setValueAtTime(0.24, context.currentTime)
    master.connect(compressor)
    compressor.connect(context.destination)
    this.master = master
    this.attachVisibilityListener()
    context.onstatechange = () => {
      if (context !== this.context || this.state === 'disposed') return
      this.state = context.state === 'running' ? 'running' : 'paused'
    }
    try {
      if (context.state !== 'running') await context.resume()
    } catch {
      this.state = 'paused'
      return false
    }
    const running = context.state === 'running'
    this.state = running ? 'running' : 'paused'
    if (running) this.applyMasterGain(0.32)
    return running
  }

  private playableContext(name: AudioCue): AudioContext | null {
    const context = this.context
    if (!context || this.state !== 'running' || this.isMuted || this.masterVolume <= 0) return null
    const cooldown = audioCueCooldown(name)
    const previous = this.lastCueAt.get(name) ?? -Infinity
    if (context.currentTime - previous < cooldown) return null
    this.lastCueAt.set(name, context.currentTime)
    return context
  }

  private tone(
    context: AudioContext,
    startTime: number,
    duration: number,
    frequency: number,
    endFrequency: number,
    peakGain: number,
    pan: number,
    type: OscillatorType,
  ): void {
    if (!this.master || context.state === 'closed' || this.state === 'disposed') return
    const oscillator = context.createOscillator()
    const envelope = context.createGain()
    const panner = typeof context.createStereoPanner === 'function' ? context.createStereoPanner() : null
    const attackEnd = startTime + Math.min(0.022, duration * 0.2)
    const endTime = startTime + duration
    oscillator.type = type
    oscillator.frequency.setValueAtTime(Math.max(1, frequency), startTime)
    oscillator.frequency.exponentialRampToValueAtTime(Math.max(1, endFrequency), endTime)
    envelope.gain.setValueAtTime(floorGain, startTime)
    envelope.gain.exponentialRampToValueAtTime(Math.max(floorGain, peakGain), attackEnd)
    envelope.gain.exponentialRampToValueAtTime(floorGain, endTime)
    oscillator.connect(envelope)
    if (panner) {
      panner.pan.setValueAtTime(Math.max(-1, Math.min(1, pan)), startTime)
      envelope.connect(panner)
      panner.connect(this.master)
    } else {
      envelope.connect(this.master)
    }
    this.sources.add(oscillator)
    oscillator.addEventListener('ended', () => {
      this.sources.delete(oscillator)
      oscillator.disconnect()
      envelope.disconnect()
      panner?.disconnect()
    }, { once: true })
    oscillator.start(startTime)
    oscillator.stop(endTime + 0.025)
  }

  private applyMasterGain(rampSeconds: number): void {
    const context = this.context
    const master = this.master
    if (!context || !master || context.state === 'closed') return
    const now = context.currentTime
    const target = this.isMuted ? 0 : this.masterVolume
    master.gain.cancelScheduledValues(now)
    master.gain.setValueAtTime(master.gain.value, now)
    master.gain.linearRampToValueAtTime(target, now + Math.max(0.005, rampSeconds))
  }

  private attachVisibilityListener(): void {
    if (!this.pauseWhenHidden || this.listenerAttached || typeof document === 'undefined') return
    document.addEventListener('visibilitychange', this.onVisibilityChange)
    this.listenerAttached = true
  }

  private detachVisibilityListener(): void {
    if (!this.listenerAttached || typeof document === 'undefined') return
    document.removeEventListener('visibilitychange', this.onVisibilityChange)
    this.listenerAttached = false
  }

  private readonly onVisibilityChange = (): void => {
    const context = this.context
    if (!context || this.state === 'disposed') return
    if (document.hidden) {
      if (context.state === 'running' && !this.manuallySuspended) {
        this.hiddenSuspended = true
        void context.suspend().then(() => {
          if (this.state !== 'disposed') this.state = 'paused'
        }).catch(() => undefined)
      }
      return
    }
    if (this.hiddenSuspended && !this.manuallySuspended) {
      this.hiddenSuspended = false
      void context.resume().then(() => {
        if (context.state === 'running' && this.state !== 'disposed') {
          this.state = 'running'
          this.applyMasterGain(0.18)
        }
      }).catch(() => { this.state = 'paused' })
    }
  }
}

function vibrateCue(name: AudioCue): void {
  if (typeof navigator === 'undefined' || typeof navigator.vibrate !== 'function') return
  if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return
  if (name === 'select') navigator.vibrate(8)
  else if (name === 'success') navigator.vibrate([10, 22, 14])
  else if (name === 'warning') navigator.vibrate([18, 32, 18])
  else if (name === 'error') navigator.vibrate([22, 28, 22])
  else if (name === 'connect') navigator.vibrate(10)
  else if (name === 'disconnect') navigator.vibrate([12, 24, 10])
}

function navigationTone(direction: NavigationDirection): { start: number; end: number; pan: number } {
  switch (direction) {
    case 'forward': return { start: 349.23, end: 415.3, pan: 0 }
    case 'backward': return { start: 349.23, end: 293.66, pan: 0 }
    case 'left': return { start: 329.63, end: 369.99, pan: -0.32 }
    case 'right': return { start: 329.63, end: 369.99, pan: 0.32 }
    case 'up': return { start: 392, end: 466.16, pan: 0 }
    case 'down': return { start: 329.63, end: 261.63, pan: 0 }
    default: return { start: 329.63, end: 369.99, pan: 0 }
  }
}

export function createAudioEngine(options?: AudioEngineOptions): AudioEngine {
  return new ProceduralAudioEngine(options)
}
