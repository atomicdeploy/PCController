import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { UpdateOperationPanel } from './update-operation-panel'
import type { UpdateStatus } from './updates-api'

const sample: UpdateStatus = { id: 'op-proof', kind: 'firmware', state: 'writing', stage: 'writing', progress_percent: 42, progress_known: true }

describe('update operation presentation', () => {
  it('labels a measured bar as the current stage, not overall completion', () => {
    const html = renderToStaticMarkup(createElement(UpdateOperationPanel, { status: sample, locale: 'en' }))
    expect(html).toContain('Current stage')
    expect(html).toContain('aria-valuenow="42"')
  })
  it('renders indeterminate reconnect and removes the percentage', () => {
    const html = renderToStaticMarkup(createElement(UpdateOperationPanel, { status: { ...sample, state: 'reconnecting', stage: 'reconnecting', progress_known: false }, locale: 'en' }))
    expect(html).toContain('is-indeterminate')
    expect(html).not.toContain('aria-valuenow')
    expect(html).not.toContain('42%')
  })
  it('preserves the full failure and failed stage without a stale progress bar', () => {
    const detail = 'AVRDUDE executable was not found at the configured location. Check programming.avrdude.'
    const html = renderToStaticMarkup(createElement(UpdateOperationPanel, { status: { ...sample, state: 'failed', stage: 'preflight', detail }, locale: 'en' }))
    expect(html).toContain(detail)
    expect(html).toContain('Stopped during')
    expect(html).toContain('role="alert"')
    expect(html).not.toContain('role="progressbar"')
  })
})
