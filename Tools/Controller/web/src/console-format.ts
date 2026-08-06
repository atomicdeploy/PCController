export type ConsoleMethod =
  | 'log'
  | 'info'
  | 'warn'
  | 'error'
  | 'debug'
  | 'dir'
  | 'dirxml'
  | 'trace'
  | 'table'
  | 'group'
  | 'groupCollapsed'
  | 'groupEnd'
  | 'clear'
  | 'assert'
  | 'count'
  | 'countReset'
  | 'time'
  | 'timeLog'
  | 'timeEnd'

export type ConsoleLevel = 'log' | 'info' | 'warn' | 'error' | 'debug'

export interface ConsoleInvocation {
  method: ConsoleMethod
  args: readonly unknown[]
  timestamp?: number
  stack?: string
}

// These are the only CSS properties a renderer should apply. Values are
// validated as well as property names, so callers can pass this object straight
// to React's `style` prop without accepting arbitrary CSS from a remote peer.
export interface ConsoleStyle {
  color?: string
  backgroundColor?: string
  fontWeight?: string
  fontStyle?: string
  textDecorationLine?: string
}

export type ConsoleValueType =
  | 'string'
  | 'number'
  | 'bigint'
  | 'boolean'
  | 'null'
  | 'undefined'
  | 'symbol'
  | 'function'
  | 'array'
  | 'object'
  | 'error'

export type ConsoleToken =
  | {
      kind: 'text'
      text: string
      style?: ConsoleStyle
    }
  | {
      kind: 'value'
      text: string
      valueType: ConsoleValueType
      presentation: 'inline' | 'compact' | 'expanded'
      style?: ConsoleStyle
    }

export interface ConsoleGroupPathItem {
  id: number
  label: string
  collapsed: boolean
}

interface ConsoleEntryBase {
  id: number
  timestamp: number
  depth: number
  groupPath: readonly ConsoleGroupPathItem[]
  hidden: boolean
}

export interface ConsoleMessageEntry extends ConsoleEntryBase {
  kind: 'message'
  method: Exclude<ConsoleMethod, 'table' | 'group' | 'groupCollapsed' | 'groupEnd' | 'clear'>
  level: ConsoleLevel
  tokens: readonly ConsoleToken[]
}

export interface ConsoleGroupEntry extends ConsoleEntryBase {
  kind: 'group'
  method: 'group' | 'groupCollapsed'
  level: 'log'
  collapsed: boolean
  tokens: readonly ConsoleToken[]
}

export interface ConsoleTableRow {
  key: string
  cells: Readonly<Record<string, readonly ConsoleToken[]>>
}

export interface ConsoleTableEntry extends ConsoleEntryBase {
  kind: 'table'
  method: 'table'
  level: 'log'
  columns: readonly string[]
  rows: readonly ConsoleTableRow[]
  omittedRows: number
  omittedColumns: number
}

export type ConsoleEntry = ConsoleMessageEntry | ConsoleGroupEntry | ConsoleTableEntry

export interface ConsoleDispatchResult {
  emitted: readonly ConsoleEntry[]
  transcript: readonly ConsoleEntry[]
  cleared: boolean
  ignored: boolean
}

export interface BrowserConsoleSnapshot {
  entries: readonly ConsoleEntry[]
  depth: number
  groups: readonly ConsoleGroupPathItem[]
  counters: Readonly<Record<string, number>>
  timers: readonly string[]
  revision: number
}

export interface ConsoleFormatOptions {
  maxDepth?: number
  maxObjectKeys?: number
  maxStringLength?: number
  maxEntries?: number
  maxTableRows?: number
  maxTableColumns?: number
  now?: () => number
  captureStack?: () => string | undefined
}

interface ResolvedOptions {
  maxDepth: number
  maxObjectKeys: number
  maxStringLength: number
  maxEntries: number
  maxTableRows: number
  maxTableColumns: number
  now: () => number
  captureStack: () => string | undefined
}

interface GroupFrame extends ConsoleGroupPathItem {}

const supportedMethods = new Set<ConsoleMethod>([
  'log', 'info', 'warn', 'error', 'debug', 'dir', 'dirxml', 'trace', 'table', 'group',
  'groupCollapsed', 'groupEnd', 'clear', 'assert', 'count',
  'countReset', 'time', 'timeLog', 'timeEnd',
])

const namedColors = new Set([
  'aliceblue', 'aqua', 'black', 'blue', 'brown', 'coral', 'crimson',
  'cyan', 'darkblue', 'darkgray', 'darkgreen', 'darkgrey', 'fuchsia',
  'gold', 'gray', 'green', 'grey', 'indigo', 'lime', 'magenta', 'maroon',
  'navy', 'olive', 'orange', 'orchid', 'pink', 'plum', 'purple', 'red',
  'salmon', 'silver', 'teal', 'tomato', 'transparent', 'violet', 'white',
  'yellow', 'currentcolor',
])

const defaultOptions: ResolvedOptions = {
  maxDepth: 4,
  maxObjectKeys: 50,
  maxStringLength: 16_384,
  maxEntries: 1_000,
  maxTableRows: 100,
  maxTableColumns: 20,
  now: () => Date.now(),
  captureStack: () => {
    try {
      const stack = new Error('Trace').stack
      return typeof stack === 'string' ? stack : undefined
    } catch {
      return undefined
    }
  },
}

