import glyphs from './channel-icon-glyphs.json'
import './channel-icon.css'

const aliases: Record<string, string> = {
  window: 'app-window', chandelier: 'lamp', loostr: 'lamp', 'stage-light': 'lighthouse',
  garden: 'tree', folder: 'folder-open', speaker: 'speaker-high', settings: 'gear', led: 'circle',
}

export function channelIconGlyph(icon: string): string | undefined {
  const key = icon.trim().toLowerCase()
  return (glyphs as Record<string, string>)[aliases[key] ?? key]
}

// Same Phosphor regular font/codepoints as Pealayer, served locally/offline.
// Unknown IDs stay visible rather than silently becoming unrelated artwork.
export function ChannelIcon({ icon }: { icon?: string }) {
  if (!icon?.trim()) return null
  const glyph = channelIconGlyph(icon)
  return <span className={glyph ? 'channel-icon' : 'channel-icon-name'} title={icon} aria-hidden="true">{glyph ?? `[${icon}]`}</span>
}
