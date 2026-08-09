import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import test from 'node:test'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  assertTrustedDependencyURL,
  assertTrustedGitHubURL,
  assertTrustedRepository,
  buildEnvironmentFromToolchainBootstrap,
  commandInvocation,
  compareHostToolLocks,
  compareCompositeVersions,
  compareVersions,
  parseWingetCompilerManifest,
  run,
  sameSubstantive,
  stableParts,
  validateHostSourcePolicy,
  validateHostToolsLock,
  validateToolchainLockSources,
  validateToolchainSourcePolicy,
  workflowActionInventory,
} from './update.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const repo = resolve(here, '..', '..')

test('Windows command scripts use an explicit ComSpec command line while native commands stay direct', () => {
  const commandProcessor = 'C:\\Windows\\System32\\cmd.exe'
  assert.deepEqual(
    commandInvocation(
      'C:\\Program Files\\PCController\\build.cmd',
      ['--report', 'C:\\Result Files\\report.json'],
      { ComSpec: commandProcessor },
      'win32',
    ),
    {
      file: commandProcessor,
      args: [
        '/d', '/s', '/v:off', '/c',
        '""C:\\Program Files\\PCController\\build.cmd" "--report" "C:\\Result Files\\report.json""',
      ],
      windowsVerbatimArguments: true,
    },
  )
  assert.deepEqual(
    commandInvocation('./build.sh', ['--all'], {}, 'linux'),
    { file: './build.sh', args: ['--all'], windowsVerbatimArguments: false },
  )
  assert.deepEqual(
    commandInvocation('node.exe', ['--version'], {}, 'win32'),
    { file: 'node.exe', args: ['--version'], windowsVerbatimArguments: false },
  )
  assert.throws(
    () => commandInvocation('build.cmd', ['%PATH%'], { ComSpec: commandProcessor }, 'win32'),
    /cannot contain/u,
  )
})

test('Windows command-script runner fixes the Node spawnSync EINVAL regression', {
  skip: process.platform !== 'win32',
}, () => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller command script '))
  const script = join(directory, 'argument fixture.cmd')
  writeFileSync(script, '@echo off\r\nif "%~1"=="hello world" exit /b 0\r\nexit /b 42\r\n', 'utf8')
  try {
    const direct = spawnSync(script, ['hello world'], { encoding: 'utf8', windowsHide: true })
    assert.equal(direct.status, null)
    assert.equal(direct.error?.code, 'EINVAL')
    assert.equal(run(script, ['hello world']).status, 0)
  } finally {
    rmSync(directory, { recursive: true, force: true })
  }
})

test('captured command failures retain child diagnostics for structured reports', () => {
  assert.throws(
    () => run(process.execPath, ['-e',
      'process.stdout.write("diagnostic stdout\\n"); process.stderr.write("diagnostic stderr\\n"); process.exit(7)']),
    (error) => {
      assert.match(error.message, /diagnostic stdout/u)
      assert.match(error.message, /diagnostic stderr/u)
      return true
    },
  )
})

test('candidate build stays bound to the CLI and config returned by bootstrap', () => {
  const environment = {
    HTTP_PROXY: 'http://proxy.invalid:8080',
    NO_PROXY: 'localhost,.lan',
    PATH: 'C:\\Windows\\System32',
    pccontroller_toolchain_cli: 'C:\\stale\\arduino-cli.exe',
    PcController_Toolchain_Config: 'C:\\stale\\firmware-cli.yaml',
  }
  const selected = buildEnvironmentFromToolchainBootstrap(`
Installing exact firmware dependencies
Saved managed firmware CLI path in PC-side host configuration.
{
  "cli_path": "C:\\\\managed tools\\\\arduino-cli.exe",
  "config_path": "C:\\\\managed tools\\\\firmware-cli.yaml"
}
`, environment)
  assert.deepEqual(selected, {
    HTTP_PROXY: environment.HTTP_PROXY,
    NO_PROXY: environment.NO_PROXY,
    PATH: environment.PATH,
    PCCONTROLLER_TOOLCHAIN_CLI: 'C:\\managed tools\\arduino-cli.exe',
    PCCONTROLLER_TOOLCHAIN_CONFIG: 'C:\\managed tools\\firmware-cli.yaml',
  })
  assert.equal(Object.keys(selected).filter((name) =>
    name.toUpperCase() === 'PCCONTROLLER_TOOLCHAIN_CLI').length, 1)
  assert.equal(Object.keys(selected).filter((name) =>
    name.toUpperCase() === 'PCCONTROLLER_TOOLCHAIN_CONFIG').length, 1)
  assert.throws(
    () => buildEnvironmentFromToolchainBootstrap('{"cli_path":"arduino-cli"}', environment),
    /config_path/u,
  )
})

