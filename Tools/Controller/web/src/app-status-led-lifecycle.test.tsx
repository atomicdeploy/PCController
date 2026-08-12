// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { StreamHandlers, StreamSource } from './api'
import type { Snapshot, UIConfig } from './types'

const mocks = vi.hoisted(() => ({
	getSnapshot: vi.fn(),
	getUIConfig: vi.fn(),
	rpc: vi.fn(),
	execute: vi.fn(),
	connectStream: vi.fn(),
	streamHandlers: null as StreamHandlers | null,
}))

vi.mock('./api', async (importOriginal) => {
	const original = await importOriginal<typeof import('./api')>()
	return {
		...original,
		getToken: () => '',
		getSnapshot: mocks.getSnapshot,
		getUIConfig: mocks.getUIConfig,
		rpc: mocks.rpc,
		execute: mocks.execute,
		connectStream: mocks.connectStream,
	}
})

vi.mock('./audio-engine', () => ({
	createAudioEngine: () => ({
		supported: false, muted: true, status: 'unsupported', volume: 0,
		start: async () => false, setMuted: () => undefined, toggleMuted: () => true,
		setVolume: () => undefined, cue: () => false, playTone: () => false,
		suspend: async () => undefined, resume: async () => false, dispose: async () => undefined,
	}),
}))

vi.mock('./tab-channel', () => ({
	createTabChannel: () => ({
		tabId: '', supported: false, subscribe: () => () => undefined,
		publishPresence: () => undefined, publishAppearance: () => undefined,
		publishControllerEvent: () => undefined, publishResourceVersion: () => undefined,
		publishTerminal: () => undefined, close: () => undefined,
	}),
}))

vi.mock('./state-favicon', () => ({
	controllerFaviconState: () => 'offline', updateRuntimeFavicon: () => undefined,
}))
vi.mock('./startup-console', () => ({ emitStartupConsoleIntroduction: () => undefined }))
vi.mock('./browser-console', () => ({
	publishBrowserConsole: () => undefined, publishBrowserConsoleState: () => undefined,
}))

function SnapshotProbe(props: {
	snapshot: Snapshot
	refresh: () => Promise<void>
	transport: { streamState: string }
}) {
	const led = props.snapshot.status_led
	return <>
		<output data-testid="led-state">{JSON.stringify({
			connected: props.snapshot.connected,
			have: props.snapshot.have_status_led,
			blue: led?.blue,
			revision: props.snapshot.status_led_revision,
			epoch: props.snapshot.status_led_epoch,
			instance: props.snapshot.host_instance_id,
			stream: props.transport.streamState,
		})}</output>
		<button data-testid="refresh" onClick={() => void props.refresh()}>refresh</button>
	</>
}

vi.mock('./views', () => ({
	DashboardView: SnapshotProbe,
	ControlsView: SnapshotProbe,
	LocalDeviceView: SnapshotProbe,
	EventsView: SnapshotProbe,
	SettingsView: SnapshotProbe,
}))
vi.mock('./workbench', () => ({ WorkbenchView: SnapshotProbe }))
vi.mock('./data-workspace', () => ({ DataWorkspaceView: SnapshotProbe }))
vi.mock('./updates-view', () => ({ UpdatesView: SnapshotProbe }))

import App from './app'
import { emptySnapshot } from './types'

const config: UIConfig = {
	name: 'PCController', setup_complete: true,
	websocket_path: '/ipc', session_ticket_path: '/api/session/ticket', auth_required: false,
	appearance: {
		theme: 'dark', locale: 'en', direction: 'ltr', reduceMotion: true,
		compactNumbers: false, audioMuted: true, audioVolume: 0,
	},
	appearance_etag: 'a'.repeat(64),
}

function ledSnapshot(instance: string, blue: number, revision: number): Snapshot {
	return {
		...emptySnapshot,
		connected: true,
		host_instance_id: instance,
		have_status_led: true,
		status_led: { red: 0, green: 0, blue, brightness: 145, effect: 1, condition: 9 },
		status_led_revision: revision,
	}
}

function ledEvent(blue: number, revision?: number, condition = 9) {
	return {
		id: revision ?? blue,
		time: '2026-08-12T00:00:00Z',
		kind: 'status_led.changed',
		text: 'rendered',
		metadata: {
			red: '0', green: '0', blue: String(blue), brightness: blue === 0 ? '0' : '145',
			effect: blue === 0 ? '0' : '1', condition: String(condition),
			...(revision === undefined ? {} : { revision: String(revision) }),
		},
	}
}

function deferred<T>() {
	let resolve!: (value: T) => void
	const promise = new Promise<T>((accept) => { resolve = accept })
	return { promise, resolve }
}

