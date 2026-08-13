export type BuzzerPath = 'board' | 'host' | 'both' | 'none'

export interface BuzzerPlaybackInput {
  source: string
  frequencyHz: number
  durationMS: number
  deviceMicros?: number
}

export interface BuzzerPlaybackPlan {
  delayMS: number
  durationMS: number
  audible: boolean
  stop: boolean
}

interface BuzzerTimelineAnchor {
  deviceMicros: number
  startMS: number
}

const maxBuzzerSourceGapUS = 5 * 60 * 1_000_000
const webAudioLookaheadMS = 8

// Maps each board's wrapping 32-bit MCU clock onto the browser's monotonic
// clock. Pauses advance the same timeline without creating an oscillator, and
// late notes are shortened or dropped instead of shifting every later note.
export class BuzzerPlaybackTimeline {
  private readonly anchors = new Map<string, BuzzerTimelineAnchor>()

  plan(input: BuzzerPlaybackInput, nowMS: number): BuzzerPlaybackPlan | null {
    if (!Number.isFinite(nowMS) || !Number.isFinite(input.frequencyHz) || input.frequencyHz < 0 ||
        !Number.isFinite(input.durationMS) || input.durationMS < 0 || input.durationMS > 60_000 ||
        (input.durationMS === 0 && input.frequencyHz !== 0)) return null
    let startMS = nowMS + webAudioLookaheadMS
    if (input.deviceMicros !== undefined) {
      if (!Number.isInteger(input.deviceMicros) || input.deviceMicros < 0 || input.deviceMicros > 0xFFFF_FFFF) return null
      const currentMicros = input.deviceMicros >>> 0
      const previous = this.anchors.get(input.source)
      if (previous) {
        const deltaUS = (currentMicros - previous.deviceMicros) >>> 0
        if (deltaUS <= maxBuzzerSourceGapUS) startMS = previous.startMS + deltaUS / 1000
      }
      this.anchors.set(input.source, { deviceMicros: currentMicros, startMS })
    }
    const endMS = startMS + input.durationMS
    const effectiveStartMS = Math.max(startMS, nowMS)
    const durationMS = Math.max(0, endMS - effectiveStartMS)
    return {
      delayMS: Math.max(0, startMS - nowMS),
      durationMS,
      audible: input.frequencyHz >= 20 && input.frequencyHz <= 20_000 && durationMS >= 1,
      stop: input.frequencyHz === 0,
    }
  }
}

export function buzzerPathFromState(boardSilent: boolean, hostSilent: boolean): BuzzerPath {
  if (!boardSilent && !hostSilent) return 'both'
  if (!boardSilent) return 'board'
  if (!hostSilent) return 'host'
  return 'none'
}
