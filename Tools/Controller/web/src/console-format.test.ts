import { describe, expect, it } from 'vitest'
import {
  BrowserConsoleModel,
  consoleTokensText,
  consoleTokensToNativeArgs,
  formatConsoleTokens,
  inspectConsoleValue,
  parseConsoleInvocation,
  parseConsoleStyle,
  type ConsoleMessageEntry,
  type ConsoleTableEntry,
} from './console-format'

describe('console invocation parser', () => {
  it('parses terminal calls without evaluating JavaScript', () => {
    expect(parseConsoleInvocation(' console.log("device %s", "ready"); ')).toEqual({
      method: 'log',
      args: ['device %s', 'ready'],
    })
    const parsed = parseConsoleInvocation(
      "console.groupCollapsed('link', {port: 'COM18', flags: [true, null, undefined,], rate: 1.25e2,})",
    )
    expect(parsed?.method).toBe('groupCollapsed')
    expect(parsed?.args[0]).toBe('link')
    expect(parsed?.args[1]).toMatchObject({ port: 'COM18', flags: [true, null, undefined], rate: 125 })
  })

  it('rejects executable syntax, unknown methods, excess input, and deep payloads', () => {
    for (const line of [
      'console.log(window.location)',
      'console.log(alert(1))',
      'console.log(() => 1)',
      'console.profile("x")',
      'console.log("ok") && run()',
      `console.log(${`${'['.repeat(34)}0${']'.repeat(34)}`})`,
    ]) expect(parseConsoleInvocation(line), line).toBeNull()
  })

  it('accepts structured bridge payloads without invoking accessors', () => {
    expect(parseConsoleInvocation({ method: 'WARN', args: ['hot'], timestamp: 42 })).toEqual({
      method: 'warn', args: ['hot'], timestamp: 42,
    })
    let getterCalls = 0
    const hostile = Object.defineProperty({}, 'method', {
      enumerable: true,
      get() { getterCalls++; return 'log' },
    })
    expect(parseConsoleInvocation(hostile)).toBeNull()
    expect(getterCalls).toBe(0)
    expect(parseConsoleInvocation({ method: 'log', args: 'not-an-array' })).toBeNull()

    const hostileArgs = new Array(1)
    Object.defineProperty(hostileArgs, '0', {
      enumerable: true,
      get() { getterCalls++; return 'unsafe' },
    })
    expect(parseConsoleInvocation({ method: 'log', args: hostileArgs })).toBeNull()
    expect(getterCalls).toBe(0)
  })

  it('keeps prototype-looking object keys as inert own data', () => {
    const invocation = parseConsoleInvocation('console.log({"__proto__": {polluted: true}, constructor: "text"})')
    const value = invocation?.args[0] as Record<string, unknown>
    expect(Object.getPrototypeOf(value)).toBeNull()
    expect(Object.prototype.hasOwnProperty.call(value, '__proto__')).toBe(true)
    expect(({} as { polluted?: boolean }).polluted).toBeUndefined()
  })
})

describe('console format substitutions', () => {
  it('supports string, integer, float, object, percent, and trailing arguments', () => {
    const tokens = formatConsoleTokens([
      'device=%s d=%d i=%i f=%f small=%o full=%O %% %x',
      'ready', '12.9', 7.8, '3.25', { ok: true }, { nested: [1, 2] }, { trailing: true },
    ])
    const text = consoleTokensText(tokens)
    expect(text).toContain('device=ready d=12 i=7 f=3.25')
    expect(text).toContain('% %x')
    expect(text).toContain('trailing: true')
    expect(tokens.some((token) => token.kind === 'value' && token.presentation === 'compact')).toBe(true)
    expect(tokens.some((token) => token.kind === 'value' && token.presentation === 'expanded')).toBe(true)
  })

  it('leaves missing and unknown substitutions literal and handles escaped percent', () => {
    expect(consoleTokensText(formatConsoleTokens(['%s %d %q %%', 'one']))).toBe('one %d %q %')
    expect(consoleTokensText(formatConsoleTokens([]))).toBe('')
    expect(consoleTokensText(formatConsoleTokens([42, 'tail']))).toBe('42 tail')
  })

  it('keeps markup as text-only token data', () => {
    const attack = '<img src=x onerror=alert(1)><script>run()</script>'
    const tokens = formatConsoleTokens(['%s', attack])
    expect(consoleTokensText(tokens)).toBe(attack)
    expect(Object.keys(tokens[0]).sort()).toEqual(['kind', 'style', 'text'])
    expect(tokens.every((token) => !('html' in token))).toBe(true)
  })

  it('formats circular data and accessor properties without executing getters', () => {
    let getterCalls = 0
    const value: Record<string, unknown> = { status: 'ready' }
    Object.defineProperty(value, 'secret', {
      enumerable: true,
      get() { getterCalls++; return 'unsafe' },
    })
    value.self = value
    const rendered = inspectConsoleValue(value)
    expect(rendered).toContain('secret: [Getter]')
    expect(rendered).toContain('self: [Circular]')
    expect(getterCalls).toBe(0)
  })
})

