import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  ArrowDownAZ,
  ArrowUpAZ,
  Check,
  ChevronLeft,
  ChevronRight,
  Clipboard,
  Columns3,
  Download,
  Eye,
  EyeOff,
  GripVertical,
  Search,
  X,
} from 'lucide-react'
import {
  anchoredScrollTop,
  clampColumnWidth,
  collectionToCSV,
  collectionToJSON,
  discoverCollectionFields,
  fieldValue,
  filterCollectionEntries,
  isKeyboardContextGesture,
  nextGridFocus,
  reconnectedScrollTop,
  reconcileFieldRegistry,
  sortCollectionEntries,
  structuredJSONStringify,
  structuredJSONValue,
  structuredValueSummary,
  type CollectionDirection,
  type CollectionEntry,
  type CollectionField,
  type CollectionFieldDefinition,
  type CollectionSort,
  type GridFocus,
} from './typed-collection-model'

export interface TypedCollectionLabels {
  search: string
  columns: string
  export: string
  exportAllJSON: string
  exportFilteredJSON: string
  exportAllCSV: string
  exportFilteredCSV: string
  selected: string
  clearSelection: string
  clearFilter: string
  noRecords: string
  noMatches: string
  copyCell: string
  copyRow: string
  copyColumn: string
  selectRow: string
  deselectRow: string
  sortAscending: string
  sortDescending: string
  hideColumn: string
  showColumn: string
  moveEarlier: string
  moveLater: string
  resizeColumn: string
  previousWindow: string
  nextWindow: string
  goTo: string
  showing: string
  of: string
  copied: string
  copyUnavailable: string
  filtered: string
  all: string
  structuredValue: string
}

export interface TypedCollectionProps<T extends object> {
  records: readonly T[]
  fields?: readonly CollectionFieldDefinition<T>[]
  preferredFields?: readonly string[]
  locale: string
  direction?: CollectionDirection
  ariaLabel: string
  labels?: Partial<TypedCollectionLabels>
  rowKey?: (row: T, index: number) => string
  rowClassName?: (row: T) => string | undefined
  renderCell?: (field: CollectionField<T>, value: unknown, row: T) => ReactNode | undefined
  windowSize?: number
  toolbar?: boolean
  filename?: string
  initialSort?: CollectionSort | null
  preserveViewportAnchor?: boolean
  className?: string
  empty?: ReactNode
}

interface MenuTarget {
  kind: 'header' | 'row'
  field: number
  row: number
  left: number
  top: number
}

interface AnchorSnapshot {
  key: string
  offset: number
  scrollTop: number
  nearStart: boolean
}

const englishLabels: TypedCollectionLabels = {
  search: 'Filter records', columns: 'Columns', export: 'Export',
  exportAllJSON: 'All · JSON', exportFilteredJSON: 'Filtered · JSON',
  exportAllCSV: 'All · CSV', exportFilteredCSV: 'Filtered · CSV',
  selected: 'selected', clearSelection: 'Clear selection', clearFilter: 'Clear filter',
  noRecords: 'No records are available.', noMatches: 'No records match this filter.',
  copyCell: 'Copy cell', copyRow: 'Copy row as JSON', copyColumn: 'Copy filtered column',
  selectRow: 'Select row', deselectRow: 'Deselect row', sortAscending: 'Sort ascending',
  sortDescending: 'Sort descending', hideColumn: 'Hide column', showColumn: 'Show column',
  moveEarlier: 'Move earlier', moveLater: 'Move later', resizeColumn: 'Resize column',
  previousWindow: 'Previous rows', nextWindow: 'Next rows', goTo: 'Go to', showing: 'Showing', of: 'of',
  copied: 'Copied to clipboard', copyUnavailable: 'Clipboard unavailable', filtered: 'filtered',
  all: 'all', structuredValue: 'Structured value',
}

