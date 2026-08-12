import type { SelectOption } from './components'

export interface ConfiguredMelodyNote {
  frequency_hz: number
  duration_ms: number
  gap_ms?: number
}

export interface ConfiguredMelody {
  name: string
  notes: readonly ConfiguredMelodyNote[]
}

export function normalizeConfiguredMelodies(value: unknown): ConfiguredMelody[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((candidate) => {
    if (!candidate || typeof candidate !== 'object') return []
    const record = candidate as Record<string, unknown>
    const name = typeof record.name === 'string' ? record.name.trim() : ''
    if (!name || !Array.isArray(record.notes)) return []
    const notes = record.notes.flatMap((note) => {
      if (!note || typeof note !== 'object') return []
      const fields = note as Record<string, unknown>
      const frequency = Number(fields.frequency_hz)
      const duration = Number(fields.duration_ms)
      const gap = fields.gap_ms === undefined ? 0 : Number(fields.gap_ms)
      const frequencyIsValid = frequency === 0 || (frequency >= 20 && frequency <= 20_000)
      if (!Number.isFinite(frequency) || !Number.isFinite(duration) || !Number.isFinite(gap) ||
        !frequencyIsValid || duration < 1 || duration > 5_000 || gap < 0 || gap > 5_000) return []
      return [{ frequency_hz: Math.round(frequency), duration_ms: Math.round(duration), gap_ms: Math.round(gap) }]
    })
    if (notes.length !== record.notes.length) return []
    return [{ name, notes }]
  })
}

export function configuredMelodyOptions(catalog: readonly ConfiguredMelody[], locale: 'en' | 'fa'): SelectOption[] {
  return catalog
    .filter((melody) => melody.name.trim() !== '')
    .map((melody) => {
      const duration = melody.notes.reduce((total, note) => total + note.duration_ms + (note.gap_ms ?? 0), 0)
      return {
        value: melody.name,
        label: melody.name,
        detail: locale === 'fa'
          ? `${melody.notes.length} نت · ${duration} میلی‌ثانیه`
          : `${melody.notes.length} notes · ${duration} ms`,
      }
    })
}
