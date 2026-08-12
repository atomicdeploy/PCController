export const quickHeaderControlIDs = ['theme', 'language', 'audio', 'hotkeys', 'notifications'] as const
export type QuickHeaderControlID = typeof quickHeaderControlIDs[number]
export type QuickHeaderPreferences = Record<QuickHeaderControlID, boolean>

export const defaultQuickHeaderPreferences: QuickHeaderPreferences = {
  theme: true,
  language: true,
  audio: true,
  hotkeys: true,
  notifications: true,
}

const storageKey = 'pccontroller.quick-header-controls.v1'

export function normalizeQuickHeaderPreferences(value: unknown): QuickHeaderPreferences {
  const candidate = value && typeof value === 'object' ? value as Partial<QuickHeaderPreferences> : {}
  return Object.fromEntries(quickHeaderControlIDs.map((id) => [id, typeof candidate[id] === 'boolean' ? candidate[id] : defaultQuickHeaderPreferences[id]])) as QuickHeaderPreferences
}

export function loadQuickHeaderPreferences(): QuickHeaderPreferences {
  try { return normalizeQuickHeaderPreferences(JSON.parse(window.localStorage.getItem(storageKey) ?? 'null')) } catch { return { ...defaultQuickHeaderPreferences } }
}

export function saveQuickHeaderPreferences(value: QuickHeaderPreferences): void {
  try { window.localStorage.setItem(storageKey, JSON.stringify(value)) } catch { /* private storage may be unavailable */ }
}
