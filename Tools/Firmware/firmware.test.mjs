import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import {
        BOARD,
        artifactRole,
        assertProgrammingImage,
	createBuildPlan,
	createProgramPlan,
	EXIT,
	main,
	packBuildTimestamp,
	parseArguments,
	parseIntelHex,
	parseToolchainPolicy,
	runValidatedProgrammingStages,
	sourceDigest
} from './firmware.mjs'

test('safe default EEPROM has a distinct bounded manifest role', () => {
        assert.equal(artifactRole('safe-default-eeprom.hex'), 'default-eeprom')
        assert.equal(artifactRole('PCController.ino.eep'), 'eeprom')
        assert.equal(artifactRole('PCController.ino.hex'), 'application')
})

test('build timestamp uses the exact compact date and time bit layout', () => {
	const value = new Date(2026, 7, 1, 19, 42, 58)
	assert.equal(packBuildTimestamp(value), 0x35019D5D)
})

test('build is the safe default and never implies upload', () => {
	const config = parseArguments([], {})
	assert.equal(config.command, 'build')
	assert.equal(config.uploadOnChange, false)
	assert.equal(config.port, '')
})

test('every UART action requires an explicit port', () => {
	for (const command of ['upload', 'backup', 'verify', 'probe', 'metadata']) {
		assert.throws(
			() => parseArguments([command, ...(command === 'backup' ? ['--output', 'x.hex'] : [])], {}),
			error => error.exitCode === EXIT.USAGE && /requires --port/.test(error.message)
		)
	}
})

test('watch upload is explicit and dry-run becomes one shot', () => {
	const config = parseArguments([
		'watch', '--upload', '--port', 'TEST_PORT', '--dry-run'
	], {})
	assert.equal(config.uploadOnChange, true)
	assert.equal(config.once, true)
	assert.equal(config.port, 'TEST_PORT')
	assert.equal(config.method, 'urclock')
})

test('USBasp upload uses the canonical method and does not require redundant flags', () => {
	const config = parseArguments(['upload', '--method', 'usbasp'], {})
	assert.equal(config.method, 'usbasp')
	assert.equal(config.port, '')
	assert.equal(config.programmer, '')
	const alternate = parseArguments([
		'upload', '--method', 'usbasp', '--programmer', 'atmelice_isp'
	], {})
	assert.equal(alternate.programmer, 'atmelice_isp')
})

test('Urclock upload requires a serial port', () => {
	assert.throws(
		() => parseArguments(['upload', '--method', 'urclock'], {}),
		error => error.exitCode === EXIT.USAGE && /requires --port/.test(error.message)
	)
})

test('direct Arduino upload is rejected', () => {
	assert.throws(
		() => parseArguments(['upload', '--method', 'arduino', '--port', 'DO_NOT_OPEN'], {}),
		error => error.exitCode === EXIT.USAGE && /direct Arduino upload is disabled/.test(error.message)
	)
})

test('invalid artifacts gate every programmer subprocess', async () => {
	for (const method of ['urclock', 'usbasp']) {
		const subprocesses = []
		await assert.rejects(
			runValidatedProgrammingStages({
				build: async () => subprocesses.push('build subprocess'),
				validate: async () => {
					throw new Error(`${method} invalid Intel HEX`)
				},
				program: async () => subprocesses.push(
					`${method} programmer subprocess`
				)
			}),
			new RegExp(`${method} invalid Intel HEX`)
		)
		assert.deepEqual(
			subprocesses,
			['build subprocess'],
			`${method} programmer subprocess crossed a failed validation gate`
		)
	}
})

test('valid programming stages retain strict build-validate-program order', async () => {
	const order = []
	await runValidatedProgrammingStages({
		build: async () => order.push('build'),
		validate: async () => {
			order.push('validate')
			return { integrity: 'passed' }
		},
		program: async result => {
			assert.equal(result.integrity, 'passed')
			order.push('program')
		}
	})
	assert.deepEqual(order, ['build', 'validate', 'program'])
})

