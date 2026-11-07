#!/usr/bin/env node
// Real application renderers, isolated offline fixtures, no board connection.
import { spawnSync } from 'node:child_process'
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadProjectEnv } from './env.mjs'

loadProjectEnv()

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const args = new Set(process.argv.slice(2))
if ([...args].some(x => !['--tui-only', '--help'].includes(x))) throw new Error('Unknown option; use --help')
if (args.has('--help')) {
  console.log('node Tools/Build/update-readme-previews.mjs [--tui-only]\nWindows: builds stable-path capture tools, renders isolated GUI fixtures and TUI SVG, then updates the README gallery. No hardware, user configuration, installed apps or network sessions are touched.')
  process.exit(0)
}
const windows = process.platform === 'win32'
if (!windows && !args.has('--tui-only')) throw new Error('GUI capture needs Windows; use --tui-only elsewhere')
const tools = windows ? join(process.env.LOCALAPPDATA, 'PCController', 'tools', 'documentation') : join(root, '.build', 'documentation-tools')
const assets = join(root, 'docs', 'assets', 'screenshots')
mkdirSync(tools, { recursive: true }); mkdirSync(assets, { recursive: true })
mkdirSync(join(root, '.build'), { recursive: true })
// Failed rendering must not replace the checked-in gallery with partial output.
const staging = mkdtempSync(join(root, '.build', 'readme-previews-'))
process.on('exit', () => rmSync(staging, { recursive: true, force: true }))
const env = { ...process.env, GOWORK: 'off', GOFLAGS: '-mod=mod', CGO_ENABLED: '0', CLICOLOR_FORCE: '1', COLORTERM: 'truecolor' }
function run(command, argv, cwd, capture = false, extra = {}) {
  const result = spawnSync(command, argv, { cwd, env: { ...env, ...extra }, encoding: 'utf8', stdio: capture ? 'pipe' : 'inherit', maxBuffer: 16 * 1024 * 1024, timeout: 300_000 })
  if (result.error || result.status !== 0) throw new Error(`${command} failed (${result.status}): ${result.error?.message ?? result.stderr ?? ''}`)
  return result.stdout
}
const tui = join(tools, `tui-preview${windows ? '.exe' : ''}`)
run('go', ['build', '-o', tui, './cmd/tui-preview'], join(root, 'Tools', 'Controller'))
for (const page of ['dashboard', 'controls']) {
  const svg = run(tui, ['--page', page, '--width', '112', '--height', '34', '--format', 'svg'], root, true)
  if (!svg.startsWith('<svg ') || !svg.includes('</svg>')) throw new Error(`Invalid ${page} SVG`)
  writeFileSync(join(staging, `tui-${page}.svg`), svg)
}
if (!args.has('--tui-only')) {
  const capture = join(tools, 'window-capture.exe')
  run('go', ['build', '-mod=vendor', '-o', capture, './tools/window-capture'], join(root, 'Tools', 'NativeDesktop'))
  run(process.execPath, ['Tools/Build/go-tests.mjs', '--module', 'Tools/NativeDesktop', '--package', 'internal/ui', '--run', '^TestControllerDocumentationGallery$', '--retest'], root, false, {
    GOFLAGS: '-mod=vendor', PCCONTROLLER_DESKTOP_WINDOW_TESTS: '1', PCCONTROLLER_DESKTOP_CAPTURE_TOOL: capture, PCCONTROLLER_DESKTOP_SCREENSHOTS: staging, PCCONTROLLER_DESKTOP_CAPTURE_MARGIN: '0'
  })
  run(process.execPath, ['Tools/Build/go-tests.mjs', '--module', 'Tools/NativeDesktop', '--package', 'internal/ui', '--run', '^TestControllerActivityFormNativeLayout$', '--retest'], root, false, {
    GOFLAGS: '-mod=vendor', PCCONTROLLER_DESKTOP_WINDOW_TESTS: '1', PCCONTROLLER_DESKTOP_CAPTURE_TOOL: capture, PCCONTROLLER_DESKTOP_SCREENSHOTS: staging
  })
  const fixtures = join(staging, 'control-fixtures')
  run(process.execPath, ['Tools/Build/go-tests.mjs', '--package', 'cmd/controller', '--run', '^TestNativeControlFixtureDocuments$', '--retest'], root, false, {
    PCCONTROLLER_NATIVE_PAGE_FIXTURES: fixtures
  })
  run(process.execPath, ['Tools/Build/go-tests.mjs', '--module', 'Tools/NativeDesktop', '--package', 'internal/ui', '--run', '^TestController(AutomationTriggerLayout|AutomationActionsLayout|MelodyEditorLayout|PeripheralEditorLayout|LCDPresentationLayout)$', '--retest'], root, false, {
    GOFLAGS: '-mod=vendor', PCCONTROLLER_DESKTOP_WINDOW_TESTS: '1', PCCONTROLLER_DESKTOP_CAPTURE_TOOL: capture, PCCONTROLLER_DESKTOP_SCREENSHOTS: staging, PCCONTROLLER_NATIVE_PAGE_FIXTURES: fixtures
  })
}
const start = '<!-- controller-preview-gallery:start -->', end = '<!-- controller-preview-gallery:end -->'
const gallery = `${start}
## 🖥️ Desktop and terminal interfaces

These are reproducible **offline previews**, rendered by the actual application UI. They do not certify a live board connection.

| Acrylic desktop | Channel cards |
| --- | --- |
| ![PCController desktop](docs/assets/screenshots/gui-dashboard.png) | ![Hardware channels](docs/assets/screenshots/gui-channels.png) |

| Inline channel rename | Windows titlebar |
| --- | --- |
| ![Aligned inline channel rename](docs/assets/screenshots/gui-inline-rename.png) | ![Compact Windows titlebar](docs/assets/screenshots/gui-windows-titlebar.png) |

| Compact front panel | Detachable card windows |
| --- | --- |
| ![Compact front panel](docs/assets/screenshots/gui-compact.png) | ![Independent acrylic widgets](docs/assets/screenshots/gui-widgets.png) |

### Desktop preferences

![PCController desktop preferences](docs/assets/screenshots/gui-settings.png)

### Native activities

![Native RF activity with tabbed editors and fixed actions](docs/assets/screenshots/gui-rf-activity.png)

| Automation triggers | Ordered automation actions |
| --- | --- |
| ![Native trigger editor](docs/assets/screenshots/gui-automation-0-en.png) | ![Native action editor](docs/assets/screenshots/gui-automation-1-en.png) |

| Melody notes | Peripheral presentation |
| --- | --- |
| ![Native melody editor](docs/assets/screenshots/gui-melody-0-en.png) | ![Native peripheral editor](docs/assets/screenshots/gui-peripheral-0-en.png) |

![Native LCD presentation](docs/assets/screenshots/gui-lcd-0-en.png)

| TUI dashboard | TUI Control page |
| --- | --- |
| ![PCController TUI dashboard](docs/assets/screenshots/tui-dashboard.svg) | ![PCController TUI Control page](docs/assets/screenshots/tui-controls.svg) |

Regenerate on Windows with \`screenshots.cmd\`, \`.\\screenshots.ps1\`, or \`node Tools/Build/update-readme-previews.mjs\`. On other systems use \`bash screenshots.sh --tui-only\` for terminal assets.
${end}`
const readmePath = join(root, 'README.md')
const readme = readFileSync(readmePath, 'utf8')
const a = readme.indexOf(start), b = readme.indexOf(end)
if ((a < 0) !== (b < 0) || (a >= 0 && b < a)) throw new Error('Malformed README gallery markers; refusing to rewrite')
if (a !== readme.lastIndexOf(start) || b !== readme.lastIndexOf(end)) throw new Error('Duplicate README gallery markers; refusing to rewrite')
const files = ['tui-dashboard.svg', 'tui-controls.svg']
if (!args.has('--tui-only')) {
  for (const name of ['dashboard', 'channels', 'inline-rename', 'windows-titlebar', 'compact', 'widgets', 'settings', 'rf-activity', 'automation-0-en', 'automation-1-en', 'melody-0-en', 'peripheral-0-en', 'lcd-0-en']) {
    const file = `gui-${name}.png`
    const png = readFileSync(join(staging, file))
    if (png.length < 100 || png.subarray(0, 8).toString('hex') !== '89504e470d0a1a0a') throw new Error(`Invalid ${file}`)
    files.push(file)
  }
}
let updated = readme
// A TUI-only run refreshes assets without introducing missing GUI links.
if (!args.has('--tui-only')) {
  updated = a >= 0 ? readme.slice(0, a) + gallery + readme.slice(b + end.length) : readme.replace('## 🧭 System architecture', `${gallery}\n\n## 🧭 System architecture`)
  if (!updated.includes(start)) throw new Error('README insertion anchor missing')
}
for (const file of files) copyFileSync(join(staging, file), join(assets, file))
if (updated !== readme) writeFileSync(readmePath, updated)
console.log(`Updated offline previews in ${assets}${args.has('--tui-only') ? '' : ' and README.md'}`)
