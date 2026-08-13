import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { renameSync } from 'node:fs'
import { chmod, mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { delimiter, join, resolve, sep } from 'node:path'
import test from 'node:test'

import {
	BuildError,
	PROJECT_ROOT,
	assertGeneratedPath,
	collectWebNotices,
	compilerManifestIdentity,
        createPlan,
        generatedCleanTargets,
        hostSourceIdentity,
	installPackage,
	isNativeWindowsGNUCompiler,
	packBuildTimestamp,
	parseArguments,
	pruneAbandonedPackageStages,
	refreshedEnvironment,
	renderTable,
	removeGeneratedWinResources,
	retirePreviousPackage,
	resolveBuildIdentity,
	resolveProductTitle,
	selectCCompiler,
	stalePackageTransactionPaths,
	unixSmokeSource,
	verboseCommandText,
	windowsCompilerProvisionArguments,
	windowsSmokeSource
} from './build.mjs'
import { createStableTestPlan, goTestSourceIdentity, stableTestBinaryName } from './go-tests.mjs'
import { PRODUCT_METADATA } from './product-metadata.mjs'
import {
	BOARD,
	PROGRAMMING_OPERATIONS,
	canonicalControllerInvocation,
	commandPlanPaths,
	createControllerProgramCommand,
	parseToolchainPolicy,
	programmingArtifact,
	relativeCommandPlanPaths,
	resolveCanonicalControllerInvocation,
	sourceControllerInvocation
} from '../CommandPlan/controller-command.mjs'

test('verbose command formatting obeys NO_COLOR byte-for-byte', () => {
	const plain = verboseCommandText('go', ['test', './...'], { NO_COLOR: '' }, true)
	assert.equal(plain, '$ go test ./...')
	assert.equal(Buffer.from(plain).includes(0x1b), false)
	assert.match(verboseCommandText('go', ['test', './...'], {}, true), /^\x1b\[90m/)
	assert.doesNotMatch(verboseCommandText('go', ['test', './...'], {}, false), /\x1b/)
	assert.match(verboseCommandText('go', ['test', './...'], { FORCE_COLOR: '' }, false), /^\x1b\[90m/)
})

test('visible build product title comes from metadata or app configuration', () => {
	assert.equal(resolveProductTitle({}, { productName: 'Configured Controller' }), 'Configured Controller')
	assert.equal(
		resolveProductTitle({ APP_TITLE: 'Workshop Console' }, { productName: 'Configured Controller' }),
		'Workshop Console'
	)
	assert.equal(resolveProductTitle({}, { name: '@vendor/device-tool' }), 'device tool')
})

test('empty NO_COLOR disables every entrypoint style even when output is captured', () => {
	const environment = { ...process.env }
	for (const key of Object.keys(environment)) {
		if (key.toLowerCase() === 'no_color' || key.toLowerCase() === 'force_color') delete environment[key]
	}
	environment.NO_COLOR = ''
	assert.equal(parseArguments([], environment).noColor, true)
	const result = spawnSync(process.execPath, [
		join(PROJECT_ROOT, 'Tools', 'Build', 'build.mjs'),
		'--firmware-only', '--dry-run', '--verbose'
	], { cwd: PROJECT_ROOT, env: environment, windowsHide: true })
	assert.equal(result.status, 0, result.stderr?.toString() || result.stdout?.toString())
	assert.equal(Buffer.concat([result.stdout, result.stderr]).includes(0x1b), false)
})

test('refreshed Windows PATH preserves invoking shell precedence and one canonical key', () => {
	const selected = resolve('selected-latest-toolchain')
	const session = resolve('session-tools')
	const windowsEnvironment = { ...process.env }
	for (const key of Object.keys(windowsEnvironment)) {
		if (key.toLowerCase() === 'path') delete windowsEnvironment[key]
	}
	windowsEnvironment.Path = [selected, session].join(delimiter)
	windowsEnvironment.LOCALAPPDATA = resolve('local-app-data')
	const refreshed = refreshedEnvironment(windowsEnvironment, 'win32')
	assert.deepEqual(refreshed.PATH.split(delimiter).slice(0, 2), [selected, session])
	assert.equal(
		refreshed.PATH.split(delimiter)[2],
		join(windowsEnvironment.LOCALAPPDATA, PRODUCT_METADATA.productConfigDirectory, 'tools', 'go', 'bin')
	)
	assert.deepEqual(Object.keys(refreshed).filter(key => key.toLowerCase() === 'path'), ['PATH'])
})

test('Windows C compiler validation rejects MSYS/Cygwin even when gcc is on PATH', () => {
	assert.equal(
		isNativeWindowsGNUCompiler('x86_64-w64-mingw32', '#define __MINGW32__ 1\n#define __MINGW64__ 1\n', 'amd64'),
		true
	)
	assert.equal(
		isNativeWindowsGNUCompiler('x86_64-pc-msys', '#define __MSYS__ 1\n', 'amd64'),
		false
	)
	assert.equal(
		isNativeWindowsGNUCompiler('x86_64-w64-mingw32', '#define __CYGWIN__ 1\n#define __MINGW32__ 1\n', 'amd64'),
		false
	)
	assert.equal(
		isNativeWindowsGNUCompiler('i686-w64-mingw32', '#define __MINGW32__ 1\n', 'amd64'),
		false
	)
})

test('Windows C compiler bootstrap requests resolved user-scoped package and forwards proxy', () => {
	const args = windowsCompilerProvisionArguments(
		{ https_proxy: 'http://proxy.invalid:8080' }, 'amd64', '16.1.0-14.0.0-r3'
	)
	assert.deepEqual(args.slice(0, 7), [
		'install', '--id', 'BrechtSanders.WinLibs.POSIX.UCRT', '--exact', '--source', 'winget', '--scope'
	])
	assert.match(args.join(' '), /--scope user --architecture x64/)
	assert.match(args.join(' '), /--proxy http:\/\/proxy\.invalid:8080/)
	assert.match(args.join(' '), /--version 16\.1\.0-14\.0\.0-r3/)
})

test('current Windows resolver selects a native compiler without accepting the first PATH gcc', {
	skip: process.platform !== 'win32'
}, () => {
	const environment = { ...process.env }
	for (const key of Object.keys(environment)) if (key.toLowerCase() === 'cc') delete environment[key]
	const selected = selectCCompiler(environment, 'amd64', { compilerBootstrap: false })
	assert.match(selected.target, /^x86_64-.*(?:mingw|windows-gnu)/i)
	assert.match(selected.version, /^\d+(?:\.\d+)+$/)
	assert.ok(selected.command.toLowerCase().endsWith(selected.env.CC.toLowerCase()))
})

test('host manifest compiler identity is locked and carries local binary integrity', () => {
	const locked = {
		package_id: 'BrechtSanders.WinLibs.POSIX.UCRT',
		package_version: '16.1.0-14.0.0-r3',
		compiler_version: '16.1.0',
		target: 'x86_64-w64-mingw32',
		installer_sha256: 'a'.repeat(64)
	}
	assert.deepEqual(compilerManifestIdentity({
		command: 'C:\\toolchain\\gcc.exe', version: '16.1.0', target: 'x86_64-w64-mingw32'
	}, locked, 'b'.repeat(64)), {
		...locked, binary_name: 'gcc.exe', binary_sha256: 'b'.repeat(64)
	})
	assert.throws(() => compilerManifestIdentity({
		command: 'gcc.exe', version: '15.1.0', target: 'x86_64-w64-mingw32'
	}, locked, 'b'.repeat(64)), /does not match the resolved host-tool lock/)
})

test('build tables center headers and align numeric values', () => {
	assert.equal(renderTable([
		{ label: 'Name' },
		{ label: 'Bytes', align: 'right' }
	], [
		['firmware', '32240'],
		['EEPROM', '0']
	]), [
		'╭──────────┬───────╮',
		'│   Name   │ Bytes │',
		'├──────────┼───────┤',
		'│ firmware │ 32240 │',
		'│ EEPROM   │     0 │',
		'╰──────────┴───────╯'
	].join('\n'))
})

test('Go test executables use deterministic project-owned names', () => {
	const importPath = 'github.com/atomicdeploy/pccontroller/internal/ipc'
	assert.equal(
		stableTestBinaryName(importPath, 'win32'),
		stableTestBinaryName(importPath, 'win32')
	)
	assert.match(stableTestBinaryName(importPath, 'win32'), /^github\.com_atomicdeploy_pccontroller_internal_ipc-[0-9a-f]{10}\.test\.exe$/)
	assert.doesNotMatch(stableTestBinaryName(importPath, 'win32'), /AppData|Temp/i)
})

test('stable Go test plan keeps every binary below its persistent output root', () => {
	const output = join(PROJECT_ROOT, '.build', 'tests', 'go')
	const plan = createStableTestPlan([
		{ importPath: 'example.invalid/controller/ipc', directory: join(PROJECT_ROOT, 'ipc') },
		{ importPath: 'example.invalid/controller/protocol', directory: join(PROJECT_ROOT, 'protocol') }
	], output, 'win32')
	assert.equal(plan.length, 2)
	assert.ok(plan.every(item => item.binary.startsWith(`${output}${sep}`)))
	assert.equal(new Set(plan.map(item => item.binary)).size, plan.length)
})

test('stable Go test identity includes embedded web assets', async t => {
	const root = await mkdtemp(join(tmpdir(), 'controller-go-test-identity-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	const dist = join(root, 'internal', 'webui', 'dist')
	await mkdir(dist, { recursive: true })
	await writeFile(join(root, 'go.mod'), 'module example.invalid/controller\n\ngo 1.26\n')
	await writeFile(join(root, 'main_test.go'), 'package controller\n')
	await writeFile(join(dist, 'index.html'), '<title>before</title>')
	const before = goTestSourceIdentity(root, 'go version go1.26.5 windows/amd64')
	await writeFile(join(dist, 'index.html'), '<title>after</title>')
	const after = goTestSourceIdentity(root, 'go version go1.26.5 windows/amd64')
	assert.notEqual(after.sha256, before.sha256)
	assert.equal(after.files, before.files)
})

test('package publishing tolerates a shell holding the canonical directory', async t => {
        const root = await mkdtemp(join(tmpdir(), 'pccontroller-package-lock-'))
        t.after(() => rm(root, { recursive: true, force: true }))
        const canonical = join(root, 'bin')
        const stage = join(root, 'stage')
        await mkdir(join(canonical, 'licenses'), { recursive: true })
        await mkdir(join(stage, 'licenses'), { recursive: true })
        await writeFile(join(canonical, 'controller.exe'), 'old host')
        const stale = join(canonical, 'stale.txt')
        await writeFile(stale, 'remove me')
        await chmod(stale, 0o444)
        await writeFile(join(stage, 'controller.exe'), 'new host')
        await writeFile(join(stage, 'licenses', 'NOTICE.txt'), 'current notice')

        let simulatedDirectoryLock = true
        installPackage(stage, {
                root,
                canonical,
                rename(source, target) {
                        if (simulatedDirectoryLock && source === canonical) {
                                simulatedDirectoryLock = false
                                const error = new Error('directory is a process working directory')
                                error.code = 'EBUSY'
                                throw error
                        }
                        renameSync(source, target)
                }
        })

        assert.equal(simulatedDirectoryLock, false)
        assert.equal(await readFile(join(canonical, 'controller.exe'), 'utf8'), 'new host')
        assert.equal(await readFile(join(canonical, 'licenses', 'NOTICE.txt'), 'utf8'), 'current notice')
        assert.deepEqual(await readdir(root), ['bin'])
        assert.deepEqual(await readdir(canonical), ['controller.exe', 'licenses'])
})

test('package publishing tolerates a transient lock on the completed stage directory', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-package-stage-lock-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	const canonical = join(root, 'bin')
	const stage = join(root, 'stage')
	await mkdir(join(stage, 'licenses'), { recursive: true })
	await writeFile(join(stage, 'controller.exe'), 'new host')
	await writeFile(join(stage, 'licenses', 'NOTICE.txt'), 'current notice')

	let simulatedStageLock = true
	installPackage(stage, {
		root,
		canonical,
		rename(source, target) {
			if (simulatedStageLock && source === stage && target === canonical) {
				simulatedStageLock = false
				const error = new Error('recent child still holds the stage directory')
				error.code = 'EPERM'
				throw error
			}
			renameSync(source, target)
		}
	})

	assert.equal(simulatedStageLock, false)
	assert.deepEqual(await readdir(root), ['bin'])
	assert.equal(await readFile(join(canonical, 'controller.exe'), 'utf8'), 'new host')
	assert.equal(await readFile(join(canonical, 'licenses', 'NOTICE.txt'), 'utf8'), 'current notice')
})

test('stale package cleanup recognizes only exact rollback transaction directories', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-package-transactions-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	await mkdir(join(root, '.bin-previous-123'))
	await mkdir(join(root, '.bin-previous-current'))
	await mkdir(join(root, '.bin-unrelated-456'))

	assert.deepEqual(stalePackageTransactionPaths(root), [join(root, '.bin-previous-123')])
})

test('package-stage pruning removes only dead incomplete stages', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-package-stages-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	const abandoned = join(root, 'host-111')
	await mkdir(abandoned)
	await writeFile(join(abandoned, 'controller.exe'), 'abandoned')
	const messages = []

	const result = pruneAbandonedPackageStages({
		success: message => messages.push(message),
		warning: message => messages.push(message)
	}, {
		packageRoot: root,
		currentPID: 999,
		isPIDAlive: () => false
	})

	assert.deepEqual(result.removed, [abandoned])
	assert.deepEqual(result.deferred, [])
	assert.deepEqual(await readdir(root), [])
	assert.match(messages[0], /Removed abandoned package stage/)
})

test('package-stage pruning preserves the current, live, and completed stages', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-package-preserve-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	const current = join(root, 'host-222')
	const live = join(root, 'host-333')
	const completed = join(root, 'host-444')
	await mkdir(current)
	await mkdir(live)
	await mkdir(completed)
	await writeFile(join(completed, 'host-manifest.json'), '{}')

	const result = pruneAbandonedPackageStages(null, {
		packageRoot: root,
		currentPID: 222,
		isPIDAlive: pid => pid === 333
	})

	assert.deepEqual(result, { removed: [], deferred: [] })
	assert.deepEqual(await readdir(root), ['host-222', 'host-333', 'host-444'])
})

test('package-stage pruning ignores foreign and nested directories', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-package-safety-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	await mkdir(join(root, 'host-current'))
	await mkdir(join(root, 'host-0'))
	await mkdir(join(root, 'unrelated-555'))
	await mkdir(join(root, 'nested', 'host-666'), { recursive: true })

	const result = pruneAbandonedPackageStages(null, {
		packageRoot: root,
		currentPID: 999,
		isPIDAlive: () => false
	})

	assert.deepEqual(result, { removed: [], deferred: [] })
	assert.deepEqual(await readdir(root), ['host-0', 'host-current', 'nested', 'unrelated-555'])
	assert.deepEqual(await readdir(join(root, 'nested')), ['host-666'])
})

