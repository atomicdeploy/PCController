#!/usr/bin/env node

// Cross-platform project build and packaging orchestrator. All firmware
// compile/program operations route through the project's Controller command;
// this file never shells through PowerShell or invokes Arduino upload directly.

import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import {
	chmodSync,
	copyFileSync,
	cpSync,
	existsSync,
	lstatSync,
	mkdirSync,
	readdirSync,
	readFileSync,
	renameSync,
	rmSync,
	statSync,
	writeFileSync
} from 'node:fs'
import { basename, delimiter, dirname, extname, isAbsolute, join, relative, resolve, sep } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { createChalk, renderUnicodeBanner, renderUnicodeTable } from './presentation.mjs'
import { resolveProductTitle } from './product-metadata.mjs'

export { resolveProductTitle } from './product-metadata.mjs'

const SCRIPT = fileURLToPath(import.meta.url)
export const PROJECT_ROOT = resolve(dirname(SCRIPT), '..', '..')
const HOST_ROOT = join(PROJECT_ROOT, 'Tools', 'Controller')
const WEB_ROOT = join(HOST_ROOT, 'web')
const WEB_DIST = join(HOST_ROOT, 'internal', 'webui', 'dist')
const WEB_LOCK = join(WEB_ROOT, 'package-lock.json')
const BUILD_ROOT = join(PROJECT_ROOT, '.build')
const FIRMWARE_OUTPUT = join(BUILD_ROOT, 'firmware')
const PACKAGE_ROOT = join(BUILD_ROOT, 'package')
const STABLE_GO_TEST_ROOT = join(BUILD_ROOT, 'tests', 'go')
const STABLE_GO_TEST_RUNNER = join(PROJECT_ROOT, 'Tools', 'Build', 'go-tests.mjs')
const PRODUCT_IDENTITY_GENERATOR = join(HOST_ROOT, 'internal', 'productidentity', 'generate.mjs')
export const CANONICAL_HOST_OUTPUT = join(HOST_ROOT, 'bin')
const LEGACY_HOST_OUTPUTS = [
	join(BUILD_ROOT, 'host'),
	join(HOST_ROOT, '.build-test-bin'),
	join(HOST_ROOT, '.build-upx-bin'),
	join(HOST_ROOT, '.cache', 'identity-build'),
	join(HOST_ROOT, 'controller.exe'),
	join(HOST_ROOT, 'controller')
]
const HOST_MANIFEST_FORMAT = 'pccontroller-host-package-manifest/v1'
const FIRMWARE_MANIFEST_FORMAT = 'pccontroller-avr-firmware-manifest/v1'
const MINIMUM_NODE = [22, 12, 0]
const MINIMUM_WEB_NODE = [22, 12, 0]

export class BuildError extends Error {
	constructor(message, exitCode = 1) {
		super(message)
		this.name = 'BuildError'
		this.exitCode = exitCode
	}
}

function valueAfter(argv, index, inline, option) {
	if (inline !== undefined) return [inline, index]
	if (index + 1 >= argv.length) throw new BuildError(`${option} requires a value`, 2)
	return [argv[index + 1], index + 1]
}

function normalizeHexTimestamp(value) {
	if (typeof value === 'number') {
		if (!Number.isInteger(value) || value < 0 || value > 0xFFFFFFFF) {
			throw new BuildError('build timestamp number must be an unsigned 32-bit integer', 2)
		}
		return value.toString(16).toUpperCase().padStart(8, '0')
	}
	const text = String(value).replace(/^0x/i, '')
	if (!/^[0-9a-f]{1,8}$/i.test(text)) {
		throw new BuildError('build timestamp must be one to eight hexadecimal digits', 2)
	}
	return Number.parseInt(text, 16).toString(16).toUpperCase().padStart(8, '0')
}

export function packBuildTimestamp(date) {
	if (!(date instanceof Date) || Number.isNaN(date.getTime())) {
		throw new BuildError('invalid build date', 2)
	}
	const year = date.getFullYear() - 2000
	if (year < 0 || year > 127) {
		throw new BuildError(`firmware build year ${date.getFullYear()} is outside 2000..2127`, 2)
	}
	const packedDate = (year << 9) | ((date.getMonth() + 1) << 5) | date.getDate()
	const packedTime = (date.getHours() << 11) | (date.getMinutes() << 5) | (date.getSeconds() >> 1)
	return (((packedDate << 16) >>> 0) | packedTime) >>> 0
}

function buildInstant(options, env, now = new Date()) {
	const explicit = options.buildTime || env.PCCONTROLLER_HOST_BUILD_TIME
	if (explicit) {
		const parsed = new Date(explicit)
		if (Number.isNaN(parsed.getTime())) throw new BuildError(`invalid host build time: ${explicit}`, 2)
		return parsed
	}
	if (env.SOURCE_DATE_EPOCH) {
		if (!/^\d+$/.test(env.SOURCE_DATE_EPOCH)) {
			throw new BuildError('SOURCE_DATE_EPOCH must be whole UTC seconds', 2)
		}
		const parsed = new Date(Number(env.SOURCE_DATE_EPOCH) * 1000)
		if (Number.isNaN(parsed.getTime())) throw new BuildError('SOURCE_DATE_EPOCH is outside the supported range', 2)
		return parsed
	}
	return now
}

export function resolveBuildIdentity(options, env = process.env, now = new Date()) {
	const instant = buildInstant(options, env, now)
	const packed = normalizeHexTimestamp(
		options.buildTimestamp || env.PCCONTROLLER_BUILD_TIMESTAMP || packBuildTimestamp(instant)
	)
	const version = options.version || env.PCCONTROLLER_VERSION || 'development'
	if (!/^[0-9A-Za-z][0-9A-Za-z._+-]*$/.test(version)) {
		throw new BuildError('version may contain only letters, digits, dot, underscore, plus, and hyphen', 2)
	}
	return {
		version,
		hostBuildTime: instant.toISOString().replace('.000Z', 'Z'),
		packedTimestamp: packed,
		env: {
			...env,
			PCCONTROLLER_BUILD_TIMESTAMP: `0x${packed}`,
			PCCONTROLLER_HOST_BUILD_TIME: instant.toISOString().replace('.000Z', 'Z')
		}
	}
}

