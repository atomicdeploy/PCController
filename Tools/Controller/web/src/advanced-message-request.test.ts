import { describe, expect, it } from 'vitest'

import { advancedMessageRequest } from './advanced-workbench'

describe('Advanced Workbench message requests', () => {
  it('uses the canonical ordered targets array for host messages', () => {
    const request = advancedMessageRequest('host', 'operator.notice', 'Commissioning is ready')

    expect(request).toEqual({
      targets: ['host'],
      type: 'operator.notice',
      text: 'Commissioning is ready',
    })
    expect(request).not.toHaveProperty('target')
  })

  it('uses the canonical ordered targets array and LCD lines for board messages', () => {
    const request = advancedMessageRequest('lcd', 'operator.notice', 'Commissioning', 'is ready')

    expect(request).toEqual({
      targets: ['lcd'],
      type: 'operator.notice',
      text: 'Commissioning\nis ready',
      line1: 'Commissioning',
      line2: 'is ready',
    })
    expect(request).not.toHaveProperty('target')
  })
})
