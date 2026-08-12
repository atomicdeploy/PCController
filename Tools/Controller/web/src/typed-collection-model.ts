export type CollectionDirection = 'ltr' | 'rtl'
export type CollectionSortDirection = 'asc' | 'desc'

export interface CollectionSort {
  field: string
  direction: CollectionSortDirection
}

export interface CollectionFieldDefinition<T extends object> {
  id: string
  icon?: 'time' | 'type' | 'message' | 'source' | 'metadata'
  label?: string
  get?: (row: T) => unknown
  visible?: boolean
  width?: number
  sortable?: boolean
}

export interface CollectionField<T extends object> extends CollectionFieldDefinition<T> {
  label: string
  visible: boolean
  width: number
  sortable: boolean
}

export interface CollectionEntry<T extends object> {
  key: string
  row: T
  sourceIndex: number
}

export interface GridFocus {
  area: 'header' | 'row'
  field: number
  row: number
}

const minimumColumnWidth = 88
const maximumColumnWidth = 520
const maximumStructuredDepth = 8
const maximumStructuredEntries = 500

function isObjectRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function primitiveText(value: unknown): string | null {
  if (value === null) return 'null'
  if (value === undefined) return 'Not set'
  if (typeof value === 'string') return value
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'number') {
    if (Number.isNaN(value)) return 'NaN'
    if (value === Number.POSITIVE_INFINITY) return '+Infinity'
    if (value === Number.NEGATIVE_INFINITY) return '-Infinity'
    if (Object.is(value, -0)) return '-0'
    return value.toString()
  }
  if (typeof value === 'bigint') return `${value.toString()}n`
  if (typeof value === 'symbol') return value.description ? `Symbol(${value.description})` : 'Symbol'
  if (typeof value === 'function') return value.name ? `Function(${value.name})` : 'Function'
  return null
}

function safeKeys(value: object): string[] | null {
  try {
    return Object.keys(value)
  } catch {
    return null
  }
}

function safeRead(value: object, key: string): unknown {
  try {
    return Reflect.get(value, key)
  } catch {
    return '[unreadable]'
  }
}

/**
 * Produces a compact human label without relying on an object's implicit
 * string conversion. The same labels are used by cells and disclosure rows.
 */
export function structuredValueSummary(value: unknown): string {
  const primitive = primitiveText(value)
  if (primitive !== null) return primitive
  if (value instanceof Date) {
    const timestamp = value.getTime()
    return Number.isFinite(timestamp) ? value.toISOString() : 'Invalid date'
  }
  if (Array.isArray(value)) return `Array · ${value.length} item${value.length === 1 ? '' : 's'}`
  if (isObjectRecord(value)) {
    const keys = safeKeys(value)
    if (keys === null) return 'Unreadable object'
    return `Object · ${keys.length} field${keys.length === 1 ? '' : 's'}`
  }
  return 'Unsupported value'
}

function jsonCompatible(
  value: unknown,
  ancestors: Set<object>,
  depth: number,
  budget: { remaining: number },
): unknown {
  const primitive = primitiveText(value)
  if (primitive !== null) {
    if (value === null || typeof value === 'string' || typeof value === 'boolean') return value
    if (typeof value === 'number' && Number.isFinite(value)) return value
    return primitive
  }
  if (value instanceof Date) return structuredValueSummary(value)
  if (typeof value !== 'object' || value === null) return 'Unsupported value'
  if (ancestors.has(value)) return '[circular]'
  if (depth >= maximumStructuredDepth) return '[depth limit]'
  if (budget.remaining <= 0) return '[entry limit]'

  const nextAncestors = new Set(ancestors)
  nextAncestors.add(value)
  if (Array.isArray(value)) {
    const result: unknown[] = []
    for (const item of value) {
      if (budget.remaining <= 0) {
        result.push('[entry limit]')
        break
      }
      budget.remaining -= 1
      result.push(jsonCompatible(item, nextAncestors, depth + 1, budget))
    }
    return result
  }

  const keys = safeKeys(value)
  if (keys === null) return '[unreadable object]'
  const result: Record<string, unknown> = Object.create(null) as Record<string, unknown>
  for (const key of keys) {
    if (budget.remaining <= 0) {
      result['…'] = '[entry limit]'
      break
    }
    budget.remaining -= 1
    result[key] = jsonCompatible(safeRead(value, key), nextAncestors, depth + 1, budget)
  }
  return result
}