function renderedState() {
	return JSON.parse(screen.getByTestId('led-state').textContent ?? '{}') as {
		connected: boolean
		have: boolean
		blue?: number
		revision?: number
		epoch?: number
		instance?: string
		stream: string
	}
}

async function mountWithStartup(snapshot: Snapshot) {
	mocks.getSnapshot.mockResolvedValueOnce(snapshot)
	render(<App />)
	await waitFor(() => expect(mocks.streamHandlers).not.toBeNull())
	await waitFor(() => expect(screen.getByTestId('led-state')).toBeTruthy())
}

function streamSource(generation: number, instanceID?: string): StreamSource {
	return { generation, instanceID }
}

beforeEach(() => {
	mocks.getSnapshot.mockReset()
	mocks.getUIConfig.mockReset().mockResolvedValue(config)
	mocks.rpc.mockReset().mockImplementation(async (method: string) =>
		method.includes('history') ? [] : {})
	mocks.execute.mockReset().mockResolvedValue({ output: '' })
	mocks.streamHandlers = null
	mocks.connectStream.mockReset().mockImplementation((_config: UIConfig, handlers: StreamHandlers) => {
		mocks.streamHandlers = handlers
		return () => undefined
	})
	window.history.replaceState({}, '', '/#/controls')
	Object.defineProperty(window, 'matchMedia', {
		configurable: true,
		value: () => ({
			matches: false, addEventListener: () => undefined, removeEventListener: () => undefined,
		}),
	})
	Object.defineProperty(globalThis, 'matchMedia', { configurable: true, value: window.matchMedia })
	Object.defineProperty(Element.prototype, 'scrollTo', { configurable: true, value: () => undefined })
})

afterEach(() => {
	cleanup()
	vi.clearAllMocks()
})

describe('mounted App status LED transport lifecycle', () => {
	it('accepts a lower-revision first-stream restart and latest legacy revisionless frames', async () => {
		const primaryA = ledSnapshot('primary-a', 145, 100)
		mocks.getSnapshot.mockResolvedValue(primaryA)
		await mountWithStartup(primaryA)
		expect(renderedState()).toMatchObject({ blue: 145, revision: 100, epoch: 0, instance: 'primary-a' })

		await act(async () => {
			mocks.streamHandlers!.state('open', undefined, streamSource(1, 'primary-b'))
			mocks.streamHandlers!.event(ledEvent(0, 1, 255), streamSource(1, 'primary-b'))
		})
		await waitFor(() => expect(renderedState()).toMatchObject({
			blue: 0, revision: 1, epoch: 1, instance: 'primary-b', stream: 'open',
		}))

		await act(async () => {
			mocks.streamHandlers!.state('connecting', undefined, streamSource(2))
			mocks.streamHandlers!.state('open', undefined, streamSource(2, 'legacy-primary'))
			mocks.streamHandlers!.event(ledEvent(18), streamSource(2, 'legacy-primary'))
		})
		await waitFor(() => expect(renderedState()).toMatchObject({
			blue: 18, revision: 0, epoch: 2, instance: 'legacy-primary',
		}))
		await act(async () => {
			mocks.streamHandlers!.event(ledEvent(36), streamSource(2, 'legacy-primary'))
		})
		expect(renderedState()).toMatchObject({
			blue: 36, revision: 0, epoch: 2, instance: 'legacy-primary',
		})
	})

	it('discards an in-flight old-primary snapshot and stale close after a newer ACK', async () => {
		const primaryA = ledSnapshot('primary-a', 145, 100)
		await mountWithStartup(primaryA)
		const delayedA = deferred<Snapshot>()
		mocks.getSnapshot.mockImplementationOnce(() => delayedA.promise)

		fireEvent.click(screen.getByTestId('refresh'))
		await waitFor(() => expect(mocks.getSnapshot).toHaveBeenCalledTimes(2))
		await act(async () => {
			mocks.streamHandlers!.state('connecting', undefined, streamSource(2))
			mocks.streamHandlers!.state('open', undefined, streamSource(2, 'primary-b'))
			delayedA.resolve(primaryA)
		})
		await waitFor(() => expect(renderedState()).toMatchObject({
			connected: false, have: false, stream: 'open',
		}))

		await act(async () => {
			mocks.streamHandlers!.event(ledEvent(0, 1, 255), streamSource(2, 'primary-b'))
			mocks.streamHandlers!.status({ time: '2026-08-12T00:00:01Z', status: emptySnapshot.status })
			mocks.streamHandlers!.state('closed', 'old socket closed', streamSource(1, 'primary-a'))
		})
		await waitFor(() => expect(renderedState()).toMatchObject({
			connected: true, have: true, blue: 0, revision: 1,
			epoch: 2, instance: 'primary-b', stream: 'open',
		}))
	})
})
