import type { UIConfig } from './types'

export type StartupStreamState = 'connecting' | 'open' | 'waiting' | 'closed'

export interface StartupConsoleFacts {
  productTitle: string
	config: Pick<UIConfig, 'host_version'> | null
  boardConnected: boolean
  port?: string
  streamState: StartupStreamState
  demonstration?: boolean
}

export interface StartupConsoleTarget {
  groupCollapsed: (...data: unknown[]) => void
  info: (...data: unknown[]) => void
  debug: (...data: unknown[]) => void
  groupEnd: () => void
}

const titleStyle = [
  'background:#7656d6',
  'border-radius:7px',
  'color:#fff',
  'font:600 12px/1.8 ui-sans-serif,system-ui,sans-serif',
  'padding:2px 8px',
].join(';')

const labelStyle = [
  'color:#9b7ce8',
  'font:600 11px/1.6 ui-sans-serif,system-ui,sans-serif',
].join(';')

const connectedStyle = 'color:#2f9e67;font:600 11px/1.6 ui-sans-serif,system-ui,sans-serif'
const offlineStyle = 'color:#d8793f;font:600 11px/1.6 ui-sans-serif,system-ui,sans-serif'
const resetStyle = 'color:inherit;font:inherit'

function boundedConsoleValue(value: unknown, fallback: string, limit = 96): string {
  const clean = String(value ?? '')
    .normalize('NFKC')
    .replace(/[\u0000-\u001f\u007f-\u009f]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return clean ? clean.slice(0, limit) : fallback
}

function streamLabel(state: StartupStreamState): string {
  switch (state) {
    case 'open': return 'Host event stream connected'
    case 'connecting': return 'Host event stream connecting'
    case 'waiting': return 'Host event stream waiting to reconnect'
    case 'closed': return 'Host event stream closed'
  }
}

/**
 * Writes the small, factual browser-console welcome shown once by App. All CSS
 * is fixed here; host and controller values are passed through `%s` slots so
 * configuration text can never become a format string or a style directive.
 */
export function emitStartupConsoleIntroduction(
  facts: StartupConsoleFacts,
  target: StartupConsoleTarget = console,
): void {
  const title = boundedConsoleValue(facts.productTitle, 'Controller')
  const version = facts.demonstration
    ? 'demonstration'
    : boundedConsoleValue(facts.config?.host_version, 'not reported')
  const port = facts.boardConnected ? boundedConsoleValue(facts.port, 'authenticated port') : ''

  target.groupCollapsed('%c%s%c · browser control center', titleStyle, title, resetStyle)
	target.info('%cHost%c version %s · living API', labelStyle, resetStyle, version)
  if (facts.boardConnected) {
    target.info('%cController%c connected · %s', connectedStyle, resetStyle, port)
  } else {
    target.info('%cController%c offline · no authenticated board', offlineStyle, resetStyle)
  }
  target.debug('%cTransport%c %s', labelStyle, resetStyle, streamLabel(facts.streamState))
  target.groupEnd()
}
