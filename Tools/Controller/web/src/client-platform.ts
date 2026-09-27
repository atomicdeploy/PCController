/** Returns the primary modifier displayed for this browser's operating system. */
export function primaryShortcutModifier(platform?: string): 'Ctrl' | '⌘' {
  const browser = typeof navigator === 'undefined' ? undefined : navigator as Navigator & { userAgentData?: { platform?: string } }
  const detected = platform ?? `${browser?.userAgentData?.platform ?? ''} ${browser?.platform ?? ''} ${browser?.userAgent ?? ''}`
  return /mac|iphone|ipad|ipod/i.test(detected) ? '⌘' : 'Ctrl'
}

export function primaryShortcutARIA(platform?: string): 'Control+K' | 'Meta+K' {
  return primaryShortcutModifier(platform) === '⌘' ? 'Meta+K' : 'Control+K'
}