test('all upload methods use a hardware-free canonical build phase', async () => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-build-plan-'))
	for (const method of ['urclock', 'usbasp']) {
		const args = ['upload', '--method', method]
		if (method === 'urclock') args.push('--port', 'DO_NOT_OPEN')
		const plan = await createBuildPlan(parseArguments(args, {}), root)
		const command = plan.args.join(' ')
		assert.match(plan.env.PCCONTROLLER_BUILD_TIMESTAMP, /^0x[0-9A-F]{8}$/)
		assert.equal(plan.file, process.execPath)
		assert.match(command, /Tools.*Build.*build\.mjs.*--firmware-only.*--build-timestamp/)
		assert.doesNotMatch(command, /PowerShell|build\.ps1|--upload/i)
	}
})

test('program plans select USBasp by method and keep programmer as an override', async () => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-program-plan-'))
	const bin = join(root, 'Tools', 'Controller', 'bin')
	await mkdir(bin, { recursive: true })
	const controller = join(bin, process.platform === 'win32' ? 'controller.exe' : 'controller')
	await writeFile(controller, '')
	const config = parseArguments(['upload', '--method', 'usbasp'], {})
	const plan = await createProgramPlan(config, root, join(root, 'firmware.hex'))
	assert.equal(plan.file, controller)
	assert.match(plan.args.join(' '), /--method usbasp/)
	assert.doesNotMatch(plan.args.join(' '), /--programmer|troubleshooting/)
	assert.doesNotMatch(plan.file, /\.build[\\/]host/)
	const custom = await createProgramPlan(
		{ ...config, port: 'DO_NOT_OPEN', programmer: 'atmelice_isp' }, root, join(root, 'firmware.hex')
	)
	assert.match(custom.args.join(' '), /--app-device DO_NOT_OPEN/)
	assert.match(custom.args.join(' '), /--programmer atmelice_isp/)
})

test('USBasp gate requires the complete merged Urboot region', () => {
	const incomplete = {
		role: 'flash+bootloader',
		ranges: [{ start: 0, end: BOARD.applicationLimitBytes - 1 }]
	}
	assert.throws(
		() => assertProgrammingImage([incomplete], 'usbasp'),
		/complete Urboot/
	)
	const complete = {
		role: 'flash+bootloader',
		ranges: [
			{ start: 0, end: BOARD.applicationLimitBytes - 1 },
			{ start: BOARD.applicationLimitBytes, end: BOARD.flashBytes - 1 }
		]
	}
	assert.equal(assertProgrammingImage([complete], 'usbasp'), complete)
})

test('Urclock requires a non-empty application, never EEPROM', () => {
	const eeprom = { role: 'eeprom', dataBytes: 10 }
	assert.throws(
		() => assertProgrammingImage([eeprom], 'urclock'),
		/non-empty canonical application/
	)
	const application = { role: 'application', dataBytes: 1 }
	assert.equal(
		assertProgrammingImage([eeprom, application], 'urclock'),
		application
	)
})

test('Intel HEX validation reports bytes and range', () => {
	const result = parseIntelHex([
		':020000040000FA',
		':0400100001020304E2',
		':00000001FF',
		''
	].join('\n'), 'fixture.hex')
	assert.equal(result.dataBytes, 4)
	assert.equal(result.startAddress, 0x10)
	assert.equal(result.endAddress, 0x13)
	assert.equal(result.records, 3)
})

test('Intel HEX validation rejects checksum errors', () => {
	assert.throws(
		() => parseIntelHex(':0400100001020304E3\n:00000001FF\n', 'bad.hex'),
		error => error.exitCode === EXIT.VALIDATION && /checksum/.test(error.message)
	)
})

test('source digest is content-based, stable, and covers domain folders', async () => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-source-'))
	await mkdir(join(root, 'Project'))
	await writeFile(join(root, 'PCController.ino'), 'void setup() {}\n')
	await writeFile(join(root, 'Project', 'Feature.cpp'), 'int feature = 1;\n')
	const first = await sourceDigest(root)
	const second = await sourceDigest(root)
	assert.equal(first.sha256, second.sha256)
	assert.equal(first.files, 2)
	assert.equal(first.sha256.length, 64)
})

test('board FQBN matches the canonical toolchain policy', async () => {
	const policy = JSON.parse(await readFile(
		new URL('../Controller/toolchain-profile.json', import.meta.url),
		'utf8'
	))
	assert.equal(BOARD.fqbn, policy.fqbn)
})

