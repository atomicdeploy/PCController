import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { SevenSegmentPreview } from './seven-segment-preview'
import type { FrontPanelState } from './types'

const livePanel: FrontPanelState = {
  schema: 1,
  raw_segments: [0x06, 0x5b, 0x4f, 0xe6],
  brightness: 5,
  blink: false,
  segments_active: true,
  category_selector: false,
  lcd_address: 0,
  lcd_available: false,
  lcd_backlight: false,
  lcd_line_1: '',
  lcd_line_2: '',
  pressed_keys: 0,
  menu_page: 3,
  program_mode: 0,
  host_captured: false,
  host_state: 0,
  host_editable_value: 0,
}

describe('live seven-segment preview', () => {
  it('does not fabricate a blank four-digit frame while board state is unavailable', () => {
    expect(renderToStaticMarkup(<SevenSegmentPreview />)).toBe('')
  })

  it('renders all four exact host-observed segment bytes with a localized accessible name', () => {
    const markup = renderToStaticMarkup(<SevenSegmentPreview panel={livePanel} label="نمايش زنده" />)
    expect(markup).toContain('aria-label="نمايش زنده"')
    expect(markup).toContain('digit 1 raw 0x06')
    expect(markup).toContain('digit 4 raw 0xe6')
    expect((markup.match(/<svg/g) ?? []).length).toBe(4)
  })

  it('rejects malformed transport data instead of rendering an invented display', () => {
    const malformed = { ...livePanel, raw_segments: [0x06, -1, 0x4f, 0x66] } as unknown as FrontPanelState
    expect(renderToStaticMarkup(<SevenSegmentPreview panel={malformed} />)).toBe('')
  })
})
