import { describe, expect, it } from 'vitest'
import { responseErrorDetail } from './api'

describe('API error details', () => {
  it('extracts nested JSON-RPC messages returned with an HTTP error status', () => {
    expect(responseErrorDetail({
      jsonrpc: '2.0',
      error: { code: -32000, message: 'unknown command "sample"' },
    }, 'Bad Request', 400)).toBe('unknown command "sample"')
  })

  it('retains text and HTTP fallbacks', () => {
    expect(responseErrorDetail('not allowed', 'Forbidden', 403)).toBe('not allowed')
    expect(responseErrorDetail(null, 'Not Found', 404)).toBe('Not Found')
  })
})