export function parseArguments(argv, env = process.env) {
	const options = {
		firmware: true,
		host: true,
		selection: '',
		clean: false,
		cleanOnly: false,
		tests: true,
		retest: false,
		vet: true,
		resources: true,
		upx: true,
		sharedLibrary: true,
		verbose: false,
		noColor: hasEnvironmentName(env, 'NO_COLOR'),
		forceColor: hasEnvironmentName(env, 'FORCE_COLOR'),
		dryRun: false,
		planJSON: false,
		help: false,
		upload: false,
		method: 'urclock',
		device: env.PCCONTROLLER_DEVICE || env.PCCONTROLLER_PORT || '',
		programmer: env.PCCONTROLLER_PROGRAMMER || 'usbasp',
		usbaspTroubleshooting: false,
		allowIncompleteBackup: false,
		installBootloader: false,
		toolchainSync: false,
		toolchainCLI: '',
		version: '',
		buildTime: '',
		buildTimestamp: ''
	}
	let substantive = false
	for (let index = 0; index < argv.length; index += 1) {
		const argument = argv[index]
		const equals = argument.indexOf('=')
		const name = equals >= 0 ? argument.slice(0, equals) : argument
		const inline = equals >= 0 ? argument.slice(equals + 1) : undefined
		switch (name.toLowerCase()) {
			case '--help':
			case '-h': options.help = true; break
			case '--all': substantive = true; options.selection = 'all'; options.firmware = true; options.host = true; break
			case '--firmware-only':
				if (options.selection === 'host') throw new BuildError('choose either --firmware-only or --host-only', 2)
				substantive = true; options.selection = 'firmware'; options.firmware = true; options.host = false; break
			case '--host-only':
				if (options.selection === 'firmware') throw new BuildError('choose either --firmware-only or --host-only', 2)
				substantive = true; options.selection = 'host'; options.firmware = false; options.host = true; break
			case '--clean': options.clean = true; break
			case '--skip-tests': options.tests = false; options.vet = false; break
			case '--retest': options.retest = true; break
			case '--skip-vet': options.vet = false; break
			case '--skip-resources':
			case '--no-resources': options.resources = false; break
			case '--no-upx': options.upx = false; break
			case '--no-shared-library': options.sharedLibrary = false; break
			case '--verbose': options.verbose = true; break
			case '--no-color': options.noColor = true; break
			case '--force-color': options.forceColor = true; break
			case '--dry-run': options.dryRun = true; break
			case '--plan-json': options.planJSON = true; options.noColor = true; break
			case '--upload': options.upload = true; substantive = true; break
			case '--usbasp-flash':
				options.upload = true; options.method = 'usbasp'; options.usbaspTroubleshooting = true; substantive = true; break
			case '--usbasp-troubleshooting': options.usbaspTroubleshooting = true; break
			case '--allow-incomplete-backup': options.allowIncompleteBackup = true; break
			case '--install-bootloader': options.installBootloader = true; substantive = true; break
			case '--toolchain-sync': options.toolchainSync = true; options.host = true; substantive = true; break
			case '--method': {
				const [value, next] = valueAfter(argv, index, inline, name)
				options.method = value.toLowerCase(); index = next; break
			}
			case '--port':
			case '--device': {
				const [value, next] = valueAfter(argv, index, inline, name)
				options.device = value; index = next; break
			}
			case '--programmer': {
				const [value, next] = valueAfter(argv, index, inline, name)
				options.programmer = value; index = next; break
			}
			case '--toolchain-cli': {
				const [value, next] = valueAfter(argv, index, inline, name)
				options.toolchainCLI = value; index = next; break
			}
			case '--version': {
				const [value, next] = valueAfter(argv, index, inline, name)
				options.version = value; index = next; break
			}
			case '--build-time': {
				const [value, next] = valueAfter(argv, index, inline, name)
				options.buildTime = value; index = next; break
			}
			case '--build-timestamp': {
				const [value, next] = valueAfter(argv, index, inline, name)
				options.buildTimestamp = value; index = next; break
			}
			default: throw new BuildError(`unknown option: ${argument}`, 2)
		}
	}
	if (options.help) return options
	if (!['urclock', 'usbasp'].includes(options.method)) {
		throw new BuildError('--method must be urclock or usbasp; direct dependency upload is intentionally disabled', 2)
	}
	if ((options.upload || options.installBootloader) && options.selection === 'host') {
		throw new BuildError('--host-only cannot be combined with programming', 2)
	}
	if (options.upload) {
		options.firmware = true
		options.host = true
		if (options.method === 'urclock' && !options.device.trim()) {
			throw new BuildError('--upload through urclock requires --port/--device or PCCONTROLLER_PORT', 2)
		}
		if (options.method === 'usbasp' && !options.usbaspTroubleshooting) {
			throw new BuildError('USBasp is hidden troubleshooting only; pass --usbasp-troubleshooting', 2)
		}
	}
	if (options.installBootloader) {
		options.host = true
		if (!options.usbaspTroubleshooting) {
			throw new BuildError('bootloader provisioning requires --usbasp-troubleshooting', 2)
		}
	}
	if (options.toolchainCLI && !options.toolchainSync) {
		throw new BuildError('--toolchain-cli is only valid with --toolchain-sync', 2)
	}
	options.cleanOnly = options.clean && !substantive
	return options
}

function controllerInvocation(options) {
	const binary = join(CANONICAL_HOST_OUTPUT, process.platform === 'win32' ? 'controller.exe' : 'controller')
	if (options.host) return { file: binary, prefix: [], cwd: PROJECT_ROOT }
	return {
		file: 'go',
		prefix: ['run', '-buildvcs=false', './cmd/controller'],
		cwd: HOST_ROOT
	}
}

function commandAction(id, stage, file, args, cwd, hardware = false) {
	return { id, stage, command: { file, args, cwd }, hardware }
}

export function createPlan(options, identity, platform = process.platform) {
	const actions = []
	if (options.clean) actions.push({
		id: 'clean',
		stage: 'Clean generated build outputs',
		paths: [BUILD_ROOT, CANONICAL_HOST_OUTPUT, ...LEGACY_HOST_OUTPUTS],
		hardware: false
	})
	if (!options.cleanOnly && options.host) {
		actions.push(commandAction('web-install', 'Install locked web dependencies', 'npm', ['ci', '--no-audit', '--no-fund'], WEB_ROOT))
		actions.push(commandAction('web-typecheck', 'Type-check embedded web application', 'npm', ['run', 'typecheck'], WEB_ROOT))
		if (options.tests) actions.push(commandAction('web-test', 'Run embedded web tests', 'npm', ['run', 'test', '--', '--passWithNoTests'], WEB_ROOT))
		actions.push(commandAction('web-build', 'Build embedded web application', 'npm', ['run', 'build'], WEB_ROOT))
		actions.push(commandAction(
			'product-identity-check',
			'Check generated product identity and Win32 metadata',
			'node',
			[relative(PROJECT_ROOT, PRODUCT_IDENTITY_GENERATOR).replaceAll('\\', '/'), '--check'],
			PROJECT_ROOT
		))
		actions.push(commandAction('go-mod-download', 'Resolve Go modules', 'go', ['mod', 'download'], HOST_ROOT))
		if (options.tests) actions.push(commandAction(
			'go-test',
			'Run Go tests from stable project-owned binaries',
			'node',
			[
				relative(PROJECT_ROOT, STABLE_GO_TEST_RUNNER).replaceAll('\\', '/'),
				'--module', relative(PROJECT_ROOT, HOST_ROOT).replaceAll('\\', '/'),
				'--output', relative(PROJECT_ROOT, STABLE_GO_TEST_ROOT).replaceAll('\\', '/'),
				...(options.retest ? ['--retest'] : [])
			],
			PROJECT_ROOT
		))
		if (options.vet) actions.push(commandAction('go-vet', 'Run Go vet', 'go', ['vet', './...'], HOST_ROOT))
		actions.push(commandAction('host-build', 'Build controller host', 'go', ['build', '-buildvcs=false', '-trimpath', '-ldflags', `<identity ${identity.version} ${identity.hostBuildTime}>`, '-o', '<staging>/controller', './cmd/controller'], HOST_ROOT))
		if (platform === 'win32' && options.resources) actions.push(commandAction('winres', 'Apply Win32 resources', 'go-winres', ['patch', '--in', 'winres/winres.json', '--delete', '--no-backup', '<staging>/controller.exe'], HOST_ROOT))
		if (platform === 'win32' && options.upx) {
			actions.push(commandAction('upx-pack', 'Compress controller host', 'upx', ['--best', '--lzma', '<staging>/controller.exe'], HOST_ROOT))
			actions.push(commandAction('upx-test', 'Test compressed controller host', 'upx', ['-t', '<staging>/controller.exe'], HOST_ROOT))
		}
		if (options.sharedLibrary) actions.push(commandAction('c-abi', 'Build and smoke-test C ABI', 'go', ['build', '-buildvcs=false', '-trimpath', '-tags', 'controllerlib', '-buildmode=c-shared', '-o', '<staging>/pccontroller', './cmd/controllerlib'], HOST_ROOT))
		actions.push({ id: 'licenses', stage: 'Collect project and Go-module notices', hardware: false })
		actions.push({ id: 'host-manifest', stage: 'Publish canonical host package and manifest', hardware: false })
	}
	if (!options.cleanOnly && options.toolchainSync) {
		const controller = controllerInvocation({ ...options, host: true })
		const args = [...controller.prefix, 'toolchain', 'sync']
		if (options.toolchainCLI) args.push('--cli', options.toolchainCLI)
		const action = commandAction(
			'toolchain-sync',
			'Explicitly synchronize firmware indexes, cores, and libraries through Controller',
			controller.file,
			args,
			controller.cwd
		)
		action.externalMutation = true
		actions.push(action)
	}
	if (!options.cleanOnly && options.firmware) {
		const controller = controllerInvocation(options)
		actions.push(commandAction(
			'firmware-compile',
			'Compile AVR firmware through Controller',
			controller.file,
			[...controller.prefix, 'program', '--method', 'compile', '--sketch', PROJECT_ROOT, '--output-dir', FIRMWARE_OUTPUT],
			controller.cwd
		))
	}
	if (!options.cleanOnly && options.installBootloader) {
		const controller = controllerInvocation({ ...options, host: true })
		actions.push(commandAction('install-bootloader', 'Provision Urboot/fuses through Controller', controller.file, [
			...controller.prefix, 'program', '--method', 'usbasp', '--operation', 'install-bootloader', '--programmer', options.programmer, '--usbasp-troubleshooting'
		], controller.cwd, true))
	}
	if (!options.cleanOnly && options.upload) {
		const controller = controllerInvocation({ ...options, host: true })
		const args = [...controller.prefix, 'program', '--method', options.method, '--operation', 'write-flash', '--hex', `<${options.method === 'usbasp' ? 'flash+bootloader' : 'application'} artifact>`]
		if (options.method === 'urclock') args.push('--device', options.device)
		if (options.method === 'usbasp') args.push('--programmer', options.programmer, '--usbasp-troubleshooting')
		if (options.allowIncompleteBackup) args.push('--allow-incomplete-backup')
		actions.push(commandAction('program', `Explicit ${options.method} programming through Controller`, controller.file, args, controller.cwd, true))
	}
	return {
		format: 'pccontroller-build-plan/v1',
		canonicalController: relative(PROJECT_ROOT, join(CANONICAL_HOST_OUTPUT, platform === 'win32' ? 'controller.exe' : 'controller')).replaceAll('\\', '/'),
		firmwareOutput: relative(PROJECT_ROOT, FIRMWARE_OUTPUT).replaceAll('\\', '/'),
		identity: { version: identity.version, hostBuildTime: identity.hostBuildTime, packedTimestamp: identity.packedTimestamp },
		actions
	}
}

