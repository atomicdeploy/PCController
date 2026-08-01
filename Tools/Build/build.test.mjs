import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import test from 'node:test'

import {
	BuildError,
	PROJECT_ROOT,
	assertGeneratedPath,
	createPlan,
	hostSourceIdentity,
	packBuildTimestamp,
	parseArguments,
	removeGeneratedWinResources,
	resolveBuildIdentity,
	windowsSmokeSource
} from './build.mjs'

test('safe default builds both targets without touching hardware', () => {
	const options = parseArguments([], {})
	assert.equal(options.host, true)
	assert.equal(options.firmware, true)
	assert.equal(options.upload, false)
	assert.equal(options.burnBootloader, false)
})

test('root wrappers contain no PowerShell policy or invocation', async () => {
	for (const name of ['build.cmd', 'build.sh']) {
		const source = await readFile(join(PROJECT_ROOT, name), 'utf8')
		assert.doesNotMatch(source, /powershell|pwsh|build\.ps1/i)
		assert.match(source, /Tools[\\/]Build[\\/]build\.mjs/)
	}
	const firmwareSource = await readFile(
		join(PROJECT_ROOT, 'Tools', 'Firmware', 'firmware.mjs'),
		'utf8'
	)
	assert.doesNotMatch(firmwareSource, /powershell|pwsh|build\.ps1|arduino-cli[^\n]*upload/i)
})

test('programming is explicit and unsupported direct Arduino upload is rejected', () => {
	assert.throws(
		() => parseArguments(['--method', 'arduino'], {}),
		error => error instanceof BuildError && /direct Arduino upload/.test(error.message)
	)
	assert.throws(
		() => parseArguments(['--upload'], {}),
		error => error instanceof BuildError && /requires --port/.test(error.message)
	)
	const serial = parseArguments(['--upload', '--port', 'DO_NOT_OPEN'], {})
	assert.equal(serial.method, 'urclock')
	assert.equal(serial.device, 'DO_NOT_OPEN')
})

test('USBasp and bootloader paths require the troubleshooting guard', () => {
	assert.throws(
		() => parseArguments(['--upload', '--method', 'usbasp'], {}),
		error => /USBasp is hidden troubleshooting only/.test(error.message)
	)
	assert.throws(
		() => parseArguments(['--burn-bootloader'], {}),
		error => /requires --usbasp-troubleshooting/.test(error.message)
	)
	const guarded = parseArguments(['--usbasp-flash'], {})
	assert.equal(guarded.upload, true)
	assert.equal(guarded.method, 'usbasp')
	assert.equal(guarded.usbaspTroubleshooting, true)
})

test('Arduino dependency update is explicit and owned by Controller', () => {
	assert.throws(
		() => parseArguments(['--arduino-cli', 'arduino-cli'], {}),
		error => /only valid with --arduino-update/.test(error.message)
	)
	const options = parseArguments([
		'--host-only', '--arduino-update', '--arduino-cli', 'CUSTOM_CLI',
		'--build-time', '2026-08-01T16:12:58Z', '--build-timestamp', '35019D5D'
	], {})
	const plan = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
	const update = plan.actions.find(action => action.id === 'arduino-update')
	assert.ok(update)
	assert.equal(update.externalMutation, true)
	assert.match(update.command.args.join(' '), /arduino update --arduino-cli CUSTOM_CLI/)
	assert.doesNotMatch(JSON.stringify(update), /arduino-cli.*core.*update-index/i)
})

test('fixed identity is propagated exactly to host and firmware', () => {
	assert.equal(packBuildTimestamp(new Date(2026, 7, 1, 19, 42, 58)), 0x35019D5D)
	const options = parseArguments([
		'--version', '0.5.0-test',
		'--build-time', '2026-08-01T16:12:58Z',
		'--build-timestamp', '0x35019D5D'
	], {})
	const identity = resolveBuildIdentity(options, {})
	assert.equal(identity.version, '0.5.0-test')
	assert.equal(identity.hostBuildTime, '2026-08-01T16:12:58Z')
	assert.equal(identity.packedTimestamp, '35019D5D')
	assert.equal(identity.env.PCCONTROLLER_BUILD_TIMESTAMP, '0x35019D5D')
})