function boundedOption(value: number | undefined, fallback: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) return fallback
  return Math.min(maximum, Math.max(minimum, Math.trunc(value as number)))
}

function resolveOptions(options: ConsoleFormatOptions = {}): ResolvedOptions {
  return {
    maxDepth: boundedOption(options.maxDepth, defaultOptions.maxDepth, 1, 12),
    maxObjectKeys: boundedOption(options.maxObjectKeys, defaultOptions.maxObjectKeys, 1, 1_000),
    maxStringLength: boundedOption(options.maxStringLength, defaultOptions.maxStringLength, 32, 1_000_000),
    maxEntries: boundedOption(options.maxEntries, defaultOptions.maxEntries, 1, 100_000),
    maxTableRows: boundedOption(options.maxTableRows, defaultOptions.maxTableRows, 1, 10_000),
    maxTableColumns: boundedOption(options.maxTableColumns, defaultOptions.maxTableColumns, 1, 500),
    now: typeof options.now === 'function' ? options.now : defaultOptions.now,
    captureStack: typeof options.captureStack === 'function' ? options.captureStack : defaultOptions.captureStack,
  }
}

function normalizeMethod(value: unknown): ConsoleMethod | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed) return null
  const canonical = trimmed.toLowerCase()
  const aliases: Record<string, ConsoleMethod> = {
    groupcollapsed: 'groupCollapsed',
    groupend: 'groupEnd',
    countreset: 'countReset',
    timelog: 'timeLog',
    timeend: 'timeEnd',
  }
  const method = aliases[canonical] ?? canonical as ConsoleMethod
  return supportedMethods.has(method) ? method : null
}

function copyInvocationArgs(value: unknown): unknown[] | null {
  if (!Array.isArray(value)) return null
  try {
    const descriptors = Object.getOwnPropertyDescriptors(value) as unknown as Record<PropertyKey, PropertyDescriptor>
    const lengthDescriptor = descriptors.length
    if (!lengthDescriptor || !('value' in lengthDescriptor)) return null
    const length = lengthDescriptor.value as unknown
    if (typeof length !== 'number' || !Number.isSafeInteger(length) || length < 0 || length > 256) return null
    const result = new Array<unknown>(length)
    for (let index = 0; index < length; index++) {
      const descriptor = descriptors[String(index)]
      if (!descriptor) {
        result[index] = undefined
      } else if (!('value' in descriptor)) {
        return null
      } else {
        result[index] = descriptor.value
      }
    }
    return result
  } catch {
    return null
  }
}

function parseStructuredInvocation(payload: unknown): ConsoleInvocation | null {
  if (payload === null || (typeof payload !== 'object' && typeof payload !== 'function')) return null
  try {
    const descriptors = Object.getOwnPropertyDescriptors(payload)
    const methodDescriptor = descriptors.method
    const argsDescriptor = descriptors.args
    const timestampDescriptor = descriptors.timestamp
    const stackDescriptor = descriptors.stack
    if (!methodDescriptor || !('value' in methodDescriptor)) return null
    const method = normalizeMethod(methodDescriptor.value)
    if (!method) return null
    if (argsDescriptor && !('value' in argsDescriptor)) return null
    const suppliedArgs = argsDescriptor?.value
    const args = suppliedArgs === undefined ? [] : copyInvocationArgs(suppliedArgs)
    if (!args) return null
    let timestamp: number | undefined
    if (timestampDescriptor) {
      if (!('value' in timestampDescriptor) || !Number.isFinite(timestampDescriptor.value)) return null
      timestamp = Number(timestampDescriptor.value)
    }
    let stack: string | undefined
    if (stackDescriptor) {
      if (!('value' in stackDescriptor) || typeof stackDescriptor.value !== 'string') return null
      stack = stackDescriptor.value
    }
    return {
      method,
      args,
      ...(timestamp === undefined ? {} : { timestamp }),
      ...(stack === undefined ? {} : { stack }),
    }
  } catch {
    return null
  }
}

class ConsoleLineParser {
  private index = 0

  constructor(private readonly source: string) {}

  parse(): ConsoleInvocation | null {
    if (this.source.length > 65_536) return null
    this.space()
    if (!this.consumeWord('console')) return null
    this.space()
    if (!this.consume('.')) return null
    this.space()
    const methodName = this.identifier()
    const method = normalizeMethod(methodName)
    if (!method) return null
    this.space()
    if (!this.consume('(')) return null
    this.space()
    const args: unknown[] = []
    if (!this.peek(')')) {
      for (;;) {
        if (args.length >= 256) return null
        const parsed = this.value(0)
        if (!parsed.ok) return null
        args.push(parsed.value)
        this.space()
        if (this.consume(')')) break
        if (!this.consume(',')) return null
        this.space()
        if (this.peek(')')) {
          this.index++
          break
        }
      }
    } else {
      this.index++
    }
    this.space()
    this.consume(';')
    this.space()
    return this.index === this.source.length ? { method, args } : null
  }

