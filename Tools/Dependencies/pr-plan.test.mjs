import assert from 'node:assert/strict'
import test from 'node:test'

import { buildDependencyPRPlan, renderDependencyPRMarkdown } from './pr-plan.mjs'

const report = {
  status: 'passed', mode: 'apply', updates_available: true, updates_applied: true,
  changes: [
    { area: 'bootloader', name: 'Urboot', current: 'u8.0', resolved: 'u8.0.1' },
    { area: 'npm-web', name: 'vite', current: '7.0.0', resolved: '7.1.0' },
  ],
  security: { npm: [{ project: 'web', vulnerabilities: { total: 0, critical: 0, high: 0, moderate: 0, low: 0 } }] },
  validation: [
    { name: 'Memory ceilings', status: 'passed', firmware_application_bytes: 32000, application_maximum_bytes: 32256, urboot_custom_bytes: 510, urboot_custom_allocated_bytes: 512 },
    { name: 'Host package size', status: 'passed', host_executable_bytes: 5000000, upx_version: '5.2.0' },
  ],
}

test('PR plan is deterministic and carries release, license, security, and size review evidence', () => {
  const first = buildDependencyPRPlan(report, { runURL: 'https://example.test/run/1' })
  const second = buildDependencyPRPlan(structuredClone(report), { runURL: 'https://example.test/run/1' })
  assert.deepEqual(first, second)
  assert.equal(first.validation_passed, true)
  assert.match(first.impact.license, /Urboot/u)
  assert.match(first.impact.security, /0 known npm audit findings/u)
  assert.match(first.impact.size, /256 free/u)
  assert.ok(first.release_notes.some((url) => url.includes('/urboot/releases')))
  const markdown = renderDependencyPRMarkdown(first)
  for (const heading of ['Release notes', 'License:', 'Security:', 'Size:', 'Validation and review']) {
    assert.ok(markdown.includes(heading), `missing ${heading}`)
  }
})

test('failed or partial reports produce an honest non-passing plan', () => {
  const plan = buildDependencyPRPlan({ status: 'failed', changes: [], validation: [] })
  assert.equal(plan.validation_passed, false)
  assert.match(plan.impact.security, /reviewer confirmation is required/u)
  assert.match(plan.impact.size, /did not complete/u)
  assert.match(renderDependencyPRMarkdown(plan), /did not pass/u)
})