test('stable comparison is semantic and rejects prereleases', () => {
  assert.equal(compareVersions('1.10.0', '1.9.9'), 1)
  assert.equal(compareVersions('u8.0', '8.0.0'), 0)
  assert.equal(stableParts('1.5.2-rc.1'), null)
})

test('dependency network sources are code allowlisted with exact GitHub repository identities', () => {
  for (const url of [
    'https://api.github.com/repos/arduino/arduino-cli/releases?per_page=100',
    'https://github.com/arduino/arduino-cli/releases/download/v1.5.1/arduino-cli.zip',
    'https://raw.githubusercontent.com/microsoft/winget-pkgs/master/manifests/example.yaml',
    'https://mcudude.github.io/MiniCore/package_MCUdude_MiniCore_index.json',
    'https://downloads.arduino.cc/libraries/library_index.json.gz',
    'https://downloads.arduino.cc/libraries/github.com/sui77/rc_switch-2.6.4.zip',
    'https://nodejs.org/dist/index.json',
    'https://go.dev/VERSION?m=text',
  ]) assert.equal(assertTrustedDependencyURL(url), url)

  assert.equal(assertTrustedRepository('MCUdude/MiniCore'), 'mcudude/minicore')
  assert.equal(
    assertTrustedGitHubURL(
      'https://api.github.com/repos/microsoft/winget-pkgs/contents/manifests/example',
      'microsoft/winget-pkgs',
    ),
    'https://api.github.com/repos/microsoft/winget-pkgs/contents/manifests/example',
  )

  for (const url of [
    'http://nodejs.org/dist/index.json',
    'https://user@nodejs.org/dist/index.json',
    'https://api.github.com.evil.invalid/repos/arduino/arduino-cli/releases',
    'https://api.github.com@evil.invalid/repos/arduino/arduino-cli/releases',
    'https://github.com/example/arduino-cli/releases',
    'https://example.invalid/downloads.arduino.cc/libraries/library_index.json.gz',
  ]) assert.throws(() => assertTrustedDependencyURL(url), /allowlist|credential-free HTTPS/u)
  assert.throws(() => assertTrustedRepository('example/arduino-cli'), /not code-review allowlisted/u)
  assert.throws(
    () => assertTrustedGitHubURL('https://api.github.com/repos/upx/upx/releases', 'arduino/arduino-cli'),
    /redirected to untrusted repository/u,
  )
})

test('canonical dependency policies and locks reject manifest-derived source substitutions', () => {
  const toolchainPolicy = JSON.parse(readFileSync(join(repo, 'Tools', 'Controller', 'toolchain-profile.json'), 'utf8'))
  const toolchainLock = JSON.parse(readFileSync(join(repo, 'Tools', 'Controller', 'toolchain-lock.json'), 'utf8'))
  const hostPolicy = JSON.parse(readFileSync(join(repo, 'Tools', 'Dependencies', 'dependency-policy.json'), 'utf8'))

  validateToolchainSourcePolicy(toolchainPolicy)
  validateToolchainLockSources(toolchainLock)
  validateHostSourcePolicy(hostPolicy)

  const redirectedCLI = structuredClone(toolchainPolicy)
  redirectedCLI.cli.release_api = 'https://api.github.com/repos/upx/upx/releases'
  assert.throws(() => validateToolchainSourcePolicy(redirectedCLI), /redirected to untrusted repository/u)

  const redirectedLibrary = structuredClone(toolchainLock)
  redirectedLibrary.libraries[0].url = 'https://downloads.arduino.cc/libraries/github.com/sui77/rc_switch-3.0.3.zip'
  assert.throws(() => validateToolchainLockSources(redirectedLibrary), /archive is not code-review allowlisted/u)

  const redirectedCompiler = structuredClone(hostPolicy)
  redirectedCompiler.windows_c_compiler.manifest_api = 'https://api.github.com/repos/arduino/arduino-cli/releases'
  assert.throws(() => validateHostSourcePolicy(redirectedCompiler), /redirected to untrusted repository/u)

  const unexpectedChecksums = structuredClone(hostPolicy)
  unexpectedChecksums.node.checksums_url_template = 'https://example.invalid/dist/v{version}/SHASUMS256.txt'
  assert.throws(() => validateHostSourcePolicy(unexpectedChecksums), /outside the code-review source allowlist/u)
})