function usage(productTitle) {
	return `${productTitle} project-owned build / package utility

Usage:
  build.cmd [options]
  ./build.sh [options]

Safe build options:
  --all                     Build/package host and compile firmware (default)
  --firmware-only           Compile firmware through current Controller source
  --host-only               Test, vet, resource-stamp, package, UPX-test host
  --clean                   Remove generated output; alone, then stop
  --skip-tests              Explicitly skip Go tests and vet
  --retest                  Re-run unchanged stable Go test binaries
  --skip-vet                Explicitly skip Go vet only
  --skip-resources          Build Windows host without regenerated resources
  --no-upx                  Leave Windows controller executable unpacked
  --no-shared-library       Skip C ABI library/header/smoke test
  --version VALUE           Host version identity (default: development)
  --build-time ISO          Freeze host build time for reproducible packaging
  --build-timestamp HEX     Freeze packed firmware timestamp
  --toolchain-sync          Explicitly synchronize firmware dependencies
  --toolchain-cli PATH      Dependency CLI override (with --toolchain-sync)
  --dry-run                 Print the plan; run no subprocess and open no device
  --plan-json               Emit the shared CMD/Bash plan as JSON only
  --verbose                 Print every native argv vector
  --no-color                Disable ANSI styling
  --force-color             Retain Chalk styling in captured/CI output

Explicit programming only:
  --upload --port DEVICE    Build, backup, flash, verify via Urclock
  --usbasp-flash            Explicit hidden USBasp troubleshooting alias
  --method urclock|usbasp   Select guarded Controller programmer
  --usbasp-troubleshooting  Required for direct ISP/bootloader access
  --install-bootloader      Explicitly provision Urboot/fuses via Controller
  --programmer ID           ISP programmer ID (default: usbasp)
  --allow-incomplete-backup Advanced logged override; never the default

No programming action is implied by a normal build. Direct dependency upload
is disabled: Controller owns compile, backup, validation, programming, verify,
and application reauthentication.`
}

function versionParts(value) {
	return value.split('.').map(part => Number.parseInt(part, 10) || 0)
}

function compareVersion(left, right) {
	const a = versionParts(left)
	const b = versionParts(right)
	for (let index = 0; index < Math.max(a.length, b.length); index += 1) {
		const difference = (a[index] || 0) - (b[index] || 0)
		if (difference !== 0) return difference
	}
	return 0
}

function assertNodeVersion(minimum = MINIMUM_NODE, purpose = '') {
	if (compareVersion(process.versions.node, minimum.join('.')) < 0) {
		const suffix = purpose ? ` for ${purpose}` : ''
		throw new BuildError(`Node.js ${minimum.join('.')} or newer is required${suffix}; found ${process.versions.node}`)
	}
}

function environmentValue(env, name) {
	const key = Object.keys(env).find(candidate => candidate.toLowerCase() === name.toLowerCase())
	return key ? env[key] : ''
}

function hasEnvironmentName(env, name) {
	return Object.keys(env).some(candidate => candidate.toLowerCase() === name.toLowerCase())
}

function expandWindowsVariables(value, env) {
	return value.replace(/%([^%]+)%/g, (match, name) => environmentValue(env, name) || match)
}

function registryPath(key, env) {
	const result = spawnSync('reg.exe', ['query', key, '/v', 'Path'], {
		encoding: 'utf8', env, shell: false, windowsHide: true, timeout: 3000
	})
	if (result.status !== 0) return ''
	const match = result.stdout.match(/^\s*Path\s+REG_(?:EXPAND_)?SZ\s+(.+)$/mi)
	return match ? expandWindowsVariables(match[1].trim(), env) : ''
}

export function refreshedEnvironment(base = process.env, platform = process.platform) {
	if (platform !== 'win32') return { ...base }
	const machine = registryPath('HKLM\\SYSTEM\\CurrentControlSet\\Control\\Session Manager\\Environment', base)
	const user = registryPath('HKCU\\Environment', base)
	// The invoking shell or CI setup deliberately selects the active toolchain.
	// Registry paths only add newly installed tools that this process has not
	// inherited yet; they must never shadow an explicit current-process choice.
	const values = [environmentValue(base, 'PATH'), machine, user].flatMap(value => value.split(delimiter)).filter(Boolean)
	const seen = new Set()
	const path = []
	for (const entry of values) {
		const key = entry.toLowerCase()
		if (!seen.has(key)) {
			seen.add(key)
			path.push(entry)
		}
	}
	const normalized = { ...base }
	for (const key of Object.keys(normalized)) {
		if (key.toLowerCase() === 'path') delete normalized[key]
	}
	return { ...normalized, PATH: path.join(delimiter) }
}

function executableCandidates(name, env, platform = process.platform) {
	if (isAbsolute(name) || /[\\/]/.test(name)) return [name]
	const extensions = platform === 'win32'
		? (env.PATHEXT || '.COM;.EXE;.BAT;.CMD').split(';')
		: ['']
	const hasExtension = Boolean(extname(name))
	const candidates = []
	for (const directory of (env.PATH || '').split(delimiter)) {
		if (!directory) continue
		if (hasExtension) candidates.push(join(directory, name))
		else for (const extension of extensions) candidates.push(join(directory, `${name}${extension}`))
	}
	return candidates
}

export function allExecutables(name, env, platform = process.platform) {
	const seen = new Set()
	const found = []
	for (const candidate of executableCandidates(name, env, platform)) {
		try {
			if (!statSync(candidate).isFile()) continue
			const key = resolve(candidate).toLowerCase()
			if (!seen.has(key)) {
				seen.add(key)
				found.push(resolve(candidate))
			}
		} catch {
			// Continue through PATH.
		}
	}
	return found
}

function requireTool(name, env) {
	const found = allExecutables(name, env)
	if (found.length === 0) throw new BuildError(`required tool '${name}' was not found on the refreshed PATH`)
	return found[0]
}

function quote(value) {
	return /^[A-Za-z0-9_./:\\=+<>-]+$/.test(value) ? value : JSON.stringify(value)
}

function commandText(file, args) {
	return [file, ...args].map(value => quote(String(value))).join(' ')
}

export function verboseCommandText(file, args, env = process.env, isTTY = process.stdout.isTTY) {
	const message = `$ ${commandText(file, args)}`
	const color = !hasEnvironmentName(env, 'NO_COLOR') && (isTTY || hasEnvironmentName(env, 'FORCE_COLOR'))
	return createChalk({ noColor: !color, forceColor: color }, isTTY).gray(message)
}

function run(file, args, { cwd = PROJECT_ROOT, env = process.env, capture = false, timeout = 0, verbose = false } = {}) {
	if (verbose) process.stdout.write(`${verboseCommandText(file, args, env)}\n`)
	const result = spawnSync(file, args, {
		cwd,
		env,
		encoding: capture ? 'utf8' : undefined,
		stdio: capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
		shell: false,
		windowsHide: true,
		...(timeout > 0 ? { timeout } : {})
	})
	if (result.error) throw new BuildError(`failed to start ${file}: ${result.error.message}`)
	if (result.status !== 0) {
		const detail = capture ? `: ${(result.stderr || result.stdout || '').trim()}` : ''
		throw new BuildError(`${file} exited with code ${result.status}${detail}`)
	}
	return capture ? { stdout: result.stdout || '', stderr: result.stderr || '' } : result
}

function npmInvocation(env) {
	const command = requireTool('npm', env)
	if (process.platform !== 'win32' || !['.cmd', '.bat'].includes(extname(command).toLowerCase())) {
		return { file: command, prefix: [] }
	}
	const candidates = [
		join(dirname(command), 'node_modules', 'npm', 'bin', 'npm-cli.js'),
		join(dirname(process.execPath), 'node_modules', 'npm', 'bin', 'npm-cli.js')
	]
	const cli = candidates.find(path => existsSync(path))
	if (!cli) throw new BuildError(`npm launcher was found at ${command}, but npm-cli.js could not be resolved`)
	return { file: process.execPath, prefix: [cli] }
}

