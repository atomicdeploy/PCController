export type ThemePreference = 'system' | 'light' | 'dark'
export type Locale = 'en' | 'fa'
export type DirectionPreference = 'auto' | 'ltr' | 'rtl'

export interface Appearance {
  theme: ThemePreference
  locale: Locale
  direction: DirectionPreference
  reduceMotion: boolean
  compactNumbers: boolean
  audioMuted: boolean
  audioVolume: number
}

export interface PortInfo {
  name?: string
  vid?: string
  pid?: string
  product?: string
  manufacturer?: string
  serial_number?: string
  friendly_name?: string
  instance_id?: string
}

export interface Hello {
  firmware_major?: number
  firmware_minor?: number
  firmware_patch?: number
  board_kind?: number
  capabilities?: number
  name?: string
  identity_schema?: number
  build_hash?: number
  build_date?: string
  build_time?: string
  build_timestamp?: string
  feature_profile?: number
  build_features?: number
}

export interface ControllerStatus {
  uptime_ms: number
  uptime?: string
  supply_mv: number
  bus_mv: number
  current_ma: number
  power_mw: number
  temperature_led_centi_c: number
  temperature_bt_audio_centi_c: number
  flags: number
  program_running: boolean
  host_offline: boolean
  hot: boolean
  raw_inputs: number
  active_keys: number
  active_relays: number
  menu_page: number
  program_mode: number
  door_open: boolean
  bluetooth_audio_state: number
  pwm_available: boolean
  pwm_channel: number
  pwm_value: number
  lcd_address: number
  pwm_errors: number
  framing_errors: number
  crc_errors: number
  reset_cause: number
  reset_count: number
}

export interface ControllerSettings {
  flags: number
  light_mode: number
  on_brightness: number
  off_brightness: number
  display_brightness: number
  display_closed_brightness: number
  motion_exit_hold_seconds: number
  status_brightness: number
  output_persistence: number
  relay_restore_mask: number
  stream_period_ms: number
  default_page: number
  extended_flags: number
  motion_break_ms: number
	persisted?: boolean
}

export interface ProgramState {
  mode?: string
  owner?: string
  reason?: string
  updated_at?: string
}

export interface RFLearnState {
  active: boolean
  mode: 'indefinite' | 'timer'
  configured_ms: number
  remaining_ms: number
  started_at?: string
  ends_at?: string
  learned: number
  reason?: string
}

export interface FrontPanelState {
  schema: number
  raw_segments: [number, number, number, number]
  brightness: number
  blink: boolean
  segments_active: boolean
  category_selector: boolean
  lcd_address: number
  lcd_available: boolean
  lcd_backlight: boolean
  lcd_line_1: string
  lcd_line_2: string
  pressed_keys: number
  menu_page: number
  program_mode: number
  host_captured: boolean
  host_state: number
  host_editable_value: number
}

export interface MenuPageInfo {
  id: number
  key: string
  label: string
  name: string
  description: string
}

export interface MenuLayout {
  visible_mask: number
  order: number[]
}

export interface MenuCatalog {
  source: string
  live_list: boolean
  firmware_hash: number
  current_page: number
  program_mode: number
  pages: MenuPageInfo[]
  layout: MenuLayout
}

export interface StatusLEDState {
  red: number
  green: number
  blue: number
  brightness: number
  effect: number
  condition: number
}

export interface Snapshot {
  connected: boolean
  paused: boolean
  port: PortInfo
  hello: Hello
  status: ControllerStatus
  settings: ControllerSettings
  have_status: boolean
  have_settings: boolean
  status_updated?: string
  connection_state: string
  connection_reason?: string
  connection_updated?: string
  program_state?: ProgramState
  rf_learning?: RFLearnState
  front_panel?: FrontPanelState
  have_front_panel?: boolean
  front_panel_updated?: string
	status_led?: StatusLEDState
	have_status_led?: boolean
	status_led_updated?: string
}

export interface StatusUpdate {
  time: string
  status: ControllerStatus
  error?: string
}

