#!/usr/bin/env node

import { createHash } from 'node:crypto'
import { spawn } from 'node:child_process'
import { promises as fs } from 'node:fs'
import {
	basename,
	dirname,
	extname,
	isAbsolute,
	join,
	relative,
	resolve
} from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import {
        createChalk,
        renderUnicodeBanner
} from '../Build/presentation.mjs'
import { resolveProductTitle } from '../Build/product-metadata.mjs'

export const EXIT = Object.freeze({
	OK: 0,
	USAGE: 2,
	VALIDATION: 3,
	TOOL: 4,
	IO: 5,
	INTERRUPTED: 130
})

export const BOARD = Object.freeze({
	fqbn: 'MiniCore:avr:328:bootloader=uart0,eeprom=keep,baudrate=115200,variant=modelP,BOD=2v7,LTO=Os_flto,clock=16MHz_external',
	mcu: 'atmega328p',
	clockHz: 16_000_000,
	bootloader: 'UART0 Urboot/urclock',
	baud: 115_200,
	applicationLimitBytes: 32_384,
	flashBytes: 32_768,
	eepromBytes: 1_024
})

// Packs local civil time as date<<16|time with two-second resolution.
export function packBuildTimestamp(value = new Date()) {
	if (!(value instanceof Date) || Number.isNaN(value.getTime())) {
		throw new TypeError('build timestamp requires a valid Date')
	}
	const year = value.getFullYear() - 2000
	if (year < 0 || year > 127) {
		throw new RangeError('firmware build year must be within 2000..2127')
	}
	const date = (year << 9) | ((value.getMonth() + 1) << 5) | value.getDate()
	const time = (value.getHours() << 11) |
		(value.getMinutes() << 5) |
		(value.getSeconds() >> 1)
	return ((date << 16) | time) >>> 0
}

function buildTimestampEnvironment(value = process.env.PCCONTROLLER_BUILD_TIMESTAMP) {
	if (value) {
		const match = String(value).trim().match(/^(?:0x)?([0-9a-f]{1,8})$/i)
		if (!match) {
			throw new FirmwareToolError(
				'PCCONTROLLER_BUILD_TIMESTAMP must be a packed hexadecimal u32',
				EXIT.USAGE
			)
		}
		return `0x${Number.parseInt(match[1], 16).toString(16).padStart(8, '0').toUpperCase()}`
	}
	return `0x${packBuildTimestamp().toString(16).padStart(8, '0').toUpperCase()}`
}

const SOURCE_EXTENSIONS = new Set([
	'.c', '.cc', '.cpp', '.cxx', '.h', '.hh', '.hpp', '.hxx', '.ino', '.S'
])
const SOURCE_ROOTS = Object.freeze([
	'PCController.ino',
	'PCControllerLocalLib.cpp',
	'PCControllerProject.cpp',
	'ProjectConfig.h',
	'LocalLib',
	'Project',
	'src'
])
const DEFAULT_POLL_MS = 250
const DEFAULT_DEBOUNCE_MS = 500
const MINIMUM_NODE = Object.freeze({ major: 20, minor: 19 })

class FirmwareToolError extends Error {
	constructor(message, exitCode = EXIT.TOOL, options = {}) {
		super(message, options)
		this.name = 'FirmwareToolError'
		this.exitCode = exitCode
	}
}

function assertNodeVersion() {
	const [major, minor] = process.versions.node.split('.').map(Number)
	if (major < MINIMUM_NODE.major ||
		(major === MINIMUM_NODE.major && minor < MINIMUM_NODE.minor)) {
		throw new FirmwareToolError(
			`Node.js ${MINIMUM_NODE.major}.${MINIMUM_NODE.minor} or newer is required; found ${process.versions.node}`,
			EXIT.TOOL
		)
	}
}

function optionValue(argv, index, inlineValue, option) {
	if (inlineValue !== undefined) return [inlineValue, index]
	if (index + 1 >= argv.length) {
		throw new FirmwareToolError(`${option} requires a value`, EXIT.USAGE)
	}
	return [argv[index + 1], index + 1]
}

function positiveInteger(value, option, { minimum = 1 } = {}) {
	if (!/^\d+$/.test(value)) {
		throw new FirmwareToolError(`${option} must be an integer`, EXIT.USAGE)
	}
	const parsed = Number(value)
	if (!Number.isSafeInteger(parsed) || parsed < minimum) {
		throw new FirmwareToolError(
			`${option} must be at least ${minimum}`,
			EXIT.USAGE
		)
	}
	return parsed
}