  private value(depth: number): { ok: true; value: unknown } | { ok: false } {
    if (depth > 32) return { ok: false }
    this.space()
    const character = this.source[this.index]
    if (character === '"' || character === "'") return this.string(character)
    if (character === '[') return this.array(depth + 1)
    if (character === '{') return this.object(depth + 1)
    for (const [literal, value] of [
      ['undefined', undefined],
      ['-Infinity', -Infinity],
      ['Infinity', Infinity],
      ['false', false],
      ['true', true],
      ['null', null],
      ['NaN', Number.NaN],
    ] as const) {
      if (this.consumeWord(literal)) return { ok: true, value }
    }
    const match = this.source.slice(this.index).match(/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/)
    if (!match) return { ok: false }
    this.index += match[0].length
    return { ok: true, value: Number(match[0]) }
  }

  private string(quote: string): { ok: true; value: string } | { ok: false } {
    this.index++
    let result = ''
    while (this.index < this.source.length) {
      const character = this.source[this.index++]
      if (character === quote) return { ok: true, value: result }
      if (character === '\n' || character === '\r') return { ok: false }
      if (character !== '\\') {
        result += character
        continue
      }
      if (this.index >= this.source.length) return { ok: false }
      const escaped = this.source[this.index++]
      const escapes: Record<string, string> = {
        '"': '"', "'": "'", '\\': '\\', '/': '/',
        b: '\b', f: '\f', n: '\n', r: '\r', t: '\t',
      }
      if (escaped in escapes) {
        result += escapes[escaped]
        continue
      }
      if (escaped !== 'u') return { ok: false }
      const digits = this.source.slice(this.index, this.index + 4)
      if (!/^[0-9a-f]{4}$/i.test(digits)) return { ok: false }
      result += String.fromCharCode(Number.parseInt(digits, 16))
      this.index += 4
    }
    return { ok: false }
  }

  private array(depth: number): { ok: true; value: unknown[] } | { ok: false } {
    this.index++
    this.space()
    const result: unknown[] = []
    if (this.consume(']')) return { ok: true, value: result }
    for (;;) {
      if (result.length >= 1_000) return { ok: false }
      const parsed = this.value(depth)
      if (!parsed.ok) return parsed
      result.push(parsed.value)
      this.space()
      if (this.consume(']')) return { ok: true, value: result }
      if (!this.consume(',')) return { ok: false }
      this.space()
      if (this.consume(']')) return { ok: true, value: result }
    }
  }

  private object(depth: number): { ok: true; value: Record<string, unknown> } | { ok: false } {
    this.index++
    this.space()
    const result = Object.create(null) as Record<string, unknown>
    if (this.consume('}')) return { ok: true, value: result }
    let count = 0
    for (;;) {
      if (count++ >= 1_000) return { ok: false }
      let key: string
      const character = this.source[this.index]
      if (character === '"' || character === "'") {
        const parsed = this.string(character)
        if (!parsed.ok) return parsed
        key = parsed.value
      } else {
        key = this.identifier()
        if (!key) return { ok: false }
      }
      this.space()
      if (!this.consume(':')) return { ok: false }
      const parsed = this.value(depth)
      if (!parsed.ok) return parsed
      Object.defineProperty(result, key, {
        value: parsed.value, enumerable: true, configurable: true, writable: true,
      })
      this.space()
      if (this.consume('}')) return { ok: true, value: result }
      if (!this.consume(',')) return { ok: false }
      this.space()
      if (this.consume('}')) return { ok: true, value: result }
    }
  }

  private identifier(): string {
    const match = this.source.slice(this.index).match(/^[A-Za-z_$][\w$-]*/)
    if (!match) return ''
    this.index += match[0].length
    return match[0]
  }

  private space(): void {
    while (/\s/.test(this.source[this.index] ?? '')) this.index++
  }

  private peek(value: string): boolean {
    return this.source.startsWith(value, this.index)
  }

  private consume(value: string): boolean {
    if (!this.peek(value)) return false
    this.index += value.length
    return true
  }

  private consumeWord(value: string): boolean {
    if (!this.peek(value)) return false
    const next = this.source[this.index + value.length]
    if (next && /[\w$-]/.test(next)) return false
    this.index += value.length
    return true
  }
}

// Accepts either a structured bridge payload or terminal text such as
// `console.info("device %s", "ready")`. The text path uses the bounded parser
// above and never evaluates JavaScript or resolves identifiers.
export function parseConsoleInvocation(payload: unknown): ConsoleInvocation | null {
  if (typeof payload === 'string') return new ConsoleLineParser(payload).parse()
  return parseStructuredInvocation(payload)
}

