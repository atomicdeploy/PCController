package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/discovery"
	"pccontroller.local/controller/internal/ipcjson"
)

func runNetwork(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	if len(args) == 0 {
		return errors.New("usage: controller network advertise|discover|list|connect|probe|edge-enable|edge-disable|peer-add|peer-remove|status")
	}
	switch strings.ToLower(args[0]) {
	case "status":
		if len(args) != 1 {
			return errors.New("usage: controller network status")
		}
		redacted := store.Redacted()
		value := struct {
			IPC       appconfig.IPC               `json:"ipc"`
			Discovery appconfig.Discovery         `json:"discovery"`
			Peers     []appconfig.WebSocketClient `json:"websocket_clients,omitempty"`
		}{redacted.IPC, redacted.Integrations.Discovery, redacted.Integrations.WebSocketClients}
		encoded, _ := json.MarshalIndent(value, "", "  ")
		fmt.Fprintln(stdout, string(encoded))
		return nil
	case "edge-enable", "enable-edge":
		flags := flag.NewFlagSet("network edge-enable", flag.ContinueOnError)
		flags.SetOutput(stderr)
		listen := flags.String("listen", "0.0.0.0:8787", "LAN IPC/API/WebSocket listen address")
		instance := flags.String("instance", "", "mDNS/SSDP instance name (hostname by default)")
		origins := stringListFlag{}
		flags.Var(&origins, "origin", "allowed browser origin host pattern, repeatable (for example David-PC:*)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("usage: controller network edge-enable [--listen HOST:PORT] [--origin HOST:*]...")
		}
		host, _, err := net.SplitHostPort(*listen)
		if err != nil || strings.TrimSpace(host) == "" {
			return errors.New("--listen must be a concrete host:port address")
		}
		if len(origins) == 0 {
			origins = append(origins, "localhost:*", "127.0.0.1:*", "[::1]:*")
		}
		if *instance == "" {
			*instance, _ = os.Hostname()
			*instance = boundedDiscoveryInstanceName(*instance)
		}
		_, err = store.Update(func(config *appconfig.Config) error {
			config.IPC.Listen = *listen
			config.IPC.AllowRemote = true
			config.IPC.AuthToken, config.IPC.AuthTokenRef = "", ""
			config.IPC.AllowedOrigins = append([]string(nil), origins...)
			config.IPC.RemotePolicy = appconfig.RemoteAccessPolicy{
				Read: true, Events: true, Messages: true, BoardCommands: true,
				HostConfiguration: true, ConnectionControl: true, Reset: true,
				Programming: true, BridgeCalls: true, Integrations: true,
			}
			config.Integrations.Discovery.MDNSEnabled = true
			config.Integrations.Discovery.DNSSDenabled = true
			config.Integrations.Discovery.SSDPEnabled = true
			config.Integrations.Discovery.UPnPEnabled = true
			config.Integrations.Discovery.WSDiscoveryEnabled = true
			config.Integrations.Discovery.BroadcastEnabled = true
			config.Integrations.Discovery.NetBIOSEnabled = true
			config.Integrations.Discovery.InstanceName = strings.TrimSpace(*instance)
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, "LAN edge mode enabled with alpha authentication disabled.")
		fmt.Fprintln(stdout, "IPC/API/WebSocket:", *listen)
		fmt.Fprintln(stdout, "Discovery: DNS-SD/mDNS + SSDP/UPnP + WS-Discovery + UDP broadcast + NetBIOS as", *instance)
		fmt.Fprintln(stdout, "Remote programming is enabled; shutdown, virtual-key, and power-action capabilities remain disabled.")
		fmt.Fprintln(stdout, "Restart the controller host after applying the required private-profile firewall rule.")
		return nil
	case "peer-add", "add-peer":
		flags := flag.NewFlagSet("network peer-add", flag.ContinueOnError)
		flags.SetOutput(stderr)
		name := flags.String("name", "", "unique peer name")
		url := flags.String("url", "", "peer ws:// or wss:// IPC endpoint")
		secretRef := flags.String("secret-ref", "", "existing OS-vault or environment bearer-token reference")
		protocol := flags.String("protocol", "jsonrpc", "jsonrpc or socketio")
		forwardEvents := flags.Bool("forward-events", true, "forward loop-safe typed events")
		allowCommands := flags.Bool("allow-commands", true, "permit correlated bridge calls")
		topics := stringListFlag{}
		flags.Var(&topics, "topic", "events, state, or status subscription, repeatable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*name) == "" || strings.TrimSpace(*url) == "" {
			return errors.New("usage: controller network peer-add --name NAME --url ws://HOST:PORT/ipc [--topic events|state|status]")
		}
		if len(topics) == 0 {
			topics = []string{"events", "state", "status"}
		}
		peer := appconfig.WebSocketClient{
			Name: strings.TrimSpace(*name), Enabled: true, URL: strings.TrimSpace(*url),
			Protocol: strings.TrimSpace(*protocol), AuthTokenRef: strings.TrimSpace(*secretRef),
			Topics: append([]string(nil), topics...), ForwardEvents: *forwardEvents,
			AllowCommands: *allowCommands,
		}
		if _, err := store.Update(func(config *appconfig.Config) error {
			for index := range config.Integrations.WebSocketClients {
				if strings.EqualFold(config.Integrations.WebSocketClients[index].Name, peer.Name) {
					config.Integrations.WebSocketClients[index] = peer
					return nil
				}
			}
			config.Integrations.WebSocketClients = append(config.Integrations.WebSocketClients, peer)
			return nil
		}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "LAN peer %q configured; the primary host hot-applies this change.\n", peer.Name)
		return nil
	case "peer-remove", "remove-peer":
		flags := flag.NewFlagSet("network peer-remove", flag.ContinueOnError)
		flags.SetOutput(stderr)
		name := flags.String("name", "", "peer name")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*name) == "" {
			return errors.New("usage: controller network peer-remove --name NAME")
		}
		found := false
		if _, err := store.Update(func(config *appconfig.Config) error {
			peers := config.Integrations.WebSocketClients[:0]
			for _, peer := range config.Integrations.WebSocketClients {
				if strings.EqualFold(peer.Name, strings.TrimSpace(*name)) {
					found = true
					continue
				}
				peers = append(peers, peer)
			}
			config.Integrations.WebSocketClients = peers
			return nil
		}); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("LAN peer %q is not configured", *name)
		}
		fmt.Fprintf(stdout, "LAN peer %q removed and hot-applied; its separate vault value was preserved.\n", strings.TrimSpace(*name))
		return nil
	case "probe":
		flags := flag.NewFlagSet("network probe", flag.ContinueOnError)
		flags.SetOutput(stderr)
		address := flags.String("addr", "", "edge host:port")
		tokenRef := flags.String("token-ref", "", "OS-vault or environment bearer-token reference")
		origin := flags.String("origin", "", "allowed HTTP origin (host name by default)")
		timeout := flags.Duration("timeout", 15*time.Second, "whole probe timeout")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*address) == "" {
			return errors.New("usage: controller network probe --addr HOST:PORT [--origin URL]")
		}
		token := ""
		if strings.TrimSpace(*tokenRef) != "" {
			resolved, err := store.ResolveSecret(*tokenRef)
			if err != nil {
				return fmt.Errorf("resolve LAN probe token: %w", err)
			}
			token = resolved
		}
		if *origin == "" {
			hostname, _ := os.Hostname()
			*origin = "http://" + hostname + ":8787"
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		if err := probeEdgeNetwork(ctx, strings.TrimSpace(*address), strings.TrimSpace(*origin), token, stdout); err != nil {
			return err
		}
		return nil
	case "advertise":
		flags := flag.NewFlagSet("network advertise", flag.ContinueOnError)
		flags.SetOutput(stderr)
		enabled := flags.Bool("enabled", true, "enable or disable advertisement")
		protocols := flags.String("protocols", "dns-sd,ssdp,upnp,ws-discovery,broadcast,netbios", "comma-separated protocols")
		instance := flags.String("instance", "", "advertised instance name")
		broadcastPort := flags.Int("broadcast-port", discovery.BroadcastPort, "UDP broadcast discovery port")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *broadcastPort < 1024 || *broadcastPort > 65535 {
			return errors.New("usage: controller network advertise [--enabled=true|false] [--protocols LIST] [--instance NAME] [--broadcast-port 37889]")
		}
		configured := appconfig.Discovery{BroadcastPort: *broadcastPort, InstanceName: strings.TrimSpace(*instance)}
		if *enabled {
			options, err := parseDiscoveryOptions(*protocols)
			if err != nil {
				return err
			}
			configured.MDNSEnabled, configured.DNSSDenabled = options.MDNS, options.DNSSD
			configured.SSDPEnabled, configured.UPnPEnabled = options.SSDP, options.UPnP
			configured.WSDiscoveryEnabled, configured.BroadcastEnabled = options.WSDiscovery, options.Broadcast
			configured.NetBIOSEnabled = options.NetBIOS
		}
		primaryContext, stopPrimary := context.WithTimeout(context.Background(), time.Second)
		havePrimary := primaryAvailable(primaryContext)
		stopPrimary()
		if havePrimary {
			var persisted appconfig.Discovery
			primaryContext, stopPrimary = context.WithTimeout(context.Background(), 5*time.Second)
			err := callPrimary(primaryContext, "controller.discovery.config.set", configured, &persisted)
			stopPrimary()
			if err != nil {
				return fmt.Errorf("persist discovery settings through primary: %w", err)
			}
			configured = persisted
		} else if _, err := store.Update(func(config *appconfig.Config) error {
			config.Integrations.Discovery = configured
			return nil
		}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Network advertisement enabled=%t protocols=%s broadcast_port=%d\n", *enabled, strings.Join(enabledProtocolNames(configured), ","), configured.BroadcastPort)
		fmt.Fprintln(stdout, "Remote command access remains governed separately by ipc.allow_remote, bearer authentication, and remote_policy.")
		return nil
	case "discover", "list":
		flags := flag.NewFlagSet("network discover", flag.ContinueOnError)
		flags.SetOutput(stderr)
		protocols := flags.String("protocols", "dns-sd,ssdp,upnp,ws-discovery,broadcast,netbios", "comma-separated protocols")
		timeout := flags.Duration("timeout", 3*time.Second, "discovery timeout")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *timeout < 100*time.Millisecond || *timeout > 30*time.Second {
			return errors.New("usage: controller network discover [--protocols LIST] [--timeout 100ms..30s]")
		}
		options, err := parseDiscoveryOptions(*protocols)
		if err != nil {
			return err
		}
		options.BroadcastPort = store.Current().Integrations.Discovery.BroadcastPort
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		scanDuration := *timeout / 2
		if scanDuration > 3*time.Second {
			scanDuration = 3 * time.Second
		}
		scanContext, stopScan := context.WithTimeout(ctx, scanDuration)
		var instances []discovery.Instance
		err = callPrimary(scanContext, "controller.discovery.scan", map[string]any{
			"timeout_ms": optionsTimeoutMilliseconds(scanDuration),
			"mdns":       options.MDNS, "dns_sd": options.DNSSD, "ssdp": options.SSDP,
			"upnp": options.UPnP, "ws_discovery": options.WSDiscovery,
			"broadcast": options.Broadcast, "netbios": options.NetBIOS,
		}, &instances)
		stopScan()
		if err != nil {
			fallbackContext, stopFallback := context.WithTimeout(ctx, scanDuration)
			instances, err = discovery.DiscoverWithOptions(fallbackContext, options)
			stopFallback()
			if err != nil {
				return err
			}
		}
		encoded, _ := json.MarshalIndent(instances, "", "  ")
		fmt.Fprintln(stdout, string(encoded))
		return nil
	case "connect":
		flags := flag.NewFlagSet("network connect", flag.ContinueOnError)
		flags.SetOutput(stderr)
		target := flags.String("target", "", "discovered name, hostname, instance id, or address")
		tokenRef := flags.String("token-ref", "", "OS-vault or environment bearer-token reference")
		protocols := flags.String("protocols", "dns-sd,ssdp,upnp,ws-discovery,broadcast,netbios", "comma-separated protocols")
		timeout := flags.Duration("timeout", 15*time.Second, "discovery and connection timeout")
		origin := flags.String("origin", "", "allowed HTTP origin (local hostname by default)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*target) == "" || *timeout < time.Second || *timeout > 30*time.Second {
			return errors.New("usage: controller network connect --target NAME|HOST [--timeout 15s]")
		}
		options, err := parseDiscoveryOptions(*protocols)
		if err != nil {
			return err
		}
		options.BroadcastPort = store.Current().Integrations.Discovery.BroadcastPort
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		instances, err := discovery.DiscoverWithOptions(ctx, options)
		if err != nil {
			return err
		}
		selected, err := resolveDiscoveredInstance(instances, *target)
		if err != nil {
			return err
		}
		address, err := discoveredAddress(selected)
		if err != nil {
			return err
		}
		token := ""
		if strings.TrimSpace(*tokenRef) != "" {
			resolved, err := store.ResolveSecret(*tokenRef)
			if err != nil {
				return fmt.Errorf("resolve discovered-host token: %w", err)
			}
			token = resolved
		}
		if *origin == "" {
			hostname, _ := os.Hostname()
			*origin = "http://" + hostname + ":8787"
		}
		fmt.Fprintf(stdout, "Connecting to %s at %s via %s...\n", selected.Name, address, strings.Join(selected.Protocols, ","))
		endpoints := discoveredProbeEndpoints(selected, address)
		if err := probeEdgeNetworkWithEndpoints(ctx, endpoints, strings.TrimSpace(*origin), token, stdout); err != nil {
			return err
		}
		provenAddress := address
		primaryContext, stopPrimary := context.WithTimeout(context.Background(), 5*time.Second)
		if primaryAvailable(primaryContext) {
			var observed map[string]any
			if observeErr := callPrimary(primaryContext, "controller.discovery.connect", map[string]any{
				"address": provenAddress, "auth": token, "timeout_ms": 4000,
			}, &observed); observeErr != nil {
				stopPrimary()
				return fmt.Errorf("publish discovered connection through primary: %w", observeErr)
			}
		}
		stopPrimary()
		encoded, _ := json.MarshalIndent(selected, "", "  ")
		fmt.Fprintln(stdout, string(encoded))
		return nil
	case "edge-disable", "disable-edge":
		if len(args) != 1 {
			return errors.New("usage: controller network edge-disable")
		}
		current := store.Current()
		reference := current.IPC.AuthTokenRef
		_, err := store.Update(func(config *appconfig.Config) error {
			defaults := appconfig.Defaults().IPC
			config.IPC = defaults
			config.Integrations.Discovery = appconfig.DefaultDiscovery()
			return nil
		})
		if err != nil {
			return err
		}
		if strings.HasPrefix(strings.ToLower(reference), "os:") {
			if err := store.DeleteSecret(reference); err != nil {
				return fmt.Errorf("LAN access disabled but token cleanup failed: %w", err)
			}
		}
		fmt.Fprintln(stdout, "LAN edge mode disabled; IPC returned to loopback defaults.")
		return nil
	default:
		return errors.New("usage: controller network advertise|discover|list|connect|probe|edge-enable|edge-disable|peer-add|peer-remove|status")
	}
}

