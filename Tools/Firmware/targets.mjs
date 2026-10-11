#!/usr/bin/env node
// Build-only larger-MCU lane. Never silently send a Mega artifact through the
// commissioned ATmega328P programmer, fuse/default-EEPROM or identity policy.
import { spawnSync } from 'node:child_process'
import { readFileSync, mkdirSync, readdirSync, writeFileSync } from 'node:fs'
import { resolve, dirname, relative, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createHash } from 'node:crypto'
import { parseIntelHex } from './firmware.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const targets = JSON.parse(readFileSync(join(root, 'Tools/Firmware/targets.json'), 'utf8'))
const names = { constrained: 0, 'expanded-panel': 1, 'expanded-autonomous': 2 }
const [action = 'help', name, profileName = 'constrained'] = process.argv.slice(2)
function run(args, capture = false) {
  const result = spawnSync(process.env.ARDUINO_CLI || 'arduino-cli', args, { cwd: root, encoding: 'utf8', stdio: capture ? 'pipe' : 'inherit' })
  if (result.error || result.status !== 0) throw result.error || new Error(result.stderr || `Arduino CLI failed (${result.status})`)
  return capture ? JSON.parse(result.stdout) : undefined
}
function collect(path, list = []) {
  for (const entry of readdirSync(path, { withFileTypes: true })) {
    const next = join(path, entry.name)
    if (entry.isDirectory()) collect(next, list)
    else if (/\.(cpp|h|ino)$/.test(entry.name)) list.push(next)
  }
  return list
}
try {
  if (action === 'help') {
    console.log('node Tools/Firmware/targets.mjs build mega2560 [constrained|expanded-panel|expanded-autonomous]\nBuild-only: same sketch and production source aggregators. Existing Arduino CLI/core/library cache; no upload/reset/fuse action.\nATmega328P remains node Tools/Firmware/firmware.mjs build (commissioned guards).')
  } else {
    if (action !== 'build' || !targets[name] || names[profileName] === undefined || process.argv.length > 5) throw new Error('unsupported target/profile/action; use help')
    const target = targets[name], profile = names[profileName]
    // Finish provisioning/compatibility checks BEFORE compilation/progress.
    const installed = run(['core', 'list', '--format', 'json'], true).platforms.find(core => core.id === target.core)
    if (!installed?.installed_version) throw new Error(`Install ${target.core} >= ${target.minimumCoreVersion} in the existing CLI cache using ${target.index}; no duplicate toolchain is provisioned automatically`)
    const parts = version => version.split('.').map(Number)
    const have = parts(installed.installed_version), need = parts(target.minimumCoreVersion)
    if (have.some(Number.isNaN) || have.reduce((order, v, i) => order || Math.sign(v - (need[i] || 0)), 0) < 0) throw new Error('installed core does not satisfy target policy')
    const details = run(['board', 'details', '--fqbn', target.fqbn, '--format', 'json'], true)
    for (const [property, expected] of [['build.mcu', target.mcu], ['build.f_cpu', '16000000L'], ['upload.maximum_size', String(target.applicationLimitBytes)], ['upload.maximum_data_size', String(target.sramBytes)]]) {
      if (!details.build_properties.includes(`${property}=${expected}`)) throw new Error(`target policy drift: ${property}`)
    }
    const files = [...collect(join(root, 'Project')), ...collect(join(root, 'LocalLib')), ...['PCController.ino', 'ProjectConfig.h', 'BoardSupportSources.cpp', 'ControllerDomainSources.cpp'].map(file => join(root, file))]
    const sources = files.map(file => ({ path: relative(root, file).replaceAll('\\', '/'), sha256: createHash('sha256').update(readFileSync(file)).digest('hex') })).sort((a, b) => a.path < b.path ? -1 : a.path > b.path ? 1 : 0)
    const sourceSha256 = createHash('sha256').update(sources.map(file => `${file.path}:${file.sha256}\n`).join('')).digest('hex')
    const buildHash = sourceSha256.slice(0, 8)
    const output = join(root, '.build/firmware', name, profileName)
    mkdirSync(output, { recursive: true })
    console.log(`Verified ${target.fqbn}, core ${installed.installed_version}; profile ${profileName}. No device access.`)
    run(['compile', '--fqbn', target.fqbn, '--build-path', join(root, '.cache/firmware', name, profileName), '--output-dir', output,
      '--build-property', `build.extra_flags=-DPCCONTROLLER_FEATURE_PROFILE=${profile} -DPCCONTROLLER_BUILD_HASH=0x${buildHash}UL -DPCCONTROLLER_IDENTITY_ADDRESS=0x${target.identityAddress.toString(16)}UL`,
      '--build-property', `compiler.c.elf.extra_flags=-w -flto -g -Wl,--section-start=.firmware_identity=0x${target.identityAddress.toString(16)}`, '--jobs', '2', root])
    for (const source of sources) {
      if (createHash('sha256').update(readFileSync(join(root, source.path))).digest('hex') !== source.sha256) throw new Error('source changed during target compilation; artifact is not source-verified, rebuild before delivery')
    }
    const artifacts = []
    for (const file of readdirSync(output).filter(file => /\.(hex|eep)$/.test(file))) {
      const bytes = readFileSync(join(output, file)), shape = parseIntelHex(bytes.toString('utf8'), file)
      const limit = file.endsWith('.eep') ? target.eepromBytes : file.includes('with_bootloader') ? target.flashBytes : target.applicationLimitBytes
      if (shape.endAddress !== null && shape.endAddress >= limit) throw new Error(`${file} exceeds target memory boundary`)
      artifacts.push({ file, sha256: createHash('sha256').update(bytes).digest('hex'), ...shape, limit })
    }
    if (!artifacts.some(file => file.file === 'PCController.ino.hex' && file.dataBytes > 12)) throw new Error('missing nonempty application artifact')
    writeFileSync(join(output, 'target-manifest.json'), JSON.stringify({ target, profile: profileName, sourceSha256, buildHash, sources, artifacts, physicalAcceptance: false, programmingCommissioned: false }, null, 2) + '\n')
  }
} catch (error) { console.error(error.message); process.exitCode = 1 }