test('WinGet compiler manifest parsing selects exact x64 integrity and composite latest order', () => {
  assert.equal(compareCompositeVersions('16.1.0-14.0.0-r3', '16.1.0-14.0.0-r2'), 1)
  const resolved = parseWingetCompilerManifest(`
PackageIdentifier: BrechtSanders.WinLibs.POSIX.UCRT
PackageVersion: 16.1.0-14.0.0-r3
Installers:
- Architecture: x86
  InstallerUrl: https://example.invalid/x86.zip
  InstallerSha256: ${'1'.repeat(64)}
- Architecture: x64
  InstallerUrl: https://example.invalid/x64.zip
  InstallerSha256: ${'A'.repeat(64)}
`, 'x64', 'x86_64-w64-mingw32', {
    url: 'https://example.invalid/manifest.yaml',
    sha: 'b'.repeat(40),
  })
  assert.deepEqual(resolved, {
    package_id: 'BrechtSanders.WinLibs.POSIX.UCRT',
    package_version: '16.1.0-14.0.0-r3',
    compiler: 'gcc',
    compiler_version: '16.1.0',
    architecture: 'x64',
    target: 'x86_64-w64-mingw32',
    provenance: 'winget-community-manifest',
    manifest_url: 'https://example.invalid/manifest.yaml',
    manifest_git_sha: 'b'.repeat(40),
    installer_url: 'https://example.invalid/x64.zip',
    installer_sha256: 'a'.repeat(64),
  })
})

test('resolved tool lock comparison ignores timestamp-only churn', () => {
  const current = { format: 'lock/v1', resolved_at_utc: '2026-01-01T00:00:00Z', tool: { version: '1.2.3', sha256: 'abc' } }
  const resolved = { ...current, resolved_at_utc: '2099-01-01T00:00:00Z' }
  assert.equal(sameSubstantive(current, resolved), true)
  resolved.tool = { version: '1.2.4', sha256: 'def' }
  assert.equal(sameSubstantive(current, resolved), false)
})

test('npm projects declare compatibility ranges while each lock stays exact', () => {
  for (const directory of [join(repo, 'Tools', 'Controller', 'web'), join(repo, 'Tools', 'Build')]) {
    const manifest = JSON.parse(readFileSync(join(directory, 'package.json'), 'utf8'))
    const lock = JSON.parse(readFileSync(join(directory, 'package-lock.json'), 'utf8'))
    for (const [name, value] of Object.entries({ ...manifest.dependencies, ...manifest.devDependencies })) {
      assert.match(value, /^\^\d/, `${name} in ${directory} must use a latest-compatible caret range`)
    }
    assert.deepEqual(lock.packages[''].dependencies ?? {}, manifest.dependencies ?? {})
    assert.deepEqual(lock.packages[''].devDependencies ?? {}, manifest.devDependencies ?? {})
  }
})