func boundedDiscoveryInstanceName(value string) string {
	value = strings.TrimSpace(value)
	for len(value) > 63 {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	if value == "" {
		return "PCController"
	}
	return value
}

func optionsTimeoutMilliseconds(value time.Duration) int64 {
	milliseconds := value.Milliseconds()
	if milliseconds < 100 {
		return 100
	}
	if milliseconds > 30_000 {
		return 30_000
	}
	return milliseconds
}

func parseDiscoveryOptions(protocols string) (discovery.Options, error) {
	options := discovery.Options{}
	for _, protocol := range strings.Split(protocols, ",") {
		switch strings.ToLower(strings.TrimSpace(protocol)) {
		case "all":
			options.MDNS, options.DNSSD, options.SSDP, options.UPnP = true, true, true, true
			options.WSDiscovery, options.Broadcast, options.NetBIOS = true, true, true
		case "mdns", "dns-sd", "dnssd":
			options.MDNS, options.DNSSD = true, true
		case "ssdp":
			options.SSDP = true
		case "upnp":
			options.SSDP, options.UPnP = true, true
		case "ws-discovery", "wsd":
			options.WSDiscovery = true
		case "broadcast", "udp":
			options.Broadcast = true
		case "netbios", "nbns":
			options.NetBIOS = true
		case "":
		default:
			return discovery.Options{}, fmt.Errorf("unsupported discovery protocol %q", protocol)
		}
	}
	return options, nil
}

func enabledProtocolNames(value appconfig.Discovery) []string {
	result := []string{}
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

func resolveDiscoveredInstance(instances []discovery.Instance, target string) (discovery.Instance, error) {
	target = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(target), "."))
	matches := make([]discovery.Instance, 0, 1)
	for _, instance := range instances {
		values := []string{instance.Name, instance.Host, instance.USN}
		values = append(values, instance.Addresses...)
		if instance.Public != nil {
			values = append(values, instance.Public.Hostname, instance.Public.InstanceID, instance.Public.InstanceName)
		}
		for _, value := range values {
			if strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), ".")) == target {
				matches = append(matches, instance)
				break
			}
		}
	}
	if len(matches) == 0 {
		return discovery.Instance{}, fmt.Errorf("no discovered PCController matches %q", target)
	}
	if len(matches) > 1 {
		return discovery.Instance{}, fmt.Errorf("%q matches %d discovered hosts; use the instance id or address", target, len(matches))
	}
	return matches[0], nil
}

