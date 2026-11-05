import type { PeripheralSettings, PresentationUpdateResult } from './types'
import { validatePeripheralName } from './settings-validation'

export interface ChannelNameDraft {
  catalog: PeripheralSettings
  saved: Record<string,string>
  values: Record<string,string>
  conflict: boolean
}

export function channelNameDraft(catalog: PeripheralSettings): ChannelNameDraft {
  if (!catalog.board_profile?.revision || !Array.isArray(catalog.peripherals) || !Array.isArray(catalog.controls)) throw new Error('Incomplete channel catalog')
  const saved = Object.fromEntries(catalog.peripherals.map(item => [item.key,item.name?.trim() || item.default_name]))
  return {catalog,saved,values:{...saved},conflict:false}
}

export function changedChannelNames(state: ChannelNameDraft): Array<[string,string]> {
  return state.catalog.peripherals.flatMap(item => {
    const value = validatePeripheralName(state.values[item.key] ?? '').normalized || item.default_name
    return value === state.saved[item.key] ? [] : [[item.key,value] as [string,string]]
  })
}

export function receiveChannelNames(current: ChannelNameDraft | null, catalog: PeripheralSettings): ChannelNameDraft {
  const next = channelNameDraft(catalog)
  if (!current || !changedChannelNames(current).length) return next
  if (catalog.board_profile.revision !== current.catalog.board_profile.revision ||
      catalog.board_profile.board_identity !== current.catalog.board_profile.board_identity) {
    return {...current,conflict:true} // Preserve draft and its original revision.
  }
  return current
}

export function acknowledgeChannelName(state: ChannelNameDraft, key: string, result: PresentationUpdateResult): ChannelNameDraft {
  if (!result.board_profile?.revision || result.board_profile.board_identity !== state.catalog.board_profile.board_identity || result.peripheral.key !== key) {
    throw new Error('Rename acknowledgement does not match this board channel')
  }
  const value = result.peripheral.name?.trim() || result.peripheral.default_name
  return {
    ...state,
    catalog: {...state.catalog,board_profile:result.board_profile,controls:result.controls,
      peripherals:state.catalog.peripherals.map(item=>item.key===key?result.peripheral:item)},
    saved:{...state.saved,[key]:value},values:{...state.values,[key]:value},
  }
}

// Each ACK advances the revision and only that row's saved baseline. A failure
// leaves the remaining draft intact and never repeats an uncertain mutation.
export async function saveChannelNames(
  initial: ChannelNameDraft,
  write: (key:string,name:string,revision:string)=>Promise<PresentationUpdateResult>,
  acknowledged: (value:ChannelNameDraft)=>void,
): Promise<void> {
  if (initial.conflict || !initial.catalog.board_profile.attached || !initial.catalog.board_profile.configured) throw new Error('Reload the current board names before saving')
  let current=initial
  for (const [key,name] of changedChannelNames(initial)) {
    current=acknowledgeChannelName(current,key,await write(key,name,current.catalog.board_profile.revision))
    acknowledged(current)
  }
}
