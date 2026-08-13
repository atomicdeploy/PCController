#!/usr/bin/env node

// Build and run Go tests from stable project-owned paths. This avoids the
// changing %TEMP% test executable names that can repeatedly trigger Windows
// Firewall prompts for IPC/network packages.

import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import {
	existsSync,
	closeSync,
	openSync,
	mkdirSync,
	readdirSync,
	readFileSync,
	renameSync,
	rmSync,
	statSync,
	writeFileSync
} from 'node:fs'
import { basename, dirname, extname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadProjectEnv } from './env.mjs'

loadProjectEnv()

const SCRIPT = fileURLToPath(import.meta.url)
const DEFAULT_MODULE = resolve(dirname(SCRIPT), '..', 'Controller')
const DEFAULT_OUTPUT = process.platform === 'win32' && process.env.LOCALAPPDATA
	? resolve(process.env.LOCALAPPDATA, 'PCController', 'test-programs', 'go')
	: resolve(dirname(SCRIPT), '..', '..', '.build', 'tests', 'go')

function parseArguments(argv) {
	const options = {
		module: DEFAULT_MODULE,
		output: DEFAULT_OUTPUT,
		go: 'go',
		retest: false,
		dryRun: false,
		help: false
	}
	for (let index = 0; index < argv.length; index += 1) {
		const argument = argv[index]
		const take = name => {
			if (index + 1 >= argv.length) throw new Error(`${name} requires a value`)
			index += 1
			return argv[index]
		}
		switch (argument) {
			case '--module': options.module = resolve(take(argument)); break
			case '--output': options.output = resolve(take(argument)); break
			case '--go': options.go = take(argument); break
			case '--retest': options.retest = true; break
			case '--dry-run': options.dryRun = true; break
			case '--help':
			case '-h': options.help = true; break
			default: throw new Error(`unknown option: ${argument}`)
		}
	}
	return options
}

function usage() {
	return `Stable-path Go test runner

Usage: node Tools/Build/go-tests.mjs [options]

  --module DIR   Go module root (default: Tools/Controller)
  --output DIR   Stable test executable/cache directory
  --go PATH      Go executable override
  --retest       Re-run unchanged binaries without rebuilding them
  --dry-run      Show the stable plan without compiling or running tests`
}

function run(file, args, options = {}) {
	const result = spawnSync(file, args, {
		cwd: options.cwd,
		env: options.env || process.env,
		encoding: options.capture ? 'utf8' : undefined,
		stdio: options.capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
		shell: false,
		windowsHide: true
	})
	if (result.error) throw new Error(`start ${file}: ${result.error.message}`)
	if (result.status !== 0) {
		const detail = options.capture ? `: ${(result.stderr || result.stdout || '').trim()}` : ''
		throw new Error(`${basename(file)} exited with ${result.status}${detail}`)
	}
	return options.capture ? result.stdout || '' : ''
}

function walkGoFiles(root, current, files) {
	for (const entry of readdirSync(current, { withFileTypes: true })) {
		if (entry.isDirectory()) {
			if (['.cache', '.git', 'bin', 'node_modules', 'web'].includes(entry.name) || entry.name.startsWith('.build')) continue
			walkGoFiles(root, join(current, entry.name), files)
		} else if (entry.isFile() && extname(entry.name).toLowerCase() === '.go') {
			files.push(join(current, entry.name))
		}
	}
}

function walkEmbeddedFiles(current, files) {
	if (!existsSync(current)) return
	for (const entry of readdirSync(current, { withFileTypes: true })) {
		const path = join(current, entry.name)
		if (entry.isDirectory()) walkEmbeddedFiles(path, files)
		else if (entry.isFile()) files.push(path)
	}
}

function sha256(value) {
	return createHash('sha256').update(value).digest('hex')
}

export function goTestSourceIdentity(moduleRoot, goVersion, environment = {}) {
	const files = []
	walkGoFiles(moduleRoot, moduleRoot, files)
	// Go's compiler embeds these generated web assets into internal/webui. They
	// must invalidate the stable test cache just like a changed .go source file.
	walkEmbeddedFiles(join(moduleRoot, 'internal', 'webui', 'dist'), files)
	walkEmbeddedFiles(join(moduleRoot, 'internal', 'defaultassets', 'assets'), files)
	for (const name of ['go.mod', 'go.sum']) {
		const path = join(moduleRoot, name)
		if (existsSync(path)) files.push(path)
	}
	files.sort((left, right) => relative(moduleRoot, left).localeCompare(relative(moduleRoot, right)))
	const manifest = files.map(path =>
		`${relative(moduleRoot, path).replaceAll('\\', '/')}:${sha256(readFileSync(path))}\n`
	).join('')
	return {
		sha256: sha256(`${goVersion.trim()}\n${[
			'GOOS', 'GOARCH', 'GOFLAGS', 'CGO_ENABLED', 'CC', 'CXX'
		].map(key => `${key}=${environment[key] || ''}`).join('\n')}\n${manifest}`),
		files: files.length,
		goVersion: goVersion.trim()
	}
}

export function stableTestBinaryName(importPath, platform = process.platform) {
	const readable = importPath.replace(/[^A-Za-z0-9._-]+/g, '_').replace(/^_+|_+$/g, '') || 'go-package'
	const suffix = sha256(importPath).slice(0, 10)
	// Avoid Go's generic/randomized *.test.exe identity on Windows. The stable,
	// product-prefixed filename and project-owned path keep one firewall identity
	// across rebuilds while preserving a recognizable package suffix.
	return platform === 'win32'
		? `pccontroller-tests-${readable}-${suffix}.exe`
		: `pccontroller-tests-${readable}-${suffix}`
}