func discoveredAddress(instance discovery.Instance) (string, error) {
	host := strings.TrimSpace(instance.Host)
	if len(instance.Addresses) != 0 {
		host = strings.TrimSpace(instance.Addresses[0])
	}
	if host == "" || instance.Port < 1 {
		return "", errors.New("discovered host has no connectable address")
	}
	return net.JoinHostPort(strings.TrimSuffix(host, "."), strconv.Itoa(instance.Port)), nil
}

type edgeProbeEndpoints struct {
	Address      string
	WebSocketURL string
	SocketIOURL  string
}

func discoveredProbeEndpoints(instance discovery.Instance, address string) edgeProbeEndpoints {
	result := edgeProbeEndpoints{Address: address}
	if instance.Public != nil {
		result.WebSocketURL = pinnedWebSocketURL(address, instance.Public.Endpoints.WebSocket, "/ipc")
		result.SocketIOURL = pinnedWebSocketURL(address, instance.Public.Endpoints.SocketIO, "/socket.io/")
	}
	if result.WebSocketURL == "" {
		result.WebSocketURL = pinnedWebSocketURL(address, "", "/ipc")
	}
	if result.SocketIOURL == "" {
		result.SocketIOURL = pinnedWebSocketURL(address, "", "/socket.io/")
	}
	return result
}

