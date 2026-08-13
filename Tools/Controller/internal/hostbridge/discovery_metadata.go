package hostbridge

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

const discoveryMetadataMinimumInterval = 5 * time.Second

// discoveryMetadata is deliberately bounded to non-secret values that help a
// client identify the app, open its WebUI/API, and render useful board state
// before establishing an alpha session. It is refreshed from pushed
// runtime events; it never initiates a board read.
func discoveryMetadata(config appconfig.Config, snapshot controller.Snapshot, identities ...DiscoveryHostIdentity) []string {
	appearance := config.UI.Appearance
	hostname, _ := os.Hostname()
	name := strings.TrimSpace(config.Integrations.Discovery.InstanceName)
	if name == "" {
		name = strings.TrimSpace(config.UI.AppTitle)
	}
	identity := DiscoveryHostIdentity{}
	if len(identities) != 0 {
		identity = identities[0]
	}
	instanceID := strings.TrimSpace(identity.InstanceID)
	if instanceID == "" {
		instanceID = strings.ToLower(hostname)
	}
	values := []string{
		"product=PCController",
		"protocol=pccontroller",
		"health=ok",
		"service=PCController IPC",
		"host.hostname=" + hostname,
		"instance.id=" + instanceID,
		"instance.name=" + name,
		"host.version=" + strings.TrimSpace(identity.Version),
		"host.source_hash=" + strings.TrimSpace(identity.SourceHash),
		"host.build_time=" + strings.TrimSpace(identity.BuildTime),
		"remote.connectable=" + strconv.FormatBool(config.IPC.RemoteConnectable()),
		"protocol.discovery=" + strings.Join(configuredDiscoveryProtocols(config.Integrations.Discovery), ","),
		"public=/upnp/public.json",
		"api=/api",
		"server_proof=/api/auth/server-proof",
		"operations=/api/rpc",
		"commands=/api/commands",
		"events=ws:" + config.IPC.WebSocketPath + ",socketio:" + config.IPC.SocketIOPath,
		"opcodes=controller.opcode.send,controller.opcode.exchange,controller.opcode.request",
		"web=/",
		"webui=embedded",
		"snapshot=/api/snapshot",
		"board.identity=/api/snapshot",
		"ws=" + config.IPC.WebSocketPath,
		"socketio=" + config.IPC.SocketIOPath,
		"auth=disabled-alpha",
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
	if snapshot.Connected && snapshot.HaveStatus {
		status := snapshot.Status
		values = append(values,
			"board.uptime_ms="+strconv.FormatUint(uint64(status.UptimeMS), 10),
			"board.flags="+strconv.FormatUint(uint64(status.Flags), 10),
			"board.program_running="+strconv.FormatBool(status.ProgramRunning),
			"board.host_offline="+strconv.FormatBool(status.HostOffline),
			"board.hot="+strconv.FormatBool(status.Hot),
			"board.program_mode="+strconv.FormatUint(uint64(status.ProgramMode), 10),
			"board.reset_count="+strconv.FormatUint(uint64(status.ResetCount), 10),
			"board.status_at="+snapshot.StatusUpdated.UTC().Format(time.RFC3339),
		)
		capabilities := snapshot.Hello.Capabilities
		if capabilities&native.CapabilityINA219 != 0 && status.INA219Available {
			values = append(values, "board.supply_mv="+strconv.FormatInt(int64(status.SupplyMV), 10), "board.bus_mv="+strconv.FormatInt(int64(status.BusMV), 10), "board.current_ma="+strconv.FormatInt(int64(status.CurrentMA), 10), "board.power_mw="+strconv.FormatInt(int64(status.PowerMW), 10))
		}
		if capabilities&native.CapabilityTemperatures != 0 && status.TLEDAvailable {
			values = append(values, "board.temperature_led_centi_c="+strconv.FormatInt(int64(status.TLEDCenti), 10))
		}
		if capabilities&native.CapabilityTemperatures != 0 && status.TBTAvailable {
			values = append(values, "board.temperature_bt_audio_centi_c="+strconv.FormatInt(int64(status.TBTCenti), 10))
		}
		if capabilities&native.CapabilityRelayMotion != 0 {
			values = append(values, "board.door_open="+strconv.FormatBool(status.DoorOpen), "board.active_relays="+strconv.FormatUint(uint64(status.ActiveRelays), 10))
		}
		if capabilities&native.CapabilityBluetoothAudio != 0 {
			values = append(values, "board.bluetooth_audio_state="+strconv.FormatUint(uint64(status.BluetoothState), 10))
		}
		if capabilities&native.CapabilityRemoteKeys != 0 {
			values = append(values, "board.active_keys="+strconv.FormatUint(uint64(status.ActiveKeys), 10))
		}
		if capabilities&native.CapabilityMenuRemote != 0 {
			values = append(values, "board.menu_page="+strconv.FormatUint(uint64(status.MenuPage), 10))
		}
		if capabilities&native.CapabilityPWM != 0 {
			values = append(values, "board.pwm_available="+strconv.FormatBool(status.PWMAvailable))
			if status.PWMAvailable {
				values = append(values, "board.pwm_channel="+strconv.FormatUint(uint64(status.PWMChannel), 10), "board.pwm_value="+strconv.FormatUint(uint64(status.PWMValue), 10))
			}
		}
		if capabilities&native.CapabilityLCD != 0 && status.LCDAddress != 0 {
			values = append(values, "board.lcd_address="+strconv.FormatUint(uint64(status.LCDAddress), 10))
		}
	}
	if snapshot.HaveSettings && snapshot.Hello.Capabilities&native.CapabilityPersistentSettings != 0 {
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

func configuredDiscoveryProtocols(value appconfig.Discovery) []string {
	result := make([]string, 0, 7)
	if value.MDNSEnabled || value.DNSSDenabled {
		result = append(result, "dns-sd")
	}
	if value.SSDPEnabled {
		result = append(result, "ssdp")
	}
	if value.UPnPEnabled {
		result = append(result, "upnp")
	}
	if value.WSDiscoveryEnabled {
		result = append(result, "ws-discovery")
	}
	if value.BroadcastEnabled {
		result = append(result, "broadcast")
	}
	if value.NetBIOSEnabled {
		result = append(result, "netbios")
	}
	return result
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
	advertiser.UpdateText(discoveryMetadata(config, manager.client.Snapshot(), manager.discoveryIdentity))
}
