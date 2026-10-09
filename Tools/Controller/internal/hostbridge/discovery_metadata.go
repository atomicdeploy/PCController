package hostbridge

import (
	"fmt"
	"strconv"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

const discoveryMetadataMinimumInterval = 5 * time.Second

// discoveryMetadata is deliberately bounded to non-secret values that help a
// client identify the app, open its WebUI/API, and render useful board state
// before establishing an authenticated session. It is refreshed from pushed
// runtime events; it never initiates a board read.
func discoveryMetadata(config appconfig.Config, snapshot controller.Snapshot) []string {
	appearance := config.UI.Appearance
	values := []string{
		"api=/api",
		"web=/",
		"webui=embedded",
		"snapshot=/api/snapshot",
		"ws=" + config.IPC.WebSocketPath,
		"socketio=" + config.IPC.SocketIOPath,
		"auth=required",
		"app.title=" + config.UI.AppTitle,
		"app.locale=" + appearance.Locale,
		"app.theme=" + appearance.Theme,
		"app.direction=" + appearance.Direction,
		"board.connected=" + strconv.FormatBool(snapshot.Connected),
		"board.connection_state=" + snapshot.ConnectionState,
	}
	if snapshot.Connected {
		values = append(values,
			"board.name="+snapshot.Hello.Name,
			fmt.Sprintf("board.build_hash=%08X", snapshot.Hello.BuildHash),
			"board.build_timestamp="+snapshot.Hello.BuildStamp,
			fmt.Sprintf("board.capabilities=%08X", snapshot.Hello.Capabilities),
			"board.port="+snapshot.Port.Name,
			"board.port_product="+snapshot.Port.Product,
		)
	}
	if snapshot.HaveStatus {
		status := snapshot.Status
		values = append(values,
			"board.uptime_ms="+strconv.FormatUint(uint64(status.UptimeMS), 10),
			"board.supply_mv="+strconv.FormatInt(int64(status.SupplyMV), 10),
			"board.bus_mv="+strconv.FormatInt(int64(status.BusMV), 10),
			"board.current_ma="+strconv.FormatInt(int64(status.CurrentMA), 10),
			"board.power_mw="+strconv.FormatInt(int64(status.PowerMW), 10),
			"board.temperature_led_centi_c="+strconv.FormatInt(int64(status.TLEDCenti), 10),
			"board.temperature_bt_audio_centi_c="+strconv.FormatInt(int64(status.TBTCenti), 10),
			"board.flags="+strconv.FormatUint(uint64(status.Flags), 10),
			"board.program_running="+strconv.FormatBool(status.ProgramRunning),
			"board.host_offline="+strconv.FormatBool(status.HostOffline),
			"board.hot="+strconv.FormatBool(status.Hot),
			"board.door_open="+strconv.FormatBool(status.DoorOpen),
			"board.bluetooth_audio_state="+strconv.FormatUint(uint64(status.BluetoothState), 10),
			"board.active_relays="+strconv.FormatUint(uint64(status.ActiveRelays), 10),
			"board.active_keys="+strconv.FormatUint(uint64(status.ActiveKeys), 10),
			"board.menu_page="+strconv.FormatUint(uint64(status.MenuPage), 10),
			"board.program_mode="+strconv.FormatUint(uint64(status.ProgramMode), 10),
			"board.pwm_available="+strconv.FormatBool(status.PWMAvailable),
			"board.pwm_channel="+strconv.FormatUint(uint64(status.PWMChannel), 10),
			"board.pwm_value="+strconv.FormatUint(uint64(status.PWMValue), 10),
			"board.lcd_address="+strconv.FormatUint(uint64(status.LCDAddress), 10),
			"board.reset_count="+strconv.FormatUint(uint64(status.ResetCount), 10),
			"board.status_at="+snapshot.StatusUpdated.UTC().Format(time.RFC3339),
		)
	}
	if snapshot.HaveSettings {
		settings := snapshot.Settings
		values = append(values,
			"board.settings_persisted="+strconv.FormatBool(settings.Persisted),
			"board.default_page="+strconv.FormatUint(uint64(settings.DefaultPage), 10),
			"board.display_brightness="+strconv.FormatUint(uint64(settings.DisplayBrightness), 10),
			"board.status_brightness="+strconv.FormatUint(uint64(settings.StatusBrightness), 10),
			"board.stream_period_ms="+strconv.FormatUint(uint64(settings.StreamPeriodMS), 10),
			"board.light_mode="+strconv.FormatUint(uint64(settings.LightMode), 10),
		)
	}
	return values
}

func (manager *Manager) requestDiscoveryMetadataRefresh() {
	select {
	case manager.discoveryRefresh <- struct{}{}:
	default:
	}
}

func (manager *Manager) discoveryMetadataLoop() {
	defer manager.wait.Done()
	lastUpdate := time.Now()
	var timer *time.Timer
	var timerChannel <-chan time.Time
	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer, timerChannel = nil, nil
	}
	defer stopTimer()
	for {
		select {
		case <-manager.ctx.Done():
			return
		case <-manager.discoveryRefresh:
			remaining := discoveryMetadataMinimumInterval - time.Since(lastUpdate)
			if remaining <= 0 {
				manager.refreshDiscoveryMetadata()
				lastUpdate = time.Now()
				continue
			}
			if timer == nil {
				timer = time.NewTimer(remaining)
				timerChannel = timer.C
			}
		case <-timerChannel:
			timer, timerChannel = nil, nil
			manager.refreshDiscoveryMetadata()
			lastUpdate = time.Now()
		}
	}
}

func (manager *Manager) refreshDiscoveryMetadata() {
	manager.mu.RLock()
	advertiser := manager.advertiser
	manager.mu.RUnlock()
	if advertiser == nil {
		return
	}
	config := manager.store.Current()
	advertiser.UpdateText(discoveryMetadata(config, manager.client.Snapshot()))
}
