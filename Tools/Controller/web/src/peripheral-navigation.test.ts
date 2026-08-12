import { describe, expect, it } from 'vitest'
import {
  canonicalPeripheralHash,
  peripheralDestinationByID,
  peripheralDestinationFromHash,
  peripheralDestinationState,
  peripheralDestinations,
} from './peripheral-navigation'
import { emptySnapshot } from './types'

describe('peripheral workbench navigation', () => {
  it('accepts only fixed canonical workbench destinations', () => {
    expect(canonicalPeripheralHash('lighting-pwm')).toBe('#/workbench/lighting/pwm')
    expect(peripheralDestinationFromHash('#/workbench')).toBe('overview')
    expect(peripheralDestinationFromHash('#/workbench/lighting/pwm')).toBe('lighting-pwm')
    expect(peripheralDestinationFromHash('#/workbench/lighting/free-text')).toBeNull()
    expect(peripheralDestinationFromHash('#/controls')).toBeNull()
    expect(new Set(peripheralDestinations.map((destination) => destination.path)).size).toBe(peripheralDestinations.length)
  })

  it('keeps known destinations visible while accurately reporting transport and capability state', () => {
    const pwm = peripheralDestinationByID('lighting-pwm')
    expect(peripheralDestinationState(pwm, emptySnapshot)).toBe('disconnected')
    expect(peripheralDestinationState(pwm, { ...emptySnapshot, connected: true, have_status: false })).toBe('pending')
    expect(peripheralDestinationState(pwm, { ...emptySnapshot, connected: true, have_status: true, status: { ...emptySnapshot.status, pwm_available: false } })).toBe('unsupported')
    expect(peripheralDestinationState(pwm, { ...emptySnapshot, connected: true, have_status: true, status: { ...emptySnapshot.status, pwm_available: true } })).toBe('available')
  })
})