function directoryIdentity(root, excludeBuildState = false) {
	const files = []
	if (existsSync(root)) walkFiles(root, files, () => true, excludeBuildState)
	files.sort((left, right) => {
		const leftName = relative(root, left).replaceAll('\\', '/')
		const rightName = relative(root, right).replaceAll('\\', '/')
		return leftName < rightName ? -1 : leftName > rightName ? 1 : 0
	})
	const manifest = files.map(path => {
		const name = relative(root, path).replaceAll('\\', '/')
		return `${name}:${sha256File(path).toUpperCase()}\n`
	}).join('')
	return { sha256: sha256Buffer(Buffer.from(manifest, 'utf8')), files: files.length, manifest }
}

function verifyEmbeddedWebBuild() {
	const index = join(WEB_DIST, 'index.html')
	if (!existsSync(index)) throw new BuildError('web build did not produce internal/webui/dist/index.html')
	const html = readFileSync(index, 'utf8')
	if (/(?:\/src\/|@vite\/client)/i.test(html)) {
		throw new BuildError('web build output still references Vite development sources')
	}
	const identity = directoryIdentity(WEB_DIST)
	if (identity.files < 2) throw new BuildError('web build output is incomplete; expected index.html and bundled assets')
	return identity
}

function buildWebUI(options, env, log) {
	assertNodeVersion(MINIMUM_WEB_NODE, 'the embedded web build')
	if (!existsSync(WEB_LOCK)) {
		throw new BuildError('embedded web build requires Tools/Controller/web/package-lock.json; regenerate and review the lockfile')
	}
	const npm = npmInvocation(env)
	const npmEnv = {
		...env,
		CI: '1',
		npm_config_audit: 'false',
		npm_config_fund: 'false',
		npm_config_update_notifier: 'false'
	}
	const invoke = args => run(npm.file, [...npm.prefix, ...args], {
		cwd: WEB_ROOT,
		env: npmEnv,
		verbose: options.verbose
	})
	const inputsBefore = directoryIdentity(WEB_ROOT, true)
	log.stage('🔒', 'Installing locked web dependencies')
	invoke(['ci', '--no-audit', '--no-fund'])
	log.stage('🧭', 'Type-checking embedded web application')
	invoke(['run', 'typecheck'])
	if (options.tests) {
		log.stage('🧪', 'Running embedded web tests')
		invoke(['run', 'test', '--', '--passWithNoTests'])
	} else log.warning('Embedded web tests were explicitly skipped.')
	log.stage('✨', 'Building embedded web application')
	invoke(['run', 'build'])
	const inputsAfter = directoryIdentity(WEB_ROOT, true)
	if (inputsAfter.sha256 !== inputsBefore.sha256) {
		throw new BuildError('web source or package lock changed during the embedded build; retry from a stable tree')
	}
	const dist = verifyEmbeddedWebBuild()
	log.detail(`embedded web: ${dist.files} files, SHA256 ${dist.sha256}`)
	return {
		dependencies: 'npm-ci',
		typecheck: 'passed',
		tests: options.tests ? 'passed' : 'skipped',
		build: 'verified',
		distFiles: dist.files,
		distSHA256: dist.sha256,
		inputSHA256: inputsBefore.sha256
	}
}

export function assertGeneratedPath(root, target) {
	const resolvedRoot = resolve(root)
	const resolvedTarget = resolve(target)
	if (resolvedTarget === resolvedRoot || !resolvedTarget.startsWith(`${resolvedRoot}${sep}`)) {
		throw new BuildError(`refusing generated-file operation outside the project: ${resolvedTarget}`)
	}
	return resolvedTarget
}

function makeTreeWritable(current) {
	if (!existsSync(current)) return
	try {
		// Never follow a generated-tree symlink and mutate permissions outside it.
		if (lstatSync(current).isSymbolicLink()) return
	} catch {
		return
	}
	let entries = []
	try {
		entries = readdirSync(current, { withFileTypes: true })
	} catch {
		// The final removal call retains the authoritative error.
	}
	for (const entry of entries) {
		const path = join(current, entry.name)
		if (entry.isSymbolicLink()) continue
		if (entry.isDirectory()) makeTreeWritable(path)
		try {
			chmodSync(path, entry.isDirectory() ? 0o700 : 0o600)
		} catch {
			// Continue so other read-only entries can still be normalized.
		}
	}
	try {
		chmodSync(current, 0o700)
	} catch {
		// The final removal call retains the authoritative error.
	}
}

export function removeGeneratedTree(target) {
	try {
		rmSync(target, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 })
	} catch (error) {
		if (!['EPERM', 'EACCES'].includes(error.code)) throw error
		makeTreeWritable(target)
		rmSync(target, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 })
	}
}

function sha256Buffer(buffer) {
	return createHash('sha256').update(buffer).digest('hex')
}

function sha256File(path) {
	return sha256Buffer(readFileSync(path))
}

function walkFiles(current, output, include, excludeBuildState = true) {
	for (const entry of readdirSync(current, { withFileTypes: true })) {
		if (entry.isDirectory()) {
			if (excludeBuildState && (['.cache', 'bin', 'node_modules'].includes(entry.name) || entry.name.startsWith('.build'))) continue
			walkFiles(join(current, entry.name), output, include, excludeBuildState)
		} else if (entry.isFile()) {
			const path = join(current, entry.name)
			if (include(path)) output.push(path)
		}
	}
}

export function hostSourceIdentity(hostRoot = HOST_ROOT) {
	const files = []
	walkFiles(hostRoot, files, path => extname(path).toLowerCase() === '.go')
	for (const path of [join(hostRoot, 'go.mod'), join(hostRoot, 'go.sum'), join(hostRoot, 'winres', 'winres.json')]) {
		if (existsSync(path)) files.push(path)
	}
	const webRoot = join(hostRoot, 'web')
	if (existsSync(webRoot)) walkFiles(webRoot, files, () => true)
	const webDist = join(hostRoot, 'internal', 'webui', 'dist')
	if (existsSync(webDist)) walkFiles(webDist, files, () => true, false)
	const uniqueFiles = [...new Set(files)]
	uniqueFiles.sort((left, right) => {
		const leftName = relative(hostRoot, left).replaceAll('\\', '/')
		const rightName = relative(hostRoot, right).replaceAll('\\', '/')
		return leftName < rightName ? -1 : leftName > rightName ? 1 : 0
	})
	const manifest = uniqueFiles.map(path => {
		const name = relative(hostRoot, path).replaceAll('\\', '/')
		return `${name}:${sha256File(path).toUpperCase()}\n`
	}).join('')
	return { sha256: sha256Buffer(Buffer.from(manifest, 'utf8')), files: uniqueFiles.length, manifest }
}

export function renderTable(columns, rows, options = {}) {
	return renderUnicodeTable(columns, rows, options)
}

function humanBytes(value) {
	const bytes = Number(value) || 0
	if (bytes < 1024) return `${bytes} B`
	return `${(bytes / 1024).toFixed(bytes < 10240 ? 2 : 1)} KiB`
}

function usageGauge(used, capacity, width = 16) {
	const ratio = capacity > 0 ? Math.max(0, Math.min(1, used / capacity)) : 0
	const filled = Math.round(ratio * width)
	return `${'█'.repeat(filled)}${'░'.repeat(width - filled)} ${(ratio * 100).toFixed(2)}%`
}

function shortHash(value) {
	const text = String(value || '')
	return text.length > 16 ? `${text.slice(0, 16)}…` : text
}

function logger(options, productTitle) {
	const chalk = createChalk(options)
	let activeStage = null
	const elapsed = started => `${((Date.now() - started) / 1000).toFixed(2)}s`
	const closeStage = () => {
		if (!activeStage) return ''
		const duration = elapsed(activeStage.started)
		activeStage = null
		return duration
	}
	return {
		banner() {
			const banner = renderUnicodeBanner([
				[chalk.bold.whiteBright(`🚀  ${productTitle} Build & Device Toolchain`)],
				[chalk.gray('AVR firmware • Go host • virtual board')]
			].map(row => row[0]), { chalk, width: 50 })
			process.stdout.write(`\n${banner}\n`)
		},
		stage(icon, message) {
			if (activeStage) process.stdout.write(`${chalk.gray(`   ↳ ${activeStage.name}: ${closeStage()}`)}\n`)
			activeStage = { name: message, started: Date.now() }
			process.stdout.write(`\n${chalk.bold.cyanBright(`${icon}  ${message}`)}\n`)
		},
		success(message) {
			const duration = closeStage()
			process.stdout.write(`${chalk.bold.greenBright(`✅  ${message}`)}${duration ? chalk.gray(`  (${duration})`) : ''}\n`)
		},
		warning(message) { process.stdout.write(`${chalk.bold.yellow(`⚠️  ${message}`)}\n`) },
		detail(message) { process.stdout.write(`${chalk.gray(message)}\n`) },
		table(title, columns, rows) {
			process.stdout.write(`\n${chalk.bold.whiteBright(title)}\n`)
			process.stdout.write(`${renderTable(columns, rows, { chalk })}\n`)
		}
	}
}

