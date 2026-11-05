import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ChannelIcon, channelIconGlyph } from './channel-icon'
import { resolvedControlIcon, resolvedControlName, motionControlKey } from './control-descriptors'
import { channelPresentationEventID, channelPresentationOwner } from './use-channel-settings'
import { isSignificantControllerEvent, prependSignificantControllerEvent } from './significant-events'
import type { ControlDescriptor, ControllerEvent, Snapshot } from './types'
import glyphs from './channel-icon-glyphs.json'

describe('global channel presentation', () => {
  const controls: ControlDescriptor[] = [
    { key:'motion.a',kind:'side',order:0,name:'Left lift',icon:'seat',control:'motion' },
    { key:'pwm.0',kind:'mosfet',order:1,name:'Window',icon:'fan',control:'pwm-user' },
  ]
  it('resolves configured motion names and PWM icons by the canonical key', () => {
    expect(resolvedControlName(controls,'motion.a','fallback')).toBe('Left lift')
    expect(resolvedControlIcon(controls,'motion.a')).toBe('seat')
    expect(resolvedControlIcon(controls,'pwm.0')).toBe('fan')
    expect(resolvedControlIcon(controls,'seat.a')).toBeUndefined()
  })
  it('uses the cinema profile seat key without treating it as a generic motion alias', () => {
    const cinema = [{...controls[0],key:'seat.a',kind:'seat',control:'seat'}] as ControlDescriptor[]
    expect(motionControlKey(cinema,'left')).toBe('seat.a')
    expect(motionControlKey(controls,'left')).toBe('motion.a')
    expect(motionControlKey(cinema,'right')).toBeUndefined()
  })
  it('retains catalog STATE events through view and cross-tab filtering', () => {
    const event: ControllerEvent = {id:42,kind:'peripherals.changed',stream:'state',text:'changed',time:''}
    expect(isSignificantControllerEvent(event)).toBe(true)
    const events = prependSignificantControllerEvent([],event)
    expect(channelPresentationEventID(events)).toBe('/42')
    expect(channelPresentationEventID(prependSignificantControllerEvent(events,event))).toBe('/42')
    expect(isSignificantControllerEvent({...event,kind:'status_led.changed'})).toBe(false)
  })
  it('separates hosts and board generations even with the same port name', () => {
    const board = {host_instance_id:'host-a',connection_generation:4,port:{name:'COM1'}} as Snapshot
    expect(channelPresentationOwner(board)).not.toBe(channelPresentationOwner({...board,host_instance_id:'host-b'}))
    expect(channelPresentationOwner(board)).not.toBe(channelPresentationOwner({...board,connection_generation:5}))
  })
  it('renders Pealayer font glyphs, including non-preset symbols, without substituting generic icons', () => {
    expect(Object.keys(glyphs).length).toBe(1530)
    for (const key of ['seat','fan','chandelier','garden','acorn','address-book-tabs','snowflake']) {
      expect(channelIconGlyph(key)).toBeTruthy()
      expect(renderToStaticMarkup(<ChannelIcon icon={key}/>)).toContain('class="channel-icon"')
    }
    expect(channelIconGlyph('chandelier')).toBe(channelIconGlyph('lamp'))
    expect(renderToStaticMarkup(<ChannelIcon icon="unknown-symbol"/>)).toContain('[unknown-symbol]')
    expect(renderToStaticMarkup(<ChannelIcon icon=""/>)).toBe('')
  })
})