export function parseArguments(argv, env = process.env) {
	const config = {
		command: null,
		port: env.PCCONTROLLER_PORT || '',
		method: '',
		programmer: env.PCCONTROLLER_PROGRAMMER || 'usbasp',
		usbaspTroubleshooting: false,
		hexPath: '',
		outputPath: '',
		manifestPath: '',
		clean: false,
		verbose: false,
		quiet: false,
		noColor: Boolean(env.NO_COLOR),
		dryRun: false,
		uploadOnChange: false,
		once: false,
		pollMs: DEFAULT_POLL_MS,
		debounceMs: DEFAULT_DEBOUNCE_MS,
		help: false
	}
	const positional = []
	const commands = new Set([
		'build', 'upload', 'watch', 'check', 'manifest',
		'backup', 'verify', 'probe', 'metadata'
	])

	for (let index = 0; index < argv.length; index++) {
		const argument = argv[index]
		if (argument === '--') {
			positional.push(...argv.slice(index + 1))
			break
		}
		const equals = argument.indexOf('=')
		const name = equals >= 0 ? argument.slice(0, equals) : argument
		const inlineValue = equals >= 0 ? argument.slice(equals + 1) : undefined

		if (!name.startsWith('-')) {
			positional.push(argument)
			continue
		}

		switch (name) {
			case '-h':
			case '--help':
				config.help = true
				break
			case '--port': {
				const [value, next] = optionValue(argv, index, inlineValue, name)
				config.port = value
				index = next
				break
			}
			case '--method': {
				const [value, next] = optionValue(argv, index, inlineValue, name)
				config.method = value.toLowerCase()
				index = next
				break
			}
			case '--programmer': {
				const [value, next] = optionValue(argv, index, inlineValue, name)
				config.programmer = value
				index = next
				break
			}
			case '--usbasp-troubleshooting':
				config.usbaspTroubleshooting = true
				break
			case '--hex': {
				const [value, next] = optionValue(argv, index, inlineValue, name)
				config.hexPath = value
				index = next
				break
			}
			case '--output': {
				const [value, next] = optionValue(argv, index, inlineValue, name)
				config.outputPath = value
				index = next
				break
			}
			case '--manifest': {
				const [value, next] = optionValue(argv, index, inlineValue, name)
				config.manifestPath = value
				index = next
				break
			}
			case '--poll': {
				const [value, next] = optionValue(argv, index, inlineValue, name)
				config.pollMs = positiveInteger(value, name, { minimum: 50 })
				index = next
				break
			}
			case '--debounce': {
				const [value, next] = optionValue(argv, index, inlineValue, name)
				config.debounceMs = positiveInteger(value, name, { minimum: 50 })
				index = next
				break
			}
			case '--clean':
				config.clean = true
				break
			case '--verbose':
				config.verbose = true
				break
			case '--quiet':
				config.quiet = true
				break
			case '--no-color':
				config.noColor = true
				break
			case '--dry-run':
				config.dryRun = true
				break
			case '--upload':
				config.uploadOnChange = true
				break
			case '--once':
				config.once = true
				break
			default:
				throw new FirmwareToolError(`Unknown option: ${name}`, EXIT.USAGE)
		}
	}

	if (positional.length > 0) {
		config.command = positional.shift().toLowerCase()
	}
	config.command ||= 'build'
	if (!commands.has(config.command)) {
		throw new FirmwareToolError(
			`Unknown command: ${config.command}`,
			EXIT.USAGE
		)
	}
	if (positional.length > 0) {
		throw new FirmwareToolError(
			`Unexpected argument: ${positional[0]}`,
			EXIT.USAGE
		)
	}
	if (config.help) return config
	config.method ||= 'urclock'
	if (!['urclock', 'usbasp'].includes(config.method)) {
		throw new FirmwareToolError(
			'--method must be urclock or usbasp; direct Arduino upload is disabled',
			EXIT.USAGE
		)
	}

	if (config.command !== 'watch' && (config.uploadOnChange || config.once)) {
		throw new FirmwareToolError(
			'--upload and --once are watch-only options',
			EXIT.USAGE
		)
	}
	if (config.dryRun && config.command === 'watch') config.once = true
	const hardwareAction = ['upload', 'backup', 'verify', 'probe', 'metadata'].includes(config.command)
	const serialMethod = config.method === 'urclock'
	if (hardwareAction && serialMethod && !config.port.trim()) {
		throw new FirmwareToolError(
			`${config.command} requires --port (or PCCONTROLLER_PORT)`,
			EXIT.USAGE
		)
	}
	if (config.command === 'watch' && config.uploadOnChange &&
		serialMethod && !config.port.trim()) {
		throw new FirmwareToolError(
			'watch --upload requires --port (or PCCONTROLLER_PORT)',
			EXIT.USAGE
		)
	}
	if ((hardwareAction || (config.command === 'watch' && config.uploadOnChange)) &&
		config.method === 'usbasp' && !config.usbaspTroubleshooting) {
		throw new FirmwareToolError(
			'USBasp is hidden troubleshooting only; pass --usbasp-troubleshooting explicitly',
			EXIT.USAGE
		)
	}
	if (config.command === 'metadata' && config.method !== 'urclock') {
		throw new FirmwareToolError(
			'metadata is an Urclock-only operation',
			EXIT.USAGE
		)
	}
	if (config.command === 'backup' && !config.outputPath) {
		throw new FirmwareToolError('backup requires --output FILE', EXIT.USAGE)
	}
	if (config.outputPath && config.command !== 'backup') {
		throw new FirmwareToolError(
			'--output is only valid with backup',
			EXIT.USAGE
		)
	}
	if (config.hexPath && ['build', 'upload', 'watch'].includes(config.command)) {
		throw new FirmwareToolError(
			'--hex is only valid with check, manifest, or verify; programming always uses the freshly built canonical image',
			EXIT.USAGE
		)
	}
	return config
}