export function removeGeneratedWinResources(controllerSource = join(HOST_ROOT, 'cmd', 'controller')) {
	if (!existsSync(controllerSource)) return
	for (const entry of readdirSync(controllerSource, { withFileTypes: true })) {
		if (entry.isFile() && /^rsrc_.*\.syso$/i.test(entry.name)) rmSync(join(controllerSource, entry.name), { force: true })
	}
}

function cleanGenerated(log) {
	for (const path of [BUILD_ROOT, CANONICAL_HOST_OUTPUT, ...LEGACY_HOST_OUTPUTS]) {
		assertGeneratedPath(PROJECT_ROOT, path)
		if (existsSync(path)) {
			removeGeneratedTree(path)
			log.success(`Removed ${relative(PROJECT_ROOT, path)}`)
		}
	}
	removeGeneratedWinResources()
}

function removeLegacyHostOutputs(log) {
	for (const path of LEGACY_HOST_OUTPUTS) {
		assertGeneratedPath(PROJECT_ROOT, path)
		if (!existsSync(path)) continue
		removeGeneratedTree(path)
		log.success(`Removed legacy host artifact ${relative(PROJECT_ROOT, path)}`)
	}
}

function copyProjectNotices(destination) {
	const project = join(destination, 'project')
	mkdirSync(project, { recursive: true })
	for (const name of ['LICENSE', 'THIRD_PARTY_NOTICES.md', 'REUSE.toml']) {
		const source = join(PROJECT_ROOT, name)
		if (existsSync(source)) copyFileSync(source, join(project, name))
	}
	const licenses = join(PROJECT_ROOT, 'LICENSES')
	if (existsSync(licenses)) cpSync(licenses, join(project, 'LICENSES'), { recursive: true })
}

