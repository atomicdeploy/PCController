import { describe, expect, it } from 'vitest'
import { peripheralDestinations } from './peripheral-navigation'
import {
  advancedWorkbenchPanelVisible,
  workbenchDestinationContent,
  workbenchDestinationHasAdvancedContent,
} from './workbench-subpages'

describe('Workbench nested page content', () => {
  it('gives every canonical destination a focused primary surface', () => {
    for (const destination of peripheralDestinations) {
      expect(workbenchDestinationContent(destination.id).length, destination.id).toBeGreaterThan(0)
    }
  })

  it('does not retain unrelated cards and advanced panels on a selected route', () => {
    expect(workbenchDestinationContent('interface-audio')).toEqual(['audio'])
    expect(advancedWorkbenchPanelVisible('interface-audio', 'audio')).toBe(true)
    expect(advancedWorkbenchPanelVisible('interface-audio', 'front-panel')).toBe(false)
    expect(workbenchDestinationContent('automation-radio')).toEqual(['radio'])
    expect(workbenchDestinationHasAdvancedContent('automation-radio')).toBe(true)
    expect(workbenchDestinationHasAdvancedContent('automation-macros')).toBe(false)
  })
})