test('toolchain policy validation identifies a missing FQBN and its source', () => {
	assert.throws(
		() => parseToolchainPolicy(JSON.stringify({
			format: 'pccontroller-toolchain-policy/v1',
			fqbn: '   '
		}), 'invalid-policy.json'),
		error => /invalid-policy\.json/.test(error.message) &&
			/non-empty fqbn/.test(error.message)
	)
})

test('board profile keeps Urboot and EEPROM-retention contract', () => {
        assert.match(BOARD.fqbn, /bootloader=uart0/)
        assert.match(BOARD.fqbn, /eeprom=keep/)
        assert.match(BOARD.fqbn, /BOD=2v7/)
        assert.equal(BOARD.applicationLimitBytes, 32_384)
        assert.equal(BOARD.eepromBytes, 1_024)
})

test('persistent Ready profile remains host-owned with a compact firmware fallback', async () => {
        const [hostProfiles, firmware] = await Promise.all([
                readFile(new URL('../Controller/internal/native/status_profiles.go', import.meta.url), 'utf8'),
                readFile(new URL('../../Project/StatusLedController.cpp', import.meta.url), 'utf8')
        ])
        assert.match(hostProfiles, /StatusConditionReady:\s+static\(255, 255, 255\)/u)
        assert.match(firmware, /Go tooling owns and provisions the full factory profile table/u)
        assert.doesNotMatch(firmware, /ReadyPalette/u)
})

test('physical, injected, and RF key actions retain the immediate dispatch contract', async () => {
	const [configuration, keys, frontPanel, protocol, radio] = await Promise.all([
		readFile(new URL('../../Project/Runtime/ControllerConfiguration.inc.h', import.meta.url), 'utf8'),
		readFile(new URL('../../LocalLib/Keys.h', import.meta.url), 'utf8'),
		readFile(new URL('../../Project/Runtime/FrontPanelRuntime.inc.h', import.meta.url), 'utf8'),
		readFile(new URL('../../Project/Runtime/ProtocolRuntime.inc.h', import.meta.url), 'utf8'),
		readFile(new URL('../../Project/Runtime/RadioRuntime.inc.h', import.meta.url), 'utf8')
	])
	assert.match(keys, /KEY_DEBOUNCE_MS = 20;/u)
	assert.match(configuration, /SHIFT_POLL_MS \+ KEY_DEBOUNCE_MS <=\s*\n?\s*KEY_PRIMARY_ACTION_BUDGET_MS/u)
	assert.match(
		keys,
		/return event == KeyEvent::Down \|\| event == KeyEvent::HoldRepeat;/u
	)
	assert.match(frontPanel, /keyEventRunsPrimaryAction\(event\)/u)
	assert.doesNotMatch(frontPanel, /event == KeyEvent::Click \|\| event == KeyEvent::HoldStart/u)
	assert.match(
		protocol,
		/case RemoteKeyGesture:[^]*?applyKeyGesture\(payload\[0\], static_cast<KeyEvent>\(payload\[1\]\)\);/u
	)
	assert.match(
		radio,
		/case RemoteActionKind::Key:[^]*?KeyEvent::Down[^]*?handleMenuAction\(remote\.actionValue, true\);/u
	)
})

test('production KEY dispatches first Down to motion and exits outside KEY', async () => {
	const frontPanel = await readFile(
		new URL('../../Project/Runtime/FrontPanelRuntime.inc.h', import.meta.url),
		'utf8'
	)
	assert.match(frontPanel, /const bool momentary = mode == MODE_KEYS \|\| mode == MODE_MOTION_CONTROL/u)
	assert.match(frontPanel, /if \(modeManager\.current\(\) == MODE_KEYS\)[^]*?relays\.allOff\(actionNow\);[^]*?modeManager\.transitionTo\(MODE_MOTION_CONTROL\);/u)
	assert.match(frontPanel, /mode == MODE_MOTION_CONTROL && event == KeyEvent::Down[^]*?shiftRegisters\.inputActive\(bit \^ 1U\)/u)
	assert.match(frontPanel, /setMenuPage\(PAGE_DOOR\);/u)
	assert.doesNotMatch(frontPanel, /case MODE_MOTION_CONTROL:\s*relays\.allOff\(at\);/u)
})

