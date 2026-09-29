#!/usr/bin/env node
// Produce the dependency PR's deterministic review plan from structured evidence.

import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadProjectEnv } from '../Build/env.mjs'

loadProjectEnv()

function escapeCell(value) {
  return String(value ?? '').replaceAll('|', '\\|').replaceAll('\n', ' ')
}

function releaseNotes(change) {
  const area = String(change.area ?? '').toLowerCase()
  const name = String(change.name ?? '').toLowerCase()
  const version = encodeURIComponent(String(change.resolved ?? change.compatible ?? '').replace(/^v|^u/u, ''))
  if (name.includes('urboot') || area.includes('bootloader')) return 'https://github.com/stefanrueger/urboot/releases'
  if (name.includes('minicore') || area.includes('core')) return 'https://github.com/MCUdude/MiniCore/releases'
  if (name.includes('firmware cli') || name.includes('arduino-cli')) return 'https://github.com/arduino/arduino-cli/releases'
  if (name === 'go' || area === 'go-toolchain') return `https://go.dev/doc/devel/release#go${version}`
  if (name.includes('node')) return `https://nodejs.org/en/blog/release/v${version}`
  if (name.includes('upx')) return 'https://github.com/upx/upx/releases'
  if (name.includes('go-winres')) return 'https://github.com/tc-hib/go-winres/releases'
  if (area.startsWith('npm-')) return `https://www.npmjs.com/package/${encodeURIComponent(change.name)}`
  if (area === 'go-module') return `https://pkg.go.dev/${change.name}@${change.resolved}`
  return ''
}

function securitySummary(security = {}) {
  const audits = security.npm ?? []
  if (!audits.length) return 'No package-manager security evidence was recorded; reviewer confirmation is required.'
  return audits.map((audit) => {
    const values = audit.vulnerabilities ?? {}
    const total = Number(values.total ?? Object.values(values).reduce((sum, value) => sum + Number(value || 0), 0))
    return `${audit.project}: ${total} known npm audit finding${total === 1 ? '' : 's'} ` +
      `(critical ${values.critical ?? 0}, high ${values.high ?? 0}, moderate ${values.moderate ?? 0}, low ${values.low ?? 0})`
  }).join('; ')
}

function sizeSummary(validation = []) {
  const memory = validation.find((item) => item.firmware_application_bytes)
  const host = validation.find((item) => item.host_executable_bytes)
  const parts = []
  if (memory) {
    parts.push(`firmware ${memory.firmware_application_bytes}/${memory.application_maximum_bytes} bytes ` +
      `(${memory.application_maximum_bytes - memory.firmware_application_bytes} free); ` +
      `Urboot-Custom ${memory.urboot_custom_bytes}/${memory.urboot_custom_allocated_bytes} bytes`)
  }
  if (host) parts.push(`compressed host executable ${host.host_executable_bytes} bytes (${host.upx_version ?? 'UPX verified'})`)
  return parts.length ? parts.join('; ') : 'Candidate size evidence is unavailable because full validation did not complete.'
}

function buildDependencyPRPlan(report, context = {}) {
  const changes = report.changes ?? []
  const identities = changes.filter((change) => !['host-lock'].includes(change.area))
  const links = [...new Set(changes.map(releaseNotes).filter(Boolean))].sort()
  const validationPassed = (report.validation ?? []).length > 0 &&
    (report.validation ?? []).every((item) => item.status === 'passed')
  const plan = {
    format: 'controller-dependency-pr-plan/v1',
    status: report.status ?? 'unknown',
    mode: report.mode ?? 'unknown',
    updates_available: Boolean(report.updates_available),
    updates_applied: Boolean(report.updates_applied),
    changes,
    release_notes: links,
    impact: {
      license: identities.length
        ? `Review upstream licenses and regenerated THIRD_PARTY_NOTICES.md for: ${identities.map((item) => item.name).join(', ')}.`
        : 'No dependency identity changed; only reproducibility hashes or metadata changed.',
      security: securitySummary(report.security),
      size: sizeSummary(report.validation),
    },
    validation_passed: validationPassed,
    source_report_sha256: createHash('sha256').update(JSON.stringify(report)).digest('hex'),
    run_url: context.runURL ?? '',
  }
  return plan
}

function renderDependencyPRMarkdown(plan) {
  const rows = plan.changes.map((item) =>
    `| ${escapeCell(item.area)} | ${escapeCell(item.name)} | \`${escapeCell(item.current)}\` | \`${escapeCell(item.resolved ?? item.compatible)}\` |`,
  )
  const validation = plan.validation_passed ? '✅ Full candidate validation passed' : '❌ Full candidate validation did not pass'
  return [
    '## ✅ Latest-compatible dependency candidate', '',
    `${validation}. Exact versions and hashes are captured in the attached dependency report and lock files.`, '',
    '| Area | Dependency | Current | Candidate |',
    '|---|---|---|---|',
    ...(rows.length ? rows : ['| — | No dependency change | — | — |']), '',
    '### Release notes', '',
    ...(plan.release_notes.length ? plan.release_notes.map((url) => `- ${url}`) : ['- No upstream identity changed.']), '',
    '### Impact review', '',
    `- **License:** ${plan.impact.license}`,
    `- **Security:** ${plan.impact.security}`,
    `- **Size:** ${plan.impact.size}`, '',
    '### Validation and review', '',
    '- [x] Stable channels resolved; prerelease and branch canaries remained observation-only.',
    `- [${plan.validation_passed ? 'x' : ' '}] Firmware, Urboot-Custom, VirtualBoard, Go, web, resources, and packaging gates passed.`,
    '- [ ] Reviewer inspected upstream release notes and any license/security changes.',
    '- [ ] Reviewer confirmed immutable GitHub Action revisions and generated lock provenance.', '',
    plan.run_url ? `Workflow evidence: ${plan.run_url}` : '',
  ].filter((line, index, values) => line !== '' || values[index - 1] !== '').join('\n').trim() + '\n'
}

function main(argv = process.argv.slice(2)) {
  const reportPath = resolve(argv[0] ?? '.build/dependencies/update-report.json')
  const planPath = resolve(argv[1] ?? '.build/dependencies/dependency-pr-plan.json')
  const markdownPath = resolve(argv[2] ?? '.build/dependencies/dependency-pr.md')
  const report = existsSync(reportPath)
    ? JSON.parse(readFileSync(reportPath, 'utf8'))
    : { status: 'failed', error: `report missing: ${reportPath}`, changes: [], validation: [] }
  const plan = buildDependencyPRPlan(report, { runURL: process.env.DEPENDENCY_RUN_URL ?? '' })
  mkdirSync(dirname(planPath), { recursive: true })
  mkdirSync(dirname(markdownPath), { recursive: true })
  writeFileSync(planPath, `${JSON.stringify(plan, null, 2)}\n`)
  writeFileSync(markdownPath, renderDependencyPRMarkdown(plan))
  process.stdout.write(`Dependency PR plan: ${planPath}\nDependency PR body: ${markdownPath}\n`)
}

export { buildDependencyPRPlan, releaseNotes, renderDependencyPRMarkdown, securitySummary, sizeSummary }

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()