func pinnedWebSocketURL(address, advertised, fallbackPath string) string {
	path, rawQuery := fallbackPath, ""
	if parsed, err := url.Parse(strings.TrimSpace(advertised)); err == nil && parsed.User == nil {
		if strings.HasPrefix(parsed.Path, "/") && parsed.Path != "" {
			path = parsed.Path
		}
		rawQuery = parsed.RawQuery
	}
	return (&url.URL{Scheme: "ws", Host: address, Path: path, RawQuery: rawQuery}).String()
}

func lanProbeHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Authenticated controller traffic is LAN-local and must never inherit an
	// Internet proxy from the invoking shell.
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("authenticated LAN probe redirects are not accepted")
		},
	}
}

func probeEdgeNetwork(ctx context.Context, address, origin, token string, output io.Writer) error {
	return probeEdgeNetworkWithEndpoints(ctx, discoveredProbeEndpoints(discovery.Instance{}, address), origin, token, output)
}

func probeEdgeNetworkWithEndpoints(ctx context.Context, endpoints edgeProbeEndpoints, origin, token string, output io.Writer) error {
	address := endpoints.Address
	if _, _, err := net.SplitHostPort(address); err != nil {
		return fmt.Errorf("probe address must be host:port: %w", err)
	}
	httpClient := lanProbeHTTPClient()
	if token != "" {
		provenAddress, err := verifyEdgeServerProof(ctx, httpClient, address, token)
		if err != nil {
			return err
		}
		address = provenAddress
		fmt.Fprintln(output, "✅ responder-bound server authentication proof")
	}
	endpoints.WebSocketURL = pinnedWebSocketURL(address, endpoints.WebSocketURL, "/ipc")
	endpoints.SocketIOURL = pinnedWebSocketURL(address, endpoints.SocketIOURL, "/socket.io/")
	response, err := ipcjson.Call(ctx, address, ipcjson.Request{Method: "controller.ping", Auth: token})
	if err != nil {
		return fmt.Errorf("raw IPC probe: %w", err)
	}
	if response.Error != nil {
		return fmt.Errorf("raw IPC probe: %w", response.Error)
	}
	fmt.Fprintln(output, "✅ raw IPC")

	var ticket probeTicket
	if token != "" {
		ticket, err = requestProbeTicket(ctx, httpClient, address, origin, token, "websocket")
		if err != nil {
			return err
		}
		fmt.Fprintln(output, "✅ protected HTTP API ticket exchange")
	}
	protocols := []string(nil)
	if ticket.Ticket != "" {
		protocols = []string{ticket.Protocol, "pccontroller.ticket." + ticket.Ticket}
	}
	connection, handshake, err := websocket.Dial(ctx, endpoints.WebSocketURL, &websocket.DialOptions{
		HTTPClient:   httpClient,
		HTTPHeader:   http.Header{"Origin": []string{origin}},
		Subprotocols: protocols,
	})
	if err != nil {
		if handshake != nil {
			return fmt.Errorf("authenticated WebSocket probe returned %s: %w", handshake.Status, err)
		}
		return fmt.Errorf("authenticated WebSocket probe: %w", err)
	}
	defer connection.CloseNow()
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`)); err != nil {
		return fmt.Errorf("write WebSocket JSON-RPC probe: %w", err)
	}
	_, payload, err := connection.Read(ctx)
	if err != nil {
		return fmt.Errorf("read WebSocket JSON-RPC probe: %w", err)
	}
	if !bytes.Contains(payload, []byte(`"ok":true`)) {
		return fmt.Errorf("WebSocket JSON-RPC probe returned an unexpected response: %s", payload)
	}
	fmt.Fprintln(output, "✅ WebSocket JSON-RPC")

	var socketTicket probeTicket
	if token != "" {
		socketTicket, err = requestProbeTicket(ctx, httpClient, address, origin, token, "socket_io")
		if err != nil {
			return err
		}
	}
	if err := probeSocketIO(ctx, httpClient, endpoints.SocketIOURL, origin, socketTicket); err != nil {
		return err
	}
	fmt.Fprintln(output, "✅ Socket.IO RPC")
	fmt.Fprintln(output, "LAN IPC/API/WebSocket/Socket.IO probe complete; no credential value was printed.")
	return nil
}

func verifyEdgeServerProof(ctx context.Context, client *http.Client, address, token string) (string, error) {
	rawNonce := make([]byte, 32)
	if _, err := rand.Read(rawNonce); err != nil {
		return "", fmt.Errorf("generate server-proof nonce: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(rawNonce)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+ipcjson.ServerProofPath, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("X-PCController-Nonce", nonce)
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("server authentication proof: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server authentication proof returned %s", response.Status)
	}
	var proof ipcjson.ServerProof
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proof); err != nil {
		return "", fmt.Errorf("decode server authentication proof: %w", err)
	}
	if proof.Nonce != nonce || !ipcjson.VerifyServerProof(token, proof) {
		return "", errors.New("server authentication proof does not match the configured token")
	}
	if err := verifyProofAudience(ctx, address, proof.Audience); err != nil {
		return "", err
	}
	return proof.Audience, nil
}

func verifyProofAudience(ctx context.Context, requested, audience string) error {
	requestedHost, requestedPort, err := net.SplitHostPort(requested)
	if err != nil {
		return fmt.Errorf("split requested proof audience: %w", err)
	}
	audienceHost, audiencePort, err := net.SplitHostPort(audience)
	if err != nil || audiencePort != requestedPort {
		return fmt.Errorf("server proof audience %q does not match requested endpoint %q", audience, requested)
	}
	audienceIP := net.ParseIP(strings.Trim(audienceHost, "[]"))
	if audienceIP == nil {
		return fmt.Errorf("server proof audience %q is not an IP endpoint", audience)
	}
	requestedIP := net.ParseIP(strings.Trim(requestedHost, "[]"))
	if requestedIP != nil {
		if !requestedIP.Equal(audienceIP) {
			return fmt.Errorf("server proof was relayed from %s instead of %s", audienceIP, requestedIP)
		}
		return nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, requestedHost)
	if err != nil {
		return fmt.Errorf("resolve requested server-proof host: %w", err)
	}
	for _, address := range addresses {
		if address.IP.Equal(audienceIP) {
			return nil
		}
	}
	return fmt.Errorf("server proof audience %s is not an address of %s", audienceIP, requestedHost)
}

type probeTicket struct {
	Ticket   string `json:"ticket"`
	Protocol string `json:"protocol"`
}

func requestProbeTicket(ctx context.Context, client *http.Client, address, origin, token, transport string) (probeTicket, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, "http://"+address+ipcjson.SessionTicketPath,
		bytes.NewBufferString(`{"transport":"`+transport+`"}`),
	)
	if err != nil {
		return probeTicket{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	response, err := client.Do(request)
	if err != nil {
		return probeTicket{}, fmt.Errorf("protected HTTP API %s ticket probe: %w", transport, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return probeTicket{}, fmt.Errorf("protected HTTP API %s ticket probe returned %s", transport, response.Status)
	}
	var ticket probeTicket
	if err := json.NewDecoder(io.LimitReader(response.Body, 16*1024)).Decode(&ticket); err != nil {
		return probeTicket{}, fmt.Errorf("decode protected HTTP API %s ticket response: %w", transport, err)
	}
	if ticket.Ticket == "" || ticket.Protocol == "" {
		return probeTicket{}, fmt.Errorf("protected HTTP API returned an incomplete %s ticket", transport)
	}
	return ticket, nil
}

func probeSocketIO(ctx context.Context, client *http.Client, endpoint, origin string, ticket probeTicket) error {
	target, err := url.Parse(endpoint)
	if err != nil || target.Host == "" {
		return errors.New("discovered Socket.IO endpoint is invalid")
	}
	query := target.Query()
	query.Set("EIO", "4")
	query.Set("transport", "websocket")
	target.RawQuery = query.Encode()
	protocols := []string(nil)
	if ticket.Ticket != "" {
		protocols = []string{ticket.Protocol, "pccontroller.ticket." + ticket.Ticket}
	}
	connection, handshake, err := websocket.Dial(ctx, target.String(), &websocket.DialOptions{
		HTTPClient: client, HTTPHeader: http.Header{"Origin": []string{origin}},
		Subprotocols: protocols,
	})
	if err != nil {
		if handshake != nil {
			return fmt.Errorf("authenticated Socket.IO probe returned %s: %w", handshake.Status, err)
		}
		return fmt.Errorf("authenticated Socket.IO probe: %w", err)
	}
	defer connection.CloseNow()
	read := func(stage string) (string, error) {
		_, payload, readErr := connection.Read(ctx)
		if readErr != nil {
			return "", fmt.Errorf("read Socket.IO %s: %w", stage, readErr)
		}
		return string(payload), nil
	}
	packet, err := read("Engine.IO open")
	if err != nil || !strings.HasPrefix(packet, "0{") {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected Engine.IO open packet %q", packet)
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte("40")); err != nil {
		return fmt.Errorf("write Socket.IO connect: %w", err)
	}
	for {
		packet, err = read("connect")
		if err != nil {
			return err
		}
		if packet == "2" {
			_ = connection.Write(ctx, websocket.MessageText, []byte("3"))
			continue
		}
		if strings.HasPrefix(packet, "40") {
			break
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`42["rpc",{"jsonrpc":"2.0","id":1,"method":"controller.ping"}]`)); err != nil {
		return fmt.Errorf("write Socket.IO RPC probe: %w", err)
	}
	for {
		packet, err = read("RPC response")
		if err != nil {
			return err
		}
		if packet == "2" {
			_ = connection.Write(ctx, websocket.MessageText, []byte("3"))
			continue
		}
		if strings.HasPrefix(packet, `42["rpc.response",`) && strings.Contains(packet, `"ok":true`) {
			return nil
		}
	}
}

type stringListFlag []string

func (values *stringListFlag) String() string { return strings.Join(*values, ",") }

func (values *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "*" || value == "*:*" || strings.ContainsAny(value, "\r\n") {
		return errors.New("origin must be a non-wildcard host or host:port pattern")
	}
	*values = append(*values, value)
	return nil
}