test('package-stage pruning defers locked abandoned stages', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-package-locked-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	const locked = join(root, 'host-777')
	await mkdir(locked)
	const warnings = []
	const lockError = new Error('stage is still locked')
	lockError.code = 'EPERM'

	const result = pruneAbandonedPackageStages({
		success() {},
		warning: message => warnings.push(message)
	}, {
		packageRoot: root,
		currentPID: 999,
		isPIDAlive: () => false,
		remove() { throw lockError }
	})

	assert.deepEqual(result.removed, [])
	assert.deepEqual(result.deferred, [locked])
	assert.deepEqual(await readdir(root), ['host-777'])
	assert.match(warnings[0], /Deferred locked package-stage cleanup/)
})

test('published packages tolerate a locked retired Windows directory', () => {
	const locked = new Error('executable is still mapped')
	locked.code = 'EPERM'
	assert.equal(retirePreviousPackage('ignored', () => { throw locked }), false)

	const unexpected = new Error('invalid target')
	unexpected.code = 'EINVAL'
	assert.throws(
		() => retirePreviousPackage('ignored', () => { throw unexpected }),
		(error) => error === unexpected,
	)
})

test('safe default builds both targets without touching hardware', () => {
	const options = parseArguments([], {})
	assert.equal(options.host, true)
	assert.equal(options.firmware, true)
	assert.equal(options.upload, false)
	assert.equal(options.installBootloader, false)
})

