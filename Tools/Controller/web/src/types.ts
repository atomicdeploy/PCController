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
}

export interface ControllerStatus {
  uptime_ms: number
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
  pwm_mode: number
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
  status_brightness: number
  pwm_boot_mode: number
  stream_period_ms: number
  default_page: number
  extended_flags: number
  motion_break_ms?: number
}

export interface ProgramState {
  mode?: string
  owner?: string
  reason?: string
  updated_at?: string
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
  text: string
  state?: string
  lifecycle?: string
  reason?: string
  source?: string
  target?: string
  message_type?: string
  action?: string
  gesture?: string
  key?: number
  rf_id?: number
  rf_code?: number
  rf_bits?: number
  rf_protocol?: number
  metadata?: Record<string, string>
}

export interface HistorySample {
  time?: string
  status?: ControllerStatus
  [key: string]: unknown
}

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
  api_version: number
  websocket_path: string
  socket_io_path?: string
  auth_required: boolean
  integrations?: {
    local_device: boolean
    data_hub: boolean
  }
  host_version?: string
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
  websocket_online?: boolean
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
  pwm_mode: 0,
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
    status_brightness: 0,
    pwm_boot_mode: 0,
    stream_period_ms: 200,
    default_page: 0,
    extended_flags: 0,
  },
  have_status: false,
  have_settings: false,
  connection_state: 'offline',
}