test('Windows resources are patched after link and before UPX', () => {
	const options = parseArguments([
		'--host-only', '--build-time', '2026-08-01T16:12:58Z',
		'--build-timestamp', '35019D5D'
	], {})
	const plan = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
	const buildIndex = plan.actions.findIndex(action => action.id === 'host-build')
	const resourceIndex = plan.actions.findIndex(action => action.id === 'winres')
	const upxIndex = plan.actions.findIndex(action => action.id === 'upx-pack')
	assert.ok(buildIndex >= 0 && buildIndex < resourceIndex)
	assert.ok(resourceIndex < upxIndex)
	assert.deepEqual(plan.actions[resourceIndex].command.args, [
		'patch', '--in', 'winres/winres.json', '--delete', '--no-backup',
		'<staging>/controller.exe'
	])
	assert.doesNotMatch(JSON.stringify(plan.actions), /go-winres[^}]*make|\.syso/i)
})

test('Win32 resource configuration retains icon, manifest, and version data', async () => {
	const source = await readFile(
		join(PROJECT_ROOT, 'Tools', 'Controller', 'winres', 'winres.json'),
		'utf8'
	)
	const resources = JSON.parse(source)
	assert.equal(resources.RT_GROUP_ICON.APP['0000'], 'icon.png')
	assert.ok(resources.RT_MANIFEST['#1']['0409'])
	assert.equal(
		resources.RT_VERSION['#1']['0409'].info['0409'].OriginalFilename,
		'controller.exe'
	)
})

test('default packed timestamp accepts the numeric clock result', () => {
	const identity = resolveBuildIdentity(
		parseArguments(['--firmware-only'], {}),
		{},
		new Date(2026, 7, 1, 19, 42, 58)
	)
	assert.equal(identity.packedTimestamp, '35019D5D')
})

test('shared plan routes compile and programming through Controller only', () => {
	const options = parseArguments([
		'--upload', '--port', 'DO_NOT_OPEN', '--dry-run',
		'--build-time', '2026-08-01T16:12:58Z',
		'--build-timestamp', '35019D5D'
	], {})
	const plan = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
	const text = JSON.stringify(plan)
	assert.match(text, /program.*method.*compile.*sketch.*output-dir/)
	assert.doesNotMatch(text, /powershell|build\.ps1|arduino-cli.*upload/i)
	const hardware = plan.actions.filter(action => action.hardware)
	assert.deepEqual(hardware.map(action => action.id), ['program'])
	assert.match(hardware[0].command.args.join(' '), /--method urclock/)
	assert.match(hardware[0].command.args.join(' '), /--device DO_NOT_OPEN/)
})

test('firmware-only plan uses current Controller source and remains hardware-free', () => {
	const options = parseArguments([
		'--firmware-only', '--build-time', '2026-08-01T16:12:58Z',
		'--build-timestamp', '35019D5D'
	], {})
	const plan = createPlan(options, resolveBuildIdentity(options, {}), process.platform)
	assert.deepEqual(plan.actions.map(action => action.id), ['firmware-compile'])
	assert.equal(plan.actions[0].command.file, 'go')
	assert.match(plan.actions[0].command.args.join(' '), /run.*cmd\/controller.*program.*compile/)
	assert.equal(plan.actions[0].hardware, false)
})

test('generated path guard refuses the project root and outside paths', () => {
	assert.throws(() => assertGeneratedPath(PROJECT_ROOT, PROJECT_ROOT), /refusing/)
	assert.throws(
		() => assertGeneratedPath(PROJECT_ROOT, resolve(PROJECT_ROOT, '..', 'outside')),
		/refusing/
	)
	assert.equal(
		assertGeneratedPath(PROJECT_ROOT, join(PROJECT_ROOT, '.build', 'fixture')),
		resolve(PROJECT_ROOT, '.build', 'fixture')
	)
})