test('host plan reproducibly builds the web application before Go embedding', () => {
	const options = parseArguments([
		'--host-only', '--build-time', '2026-08-01T16:12:58Z',
		'--build-timestamp', '35019D5D'
	], {})
	const plan = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
        assert.equal(plan.actions[0].id, 'embedded-defaults')
	assert.deepEqual(plan.actions.slice(1, 8).map(action => action.id), [
		'web-install', 'web-typecheck', 'web-test', 'web-build',
		'toolchain-policy-check', 'product-identity-check', 'go-mod-download'
	])
        assert.deepEqual(plan.actions[1].command.args, ['ci', '--no-audit', '--no-fund'])
        assert.equal(plan.actions[1].command.cwd, join(PROJECT_ROOT, 'Tools', 'Controller', 'web'))
	assert.ok(plan.actions.findIndex(action => action.id === 'web-build') <
		plan.actions.findIndex(action => action.id === 'go-test'))
	assert.deepEqual(plan.actions.find(action => action.id === 'toolchain-policy-check').command.args, [
		'Tools/Controller/internal/programmer/generate-toolchain-policy.mjs', '--check'
	])
	assert.deepEqual(plan.actions.find(action => action.id === 'product-identity-check').command.args, [
		'Tools/Controller/internal/productidentity/generate.mjs', '--check'
	])

	const skipped = createPlan(
		parseArguments(['--host-only', '--skip-tests'], {}),
		resolveBuildIdentity(parseArguments(['--host-only', '--skip-tests'], {}), {}),
		'win32'
	)
	assert.ok(!skipped.actions.some(action => action.id === 'web-test'))
	assert.ok(skipped.actions.some(action => action.id === 'web-typecheck'))
})

test('generated runtime toolchain policy is current with the canonical profile', () => {
	const result = spawnSync(process.execPath, [
		join(PROJECT_ROOT, 'Tools', 'Controller', 'internal', 'programmer', 'generate-toolchain-policy.mjs'),
		'--check'
	], { cwd: PROJECT_ROOT, windowsHide: true, encoding: 'utf8' })
	assert.equal(result.status, 0, `${result.stdout}${result.stderr}`)
})

