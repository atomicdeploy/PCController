import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  TAB_CHANNEL_PROTOCOL,
  TAB_CHANNEL_VERSION,
  createTabChannel,
  type BroadcastChannelPort,
  type TabChannelEnvelope,
  type TabChannelIDKind,
} from './tab-channel'
import { applyPushedOutputEvent } from './status-led-event'
import { emptySnapshot } from './types'
import type { ControllerEvent, Snapshot } from './types'

class FakeBroadcastChannel implements BroadcastChannelPort {
  static rooms = new Map<string, Set<FakeBroadcastChannel>>()
  static posted: Array<{ name: string; value: unknown }> = []

  readonly name: string
  readonly listeners = new Set<EventListener>()
  closed = false

  constructor(name: string) {
    this.name = name
    const room = FakeBroadcastChannel.rooms.get(name) ?? new Set()
    room.add(this)
    FakeBroadcastChannel.rooms.set(name, room)
  }

  static reset() {
    FakeBroadcastChannel.rooms.clear()
    FakeBroadcastChannel.posted = []
  }

  static inject(name: string, value: unknown) {
    for (const member of FakeBroadcastChannel.rooms.get(name) ?? []) member.deliver(value)
  }

  postMessage(value: unknown) {
    if (this.closed) throw new Error('closed')
    FakeBroadcastChannel.posted.push({ name: this.name, value })
    for (const member of FakeBroadcastChannel.rooms.get(this.name) ?? []) {
      if (member !== this) member.deliver(value)
    }
  }

  addEventListener(type: 'message', listener: EventListener) {
    if (type === 'message') this.listeners.add(listener)
  }

  removeEventListener(type: 'message', listener: EventListener) {
    if (type === 'message') this.listeners.delete(listener)
  }

  close() {
    if (this.closed) return
    this.closed = true
    this.listeners.clear()
    FakeBroadcastChannel.rooms.get(this.name)?.delete(this)
  }

  private deliver(value: unknown) {
    if (this.closed) return
    const event = { data: value } as MessageEvent<unknown>
    for (const listener of [...this.listeners]) listener(event as unknown as Event)
  }
}

function sequenceFactory(prefix: string) {
  let sequence = 0
  return (kind: TabChannelIDKind) => `${prefix}-${kind}-${++sequence}`
}

function baseEnvelope(
  channel: ReturnType<typeof createTabChannel>,
  now: number,
  overrides: Partial<TabChannelEnvelope> = {},
): TabChannelEnvelope {
  return {
    protocol: TAB_CHANNEL_PROTOCOL,
    version: TAB_CHANNEL_VERSION,
    origin: channel.origin,
    messageId: 'remote-message-0001',
    tabId: 'tab:remote-identity:1',
    sentAt: now - 100,
    expiresAt: now + 1_000,
    payload: { type: 'presence', state: 'active', page: 'dashboard' },
    ...overrides,
  }
}

