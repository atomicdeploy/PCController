// Resolve opt-in board cores using their official package indexes, not the
// compatibility minimum as an exact install pin.
import { appendFileSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import { compareVersions, stableParts } from './stable-version.mjs'
import { withDirectFallback } from './network.mjs'

function fetchIndexJSON(url) {
  return withDirectFallback((env, direct) => JSON.parse(execFileSync(process.platform === 'win32' ? 'curl.exe' : 'curl', [
    ...(direct ? ['--noproxy', '*'] : []), '--fail', '--silent', '--show-error', '--location',
    '--proto', '=https', '--proto-redir', '=https', '--connect-timeout', '15', '--max-time', '60', url,
  ], { env, encoding: 'utf8', windowsHide: true, maxBuffer: 4 * 1024 * 1024 }))).value
}

export function resolveTargetCores(targets, fetchIndex = fetchIndexJSON) {
  const indexes = new Map()
  return Object.entries(targets).filter(([, target]) => target.buildAdapter === 'arduino-avr').map(([id, target]) => {
    if (target.core !== 'MegaCore:avr' || target.index !== 'https://mcudude.github.io/MegaCore/package_MCUdude_MegaCore_index.json') {
      throw new Error(`Target ${id} core source requires code review`)
    }
    if (!stableParts(target.minimumCoreVersion)) throw new Error(`Invalid core minimum for ${id}`)
    if (!indexes.has(target.index)) indexes.set(target.index, fetchIndex(target.index))
    const [name, architecture] = target.core.split(':')
    const candidates = (indexes.get(target.index).packages ?? []).filter(item => item.name === name)
      .flatMap(item => item.platforms ?? []).filter(item => item.architecture === architecture && stableParts(item.version) && compareVersions(item.version, target.minimumCoreVersion) >= 0)
      .sort((a, b) => compareVersions(b.version, a.version))
    const selected = candidates[0]
    if (!selected) throw new Error(`No compatible stable core for ${id}`)
    const source = new URL(selected.url)
    if (!/^SHA-256:[a-f0-9]{64}$/iu.test(selected.checksum ?? '') || source.protocol !== 'https:' || source.hostname !== 'mcudude.github.io' || source.username || source.password || source.port || !source.pathname.startsWith('/MegaCore/')) throw new Error(`Invalid core archive provenance for ${id}`)
    return { target: id, kind: 'build-only core', name: target.core, current: `minimum ${target.minimumCoreVersion}`, resolved: selected.version,
      changed: false, result: 'Latest stable selected for CI', url: selected.url, sha256: selected.checksum.slice(8) }
  })
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const targets = JSON.parse(readFileSync(new URL('../Firmware/targets.json', import.meta.url), 'utf8'))
  const selected = resolveTargetCores(targets).find(item => item.target === process.argv[2])
  if (!selected) throw new Error('Specify a declared AVR target')
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `core=${selected.name}\nversion=${selected.resolved}\n`)
  console.log(JSON.stringify(selected, null, 2))
}