function usage(color = true, productTitle = resolveProductTitle()) {
        const chalk = createChalk({ noColor: !color, forceColor: color }, color)
        return `${chalk.bold.cyanBright(`${productTitle} AVR firmware studio`)}

${chalk.bold.yellowBright('Usage')}
  node Tools/Firmware/firmware.mjs [command] [options]
  firmware.cmd [command] [options]
  ./firmware.sh [command] [options]

${chalk.bold.yellowBright('Commands')}
  build       Build AVR firmware, validate Intel HEX, and write a SHA-256 manifest
  upload      Build, upload, and verify through MiniCore Urboot/urclock
  watch       Watch firmware sources and run stable, coalesced builds
  check       Validate generated (or --hex) Intel HEX and print size/hash details
  manifest    Validate artifacts and atomically refresh firmware-manifest.json
  backup      Read flash through Urclock into --output FILE
  verify      Verify --hex (or the normal application image) through Urclock
  probe       Probe the AVR signature through Urclock
  metadata    Request Urboot/Urclock metadata

${chalk.bold.yellowBright('Options')}
  --port PORT       Explicit serial port; required for every UART hardware action
  --method METHOD   urclock (default) or guarded usbasp; Arduino upload is disabled
  --programmer ID   ISP programmer ID used by the canonical build (default: usbasp)
  --usbasp-troubleshooting
                    Explicitly authorize hidden ISP diagnostics/programming
  --hex FILE        Override the application Intel HEX file
  --output FILE     Backup destination (backup only)
  --manifest FILE   Override manifest output
  --clean           Clean before building
  --verbose         Show commands and verbose compiler output
  --dry-run         Print the exact action without executing it or opening a port
  --no-color        Disable VT-100 styling
  --quiet           Suppress informational output
  -h, --help        Show this help

${chalk.bold.yellowBright('Watch options')}
  --upload          Program after each successful watched build (never implicit)
  --once            Run the initial watched action and exit
  --poll MS         Content scan interval (default: ${DEFAULT_POLL_MS})
  --debounce MS     Stable-source window (default: ${DEFAULT_DEBOUNCE_MS})

${chalk.dim(`Target: MiniCore 3.1.2+, ATmega328P, external 16 MHz, EEPROM retained,
BOD 2.7 V, UART0 Urboot/urclock at 115200 baud. Default command is build.
Exit codes: 0 success, 2 usage, 3 validation, 4 build/program, 5 local I/O,
130 interrupted.`)}`
}

function createLogger(config, productTitle = resolveProductTitle()) {
	const chalk = createChalk(config, process.stdout.isTTY)
	return {
		banner() {
			if (config.quiet) return
                        console.log('')
                        console.log(renderUnicodeBanner(
                                [chalk.bold(`⚡ ${productTitle} AVR firmware studio`)],
				{ chalk, width: 44, borderColor: 'magentaBright' }
			))
		},
		stage(icon, message) {
			if (!config.quiet) console.log(`\n${chalk.bold.cyanBright(`${icon}  ${message}`)}`)
		},
		info(message) {
			if (!config.quiet) console.log(message)
		},
		detail(message) {
			if (!config.quiet) console.log(chalk.dim(message))
		},
		success(message) {
			if (!config.quiet) console.log(chalk.bold.greenBright(`✅  ${message}`))
		},
		warn(message) {
			if (!config.quiet) console.warn(chalk.bold.yellowBright(`⚠️  ${message}`))
		},
		error(message) {
			console.error(chalk.bold.redBright(`❌  ${message}`))
		}
	}
}

function quoteArgument(value) {
	if (/^[A-Za-z0-9_./:\\=-]+$/.test(value)) return value
	return `"${value.replaceAll('"', '\\"')}"`
}

function commandText(plan) {
	return [plan.file, ...plan.args].map(quoteArgument).join(' ')
}

export async function createBuildPlan(config, projectRoot) {
	const packedTimestamp = buildTimestampEnvironment()
	const env = { PCCONTROLLER_BUILD_TIMESTAMP: packedTimestamp }
	const file = process.execPath
	const args = [
		join(projectRoot, 'Tools', 'Build', 'build.mjs'),
		'--firmware-only',
		'--build-timestamp', packedTimestamp
	]
	if (config.clean) args.push('--clean')
	if (config.verbose) args.push('--verbose')
	return { file, args, cwd: projectRoot, env }
}