function listTestPackages(go, moduleRoot, env) {
	const template = '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}\t{{.Dir}}{{end}}'
	const output = run(go, ['list', '-f', template, './...'], {
		cwd: moduleRoot, env, capture: true
	})
	return output.split(/\r?\n/).filter(Boolean).map(line => {
		const separator = line.indexOf('\t')
		if (separator <= 0) throw new Error(`unexpected go list test package row: ${line}`)
		return { importPath: line.slice(0, separator), directory: line.slice(separator + 1) }
	})
}

function loadCache(path) {
	try {
		return JSON.parse(readFileSync(path, 'utf8'))
	} catch {
		return null
	}
}

function writeJSON(path, value) {
	const temporary = `${path}.tmp-${process.pid}`
	writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, 'utf8')
	try {
		renameSync(temporary, path)
	} catch {
		rmSync(path, { force: true })
		renameSync(temporary, path)
	} finally {
		rmSync(temporary, { force: true })
	}
}

function waitMilliseconds(milliseconds) {
	Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, milliseconds)
}

function withOutputLock(output, action) {
	mkdirSync(output, { recursive: true })
	const lockPath = join(output, '.pccontroller-go-tests.lock')
	const deadline = Date.now() + 10 * 60 * 1000
	let descriptor
	while (descriptor === undefined) {
		try {
			descriptor = openSync(lockPath, 'wx')
			writeFileSync(descriptor, `${JSON.stringify({ pid: process.pid, startedAt: new Date().toISOString() })}\n`, 'utf8')
		} catch (error) {
			if (error?.code !== 'EEXIST') throw error
			try {
				if (Date.now() - statSync(lockPath).mtimeMs > 15 * 60 * 1000) {
					rmSync(lockPath, { force: true })
					continue
				}
			} catch (lockError) {
				if (lockError?.code === 'ENOENT') continue
				throw lockError
			}
			if (Date.now() >= deadline) {
				throw new Error(`timed out waiting for stable Go test runner lock: ${lockPath}`)
			}
			waitMilliseconds(200)
		}
	}
	try {
		return action()
	} finally {
		closeSync(descriptor)
		rmSync(lockPath, { force: true })
	}
}

export function createStableTestPlan(packages, output, platform = process.platform) {
	return packages.map(value => ({
		...value,
		binary: join(output, stableTestBinaryName(value.importPath, platform))
	}))
}

export function main(argv = process.argv.slice(2), env = process.env) {
	const options = parseArguments(argv)
	if (options.help) {
		process.stdout.write(`${usage()}\n`)
		return 0
	}
	if (!existsSync(options.module) || !statSync(options.module).isDirectory()) {
		throw new Error(`Go module directory does not exist: ${options.module}`)
	}
	const goVersion = run(options.go, ['version'], { cwd: options.module, env, capture: true })
	const identity = goTestSourceIdentity(options.module, goVersion, env)
	const packages = listTestPackages(options.go, options.module, env)
	const plan = createStableTestPlan(packages, options.output)
	const cachePath = join(options.output, 'passed.json')
	const cache = loadCache(cachePath)
	const binariesExist = plan.every(item => existsSync(item.binary))
	const current = cache?.sourceSHA256 === identity.sha256 && binariesExist

	process.stdout.write(`Go tests: ${packages.length} packages, stable root ${options.output}\n`)
	if (options.dryRun) {
		for (const item of plan) process.stdout.write(`  ${item.importPath} -> ${item.binary}\n`)
		process.stdout.write(current && !options.retest ? '  cached pass would be reused\n' : '  tests would be built/run\n')
		return 0
	}
	return withOutputLock(options.output, () => {
		// Re-read after acquiring the machine-wide lock: another worktree may
		// have populated the stable cache while this process was waiting.
		const lockedCache = loadCache(cachePath)
		const lockedCurrent = lockedCache?.sourceSHA256 === identity.sha256 &&
			plan.every(item => existsSync(item.binary))
		if (lockedCurrent && !options.retest) {
			process.stdout.write(`✅ Go test cache matches ${identity.sha256.slice(0, 12)}; no test executable was rebuilt or run.\n`)
			return 0
		}
		if (!lockedCurrent) {
			for (const item of plan) {
				process.stdout.write(`🔨 ${item.importPath} -> ${item.binary}\n`)
				run(options.go, ['test', '-c', '-o', item.binary, item.importPath], {
					cwd: options.module, env
				})
			}
		} else {
			process.stdout.write('♻️ Reusing unchanged stable test executables.\n')
		}
		for (const item of plan) {
			process.stdout.write(`🧪 ${item.importPath}\n`)
			run(item.binary, ['-test.count=1'], { cwd: item.directory, env })
		}
		writeJSON(cachePath, {
			format: 'pccontroller-stable-go-test-cache/v1',
			sourceSHA256: identity.sha256,
			goVersion: identity.goVersion,
			sourceFiles: identity.files,
			packages: plan.map(item => ({ importPath: item.importPath, binary: basename(item.binary) }))
		})
		process.stdout.write(`✅ Stable-path Go tests passed; cache ${cachePath}\n`)
		return 0
	})
}

const isMain = process.argv[1] && resolve(process.argv[1]) === resolve(SCRIPT)
if (isMain) {
	try {
		process.exitCode = main()
	} catch (error) {
		process.stderr.write(`❌ ${error.stack || error.message || error}\n`)
		process.exitCode = 1
	}
}
