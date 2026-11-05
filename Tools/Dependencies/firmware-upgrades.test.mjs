import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { dependencyPRTitle, firmwareUpgradeChecks, groupDependencyChanges, recordFirmwareResolution, renderFirmwareChecks } from './firmware-upgrades.mjs'

const policy = { core: { id: 'MiniCore:avr' }, libraries: [{ name: 'Example' }, { name: 'Unchanged' }] }
const lock = { firmware: { core_version: '3.1.3', libraries: [{ name: 'Example', version: '1.9.0' }, { name: 'Unchanged', version: '2.0.0' }] } }
const changes = [
  { area: 'firmware', name: 'MiniCore:avr', current: '3.1.3', resolved: '3.2.0' },
  { area: 'library', name: 'Example', current: '1.9.0', resolved: '1.10.0' },
  { area: 'library-artifact', name: 'Example', current: 'old-hash', resolved: 'new-hash' },
]

test('every declared core/library is reported, including unchanged libraries', () => {
  const checks = firmwareUpgradeChecks(policy, lock, changes)
  assert.equal(checks.length, 3)
  assert.equal(checks[1].resolved, '1.10.0')
  assert.equal(checks[2].changed, false)
  assert.equal(checks[2].resolved, '2.0.0')
  assert.match(renderFirmwareChecks(checks), /Unchanged.*Current/u)
  assert.throws(() => firmwareUpgradeChecks(policy, { firmware: { core_version: '3.1.3', libraries: [] } }), /Missing locked firmware dependency/u)
})

test('a completed firmware check retains update evidence before a later host failure', () => {
  const report = { changes: [], updates_available: false, updates_applied: false }
  recordFirmwareResolution(report, policy, lock, { changes, written: true })
  report.status = 'failed'
  report.error = 'host registry unavailable'
  assert.equal(report.updates_available, true)
  assert.equal(report.updates_applied, true)
  assert.equal(report.firmware_checks.length, 3)
  const checkOnly = { changes: [] }
  recordFirmwareResolution(checkOnly, policy, lock, { changes, written: false })
  assert.equal(checkOnly.updates_applied, false)
  assert.deepEqual(lock.firmware.libraries[0], { name: 'Example', version: '1.9.0' })
})

test('PU850-style titles count version changes, not duplicate archive hashes', () => {
  assert.equal(dependencyPRTitle(changes), '⬆️ Update 1 core and 1 library and related dependencies')
  assert.equal(dependencyPRTitle([changes[0]]), '⬆️ Update 1 core')
  assert.equal(dependencyPRTitle([changes[1]]), '⬆️ Update 1 library')
  assert.equal(dependencyPRTitle([]), '⬆️ Refresh latest-compatible dependencies')
  assert.equal(groupDependencyChanges(changes).libraries.length, 1)
})

test('daily workflow retains stable resolution, check-only mode and validation before PR creation', () => {
  const workflow = readFileSync(new URL('../../.github/workflows/update-dependencies.yml', import.meta.url), 'utf8')
  assert.match(workflow, /cron: "17 1 \* \* \*"/u)
  assert.match(workflow, /update-dependencies\.cmd --check --report/u)
  assert.match(workflow, /update-dependencies\.cmd --apply --validate --report/u)
  assert.match(workflow, /title: \$\{\{ steps\.review-plan\.outputs\.title \}\}/u)
  assert.match(workflow, /if: steps\.candidate\.outcome == 'success' && env\.CHECK_ONLY != 'true'/u)
  const updater = readFileSync(new URL('./update.mjs', import.meta.url), 'utf8')
  assert.ok(updater.indexOf('recordFirmwareResolution(report,') < updater.indexOf('let hostTools = resolveHostTools'))
  const sourcePolicy = JSON.parse(readFileSync(new URL('../Controller/toolchain-profile.json', import.meta.url), 'utf8'))
  for (const dep of [sourcePolicy.core, ...sourcePolicy.libraries]) {
    assert.equal(dep.constraint.channel, 'stable')
    assert.ok(dep.constraint.minimum_version)
    assert.equal(dep.version, undefined, 'selection must not freeze to one version')
    assert.equal(dep.constraint.maximum_version, undefined)
  }
})

test('job summary and PR output expose partial evidence without claiming a successful update', t => {
  const directory = mkdtempSync(join(tmpdir(), 'pccontroller-upgrade-report-'))
  t.after(() => rmSync(directory, { recursive: true, force: true }))
  const reportPath = join(directory, 'report.json')
  const report = { status: 'failed', error: 'host registry unavailable', changes: [], validation: [] }
  recordFirmwareResolution(report, policy, lock, { changes, written: false })
  writeFileSync(reportPath, JSON.stringify(report))
  const summary = spawnSync(process.execPath, ['Tools/Dependencies/workflow-summary.mjs', reportPath], {
    encoding: 'utf8', env: { ...process.env, GITHUB_STEP_SUMMARY: '' }, windowsHide: true,
  })
  assert.equal(summary.status, 0, summary.stderr)
  assert.match(summary.stdout, /\*\*Status:\*\* ❌ failed/u)
  assert.match(summary.stdout, /\*\*Updates available:\*\* yes/u)
  assert.match(summary.stdout, /\n\n## 🔎 Core and library checks\n\n/u)
  const outputPath = join(directory, 'outputs.txt')
  const plan = spawnSync(process.execPath, ['Tools/Dependencies/pr-plan.mjs', reportPath, join(directory, 'plan.json'), join(directory, 'body.md')], {
    encoding: 'utf8', env: { ...process.env, GITHUB_OUTPUT: outputPath }, windowsHide: true,
  })
  assert.equal(plan.status, 0, plan.stderr)
  assert.match(readFileSync(outputPath, 'utf8'), /^title=⬆️ Update 1 core and 1 library/u)
  const body = readFileSync(join(directory, 'body.md'), 'utf8')
  assert.match(body, /### 🔧 Core updates/u)
  assert.match(body, /### 📚 Library updates/u)
  assert.match(body, /validation did not pass/u)
})