function safeColor(value: string): string | null {
  const normalized = value.trim().toLowerCase()
  if (namedColors.has(normalized)) return normalized
  if (/^#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/i.test(normalized)) return normalized
  if (/^(?:rgb|rgba|hsl|hsla)\([\d\s.,%+\-/]+\)$/i.test(normalized)) return normalized
  return null
}

function safeFontWeight(value: string): string | null {
  const normalized = value.trim().toLowerCase()
  return /^(?:normal|bold|bolder|lighter|[1-9]00)$/.test(normalized) ? normalized : null
}

function safeFontStyle(value: string): string | null {
  const normalized = value.trim().toLowerCase()
  return /^(?:normal|italic|oblique)$/.test(normalized) ? normalized : null
}

function safeDecoration(value: string): string | null {
  const parts = value.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (!parts.length) return null
  const allowed = new Set(['none', 'underline', 'overline', 'line-through'])
  if (parts.some((part) => !allowed.has(part))) return null
  if (parts.includes('none') && parts.length !== 1) return null
  return [...new Set(parts)].join(' ')
}

export function parseConsoleStyle(value: unknown): ConsoleStyle {
  if (typeof value !== 'string' || value.length > 4_096) return {}
  const result: ConsoleStyle = {}
  for (const declaration of value.split(';')) {
    const separator = declaration.indexOf(':')
    if (separator < 1) continue
    const property = declaration.slice(0, separator).trim().toLowerCase()
    const candidate = declaration.slice(separator + 1).trim()
    if (!candidate || /(?:url|expression|var)\s*\(/i.test(candidate) || /[{}<>]/.test(candidate)) continue
    switch (property) {
      case 'color': {
        const color = safeColor(candidate)
        if (color) result.color = color
        break
      }
      case 'background-color': {
        const color = safeColor(candidate)
        if (color) result.backgroundColor = color
        break
      }
      case 'font-weight': {
        const weight = safeFontWeight(candidate)
        if (weight) result.fontWeight = weight
        break
      }
      case 'font-style': {
        const style = safeFontStyle(candidate)
        if (style) result.fontStyle = style
        break
      }
      case 'text-decoration':
      case 'text-decoration-line': {
        const decoration = safeDecoration(candidate)
        if (decoration) result.textDecorationLine = decoration
        break
      }
    }
  }
  return result
}

function cloneStyle(style: ConsoleStyle | undefined): ConsoleStyle | undefined {
  return style && Object.keys(style).length ? { ...style } : undefined
}

function styleEqual(left: ConsoleStyle | undefined, right: ConsoleStyle | undefined): boolean {
  return left?.color === right?.color &&
    left?.backgroundColor === right?.backgroundColor &&
    left?.fontWeight === right?.fontWeight &&
    left?.fontStyle === right?.fontStyle &&
    left?.textDecorationLine === right?.textDecorationLine
}

function appendText(tokens: ConsoleToken[], text: string, style?: ConsoleStyle): void {
  const previous = tokens[tokens.length - 1]
  if (previous?.kind === 'text' && styleEqual(previous.style, style)) {
    tokens[tokens.length - 1] = { ...previous, text: previous.text + text }
    return
  }
  tokens.push({ kind: 'text', text, style: cloneStyle(style) })
}

function limitedString(value: string, options: ResolvedOptions): string {
  if (value.length <= options.maxStringLength) return value
  return `${value.slice(0, options.maxStringLength)}…[${value.length - options.maxStringLength} more]`
}

function quotedString(value: string, options: ResolvedOptions): string {
  const limited = limitedString(value, options)
  return JSON.stringify(limited) ?? '""'
}

function numberText(value: number): string {
  if (Number.isNaN(value)) return 'NaN'
  if (value === Infinity) return 'Infinity'
  if (value === -Infinity) return '-Infinity'
  if (Object.is(value, -0)) return '-0'
  return String(value)
}

function valueType(value: unknown): ConsoleValueType {
  if (value === null) return 'null'
  try {
    if (Array.isArray(value)) return 'array'
    if (value instanceof Error) return 'error'
  } catch {
    return 'object'
  }
  const kind = typeof value
  if (kind === 'string' || kind === 'number' || kind === 'bigint' || kind === 'boolean' ||
      kind === 'undefined' || kind === 'symbol' || kind === 'function') return kind
  return 'object'
}

function inheritedDataString(value: object, property: string): string | undefined {
  try {
    let current: object | null = value
    for (let depth = 0; current && depth < 16; depth++) {
      const descriptor = Object.getOwnPropertyDescriptor(current, property)
      if (descriptor) return 'value' in descriptor && typeof descriptor.value === 'string'
        ? descriptor.value
        : undefined
      current = Object.getPrototypeOf(current) as object | null
    }
  } catch {
    // Hostile proxies are represented generically without invoking accessors.
  }
  return undefined
}

function collectionSize(value: object, prototype: object): number | null {
  try {
    const getter = Object.getOwnPropertyDescriptor(prototype, 'size')?.get
    if (!getter) return null
    const size = Reflect.apply(getter, value, []) as unknown
    return typeof size === 'number' && Number.isSafeInteger(size) && size >= 0 ? size : null
  } catch {
    return null
  }
}

function safeFunctionName(value: Function): string {
  try {
    const descriptor = Object.getOwnPropertyDescriptor(value, 'name')
    return typeof descriptor?.value === 'string' && descriptor.value ? descriptor.value : 'anonymous'
  } catch {
    return 'anonymous'
  }
}

function terminalObjectLabel(value: object): string {
  try {
    if (Array.isArray(value)) {
      const length = Object.getOwnPropertyDescriptor(value, 'length')?.value
      return `Array(${typeof length === 'number' ? length : '?'})`
    }
    if (value instanceof Map) return `Map(${collectionSize(value, Map.prototype) ?? '?'})`
    if (value instanceof Set) return `Set(${collectionSize(value, Set.prototype) ?? '?'})`
    if (value instanceof Date) return 'Date'
    if (value instanceof Error) return inheritedDataString(value, 'name') || 'Error'
  } catch {
    return 'Object'
  }
  return 'Object'
}

function inspectValue(
  value: unknown,
  options: ResolvedOptions,
  depth: number,
  seen: Set<object>,
  nested: boolean,
): string {
  if (value === null) return 'null'
  switch (typeof value) {
    case 'string': return nested ? quotedString(value, options) : limitedString(value, options)
    case 'number': return numberText(value)
    case 'bigint': return `${value}n`
    case 'boolean': return String(value)
    case 'undefined': return 'undefined'
    case 'symbol': return String(value)
    case 'function': return `[Function ${safeFunctionName(value)}]`
  }
  const object = value as object
  if (seen.has(object)) return '[Circular]'
  if (depth >= options.maxDepth) return `[${terminalObjectLabel(object)}]`
  seen.add(object)
  try {
    if (object instanceof Date) {
      const time = Date.prototype.getTime.call(object)
      return Number.isNaN(time) ? 'Invalid Date' : Date.prototype.toISOString.call(object)
    }
    if (object instanceof RegExp) return RegExp.prototype.toString.call(object)
    if (object instanceof Error) {
      const name = inheritedDataString(object, 'name')
      const message = inheritedDataString(object, 'message')
      return `${name || 'Error'}${message ? `: ${limitedString(message, options)}` : ''}`
    }
    if (object instanceof Map) {
      const size = collectionSize(object, Map.prototype)
      const values: string[] = []
      let index = 0
      for (const [key, item] of Map.prototype.entries.call(object) as Iterable<[unknown, unknown]>) {
        if (index++ >= options.maxObjectKeys) break
        values.push(`${inspectValue(key, options, depth + 1, seen, true)} => ${inspectValue(item, options, depth + 1, seen, true)}`)
      }
      if (size !== null && size > values.length) values.push(`… ${size - values.length} more`)
      return `Map(${size ?? '?'}) { ${values.join(', ')} }`
    }
    if (object instanceof Set) {
      const size = collectionSize(object, Set.prototype)
      const values: string[] = []
      let index = 0
      for (const item of Set.prototype.values.call(object) as Iterable<unknown>) {
        if (index++ >= options.maxObjectKeys) break
        values.push(inspectValue(item, options, depth + 1, seen, true))
      }
      if (size !== null && size > values.length) values.push(`… ${size - values.length} more`)
      return `Set(${size ?? '?'}) { ${values.join(', ')} }`
    }
    const descriptors = Object.getOwnPropertyDescriptors(object)
    const keys = Reflect.ownKeys(descriptors).filter((key) => descriptors[key as keyof typeof descriptors]?.enumerable)
    const limited = keys.slice(0, options.maxObjectKeys)
    const values = limited.map((key) => {
      const descriptor = descriptors[key as keyof typeof descriptors]
      const label = typeof key === 'symbol' ? `[${String(key)}]` : /^[A-Za-z_$][\w$]*$/.test(key) ? key : quotedString(key, options)
      if (!descriptor || !('value' in descriptor)) {
        const accessor = descriptor?.get && descriptor?.set ? '[Getter/Setter]' : descriptor?.get ? '[Getter]' : '[Setter]'
        return `${label}: ${accessor}`
      }
      return `${label}: ${inspectValue(descriptor.value, options, depth + 1, seen, true)}`
    })
    if (keys.length > limited.length) values.push(`… ${keys.length - limited.length} more`)
    if (Array.isArray(object)) return `[${values.map((value) => value.replace(/^\d+: /, '')).join(', ')}]`
    return `{ ${values.join(', ')} }`
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : 'inspection failed'
    return `[Uninspectable: ${limitedString(message, options)}]`
  } finally {
    seen.delete(object)
  }
}

export function inspectConsoleValue(value: unknown, options: ConsoleFormatOptions = {}): string {
  return inspectValue(value, resolveOptions(options), 0, new Set(), false)
}

function tokenForValue(
  value: unknown,
  options: ResolvedOptions,
  presentation: 'inline' | 'compact' | 'expanded' = 'inline',
  style?: ConsoleStyle,
): ConsoleToken {
  const effective = presentation === 'compact'
    ? { ...options, maxDepth: Math.min(2, options.maxDepth), maxObjectKeys: Math.min(12, options.maxObjectKeys) }
    : options
  return {
    kind: 'value',
    text: inspectValue(value, effective, 0, new Set(), false),
    valueType: valueType(value),
    presentation,
    style: cloneStyle(style),
  }
}

function safeString(value: unknown, options: ResolvedOptions): string {
  if (typeof value === 'string') return limitedString(value, options)
  return inspectValue(value, { ...options, maxDepth: Math.min(options.maxDepth, 2) }, 0, new Set(), false)
}

function numericSubstitution(value: unknown, integer: boolean): string {
  if (typeof value === 'bigint') return String(value)
  let number: number
  try {
    number = Number(value)
  } catch {
    number = Number.NaN
  }
  if (integer && Number.isFinite(number)) number = Math.trunc(number)
  return numberText(number)
}

export function formatConsoleTokens(args: readonly unknown[], options: ConsoleFormatOptions = {}): readonly ConsoleToken[] {
  const resolved = resolveOptions(options)
  if (!args.length) return [{ kind: 'text', text: '' }]
  const tokens: ConsoleToken[] = []
  let consumed = 0
  if (typeof args[0] === 'string') {
    const format = limitedString(args[0], resolved)
    consumed = 1
    let style: ConsoleStyle = {}
    let plain = ''
    const flush = () => {
      if (!plain) return
      appendText(tokens, plain, style)
      plain = ''
    }
    for (let index = 0; index < format.length; index++) {
      const character = format[index]
      if (character !== '%' || index + 1 >= format.length) {
        plain += character
        continue
      }
      const marker = format[index + 1]
      if (marker === '%') {
        plain += '%'
        index++
        continue
      }
      if (!'sdifoOc'.includes(marker) || consumed >= args.length) {
        plain += `%${marker}`
        index++
        continue
      }
      flush()
      const value = args[consumed++]
      index++
      switch (marker) {
        case 's': appendText(tokens, safeString(value, resolved), style); break
        case 'd':
        case 'i': tokens.push({ kind: 'value', text: numericSubstitution(value, true), valueType: 'number', presentation: 'inline', style: cloneStyle(style) }); break
        case 'f': tokens.push({ kind: 'value', text: numericSubstitution(value, false), valueType: 'number', presentation: 'inline', style: cloneStyle(style) }); break
        case 'o': tokens.push(tokenForValue(value, resolved, 'compact', style)); break
        case 'O': tokens.push(tokenForValue(value, resolved, 'expanded', style)); break
        case 'c': style = parseConsoleStyle(value); break
      }
    }
    flush()
  }
  for (; consumed < args.length; consumed++) {
    if (tokens.length) appendText(tokens, ' ')
    tokens.push(tokenForValue(args[consumed], resolved))
  }
  return tokens.length ? tokens : [{ kind: 'text', text: '' }]
}

export function consoleTokensText(tokens: readonly ConsoleToken[]): string {
  return tokens.map((token) => token.text).join('')
}

function nativeStyle(style: ConsoleStyle | undefined): string {
  if (!style) return ''
  const declarations: string[] = []
  if (style.color) declarations.push(`color:${style.color}`)
  if (style.backgroundColor) declarations.push(`background-color:${style.backgroundColor}`)
  if (style.fontWeight) declarations.push(`font-weight:${style.fontWeight}`)
  if (style.fontStyle) declarations.push(`font-style:${style.fontStyle}`)
  if (style.textDecorationLine) declarations.push(`text-decoration-line:${style.textDecorationLine}`)
  return declarations.join(';')
}

// Native devtools mirroring remains data-only: token text is always passed as
// a `%s` argument, never inserted into the format string, and `%c` receives only
// properties already accepted by parseConsoleStyle.
export function consoleTokensToNativeArgs(tokens: readonly ConsoleToken[]): readonly unknown[] {
  let format = ''
  let activeStyle = ''
  const args: unknown[] = []
  const values = tokens.length ? tokens : [{ kind: 'text', text: '' } as ConsoleToken]
  for (const token of values) {
    const nextStyle = nativeStyle(token.style)
    if (nextStyle !== activeStyle) {
      format += '%c'
      args.push(nextStyle)
      activeStyle = nextStyle
    }
    format += '%s'
    args.push(token.text)
  }
  if (activeStyle) {
    format += '%c'
    args.push('')
  }
  return [format, ...args]
}

function ownEnumerableValues(value: object): Array<{ key: string; value: unknown }> {
  try {
    const descriptors = Object.getOwnPropertyDescriptors(value)
    return Reflect.ownKeys(descriptors)
      .filter((key) => descriptors[key as keyof typeof descriptors]?.enumerable)
      .map((key) => {
        const descriptor = descriptors[key as keyof typeof descriptors]
        const renderedKey = typeof key === 'symbol' ? String(key) : key
        if (!descriptor || !('value' in descriptor)) {
          return { key: renderedKey, value: descriptor?.get ? '[Getter]' : '[Setter]' }
        }
        return { key: renderedKey, value: descriptor.value }
      })
  } catch {
    return []
  }
}

function tableSource(value: unknown): Array<{ key: string; value: unknown }> {
  try {
    if (value instanceof Map) {
      return Array.from(Map.prototype.entries.call(value) as Iterable<[unknown, unknown]>, ([key, item]) => ({
        key: inspectConsoleValue(key, { maxDepth: 1, maxObjectKeys: 4 }),
        value: item,
      }))
    }
    if (value instanceof Set) {
      return Array.from(Set.prototype.values.call(value) as Iterable<unknown>, (item, index) => ({ key: String(index), value: item }))
    }
    if (value !== null && (typeof value === 'object' || typeof value === 'function')) return ownEnumerableValues(value as object)
  } catch {
    // The fallback row below remains safe for hostile proxies and iterators.
  }
  return [{ key: '0', value }]
}

function requestedTableColumns(value: unknown, options: ResolvedOptions): string[] | null {
  if (!Array.isArray(value)) return null
  const result: string[] = []
  for (const candidate of value) {
    if (typeof candidate !== 'string' && typeof candidate !== 'number') continue
    const column = limitedString(String(candidate), options)
    if (column && !result.includes(column)) result.push(column)
    if (result.length >= options.maxTableColumns) break
  }
  return result
}

function buildTable(value: unknown, columnsArg: unknown, options: ResolvedOptions): Pick<ConsoleTableEntry, 'columns' | 'rows' | 'omittedRows' | 'omittedColumns'> {
  const source = tableSource(value)
  const visibleSource = source.slice(0, options.maxTableRows)
  const requested = requestedTableColumns(columnsArg, options)
  const discovered: string[] = []
  let needsValue = false
  for (const row of visibleSource) {
    const item = row.value
    if (item !== null && (typeof item === 'object' || typeof item === 'function')) {
      for (const property of ownEnumerableValues(item as object)) {
        if (!discovered.includes(property.key)) discovered.push(property.key)
      }
    } else {
      needsValue = true
    }
  }
  const rawColumns = requested ?? [...discovered, ...(needsValue || !discovered.length ? ['Value'] : [])]
  const selectedColumns = rawColumns.slice(0, options.maxTableColumns)
  const columns = ['(index)', ...selectedColumns]
  const rows = visibleSource.map((row): ConsoleTableRow => {
    const cells: Record<string, readonly ConsoleToken[]> = {
      '(index)': [tokenForValue(row.key, options)],
    }
    const childValues = row.value !== null && (typeof row.value === 'object' || typeof row.value === 'function')
      ? new Map(ownEnumerableValues(row.value as object).map((item) => [item.key, item.value]))
      : null
    for (const column of selectedColumns) {
      if (column === 'Value') {
        cells[column] = [tokenForValue(row.value, options, 'compact')]
      } else if (childValues?.has(column)) {
        cells[column] = [tokenForValue(childValues.get(column), options, 'compact')]
      } else {
        cells[column] = [{ kind: 'text', text: '' }]
      }
    }
    return { key: row.key, cells }
  })
  return {
    columns,
    rows,
    omittedRows: Math.max(0, source.length - visibleSource.length),
    omittedColumns: Math.max(0, rawColumns.length - selectedColumns.length),
  }
}

function methodLevel(method: ConsoleMethod): ConsoleLevel {
  switch (method) {
    case 'info': return 'info'
    case 'warn': return 'warn'
    case 'error':
    case 'assert': return 'error'
    case 'debug': return 'debug'
    default: return 'log'
  }
}

function timerDuration(milliseconds: number): string {
  const safe = Math.max(0, milliseconds)
  const value = safe < 1_000 ? safe : safe / 1_000
  const unit = safe < 1_000 ? 'ms' : 's'
  return `${Number(value.toFixed(3))} ${unit}`
}

function boundedStackText(value: unknown, options: ResolvedOptions): string | undefined {
  if (typeof value !== 'string') return undefined
  const normalized = value
    .replace(/\r\n?/g, '\n')
    .replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, '')
    .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, '')
    .trim()
  return normalized ? limitedString(normalized, options) : undefined
}

