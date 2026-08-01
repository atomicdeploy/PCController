#!/usr/bin/env node
// Convert the structured dependency report into a concise GitHub job summary.

import { appendFileSync, existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const path = resolve(process.argv[2] ?? '.build/dependencies/update-report.json')
const report = existsSync(path)
  ? JSON.parse(readFileSync(path, 'utf8'))
  : { status: 'failed', error: `report missing: ${path}`, changes: [], validation: [] }
const rows = (report.changes ?? []).map((item) =>
  `| ${item.area ?? ''} | ${item.name ?? ''} | \`${item.current ?? ''}\` | \`${item.resolved ?? item.compatible ?? ''}\` |`,
)
const validations = (report.validation ?? []).map((item) =>
  `| ${item.status === 'passed' ? '✅' : '❌'} | ${item.name} | ${item.firmware_application_bytes ? `${item.firmware_application_bytes}/${item.application_maximum_bytes} firmware bytes; ${item.urboot_custom_bytes}/${item.urboot_custom_allocated_bytes} boot bytes` : item.status} |`,
)
const output = [
  '# 🔄 Dependency resolution', '',
  `**Status:** ${report.status === 'passed' ? '✅ passed' : '❌ failed'}`,
  `**Mode:** ${report.mode ?? 'unknown'}`,
  `**Updates available:** ${report.updates_available ? 'yes' : 'no'}`,
  `**Updates applied:** ${report.updates_applied ? 'yes' : 'no'}`,
  report.error ? `**Error:** \`${String(report.error).slice(0, 2000)}\`` : '', '',
  '## Stable dependency changes', '',
  '| Area | Dependency | Current | Resolved |',
  '|---|---|---|---|',
  ...(rows.length ? rows : ['| — | No stable changes | — | — |']), '',
  '## Validation', '',
  '| Result | Gate | Evidence |',
  '|:---:|---|---|',
  ...(validations.length ? validations : ['| — | Not run | — |']), '',
  '## Observation-only canaries', '',
  `- CLI: \`${report.canary?.cli_release || 'none'}\``,
  `- Urboot: \`${report.canary?.bootloader_ref || 'none'}@${report.canary?.bootloader_commit || 'none'}\``, '',
].filter((line) => line !== '').join('\n') + '\n'

if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, output)
else process.stdout.write(output)
