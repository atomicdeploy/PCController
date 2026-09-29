import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { normalizeCommandCatalog, TerminalHistory, terminalCompletions, type CommandDescriptor } from './terminal-session'

const returnedByHost: CommandDescriptor[] = [
  { name: 'status', aliases: ['st'], usage: 'status', summary: 'read status', group: 'Connection' },
  { name: 'melody', usage: 'melody list|create NAME NOTE...|delete NAME|play NAME [REPEATS]|wait NAME [REPEATS]|stop|status', summary: 'configured melodies', group: 'Outputs' },
]

describe('host-backed Web terminal session', () => {
  it('derives command and argument completion only from host descriptors', () => {
    expect(terminalCompletions('st', returnedByHost)[0]).toMatchObject({ value: 'status ', label: 'status' })
    expect(terminalCompletions('melody p', returnedByHost)).toEqual([
      { value: 'melody play ', label: 'play', detail: returnedByHost[1].usage },
    ])
    expect(terminalCompletions('bee', returnedByHost)).toEqual([])
    expect(terminalCompletions('status', [])).toEqual([])
    expect(terminalCompletions('', returnedByHost)).toEqual([])
  })

  it('normalizes malformed host responses without inventing descriptors', () => {
    expect(normalizeCommandCatalog([
      { name: ' Status ', aliases: [' ST ', 'st', 42], usage: 'status', summary: ' read status ', group: 'Read' },
      { name: 'status', usage: 'duplicate', summary: 'duplicate', group: 'Read' },
      { name: 'missing-group', usage: 'missing-group', summary: 'invalid' },
      null,
    ])).toEqual([{ name: 'status', aliases: ['st'], usage: 'status', summary: 'read status', group: 'Read' }])
  })

  it('navigates deduplicated bounded history and restores the current draft', () => {
    const history = new TerminalHistory(2)
    history.record('status')
    history.record('status')
    history.record('melody list')
    history.record('help')
    expect(history.values()).toEqual(['melody list', 'help'])
    expect(history.move(-1, 'sta')).toBe('help')
    expect(history.move(-1, 'help')).toBe('melody list')
    expect(history.move(1, 'melody list')).toBe('help')
    expect(history.move(1, 'help')).toBe('sta')
  })

  it('keeps the runtime catalog source host-only', () => {
    const sessionSource = readFileSync(new URL('./terminal-session.ts', import.meta.url), 'utf8')
    const workbenchSource = readFileSync(new URL('./workbench.tsx', import.meta.url), 'utf8')
    expect(workbenchSource).toContain("rpc<unknown>('controller.command.catalog'")
    expect(`${sessionSource}\n${workbenchSource}`).not.toMatch(/localBeepDescriptor|staticCommandCatalog|sampleCommand|exampleCommand/i)
  })
})