test('scheduled updater validates every required candidate gate before PR creation', () => {
  const workflow = readFileSync(join(repo, '.github', 'workflows', 'update-dependencies.yml'), 'utf8')
  const updater = readFileSync(join(here, 'update.mjs'), 'utf8')
  const attributes = readFileSync(join(repo, '.gitattributes'), 'utf8')
  for (const expected of [
    'schedule:', '--apply --validate', 'steps.candidate.outcome == \'success\'',
    'body-path: .build/dependencies/dependency-pr.md', 'dependency-blocked',
    '📦 dependencies', '🏗️ tooling-build',
  ]) assert.ok(workflow.includes(expected), `workflow missing ${expected}`)
  assert.match(workflow, /peter-evans\/create-pull-request@[0-9a-f]{40}\s+# v8/u)
  for (const expected of [
    '32256', 'Urboot-Custom', 'VirtualBoard tests', 'Generated product identity',
    'Go tests from stable paths', 'Web tests',
    'windowsResources', 'upx', 'function resolveToolchain(mode, directRetry)',
    'select-windows-compiler.mjs', 'generate-toolchain-policy.mjs',
  ]) assert.ok(updater.includes(expected), `updater missing ${expected}`)
  assert.ok(
    updater.indexOf("step('Clean generated build outputs'") < updater.indexOf('installResolvedHostTools(hostTools'),
    'managed host tools must be provisioned after the root clean step',
  )
  assert.ok(
    updater.indexOf('run(process.execPath, [toolchainPolicyGenerator]') < updater.indexOf('if (options.validate)'),
    'the generated runtime policy must be refreshed before candidate validation',
  )
  assert.match(updater, /PCCONTROLLER_TOOLCHAIN_CLI:\s*report\.cli_path/u)
  assert.match(updater, /PCCONTROLLER_TOOLCHAIN_CONFIG:\s*report\.config_path/u)
  assert.match(updater, /run\(rootBuild, \['--all'\], \{[\s\S]*?env: buildEnvironment/u)
  for (const path of [
    'LICENSE\\.upstream',
    'backends\\/tm1637_progress\\.S',
    'patches\\/0001-optional-progress-backend-hook\\.patch',
  ]) {
    assert.match(attributes,
      new RegExp(`^Tools/Bootloader/Urboot-Custom/${path} text eol=lf$`, 'mu'),
      'exact Urboot-Custom input hashes must survive Windows checkout conversion')
  }
  const buildSystemGate = updater.slice(
    updater.indexOf("step('Build-system tests'"),
    updater.indexOf("step('Firmware and host build'"),
  )
  assert.doesNotMatch(buildSystemGate, /capture:\s*false/u,
    'build-system test failures must remain captured for the structured report')
})

test('every external workflow action is immutable and included in the resolved inventory', () => {
  const inventory = workflowActionInventory()
  assert.ok(inventory.length >= 8)
  for (const action of inventory) {
    assert.match(action.revision, /^[0-9a-f]{40}$/u)
    assert.match(action.version, /^v\d+/u)
    assert.ok(action.workflows.length > 0)
  }
})

test('host tool lock replay validates hashes and comparison is idempotent', () => {
  const lock = JSON.parse(readFileSync(join(repo, 'Tools', 'Dependencies', 'resolved-tools-lock.json'), 'utf8'))
  validateHostToolsLock(lock)
  assert.deepEqual(compareHostToolLocks(lock, structuredClone(lock)), [])
  const changed = structuredClone(lock)
  changed.node.version = '99.0.0'
  assert.deepEqual(compareHostToolLocks(lock, changed).map((item) => item.name), ['Node.js LTS'])
  changed.upx.assets[0].sha256 = '0'.repeat(64)
  assert.ok(compareHostToolLocks(lock, changed).some((item) => item.name === 'UPX asset inventory'))
})

test('one canonical scheduled updater owns dependency resolution', () => {
  const workflows = readFileSync(join(repo, '.github', 'workflows', 'update-dependencies.yml'), 'utf8')
  const exporter = readFileSync(join(repo, 'Tools', 'Dependencies', 'export-lock.mjs'), 'utf8')
  assert.match(workflows, /update-dependencies\.cmd --apply --validate/u)
  assert.match(exporter, /Tools[^\n]+Controller[^\n]+toolchain-lock\.json/u)
  assert.equal(requiredWorkflowAbsent(join(repo, '.github', 'workflows', 'dependencies.yml')), true)
  assert.equal(requiredWorkflowAbsent(join(repo, '.github', 'dependencies.json')), true)
})

test('host CI consumes exact Node.js and go-winres identities from the canonical host lock', () => {
  const workflow = readFileSync(join(repo, '.github', 'workflows', 'host.yml'), 'utf8')
  assert.match(workflow, /export-lock\.mjs export-host/u)
  assert.match(workflow, /steps\.host-dependencies\.outputs\.node_version/u)
  assert.match(workflow, /steps\.host-dependencies\.outputs\.go_winres_version/u)
  assert.doesNotMatch(workflow, /go install github\.com\/tc-hib\/go-winres@v\d/u)
  assert.doesNotMatch(workflow, /node-version:\s*["']?24["']?\s*$/mu)
})

test('dependency output uses the shared Chalk and Unicode table renderer', () => {
  const updater = readFileSync(join(here, 'update.mjs'), 'utf8')
  assert.match(updater, /createChalk, renderUnicodeTable/)
  assert.doesNotMatch(updater, /\\u001b|\\x1b|padEnd\(|\.repeat\(/)
})

function requiredWorkflowAbsent(path) {
  try {
    readFileSync(path, 'utf8')
    return false
  } catch (error) {
    if (error?.code === 'ENOENT') return true
    throw error
  }
}
