export interface CardLayout<CardID extends string = string> {
  order: CardID[]
  collapsed: CardID[]
  hidden: CardID[]
}

export type CardLayoutSurface = 'dashboard' | 'workbench'

function storageKey(surface: CardLayoutSurface): string {
  return `${__PRODUCT_PROTOCOL__}.${surface}-layout`
}

export function defaultCardLayout<CardID extends string>(allowedIDs: readonly CardID[]): CardLayout<CardID> {
  return { order: [...allowedIDs], collapsed: [], hidden: [] }
}

export function normalizeCardLayout<CardID extends string>(
  value: Partial<CardLayout<string>> | null | undefined,
  allowedIDs: readonly CardID[],
): CardLayout<CardID> {
  const allowed = new Set<string>(allowedIDs)
  const unique = (items: readonly string[] | undefined): CardID[] => {
    const seen = new Set<string>()
    return (items ?? []).filter((id): id is CardID => {
      if (!allowed.has(id) || seen.has(id)) return false
      seen.add(id)
      return true
    })
  }
  const order = unique(value?.order)
  order.push(...allowedIDs.filter((id) => !order.includes(id)))
  return {
    order,
    collapsed: unique(value?.collapsed),
    hidden: unique(value?.hidden),
  }
}

export function loadCardLayout<CardID extends string>(
  surface: CardLayoutSurface,
  allowedIDs: readonly CardID[],
  storage: Pick<Storage, 'getItem'> | null = typeof localStorage === 'undefined' ? null : localStorage,
): CardLayout<CardID> {
  try {
    return normalizeCardLayout(JSON.parse(storage?.getItem(storageKey(surface)) ?? 'null'), allowedIDs)
  } catch {
    return defaultCardLayout(allowedIDs)
  }
}

export function saveCardLayout<CardID extends string>(
  surface: CardLayoutSurface,
  allowedIDs: readonly CardID[],
  layout: CardLayout<CardID>,
  storage: Pick<Storage, 'setItem'> | null = typeof localStorage === 'undefined' ? null : localStorage,
): void {
  try {
    storage?.setItem(storageKey(surface), JSON.stringify(normalizeCardLayout(layout, allowedIDs)))
  } catch {
    // Browser storage is optional; the in-memory layout remains usable.
  }
}

export function resetCardLayout(
  surface: CardLayoutSurface,
  storage: Pick<Storage, 'removeItem'> | null = typeof localStorage === 'undefined' ? null : localStorage,
): void {
  try {
    storage?.removeItem(storageKey(surface))
  } catch {
    // Browser storage is optional.
  }
}

export function moveCard<CardID extends string>(layout: CardLayout<CardID>, source: CardID, target: CardID): CardLayout<CardID> {
  if (source === target) return layout
  const sourceIndex = layout.order.indexOf(source)
  const targetIndex = layout.order.indexOf(target)
  if (sourceIndex < 0 || targetIndex < 0) return layout
  const order = layout.order.filter((id) => id !== source)
  const insertion = order.indexOf(target) + (sourceIndex < targetIndex ? 1 : 0)
  order.splice(Math.max(0, insertion), 0, source)
  return { ...layout, order }
}

export function toggleCard<CardID extends string>(items: CardID[], id: CardID): CardID[] {
  return items.includes(id) ? items.filter((item) => item !== id) : [...items, id]
}
