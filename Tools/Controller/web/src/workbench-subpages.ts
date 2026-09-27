import type { PeripheralDestinationID } from './peripheral-navigation'

export type WorkbenchContentID =
  | 'terminal'
  | 'displays'
  | 'status-lighting'
  | 'pwm'
  | 'power-sensor'
  | 'temperature-sensor'
  | 'audio'
  | 'radio'
  | 'macros'
  | 'automations'
  | 'i2c'
  | 'host-control'
  | 'firmware'

export type AdvancedWorkbenchPanelID =
  | 'connection'
  | 'temperature'
  | 'audio'
  | 'front-panel'
  | 'lighting'
  | 'radio'
  | 'i2c'
  | 'keyboard'
  | 'os'
  | 'services'
  | 'firmware'

const contentByDestination: Record<PeripheralDestinationID, readonly WorkbenchContentID[]> = {
  overview: ['terminal', 'host-control', 'firmware'],
  'lighting-status': ['status-lighting'],
  'lighting-pwm': ['pwm'],
  'sensors-ina219': ['power-sensor'],
  'sensors-temperature': ['temperature-sensor'],
  'interface-displays': ['displays', 'i2c'],
  'interface-audio': ['audio'],
  'automation-radio': ['radio'],
  'automation-macros': ['macros', 'automations'],
}

const panelsByDestination: Record<PeripheralDestinationID, readonly AdvancedWorkbenchPanelID[]> = {
  overview: ['connection', 'keyboard', 'os', 'services', 'firmware'],
  'lighting-status': ['lighting'],
  'lighting-pwm': [],
  'sensors-ina219': [],
  'sensors-temperature': ['temperature'],
  'interface-displays': ['front-panel', 'i2c'],
  'interface-audio': ['audio'],
  'automation-radio': ['radio'],
  'automation-macros': [],
}

export function workbenchContentVisible(destination: PeripheralDestinationID, content: WorkbenchContentID): boolean {
  return contentByDestination[destination].includes(content)
}

export function advancedWorkbenchPanelVisible(destination: PeripheralDestinationID, panel: AdvancedWorkbenchPanelID): boolean {
  return panelsByDestination[destination].includes(panel)
}

export function workbenchDestinationHasAdvancedContent(destination: PeripheralDestinationID): boolean {
  return panelsByDestination[destination].length > 0
}

export function workbenchDestinationContent(destination: PeripheralDestinationID): readonly WorkbenchContentID[] {
  return contentByDestination[destination]
}