export interface ControllerEvent {
  id: number
  time: string
  kind: string
  stream?: 'activity' | 'state' | 'telemetry' | 'debug'
  text: string
  state?: string
  lifecycle?: string
  reason?: string
  source?: string
  target?: string
  targets?: string[]
  message_type?: string
  action?: string
  severity?: 'debug' | 'info' | 'success' | 'warning' | 'error'
  correlation?: string
  delivery?: 'sync' | 'async'
  gesture?: string
  key?: number
  rf_id?: number
  rf_code?: number
  rf_bits?: number
  rf_protocol?: number
  rf_pulse_us?: number
  metadata?: Record<string, string>
}

export interface MacroStep {
  at_us?: number
  kind: string
  target?: number
  value?: number
  duration_ms?: number
  frequency_hz?: number
  text?: string
  destination?: string
  code?: number
  bits?: number
  protocol?: number
  pulse_us?: number
  red?: number
  green?: number
  blue?: number
  brightness?: number
  opcode?: number
  payload_hex?: string
}

export interface ControllerMacro {
  id: number
  name: string
  category?: string
  color?: string
  label?: string
  lcd_message?: string
  timing_tolerance_us?: number
  keep_outputs_on_cancel?: boolean
  recording_source?: string
  capture_dropped_steps?: number
  capture_missing_steps?: number
  steps: MacroStep[]
}

export interface MacroPlaybackState {
  running: boolean
  connection_generation?: number
  id: number
  name: string
  category?: string
  color?: string
  step: number
  step_count: number
  duration_us: number
  started_at?: string
  finished_at?: string
  device_started_at_us?: number
  accepted_bytes: number
  buffer_fill: number
  underruns: number
  dispatch_errors: number
  dropped_steps: number
  evidence_steps: number
  timing_violations: number
  last_timing_delta_us: number
  maximum_timing_error_us: number
  timing_tolerance_us: number
  faithful: boolean
  lifecycle?: string
  last_error?: string
  device?: Record<string, unknown>
}

export interface MacroRecordingState {
  active: boolean
  id: number
  name: string
  category?: string
  color?: string
  steps: number
  host_steps: number
  panel_steps: number
  rf_steps: number
  last_at_us: number
  last_delta_us: number
  last_opcode: number
  last_source: number
  board_owned?: boolean
  board_id?: number
  dropped_steps?: number
  started_at?: string
  last_error?: string
}

export interface MacroSnapshot {
  library: ControllerMacro[]
  playback: MacroPlaybackState
  recording: MacroRecordingState
  latest_event_id: number
}

export interface RFLearnedEntry {
  id: number
  code: number
  code_display: string
  bits: number
  protocol: number
  pulse_us: number
  action_kind: number
  action_value: number
  behavior: number
  name?: string
  category?: string
}

export interface HistorySample {
  time?: string
  status?: ControllerStatus
  [key: string]: unknown
}

export type BoardSettingsReadState = 'idle' | 'loading' | 'ready' | 'unavailable'

export interface MetricSample {
  at: number
  supply: number
  bus: number
  current: number
  power: number
  ledTemp: number
  btTemp: number
}

export interface UIConfig {
  name: string
	tagline?: string
  setup_complete: boolean
  appearance: Appearance
  appearance_etag: string
  welcome_melody?: string
	websocket_path: string
  socket_io_path?: string
  session_ticket_path: string
  auth_required: boolean
  integrations?: {
    local_device: boolean
    data_hub: boolean
		buzzer_host_enabled: boolean
		buzzer_native_enabled: boolean
		buzzer_web_audio: boolean
  }
  host_version?: string
  source_hash?: string
  build_time?: string
}

export interface HostUISettings {
  app_title: string
	tagline: string
  setup_complete: boolean
  welcome_melody: string
  appearance: Appearance
  appearance_etag: string
  segment_scroll: SegmentScrollSettings
  peripheral_names: Record<string, string>
  peripherals: PeripheralDescriptor[]
  changed?: boolean
  changed_fields?: string[]
  before?: Record<string, unknown>
  after?: Record<string, unknown>
}

export interface PeripheralDescriptor {
  key: string
  kind: 'relay' | 'motion' | 'pwm' | 'display' | 'sensor'
  role: string
  index: number
  default_name: string
  control: 'relay' | 'motion' | 'pwm-user' | 'role-specific' | 'read-only'
}

export interface PeripheralSettings {
  peripheral_names: Record<string, string>
  peripherals: PeripheralDescriptor[]
}