const persianLabels: TypedCollectionLabels = {
  search: 'فیلتر رکوردها', columns: 'ستون‌ها', export: 'خروجی',
  exportAllJSON: 'همه · JSON', exportFilteredJSON: 'فیلترشده · JSON',
  exportAllCSV: 'همه · CSV', exportFilteredCSV: 'فیلترشده · CSV',
  selected: 'انتخاب‌شده', clearSelection: 'پاک‌کردن انتخاب', clearFilter: 'پاک‌کردن فیلتر',
  noRecords: 'رکوردی در دسترس نیست.', noMatches: 'هیچ رکوردی با این فیلتر هم‌خوانی ندارد.',
  copyCell: 'کپی سلول', copyRow: 'کپی رکورد به‌صورت JSON', copyColumn: 'کپی ستون فیلترشده',
  selectRow: 'انتخاب ردیف', deselectRow: 'برداشتن انتخاب ردیف', sortAscending: 'مرتب‌سازی صعودی',
  sortDescending: 'مرتب‌سازی نزولی', hideColumn: 'پنهان‌کردن ستون', showColumn: 'نمایش ستون',
  moveEarlier: 'انتقال به قبل', moveLater: 'انتقال به بعد', resizeColumn: 'تغییر عرض ستون',
  previousWindow: 'ردیف‌های قبلی', nextWindow: 'ردیف‌های بعدی', goTo: 'رفتن به', showing: 'نمایش', of: 'از',
  copied: 'در کلیپ‌بورد کپی شد', copyUnavailable: 'کلیپ‌بورد در دسترس نیست', filtered: 'فیلترشده',
  all: 'همه', structuredValue: 'مقدار ساخت‌یافته',
}

const noPreferredFields: readonly string[] = []

function isExpandableValue(value: unknown): boolean {
  return typeof value === 'object' && value !== null
}

function compatibleSummary(value: unknown): string {
  if (Array.isArray(value)) return `Array · ${value.length}`
  if (typeof value === 'object' && value !== null) return `Object · ${Object.keys(value).length}`
  if (value === null) return 'null'
  if (typeof value === 'string') return value
  if (typeof value === 'number') return value.toString()
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  return 'Not set'
}

function StructuredBranch({ value, depth }: { value: unknown; depth: number }) {
  if (Array.isArray(value)) {
    if (!value.length) return <span className="structured-value__empty">[]</span>
    return <ol className="structured-value__array">{value.map((item, index) => (
      <li key={index}><StructuredBranch value={item} depth={depth + 1} /></li>
    ))}</ol>
  }
  if (typeof value === 'object' && value !== null) {
    const entries = Object.entries(value)
    if (!entries.length) return <span className="structured-value__empty">{'{}'}</span>
    return <dl className="structured-value__object">{entries.map(([key, item]) => (
      <div key={key}><dt>{key}</dt><dd><StructuredBranch value={item} depth={depth + 1} /></dd></div>
    ))}</dl>
  }
  return <span className="structured-value__scalar" dir="auto">{compatibleSummary(value)}</span>
}

/** Expandable, recursively safe presentation for arbitrary cell values. */
export function StructuredValue({ value, label }: { value: unknown; label?: string }) {
  if (!isExpandableValue(value)) {
    return <span className="structured-value__scalar" dir="auto">{structuredValueSummary(value)}</span>
  }
  return (
    <details className="structured-value">
      <summary aria-label={label}>{structuredValueSummary(value)}</summary>
      <div className="structured-value__content" dir="auto">
        <StructuredBranch value={structuredJSONValue(value)} depth={0} />
      </div>
    </details>
  )
}

function safeRowKey<T extends object>(row: T, index: number, keyFactory?: (row: T, index: number) => string): string {
  if (keyFactory) {
    try {
      const key = keyFactory(row, index).trim()
      if (key) return key
    } catch {
      // Fall through to the deterministic local key.
    }
  }
  if (typeof row === 'object' && row !== null) {
    for (const candidate of ['id', 'code', 'key']) {
      let value: unknown
      try { value = Reflect.get(row, candidate) } catch { value = undefined }
      if (typeof value === 'string' && value) return `${candidate}:${value}`
      if (typeof value === 'number' && Number.isFinite(value)) return `${candidate}:${value.toString()}`
    }
  }
  return `row:${index}`
}

function collectionEntries<T extends object>(records: readonly T[], keyFactory?: (row: T, index: number) => string): CollectionEntry<T>[] {
  const counts = new Map<string, number>()
  return records.map((row, sourceIndex) => {
    const base = safeRowKey(row, sourceIndex, keyFactory)
    const occurrence = counts.get(base) ?? 0
    counts.set(base, occurrence + 1)
    return { key: occurrence === 0 ? base : `${base}#${occurrence}`, row, sourceIndex }
  })
}

