export interface CommandDescriptor {
  name: string
  aliases?: readonly string[]
  usage: string
  summary: string
  group: string
}

export interface TerminalCompletion {
  value: string
  label: string
  detail: string
}

export type TerminalBeepTarget = 'board' | 'browser' | 'both'

export interface TerminalBeepRequest {
  frequencyHz: number
  durationMS: number
  target?: TerminalBeepTarget
}

export type TerminalCommandExecutor = (
  command: string,
  success?: string,
  options?: { notifyOnSuccess?: boolean },
) => Promise<string>

const localBeepDescriptor: CommandDescriptor = {
  name: 'beep',
  aliases: ['.beep'],
  usage: 'beep [FREQUENCY_HZ] [DURATION_MS] [browser|board|both]',
  summary: 'play a validated Web terminal tone, optionally mirrored by the board',
  group: 'Console',
}

export function normalizeCommandCatalog(value: unknown): CommandDescriptor[] {
  if (!Array.isArray(value)) return []
  const seen = new Set<string>()
  return value.flatMap((candidate) => {
    if (!candidate || typeof candidate !== 'object') return []
    const record = candidate as Record<string, unknown>
    const name = typeof record.name === 'string' ? record.name.trim().toLowerCase() : ''
    const usage = typeof record.usage === 'string' ? record.usage.trim() : ''
    const summary = typeof record.summary === 'string' ? record.summary.trim() : ''
    const group = typeof record.group === 'string' ? record.group.trim() : ''
    if (!name || !usage || !summary || seen.has(name)) return []
    const aliases = Array.isArray(record.aliases)
      ? record.aliases.flatMap((alias) => typeof alias === 'string' && alias.trim() ? [alias.trim().toLowerCase()] : [])
      : []
    seen.add(name)
    return [{ name, aliases, usage, summary, group }]
  })
}

function normalizedCatalog(catalog: readonly CommandDescriptor[]): CommandDescriptor[] {
  const result = [...catalog]
  if (!result.some((descriptor) => descriptor.name.toLowerCase() === localBeepDescriptor.name)) {
    result.push(localBeepDescriptor)
  }
  return result
}

function usageLiterals(descriptor: CommandDescriptor): string[] {
  const seen = new Set<string>()
  const result: string[] = []
  for (const token of descriptor.usage.match(/[A-Za-z][A-Za-z0-9_-]*/g) ?? []) {
    const normalized = token.toLowerCase()
    if (token !== normalized || normalized === descriptor.name.toLowerCase() || seen.has(normalized)) continue
    seen.add(normalized)
    result.push(normalized)
  }
  return result
}

/**
 * Completes from the host's immutable command catalog. Argument literals are
 * derived from each descriptor's usage string, so the Web UI does not carry a
 * second command/subcommand registry beside the shell engine.
 */
export function terminalCompletions(
  line: string,
  catalog: readonly CommandDescriptor[],
  limit = 10,
): TerminalCompletion[] {
  const leading = line.match(/^\s*/)?.[0] ?? ''
  const content = line.slice(leading.length)
  const firstWhitespace = content.search(/\s/)
  const descriptors = normalizedCatalog(catalog)

  if (firstWhitespace < 0) {
    const prefix = content.toLowerCase()
    return descriptors
      .filter((descriptor) => [descriptor.name, ...(descriptor.aliases ?? [])]
        .some((name) => name.toLowerCase().startsWith(prefix)))
      .slice(0, limit)
      .map((descriptor) => ({
        value: `${leading}${descriptor.name} `,
        label: descriptor.name,
        detail: `${descriptor.usage} — ${descriptor.summary}`,
      }))
  }

  const commandName = content.slice(0, firstWhitespace).toLowerCase()
  const descriptor = descriptors.find((candidate) => [candidate.name, ...(candidate.aliases ?? [])]
    .some((name) => name.toLowerCase() === commandName))
  if (!descriptor) return []

  const tokenStart = Math.max(line.lastIndexOf(' '), line.lastIndexOf('\t')) + 1
  const prefix = line.slice(tokenStart).toLowerCase()
  return usageLiterals(descriptor)
    .filter((literal) => literal.startsWith(prefix) && literal !== prefix)
    .slice(0, limit)
    .map((literal) => ({
      value: `${line.slice(0, tokenStart)}${literal} `,
      label: literal,
      detail: descriptor.usage,
    }))
}

function beepArguments(value: string): string[] | null {
  const functionStyle = value.match(/^\.?beep\s*\((.*)\)\s*$/i)
  if (functionStyle) {
    const body = functionStyle[1].trim()
    if (!body) return []
    return body.split(',').map((argument) => argument.trim().replace(/^(['"])(.*)\1$/, '$2'))
  }
  const shellStyle = value.match(/^\.?beep(?:\s+(.*))?$/i)
  if (!shellStyle) return null
  return shellStyle[1]?.trim().split(/\s+/).filter(Boolean) ?? []
}

/** Parses only the browser-local beep alias. Other commands return null. */
export function parseTerminalBeep(value: string): TerminalBeepRequest | null {
  const args = beepArguments(value.trim())
  if (args === null) return null
  if (args.length > 3) throw new Error('usage: beep [FREQUENCY_HZ] [DURATION_MS] [browser|board|both]')

  const frequencyHz = args[0] === undefined ? 2000 : Number(args[0])
  const durationMS = args[1] === undefined ? 40 : Number(args[1])
  const target = args[2]?.toLowerCase() as TerminalBeepTarget | undefined
  if (!Number.isFinite(frequencyHz) || frequencyHz < 20 || frequencyHz > 20_000) {
    throw new RangeError('beep frequency must be between 20 and 20000 Hz')
  }
  if (!Number.isFinite(durationMS) || durationMS < 1 || durationMS > 60_000) {
    throw new RangeError('beep duration must be between 1 and 60000 ms')
  }
  if (target !== undefined && target !== 'board' && target !== 'browser' && target !== 'both') {
    throw new TypeError('beep target must be browser, board, or both')
  }
  return { frequencyHz: Math.round(frequencyHz), durationMS: Math.round(durationMS), target }
}

/**
 * Executes commands originating inside the terminal without duplicating their
 * successful result as an application toast. Rejections remain untouched so
 * the terminal and the shared command failure path can both report them.
 */
export function executeTerminalCommand(execute: TerminalCommandExecutor, value: string): Promise<string> {
  return execute(value, undefined, { notifyOnSuccess: false })
}

/** Small state object for shell-like Up/Down history with draft restoration. */
export class TerminalHistory {
  private readonly entries: string[] = []
  private cursor = 0
  private draft = ''

  constructor(private readonly limit = 100) {}

  record(value: string): void {
    const normalized = value.trim()
    if (normalized && this.entries[this.entries.length - 1] !== normalized) {
      this.entries.push(normalized)
      if (this.entries.length > this.limit) this.entries.splice(0, this.entries.length - this.limit)
    }
    this.cursor = this.entries.length
    this.draft = ''
  }

  edited(value: string): void {
    this.cursor = this.entries.length
    this.draft = value
  }

  move(direction: -1 | 1, currentValue: string): string {
    if (!this.entries.length) return currentValue
    if (this.cursor === this.entries.length) this.draft = currentValue
    this.cursor = Math.max(0, Math.min(this.entries.length, this.cursor + direction))
    return this.cursor === this.entries.length ? this.draft : this.entries[this.cursor]
  }

  values(): readonly string[] {
    return [...this.entries]
  }
}
