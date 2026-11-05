import type { BoardProfileDescriptor, PeripheralDescriptor } from './types'

export interface SemanticChannel {
  key: string
  kind: 'relay' | 'pwm'
  target: string
  name: string
  icon?: string
}
export interface ChannelRouting { enabled: boolean; channels: SemanticChannel[] }
export interface ChannelRoutingCatalog {
  host_instance_id: string
  connection_generation: number
  board_profile: BoardProfileDescriptor
  routing: ChannelRouting
  targets: PeripheralDescriptor[]
}

export function routingDirty(a: ChannelRouting, b: ChannelRouting): boolean {
  return JSON.stringify(a) !== JSON.stringify(b)
}

// Selecting an occupied compatible output swaps both bindings in the draft.
// Save sends the complete pair in one revision-fenced host transaction.
export function rebindChannel(routing: ChannelRouting, key: string, target: string): ChannelRouting {
  const current = routing.channels.find(channel => channel.key === key)
  if (!current || !target.startsWith(`${current.kind}.`)) return routing
  const occupied = routing.channels.find(channel => channel.target === target)
  if (occupied && occupied.kind !== current.kind) return routing
  return {...routing, channels: routing.channels.map(channel => channel.key === key
    ? {...channel, target}
    : occupied && channel.key === occupied.key ? {...channel, target: current.target} : channel)}
}

export function semanticKeyValid(key: string): boolean {
  return /^[a-z][a-z0-9._-]{0,63}$/.test(key) && !key.endsWith('.') && !key.includes('..') &&
    !/^(relay|pwm|seat|motion|display|sensor|strip)(\.|$)/.test(key)
}