async function findController(projectRoot) {
	const names = process.platform === 'win32'
		? ['controller.exe', 'controller']
		: ['controller', 'controller.exe']
	const directories = [join(projectRoot, 'Tools', 'Controller', 'bin')]
	for (const directory of directories) {
		for (const name of names) {
			const candidate = join(directory, name)
			try {
				const stat = await fs.stat(candidate)
				if (stat.isFile()) return candidate
			} catch {
				// Continue through executable suffixes in the canonical package.
			}
		}
	}
	throw new FirmwareToolError(
		'Native controller executable was not found; run build.cmd --host-only first',
		EXIT.TOOL
	)
}

export async function createProgramPlan(config, projectRoot, artifactPath, outputPath = '') {
	const controller = await findController(projectRoot)
	const args = ['program', '--method', config.method]
	if (config.method === 'urclock') args.push('--device', config.port)
	if (config.method === 'usbasp') {
		args.push(
			'--programmer', config.programmer,
			'--usbasp-troubleshooting'
		)
	}
	switch (config.command) {
		case 'backup':
			args.push('--operation', 'read-flash', '--output', outputPath || config.outputPath)
			break
		case 'upload':
			args.push('--operation', 'write-flash', '--hex', artifactPath)
			break
		case 'verify':
			args.push('--operation', 'verify-flash', '--hex', artifactPath)
			break
		case 'probe':
			args.push('--operation', 'probe')
			break
		case 'metadata':
			args.push('--operation', 'metadata')
			break
		default:
			throw new FirmwareToolError(
				`Unsupported programmer action: ${config.command}`,
				EXIT.USAGE
			)
	}
	if (config.dryRun) args.push('--dry-run')
	return { file: controller, args, cwd: projectRoot }
}

async function createUploadPlan(config, projectRoot) {
	const artifact = config.method === 'usbasp'
		? join(
			projectRoot,
			'.build',
			'firmware',
			'PCController.ino.with_bootloader.hex'
		)
		: defaultApplicationHex(projectRoot)
	return await createProgramPlan(
		{ ...config, command: 'upload' },
		projectRoot,
		artifact
	)
}

async function runChild(plan, config, logger) {
	logger.detail(`$ ${commandText(plan)}`)
	if (config.dryRun) {
		logger.success('Dry-run complete; no process was started and no port was opened.')
		return
	}
	await new Promise((accept, reject) => {
		const child = spawn(plan.file, plan.args, {
			cwd: plan.cwd,
			env: { ...process.env, ...plan.env },
			stdio: 'inherit',
			shell: false,
			windowsHide: false
		})
		child.once('error', error => reject(new FirmwareToolError(
			`Unable to start ${basename(plan.file)}: ${error.message}`,
			EXIT.TOOL,
			{ cause: error }
		)))
		child.once('exit', (code, signal) => {
			if (signal) {
				reject(new FirmwareToolError(
					`${basename(plan.file)} ended after signal ${signal}`,
					EXIT.INTERRUPTED
				))
			} else if (code !== 0) {
				reject(new FirmwareToolError(
					`${basename(plan.file)} exited with code ${code}`,
					EXIT.TOOL
				))
			} else {
				accept()
			}
		})
	})
}

function resolveFromProject(path, projectRoot) {
	if (!path) return ''
	return isAbsolute(path) ? resolve(path) : resolve(projectRoot, path)
}

function defaultApplicationHex(projectRoot) {
	return join(projectRoot, '.build', 'firmware', 'PCController.ino.hex')
}

function addRange(ranges, start, length) {
	if (length === 0) return
	ranges.push({ start, end: start + length - 1 })
}

function mergeRanges(ranges) {
	const sorted = ranges.toSorted((left, right) => left.start - right.start)
	const merged = []
	for (const range of sorted) {
		const previous = merged.at(-1)
		if (previous && range.start <= previous.end + 1) {
			previous.end = Math.max(previous.end, range.end)
		} else {
			merged.push({ ...range })
		}
	}
	return merged
}

