import { describe, expect, it } from 'vitest'
import { rebindChannel, semanticKeyValid } from './channel-routing'
import { semanticObservedValue } from './semantic-channel-controls'
import type { ChannelRouting } from './channel-routing'
import type { Snapshot } from './types'

const routing: ChannelRouting = {enabled: true, channels: [
  {key:'lighting.aisle', kind:'relay', name:'Aisle', icon:'lamp', target:'relay.5'},
  {key:'lighting.screen', kind:'relay', name:'Screen', target:'relay.6'},
  {key:'lighting.front', kind:'pwm', name:'Front', target:'pwm.0'},
]}

describe('semantic channel wiring', () => {
  it('swaps both bindings atomically without moving names or icons', () => {
    const result = rebindChannel(routing, 'lighting.aisle', 'relay.6')
    expect(result.channels[0]).toEqual({...routing.channels[0], target:'relay.6'})
    expect(result.channels[1]).toEqual({...routing.channels[1], target:'relay.5'})
    expect(routing.channels[0].target).toBe('relay.5')
  })
  it('supports rebind to an unused compatible output without changing peers', () => {
    const result = rebindChannel(routing, 'lighting.aisle', 'relay.7')
    expect(result.channels[0].target).toBe('relay.7')
    expect(result.channels[1]).toEqual(routing.channels[1])
    expect(rebindChannel(routing, 'lighting.aisle', 'pwm.0')).toBe(routing)
  })
  it('rejects keys that could masquerade as physical outputs or malformed paths', () => {
    for (const key of ['relay.5','pwm.0','seat.a','sensor.temperature','Upper',' x','a..b','a.','<x>','']) expect(semanticKeyValid(key), key).toBe(false)
    for (const key of ['lighting.aisle','fan','screen-left']) expect(semanticKeyValid(key), key).toBe(true)
  })
  it('reads the current physical binding and never invents unknown output values', () => {
    const snapshot = {connected:true,have_status:true,status:{active_relays:1<<5},pwm_values:{available:true,values:[123]}} as Snapshot
    expect(semanticObservedValue(routing.channels[0], snapshot)).toBe(0)
    const swapped = rebindChannel(routing, 'lighting.aisle', 'relay.6')
    expect(semanticObservedValue(swapped.channels[0], snapshot)).toBe(1)
    expect(semanticObservedValue(routing.channels[2], snapshot)).toBe(123)
    expect(semanticObservedValue(routing.channels[2], {...snapshot,pwm_values:undefined})).toBeNull()
    expect(semanticObservedValue(routing.channels[0], {...snapshot,have_status:false})).toBeNull()
    expect(semanticObservedValue(routing.channels[0], {...snapshot,connected:false})).toBeNull()
  })
})
