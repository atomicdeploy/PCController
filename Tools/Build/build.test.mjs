import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { renameSync } from 'node:fs'
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { delimiter, join, resolve, sep } from 'node:path'
import test from 'node:test'

import {
	BuildError,
	PROJECT_ROOT,
	assertGeneratedPath,
	collectWebNotices,
	createPlan,
        hostSourceIdentity,
        installPackage,
	packBuildTimestamp,
	parseArguments,
	refreshedEnvironment,
	renderTable,
	removeGeneratedWinResources,
	resolveBuildIdentity,
	resolveProductTitle,
	unixSmokeSource,
	verboseCommandText,
	windowsSmokeSource
} from './build.mjs'
import { createStableTestPlan, stableTestBinaryName } from './go-tests.mjs'

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
	const refreshed = refreshedEnvironment(windowsEnvironment, 'win32')
	assert.deepEqual(refreshed.PATH.split(delimiter).slice(0, 2), [selected, session])
	assert.deepEqual(Object.keys(refreshed).filter(key => key.toLowerCase() === 'path'), ['PATH'])
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

test('package publishing tolerates a shell holding the canonical directory', async t => {
        const root = await mkdtemp(join(tmpdir(), 'pccontroller-package-lock-'))
        t.after(() => rm(root, { recursive: true, force: true }))
        const canonical = join(root, 'bin')
        const stage = join(root, 'stage')
        await mkdir(join(canonical, 'licenses'), { recursive: true })
        await mkdir(join(stage, 'licenses'), { recursive: true })
        await writeFile(join(canonical, 'controller.exe'), 'old host')
        await writeFile(join(canonical, 'stale.txt'), 'remove me')
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
	assert.deepEqual(plan.actions.slice(0, 6).map(action => action.id), [
		'web-install', 'web-typecheck', 'web-test', 'web-build',
		'product-identity-check', 'go-mod-download'
	])
	assert.deepEqual(plan.actions[0].command.args, ['ci', '--no-audit', '--no-fund'])
	assert.equal(plan.actions[0].command.cwd, join(PROJECT_ROOT, 'Tools', 'Controller', 'web'))
	assert.ok(plan.actions.findIndex(action => action.id === 'web-build') <
		plan.actions.findIndex(action => action.id === 'go-test'))
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
	await writeFile(join(web, 'node_modules', 'runtime-package', 'LICENSE'), 'runtime license')
	await writeFile(join(web, 'node_modules', 'build-package', 'package.json'), '{"name":"build-package","version":"4.5.6"}')
	await writeFile(join(web, 'node_modules', 'build-package', 'LICENSE'), 'build license')

	assert.equal(collectWebNotices(destination, web), 1)
	assert.equal(await readFile(join(destination, 'web', 'runtime-package@1.2.3', 'LICENSE'), 'utf8'), 'runtime license')
	assert.deepEqual(await readdir(join(destination, 'web')), ['runtime-package@1.2.3'])
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

test('USBasp and bootloader paths require the troubleshooting guard', () => {
	assert.throws(
		() => parseArguments(['--upload', '--method', 'usbasp'], {}),
		error => /USBasp is hidden troubleshooting only/.test(error.message)
	)
	assert.throws(
		() => parseArguments(['--install-bootloader'], {}),
		error => /requires --usbasp-troubleshooting/.test(error.message)
	)
	const guarded = parseArguments(['--usbasp-flash'], {})
	assert.equal(guarded.upload, true)
	assert.equal(guarded.method, 'usbasp')
	assert.equal(guarded.usbaspTroubleshooting, true)
})

test('toolchain synchronization is explicit and owned by Controller', () => {
	assert.throws(
		() => parseArguments(['--toolchain-cli', 'arduino-cli'], {}),
		error => /only valid with --toolchain-sync/.test(error.message)
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
})
