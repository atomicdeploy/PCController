import { readFileSync, statSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import { loadProjectEnv } from '../Build/env.mjs'

loadProjectEnv()

export const EXIT = Object.freeze({
	OK: 0,
	USAGE: 2,
	VALIDATION: 3,
	TOOL: 4,
	IO: 5,
	INTERRUPTED: 130
})

export const PROGRAMMING_METHODS = Object.freeze(['urclock', 'usbasp'])
export const PROGRAMMING_OPERATIONS = Object.freeze({
	backup: 'read-flash',
	upload: 'write-flash',
	verify: 'verify-flash',
	probe: 'probe',
	metadata: 'metadata',
	installBootloader: 'install-bootloader'
})

const TOOLCHAIN_POLICY_FORMAT = 'pccontroller-toolchain-policy/v1'
const TOOLCHAIN_POLICY_URL = new URL('../Controller/toolchain-profile.json', import.meta.url)

export class CommandPlanError extends Error {
	constructor(message, exitCode = EXIT.USAGE, options = {}) {
		super(message, options)
		this.name = 'CommandPlanError'
		this.exitCode = exitCode
	}
}

function positiveInteger(value, field, source) {
	if (!Number.isSafeInteger(value) || value <= 0) {
		throw new Error(`Toolchain policy ${source} requires target.${field} to be a positive integer`)
	}
	return value
}

export function parseToolchainPolicy(contents, source = 'toolchain policy') {
	let policy
	try {
		policy = JSON.parse(contents)
	} catch (error) {
		throw new Error(`Invalid JSON in toolchain policy ${source}: ${error.message}`, { cause: error })
	}
	if (!policy || typeof policy !== 'object' || Array.isArray(policy)) {
		throw new Error(`Toolchain policy ${source} must be a JSON object`)
	}
	if (policy.format !== TOOLCHAIN_POLICY_FORMAT) {
		throw new Error(
			`Toolchain policy ${source} uses unsupported format ${JSON.stringify(policy.format)}`
		)
	}
	if (typeof policy.fqbn !== 'string' || !policy.fqbn.trim()) {
		throw new Error(`Toolchain policy ${source} requires a non-empty fqbn string`)
	}
	if (typeof policy.name !== 'string' || !policy.name.trim()) {
		throw new Error(`Toolchain policy ${source} requires a non-empty name string`)
	}
	const target = policy.target
	if (!target || typeof target !== 'object' || Array.isArray(target)) {
		throw new Error(`Toolchain policy ${source} requires a target object`)
	}
	for (const field of ['mcu', 'bootloader']) {
		if (typeof target[field] !== 'string' || !target[field].trim()) {
			throw new Error(`Toolchain policy ${source} requires target.${field}`)
		}
	}
	const normalizedTarget = Object.freeze({
		mcu: target.mcu.trim(),
		clockHz: positiveInteger(target.clock_hz, 'clock_hz', source),
		bootloader: target.bootloader.trim(),
		baud: positiveInteger(target.baud, 'baud', source),
		applicationLimitBytes: positiveInteger(
			target.application_limit_bytes,
			'application_limit_bytes',
			source
		),
		flashBytes: positiveInteger(target.flash_bytes, 'flash_bytes', source),
		eepromBytes: positiveInteger(target.eeprom_bytes, 'eeprom_bytes', source)
	})
	if (normalizedTarget.applicationLimitBytes >= normalizedTarget.flashBytes) {
		throw new Error(
			`Toolchain policy ${source} requires the application limit below total flash capacity`
		)
	}
	return Object.freeze({
		...policy,
		name: policy.name.trim(),
		fqbn: policy.fqbn.trim(),
		target: normalizedTarget
	})
}

export function loadToolchainPolicy(source = TOOLCHAIN_POLICY_URL) {
	const label = source instanceof URL ? fileURLToPath(source) : String(source)
	let contents
	try {
		contents = readFileSync(source, 'utf8')
	} catch (error) {
		throw new Error(`Unable to read toolchain policy ${label}: ${error.message}`, { cause: error })
	}
	return parseToolchainPolicy(contents, label)
}

export const TOOLCHAIN_POLICY = loadToolchainPolicy()
export const BOARD = Object.freeze({
	profile: TOOLCHAIN_POLICY.name,
	fqbn: TOOLCHAIN_POLICY.fqbn,
	...TOOLCHAIN_POLICY.target
})

export function commandPlanPaths(projectRoot, platform = process.platform) {
	const root = resolve(projectRoot)
	const firmwareOutput = join(root, '.build', 'firmware')
	const controllerRoot = join(root, 'Tools', 'Controller')
	const controllerBin = join(controllerRoot, 'bin')
	return Object.freeze({
		projectRoot: root,
		controllerRoot,
		controllerBin,
		controller: join(
			controllerBin,
			platform === 'win32' ? 'controller.exe' : 'controller'
		),
		firmwareOutput,
		application: join(firmwareOutput, 'PCController.ino.hex'),
		completeFlash: join(firmwareOutput, 'PCController.ino.with_bootloader.hex'),
		defaultEEPROM: join(firmwareOutput, 'safe-default-eeprom.hex'),
		manifest: join(firmwareOutput, 'firmware-manifest.json')
	})
}

export function relativeCommandPlanPaths(projectRoot, platform = process.platform) {
	const paths = commandPlanPaths(projectRoot, platform)
	const portable = value => relative(paths.projectRoot, value).replaceAll('\\', '/')
	return Object.freeze({
		controller: portable(paths.controller),
		controllerBin: portable(paths.controllerBin),
		firmwareOutput: portable(paths.firmwareOutput),
		application: portable(paths.application),
		completeFlash: portable(paths.completeFlash),
		defaultEEPROM: portable(paths.defaultEEPROM),
		manifest: portable(paths.manifest)
	})
}

export function sourceControllerInvocation(projectRoot) {
	const paths = commandPlanPaths(projectRoot)
	return Object.freeze({
		file: 'go',
		prefix: Object.freeze(['run', '-buildvcs=false', './cmd/controller']),
		cwd: paths.controllerRoot
	})
}

export function canonicalControllerInvocation(projectRoot, platform = process.platform) {
	const paths = commandPlanPaths(projectRoot, platform)
	return Object.freeze({ file: paths.controller, prefix: Object.freeze([]), cwd: paths.projectRoot })
}

export function resolveCanonicalControllerInvocation(
	projectRoot,
	platform = process.platform,
	inspect = statSync
) {
	const invocation = canonicalControllerInvocation(projectRoot, platform)
	try {
		if (inspect(invocation.file).isFile()) return invocation
	} catch {
		// Report the exact platform-specific package route below.
	}
	throw new CommandPlanError(
		`Native controller executable was not found at ${invocation.file}; run build.cmd --host-only first`,
		EXIT.TOOL
	)
}

export function controllerCommand(invocation, args) {
	if (!invocation || typeof invocation.file !== 'string' || !invocation.file.trim()) {
		throw new CommandPlanError('Controller invocation requires an executable', EXIT.TOOL)
	}
	return {
		file: invocation.file,
		args: [...(invocation.prefix || []), ...args],
		cwd: invocation.cwd
	}
}

function requireValue(value, description) {
	if (typeof value !== 'string' || !value.trim()) {
		throw new CommandPlanError(`${description} is required`)
	}
	return value
}

export function createControllerProgramCommand({
	invocation,
	method,
	operation = '',
	device = '',
	appDevice = '',
	programmer = '',
	hex = '',
	output = '',
	sketch = '',
	outputDir = '',
	toolchainCLI = '',
	toolchainConfig = '',
	firmwareFeatures = [],
	dryRun = false,
	allowIncompleteBackup = false
}) {
	const normalizedMethod = String(method || '').toLowerCase()
	if (normalizedMethod === 'compile') {
		const args = [
			'program', '--method', 'compile',
			'--sketch', requireValue(sketch, 'compile sketch'),
			'--output-dir', requireValue(outputDir, 'compile output directory')
		]
		if (String(toolchainCLI).trim()) args.push('--toolchain-cli', String(toolchainCLI))
		if (String(toolchainConfig).trim()) args.push('--toolchain-config', String(toolchainConfig))
		for (const feature of normalizeFirmwareFeatures(firmwareFeatures)) {
			args.push('--firmware-feature', feature)
		}
		if (dryRun) args.push('--dry-run')
		return controllerCommand(invocation, args)
	}
	if (!PROGRAMMING_METHODS.includes(normalizedMethod)) {
		throw new CommandPlanError(
			`programming method ${JSON.stringify(method)} is unsupported; use ${PROGRAMMING_METHODS.join(' or ')}`
		)
	}
	const normalizedOperation = String(operation || '').toLowerCase()
	const knownOperations = Object.values(PROGRAMMING_OPERATIONS)
	if (!knownOperations.includes(normalizedOperation)) {
		throw new CommandPlanError(`programming operation ${JSON.stringify(operation)} is unsupported`)
	}
	if (normalizedOperation === PROGRAMMING_OPERATIONS.installBootloader && normalizedMethod !== 'usbasp') {
		throw new CommandPlanError('bootloader installation requires explicit usbasp method selection')
	}
	if (normalizedOperation === PROGRAMMING_OPERATIONS.metadata && normalizedMethod !== 'urclock') {
		throw new CommandPlanError('metadata is an Urclock-only operation')
	}

	const args = ['program', '--method', normalizedMethod]
	if (normalizedMethod === 'urclock') {
		args.push('--device', requireValue(device, `${normalizedOperation} serial device`))
	} else {
		if (appDevice.trim()) args.push('--app-device', appDevice)
		if (programmer.trim()) args.push('--programmer', programmer)
	}
	args.push('--operation', normalizedOperation)
	if ([PROGRAMMING_OPERATIONS.upload, PROGRAMMING_OPERATIONS.verify].includes(normalizedOperation)) {
		args.push('--hex', requireValue(hex, `${normalizedOperation} Intel HEX input`))
	}
	if (normalizedOperation === PROGRAMMING_OPERATIONS.backup) {
		args.push('--output', requireValue(output, 'read-flash output'))
	}
	if (allowIncompleteBackup) {
		if (normalizedOperation !== PROGRAMMING_OPERATIONS.upload) {
			throw new CommandPlanError('--allow-incomplete-backup is only valid with write-flash')
		}
		args.push('--allow-incomplete-backup')
	}
	if (dryRun) args.push('--dry-run')
	return controllerCommand(invocation, args)
}

// normalizeFirmwareFeatures only transports well-formed named feature tokens.
// The Controller is the single semantic authority and rejects unsupported
// names before any compiler or device action is attempted.
function normalizeFirmwareFeatures(features) {
	if (!Array.isArray(features)) throw new CommandPlanError('firmware features must be an array')
	return features.map(feature => {
		const normalized = String(feature || '').trim().toLowerCase()
		if (!/^[a-z0-9][a-z0-9-]*$/.test(normalized)) {
			throw new CommandPlanError(`invalid named firmware feature ${JSON.stringify(feature)}`)
		}
		return normalized
	})
}

export function programmingArtifact(paths, method) {
	if (method === 'usbasp') return paths.completeFlash
	if (method === 'urclock') return paths.application
	throw new CommandPlanError(`no canonical firmware artifact exists for method ${JSON.stringify(method)}`)
}
