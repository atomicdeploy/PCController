import { describe, expect, it } from 'vitest'
import { peripheralAvailability } from './peripheral-availability'
import { emptySnapshot } from './types'

const allPeripheralCapabilities = (1 << 0) | (1 << 1) | (1 << 2) | (1 << 3) | (1 << 4) |
  (1 << 5) | (1 << 6) | (1 << 7) | (1 << 8) | (1 << 9) | (1 << 11) | (1 << 20)

function liveSnapshot() {
  return {
    ...emptySnapshot,
    connected: true,
    have_status: true,
    hello: { ...emptySnapshot.hello, capabilities: allPeripheralCapabilities },
    status: {
      ...emptySnapshot.status,
      flags: 0x0f,
      ina219_available: true,
      temperature_led_available: true,
      temperature_bt_audio_available: true,
      pwm_available: true,
      supply_mv: 12_100,
      bus_mv: 12_000,
      current_ma: 320,
      power_mw: 3_840,
      temperature_led_centi_c: 3_600,
      temperature_bt_audio_centi_c: 3_200,
    },
    front_panel: { ...emptySnapshot.front_panel!, lcd_available: true, lcd_address: 0x27 },
    have_front_panel: true,
  }
}

describe('peer-advertised peripheral availability', () => {
  it('does not expose peripherals from disconnected, pending, or capability-free snapshots', () => {
    expect(Object.values(peripheralAvailability(emptySnapshot)).some(Boolean)).toBe(false)
    expect(Object.values(peripheralAvailability({ ...liveSnapshot(), have_status: false })).some(Boolean)).toBe(false)
    const withoutCapabilities = peripheralAvailability({ ...liveSnapshot(), hello: { capabilities: 0 } })
    expect(withoutCapabilities.ina219).toBe(false)
    expect(withoutCapabilities.temperatureLED).toBe(false)
    expect(withoutCapabilities.segments).toBe(false)
    expect(withoutCapabilities.lcd).toBe(false)
  })

  it('requires both a board capability and live availability evidence for measured devices', () => {
    const available = peripheralAvailability(liveSnapshot())
    expect(available).toMatchObject({
      ina219: true,
      temperatureLED: true,
      temperatureBTAudio: true,
      pwm: true,
      segments: true,
      lcd: true,
      relays: true,
      rf: true,
      statusLED: true,
      settings: true,
      menus: true,
      bluetoothAudio: true,
      buzzer: true,
    })
  })

  it('marks advertised invalid sensor values unavailable without substituting zero', () => {
    const invalid = peripheralAvailability({
      ...liveSnapshot(),
      status: {
        ...liveSnapshot().status,
        temperature_led_centi_c: -32_768,
        power_mw: Number.NaN,
      },
    })
    expect(invalid.ina219).toBe(false)
    expect(invalid.invalidINA219).toBe(true)
    expect(invalid.temperatureLED).toBe(false)
    expect(invalid.invalidTemperatureLED).toBe(true)
    expect(invalid.temperatureBTAudio).toBe(true)
  })
})
