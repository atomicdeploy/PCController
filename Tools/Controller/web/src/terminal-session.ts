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

/** Accept only complete descriptors returned by the connected host. */
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
    if (!name || !usage || !summary || !group || seen.has(name)) return []
    const aliases = Array.isArray(record.aliases)
      ? [...new Set(record.aliases.flatMap((alias) => typeof alias === 'string' && alias.trim()
        ? [alias.trim().toLowerCase()]
        : []))]
      : []
    seen.add(name)
    return [{ name, aliases, usage, summary, group }]
  })
}

function usageLiterals(descriptor: CommandDescriptor): string[] {
  const seen = new Set<string>()
  const result: string[] = []
  for (const token of descriptor.usage.match(/[A-Za-z][A-Za-z0-9_-]*/g) ?? []) {
    const normalized = token.toLowerCase()
    if (token !== normalized || normalized === descriptor.name || seen.has(normalized)) continue
    seen.add(normalized)
    result.push(normalized)
  }
  return result
}

/** Derive completions exclusively from the catalog returned by the current host. */
export function terminalCompletions(
  line: string,
  catalog: readonly CommandDescriptor[],
  limit = 10,
): TerminalCompletion[] {
  const leading = line.match(/^\s*/)?.[0] ?? ''
  const content = line.slice(leading.length)
  if (!content) return []
  const firstWhitespace = content.search(/\s/)

  if (firstWhitespace < 0) {
    const prefix = content.toLowerCase()
    return catalog
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
  const descriptor = catalog.find((candidate) => [candidate.name, ...(candidate.aliases ?? [])]
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

/** In-memory shell-style Up/Down history with draft restoration. */
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
