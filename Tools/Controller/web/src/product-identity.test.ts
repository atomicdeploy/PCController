import { describe, expect, it } from 'vitest'
import { effectiveProductTitle, productMark } from './product-identity'

describe('product identity', () => {
  it('uses the live app title instead of the package fallback', () => {
    expect(effectiveProductTitle(' Workshop Controller ', 'Package Default')).toBe('Workshop Controller')
    expect(productMark('Workshop Controller', 'PC')).toBe('WC')
  })

  it('uses package metadata when no live title is available', () => {
    expect(effectiveProductTitle(' ', 'Package Default')).toBe('Package Default')
    expect(productMark('', 'PD')).toBe('PD')
  })
})