export class BrowserConsoleModel {
  private readonly options: ResolvedOptions
  private transcriptValue: ConsoleEntry[] = []
  private groups: GroupFrame[] = []
  private counters = new Map<string, number>()
  private timers = new Map<string, number>()
  private nextEntryID = 0
  private nextGroupID = 0
  private revisionValue = 0

  constructor(options: ConsoleFormatOptions = {}) {
    this.options = resolveOptions(options)
  }

  dispatch(payload: unknown): ConsoleDispatchResult {
    const invocation = parseConsoleInvocation(payload)
    if (!invocation) {
      return this.emitMessage('warn', ['Unsupported console invocation payload.'], this.options.now())
    }
    return this.invoke(invocation.method, invocation.args, invocation.timestamp, invocation.stack)
  }

  invoke(
    methodValue: ConsoleMethod | string,
    args: readonly unknown[] = [],
    timestamp?: number,
    stack?: string,
  ): ConsoleDispatchResult {
    const method = normalizeMethod(methodValue)
    const at = Number.isFinite(timestamp) ? Number(timestamp) : this.options.now()
    if (!method) return this.emitMessage('warn', [`Unsupported console method: ${String(methodValue)}`], at)
    switch (method) {
      case 'log':
      case 'info':
      case 'warn':
      case 'error':
      case 'debug':
        return this.emitMessage(method, args, at)
      case 'dir':
      case 'dirxml':
        return this.append({
          ...this.base(at), kind: 'message', method, level: 'log',
          tokens: [tokenForValue(args[0], this.options, 'expanded')],
        })
      case 'trace': {
        const tokens: ConsoleToken[] = args.length
          ? [...formatConsoleTokens(args, this.options)]
          : [{ kind: 'text', text: 'Trace' }]
        let captured = stack
        if (captured === undefined) {
          try {
            captured = this.options.captureStack()
          } catch {
            captured = undefined
          }
        }
        const trace = boundedStackText(captured, this.options)
        if (trace) tokens.push({ kind: 'text', text: `\n${trace}` })
        return this.append({
          ...this.base(at), kind: 'message', method, level: 'debug', tokens,
        })
      }
      case 'assert': {
        if (Boolean(args[0])) return this.noop()
        const detail = args.slice(1)
        const tokens = detail.length ? [...formatConsoleTokens(detail, this.options)] : []
        const prefixed: ConsoleToken[] = [{ kind: 'text', text: 'Assertion failed' }]
        if (tokens.length) prefixed.push({ kind: 'text', text: ': ' }, ...tokens)
        return this.append({
          ...this.base(at), kind: 'message', method: 'assert', level: 'error', tokens: prefixed,
        })
      }
      case 'group':
      case 'groupCollapsed': {
        const collapsed = method === 'groupCollapsed'
        const tokens = args.length ? formatConsoleTokens(args, this.options) : [{ kind: 'text', text: 'console.group' } as ConsoleToken]
        const entry: ConsoleGroupEntry = {
          ...this.base(at), kind: 'group', method, level: 'log', collapsed, tokens,
        }
        const result = this.append(entry)
        this.groups.push({ id: ++this.nextGroupID, label: consoleTokensText(tokens), collapsed })
        this.revisionValue++
        return { ...result, transcript: this.entries() }
      }
      case 'groupEnd':
        if (this.groups.length) {
          this.groups.pop()
          this.revisionValue++
        }
        return this.noop()
      case 'clear':
        this.transcriptValue = []
        this.groups = []
        this.revisionValue++
        return { emitted: [], transcript: [], cleared: true, ignored: false }
      case 'count': {
        const label = this.label(args[0])
        const next = (this.counters.get(label) ?? 0) + 1
        this.counters.set(label, next)
        return this.emitMessage('log', [`${label}: ${next}`], at)
      }
      case 'countReset': {
        const label = this.label(args[0])
        if (!this.counters.has(label)) return this.emitMessage('warn', [`Count for '${label}' does not exist.`], at)
        this.counters.set(label, 0)
        this.revisionValue++
        return this.noop()
      }
      case 'time': {
        const label = this.label(args[0])
        if (this.timers.has(label)) return this.emitMessage('warn', [`Timer '${label}' already exists.`], at)
        this.timers.set(label, at)
        this.revisionValue++
        return this.noop()
      }
      case 'timeLog':
      case 'timeEnd': {
        const label = this.label(args[0])
        const started = this.timers.get(label)
        if (started === undefined) return this.emitMessage('warn', [`Timer '${label}' does not exist.`], at)
        const prefix = `${label}: ${timerDuration(at - started)}`
        const detail = method === 'timeLog' ? args.slice(1) : []
        if (method === 'timeEnd') this.timers.delete(label)
        const tokens: ConsoleToken[] = [{ kind: 'text', text: prefix }]
        if (detail.length) tokens.push({ kind: 'text', text: ' ' }, ...formatConsoleTokens(detail, this.options))
        const result = this.append({
          ...this.base(at), kind: 'message', method, level: 'log', tokens,
        })
        return result
      }
      case 'table': {
        const table = buildTable(args[0], args[1], this.options)
        return this.append({ ...this.base(at), kind: 'table', method: 'table', level: 'log', ...table })
      }
    }
  }

