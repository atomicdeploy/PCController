export const dashboardCardIDs = ['telemetry', 'outputs', 'overview', 'actions', 'events'] as const
export type DashboardCardID = typeof dashboardCardIDs[number]

export interface DashboardLayout {
  order: DashboardCardID[]
  collapsed: DashboardCardID[]
  hidden: DashboardCardID[]
}

const storageKey = `${__PRODUCT_PROTOCOL__}.dashboard-layout.v1`

export const defaultDashboardLayout = (): DashboardLayout => ({ order: [...dashboardCardIDs], collapsed: [], hidden: [] })

export function normalizeDashboardLayout(value: Partial<DashboardLayout> | null | undefined): DashboardLayout {
  const present = new Set<DashboardCardID>()
  const order = (value?.order ?? []).filter((id): id is DashboardCardID => {
    if (!dashboardCardIDs.includes(id as DashboardCardID) || present.has(id as DashboardCardID)) return false
    present.add(id as DashboardCardID)
    return true
  })
  order.push(...dashboardCardIDs.filter((id) => !present.has(id)))
  const select = (items: readonly string[] | undefined) => [...new Set((items ?? []).filter((id): id is DashboardCardID => dashboardCardIDs.includes(id as DashboardCardID)))]
  return { order, collapsed: select(value?.collapsed), hidden: select(value?.hidden) }
}

export function loadDashboardLayout(storage: Pick<Storage, 'getItem'> | null = typeof localStorage === 'undefined' ? null : localStorage): DashboardLayout {
  try { return normalizeDashboardLayout(JSON.parse(storage?.getItem(storageKey) ?? 'null')) } catch { return defaultDashboardLayout() }
}

export function saveDashboardLayout(layout: DashboardLayout, storage: Pick<Storage, 'setItem'> | null = typeof localStorage === 'undefined' ? null : localStorage): void {
  try { storage?.setItem(storageKey, JSON.stringify(normalizeDashboardLayout(layout))) } catch { /* private storage is optional */ }
}

export function moveDashboardCard(layout: DashboardLayout, source: DashboardCardID, target: DashboardCardID): DashboardLayout {
  if (source === target) return layout
  const order = layout.order.filter((id) => id !== source)
  order.splice(Math.max(0, order.indexOf(target)), 0, source)
  return { ...layout, order }
}

export function toggleDashboardCard(items: DashboardCardID[], id: DashboardCardID): DashboardCardID[] {
  return items.includes(id) ? items.filter((item) => item !== id) : [...items, id]
}