/** Converts a value without invoking object stringification or custom toJSON methods. */
export function structuredJSONValue(value: unknown): unknown {
  return jsonCompatible(value, new Set(), 0, { remaining: maximumStructuredEntries })
}

/** Serializes JSON-compatible data safely, including cycles and non-JSON scalars. */
export function structuredJSONStringify(value: unknown, pretty = false): string {
  const compatible = structuredJSONValue(value)
  try {
    return JSON.stringify(compatible, null, pretty ? 2 : 0) ?? 'null'
  } catch {
    return '"[unserializable]"'
  }
}

/** Recursive text used for filtering; complex values remain structured JSON. */
export function structuredSearchText(value: unknown): string {
  const primitive = primitiveText(value)
  return primitive === null ? structuredJSONStringify(value) : primitive
}

export function fieldValue<T extends object>(field: CollectionFieldDefinition<T>, row: T): unknown {
  try {
    if (field.get) return field.get(row)
    if (isObjectRecord(row)) return safeRead(row, field.id)
  } catch {
    return '[unreadable]'
  }
  return undefined
}

function defaultWidth(id: string): number {
  if (/metadata|details|message|description|reason|title/i.test(id)) return 260
  if (/time|date|updated|created/i.test(id)) return 190
  return 156
}

function normalizeWidth(value: number | undefined, id: string): number {
  if (!Number.isFinite(value)) return defaultWidth(id)
  return Math.min(maximumColumnWidth, Math.max(minimumColumnWidth, Math.round(value!)))
}

export function discoverCollectionFields<T extends object>(
  records: readonly T[],
  preferred: readonly string[] = [],
): CollectionFieldDefinition<T>[] {
  const discovered = new Set<string>()
  for (const row of records) {
    if (!isObjectRecord(row)) continue
    const keys = safeKeys(row)
    if (keys === null) continue
    for (const key of keys) discovered.add(key)
  }
  const ordered = [
    ...preferred.filter((key) => discovered.delete(key)),
    ...discovered,
  ]
  return ordered.map((id) => ({ id, label: id.replaceAll('_', ' ') }))
}

/** Preserves user order/visibility/width while appending newly discovered fields. */
export function reconcileFieldRegistry<T extends object>(
  previous: readonly CollectionField<T>[],
  definitions: readonly CollectionFieldDefinition<T>[],
): CollectionField<T>[] {
  const incoming = new Map(definitions.map((field) => [field.id, field]))
  const result: CollectionField<T>[] = []
  const seen = new Set<string>()
  for (const existing of previous) {
    const definition = incoming.get(existing.id)
    result.push({
      ...existing,
      ...(definition ?? {}),
      id: existing.id,
      label: definition?.label?.trim() || existing.label,
      visible: existing.visible,
      width: existing.width,
      sortable: definition?.sortable ?? existing.sortable,
    })
    seen.add(existing.id)
  }
  for (const definition of definitions) {
    if (!definition.id || seen.has(definition.id)) continue
    result.push({
      ...definition,
      label: definition.label?.trim() || definition.id.replaceAll('_', ' '),
      visible: definition.visible !== false,
      width: normalizeWidth(definition.width, definition.id),
      sortable: definition.sortable !== false,
    })
    seen.add(definition.id)
  }
  return result
}

function compareValues(left: unknown, right: unknown, collator: Intl.Collator): number {
  if (left === right) return 0
  if (left === undefined || left === null) return 1
  if (right === undefined || right === null) return -1
  if (typeof left === 'number' && typeof right === 'number') {
    if (Number.isNaN(left)) return Number.isNaN(right) ? 0 : 1
    if (Number.isNaN(right)) return -1
    return left - right
  }
  if (typeof left === 'boolean' && typeof right === 'boolean') return Number(left) - Number(right)
  return collator.compare(structuredSearchText(left), structuredSearchText(right))
}