export function parseIntelHex(text, label = 'Intel HEX') {
	const lines = text.replace(/^\uFEFF/, '').split(/\r?\n/)
	let base = 0
	let dataBytes = 0
	let eofSeen = false
	let records = 0
	const ranges = []

	for (let index = 0; index < lines.length; index++) {
		const lineNumber = index + 1
		const line = lines[index].trim()
		if (!line) continue
		if (eofSeen) {
			throw new FirmwareToolError(
				`${label}:${lineNumber}: data appears after EOF`,
				EXIT.VALIDATION
			)
		}
		if (!/^:[0-9A-Fa-f]+$/.test(line) || (line.length - 1) % 2 !== 0) {
			throw new FirmwareToolError(
				`${label}:${lineNumber}: malformed Intel HEX record`,
				EXIT.VALIDATION
			)
		}
		const bytes = Buffer.from(line.slice(1), 'hex')
		if (bytes.length < 5 || bytes.length !== bytes[0] + 5) {
			throw new FirmwareToolError(
				`${label}:${lineNumber}: record length mismatch`,
				EXIT.VALIDATION
			)
		}
		let checksum = 0
		for (const byte of bytes) checksum = (checksum + byte) & 0xFF
		if (checksum !== 0) {
			throw new FirmwareToolError(
				`${label}:${lineNumber}: checksum mismatch`,
				EXIT.VALIDATION
			)
		}

		const length = bytes[0]
		const address = (bytes[1] << 8) | bytes[2]
		const type = bytes[3]
		records++
		switch (type) {
			case 0x00: {
				const absolute = base + address
				if (absolute + length > 0x1_0000_0000) {
					throw new FirmwareToolError(
						`${label}:${lineNumber}: address exceeds 32-bit Intel HEX range`,
						EXIT.VALIDATION
					)
				}
				dataBytes += length
				addRange(ranges, absolute, length)
				break
			}
			case 0x01:
				if (length !== 0 || address !== 0) {
					throw new FirmwareToolError(
						`${label}:${lineNumber}: invalid EOF record`,
						EXIT.VALIDATION
					)
				}
				eofSeen = true
				break
			case 0x02:
				if (length !== 2 || address !== 0) {
					throw new FirmwareToolError(
						`${label}:${lineNumber}: invalid extended-segment record`,
						EXIT.VALIDATION
					)
				}
				base = bytes.readUInt16BE(4) << 4
				break
			case 0x03:
			case 0x05:
				if (length !== 4 || address !== 0) {
					throw new FirmwareToolError(
						`${label}:${lineNumber}: invalid start-address record`,
						EXIT.VALIDATION
					)
				}
				break
			case 0x04:
				if (length !== 2 || address !== 0) {
					throw new FirmwareToolError(
						`${label}:${lineNumber}: invalid extended-linear record`,
						EXIT.VALIDATION
					)
				}
				base = bytes.readUInt16BE(4) * 0x1_0000
				break
			default:
				throw new FirmwareToolError(
					`${label}:${lineNumber}: unsupported record type 0x${type.toString(16).padStart(2, '0')}`,
					EXIT.VALIDATION
				)
		}
	}
	if (!eofSeen) {
		throw new FirmwareToolError(
			`${label}: missing EOF record`,
			EXIT.VALIDATION
		)
	}
	const merged = mergeRanges(ranges)
	return {
		records,
		dataBytes,
		ranges: merged,
		startAddress: merged.length ? merged[0].start : null,
		endAddress: merged.length ? merged.at(-1).end : null
	}
}

async function sha256File(path) {
	const content = await fs.readFile(path)
	return {
		content,
		sha256: createHash('sha256').update(content).digest('hex')
	}
}

function artifactRole(path) {
	const name = basename(path).toLowerCase()
	if (name.endsWith('.eep')) return 'eeprom'
	if (name.includes('with_bootloader')) return 'flash+bootloader'
	return 'application'
}

async function inspectArtifact(path, projectRoot) {
	let file
	try {
		file = await sha256File(path)
	} catch (error) {
		throw new FirmwareToolError(
			`Unable to read firmware artifact "${path}": ${error.message}`,
			error.code === 'ENOENT' ? EXIT.VALIDATION : EXIT.IO,
			{ cause: error }
		)
	}
	const intelHex = parseIntelHex(file.content.toString('utf8'), basename(path))
	const role = artifactRole(path)
	if (role === 'application' &&
		intelHex.endAddress !== null &&
		intelHex.endAddress >= BOARD.applicationLimitBytes) {
		throw new FirmwareToolError(
			`${basename(path)} reaches address 0x${intelHex.endAddress.toString(16)}, beyond the ${BOARD.applicationLimitBytes}-byte application area`,
			EXIT.VALIDATION
		)
	}
	if (role === 'flash+bootloader' &&
		intelHex.endAddress !== null &&
		intelHex.endAddress >= BOARD.flashBytes) {
		throw new FirmwareToolError(
			`${basename(path)} exceeds the ATmega328P flash range`,
			EXIT.VALIDATION
		)
	}
	const displayPath = relative(projectRoot, path)
	const capacityBytes = role === 'application'
		? BOARD.applicationLimitBytes
		: role === 'flash+bootloader'
			? BOARD.flashBytes
			: BOARD.eepromBytes
	return {
		path: displayPath.startsWith('..') ? path : displayPath.replaceAll('\\', '/'),
		role,
		containerBytes: file.content.length,
		sha256: file.sha256,
		capacityBytes,
		freeBytes: capacityBytes - intelHex.dataBytes,
		usagePercent: Number(
			((intelHex.dataBytes / capacityBytes) * 100).toFixed(2)
		),
		...intelHex
	}
}

