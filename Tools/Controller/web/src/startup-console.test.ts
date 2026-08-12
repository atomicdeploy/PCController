import { describe, expect, it, vi } from 'vitest'
import { emitStartupConsoleIntroduction, type StartupConsoleTarget } from './startup-console'

function target() {
  return {
    groupCollapsed: vi.fn((..._data: unknown[]) => undefined),
    info: vi.fn((..._data: unknown[]) => undefined),
    debug: vi.fn((..._data: unknown[]) => undefined),
    groupEnd: vi.fn(() => undefined),
  } satisfies StartupConsoleTarget
}

describe('startup browser console', () => {
  it('uses fixed %c styles and reports an offline controller truthfully', () => {
    const sink = target()
    emitStartupConsoleIntroduction({
      productTitle: 'PCController',
		config: { host_version: '1.2.3' },
      boardConnected: false,
      streamState: 'open',
    }, sink)

    expect(sink.groupCollapsed).toHaveBeenCalledWith(expect.stringContaining('%c%s%c'), expect.any(String), 'PCController', expect.any(String))
		expect(sink.info).toHaveBeenCalledWith('%cHost%c 🧩 version %s · versionless living API', expect.any(String), expect.any(String), '1.2.3')
    expect(sink.info).toHaveBeenCalledWith('%cController%c 🟠 offline · no authenticated board', expect.any(String), expect.any(String))
    expect(sink.debug).toHaveBeenCalledWith('%cTransport%c 📡 %s · subscribe with addEventListener("pccontroller:state", handler)', expect.any(String), expect.any(String), 'Host event stream connected')
    expect(sink.info.mock.calls.flat().join(' ')).not.toContain('Live')
    expect(sink.groupEnd).toHaveBeenCalledOnce()
  })

  it('keeps configuration text out of format and style positions', () => {
    const sink = target()
    emitStartupConsoleIntroduction({
      productTitle: '%c hostile\nname',
		config: { host_version: '%c 9.0' },
      boardConnected: true,
      port: '%c COM18',
      streamState: 'connecting',
    }, sink)

		expect(sink.groupCollapsed.mock.calls[0][0]).toBe('%c%s%c ✨ browser control center')
    expect(sink.groupCollapsed.mock.calls[0][2]).toBe('%c hostile name')
    expect(sink.info).toHaveBeenCalledWith('%cController%c 🟢 connected · %s', expect.any(String), expect.any(String), '%c COM18')
  })
})