export interface PWMValues {
  available: boolean
  selected_channel: number
  values: number[]
}

export interface SegmentScrollSettings {
  enabled: boolean
  pages: string[]
  door_open_text: string
  door_closed_text: string
  speed_ms: number
  gap_cells: number
}

export interface LocalIntegrationSettings {
  local_device: {
    enabled: boolean
    base_url?: string
  }
  data_hub: {
    enabled: boolean
    base_url?: string
  }
  lifecycle_safety: {
    session_lock: LifecycleSafetyAction
    suspend: LifecycleSafetyAction
    refresh_on_resume: boolean
  }
}

export type LifecycleSafetyAction = 'leave' | 'stop-motion' | 'all-off'

export interface HostHotkeyBinding {
  name: string
  enabled: boolean
  chord: string
  command: string
}

export interface ActiveHostHotkeyBinding {
  name: string
  accelerator: string
  command: string
}

export interface HotkeyRegistrarStatus {
  supported: boolean
  running: boolean
  bindings?: ActiveHostHotkeyBinding[]
  last_error?: string
  last_event?: {
    binding: ActiveHostHotkeyBinding
    at: string
  }
}

export interface HotkeySettingsResponse {
  bindings: HostHotkeyBinding[]
  apply_pending: boolean
  operation?: 'upsert' | 'remove'
  name?: string
  status?: HotkeyRegistrarStatus
}

export interface RPCResponse<T> {
  jsonrpc: '2.0'
  id?: number
  result?: T
  error?: { code: number; message: string }
}

export interface CommandResult {
  output?: string
}

export interface LocalDeviceSnapshot {
  configured?: boolean
  power?: 'ON' | 'OFF' | 'UNKNOWN'
  phase?: string
  http_reachable?: boolean
  events_online?: boolean
  updated_at?: string
  last_error?: string
  last_event?: string
  capabilities?: string[]
  base_url?: string
}

export interface ToastMessage {
  id: number
  tone: 'info' | 'success' | 'warning' | 'danger'
  title: string
  detail?: string
}

export interface DialogState {
  open: boolean
  tone?: 'normal' | 'danger'
  title: string
  body: string
  confirmLabel: string
  action?: () => Promise<void> | void
  cancel?: () => void
}

export const emptyStatus: ControllerStatus = {
  uptime_ms: 0,
  supply_mv: 0,
  bus_mv: 0,
  current_ma: 0,
  power_mw: 0,
  temperature_led_centi_c: 0,
  temperature_bt_audio_centi_c: 0,
  flags: 0,
  program_running: false,
  host_offline: false,
  hot: false,
  raw_inputs: 0,
  active_keys: 0,
  active_relays: 0,
  menu_page: 0,
  program_mode: 0,
  door_open: false,
  bluetooth_audio_state: 0,
  pwm_available: false,
  pwm_channel: 0,
  pwm_value: 0,
  lcd_address: 0,
  pwm_errors: 0,
  framing_errors: 0,
  crc_errors: 0,
  reset_cause: 0,
  reset_count: 0,
}

export const emptySnapshot: Snapshot = {
  connected: false,
  paused: false,
  port: {},
  hello: {},
  status: emptyStatus,
  settings: {
    flags: 0,
    light_mode: 0,
    on_brightness: 0,
    off_brightness: 0,
    display_brightness: 0,
    display_closed_brightness: 0,
    motion_exit_hold_seconds: 2,
    status_brightness: 0,
    output_persistence: 0,
    relay_restore_mask: 0,
    stream_period_ms: 200,
    default_page: 0,
    extended_flags: 0,
    motion_break_ms: 1,
	persisted: false,
  },
  have_status: false,
  have_settings: false,
  front_panel: {
    schema: 0,
    raw_segments: [0, 0, 0, 0],
    brightness: 0,
    blink: false,
    segments_active: false,
    category_selector: false,
    lcd_address: 0,
    lcd_available: false,
    lcd_backlight: false,
    lcd_line_1: '',
    lcd_line_2: '',
    pressed_keys: 0,
    menu_page: 0,
    program_mode: 0,
    host_captured: false,
    host_state: 0,
    host_editable_value: 0,
  },
  have_front_panel: false,
  connection_state: 'offline',
}