async function collectSourceFiles(projectRoot) {
	const files = []
	const visit = async path => {
		let stat
		try {
			stat = await fs.lstat(path)
		} catch (error) {
			if (error.code === 'ENOENT') return
			throw error
		}
		if (stat.isSymbolicLink()) return
		if (stat.isFile()) {
			if (SOURCE_EXTENSIONS.has(extname(path))) files.push(path)
			return
		}
		if (!stat.isDirectory()) return
		const entries = await fs.readdir(path, { withFileTypes: true })
		for (const entry of entries) {
			await visit(join(path, entry.name))
		}
	}
	for (const sourceRoot of SOURCE_ROOTS) {
		await visit(join(projectRoot, sourceRoot))
	}
	return files.toSorted((left, right) =>
		relative(projectRoot, left).localeCompare(relative(projectRoot, right), 'en')
	)
}

export async function sourceDigest(projectRoot) {
	const files = await collectSourceFiles(projectRoot)
	if (files.length === 0) {
		throw new FirmwareToolError(
			'No firmware source files were found',
			EXIT.VALIDATION
		)
	}
	const hash = createHash('sha256')
	for (const path of files) {
		const name = relative(projectRoot, path).replaceAll('\\', '/')
		hash.update(name)
		hash.update('\0')
		hash.update(await fs.readFile(path))
		hash.update('\0')
	}
	return { sha256: hash.digest('hex'), files: files.length }
}

async function discoverArtifacts(config, projectRoot) {
	if (config.hexPath) return [resolveFromProject(config.hexPath, projectRoot)]
	const output = join(projectRoot, '.build', 'firmware')
	let entries
	try {
		entries = await fs.readdir(output, { withFileTypes: true })
	} catch (error) {
		if (error.code === 'ENOENT') {
			throw new FirmwareToolError(
				'No firmware output exists; run the build command first',
				EXIT.VALIDATION
			)
		}
		throw error
	}
	return entries
		.filter(entry => entry.isFile() &&
			(entry.name.endsWith('.hex') || entry.name.endsWith('.eep')))
		.map(entry => join(output, entry.name))
		.toSorted()
}

async function inspectArtifacts(config, projectRoot, logger) {
	const paths = await discoverArtifacts(config, projectRoot)
	if (paths.length === 0) {
		throw new FirmwareToolError(
			'No Intel HEX firmware artifacts were found',
			EXIT.VALIDATION
		)
	}
	const artifacts = []
	for (const path of paths) {
		const artifact = await inspectArtifact(path, projectRoot)
		artifacts.push(artifact)
		const range = artifact.startAddress === null
			? 'empty'
			: `0x${artifact.startAddress.toString(16).padStart(4, '0')}–0x${artifact.endAddress.toString(16).padStart(4, '0')}`
		logger.info(
			`  ${basename(path)}  ${artifact.dataBytes}/${artifact.capacityBytes} bytes  ${artifact.usagePercent.toFixed(2)}%  ${artifact.freeBytes} free  ${range}`
		)
		logger.detail(`    SHA256 ${artifact.sha256}`)
	}
	return artifacts
}

async function writeManifest(config, projectRoot, artifacts, source, logger) {
	const path = resolveFromProject(
		config.manifestPath ||
			join('.build', 'firmware', 'firmware-manifest.json'),
		projectRoot
	)
	let prior = null
	try {
		prior = JSON.parse(await fs.readFile(path, 'utf8'))
	} catch (error) {
		if (error.code !== 'ENOENT' && !(error instanceof SyntaxError)) throw error
	}
	const identityMatches = prior?.format === 'pccontroller-avr-firmware-manifest/v1' &&
		Array.isArray(prior.artifacts) &&
		prior.artifacts.length === artifacts.length &&
		artifacts.every(artifact => prior.artifacts.some(previous =>
			String(previous.path).replaceAll('\\', '/') === String(artifact.path).replaceAll('\\', '/') &&
			String(previous.sha256).toLowerCase() === artifact.sha256.toLowerCase()
		))
	const manifest = {
		format: 'pccontroller-avr-firmware-manifest/v1',
		generatedUtc: identityMatches && prior.generatedUtc
			? prior.generatedUtc
			: new Date().toISOString(),
		target: identityMatches ? { ...BOARD, ...prior.target } : BOARD,
		// Preserve the Controller compiler's build hash and packed timestamp when
		// these are exactly the same bytes. The studio adds validation; it must
		// not replace the compiler-owned deterministic identity with wall time.
		source: identityMatches ? { ...source, ...prior.source } : source,
		artifacts
	}
	await fs.mkdir(dirname(path), { recursive: true })
	const temporary = `${path}.${process.pid}.tmp`
	try {
		await fs.writeFile(temporary, `${JSON.stringify(manifest, null, 2)}\n`, {
			encoding: 'utf8',
			flag: 'wx'
		})
		await fs.rename(temporary, path)
	} catch (error) {
		try {
			await fs.rm(temporary, { force: true })
		} catch {
			// Preserve the original atomic-write failure.
		}
		throw new FirmwareToolError(
			`Unable to write manifest "${path}": ${error.message}`,
			EXIT.IO,
			{ cause: error }
		)
	}
	logger.success(`Manifest written atomically: ${relative(projectRoot, path)}`)
	return manifest
}