describe('console style safety and native mirroring', () => {
  it('allows only a narrow property and value set', () => {
    expect(parseConsoleStyle(
      'color:#0ff; background-color:rgb(1, 2, 3); font-weight:700; font-style:italic; ' +
      'text-decoration:underline line-through; position:fixed; background-image:url(https://bad); width:9999px',
    )).toEqual({
      color: '#0ff',
      backgroundColor: 'rgb(1, 2, 3)',
      fontWeight: '700',
      fontStyle: 'italic',
      textDecorationLine: 'underline line-through',
    })
    expect(parseConsoleStyle('color:var(--secret);color:expression(run());color:<red>')).toEqual({})
    expect(parseConsoleStyle('color:#12345;color:#1234567')).toEqual({})
  })

  it('applies and resets %c styles as structured token metadata', () => {
    const tokens = formatConsoleTokens([
      '%chot%c plain',
      'color:red;font-weight:bold;position:absolute',
      '',
    ])
    expect(tokens).toEqual([
      { kind: 'text', text: 'hot', style: { color: 'red', fontWeight: 'bold' } },
      { kind: 'text', text: ' plain', style: undefined },
    ])
    expect(consoleTokensToNativeArgs(tokens)).toEqual([
      '%c%s%c%s',
      'color:red;font-weight:bold',
      'hot',
      '',
      ' plain',
    ])
  })

  it('passes mirrored text as %s data instead of a native format string', () => {
    const tokens = formatConsoleTokens(['%s', '%c<script>'])
    const mirrored = consoleTokensToNativeArgs(tokens)
    expect(mirrored[0]).toBe('%s')
    expect(mirrored[1]).toBe('%c<script>')
  })

  it('preserves Unicode and keeps ANSI-like input out of the native format string', () => {
    const unicode = 'در باز است 🚪 — Δt=۱٫۵ ms'
    const ansi = '\u001b[31mHOT\u001b[0m'
    const tokens = formatConsoleTokens(['%s %s', unicode, ansi])
    const mirrored = consoleTokensToNativeArgs(tokens)

    expect(consoleTokensText(tokens)).toBe(`${unicode} ${ansi}`)
    expect(mirrored[0]).not.toContain('\u001b')
    expect(mirrored.slice(1).some((value) => String(value).includes(ansi))).toBe(true)
  })
})