describe('tab channel', () => {
  beforeEach(() => FakeBroadcastChannel.reset())

  it('scopes its versioned transport to origin and gives every tab a distinct identity', () => {
    const fixedFactory = () => 'fixed-identity'
    const first = createTabChannel({ origin: 'HTTPS://CONTROL.EXAMPLE/', BroadcastChannel: FakeBroadcastChannel, idFactory: fixedFactory })
    const second = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, idFactory: fixedFactory })
    const isolated = createTabChannel({ origin: 'https://other.example', BroadcastChannel: FakeBroadcastChannel, idFactory: fixedFactory })

    expect(first.supported).toBe(true)
    expect(first.origin).toBe('https://control.example')
    expect(first.channelName).toBe(second.channelName)
    expect(first.channelName).toContain(`:v${TAB_CHANNEL_VERSION}:`)
    expect(isolated.channelName).not.toBe(first.channelName)
    expect(first.tabId).not.toBe(second.tabId)

    first.close()
    second.close()
    isolated.close()
  })

  it('delivers all typed payloads and reconstructs sanitized protocol data', () => {
    let now = 10_000
    const sender = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, now: () => now, idFactory: sequenceFactory('sender') })
    const receiver = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, now: () => now, idFactory: sequenceFactory('receiver') })
    const received: TabChannelEnvelope[] = []
    receiver.subscribe((message) => received.push(message))

    expect(sender.publishPresence('active', 'workbench')).toBeTruthy()
    expect(sender.publishAppearance({ theme: 'dark', locale: 'fa', direction: 'rtl', audioVolume: 0.4 }, 'a'.repeat(64))).toBeTruthy()
    expect(sender.publishTerminal({ kind: 'output', text: 'safe\u0000 output\u202e', at: now })).toBeTruthy()
    expect(sender.publishControllerEvent({
      id: 9,
      time: '2026-08-02T00:00:00.000Z',
      kind: 'door',
      text: 'opened',
      state: 'open',
      metadata: { zone: 'front' },
    })).toBeTruthy()

    expect(received).toHaveLength(4)
    expect(received.map((message) => message.payload.type)).toEqual([
      'presence', 'appearance', 'terminal', 'controller-event',
    ])
    expect(received[0]).toMatchObject({
      protocol: TAB_CHANNEL_PROTOCOL,
      version: TAB_CHANNEL_VERSION,
      origin: 'https://control.example',
      tabId: sender.tabId,
    })
    expect(received[2].payload).toEqual({
      type: 'terminal',
      entry: { kind: 'output', text: 'safe output', at: now },
    })
    expect(received[1].payload).toMatchObject({ type: 'appearance', etag: 'a'.repeat(64) })
    expect(received[3].payload).toMatchObject({
      event: { id: 9, metadata: { zone: 'front' } },
    })

    now += 1
    sender.close()
    receiver.close()
  })

  it('keeps two Web tabs on the same pushed seven-segment frame without refresh polling', () => {
    const first = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, idFactory: sequenceFactory('first') })
    const second = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, idFactory: sequenceFactory('second') })
    const frame: ControllerEvent = {
      id: 44,
      time: '2026-08-12T10:00:00.000Z',
      kind: 'front_panel.segment',
      stream: 'state',
      text: 'changed',
      metadata: { raw_segments: '6D3F546E', brightness: '7' },
    }
    let firstSnapshot: Snapshot = applyPushedOutputEvent(emptySnapshot, frame)
    let secondSnapshot: Snapshot = emptySnapshot
    second.subscribe(({ payload }) => {
      if (payload.type === 'controller-event') secondSnapshot = applyPushedOutputEvent(secondSnapshot, payload.event as ControllerEvent)
    })

    first.publishControllerEvent(frame)

    expect(firstSnapshot.front_panel?.raw_segments).toEqual([0x6d, 0x3f, 0x54, 0x6e])
    expect(secondSnapshot.front_panel?.raw_segments).toEqual(firstSnapshot.front_panel?.raw_segments)
    expect(firstSnapshot.front_panel_updated).toBe(frame.time)
    expect(secondSnapshot.front_panel_updated).toBe(frame.time)
    first.close()
    second.close()
  })

  it('rejects secrets, unknown fields, invalid values, and oversized content before posting', () => {
    const channel = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, now: () => 1_000, idFactory: sequenceFactory('safe') })

    expect(channel.publishTerminal({ kind: 'command', text: 'Authorization: Bearer abcdefghijklmnop' })).toBeNull()
    expect(channel.publishTerminal({ kind: 'command', text: 'Authoriz\u202eation: Bearer abcdefghijklmnop' })).toBeNull()
    expect(channel.publishTerminal({ kind: 'output', text: 'x'.repeat(8_193) })).toBeNull()
    expect(channel.publishControllerEvent({
      id: 1,
      time: '2026-08-02T00:00:00Z',
      kind: 'message',
      text: 'safe',
      metadata: { accessToken: 'do-not-share' },
    })).toBeNull()
    expect(channel.publish({
      type: 'appearance',
      appearance: { theme: 'dark', accessToken: 'hidden' },
    } as never)).toBeNull()
    expect(channel.publishAppearance({ audioVolume: 2 })).toBeNull()
    expect(channel.publishAppearance({ theme: 'dark' }, 'not-an-etag')).toBeNull()
    expect(channel.publishPresence('active', '../unsafe page')).toBeNull()
    expect(FakeBroadcastChannel.posted).toHaveLength(0)

    channel.close()
  })

  it('bounds caller-provided identity seeds so locally published envelopes remain valid', () => {
    const sender = createTabChannel({
      origin: 'https://control.example',
      BroadcastChannel: FakeBroadcastChannel,
      idFactory: () => 'a'.repeat(180),
    })
    const receiver = createTabChannel({
      origin: 'https://control.example',
      BroadcastChannel: FakeBroadcastChannel,
      idFactory: sequenceFactory('receiver'),
    })
    const listener = vi.fn()
    receiver.subscribe(listener)

    expect(sender.tabId.length).toBeLessThanOrEqual(180)
    expect(sender.publishPresence('active')).toBeTruthy()
    expect(listener).toHaveBeenCalledTimes(1)

    sender.close()
    receiver.close()
  })

  it('ignores self, expired, future, wrong-origin, wrong-version, malformed, and duplicate envelopes', () => {
    let now = 50_000
    const channel = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, now: () => now, idFactory: sequenceFactory('local') })
    const listener = vi.fn()
    channel.subscribe(listener)

    FakeBroadcastChannel.inject(channel.channelName, baseEnvelope(channel, now, { tabId: channel.tabId, messageId: 'self-message-0001' }))
    FakeBroadcastChannel.inject(channel.channelName, baseEnvelope(channel, now, { expiresAt: now, messageId: 'expired-message-0001' }))
    FakeBroadcastChannel.inject(channel.channelName, baseEnvelope(channel, now, { sentAt: now + 300_001, expiresAt: now + 300_002, messageId: 'future-message-0001' }))
    FakeBroadcastChannel.inject(channel.channelName, baseEnvelope(channel, now, { origin: 'https://other.example', messageId: 'origin-message-0001' }))
    FakeBroadcastChannel.inject(channel.channelName, { ...baseEnvelope(channel, now), version: 99, messageId: 'version-message-0001' })
    FakeBroadcastChannel.inject(channel.channelName, { hello: 'world' })

    const valid = baseEnvelope(channel, now, { messageId: 'dedupe-message-0001' })
    FakeBroadcastChannel.inject(channel.channelName, valid)
    FakeBroadcastChannel.inject(channel.channelName, valid)
    expect(listener).toHaveBeenCalledTimes(1)

    now += 2_000
    FakeBroadcastChannel.inject(channel.channelName, baseEnvelope(channel, now, { messageId: 'fresh-message-0001' }))
    expect(listener).toHaveBeenCalledTimes(2)
    channel.close()
  })

  it('isolates subscriber failures and supports deterministic unsubscription', () => {
    const sender = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, idFactory: sequenceFactory('sender') })
    const receiver = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, idFactory: sequenceFactory('receiver') })
    const first = vi.fn(() => { throw new Error('consumer failure') })
    const second = vi.fn()
    const stopFirst = receiver.subscribe(first)
    receiver.subscribe(second)

    sender.publishPresence('active')
    expect(first).toHaveBeenCalledTimes(1)
    expect(second).toHaveBeenCalledTimes(1)

    stopFirst()
    stopFirst()
    sender.publishPresence('hidden')
    expect(first).toHaveBeenCalledTimes(1)
    expect(second).toHaveBeenCalledTimes(2)

    sender.close()
    receiver.close()
  })

  it('closes cleanly and becomes inert', () => {
    const sender = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, idFactory: sequenceFactory('sender') })
    const receiver = createTabChannel({ origin: 'https://control.example', BroadcastChannel: FakeBroadcastChannel, idFactory: sequenceFactory('receiver') })
    const listener = vi.fn()
    receiver.subscribe(listener)
    const receiverPort = [...(FakeBroadcastChannel.rooms.get(receiver.channelName) ?? [])]
      .find((candidate) => candidate !== [...(FakeBroadcastChannel.rooms.get(receiver.channelName) ?? [])][0])

    receiver.close()
    receiver.close()
    expect(receiver.publishPresence('active')).toBeNull()
    expect(receiver.subscribe(listener)).toBeTypeOf('function')
    sender.publishPresence('active')
    expect(listener).not.toHaveBeenCalled()
    expect(receiverPort?.closed).toBe(true)

    sender.close()
  })

  it('degrades to a safe no-op when BroadcastChannel is unavailable or construction fails', () => {
    class ThrowingBroadcastChannel {
      constructor() { throw new Error('unavailable') }
    }
    const unsupported = createTabChannel({ origin: 'https://control.example', BroadcastChannel: null, idFactory: sequenceFactory('none') })
    const failed = createTabChannel({ origin: 'https://control.example', BroadcastChannel: ThrowingBroadcastChannel as never, idFactory: sequenceFactory('failed') })

    expect(unsupported.supported).toBe(false)
    expect(failed.supported).toBe(false)
    expect(unsupported.publishPresence('active')).toBeNull()
    expect(failed.publishAppearance({ theme: 'light' })).toBeNull()
    expect(() => unsupported.close()).not.toThrow()
    expect(() => failed.close()).not.toThrow()
  })

  it('rejects invalid transport lifetimes', () => {
    expect(() => createTabChannel({ origin: 'https://control.example', ttlMS: 0, BroadcastChannel: null })).toThrow(RangeError)
    expect(() => createTabChannel({ origin: 'https://control.example', ttlMS: 300_001, BroadcastChannel: null })).toThrow(RangeError)
    expect(() => createTabChannel({ origin: 'https://user:password@control.example', BroadcastChannel: null })).toThrow(TypeError)
  })
})