async function validateAndManifest(config, projectRoot, logger, write) {
	logger.stage('🔍', 'Validating Intel HEX records and SHA-256 hashes')
	const artifacts = await inspectArtifacts(config, projectRoot, logger)
	const source = await sourceDigest(projectRoot)
	logger.detail(`Firmware source SHA256 ${source.sha256} (${source.files} files)`)
	if (write) {
		await writeManifest(config, projectRoot, artifacts, source, logger)
	}
	logger.success(`${artifacts.length} firmware artifact(s) validated.`)
	return artifacts
}

export function assertProgrammingImage(artifacts, method) {
	if (method === 'usbasp') {
		const merged = artifacts.find(
			artifact => artifact.role === 'flash+bootloader'
		)
		if (!merged) {
			throw new FirmwareToolError(
				'USBasp programming requires the canonical merged application + Urboot image',
				EXIT.VALIDATION
			)
		}
		const completeBootloader = merged.ranges.some(
			range =>
				range.start <= BOARD.applicationLimitBytes &&
				range.end === BOARD.flashBytes - 1
		)
		if (!completeBootloader) {
			throw new FirmwareToolError(
				'The merged ISP image does not contain the complete Urboot flash region',
				EXIT.VALIDATION
			)
		}
		return merged
	}

	const application = artifacts.find(
		artifact => artifact.role === 'application'
	)
	if (!application || application.dataBytes === 0) {
		throw new FirmwareToolError(
			'Serial programming requires a non-empty canonical application image',
			EXIT.VALIDATION
		)
	}
	return application
}

export async function runValidatedProgrammingStages({
	build,
	validate,
	program
}) {
	await build()
	const validation = await validate()
	return await program(validation)
}

async function runBuild(config, projectRoot, logger) {
	const plan = await createBuildPlan(config, projectRoot)
	const programming = config.command === 'upload' ||
		(config.command === 'watch' && config.uploadOnChange)
	logger.stage('🔧', programming
		? `Building AVR firmware before ${config.method} programming`
		: 'Building AVR firmware'
	)

	if (!programming) {
		await runChild(plan, config, logger)
		if (!config.dryRun) {
			await validateAndManifest(config, projectRoot, logger, true)
		}
		return
	}

	if (config.dryRun) {
		await runChild(plan, config, logger)
		logger.stage(
			'🔍',
			'Strict Intel HEX, boundary, required-image, and SHA-256 validation gate'
		)
		logger.info('  Dry-run: validation is ordered here before any programmer process.')
		const uploadPlan = await createUploadPlan(config, projectRoot)
		logger.stage('🚚', config.method === 'usbasp'
			? `Programming through ISP programmer ${config.programmer}`
			: `Programming through ${config.method} on ${config.port}`
		)
		await runChild(uploadPlan, config, logger)
		return
	}

	await runValidatedProgrammingStages({
		build: async () => await runChild(plan, config, logger),
		validate: async () => {
			const artifacts = await validateAndManifest(
				config,
				projectRoot,
				logger,
				true
			)
			assertProgrammingImage(artifacts, config.method)
			logger.success(
				'Required programming image passed the pre-device integrity gate.'
			)
			return artifacts
		},
		program: async () => {
			const uploadPlan = await createUploadPlan(config, projectRoot)
			logger.stage('🚚', config.method === 'usbasp'
				? `Programming validated merged image through ${config.programmer}`
				: `Programming validated application through ${config.method} on ${config.port}`
			)
			await runChild(uploadPlan, config, logger)
		}
	})
}

async function runProgram(config, projectRoot, logger) {
	const artifact = resolveFromProject(
		config.hexPath || defaultApplicationHex(projectRoot),
		projectRoot
	)
	if (config.command === 'verify' && !config.dryRun) {
		await inspectArtifact(artifact, projectRoot)
	}
	const descriptions = {
		backup: `Read flash through ${config.method}${config.port ? ` on ${config.port}` : ''}`,
		verify: `Verify flash through ${config.method}${config.port ? ` on ${config.port}` : ''}`,
		probe: `Probe AVR signature through ${config.method}${config.port ? ` on ${config.port}` : ''}`,
		metadata: `Read Urboot metadata on ${config.port}`
	}
	logger.stage('🧰', descriptions[config.command])
	if (config.command === 'backup') {
		const output = resolveFromProject(config.outputPath, projectRoot)
		let stat = null
		try {
			stat = await fs.stat(output)
		} catch (error) {
			if (error.code !== 'ENOENT') throw error
		}
		if (stat?.isDirectory()) {
			throw new FirmwareToolError(
				`Backup output is a directory: ${output}`,
				EXIT.VALIDATION
			)
		}
		await fs.mkdir(dirname(output), { recursive: true })
		const temporary = config.dryRun
			? output
			: join(
				dirname(output),
				`.${basename(output)}.${process.pid}.${Date.now()}.part`
			)
		const plan = await createProgramPlan(
			config,
			projectRoot,
			artifact,
			temporary
		)
		try {
			await runChild(plan, config, logger)
			if (!config.dryRun) {
				const { content, sha256 } = await sha256File(temporary)
				const parsed = parseIntelHex(content.toString('utf8'), basename(temporary))
				if (parsed.endAddress !== null && parsed.endAddress >= BOARD.flashBytes) {
					throw new FirmwareToolError(
						'Flash backup exceeds the ATmega328P address range',
						EXIT.VALIDATION
					)
				}
				await fs.rename(temporary, output)
				logger.success(
					`Validated backup committed atomically: ${output} (${parsed.dataBytes} bytes, SHA256 ${sha256})`
				)
			}
		} catch (error) {
			if (!config.dryRun) {
				try {
					await fs.rm(temporary, { force: true })
				} catch {
					// Preserve the original transfer or integrity failure.
				}
			}
			throw error
		}
	} else {
		const plan = await createProgramPlan(config, projectRoot, artifact)
		await runChild(plan, config, logger)
	}
	logger.success(`${config.command} operation completed.`)
}