export function collectWebNotices(destination, webRoot = WEB_ROOT) {
	const lockPath = join(webRoot, 'package-lock.json')
	let lock
	try {
		lock = JSON.parse(readFileSync(lockPath, 'utf8'))
	} catch (error) {
		throw new BuildError(`decode embedded web package lock for notices: ${error.message}`)
	}
	let packages = 0
	for (const [packagePath, metadata] of Object.entries(lock.packages || {}).sort(([left], [right]) => left.localeCompare(right))) {
		if (!packagePath.startsWith('node_modules/') || metadata?.dev === true) continue
		const segments = packagePath.split('/')
		if (segments.includes('..')) throw new BuildError(`unsafe package path in embedded web lock: ${packagePath}`)
		const directory = join(webRoot, ...segments)
		if (!existsSync(directory)) throw new BuildError(`locked web dependency is not installed: ${packagePath}`)
		const notices = readdirSync(directory, { withFileTypes: true }).filter(entry =>
			entry.isFile() && /^(LICENSE|COPYING|NOTICE)/i.test(entry.name)
		)
		if (notices.length === 0) throw new BuildError(`bundled web dependency has no package notice: ${packagePath}`)
		const packageJSON = join(directory, 'package.json')
		let packageName = packagePath.slice('node_modules/'.length)
		let version = metadata?.version || 'locked'
		if (existsSync(packageJSON)) {
			try {
				const value = JSON.parse(readFileSync(packageJSON, 'utf8'))
				packageName = value.name || packageName
				version = value.version || version
			} catch (error) {
				throw new BuildError(`decode ${packagePath}/package.json for notices: ${error.message}`)
			}
		}
		const safe = `${packageName}@${version}`.replace(/[\\/:*?"<>|]/g, '_')
		const target = join(destination, 'web', safe)
		mkdirSync(target, { recursive: true })
		for (const notice of notices) copyFileSync(join(directory, notice.name), join(target, notice.name))
		packages += 1
	}
	return packages
}

function collectModuleNotices(go, stage, env, options) {
	const root = join(stage, 'licenses')
	removeGeneratedTree(root)
	mkdirSync(root, { recursive: true })
	copyProjectNotices(root)
	let goModules = 0
	const result = run(go, ['list', '-m', '-f', '{{.Path}}|{{.Version}}|{{.Dir}}', 'all'], {
		cwd: HOST_ROOT, env, capture: true, verbose: options.verbose
	})
	for (const line of result.stdout.split(/\r?\n/)) {
		if (!line.trim()) continue
		const [modulePath, version, directory] = line.split('|')
		if (!modulePath || !directory || !existsSync(directory)) continue
		const notices = readdirSync(directory, { withFileTypes: true }).filter(entry =>
			entry.isFile() && /^(LICENSE|COPYING|NOTICE)/i.test(entry.name)
		)
		if (notices.length === 0) continue
		const safe = `${modulePath}@${version || 'local'}`.replace(/[\\/:*?"<>|]/g, '_')
		const target = join(root, 'modules', safe)
		mkdirSync(target, { recursive: true })
		for (const notice of notices) copyFileSync(join(directory, notice.name), join(target, notice.name))
		goModules += 1
	}
	return { goModules, webPackages: collectWebNotices(root) }
}

function compilerVersion(text) {
	const match = text.match(/\d+(?:\.\d+)+/)
	return match ? match[0] : '0.0'
}

function selectCCompiler(env, goArch, options) {
	if (env.CC) return { command: env.CC, env }
	if (process.platform !== 'win32') {
		for (const name of ['cc', 'gcc', 'clang']) {
			const candidates = allExecutables(name, env)
			if (candidates.length) return { command: candidates[0], env: { ...env, CC: candidates[0] } }
		}
		throw new BuildError('no C compiler was found for the requested C ABI package')
	}
	const targetPattern = goArch === 'amd64'
		? /^x86_64-.*(mingw|windows-gnu)/i
		: goArch === '386'
			? /^i[3-6]86-.*(mingw|windows-gnu)/i
			: goArch === 'arm64'
				? /^(aarch64|arm64)-.*(mingw|windows-gnu)/i
				: /(mingw|windows-gnu)/i
	const names = goArch === 'amd64'
		? ['x86_64-w64-mingw32-gcc', 'gcc', 'clang', 'cc']
		: goArch === '386'
			? ['i686-w64-mingw32-gcc', 'gcc', 'clang', 'cc']
			: ['gcc', 'clang', 'cc']
	const compatible = []
	for (const name of names) {
		for (const candidate of allExecutables(name, env)) {
			const target = spawnSync(candidate, ['-dumpmachine'], { encoding: 'utf8', env, shell: false, windowsHide: true, timeout: 2500 })
			if (target.status !== 0 || !targetPattern.test((target.stdout || '').trim())) continue
			const version = spawnSync(candidate, ['-dumpfullversion'], { encoding: 'utf8', env, shell: false, windowsHide: true, timeout: 2500 })
			compatible.push({ candidate, version: compilerVersion(version.stdout || '') })
		}
	}
	compatible.sort((left, right) => compareVersion(right.version, left.version))
	if (compatible.length === 0) {
		throw new BuildError(`no native Windows ${goArch} MinGW-w64 compiler was found; Git/MSYS-target compilers are intentionally rejected`)
	}
	const selected = compatible[0].candidate
	const selectedEnv = {
		...env,
		PATH: `${dirname(selected)}${delimiter}${env.PATH || ''}`,
		// Go invokes CC itself. Keep the selected directory first on PATH so a
		// compiler installed below a path containing spaces stays one argv item.
		CC: basename(selected)
	}
	if (options.verbose) process.stdout.write(`C compiler: ${selected} (${compatible[0].version})\n`)
	return { command: selected, env: selectedEnv }
}

export function windowsSmokeSource() {
	return `#include <windows.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
typedef char* (__cdecl *invoke_fn)(char*);
typedef void (__cdecl *free_fn)(char*);
int main(void) {
  HMODULE module = LoadLibraryA("pccontroller.dll");
  if (!module) return 10;
  invoke_fn invoke = (invoke_fn)GetProcAddress(module, "PCControllerInvoke");
  free_fn release = (free_fn)GetProcAddress(module, "PCControllerFree");
  if (!invoke || !release) return 11;
  char create[] = "{\\\"operation\\\":\\\"create\\\"}";
  char *response = invoke(create);
  if (!response || !strstr(response, "\\\"ok\\\":true")) return 12;
  char *handle_field = strstr(response, "\\\"handle\\\":");
  if (!handle_field) return 13;
  unsigned long long handle = strtoull(handle_field + 9, NULL, 10);
  release(response);

  char request[160];
  snprintf(request, sizeof(request),
           "{\\\"operation\\\":\\\"build-smoke-invalid\\\",\\\"handle\\\":%llu}",
           handle);
  response = invoke(request);
  if (!response || !strstr(response, "unknown operation build-smoke-invalid")) return 14;
  release(response);

  snprintf(request, sizeof(request),
           "{\\\"operation\\\":\\\"destroy\\\",\\\"handle\\\":%llu}", handle);
  response = invoke(request);
  if (!response || !strstr(response, "\\\"destroyed\\\":true")) return 15;
  release(response);
  // The Go shared runtime owns process-lifetime state; leave it loaded.
  return 0;
}
`
}

export function unixSmokeSource(libraryName) {
	return `#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
typedef char* (*invoke_fn)(char*);
typedef void (*free_fn)(char*);
int main(void) {
  void *module = dlopen("./${libraryName}", RTLD_NOW | RTLD_LOCAL);
  if (!module) {
    fprintf(stderr, "dlopen failed: %s\\n", dlerror());
    return 10;
  }
  invoke_fn invoke = (invoke_fn)dlsym(module, "PCControllerInvoke");
  free_fn release = (free_fn)dlsym(module, "PCControllerFree");
  if (!invoke || !release) return 11;
  char create[] = "{\\"operation\\":\\"create\\"}";
  char *response = invoke(create);
  if (!response || !strstr(response, "\\"ok\\":true")) return 12;
  char *handle_field = strstr(response, "\\"handle\\":");
  if (!handle_field) return 13;
  unsigned long long handle = strtoull(handle_field + 9, NULL, 10);
  release(response);

  char request[160];
  snprintf(request, sizeof(request),
           "{\\"operation\\":\\"build-smoke-invalid\\",\\"handle\\":%llu}",
           handle);
  response = invoke(request);
  if (!response || !strstr(response, "unknown operation build-smoke-invalid")) return 14;
  release(response);

  snprintf(request, sizeof(request),
           "{\\"operation\\":\\"destroy\\",\\"handle\\":%llu}", handle);
  response = invoke(request);
  if (!response || !strstr(response, "\\"destroyed\\":true")) return 15;
  release(response);
  // The Go c-shared runtime owns process-lifetime state; do not dlclose it.
  return 0;
}
`
}

function buildSharedLibrary(go, stage, env, goArch, options, log) {
	const extension = process.platform === 'win32' ? '.dll' : process.platform === 'darwin' ? '.dylib' : '.so'
	const output = join(stage, `pccontroller${extension}`)
	const compiler = selectCCompiler(env, goArch, options)
	log.detail(`C compiler: ${compiler.command}`)
	run(go, [
		'build', '-buildvcs=false', '-trimpath', '-tags', 'controllerlib',
		'-buildmode=c-shared', '-o', output, './cmd/controllerlib'
	], { cwd: HOST_ROOT, env: { ...compiler.env, CGO_ENABLED: '1' }, verbose: options.verbose })
	const header = join(stage, 'pccontroller.h')
	if (!existsSync(output) || !existsSync(header)) throw new BuildError('C ABI build did not produce both library and header')
	const headerText = readFileSync(header, 'utf8')
	for (const symbol of ['PCControllerInvoke', 'PCControllerFree']) {
		if (!headerText.includes(symbol)) throw new BuildError(`C ABI header is missing ${symbol}`)
	}
	if (process.platform === 'win32') {
		const smokeSource = join(stage, 'pccontroller-smoke.c')
		const smoke = join(stage, 'pccontroller-smoke.exe')
		writeFileSync(smokeSource, windowsSmokeSource(), 'utf8')
		run(compiler.command, [smokeSource, '-o', smoke], { cwd: stage, env: compiler.env, verbose: options.verbose })
		run(smoke, [], { cwd: stage, env: compiler.env, verbose: options.verbose })
		rmSync(smokeSource, { force: true })
		rmSync(smoke, { force: true })
	} else {
		const smokeSource = join(stage, 'pccontroller-smoke.c')
		const smoke = join(stage, 'pccontroller-smoke')
		writeFileSync(smokeSource, unixSmokeSource(basename(output)), 'utf8')
		const smokeArgs = [smokeSource, '-o', smoke]
		if (process.platform === 'linux') smokeArgs.push('-ldl')
		run(compiler.command, smokeArgs, { cwd: stage, env: compiler.env, verbose: options.verbose })
		run(smoke, [], { cwd: stage, env: compiler.env, verbose: options.verbose })
		rmSync(smokeSource, { force: true })
		rmSync(smoke, { force: true })
	}
	return [output, header]
}

function peSectionNames(path) {
	const data = readFileSync(path)
	if (data.length < 0x40 || data.readUInt16LE(0) !== 0x5A4D) throw new BuildError(`${path} is not a PE executable`)
	const pe = data.readUInt32LE(0x3C)
	if (pe + 24 > data.length || data.toString('ascii', pe, pe + 4) !== 'PE\0\0') throw new BuildError(`${path} has no PE header`)
	const sections = data.readUInt16LE(pe + 6)
	const optional = data.readUInt16LE(pe + 20)
	const table = pe + 24 + optional
	const names = []
	for (let index = 0; index < sections; index += 1) {
		const offset = table + index * 40
		if (offset + 40 > data.length) throw new BuildError(`${path} has a truncated PE section table`)
		names.push(data.toString('ascii', offset, offset + 8).replace(/\0.*$/, ''))
	}
	return names
}

function verifyWindowsResources(executable) {
	if (!peSectionNames(executable).includes('.rsrc')) throw new BuildError('controller.exe does not contain a Win32 .rsrc section')
	const binary = readFileSync(executable)
	const resources = JSON.parse(readFileSync(join(HOST_ROOT, 'winres', 'winres.json'), 'utf8'))
	const info = resources.RT_VERSION?.['#1']?.['0409']?.info?.['0409'] || {}
	for (const value of [info.ProductName, info.CompanyName].filter(Boolean)) {
		if (binary.indexOf(Buffer.from(value, 'utf16le')) < 0) throw new BuildError(`controller.exe resource data is missing ${value}`)
	}
}

function atomicWriteJSON(path, value) {
	const temporary = `${path}.tmp-${process.pid}`
	writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, 'utf8')
	try {
		renameSync(temporary, path)
	} catch {
		rmSync(path, { force: true })
		renameSync(temporary, path)
	}
}

function artifactRecord(path, base) {
	const info = statSync(path)
	return {
		path: relative(base, path).replaceAll('\\', '/'),
		bytes: info.size,
		sha256: sha256File(path)
	}
}

function installPackageEntries(stage, canonical, previous, rename) {
        mkdirSync(previous, { recursive: true })
        const saved = []
        const installed = []
        try {
                for (const name of readdirSync(canonical)) {
                        rename(join(canonical, name), join(previous, name))
                        saved.push(name)
                }
                for (const name of readdirSync(stage)) {
                        rename(join(stage, name), join(canonical, name))
                        installed.push(name)
                }
        } catch (error) {
                const rollbackErrors = []
                for (const name of installed.reverse()) {
                        try {
                                rename(join(canonical, name), join(stage, name))
                        } catch (rollback) {
                                rollbackErrors.push(`new ${name}: ${rollback.message}`)
                        }
                }
                for (const name of saved.reverse()) {
                        try {
                                rename(join(previous, name), join(canonical, name))
                        } catch (rollback) {
                                rollbackErrors.push(`previous ${name}: ${rollback.message}`)
                        }
                }
                const suffix = rollbackErrors.length === 0
                        ? ''
                        : `; rollback errors: ${rollbackErrors.join('; ')}`
                throw new BuildError(`entry-wise package transaction: ${error.message}${suffix}`)
        }
        removeGeneratedTree(stage)
        removeGeneratedTree(previous)
}

export function installPackage(stage, options = {}) {
        const root = options.root || PROJECT_ROOT
        const canonical = options.canonical || CANONICAL_HOST_OUTPUT
        const rename = options.rename || renameSync
        const previous = join(dirname(canonical), `.bin-previous-${process.pid}`)
        assertGeneratedPath(root, stage)
        assertGeneratedPath(root, canonical)
        assertGeneratedPath(root, previous)
        removeGeneratedTree(previous)
        let movedPrevious = false
        try {
                if (existsSync(canonical)) {
                        rename(canonical, previous)
                        movedPrevious = true
                }
                rename(stage, canonical)
        } catch (error) {
                if (!movedPrevious && existsSync(canonical) &&
                        ['EBUSY', 'EPERM', 'EACCES'].includes(error.code)) {
                        installPackageEntries(stage, canonical, previous, rename)
                        return
                }
                if (movedPrevious && !existsSync(canonical) && existsSync(previous)) {
                        rename(previous, canonical)
                }
                throw new BuildError(`atomically publish canonical host package: ${error.message}`)
        }
	removeGeneratedTree(previous)
}

function buildHost(options, identity, env, log) {
	const stage = join(PACKAGE_ROOT, `host-${process.pid}`)
	assertGeneratedPath(PROJECT_ROOT, stage)
	removeGeneratedTree(stage)
	mkdirSync(stage, { recursive: true })
	const webUI = buildWebUI(options, env, log)
	log.stage('🪪', 'Checking package-derived product identity and Win32 metadata')
	run(process.execPath, [PRODUCT_IDENTITY_GENERATOR, '--check'], {
		cwd: PROJECT_ROOT, env, verbose: options.verbose
	})
	const go = requireTool('go', env)
	const goEnv = { ...env, GOCACHE: env.GOCACHE || join(HOST_ROOT, '.cache', 'go-build') }
	removeGeneratedWinResources()

	log.stage('📦', 'Resolving Go modules')
	run(go, ['mod', 'download'], { cwd: HOST_ROOT, env: goEnv, verbose: options.verbose })
	if (options.tests) {
		log.stage('🧪', 'Running Go tests from stable project-owned binaries')
		const args = [
			STABLE_GO_TEST_RUNNER,
			'--module', HOST_ROOT,
			'--output', STABLE_GO_TEST_ROOT,
			'--go', go,
			...(options.retest ? ['--retest'] : [])
		]
		run(process.execPath, args, { cwd: PROJECT_ROOT, env: goEnv, verbose: options.verbose })
	} else log.warning('Go tests were explicitly skipped.')
	if (options.vet) {
		log.stage('🔎', 'Running Go vet')
		run(go, ['vet', './...'], { cwd: HOST_ROOT, env: goEnv, verbose: options.verbose })
	} else log.warning('Go vet was explicitly skipped.')

	const goArch = run(go, ['env', 'GOARCH'], { cwd: HOST_ROOT, env: goEnv, capture: true }).stdout.trim()
	let resourceGenerated = false
	let winres = ''
	if (process.platform === 'win32' && options.resources) {
		log.stage('🎨', 'Preparing Win32 icon, manifest, and version resources')
		winres = requireTool('go-winres', env)
		run(go, ['run', './winres/generate_icon.go', './winres/icon.png'], { cwd: HOST_ROOT, env: goEnv, verbose: options.verbose })
	} else if (process.platform === 'win32') log.warning('Win32 resource regeneration was explicitly skipped.')

	const before = hostSourceIdentity()
	const executableName = process.platform === 'win32' ? 'controller.exe' : 'controller'
	const executable = join(stage, executableName)
	const ldflags = `-s -w -X main.version=${identity.version} -X main.sourceHash=${before.sha256} -X main.buildTime=${identity.hostBuildTime}`
	log.stage('🖥️', `Building host ${identity.version} from ${before.sha256.slice(0, 12)}`)
	const executableCGO = process.platform === 'darwin' ? '1' : '0'
	run(go, ['build', '-buildvcs=false', '-trimpath', '-ldflags', ldflags, '-o', executable, './cmd/controller'], {
		// go.bug.st/serial uses Apple IOKit through CGO on macOS. Windows and
		// Linux retain the self-contained executable build.
		cwd: HOST_ROOT, env: { ...goEnv, CGO_ENABLED: executableCGO }, verbose: options.verbose
	})
	const after = hostSourceIdentity()
	if (after.sha256 !== before.sha256) throw new BuildError('Controller source changed during packaging; retry from a stable tree')
	if (process.platform === 'win32' && options.resources) {
		log.stage('🎨', 'Applying Win32 icon, manifest, and version resources')
		run(winres, ['patch', '--in', 'winres/winres.json', '--delete', '--no-backup', executable], {
			cwd: HOST_ROOT, env: goEnv, verbose: options.verbose
		})
		resourceGenerated = true
	}
	let versionOutput = run(executable, ['version'], { cwd: stage, env, capture: true }).stdout.trim()
	const expectedIdentity = [identity.version, `source-hash=${before.sha256}`, `built=${identity.hostBuildTime}`]
	for (const expected of expectedIdentity) {
		if (!versionOutput.includes(expected)) throw new BuildError(`controller identity check is missing ${expected}`)
	}
	if (process.platform === 'win32' && options.resources) verifyWindowsResources(executable)

	let upx = { enabled: false, tested: false, version: '' }
	if (process.platform === 'win32' && options.upx) {
		log.stage('📦', 'Compressing and testing controller.exe with UPX')
		const upxPath = requireTool('upx', env)
		const version = run(upxPath, ['--version'], { env, capture: true }).stdout.split(/\r?\n/)[0].trim()
		run(upxPath, ['--best', '--lzma', executable], { cwd: stage, env, verbose: options.verbose })
		run(upxPath, ['-t', executable], { cwd: stage, env, verbose: options.verbose })
		versionOutput = run(executable, ['version'], { cwd: stage, env, capture: true }).stdout.trim()
		for (const expected of expectedIdentity) {
			if (!versionOutput.includes(expected)) throw new BuildError(`packed controller identity check is missing ${expected}`)
		}
		upx = { enabled: true, tested: true, version }
	} else if (process.platform === 'win32') log.warning('UPX packaging was explicitly skipped.')

	let shared = []
	if (options.sharedLibrary) {
		log.stage('🧩', 'Building and smoke-testing the C ABI library')
		shared = buildSharedLibrary(go, stage, goEnv, goArch, options, log)
	} else log.warning('C ABI package was explicitly skipped.')

	log.stage('📜', 'Collecting project and dependency notices')
	const notices = collectModuleNotices(go, stage, goEnv, options)
	const artifacts = [executable, ...shared].map(path => artifactRecord(path, stage))
	const manifest = {
		format: HOST_MANIFEST_FORMAT,
		generatedUtc: identity.hostBuildTime,
		target: { platform: process.platform, architecture: goArch },
		identity: {
			version: identity.version,
			sourceSHA256: before.sha256,
			sourceFiles: before.files,
			buildTime: identity.hostBuildTime,
			packedFirmwareTimestamp: identity.packedTimestamp
		},
		validation: {
			webUI,
			notices,
			tests: options.tests ? 'passed' : 'skipped',
			vet: options.vet ? 'passed' : 'skipped',
			windowsResources: process.platform === 'win32' ? (resourceGenerated ? 'verified' : 'skipped') : 'not-applicable',
			upx,
			sharedLibrary: options.sharedLibrary ? 'smoke-passed' : 'skipped'
		},
		artifacts
	}
	atomicWriteJSON(join(stage, 'host-manifest.json'), manifest)
	log.stage('📤', 'Publishing the canonical host package')
	installPackage(stage)
	removeGeneratedTree(stage)
	removeLegacyHostOutputs(log)
	log.table('📦 Host package', [
		{ label: 'Artifact' },
		{ label: 'Size', align: 'right' },
		{ label: 'SHA-256' }
	], artifacts.map(artifact => [artifact.path, humanBytes(artifact.bytes), shortHash(artifact.sha256)]))
	log.table('🧾 Verified host identity', [
		{ label: 'Property' },
		{ label: 'Verified value' }
	], [
		['Version', identity.version],
		['Source SHA-256', before.sha256],
		['Build time', identity.hostBuildTime],
		['Win32 resources', manifest.validation.windowsResources],
		['UPX', upx.enabled ? `${upx.version} / ${upx.tested ? 'tested' : 'not tested'}` : 'disabled'],
		['C ABI', manifest.validation.sharedLibrary]
	])
	log.success(`Canonical host package: ${relative(PROJECT_ROOT, CANONICAL_HOST_OUTPUT)}`)
	return join(CANONICAL_HOST_OUTPUT, executableName)
}

function controllerCommand(options, controllerPath) {
	if (controllerPath) return { file: controllerPath, prefix: [], cwd: PROJECT_ROOT }
	const go = 'go'
	return { file: go, prefix: ['run', '-buildvcs=false', './cmd/controller'], cwd: HOST_ROOT }
}

function readFirmwareManifest() {
	const path = join(FIRMWARE_OUTPUT, 'firmware-manifest.json')
	if (!existsSync(path)) throw new BuildError(`Controller compile did not publish ${path}`)
	let manifest
	try { manifest = JSON.parse(readFileSync(path, 'utf8')) } catch (error) {
		throw new BuildError(`decode firmware manifest: ${error.message}`)
	}
	if (manifest.format !== FIRMWARE_MANIFEST_FORMAT) throw new BuildError(`unexpected firmware manifest format: ${manifest.format}`)
	if (!Array.isArray(manifest.artifacts) || !manifest.artifacts.some(artifact => artifact.role === 'application')) {
		throw new BuildError('firmware manifest has no canonical application artifact')
	}
	return manifest
}

function firmwareArtifact(manifest, role) {
	const artifact = manifest.artifacts.find(value => value.role === role)
	if (!artifact) throw new BuildError(`firmware manifest has no ${role} artifact`)
	const path = isAbsolute(artifact.path) ? artifact.path : resolve(PROJECT_ROOT, artifact.path)
	if (!existsSync(path) || sha256File(path).toLowerCase() !== String(artifact.sha256).toLowerCase()) {
		throw new BuildError(`${role} artifact does not match the Controller manifest: ${path}`)
	}
	return { ...artifact, absolutePath: path }
}

function compileFirmware(options, identity, env, controllerPath, log) {
	log.stage('🔧', 'Compiling AVR firmware through the Controller interface')
	const controller = controllerCommand(options, controllerPath)
	const args = [
		...controller.prefix,
		'program', '--method', 'compile', '--sketch', PROJECT_ROOT,
		'--output-dir', FIRMWARE_OUTPUT
	]
	run(controller.file, args, { cwd: controller.cwd, env, verbose: options.verbose })
	const manifest = readFirmwareManifest()
	if (String(manifest.source?.packedTimestamp).toUpperCase() !== identity.packedTimestamp) {
		throw new BuildError('firmware manifest packed timestamp differs from the frozen build plan')
	}
	log.table('⚙️ AVR target', [
		{ label: 'Property' },
		{ label: 'Resolved value' }
	], [
		['FQBN', manifest.target?.fqbn || 'MiniCore ATmega328P'],
		['MCU / clock', `${manifest.target?.mcu || 'atmega328p'} / ${Number(manifest.target?.clockHz || 0).toLocaleString('en-US')} Hz`],
		['Bootloader', `${manifest.target?.bootloader || 'Urboot/urclock'} @ ${manifest.target?.baud || '?'} baud`],
		['Source', `${manifest.source?.buildHash || shortHash(manifest.source?.sha256)} (${manifest.source?.files || '?'} files)`],
		['Packed build time', manifest.source?.buildTimestamp || identity.packedTimestamp]
	])
	log.table('💾 Firmware memory map', [
		{ label: 'Image' },
		{ label: 'Used', align: 'right' },
		{ label: 'Capacity', align: 'right' },
		{ label: 'Free', align: 'right' },
		{ label: 'Utilization' },
		{ label: 'SHA-256' }
	], manifest.artifacts.map(artifact => [
		artifact.role,
		humanBytes(artifact.dataBytes),
		humanBytes(artifact.capacityBytes),
		humanBytes(artifact.freeBytes),
		usageGauge(artifact.dataBytes, artifact.capacityBytes),
		shortHash(artifact.sha256)
	]))
	if (manifest.stackBudget) {
		log.table('🧠 SRAM safety budget', [
			{ label: 'Static', align: 'right' },
			{ label: 'Serial path', align: 'right' },
			{ label: 'RF ISR', align: 'right' },
			{ label: 'Peak', align: 'right' },
			{ label: 'Estimated free', align: 'right' },
			{ label: 'Required free', align: 'right' }
		], [[
			humanBytes(manifest.stackBudget.staticSramBytes),
			humanBytes(manifest.stackBudget.serialPathBytes),
			humanBytes(manifest.stackBudget.rfInterruptAllowanceBytes),
			humanBytes(manifest.stackBudget.estimatedPeakSramBytes),
			humanBytes(manifest.stackBudget.estimatedFreeSramBytes),
			humanBytes(manifest.stackBudget.minimumFreeSramBytes)
		]])
	}
	log.success(`Firmware manifest: ${relative(PROJECT_ROOT, join(FIRMWARE_OUTPUT, 'firmware-manifest.json'))}`)
	return manifest
}

function syncToolchain(options, env, controllerPath, log) {
	if (!controllerPath) throw new BuildError('Toolchain sync requires the freshly packaged Controller')
	log.stage('🌐', 'Synchronizing firmware indexes, cores, and libraries through Controller')
	const args = ['toolchain', 'sync']
	if (options.toolchainCLI) args.push('--cli', options.toolchainCLI)
	run(controllerPath, args, { cwd: PROJECT_ROOT, env, verbose: options.verbose })
	log.success('Controller-owned toolchain sync completed.')
}

function executeProgramming(options, env, controllerPath, manifest, log) {
	const controller = controllerCommand({ ...options, host: true }, controllerPath)
	if (options.installBootloader) {
		log.stage('🔥', `Explicit Urboot/fuse provisioning through ${options.programmer}`)
		run(controller.file, [
			...controller.prefix, 'program', '--method', 'usbasp', '--operation', 'install-bootloader',
			'--programmer', options.programmer, '--usbasp-troubleshooting'
		], { cwd: controller.cwd, env, verbose: options.verbose })
	}
	if (!options.upload) return
	const role = options.method === 'usbasp' ? 'flash+bootloader' : 'application'
	const artifact = firmwareArtifact(manifest, role)
	log.stage('⚡', `Explicit guarded ${options.method} programming`)
	const args = [
		...controller.prefix, 'program', '--method', options.method, '--operation', 'write-flash',
		'--hex', artifact.absolutePath
	]
	if (options.method === 'urclock') args.push('--device', options.device)
	else args.push('--programmer', options.programmer, '--usbasp-troubleshooting')
	if (options.allowIncompleteBackup) args.push('--allow-incomplete-backup')
	run(controller.file, args, { cwd: controller.cwd, env, verbose: options.verbose })
}

function printDryRun(plan, options, log) {
	log.banner()
	for (const action of plan.actions) {
		log.stage(action.hardware ? '🔒' : '🧩', `${action.stage}${action.hardware ? ' [EXPLICIT HARDWARE]' : ''}`)
		if (action.command) log.detail(`$ ${commandText(action.command.file, action.command.args)}`)
		else if (action.paths) for (const path of action.paths) log.detail(`generated path: ${path}`)
	}
	log.success('Dry-run complete; no subprocess ran, no file changed, and no device was opened.')
}

export async function main(argv = process.argv.slice(2), env = process.env) {
	assertNodeVersion()
	const options = parseArguments(argv, env)
	const productTitle = resolveProductTitle(env)
	if (options.help) {
		process.stdout.write(`${usage(productTitle)}\n`)
		return 0
	}
	const identity = resolveBuildIdentity(options, env)
	const plan = createPlan(options, identity)
	if (options.planJSON) {
		process.stdout.write(`${JSON.stringify(plan, null, 2)}\n`)
		return 0
	}
	const log = logger(options, productTitle)
	if (options.dryRun) {
		printDryRun(plan, options, log)
		return 0
	}
	const started = Date.now()
	const executionEnvironment = { ...identity.env }
	if (options.noColor) {
		for (const key of Object.keys(executionEnvironment)) {
			if (key.toLowerCase() === 'no_color') delete executionEnvironment[key]
		}
		executionEnvironment.NO_COLOR = '1'
	}
	const refreshed = refreshedEnvironment(executionEnvironment)
	log.banner()
	log.detail(`identity: version=${identity.version} host-time=${identity.hostBuildTime} firmware-stamp=0x${identity.packedTimestamp}`)
	if (options.clean) {
		log.stage('🧹', 'Cleaning generated output')
		cleanGenerated(log)
		if (options.cleanOnly) return 0
	}
	let controllerPath = ''
	if (options.host) controllerPath = buildHost(options, identity, refreshed, log)
	if (options.toolchainSync) syncToolchain(options, refreshed, controllerPath, log)
	let manifest = null
	if (options.firmware) manifest = compileFirmware(options, identity, refreshed, controllerPath, log)
	if (options.installBootloader || options.upload) executeProgramming(options, refreshed, controllerPath, manifest, log)
	log.success(`All selected operations completed in ${((Date.now() - started) / 1000).toFixed(1)}s.`)
	return 0
}

const isMain = process.argv[1] && resolve(process.argv[1]) === resolve(SCRIPT)
if (isMain) {
	main().catch(error => {
		const exitCode = error instanceof BuildError ? error.exitCode : 1
		process.stderr.write(`\n❌  ${error.stack || error.message || error}\n`)
		process.exitCode = exitCode
	})
}