test('one canonical policy owns FQBN, board geometry, and artifact routes', async () => {
	const policyPath = join(PROJECT_ROOT, 'Tools', 'Controller', 'toolchain-profile.json')
	const policy = parseToolchainPolicy(await readFile(policyPath, 'utf8'), policyPath)
	assert.equal(BOARD.profile, policy.name)
	assert.equal(BOARD.fqbn, policy.fqbn)
	assert.equal(BOARD.mcu, policy.target.mcu)
	assert.equal(BOARD.applicationLimitBytes, policy.target.applicationLimitBytes)
	assert.equal(BOARD.flashBytes, policy.target.flashBytes)
	assert.equal(BOARD.eepromBytes, policy.target.eepromBytes)

	const absolute = commandPlanPaths(PROJECT_ROOT, 'win32')
	const portable = relativeCommandPlanPaths(PROJECT_ROOT, 'win32')
	assert.equal(portable.controller, 'Tools/Controller/bin/controller.exe')
	assert.equal(portable.application, '.build/firmware/PCController.ino.hex')
	assert.equal(portable.completeFlash, '.build/firmware/PCController.ino.with_bootloader.hex')
	assert.equal(programmingArtifact(absolute, 'urclock'), absolute.application)
	assert.equal(programmingArtifact(absolute, 'usbasp'), absolute.completeFlash)
})

test('controller resolution accepts only the canonical platform artifact', () => {
	const inspected = []
	assert.throws(
		() => resolveCanonicalControllerInvocation(PROJECT_ROOT, 'win32', path => {
			inspected.push(path)
			const error = new Error('not found')
			error.code = 'ENOENT'
			throw error
		}),
		error => error.exitCode === 4 && /Tools[\\/]Controller[\\/]bin[\\/]controller\.exe/.test(error.message)
	)
	assert.deepEqual(inspected, [commandPlanPaths(PROJECT_ROOT, 'win32').controller])
})

test('build plan and execution share exact Controller programming argv construction', () => {
	const source = sourceControllerInvocation(PROJECT_ROOT)
	const compile = createControllerProgramCommand({
		invocation: source,
		method: 'compile',
		sketch: PROJECT_ROOT,
		outputDir: commandPlanPaths(PROJECT_ROOT).firmwareOutput,
		toolchainCLI: 'C:\\portable\\arduino-cli.exe',
		toolchainConfig: 'C:\\portable\\firmware-cli.yaml'
	})
	assert.deepEqual(compile.args.slice(0, 7), [
		'run', '-buildvcs=false', './cmd/controller', 'program', '--method', 'compile', '--sketch'
	])
	assert.equal(
		compile.args[compile.args.indexOf('--output-dir') + 1],
		commandPlanPaths(PROJECT_ROOT).firmwareOutput
	)
	assert.deepEqual(compile.args.slice(-4), [
		'--toolchain-cli', 'C:\\portable\\arduino-cli.exe',
		'--toolchain-config', 'C:\\portable\\firmware-cli.yaml'
	])

	const packaged = canonicalControllerInvocation(PROJECT_ROOT, 'win32')
	const usbasp = createControllerProgramCommand({
		invocation: packaged,
		method: 'usbasp',
		operation: PROGRAMMING_OPERATIONS.upload,
		appDevice: 'DO_NOT_OPEN',
		programmer: 'atmelice_isp',
		hex: commandPlanPaths(PROJECT_ROOT).completeFlash
	})
	assert.deepEqual(usbasp.args.slice(0, 8), [
		'program', '--method', 'usbasp', '--app-device', 'DO_NOT_OPEN',
		'--programmer', 'atmelice_isp', '--operation'
	])
	assert.throws(
		() => createControllerProgramCommand({
			invocation: packaged,
			method: 'urclock',
			operation: PROGRAMMING_OPERATIONS.upload,
			hex: 'firmware.hex'
		}),
		/serial device is required/
	)
})

test('firmware plan publishes the same target, artifacts, and explicit USBasp route', () => {
	const result = spawnSync(process.execPath, [
		join(PROJECT_ROOT, 'Tools', 'Firmware', 'firmware.mjs'),
		'upload', '--method', 'usbasp', '--plan-json'
	], {
		cwd: PROJECT_ROOT,
		env: { ...process.env, PCCONTROLLER_BUILD_TIMESTAMP: '0x35019D5D', NO_COLOR: '1' },
		encoding: 'utf8',
		windowsHide: true
	})
	assert.equal(result.status, 0, result.stderr || result.stdout)
	const plan = JSON.parse(result.stdout)
	assert.equal(plan.format, 'pccontroller-firmware-plan/v1')
	assert.deepEqual(plan.target, BOARD)
	assert.equal(plan.artifacts.application, '.build/firmware/PCController.ino.hex')
	assert.equal(plan.artifacts.completeFlash, '.build/firmware/PCController.ino.with_bootloader.hex')
	const program = plan.actions.find(action => action.id === 'program')
	assert.equal(program.hardware, true)
	assert.match(program.command.args.join(' '), /--method usbasp --operation write-flash/)
	assert.ok(program.command.args.includes(resolve(PROJECT_ROOT, plan.artifacts.completeFlash)))
	assert.doesNotMatch(JSON.stringify(plan), /powershell|pwsh|arduino-cli.*upload/i)
})

test('embedded web package has a lock matching every declared dependency', async () => {
	const web = join(PROJECT_ROOT, 'Tools', 'Controller', 'web')
	const declared = JSON.parse(await readFile(join(web, 'package.json'), 'utf8'))
	const lock = JSON.parse(await readFile(join(web, 'package-lock.json'), 'utf8'))
	assert.equal(lock.lockfileVersion, 3)
	assert.deepEqual(lock.packages[''].dependencies, declared.dependencies)
	assert.deepEqual(lock.packages[''].devDependencies, declared.devDependencies)
	for (const [name, range] of Object.entries({
		...(declared.dependencies || {}),
		...(declared.devDependencies || {})
	})) {
		assert.match(range, /^\^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/)
		const resolved = lock.packages[`node_modules/${name}`]?.version
		assert.match(resolved, /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/, `${name} must resolve exactly in package-lock.json`)
		const minimum = range.slice(1).split(/[+-]/, 1)[0].split('.').map(Number)
		const actual = resolved.split(/[+-]/, 1)[0].split('.').map(Number)
		assert.equal(actual[0], minimum[0], `${name} escaped its compatible major range`)
		if (minimum[0] === 0) assert.equal(actual[1], minimum[1], `${name} escaped its compatible 0.x minor range`)
		assert.ok(
			actual[0] > minimum[0] ||
			actual[1] > minimum[1] ||
			(actual[1] === minimum[1] && actual[2] >= minimum[2]),
			`${name}@${resolved} is older than ${range}`
		)
	}
})

