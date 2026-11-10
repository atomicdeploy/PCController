#!/usr/bin/env node
// Launch the integrated controller GUI, never the donor's standalone board owner.
import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadProjectEnv } from './env.mjs'

loadProjectEnv()

const script = fileURLToPath(import.meta.url)
const root = resolve(dirname(script), '../..')
export const help = `PCController Desktop
Usage: desktop.cmd [--installed | --development | --package DIR]
                   [--config FILE] [--preview [--page ACTIVITY]]
                   [--attach HOST:PORT | --auto-connect]

Defaults to the installed application, starting a local core with hardware
auto-connect paused. --preview opens no transport. --attach never changes the
selected host's connection policy. --auto-connect explicitly enables automatic
hardware connection for a newly started core. Existing cores are not paused.

Build with build.cmd --host-only, then use --development; or use --package DIR
with a staged package produced by build.cmd --host-only --stage-only.
No source, configuration, installation, or running application is replaced.
`

export function desktopInvocation(argv, options = {}) {
  const platform = options.platform ?? process.platform
  const project = options.root ?? root
  const local = options.localAppData ?? process.env.LOCALAPPDATA
  const exists = options.exists ?? existsSync
  if (platform !== 'win32') throw new Error('The native Desktop GUI requires Windows.')
  let selection = '', packageDir = '', config = '', address = '', page = ''
  let preview = false, auto = false
  const select = (value) => {
    if (selection) throw new Error('Select only one of --installed, --development or --package.')
    selection = value
  }
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    const value = () => {
      const result = argv[++i]
      if (!result?.trim() || result.startsWith('--')) throw new Error(`${arg} requires a value.`)
      return result
    }
    switch (arg) {
      case '--installed': select('installed'); break
      case '--development': select('development'); break
      case '--package': select('package'); packageDir = resolve(value()); break
      case '--config': config = resolve(value()); break
      case '--attach': address = value(); break
      case '--page': page = value(); break
      case '--preview': preview = true; break
      case '--auto-connect': auto = true; break
      default: throw new Error(`Unknown option: ${arg}. Use --help.`)
    }
  }
  if (page && !preview) throw new Error('--page requires --preview.')
  if (preview && (address || auto)) throw new Error('--preview cannot connect to a host or board.')
  if (address && auto) throw new Error('--attach cannot change an existing host auto-connect policy.')
  selection ||= 'installed'
  if (selection === 'installed' && !local) throw new Error('LOCALAPPDATA is required to locate the installed application.')
  const directory = selection === 'package' ? packageDir : selection === 'development'
    ? join(project, 'Tools', 'Controller', 'bin') : join(local, 'Programs', 'PCController', 'bin')
  const executable = join(directory, 'controller.exe')
  if (!exists(executable)) throw new Error(`Controller package not found: ${executable}. Build with build.cmd --host-only or select --package DIR.`)
  const args = config ? ['--config', config, 'gui'] : ['gui']
  if (preview) args.push('--preview')
  else if (address) args.push('--ipc-addr', address)
  else if (!auto) args.push('--no-auto')
  if (page) args.push('--preview-page', page)
  return { executable, args, cwd: directory }
}

export function main(argv = process.argv.slice(2)) {
  if (argv.length === 1 && ['--help', '-h'].includes(argv[0])) { console.log(help); return 0 }
  const launch = desktopInvocation(argv)
  const result = spawnSync(launch.executable, launch.args, { cwd: launch.cwd, stdio: 'inherit', windowsHide: true })
  if (result.error) throw result.error
  return result.status ?? 1
}

if (process.argv[1] && resolve(process.argv[1]) === script) {
  try { process.exitCode = main() }
  catch (error) { console.error(error.message); process.exitCode = 1 }
}
