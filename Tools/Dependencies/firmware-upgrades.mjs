// Share core/library upgrade evidence between the updater, job summary and PR.
export function firmwareUpgradeChecks(policy, current, changes = []) {
  const declared = [
    { kind: 'core', area: 'firmware', name: policy.core.id, current: current.firmware.core_version },
    ...policy.libraries.map(library => ({
      kind: 'library', area: 'library', name: library.name,
      current: current.firmware.libraries.find(item => item.name === library.name)?.version,
    })),
  ]
  return declared.map(item => {
    if (!item.current) throw new Error(`Missing locked firmware dependency: ${item.name}`)
    const change = changes.find(candidate => candidate.area === item.area && candidate.name === item.name)
    return { ...item, resolved: change?.resolved ?? item.current, changed: Boolean(change) }
  })
}

export function recordFirmwareResolution(report, policy, current, result) {
  report.firmware_checks = firmwareUpgradeChecks(policy, current, result.changes ?? [])
  report.canary = result.canary ?? {}
  report.changes.push(...(result.changes ?? []))
  report.updates_available = report.changes.length !== 0
  // A later host validation failure must not hide an already-written candidate lock.
  // This flag describes the disposable candidate checkout, never deployed hardware.
  report.updates_applied = Boolean(result.written)
}

export function groupDependencyChanges(changes = []) {
  const core = changes.filter(item => item.area === 'firmware' && /^[^:]+:[^:]+$/u.test(item.name))
  const libraries = changes.filter(item => item.area === 'library')
  const other = changes.filter(item => !core.includes(item) && !libraries.includes(item))
  return { core, libraries, other }
}

export function dependencyPRTitle(changes = []) {
  const { core, libraries, other } = groupDependencyChanges(changes)
  const names = []
  if (core.length) names.push(`${core.length} core${core.length === 1 ? '' : 's'}`)
  if (libraries.length) names.push(`${libraries.length} librar${libraries.length === 1 ? 'y' : 'ies'}`)
  if (!names.length) return '⬆️ Refresh latest-compatible dependencies'
  return `⬆️ Update ${names.join(' and ')}${other.length ? ' and related dependencies' : ''}`
}

export function renderFirmwareChecks(checks) {
  if (!checks?.length) return ''
  const cell = value => String(value ?? '').replaceAll('|', '\\|').replace(/[\r\n`]/gu, ' ')
  return [
    '## 🔎 Core and library checks', '',
    '| Kind | Dependency | Baseline | Latest stable candidate | Result |',
    '|---|---|---|---|---|',
    ...checks.map(item => `| ${cell(item.kind)} | ${cell(item.name)} | \`${cell(item.current)}\` | \`${cell(item.resolved)}\` | ${cell(item.result ?? (item.changed ? 'Update available' : 'Current'))} |`),
    '',
  ].join('\n')
}
