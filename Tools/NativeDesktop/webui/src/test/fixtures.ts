import type { AppConfig, Snapshot, UIBootstrap } from "../contracts";

export const testSnapshot: Snapshot = {
  state: "off",
  http_reachable: true,
  websocket_online: true,
  device_url: "http://pccontroller-desktop.local",
  last_source: "websocket",
  reconnect_attempt: 0
};

export const testConfig: AppConfig = {
  device: {
    base_url: "http://pccontroller-desktop.local",
    request_timeout: "5s",
    websocket: true,
    reconnect_min: "1s",
    reconnect_max: "30s",
    http_poll: "30s",
    telnet_port: 23
  },
  ui: {
    view_mode: "compact",
    theme: "system",
    backdrop: "acrylic",
    render_backend: "auto",
    rounded_corners: true,
    layered_foreground: true,
    title_bar_mode: "none",
    border_mode: "auto",
    settings_tab: 0,
    settings_bounds: { x: -1, y: -1, width: 780, height: 640 },
    show_title_bar: false,
    always_on_top: false,
    close_to_tray: true,
    prewarm_tray_menu: true,
    start_minimized: false,
    window_visible: true,
    window_state: "normal",
    show_taskbar: true,
    animations: true,
    magnetic_snap: false,
    magnetic_snap_distance: 16,
    window_bounds: { x: -1, y: -1, width: 176, height: 176 }
  },
  notifications: {
    enabled: true,
    state_changes: true,
    initial_sync: false,
    custom_message: true,
    sound: true,
    sound_source: "bundled",
    sound_file: "",
    sound_name: "",
    delivery: "auto"
  },
  hotkeys: {
    toggle: "Ctrl+Alt+L",
    on: "Ctrl+Alt+Shift+O",
    off: "Ctrl+Alt+Shift+F",
    show: "Ctrl+Alt+Shift+L"
  },
  automation: {
    enabled: false,
    on_script: "",
    off_script: "",
    message_script: "",
    lock_script: "",
    unlock_script: "",
    suspend_script: "",
    resume_script: "",
    network_change_script: "",
    working_directory: "",
    timeout: "15s",
    capture_output: true
  },
  integration: {
    start_with_windows: false,
    local_api_enabled: true,
    local_api_listen: "127.0.0.1:47831",
    local_api_token_configured: true
  },
  diagnostics: {
    safe_endpoints: [
      "/info",
      "/update/info",
      "/wifi/signal",
      "/wifi/status",
      "/wifi/country",
      "/description.xml"
    ],
    expert_enabled: false,
    expert_endpoints: []
  }
};

export const testBootstrap: UIBootstrap = {
  snapshot: testSnapshot,
  config: testConfig,
  version: {
    name: "PCControllerDesktop",
    display_name: "PCController",
    version: "1.0.0",
    company: "AtomicDeploy",
    description: "Test controller"
  },
  revision: 7,
  diagnostics: {
    device_url: "http://pccontroller-desktop.local",
    device: {},
    endpoints: {},
    refreshing: false
  }
};