test('packaging collects runtime web licenses and excludes build-only dependencies', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-web-notices-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	const web = join(root, 'web')
	const destination = join(root, 'licenses')
	await mkdir(join(web, 'node_modules', 'runtime-package'), { recursive: true })
	await mkdir(join(web, 'node_modules', 'build-package'), { recursive: true })
	await writeFile(join(web, 'package-lock.json'), JSON.stringify({
		lockfileVersion: 3,
		packages: {
			'': {},
			'node_modules/runtime-package': { version: '1.2.3', license: 'MIT' },
			'node_modules/build-package': { version: '4.5.6', license: 'MIT', dev: true }
		}
	}))
	await writeFile(join(web, 'node_modules', 'runtime-package', 'package.json'), '{"name":"runtime-package","version":"1.2.3"}')
	await writeFile(join(web, 'node_modules', 'runtime-package', 'LICENCE.md'), 'runtime license')
	await writeFile(join(web, 'node_modules', 'build-package', 'package.json'), '{"name":"build-package","version":"4.5.6"}')
	await writeFile(join(web, 'node_modules', 'build-package', 'LICENSE'), 'build license')

	assert.equal(collectWebNotices(destination, web), 1)
	assert.equal(await readFile(join(destination, 'web', 'runtime-package@1.2.3', 'LICENCE.md'), 'utf8'), 'runtime license')
	assert.deepEqual(await readdir(join(destination, 'web')), ['runtime-package@1.2.3'])
})