test('clean plan includes canonical and audited legacy host outputs', () => {
	const options = parseArguments(['--clean'], {})
	const plan = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
	assert.equal(plan.actions.length, 1)
	const paths = plan.actions[0].paths.map(path => path.replaceAll('\\', '/'))
	assert.ok(paths.some(path => path.endsWith('/Tools/Controller/bin')))
	assert.ok(paths.some(path => path.endsWith('/Tools/Controller/controller.exe')))
	assert.ok(paths.some(path => path.endsWith('/Tools/Controller/.cache/identity-build')))
})

test('host source identity is content-stable and changes with source bytes', async () => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-host-identity-'))
	await mkdir(join(root, 'nested'))
	await writeFile(join(root, 'B.go'), 'package fixture\nconst B = 2\n')
	await writeFile(join(root, 'nested', 'a.go'), 'package nested\nconst A = 1\n')
	await writeFile(join(root, 'go.mod'), 'module example.invalid/fixture\n')
	const first = hostSourceIdentity(root)
	const second = hostSourceIdentity(root)
	assert.equal(first.sha256, second.sha256)
	assert.equal(first.files, 3)
	assert.match(first.manifest, /^B\.go:/)
	await writeFile(join(root, 'rsrc_windows_amd64.syso'), 'generated resource bytes')
	assert.deepEqual(hostSourceIdentity(root), first)
	await writeFile(join(root, 'nested', 'a.go'), 'package nested\nconst A = 3\n')
	assert.notEqual(hostSourceIdentity(root).sha256, first.sha256)
})

test('stale generated Win32 resource objects are removed from package source', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-winres-clean-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	await writeFile(join(root, 'rsrc_windows_amd64.syso'), 'stale')
	await writeFile(join(root, 'keep.syso'), 'unrelated')
	removeGeneratedWinResources(root)
	assert.deepEqual(await readdir(root), ['keep.syso'])
})

test('Windows C ABI smoke uses a valid handle and destroys it', () => {
	const source = windowsSmokeSource()
	assert.ok(source.includes('{\\"operation\\":\\"create\\"}'))
	assert.match(source, /strtoull\(handle_field/)
	assert.ok(source.includes('{\\"operation\\":\\"build-smoke-invalid\\",\\"handle\\":%llu}'))
	assert.ok(source.includes('{\\"operation\\":\\"destroy\\",\\"handle\\":%llu}'))
	assert.doesNotMatch(source, /FreeLibrary\(module\)/)
})

test('CMD and Bash wrappers emit the same shared plan on Windows', {
	skip: process.platform !== 'win32'
}, () => {
	const env = {
		...process.env,
		PCCONTROLLER_VERSION: 'wrapper-test',
		PCCONTROLLER_HOST_BUILD_TIME: '2026-08-01T16:12:58Z',
		PCCONTROLLER_BUILD_TIMESTAMP: '0x35019D5D',
		NO_COLOR: '1'
	}
	const cmd = spawnSync('cmd.exe', [
		'/d', '/s', '/c', 'build.cmd --firmware-only --plan-json'
	], { cwd: PROJECT_ROOT, env, encoding: 'utf8', windowsHide: true })
	assert.equal(cmd.status, 0, cmd.stderr || cmd.stdout)
	const bash = spawnSync('bash.exe', [
		'build.sh', '--firmware-only', '--plan-json'
	], { cwd: PROJECT_ROOT, env, encoding: 'utf8', windowsHide: true })
	assert.equal(bash.status, 0, bash.stderr || bash.stdout)
	assert.deepEqual(JSON.parse(cmd.stdout), JSON.parse(bash.stdout))
})
