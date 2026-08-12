export const pageOrder = [
  'dashboard',
  'controls',
  'workbench',
  'device',
  'data',
  'updates',
  'events',
  'settings',
] as const

export type PageID = (typeof pageOrder)[number]

type ShortcutEvent = Pick<
  KeyboardEvent,
  'altKey' | 'ctrlKey' | 'defaultPrevented' | 'isComposing' | 'key' | 'keyCode' | 'metaKey' | 'repeat' | 'shiftKey' | 'target'
>

const editableSelector = [
  'input',
  'textarea',
  'select',
  '[contenteditable]:not([contenteditable="false"])',
  '[role="textbox"]',
  '[role="combobox"]',
].join(',')

export function isEditableTarget(target: EventTarget | null): boolean {
  if (!target) return false
  const element = target as EventTarget & {
    closest?: (selector: string) => unknown
    isContentEditable?: boolean
  }
  if (element.isContentEditable) return true
  return typeof element.closest === 'function' && Boolean(element.closest(editableSelector))
}

function isCommandPaletteTarget(target: EventTarget | null): boolean {
  const element = target as EventTarget & { closest?: (selector: string) => unknown }
  return typeof element?.closest === 'function' && Boolean(element.closest('.palette'))
}

export function ignoresGlobalHotkeys(event: ShortcutEvent, paletteOpen = false): boolean {
  if (event.defaultPrevented || event.isComposing || event.keyCode === 229) return true
  if (!isEditableTarget(event.target)) return false
  // The palette owns navigation keys only while focus is inside its own input.
  // Editable controls elsewhere, especially the session token field, always
  // retain every keystroke even if another layer was left open.
  return !(paletteOpen && isCommandPaletteTarget(event.target))
}

export function pageFromAppAction(value: string | undefined): PageID | null {
  const page = (value ?? '').trim().toLowerCase().replace(/[\s_]+/g, '-')
  if (!page) return null
  const aliases: Record<string, PageID> = {
    '0': 'controls',
    '1': 'dashboard',
    '2': 'controls',
    '3': 'controls',
    '4': 'controls',
    '5': 'settings',
    '6': 'controls',
    '7': 'controls',
    '8': 'controls',
    '9': 'events',
    app: 'settings',
    automate: 'workbench',
    automation: 'workbench',
    automations: 'workbench',
    board: 'workbench',
    catalog: 'data',
    console: 'workbench',
    control: 'controls',
    controls: 'controls',
    dashboard: 'dashboard',
    event: 'events',
    events: 'events',
    data: 'data',
    'data-hub': 'data',
    datahub: 'data',
    device: 'device',
    devices: 'device',
    export: 'data',
    firmware: 'updates',
    flash: 'updates',
    history: 'events',
    home: 'dashboard',
    'local-device': 'device',
    live: 'dashboard',
    i2c: 'workbench',
    macros: 'workbench',
    menus: 'workbench',
    output: 'controls',
    outputs: 'controls',
    records: 'data',
    recovery: 'updates',
    program: 'updates',
    programming: 'updates',
    peripherals: 'workbench',
    rf: 'workbench',
    settings: 'settings',
    timeline: 'events',
    terminal: 'workbench',
    workbench: 'workbench',
    update: 'updates',
    updates: 'updates',
  }
  return aliases[page] ?? null
}

export function isFreshAppAction(time: string | undefined, now = Date.now(), maxAgeMS = 10_000): boolean {
  const observed = Date.parse(time ?? '')
  return Number.isFinite(observed) && observed <= now + 1_000 && now - observed <= maxAgeMS
}

export function pageFromNumberHotkey(event: ShortcutEvent): PageID | null {
  if (!event.altKey || event.ctrlKey || event.metaKey || event.shiftKey || event.repeat) return null
  const index = Number(event.key) - 1
  return Number.isInteger(index) && index >= 0 && index < pageOrder.length ? pageOrder[index] : null
}

export function adjacentPageHotkey(
  event: ShortcutEvent,
  current: PageID,
  direction: 'ltr' | 'rtl',
): PageID | null {
  if (!(event.ctrlKey || event.metaKey) || !event.shiftKey || event.altKey) return null
  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return null
  const visualForward = direction === 'rtl' ? event.key === 'ArrowLeft' : event.key === 'ArrowRight'
  const offset = visualForward ? 1 : -1
  const currentIndex = pageOrder.indexOf(current)
  return pageOrder[(currentIndex + offset + pageOrder.length) % pageOrder.length]
}

export function pageFromGoChord(key: string): PageID | null {
  const pages: Record<string, PageID> = {
    c: 'controls',
    b: 'workbench',
    d: 'dashboard',
    e: 'events',
    v: 'device',
    w: 'data',
    u: 'updates',
    s: 'settings',
  }
  return pages[key.toLowerCase()] ?? null
}