test('packaging requires an exact reviewed supplement when a runtime package omits notice files', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-web-supplement-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	const web = join(root, 'web')
	const destination = join(root, 'licenses')
	await mkdir(join(web, 'node_modules', 'runtime-package'), { recursive: true })
	await mkdir(join(web, 'third-party-notices'), { recursive: true })
	await writeFile(join(web, 'package-lock.json'), JSON.stringify({
		lockfileVersion: 3,
		packages: {
			'': {},
			'node_modules/runtime-package': { version: '1.2.3', license: 'MIT AND ISC' }
		}
	}))
	await writeFile(join(web, 'node_modules', 'runtime-package', 'package.json'),
		'{"name":"runtime-package","version":"1.2.3","license":"MIT AND ISC"}')

	assert.throws(() => collectWebNotices(destination, web), /no package notice or reviewed supplement/)
	await writeFile(join(web, 'third-party-notices', 'runtime-package.txt'), 'Package: wrong\nDeclared license: MIT\n')
	assert.throws(() => collectWebNotices(destination, web), /header does not match/)
	await writeFile(join(web, 'third-party-notices', 'runtime-package.txt'),
		'Package: runtime-package\nDeclared license: MIT AND ISC\n\nReviewed license text.\n')
	assert.equal(collectWebNotices(destination, web), 1)
	assert.equal(
		await readFile(join(destination, 'web', 'runtime-package@1.2.3', 'LICENSE-SUPPLEMENT.txt'), 'utf8'),
		'Package: runtime-package\nDeclared license: MIT AND ISC\n\nReviewed license text.\n'
	)
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

test('all public launchers advertise the shared Node runtime floor', async () => {
	const launchers = new Map()
	for (const name of ['build.cmd', 'build.sh', 'firmware.cmd', 'firmware.sh']) {
		const source = await readFile(join(PROJECT_ROOT, name), 'utf8')
		launchers.set(name, source)
		assert.match(source, /Node\.js 22\.12 or newer/, `${name} has a divergent Node requirement`)
		assert.match(source, /Install Node\.js, then run this command again\./)
	}
	const npmFailure = 'npm was not found in PATH; it is required to install the locked build UI dependencies.'
	assert.ok(launchers.get('build.cmd').includes(npmFailure))
	assert.ok(launchers.get('build.sh').includes(npmFailure))
	const firmwareSource = await readFile(
		join(PROJECT_ROOT, 'Tools', 'Firmware', 'firmware.mjs'),
		'utf8'
	)
	assert.match(firmwareSource, /MINIMUM_NODE = Object\.freeze\(\{ major: 22, minor: 12 \}\)/)
})

test('programming is explicit and unsupported direct dependency upload is rejected', () => {
	assert.throws(
		() => parseArguments(['--method', 'arduino'], {}),
		error => error instanceof BuildError && /direct dependency upload/.test(error.message)
	)
	assert.throws(
		() => parseArguments(['--upload'], {}),
		error => error instanceof BuildError && /requires --port/.test(error.message)
	)
	const serial = parseArguments(['--upload', '--port', 'DO_NOT_OPEN'], {})
	assert.equal(serial.method, 'urclock')
	assert.equal(serial.device, 'DO_NOT_OPEN')
})

test('USBasp and bootloader paths use the canonical method without alpha aliases', () => {
	const usbasp = parseArguments(['--upload', '--method', 'usbasp'], {})
	assert.equal(usbasp.upload, true)
	assert.equal(usbasp.method, 'usbasp')
	assert.equal(usbasp.programmer, '')
	assert.throws(
		() => parseArguments(['--install-bootloader'], {}),
		error => /requires explicit --method usbasp/.test(error.message)
	)
	const bootloader = parseArguments(['--install-bootloader', '--method', 'usbasp'], {})
	const plan = createPlan(bootloader, resolveBuildIdentity(bootloader, {}), 'win32')
	const command = plan.actions.find(action => action.id === 'install-bootloader').command.args
	assert.match(command.join(' '), /--method usbasp --operation install-bootloader/)
	assert.doesNotMatch(command.join(' '), /--programmer|troubleshooting/)
	const alternate = parseArguments([
		'--upload', '--method', 'usbasp', '--port', 'DO_NOT_OPEN',
		'--programmer', 'atmelice_isp'
	], {})
	const alternatePlan = createPlan(alternate, resolveBuildIdentity(alternate, {}), 'win32')
	const program = alternatePlan.actions.find(action => action.id === 'program').command.args
	assert.match(program.join(' '), /--method usbasp/)
	assert.match(program.join(' '), /--app-device DO_NOT_OPEN/)
	assert.match(program.join(' '), /--programmer atmelice_isp/)
})

test('toolchain synchronization is explicit and owned by Controller', () => {
	assert.throws(
		() => parseArguments(['--host-only', '--toolchain-cli', 'arduino-cli'], {}),
		error => /requires firmware compilation or --toolchain-sync/.test(error.message)
	)
	const options = parseArguments([
		'--host-only', '--toolchain-sync', '--toolchain-cli', 'CUSTOM_CLI',
		'--build-time', '2026-08-01T16:12:58Z', '--build-timestamp', '35019D5D'
	], {})
	const plan = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
	const update = plan.actions.find(action => action.id === 'toolchain-sync')
	assert.ok(update)
	assert.equal(update.externalMutation, true)
	assert.match(update.command.args.join(' '), /toolchain sync --cli CUSTOM_CLI/)
	assert.doesNotMatch(JSON.stringify(update), /arduino-cli.*core.*update-index/i)
})

test('portable CLI and config are propagated to compile with flags over environment', () => {
	const fromEnvironment = parseArguments(['--firmware-only'], {
		pccontroller_toolchain_cli: 'ENV_CLI',
		PcController_Toolchain_Config: 'ENV_CONFIG'
	})
	let compile = createPlan(
		fromEnvironment,
		resolveBuildIdentity(fromEnvironment, {}),
		'win32'
	).actions.find(action => action.id === 'firmware-compile').command.args
	assert.match(compile.join(' '), /--toolchain-cli ENV_CLI --toolchain-config ENV_CONFIG/)

	const fromFlags = parseArguments([
		'--firmware-only',
		'--toolchain-cli', 'FLAG_CLI',
		'--toolchain-config', 'FLAG_CONFIG'
	], {
		PCCONTROLLER_TOOLCHAIN_CLI: 'ENV_CLI',
		PCCONTROLLER_TOOLCHAIN_CONFIG: 'ENV_CONFIG'
	})
	compile = createPlan(
		fromFlags,
		resolveBuildIdentity(fromFlags, {}),
		'win32'
	).actions.find(action => action.id === 'firmware-compile').command.args
	assert.match(compile.join(' '), /--toolchain-cli FLAG_CLI --toolchain-config FLAG_CONFIG/)
	assert.doesNotMatch(compile.join(' '), /ENV_CLI|ENV_CONFIG/)
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

test('build presentation switches override environment and package defaults', () => {
	const options = parseArguments([
		'--host-only', '--app-name', 'Flag Controller',
		'--tagline', 'Flag first-run line'
	], {})
	const identity = resolveBuildIdentity(options, {
		PCCONTROLLER_BUILD_APP_NAME: 'Environment Controller',
		PCCONTROLLER_BUILD_TAGLINE: 'Environment line',
		APP_TITLE: 'Legacy build title',
		APP_TAGLINE: 'Runtime/build line'
	})
	assert.equal(identity.appName, 'Flag Controller')
	assert.equal(identity.tagline, 'Flag first-run line')
	assert.equal(identity.env.PCCONTROLLER_BUILD_APP_NAME, 'Flag Controller')
	assert.equal(identity.env.PCCONTROLLER_BUILD_TAGLINE, 'Flag first-run line')
	assert.deepEqual(
		Object.keys(identity.env).filter(name => name.toLowerCase() === 'pccontroller_build_app_name'),
		['PCCONTROLLER_BUILD_APP_NAME']
	)
	assert.deepEqual(
		Object.keys(identity.env).filter(name => name.toLowerCase() === 'pccontroller_build_tagline'),
		['PCCONTROLLER_BUILD_TAGLINE']
	)
	const plan = createPlan(options, identity, 'win32')
	assert.deepEqual(plan.identity, {
		version: PRODUCT_METADATA.version,
		appName: 'Flag Controller',
		tagline: 'Flag first-run line',
		hostBuildTime: identity.hostBuildTime,
		packedTimestamp: identity.packedTimestamp
	})
})

test('build presentation environment is case-insensitive and validated', () => {
	const options = parseArguments(['--host-only'], {})
	const identity = resolveBuildIdentity(options, {
		pccontroller_build_app_name: 'Environment Controller',
		PcController_Build_Tagline: 'Environment first-run line'
	})
	assert.equal(identity.appName, 'Environment Controller')
	assert.equal(identity.tagline, 'Environment first-run line')
	const overridden = resolveBuildIdentity(parseArguments([
		'--app-name', 'Flag Controller', '--tagline', 'Flag line'
	], {}), {
		pccontroller_build_app_name: 'Stale environment title',
		PcController_Build_Tagline: 'Stale environment line'
	})
	assert.equal(overridden.appName, 'Flag Controller')
	assert.equal(overridden.tagline, 'Flag line')
	assert.deepEqual(
		Object.keys(overridden.env).filter(name => name.toLowerCase() === 'pccontroller_build_app_name'),
		['PCCONTROLLER_BUILD_APP_NAME']
	)
	assert.deepEqual(
		Object.keys(overridden.env).filter(name => name.toLowerCase() === 'pccontroller_build_tagline'),
		['PCCONTROLLER_BUILD_TAGLINE']
	)
	assert.throws(
		() => resolveBuildIdentity(parseArguments(['--app-name', 'x'.repeat(65)], {}), {}),
		/1\.\.64 printable characters/
	)
	assert.throws(
		() => resolveBuildIdentity(parseArguments(['--tagline', 'line\nbreak'], {}), {}),
		/1\.\.96 printable characters/
	)
	assert.throws(
		() => resolveBuildIdentity(parseArguments(['--app-name='], {}), {
			PCCONTROLLER_BUILD_APP_NAME: 'must not mask an explicit empty flag'
		}),
		/1\.\.64 printable characters/
	)
	assert.throws(
		() => resolveBuildIdentity(parseArguments(['--tagline', ''], {}), {
			PCCONTROLLER_BUILD_TAGLINE: 'must not mask an explicit empty flag'
		}),
		/1\.\.96 printable characters/
	)
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
		'--product-version', PRODUCT_METADATA.version, '--file-version', PRODUCT_METADATA.version,
		'<staging>/controller.exe'
	])
	assert.doesNotMatch(JSON.stringify(plan.actions), /go-winres[^}]*make|\.syso/i)
})