describe('stateful browser console model', () => {
  it('supports safe dir, dirxml, and trace entries', () => {
    let captures = 0
    const model = new BrowserConsoleModel({
      captureStack: () => { captures++; return 'Trace\r\n  at local:1\u001b[31m' },
    })
    const value = { node: '<device>', state: { ready: true } }
    for (const method of ['dir', 'dirxml'] as const) {
      const entry = model.invoke(method, [value], 1).emitted[0] as ConsoleMessageEntry
      expect(entry.tokens).toMatchObject([{ kind: 'value', presentation: 'expanded' }])
      expect(consoleTokensText(entry.tokens)).toContain('<device>')
      expect(entry.tokens.every((token) => !('html' in token))).toBe(true)
    }
    const traced = model.invoke('trace', ['link %s', 'lost'], 2).emitted[0] as ConsoleMessageEntry
    expect(traced.level).toBe('debug')
    expect(consoleTokensText(traced.tokens)).toBe('link lost\nTrace\n  at local:1')
    expect(captures).toBe(1)

    const remote = model.dispatch({ method: 'trace', args: ['remote'], stack: 'bridge.ts:42' })
      .emitted[0] as ConsoleMessageEntry
    expect(consoleTokensText(remote.tokens)).toBe('remote\nbridge.ts:42')
    expect(captures).toBe(1)
  })

  it('records levels and keeps a bounded transcript', () => {
    const model = new BrowserConsoleModel({ maxEntries: 2 })
    model.invoke('log', ['one'], 1)
    model.invoke('info', ['two'], 2)
    const result = model.invoke('error', ['three'], 3)
    expect(result.transcript.map((entry) => entry.id)).toEqual([2, 3])
    expect((result.emitted[0] as ConsoleMessageEntry).level).toBe('error')
  })

  it('tracks nested and collapsed groups without discarding their entries', () => {
    const model = new BrowserConsoleModel()
    const header = model.invoke('groupCollapsed', ['Transport'], 1).emitted[0]
    const hidden = model.invoke('warn', ['retry'], 2).emitted[0]
    expect(header.kind).toBe('group')
    expect(header.depth).toBe(0)
    expect(header.hidden).toBe(false)
    expect(hidden.depth).toBe(1)
    expect(hidden.hidden).toBe(true)
    expect(hidden.groupPath).toMatchObject([{ label: 'Transport', collapsed: true }])
    model.invoke('groupEnd', [], 3)
    expect(model.invoke('log', ['visible'], 4).emitted[0].hidden).toBe(false)
  })

  it('implements assert without emitting successful assertions', () => {
    const model = new BrowserConsoleModel()
    expect(model.invoke('assert', [true, 'ignored']).ignored).toBe(true)
    const failed = model.invoke('assert', [false, 'port %s', 'closed']).emitted[0] as ConsoleMessageEntry
    expect(failed.level).toBe('error')
    expect(consoleTokensText(failed.tokens)).toBe('Assertion failed: port closed')
  })

  it('counts, resets, and warns for unknown counters', () => {
    const model = new BrowserConsoleModel()
    expect(consoleTokensText((model.invoke('count', ['rx']).emitted[0] as ConsoleMessageEntry).tokens)).toBe('rx: 1')
    expect(consoleTokensText((model.invoke('count', ['rx']).emitted[0] as ConsoleMessageEntry).tokens)).toBe('rx: 2')
    expect(model.invoke('countReset', ['rx']).emitted).toHaveLength(0)
    expect(consoleTokensText((model.invoke('count', ['rx']).emitted[0] as ConsoleMessageEntry).tokens)).toBe('rx: 1')
    const missing = model.invoke('countReset', ['missing']).emitted[0] as ConsoleMessageEntry
    expect(missing.level).toBe('warn')

    model.invoke('count', ['__proto__'])
    const snapshot = model.snapshot()
    expect(Object.getPrototypeOf(snapshot.counters)).toBeNull()
    expect(snapshot.counters.__proto__).toBe(1)
  })

  it('tracks timeLog/timeEnd with deterministic timestamps', () => {
    const model = new BrowserConsoleModel()
    expect(model.invoke('time', ['request'], 1_000).emitted).toHaveLength(0)
    const logged = model.invoke('timeLog', ['request', 'phase %d', 2], 1_123.456).emitted[0] as ConsoleMessageEntry
    expect(consoleTokensText(logged.tokens)).toBe('request: 123.456 ms phase 2')
    const ended = model.invoke('timeEnd', ['request'], 2_500).emitted[0] as ConsoleMessageEntry
    expect(consoleTokensText(ended.tokens)).toBe('request: 1.5 s')
    expect(model.snapshot().timers).toEqual([])
    expect((model.invoke('timeEnd', ['request'], 3_000).emitted[0] as ConsoleMessageEntry).level).toBe('warn')
  })

  it('builds bounded tables and never invokes cell getters', () => {
    let getterCalls = 0
    const second = { score: 2 } as { name?: string; score: number }
    Object.defineProperty(second, 'name', {
      enumerable: true,
      get() { getterCalls++; return 'unsafe' },
    })
    const model = new BrowserConsoleModel({ maxTableRows: 2, maxTableColumns: 1 })
    const table = model.invoke('table', [[
      { name: 'alpha', score: 1 }, second, { name: 'omitted', score: 3 },
    ]], 1).emitted[0] as ConsoleTableEntry
    expect(table.columns).toEqual(['(index)', 'name'])
    expect(table.rows).toHaveLength(2)
    expect(table.omittedRows).toBe(1)
    expect(table.omittedColumns).toBe(1)
    expect(consoleTokensText(table.rows[1].cells.name)).toBe('[Getter]')
    expect(getterCalls).toBe(0)
  })

  it('clears display state while preserving counters and timers, and reset clears all state', () => {
    const model = new BrowserConsoleModel()
    model.invoke('count', ['jobs'], 1)
    model.invoke('time', ['uptime'], 1)
    model.invoke('group', ['open'], 1)
    const cleared = model.invoke('clear', [], 2)
    expect(cleared.cleared).toBe(true)
    expect(cleared.transcript).toEqual([])
    expect(model.snapshot()).toMatchObject({ depth: 0, counters: { jobs: 1 }, timers: ['uptime'] })
    expect(consoleTokensText((model.invoke('count', ['jobs'], 3).emitted[0] as ConsoleMessageEntry).tokens)).toBe('jobs: 2')
    model.reset()
    expect(model.snapshot()).toMatchObject({ entries: [], depth: 0, counters: {}, timers: [] })
  })

  it('dispatches parsed lines and emits a safe warning for malformed payloads', () => {
    const model = new BrowserConsoleModel()
    const accepted = model.dispatch('console.debug("value=%d", 9.8)')
    expect((accepted.emitted[0] as ConsoleMessageEntry).level).toBe('debug')
    expect(consoleTokensText((accepted.emitted[0] as ConsoleMessageEntry).tokens)).toBe('value=9')
    const rejected = model.dispatch('console.log(run())').emitted[0] as ConsoleMessageEntry
    expect(rejected.level).toBe('warn')
    expect(consoleTokensText(rejected.tokens)).toBe('Unsupported console invocation payload.')
  })
})