  snapshot(): BrowserConsoleSnapshot {
    const counters = Object.create(null) as Record<string, number>
    for (const [label, value] of this.counters) {
      Object.defineProperty(counters, label, {
        value, enumerable: true, configurable: true, writable: false,
      })
    }
    return {
      entries: this.entries(),
      depth: this.groups.length,
      groups: this.groupPath(),
      counters,
      timers: [...this.timers.keys()],
      revision: this.revisionValue,
    }
  }

  entries(): readonly ConsoleEntry[] {
    return this.transcriptValue.slice()
  }

  reset(): void {
    this.transcriptValue = []
    this.groups = []
    this.counters.clear()
    this.timers.clear()
    this.nextEntryID = 0
    this.nextGroupID = 0
    this.revisionValue++
  }

  private label(value: unknown): string {
    return value === undefined ? 'default' : safeString(value, this.options)
  }

  private groupPath(): ConsoleGroupPathItem[] {
    return this.groups.map((group) => ({ ...group }))
  }

  private base(timestamp: number): ConsoleEntryBase {
    const groupPath = this.groupPath()
    return {
      id: ++this.nextEntryID,
      timestamp,
      depth: groupPath.length,
      groupPath,
      hidden: groupPath.some((group) => group.collapsed),
    }
  }

  private emitMessage(
    method: ConsoleMessageEntry['method'],
    args: readonly unknown[],
    timestamp: number,
  ): ConsoleDispatchResult {
    return this.append({
      ...this.base(timestamp), kind: 'message', method, level: methodLevel(method),
      tokens: formatConsoleTokens(args, this.options),
    })
  }

  private append(entry: ConsoleEntry): ConsoleDispatchResult {
    this.transcriptValue.push(entry)
    if (this.transcriptValue.length > this.options.maxEntries) {
      this.transcriptValue.splice(0, this.transcriptValue.length - this.options.maxEntries)
    }
    this.revisionValue++
    return {
      emitted: [entry],
      transcript: this.entries(),
      cleared: false,
      ignored: false,
    }
  }

  private noop(): ConsoleDispatchResult {
    return {
      emitted: [],
      transcript: this.entries(),
      cleared: false,
      ignored: true,
    }
  }
}