test('Windows installation inventory follows the final packed host manifest', () => {
	const options = parseArguments(['--host-only'], {})
	const windows = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
	const ids = windows.actions.map(action => action.id)
	assert.ok(ids.indexOf('upx-test') < ids.indexOf('host-manifest'))
	assert.ok(ids.indexOf('host-manifest') < ids.indexOf('installation-inventory'))
	assert.ok(ids.indexOf('installation-inventory') < ids.indexOf('host-publish'))
	const linux = createPlan(options, resolveBuildIdentity(options, {}), 'linux')
	assert.equal(linux.actions.some(action => action.id === 'installation-inventory'), false)
})

test('Windows package carries the hash-bound toast logo before inventory generation', async () => {
	const source = await readFile(join(PROJECT_ROOT, 'Tools', 'Build', 'build.mjs'), 'utf8')
	assert.match(source, /copyFileSync\(join\(HOST_ROOT, 'winres', 'icon\.png'\), toastLogo\)/)
	assert.match(source, /\[executable, \.\.\.\(toastLogo \? \[toastLogo\] : \[\]\), \.\.\.shared\.paths\]/)
	assert.ok(source.indexOf("toastLogo = join(stage, 'toast-logo.png')") < source.indexOf("'installation-package.json'"))
})

test('Win32 resource configuration retains icon, manifest, and version data', async () => {
	const source = await readFile(
		join(PROJECT_ROOT, 'Tools', 'Controller', 'winres', 'winres.json'),
		'utf8'
	)
	const resources = JSON.parse(source)
	assert.equal(resources.RT_GROUP_ICON.APP['0000'], 'icon.ico')
	assert.deepEqual(
		Object.fromEntries(Object.entries(resources.RT_GROUP_ICON).map(([name, entry]) => [name, entry['0000']])),
		{
			APP: 'icon.ico',
			TRAY_CONNECTED: 'icon-connected.ico',
			TRAY_RECONNECTING: 'icon-reconnecting.ico',
			TRAY_PAUSED: 'icon-paused.ico',
			TRAY_OFFLINE: 'icon-offline.ico',
		}
	)
	assert.ok(resources.RT_MANIFEST['#1']['0409'])
	assert.equal(
		resources.RT_VERSION['#1']['0409'].info['0409'].OriginalFilename,
		'controller.exe'
	)
	const fixedVersion = `${PRODUCT_METADATA.version.match(/^\d+\.\d+\.\d+/)?.[0]}.0`
	assert.equal(resources.RT_VERSION['#1']['0409'].fixed.file_version, fixedVersion)
	assert.equal(resources.RT_VERSION['#1']['0409'].fixed.product_version, fixedVersion)
	assert.equal(resources.RT_VERSION['#1']['0409'].info['0409'].FileVersion, PRODUCT_METADATA.version)
	assert.equal(resources.RT_VERSION['#1']['0409'].info['0409'].ProductVersion, PRODUCT_METADATA.version)
})

