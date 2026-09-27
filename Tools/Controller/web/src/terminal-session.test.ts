import { describe, expect, it, vi } from 'vitest'
import { executeTerminalCommand, normalizeCommandCatalog, parseTerminalBeep, TerminalHistory, terminalCompletions, type CommandDescriptor } from './terminal-session'

const catalog: CommandDescriptor[] = [
  { name: 'status', aliases: ['st'], usage: 'status', summary: 'read status', group: 'Connection' },
  { name: 'melody', usage: 'melody list|create NAME NOTE...|delete NAME|play NAME [REPEATS]|wait NAME [REPEATS]|stop|status', summary: 'configured melodies', group: 'Outputs' },
]

describe('Web terminal session', () => {
  it('derives command and subcommand completion from the canonical catalog', () => {
    expect(terminalCompletions('st', catalog)[0]).toMatchObject({ value: 'status ', label: 'status' })
    expect(terminalCompletions('melody p', catalog)).toEqual([
      { value: 'melody play ', label: 'play', detail: catalog[1].usage },
    ])
    expect(terminalCompletions('melody ', catalog).map((item) => item.label)).toEqual([
      'list', 'create', 'delete', 'play', 'wait', 'stop', 'status',
    ])
  })

  it('normalizes malformed network catalog entries before completion', () => {
    expect(normalizeCommandCatalog([
      { name: ' Status ', aliases: [' ST ', 42], usage: 'status', summary: ' read status ', group: 'Read' },
      { name: 'status', usage: 'duplicate', summary: 'duplicate', group: 'Read' },
      { name: '', usage: 'bad', summary: 'bad' },
      null,
    ])).toEqual([{ name: 'status', aliases: ['st'], usage: 'status', summary: 'read status', group: 'Read' }])
  })

  it('adds the Web-only beep alias without replacing a server descriptor', () => {
    expect(terminalCompletions('bee', catalog)[0]?.value).toBe('beep ')
    const serverCatalog = [...catalog, { name: 'beep', usage: 'beep custom', summary: 'server', group: 'Console' }]
    expect(terminalCompletions('bee', serverCatalog)).toHaveLength(1)
    expect(terminalCompletions('bee', serverCatalog)[0]?.detail).toContain('beep custom')
  })

  it('parses default, shell, and function-style beep requests', () => {
    expect(parseTerminalBeep('beep')).toEqual({ frequencyHz: 2000, durationMS: 40, target: undefined })
    expect(parseTerminalBeep('beep 440 125 browser')).toEqual({ frequencyHz: 440, durationMS: 125, target: 'browser' })
    expect(parseTerminalBeep('.beep(660, 80, "both")')).toEqual({ frequencyHz: 660, durationMS: 80, target: 'both' })
    expect(parseTerminalBeep('status')).toBeNull()
    expect(() => parseTerminalBeep('beep 10')).toThrow(/20 and 20000/)
  })

  it('navigates deduplicated history and restores the current draft', () => {
    const history = new TerminalHistory(3)
    history.record('status')
    history.record('status')
    history.record('melody list')
    expect(history.values()).toEqual(['status', 'melody list'])
    expect(history.move(-1, 'bee')).toBe('melody list')
    expect(history.move(-1, 'melody list')).toBe('status')
    expect(history.move(1, 'status')).toBe('melody list')
    expect(history.move(1, 'melody list')).toBe('bee')
  })

  it('suppresses success toasts for every command dispatched from the terminal', async () => {
    const execute = vi.fn(async () => 'done')
    await expect(executeTerminalCommand(execute, 'status')).resolves.toBe('done')
    expect(execute).toHaveBeenCalledWith('status', undefined, { notifyOnSuccess: false })
  })
})
