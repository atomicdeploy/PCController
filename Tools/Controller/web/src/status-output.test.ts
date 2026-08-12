import { describe, expect, it } from 'vitest'
import { parseStatusCommandOutput } from './status-output'

const sample = 'uptime=2h30m1.583s supply=12.225V bus=12.197V current=272mA power=3357mW tLED=-327.68C tBT=-327.68C flags=0x08D3 running=false host_offline=false hot=false inputs=0xFF keys=0x00 relays=0x00 menu=6 mode=7 door=true bt=0 PWM available=true channel=15 value=3967 errors=0 LCD=0x00 framing=1 crc=0 reset_cause=0x08 reset_count=78'

describe('status command output', () => {
  it('parses the complete bridge status line into controller-native units', () => {
    expect(parseStatusCommandOutput(sample)).toEqual({
      uptime_ms: 9001583,
      uptime: '2h30m1.583s',
      supply_mv: 12225,
      bus_mv: 12197,
      current_ma: 272,
      power_mw: 3357,
      temperature_led_centi_c: -32768,
      temperature_bt_audio_centi_c: -32768,
      flags: 0x08d3,
      program_running: false,
      host_offline: false,
      hot: false,
      raw_inputs: 0xff,
      active_keys: 0,
      active_relays: 0,
      menu_page: 6,
      program_mode: 7,
      door_open: true,
      bluetooth_audio_state: 0,
      pwm_available: true,
      pwm_channel: 15,
      pwm_value: 3967,
      pwm_errors: 0,
      lcd_address: 0,
      framing_errors: 1,
      crc_errors: 0,
      reset_cause: 8,
      reset_count: 78,
    })
  })

  it('rejects unrelated terminal output', () => {
    expect(parseStatusCommandOutput('relay 5 on')).toBeNull()
    expect(parseStatusCommandOutput('uptime=1s supply=12V')).toBeNull()
    expect(parseStatusCommandOutput('uptime=nope supply=garbage relays=nope')).toBeNull()
    expect(parseStatusCommandOutput('uptime=1s supply=12V relays=0x00junk')).toBeNull()
  })
})