test('browser ICO is the exact seven-size native executable icon', async () => {
	const ico = await readFile(join(
		PROJECT_ROOT, 'Tools', 'Controller', 'web', 'public', 'favicon.ico'
	))
	const nativeICO = await readFile(join(
		PROJECT_ROOT, 'Tools', 'Controller', 'winres', 'icon.ico'
	))
	assert.deepEqual(ico, nativeICO)
	assert.equal(ico.readUInt16LE(0), 0)
	assert.equal(ico.readUInt16LE(2), 1)
	assert.equal(ico.readUInt16LE(4), 7)
	assert.deepEqual(
		Array.from({ length: 7 }, (_, index) => ico[6 + index * 16]),
		[0, 128, 64, 48, 32, 24, 16]
	)
	for (let index = 0; index < 7; index += 1) {
		const entry = 6 + index * 16
		const bytes = ico.readUInt32LE(entry + 8)
		const offset = ico.readUInt32LE(entry + 12)
		assert.ok(bytes > 0 && offset + bytes <= ico.length)
		assert.deepEqual(ico.subarray(offset, offset + 8), Buffer.from([
			0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a
		]))
	}
	const buildSource = await readFile(
		join(PROJECT_ROOT, 'Tools', 'Build', 'build.mjs'),
		'utf8'
	)
	assert.match(
		buildSource,
		/generate_icon\.go', '\.\/winres\/icon\.png', '\.\/winres\/icon\.ico'/u
	)
	assert.match(
		buildSource,
		/copyFileSync\(join\(HOST_ROOT, 'winres', 'icon\.ico'\), join\(WEB_ROOT, 'public', 'favicon\.ico'\)\)/u
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
        assert.deepEqual(plan.actions.map(action => action.id), [
                'firmware-compile', 'default-eeprom', 'firmware-manifest'
        ])
	assert.equal(plan.actions[0].command.file, 'go')
	assert.match(plan.actions[0].command.args.join(' '), /run.*cmd\/controller.*program.*compile/)
        assert.equal(plan.actions[0].hardware, false)
        assert.match(plan.actions[1].command.args.join(' '), /default-assets.*safe-default-eeprom\.hex/)
        assert.match(plan.actions[2].command.args.join(' '), /firmware\.mjs manifest/)
        assert.ok(plan.actions.every(action => action.hardware === false))
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

test('clean plan includes canonical and audited stale host outputs', () => {
	const options = parseArguments(['--clean'], {})
	const plan = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
	assert.equal(plan.actions.length, 1)
	const paths = plan.actions[0].paths.map(path => path.replaceAll('\\', '/'))
	assert.ok(paths.some(path => path.endsWith('/Tools/Controller/bin')))
	assert.ok(paths.some(path => path.endsWith('/Tools/Controller/controller.exe')))
	assert.ok(paths.some(path => path.endsWith('/Tools/Controller/.cache/identity-build')))
})

test('firmware-only clean never targets a running packaged host', () => {
        const options = parseArguments(['--firmware-only', '--clean'], {})
        const targets = generatedCleanTargets(options).map(path => path.replaceAll('\\', '/'))
        assert.equal(targets.length, 1)
        assert.match(targets[0], /\/\.build\/firmware$/)
        assert.ok(!targets.some(path => /\/Tools\/Controller\/bin(?:\/|$)/.test(path)))
        const plan = createPlan(options, resolveBuildIdentity(options, {}), 'win32')
        assert.deepEqual(plan.actions.map(action => action.id), [
                'clean', 'firmware-compile', 'default-eeprom', 'firmware-manifest'
        ])
        assert.deepEqual(plan.actions[0].paths, generatedCleanTargets(options))
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

test('host source identity covers locked web inputs and exact embedded output', async t => {
	const root = await mkdtemp(join(tmpdir(), 'pccontroller-web-identity-'))
	t.after(() => rm(root, { recursive: true, force: true }))
	await mkdir(join(root, 'web', 'src'), { recursive: true })
	await mkdir(join(root, 'web', 'public', 'fonts'), { recursive: true })
	await mkdir(join(root, 'internal', 'webui', 'dist', 'assets'), { recursive: true })
	await writeFile(join(root, 'main.go'), 'package fixture\n')
	await writeFile(join(root, 'go.mod'), 'module example.invalid/fixture\n')
	await writeFile(join(root, 'web', 'package.json'), '{"name":"fixture"}\n')
	await writeFile(join(root, 'web', 'package-lock.json'), '{"lockfileVersion":3}\n')
	await writeFile(join(root, 'web', 'src', 'app.tsx'), 'export const app = 1\n')
	await writeFile(join(root, 'web', 'public', 'fonts', 'ui.woff2'), 'font-v1')
	await writeFile(join(root, 'internal', 'webui', 'dist', 'index.html'), '<script src="/assets/app.js"></script>\n')
	await writeFile(join(root, 'internal', 'webui', 'dist', 'assets', 'app.js'), 'console.log(1)\n')

	const first = hostSourceIdentity(root)
	assert.match(first.manifest, /web\/package-lock\.json:/)
	assert.match(first.manifest, /web\/src\/app\.tsx:/)
	assert.match(first.manifest, /web\/public\/fonts\/ui\.woff2:/)
	assert.match(first.manifest, /internal\/webui\/dist\/assets\/app\.js:/)

	await writeFile(join(root, 'web', 'src', 'app.tsx'), 'export const app = 2\n')
	const sourceChanged = hostSourceIdentity(root)
	assert.notEqual(sourceChanged.sha256, first.sha256)
	await writeFile(join(root, 'web', 'src', 'app.tsx'), 'export const app = 1\n')
	await writeFile(join(root, 'internal', 'webui', 'dist', 'assets', 'app.js'), 'console.log(2)\n')
	assert.notEqual(hostSourceIdentity(root).sha256, first.sha256)

	await mkdir(join(root, 'web', 'node_modules', 'ignored'), { recursive: true })
	await writeFile(join(root, 'web', 'node_modules', 'ignored', 'cache.js'), 'ignored')
	const withInstallState = hostSourceIdentity(root)
	await rm(join(root, 'web', 'node_modules'), { recursive: true, force: true })
	assert.deepEqual(hostSourceIdentity(root), withInstallState)
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

test('Unix C ABI smoke uses a valid handle and keeps the Go runtime loaded', () => {
	const source = unixSmokeSource('pccontroller.so')
	assert.match(source, /dlopen\("\.\/pccontroller\.so"/)
	assert.ok(source.includes('{\\"operation\\":\\"create\\"}'))
	assert.match(source, /strtoull\(handle_field/)
	assert.ok(source.includes('{\\"operation\\":\\"destroy\\",\\"handle\\":%llu}'))
	assert.doesNotMatch(source, /dlclose\(module\)/)
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

	const firmwareCMD = spawnSync('cmd.exe', [
		'/d', '/s', '/c', 'firmware.cmd upload --method usbasp --plan-json'
	], { cwd: PROJECT_ROOT, env, encoding: 'utf8', windowsHide: true })
	assert.equal(firmwareCMD.status, 0, firmwareCMD.stderr || firmwareCMD.stdout)
	const firmwareBash = spawnSync('bash.exe', [
		'firmware.sh', 'upload', '--method', 'usbasp', '--plan-json'
	], { cwd: PROJECT_ROOT, env, encoding: 'utf8', windowsHide: true })
	assert.equal(firmwareBash.status, 0, firmwareBash.stderr || firmwareBash.stdout)
        assert.deepEqual(JSON.parse(firmwareCMD.stdout), JSON.parse(firmwareBash.stdout))
})

test('CMD and Bash wrappers expose identical help and failure contracts on Windows', {
        skip: process.platform !== 'win32'
}, () => {
        const env = { ...process.env, NO_COLOR: '1' }
        const normalized = result => ({
                status: result.status,
                stdout: (result.stdout || '').replaceAll('\r\n', '\n'),
                stderr: (result.stderr || '').replaceAll('\r\n', '\n')
        })
        const cases = [
                { cmd: 'build.cmd', bash: 'build.sh', args: ['--help'], status: 0, text: 'project-owned build' },
                { cmd: 'build.cmd', bash: 'build.sh', args: ['--invalid-entrypoint-test'], status: 2, text: 'unknown option' },
                { cmd: 'firmware.cmd', bash: 'firmware.sh', args: ['--help'], status: 0, text: 'firmware studio' },
                { cmd: 'firmware.cmd', bash: 'firmware.sh', args: ['--invalid-entrypoint-test'], status: 2, text: 'Unknown option' }
        ]
        for (const fixture of cases) {
                const cmd = normalized(spawnSync('cmd.exe', [
                        '/d', '/s', '/c', [fixture.cmd, ...fixture.args].join(' ')
                ], { cwd: PROJECT_ROOT, env, encoding: 'utf8', windowsHide: true }))
                const bash = normalized(spawnSync('bash.exe', [
                        fixture.bash, ...fixture.args
                ], { cwd: PROJECT_ROOT, env, encoding: 'utf8', windowsHide: true }))
                assert.equal(cmd.status, fixture.status, cmd.stderr || cmd.stdout)
                assert.equal(bash.status, fixture.status, bash.stderr || bash.stdout)
                assert.deepEqual(cmd, bash, `${fixture.cmd} and ${fixture.bash} drifted`)
                assert.match(`${cmd.stdout}\n${cmd.stderr}`, new RegExp(fixture.text, 'i'))
        }
})
