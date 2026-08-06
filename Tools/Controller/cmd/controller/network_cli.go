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
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/ipcjson"
)

func runNetwork(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	if len(args) == 0 {
		return errors.New("usage: controller network edge-enable|edge-disable|peer-add|peer-remove|probe|status")
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
		principal := flags.String("principal", "lan-operator", "remote audit principal")
		secretRef := flags.String("secret-ref", "os:ipc/edge-lan", "OS-vault reference for the generated bearer token")
		instance := flags.String("instance", "", "mDNS/SSDP instance name (hostname by default)")
		tokenEnvironment := flags.String("token-env", "", "read an existing bearer token from this environment variable instead of generating one")
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
		}
		var token string
		if name := strings.TrimSpace(*tokenEnvironment); name != "" {
			token = strings.TrimSpace(os.Getenv(name))
			if len(token) < 24 || len(token) > 512 || strings.ContainsAny(token, "\r\n\t ") {
				return fmt.Errorf("%s must contain a 24..512 byte bearer token without whitespace", name)
			}
		} else {
			tokenBytes := make([]byte, 32)
			if _, err := rand.Read(tokenBytes); err != nil {
				return fmt.Errorf("generate LAN bearer token: %w", err)
			}
			token = base64.RawURLEncoding.EncodeToString(tokenBytes)
		}
		if err := store.SetSecret(*secretRef, token); err != nil {
			return fmt.Errorf("store LAN bearer token: %w", err)
		}
		_, err = store.Update(func(config *appconfig.Config) error {
			config.IPC.Listen = *listen
			config.IPC.AllowRemote = true
			config.IPC.AuthToken, config.IPC.AuthTokenRef = "", *secretRef
			config.IPC.RemotePrincipal = *principal
			config.IPC.AllowedOrigins = append([]string(nil), origins...)
			config.IPC.RemotePolicy = appconfig.RemoteAccessPolicy{
				Read: true, Events: true, Messages: true, BoardCommands: true,
				HostConfiguration: true, ConnectionControl: true, Reset: true,
				Programming: true, BridgeCalls: true, Integrations: true,
			}
			config.Integrations.Discovery.MDNSEnabled = true
			config.Integrations.Discovery.SSDPEnabled = true
			config.Integrations.Discovery.InstanceName = strings.TrimSpace(*instance)
			return nil
		})
		if err != nil {
			_ = store.DeleteSecret(*secretRef)
			return err
		}
		fmt.Fprintln(stdout, "LAN edge mode enabled with an OS-vault bearer token (value hidden).")
		fmt.Fprintln(stdout, "IPC/API/WebSocket:", *listen)
		fmt.Fprintln(stdout, "Discovery: mDNS + SSDP as", *instance)
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
		flags.Var(&topics, "topic", "events or status subscription, repeatable")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*name) == "" || strings.TrimSpace(*url) == "" || strings.TrimSpace(*secretRef) == "" {
			return errors.New("usage: controller network peer-add --name NAME --url ws://HOST:PORT/ipc --secret-ref REF [--topic events|status]")
		}
		if len(topics) == 0 {
			topics = []string{"events", "status"}
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
		fmt.Fprintf(stdout, "LAN peer %q configured; restart or reload the primary host to connect.\n", peer.Name)
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
		fmt.Fprintf(stdout, "LAN peer %q removed; its separate vault value was preserved.\n", strings.TrimSpace(*name))
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
		if flags.NArg() != 0 || strings.TrimSpace(*address) == "" || strings.TrimSpace(*tokenRef) == "" {
			return errors.New("usage: controller network probe --addr HOST:PORT --token-ref REF [--origin URL]")
		}
		token, err := store.ResolveSecret(*tokenRef)
		if err != nil {
			return fmt.Errorf("resolve LAN probe token: %w", err)
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
	case "edge-disable", "disable-edge":
		if len(args) != 1 {
			return errors.New("usage: controller network edge-disable")
		}
		current := store.Current()
		reference := current.IPC.AuthTokenRef
		_, err := store.Update(func(config *appconfig.Config) error {
			defaults := appconfig.Defaults().IPC
			config.IPC = defaults
			config.Integrations.Discovery = appconfig.Discovery{}
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
		fmt.Fprintln(stdout, "LAN edge mode disabled; IPC returned to authenticated-safe loopback defaults.")
		return nil
	default:
		return errors.New("usage: controller network edge-enable|edge-disable|peer-add|peer-remove|probe|status")
	}
}

func probeEdgeNetwork(ctx context.Context, address, origin, token string, output io.Writer) error {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return fmt.Errorf("probe address must be host:port: %w", err)
	}
	response, err := ipcjson.Call(ctx, address, ipcjson.Request{Method: "controller.ping", Auth: token})
	if err != nil {
		return fmt.Errorf("raw IPC probe: %w", err)
	}
	if response.Error != nil {
		return fmt.Errorf("raw IPC probe: %w", response.Error)
	}
	fmt.Fprintln(output, "✅ authenticated raw IPC")

	ticketRequest, err := http.NewRequestWithContext(
		ctx, http.MethodPost, "http://"+address+ipcjson.SessionTicketPath,
		bytes.NewBufferString(`{"transport":"websocket"}`),
	)
	if err != nil {
		return err
	}
	ticketRequest.Header.Set("Authorization", "Bearer "+token)
	ticketRequest.Header.Set("Content-Type", "application/json")
	ticketRequest.Header.Set("Origin", origin)
	ticketResponse, err := http.DefaultClient.Do(ticketRequest)
	if err != nil {
		return fmt.Errorf("protected HTTP API probe: %w", err)
	}
	defer ticketResponse.Body.Close()
	if ticketResponse.StatusCode != http.StatusCreated {
		return fmt.Errorf("protected HTTP API probe returned %s", ticketResponse.Status)
	}
	var ticket struct {
		Ticket   string `json:"ticket"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(io.LimitReader(ticketResponse.Body, 16*1024)).Decode(&ticket); err != nil {
		return fmt.Errorf("decode protected HTTP API response: %w", err)
	}
	if ticket.Ticket == "" || ticket.Protocol == "" {
		return errors.New("protected HTTP API returned an incomplete WebSocket ticket")
	}
	fmt.Fprintln(output, "✅ protected HTTP API ticket exchange")

	connection, handshake, err := websocket.Dial(ctx, "ws://"+address+"/ipc", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{origin}},
		Subprotocols: []string{ticket.Protocol, "pccontroller.ticket." + ticket.Ticket},
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
	fmt.Fprintln(output, "✅ one-use-ticket WebSocket JSON-RPC")
	fmt.Fprintln(output, "LAN IPC/API/WebSocket probe complete; no credential value was printed.")
	return nil
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