function registryEqual<T extends object>(left: readonly CollectionField<T>[], right: readonly CollectionField<T>[]): boolean {
  return left.length === right.length && left.every((field, index) => {
    const other = right[index]
    return other !== undefined && field.id === other.id && field.label === other.label &&
      field.get === other.get && field.visible === other.visible && field.width === other.width &&
      field.sortable === other.sortable
  })
}

async function copyText(text: string): Promise<boolean> {
  try {
    const clipboard = globalThis.navigator?.clipboard
    if (!clipboard || typeof clipboard.writeText !== 'function') return false
    await clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

function safeFilename(value: string): string {
  const sanitized = value.trim().replace(/[^a-z0-9._-]+/gi, '-').replace(/^-+|-+$/g, '')
  return sanitized || 'records'
}

function clipboardValue(value: unknown, pretty = false): string {
  return isExpandableValue(value) ? structuredJSONStringify(value, pretty) : structuredValueSummary(value)
}

function downloadText(filename: string, content: string, mime: string): void {
  if (typeof document === 'undefined' || typeof URL.createObjectURL !== 'function') return
  const url = URL.createObjectURL(new Blob([content], { type: mime }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.hidden = true
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  globalThis.setTimeout(() => URL.revokeObjectURL(url), 0)
}

export function TypedCollection<T extends object>({
  records,
  fields: suppliedFields,
  preferredFields = noPreferredFields,
  locale,
  direction = locale.toLowerCase().startsWith('fa') ? 'rtl' : 'ltr',
  ariaLabel,
  labels: labelOverrides,
  rowKey,
  rowClassName,
  renderCell,
  windowSize: requestedWindowSize = 120,
  toolbar = true,
  filename = 'records',
  initialSort = null,
  preserveViewportAnchor = false,
  className = '',
  empty,
}: TypedCollectionProps<T>) {
  const labels = { ...(locale.toLowerCase().startsWith('fa') ? persianLabels : englishLabels), ...labelOverrides }
  const collectionID = useId().replaceAll(':', '')
  const windowSize = Math.max(20, Math.min(200, Math.round(requestedWindowSize)))
  const discovered = useMemo(
    () => suppliedFields ? [] : discoverCollectionFields(records, preferredFields),
    [records, preferredFields, suppliedFields],
  )
  const definitions = suppliedFields ?? discovered
  const [registry, setRegistry] = useState<CollectionField<T>[]>(() => reconcileFieldRegistry([], definitions))
  const effectiveRegistry = useMemo(
    () => reconcileFieldRegistry(registry, definitions),
    [definitions, registry],
  )

  useEffect(() => {
    setRegistry((current) => {
      const next = reconcileFieldRegistry(current, definitions)
      return registryEqual(current, next) ? current : next
    })
  }, [definitions])

  const visibleFields = useMemo(() => {
    const visible = effectiveRegistry.filter((field) => field.visible)
    return visible.length ? visible : effectiveRegistry.slice(0, 1)
  }, [effectiveRegistry])
  const entries = useMemo(() => collectionEntries(records, rowKey), [records, rowKey])
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState<CollectionSort | null>(initialSort)
  const [selected, setSelected] = useState<Set<string>>(() => new Set())
  const [windowStart, setWindowStart] = useState(0)
  const [focus, setFocus] = useState<GridFocus>({ area: 'header', field: 0, row: 0 })
  const [menu, setMenu] = useState<MenuTarget | null>(null)
  const [notice, setNotice] = useState('')
  const [draggingField, setDraggingField] = useState<string | null>(null)
  const viewportRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const anchorRef = useRef<AnchorSnapshot | null>(null)

  const allSorted = useMemo(
    () => sortCollectionEntries(entries, effectiveRegistry, sort, locale),
    [effectiveRegistry, entries, locale, sort],
  )
  const filtered = useMemo(
    () => filterCollectionEntries(allSorted, effectiveRegistry, query, locale),
    [allSorted, effectiveRegistry, locale, query],
  )
  const maximumWindowStart = Math.max(0, Math.floor(Math.max(0, filtered.length - 1) / windowSize) * windowSize)
  const boundedWindowStart = Math.min(windowStart, maximumWindowStart)
  const windowEntries = filtered.slice(boundedWindowStart, boundedWindowStart + windowSize)
  const windowEnd = boundedWindowStart + windowEntries.length

  useEffect(() => setWindowStart(0), [query, sort?.direction, sort?.field])
  useEffect(() => {
    if (windowStart > maximumWindowStart) setWindowStart(maximumWindowStart)
  }, [maximumWindowStart, windowStart])
  useEffect(() => {
    setFocus((current) => {
      const field = Math.max(0, Math.min(Math.max(0, visibleFields.length - 1), current.field))
      if (current.area === 'header' || filtered.length === 0) return { area: 'header', field, row: 0 }
      return { area: 'row', field, row: Math.min(filtered.length - 1, current.row) }
    })
  }, [filtered.length, visibleFields.length])
  useEffect(() => {
    const available = new Set(entries.map((entry) => entry.key))
    setSelected((current) => {
      const next = new Set([...current].filter((key) => available.has(key)))
      return next.size === current.size ? current : next
    })
  }, [entries])

  const captureAnchor = () => {
    if (!preserveViewportAnchor) return
    const viewport = viewportRef.current
    if (!viewport) return
    const viewportRect = viewport.getBoundingClientRect()
    const rows = [...viewport.querySelectorAll<HTMLElement>('[data-collection-row-key]')]
    const anchor = rows.find((row) => row.getBoundingClientRect().bottom > viewportRect.top + 1) ?? rows[0]
    if (!anchor) return
    anchorRef.current = {
      key: anchor.dataset.collectionRowKey ?? '',
      offset: anchor.getBoundingClientRect().top - viewportRect.top,
      scrollTop: viewport.scrollTop,
      nearStart: viewport.scrollTop <= 4,
    }
  }

  useLayoutEffect(() => {
    if (!preserveViewportAnchor) return
    const viewport = viewportRef.current
    const previous = anchorRef.current
    if (!viewport || !previous) {
      captureAnchor()
      return
    }
    if (previous.nearStart) {
      viewport.scrollTop = 0
    } else {
      const escaped = typeof CSS !== 'undefined' && typeof CSS.escape === 'function'
        ? CSS.escape(previous.key)
        : previous.key.replace(/["\\]/g, '\\$&')
      const anchoredRow = viewport.querySelector<HTMLElement>(`[data-collection-row-key="${escaped}"]`)
      if (anchoredRow) {
        const nextOffset = anchoredRow.getBoundingClientRect().top - viewport.getBoundingClientRect().top
        viewport.scrollTop = anchoredScrollTop(viewport.scrollTop, previous.offset, nextOffset)
      } else {
        viewport.scrollTop = reconnectedScrollTop(previous.scrollTop, viewport.scrollHeight, viewport.clientHeight)
      }
    }
    captureAnchor()
  }, [filtered, preserveViewportAnchor])

  useEffect(() => {
    if (!menu) return
    const close = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setMenu(null)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenu(null)
    }
    document.addEventListener('pointerdown', close)
    document.addEventListener('keydown', escape)
    menuRef.current?.querySelector<HTMLButtonElement>('button')?.focus()
    return () => {
      document.removeEventListener('pointerdown', close)
      document.removeEventListener('keydown', escape)
    }
  }, [menu])

  const updateRegistry = (update: (fields: CollectionField<T>[]) => CollectionField<T>[]) => {
    setRegistry((current) => update(reconcileFieldRegistry(current, definitions)))
  }
  const setFieldWidth = (id: string, width: number) => updateRegistry((current) =>
    current.map((field) => field.id === id ? { ...field, width: clampColumnWidth(width) } : field))
  const setFieldVisible = (id: string, visible: boolean) => updateRegistry((current) =>
    current.map((field) => field.id === id ? { ...field, visible } : field))
  const moveField = (id: string, delta: number) => updateRegistry((current) => {
    const index = current.findIndex((field) => field.id === id)
    const target = Math.max(0, Math.min(current.length - 1, index + delta))
    if (index < 0 || index === target) return current
    const next = [...current]
    const [field] = next.splice(index, 1)
    next.splice(target, 0, field)
    return next
  })
  const moveFieldTo = (id: string, targetID: string) => updateRegistry((current) => {
    const from = current.findIndex((field) => field.id === id)
    const target = current.findIndex((field) => field.id === targetID)
    if (from < 0 || target < 0 || from === target) return current
    const next = [...current]
    const [field] = next.splice(from, 1)
    next.splice(target, 0, field)
    return next
  })

  const toggleSelected = (key: string) => setSelected((current) => {
    const next = new Set(current)
    if (next.has(key)) next.delete(key)
    else next.add(key)
    return next
  })

  const announceCopy = async (text: string) => {
    const copied = await copyText(text)
    setNotice(copied ? labels.copied : labels.copyUnavailable)
  }

  const applySort = (field: CollectionField<T>, direction: 'asc' | 'desc') => {
    if (field.sortable) setSort({ field: field.id, direction })
    setMenu(null)
  }

  const toggleSort = (field: CollectionField<T>) => {
    if (!field.sortable) return
    setSort((current) => current?.field === field.id
      ? { field: field.id, direction: current.direction === 'asc' ? 'desc' : 'asc' }
      : { field: field.id, direction: 'asc' })
  }

  const openContext = (
    kind: MenuTarget['kind'],
    field: number,
    row: number,
    target: HTMLElement,
    coordinates?: { left: number; top: number },
  ) => {
    const rect = target.getBoundingClientRect()
    const viewportWidth = globalThis.innerWidth || 1024
    const viewportHeight = globalThis.innerHeight || 768
    const left = Math.max(8, Math.min(coordinates?.left ?? rect.left, viewportWidth - 226))
    const top = Math.max(8, Math.min(coordinates?.top ?? rect.bottom + 4, viewportHeight - 280))
    setMenu({ kind, field, row, left, top })
  }

  const focusToken = (next: GridFocus) => `${collectionID}-${next.area}-${next.row}-${next.field}`
  const moveFocus = (next: GridFocus) => {
    setFocus(next)
    if (next.area === 'row') {
      if (next.row < boundedWindowStart) setWindowStart(Math.floor(next.row / windowSize) * windowSize)
      else if (next.row >= windowEnd) setWindowStart(Math.floor(next.row / windowSize) * windowSize)
    }
    globalThis.requestAnimationFrame?.(() => {
      viewportRef.current?.querySelector<HTMLElement>(`[data-focus-token="${focusToken(next)}"]`)?.focus()
    })
  }

  const handleGridKey = (
    event: ReactKeyboardEvent<HTMLElement>,
    current: GridFocus,
  ) => {
    if (isKeyboardContextGesture(event)) {
      event.preventDefault()
      openContext(current.area, current.field, current.row, event.currentTarget)
      return
    }
    if (current.area === 'row' && event.key === ' ') {
      event.preventDefault()
      const entry = filtered[current.row]
      if (entry) toggleSelected(entry.key)
      return
    }
    if (current.area === 'row' && event.key === 'Enter') {
      const disclosure = event.currentTarget.querySelector<HTMLElement>('summary')
      if (disclosure) {
        event.preventDefault()
        disclosure.click()
      }
      return
    }
    const next = nextGridFocus(current, event.key, filtered.length, visibleFields.length, direction, event.ctrlKey || event.metaKey)
    if (next.area !== current.area || next.field !== current.field || next.row !== current.row) {
      event.preventDefault()
      moveFocus(next)
    }
  }

  const beginResize = (field: CollectionField<T>, event: ReactPointerEvent<HTMLButtonElement>) => {
    event.preventDefault()
    const startX = event.clientX
    const startWidth = field.width
    const sign = direction === 'rtl' ? -1 : 1
    const move = (moveEvent: PointerEvent) => setFieldWidth(field.id, startWidth + (moveEvent.clientX - startX) * sign)
    const finish = () => {
      globalThis.removeEventListener('pointermove', move)
      globalThis.removeEventListener('pointerup', finish)
      globalThis.removeEventListener('pointercancel', finish)
    }
    globalThis.addEventListener('pointermove', move)
    globalThis.addEventListener('pointerup', finish)
    globalThis.addEventListener('pointercancel', finish)
  }

  const exportRows = (scope: 'all' | 'filtered', format: 'json' | 'csv') => {
    const selectedRows = (scope === 'all' ? allSorted : filtered).map((entry) => entry.row)
    const content = format === 'json'
      ? collectionToJSON(selectedRows)
      : collectionToCSV(selectedRows, effectiveRegistry)
    const suffix = scope === 'all' ? 'all' : 'filtered'
    downloadText(
      `${safeFilename(filename)}-${suffix}.${format}`,
      content,
      format === 'json' ? 'application/json;charset=utf-8' : 'text/csv;charset=utf-8',
    )
  }

  const menuField = menu ? visibleFields[menu.field] : undefined
  const menuEntry = menu?.kind === 'row' ? filtered[menu.row] : undefined
  const visibleCount = effectiveRegistry.filter((field) => field.visible).length

  return (
    <section className={`typed-collection ${className}`.trim()} dir={direction} aria-label={ariaLabel}>
      {toolbar && <div className="typed-collection__toolbar">
        <label className="typed-collection__search">
          <Search size={16} aria-hidden="true" />
          <span className="sr-only">{labels.search}</span>
          <input value={query} placeholder={labels.search} onChange={(event) => setQuery(event.target.value)} />
          {query && <button type="button" aria-label={labels.clearFilter} onClick={() => setQuery('')}><X size={14} /></button>}
        </label>
        <div className="typed-collection__summary" aria-live="polite">
          <strong>{filtered.length}</strong><span>{query ? labels.filtered : labels.all}</span>
          {selected.size > 0 && <><i aria-hidden="true" /><strong>{selected.size}</strong><span>{labels.selected}</span></>}
        </div>
        {selected.size > 0 && <button type="button" className="typed-collection__quiet-action" onClick={() => setSelected(new Set())}>{labels.clearSelection}</button>}
        <details className="typed-collection__popover typed-collection__columns">
          <summary><Columns3 size={16} />{labels.columns}</summary>
          <div className="typed-collection__popover-panel" role="group" aria-label={labels.columns}>
            {effectiveRegistry.map((field, index) => <div className="typed-collection__column-option" key={field.id}>
              <label><input type="checkbox" checked={field.visible} disabled={field.visible && visibleCount <= 1} onChange={(event) => setFieldVisible(field.id, event.target.checked)} />{field.visible ? <Eye size={14} /> : <EyeOff size={14} />}<span>{field.label}</span></label>
              <div>
                <button type="button" disabled={index === 0} aria-label={`${labels.moveEarlier}: ${field.label}`} onClick={() => moveField(field.id, -1)}><ChevronLeft size={14} /></button>
                <button type="button" disabled={index === effectiveRegistry.length - 1} aria-label={`${labels.moveLater}: ${field.label}`} onClick={() => moveField(field.id, 1)}><ChevronRight size={14} /></button>
              </div>
              <label className="typed-collection__column-width"><span className="sr-only">{`${labels.resizeColumn}: ${field.label}`}</span><input type="range" min="88" max="520" step="8" value={field.width} onChange={(event) => setFieldWidth(field.id, event.target.valueAsNumber)} /><output>{field.width}px</output></label>
            </div>)}
          </div>
        </details>
        <details className="typed-collection__popover typed-collection__export">
          <summary><Download size={16} />{labels.export}</summary>
          <div className="typed-collection__popover-panel" role="group" aria-label={labels.export}>
            <button type="button" onClick={() => exportRows('all', 'json')}>{labels.exportAllJSON}</button>
            <button type="button" onClick={() => exportRows('filtered', 'json')}>{labels.exportFilteredJSON}</button>
            <button type="button" onClick={() => exportRows('all', 'csv')}>{labels.exportAllCSV}</button>
            <button type="button" onClick={() => exportRows('filtered', 'csv')}>{labels.exportFilteredCSV}</button>
          </div>
        </details>
      </div>}

      {!effectiveRegistry.length || !records.length ? (empty ?? <p className="typed-collection__empty">{labels.noRecords}</p>) : !filtered.length ? (
        <div className="typed-collection__empty"><Search size={21} /><p>{labels.noMatches}</p><button type="button" onClick={() => setQuery('')}>{labels.clearFilter}</button></div>
      ) : <>
        <div className="typed-collection__viewport" ref={viewportRef} onScroll={captureAnchor}>
          <table
            className="typed-collection__grid"
            role="grid"
            aria-label={ariaLabel}
            aria-multiselectable="true"
            aria-rowcount={filtered.length + 1}
            aria-colcount={visibleFields.length + 1}
          >
            <colgroup><col className="typed-collection__select-column" />{visibleFields.map((field) => <col key={field.id} style={{ width: field.width }} />)}</colgroup>
            <thead><tr role="row" aria-rowindex={1}>
              <th className="typed-collection__select" scope="col"><span className="sr-only">{labels.selected}</span></th>
              {visibleFields.map((field, fieldIndex) => {
                const current: GridFocus = { area: 'header', field: fieldIndex, row: 0 }
                const sorted = sort?.field === field.id ? sort.direction : null
                return <th
                  key={field.id}
                  scope="col"
                  role="columnheader"
                  aria-colindex={fieldIndex + 2}
                  aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : 'none'}
                  aria-haspopup="menu"
                  tabIndex={focus.area === 'header' && focus.field === fieldIndex ? 0 : -1}
                  data-focus-token={focusToken(current)}
                  style={{ '--collection-column-width': `${field.width}px` } as CSSProperties}
                  onFocus={() => setFocus(current)}
                  onKeyDown={(event) => handleGridKey(event, current)}
                  onContextMenu={(event) => { event.preventDefault(); openContext('header', fieldIndex, 0, event.currentTarget, { left: event.clientX, top: event.clientY }) }}
                  draggable
                  onDragStart={(event) => { setDraggingField(field.id); event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', field.id) }}
                  onDragOver={(event) => { if (draggingField && draggingField !== field.id) event.preventDefault() }}
                  onDrop={(event) => { event.preventDefault(); const id = draggingField ?? event.dataTransfer.getData('text/plain'); if (id) moveFieldTo(id, field.id); setDraggingField(null) }}
                  onDragEnd={() => setDraggingField(null)}
                >
                  <button type="button" className="typed-collection__sort" tabIndex={-1} disabled={!field.sortable} onClick={() => toggleSort(field)}>
                    <span>{field.label}</span>
                    {sorted === 'asc' ? <ArrowDownAZ size={14} /> : sorted === 'desc' ? <ArrowUpAZ size={14} /> : <span className="typed-collection__sort-placeholder" aria-hidden="true" />}
                  </button>
                  <button
                    type="button"
                    className="typed-collection__resize"
                    tabIndex={-1}
                    aria-label={`${labels.resizeColumn}: ${field.label}`}
                    onPointerDown={(event) => beginResize(field, event)}
                    onKeyDown={(event) => {
                      if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
                      event.preventDefault()
                      const physical = event.key === 'ArrowRight' ? 12 : -12
                      setFieldWidth(field.id, field.width + (direction === 'rtl' ? -physical : physical))
                    }}
                  ><GripVertical size={13} /></button>
                </th>
              })}
            </tr></thead>
            <tbody>{windowEntries.map((entry, localRowIndex) => {
              const rowIndex = boundedWindowStart + localRowIndex
              const selectedRow = selected.has(entry.key)
              return <tr
                key={entry.key}
                role="row"
                aria-rowindex={rowIndex + 2}
                aria-selected={selectedRow}
                data-collection-row-key={entry.key}
                className={rowClassName?.(entry.row)}
              >
                <td className="typed-collection__select" role="gridcell" aria-colindex={1}>
                  <label><input type="checkbox" tabIndex={-1} checked={selectedRow} onChange={() => toggleSelected(entry.key)} /><span className="sr-only">{selectedRow ? labels.deselectRow : labels.selectRow}</span>{selectedRow && <Check size={13} aria-hidden="true" />}</label>
                </td>
                {visibleFields.map((field, fieldIndex) => {
                  const value = fieldValue(field, entry.row)
                  const custom = renderCell?.(field, value, entry.row)
                  const current: GridFocus = { area: 'row', field: fieldIndex, row: rowIndex }
                  return <td
                    key={field.id}
                    role="gridcell"
                    aria-colindex={fieldIndex + 2}
                    aria-haspopup="menu"
                    tabIndex={focus.area === 'row' && focus.row === rowIndex && focus.field === fieldIndex ? 0 : -1}
                    data-focus-token={focusToken(current)}
                    onFocus={() => setFocus(current)}
                    onKeyDown={(event) => handleGridKey(event, current)}
                    onContextMenu={(event) => { event.preventDefault(); openContext('row', fieldIndex, rowIndex, event.currentTarget, { left: event.clientX, top: event.clientY }) }}
                  >{custom === undefined ? <StructuredValue value={value} label={`${labels.structuredValue}: ${field.label}`} /> : custom}</td>
                })}
              </tr>
            })}</tbody>
          </table>
        </div>
        {filtered.length > windowSize && <nav className="typed-collection__window-nav" aria-label={`${ariaLabel} · ${labels.showing}`}>
          <button type="button" disabled={boundedWindowStart === 0} onClick={() => setWindowStart(Math.max(0, boundedWindowStart - windowSize))}><ChevronLeft size={15} />{labels.previousWindow}</button>
          <span>{labels.showing} <strong>{boundedWindowStart + 1}–{windowEnd}</strong> {labels.of} <strong>{filtered.length}</strong></span>
          <label className="typed-collection__go-to"><span>{labels.goTo}</span><select value={boundedWindowStart} onChange={(event) => setWindowStart(Number(event.target.value))}>{Array.from({ length: Math.ceil(filtered.length / windowSize) }, (_, index) => { const start = index * windowSize; return <option key={start} value={start}>{start + 1}–{Math.min(filtered.length, start + windowSize)}</option> })}</select></label>
          <button type="button" disabled={windowEnd >= filtered.length} onClick={() => setWindowStart(Math.min(maximumWindowStart, boundedWindowStart + windowSize))}>{labels.nextWindow}<ChevronRight size={15} /></button>
        </nav>}
      </>}

      <span className="sr-only" role="status" aria-live="polite">{notice}</span>
      {menu && menuField && <div
        ref={menuRef}
        className="typed-collection__context-menu"
        role="menu"
        aria-label={menu.kind === 'header' ? menuField.label : `${menuField.label} · ${menu.row + 1}`}
        style={{ left: menu.left, top: menu.top }}
        onKeyDown={(event) => {
          const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')]
          const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault()
            const delta = event.key === 'ArrowDown' ? 1 : -1
            buttons[(index + delta + buttons.length) % buttons.length]?.focus()
          }
        }}
      >
        {menu.kind === 'header' ? <>
          <button type="button" role="menuitem" onClick={() => applySort(menuField, 'asc')}><ArrowDownAZ size={15} />{labels.sortAscending}</button>
          <button type="button" role="menuitem" onClick={() => applySort(menuField, 'desc')}><ArrowUpAZ size={15} />{labels.sortDescending}</button>
          <button type="button" role="menuitem" onClick={() => { void announceCopy(filtered.map((entry) => clipboardValue(fieldValue(menuField, entry.row))).join('\n')); setMenu(null) }}><Clipboard size={15} />{labels.copyColumn}</button>
          <button type="button" role="menuitem" disabled={visibleCount <= 1} onClick={() => { setFieldVisible(menuField.id, false); setMenu(null) }}><EyeOff size={15} />{labels.hideColumn}</button>
        </> : menuEntry ? <>
          <button type="button" role="menuitem" onClick={() => { void announceCopy(clipboardValue(fieldValue(menuField, menuEntry.row), true)); setMenu(null) }}><Clipboard size={15} />{labels.copyCell}</button>
          <button type="button" role="menuitem" onClick={() => { void announceCopy(structuredJSONStringify(menuEntry.row, true)); setMenu(null) }}><Clipboard size={15} />{labels.copyRow}</button>
          <button type="button" role="menuitemcheckbox" aria-checked={selected.has(menuEntry.key)} onClick={() => { toggleSelected(menuEntry.key); setMenu(null) }}><Check size={15} />{selected.has(menuEntry.key) ? labels.deselectRow : labels.selectRow}</button>
          {selected.size > 0 && <button type="button" role="menuitem" onClick={() => { setSelected(new Set()); setMenu(null) }}><X size={15} />{labels.clearSelection}</button>}
          {query && <button type="button" role="menuitem" onClick={() => { setQuery(''); setMenu(null) }}><Search size={15} />{labels.clearFilter}</button>}
        </> : null}
      </div>}
    </section>
  )
}