export function sortCollectionEntries<T extends object>(
  entries: readonly CollectionEntry<T>[],
  fields: readonly CollectionFieldDefinition<T>[],
  sort: CollectionSort | null,
  locale: string,
): CollectionEntry<T>[] {
  if (sort === null) return [...entries]
  const field = fields.find((candidate) => candidate.id === sort.field)
  if (!field || field.sortable === false) return [...entries]
  const collator = new Intl.Collator(locale, { numeric: true, sensitivity: 'base' })
  const direction = sort.direction === 'asc' ? 1 : -1
  return entries
    .map((entry, stableIndex) => ({ entry, stableIndex }))
    .sort((left, right) => {
      const compared = compareValues(fieldValue(field, left.entry.row), fieldValue(field, right.entry.row), collator)
      return compared === 0 ? left.stableIndex - right.stableIndex : compared * direction
    })
    .map(({ entry }) => entry)
}

export function filterCollectionEntries<T extends object>(
  entries: readonly CollectionEntry<T>[],
  fields: readonly CollectionFieldDefinition<T>[],
  query: string,
  locale: string,
): CollectionEntry<T>[] {
  const term = query.trim().toLocaleLowerCase(locale)
  if (!term) return [...entries]
  return entries.filter(({ row }) => fields.some((field) =>
    structuredSearchText(fieldValue(field, row)).toLocaleLowerCase(locale).includes(term)))
}

function csvValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  const primitive = primitiveText(value)
  return primitive === null ? structuredJSONStringify(value) : primitive
}

function escapeCSV(value: string): string {
  return /[",\r\n]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value
}

export function collectionToCSV<T extends object>(
  rows: readonly T[],
  fields: readonly CollectionFieldDefinition<T>[],
): string {
  const header = fields.map((field) => escapeCSV(field.id)).join(',')
  const body = rows.map((row) => fields.map((field) => escapeCSV(csvValue(fieldValue(field, row)))).join(','))
  return [header, ...body].join('\r\n')
}

export function collectionToJSON<T extends object>(rows: readonly T[]): string {
  return structuredJSONStringify(rows, true)
}

/** Computes the scroll correction that keeps the prior anchor at the same offset. */
export function anchoredScrollTop(currentScrollTop: number, previousOffset: number, nextOffset: number): number {
  return Math.max(0, currentScrollTop + nextOffset - previousOffset)
}

/** Retains the nearest valid viewport position when a reconnect replaces every row key. */
export function reconnectedScrollTop(previousScrollTop: number, scrollHeight: number, clientHeight: number): number {
  return Math.max(0, Math.min(previousScrollTop, Math.max(0, scrollHeight - clientHeight)))
}

export function isKeyboardContextGesture(event: Pick<KeyboardEvent, 'key' | 'shiftKey'>): boolean {
  return event.key === 'ContextMenu' || event.key === 'Apps' || (event.key === 'F10' && event.shiftKey)
}

/** Pure roving-focus navigation shared by the grid and keyboard contract tests. */
export function nextGridFocus(
  current: GridFocus,
  key: string,
  rowCount: number,
  fieldCount: number,
  direction: CollectionDirection,
  ctrlKey = false,
): GridFocus {
  const maximumField = Math.max(0, fieldCount - 1)
  const maximumRow = Math.max(0, rowCount - 1)
  if (ctrlKey && key === 'Home') return { area: 'header', field: 0, row: 0 }
  if (ctrlKey && key === 'End' && rowCount > 0) return { area: 'row', field: maximumField, row: maximumRow }
  if (key === 'Home') return { ...current, field: 0 }
  if (key === 'End') return { ...current, field: maximumField }
  if (key === 'ArrowLeft' || key === 'ArrowRight') {
    const visualDelta = key === 'ArrowRight' ? 1 : -1
    const delta = direction === 'rtl' ? -visualDelta : visualDelta
    return { ...current, field: Math.max(0, Math.min(maximumField, current.field + delta)) }
  }
  if (key === 'ArrowDown') {
    if (rowCount === 0) return current
    if (current.area === 'header') return { area: 'row', field: current.field, row: 0 }
    return { ...current, row: Math.min(maximumRow, current.row + 1) }
  }
  if (key === 'ArrowUp') {
    if (current.area === 'row' && current.row === 0) return { area: 'header', field: current.field, row: 0 }
    if (current.area === 'row') return { ...current, row: Math.max(0, current.row - 1) }
  }
  return current
}

export function clampColumnWidth(value: number): number {
  return Math.min(maximumColumnWidth, Math.max(minimumColumnWidth, Math.round(value)))
}