test('firmware runtime owns one shared ordinary-service clock snapshot', async () => {
        const runtimeFiles = [
                'ControllerContext.inc.h',
                'LifecycleRuntime.inc.h',
                'ProtocolRuntime.inc.h',
                'FrontPanelRuntime.inc.h'
        ]
        const sources = await Promise.all(runtimeFiles.map(file => readFile(
			new URL(`../../Project/Runtime/${file}`, import.meta.url),
                'utf8'
        )))
        const helperSources = await Promise.all([
                'ControllerUtilities.inc.h',
                'RadioRuntime.inc.h',
                'SensorRuntime.inc.h'
        ].map(file => readFile(
			new URL(`../../Project/Runtime/${file}`, import.meta.url),
                'utf8'
        )))
        assert.match(sources[0], /static uint32_t now = 0;/u)
        assert.doesNotMatch(sources.join('\n'), /const uint32_t now = millis\(\);/u)
        assert.doesNotMatch(sources.join('\n'), /::now = now;/u)
        assert.doesNotMatch(
                [...sources.slice(1), ...helperSources].join('\n'),
                /\buint32_t now\b/u
        )
        assert.equal(
                sources.join('\n').match(/\bnow = millis\(\);/gu)?.length,
                6
        )
        assert.match(
                sources[1],
                /serviceController\(\) \{\s*now = millis\(\);[^]*?const uint32_t loopNow = now;/u
        )
        assert.match(
                sources[2],
                /handleProtocolFrame[^]*?\{[^]*?now = millis\(\);\s*const uint32_t frameNow = now;/u
        )
        assert.match(
                sources[3],
                /handleMenuAction[^]*?\{[^]*?now = millis\(\);\s*const uint32_t actionNow = now;/u
        )
        assert.match(sources[3], /const uint32_t releaseNow = now;/u)
})

test('studio validation preserves matching Controller compile identity', async () => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-manifest-identity-'))
	const output = join(root, '.build', 'firmware')
	await mkdir(output, { recursive: true })
	await writeFile(join(root, 'PCController.ino'), 'void setup() {}\n')
	await writeFile(join(output, 'PCController.ino.hex'), [
		':020000040000FA',
		':0400100001020304E2',
		':00000001FF',
		''
	].join('\n'))
	await main(['manifest', '--quiet', '--no-color'], {}, root)
	const manifestPath = join(output, 'firmware-manifest.json')
	const controllerManifest = JSON.parse(await readFile(manifestPath, 'utf8'))
	controllerManifest.generatedUtc = '2026-08-01T16:12:58Z'
	controllerManifest.source.buildHash = '1234ABCD'
	controllerManifest.source.packedTimestamp = '35019D5D'
	controllerManifest.source.buildTimestamp = '2026-08-01 19:42:58'
	controllerManifest.stackBudget = { estimatedFreeSRAMBytes: 287 }
	controllerManifest.patchRegions = [{
		name: 'firmware-identity', start: 0x7E74, length: 12,
		schema: 1, magic: 'PCI1'
	}]
	await writeFile(manifestPath, `${JSON.stringify(controllerManifest, null, 2)}\n`)
	await main(['manifest', '--quiet', '--no-color'], {}, root)
	const validated = JSON.parse(await readFile(manifestPath, 'utf8'))
	assert.equal(validated.generatedUtc, '2026-08-01T16:12:58Z')
	assert.equal(validated.source.buildHash, '1234ABCD')
	assert.equal(validated.source.packedTimestamp, '35019D5D')
	assert.deepEqual(validated.stackBudget, { estimatedFreeSRAMBytes: 287 })
	assert.deepEqual(validated.patchRegions, controllerManifest.patchRegions)
})

test('one-shot watched dry-run is serialized and never starts a tool', async () => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-watch-'))
	await writeFile(join(root, 'PCController.ino'), 'void loop() {}\n')
	const result = await main([
		'watch',
		'--once',
		'--dry-run',
		'--quiet',
		'--no-color',
		'--poll',
		'50',
		'--debounce',
		'50'
	], {}, root)
	assert.equal(result, EXIT.OK)
})
