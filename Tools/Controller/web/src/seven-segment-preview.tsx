import type { FrontPanelState } from './types'

const segmentLines = [
  ['a', 8, 5, 28, 5], ['b', 31, 8, 31, 27], ['c', 31, 32, 31, 51],
  ['d', 8, 54, 28, 54], ['e', 5, 32, 5, 51], ['f', 5, 8, 5, 27],
  ['g', 8, 29.5, 28, 29.5],
] as const

export function SevenSegmentPreview({
  panel,
  label = 'Live four-digit display preview',
}: {
  panel?: FrontPanelState
  label?: string
}) {
  const raw = panel?.raw_segments ?? [0, 0, 0, 0]
  return (
    <div className={`live-segment-preview${panel?.segments_active ? ' is-active' : ''}`} dir="ltr" aria-label={label}>
      {raw.map((mask, index) => (
        <svg key={index} viewBox="0 0 40 62" role="img" aria-label={`digit ${index + 1} raw 0x${mask.toString(16).padStart(2, '0')}`}>
          {segmentLines.map(([name, x1, y1, x2, y2], bit) => (
            <line key={name} x1={x1} y1={y1} x2={x2} y2={y2} className={(mask & (1 << bit)) !== 0 ? 'is-lit' : ''} />
          ))}
          <circle cx="36" cy="54" r="2.2" className={(mask & 0x80) !== 0 ? 'is-lit' : ''} />
        </svg>
      ))}
    </div>
  )
}