function delay(milliseconds) {
	return new Promise(accept => setTimeout(accept, milliseconds))
}

async function runWatch(config, projectRoot, logger) {
	logger.stage(
		'👀',
		config.uploadOnChange
			? config.method === 'usbasp'
				? `Watching firmware; stable changes build and program through ${config.programmer}`
				: `Watching firmware; stable changes build and upload through ${config.method} on ${config.port}`
			: 'Watching firmware; stable changes build without touching hardware'
	)
	logger.detail(
		`Content polling ${config.pollMs} ms • debounce ${config.debounceMs} ms • byte-identical touches skipped`
	)
	let interrupted = false
	const stop = () => { interrupted = true }
	process.once('SIGINT', stop)
	process.once('SIGTERM', stop)
	let completed = false
	let lastAttempt = ''
	let candidate = ''
	let stableSince = 0
	try {
		while (!interrupted) {
			const current = (await sourceDigest(projectRoot)).sha256
			const now = Date.now()
			if (current !== candidate) {
				candidate = current
				stableSince = now
			}
			if (candidate !== lastAttempt && now - stableSince >= config.debounceMs) {
				logger.detail(`Stable source ${candidate.slice(0, 12)}; starting action.`)
				lastAttempt = candidate
				try {
					await runBuild(config, projectRoot, logger)
					completed = true
				} catch (error) {
					logger.error(error.message || String(error))
					if (config.once) throw error
					logger.warn('Watcher remains active; the next content change will retry.')
				}
				if (config.once) return
				const after = (await sourceDigest(projectRoot)).sha256
				if (after !== lastAttempt) {
					candidate = after
					stableSince = Date.now()
					logger.detail('A newer edit arrived during the action; it is queued.')
				}
			}
			await delay(config.pollMs)
		}
	} finally {
		process.removeListener('SIGINT', stop)
		process.removeListener('SIGTERM', stop)
	}
	if (interrupted) {
		if (completed) logger.success('Watcher stopped cleanly.')
		else logger.warn('Watcher stopped before its first action.')
		return EXIT.INTERRUPTED
	}
	return EXIT.OK
}

export async function main(
	argv = process.argv.slice(2),
	env = process.env,
	projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
) {
        const productTitle = resolveProductTitle(env)
        let config
	try {
		assertNodeVersion()
		config = parseArguments(argv, env)
	} catch (error) {
                const fallback = createLogger(
                        { quiet: false, noColor: Boolean(env.NO_COLOR) },
                        productTitle
                )
		fallback.error(error.message || String(error))
		console.error('Run firmware.cmd --help for usage.')
		return error.exitCode || EXIT.USAGE
        }
        if (config.help) {
                console.log(usage(!config.noColor && process.stdout.isTTY, productTitle))
                return EXIT.OK
        }

        const logger = createLogger(config, productTitle)
	logger.banner()
	const started = process.hrtime.bigint()
	try {
		switch (config.command) {
			case 'build':
			case 'upload':
				await runBuild(config, projectRoot, logger)
				break
			case 'watch':
				if (await runWatch(config, projectRoot, logger) === EXIT.INTERRUPTED) {
					return EXIT.INTERRUPTED
				}
				break
			case 'check':
				await validateAndManifest(config, projectRoot, logger, false)
				break
			case 'manifest':
				await validateAndManifest(config, projectRoot, logger, true)
				break
			case 'backup':
			case 'verify':
			case 'probe':
			case 'metadata':
				await runProgram(config, projectRoot, logger)
				break
		}
		const seconds = Number(process.hrtime.bigint() - started) / 1_000_000_000
		logger.success(`Completed in ${seconds.toFixed(2)}s.`)
		return EXIT.OK
	} catch (error) {
		logger.error(error.message || String(error))
		if (config.verbose && error.stack) logger.detail(error.stack)
		return error.exitCode || EXIT.TOOL
	}
}

const isEntryPoint = process.argv[1] &&
	resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url))
if (isEntryPoint) {
	process.exitCode = await main()
}
