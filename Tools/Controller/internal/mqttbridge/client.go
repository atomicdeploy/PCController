//go:build !no_mqtt

package mqttbridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const Available = true
const maxPublishBytes = 256 * 1024

type delivery struct {
	data                []byte
	retained, duplicate bool
}

// Run owns clean, bounded broker sessions. It never reconnects or replays a
// command within the MQTT library, and only retains the latest state.
func Run(ctx context.Context, config Config, hooks Hooks) {
	if !config.Enabled {
		return
	}
	report := func(connected bool, failure string) {
		hooks.Status(Status{Available: true, Enabled: true, Connected: connected, Error: failure})
	}
	if err := config.Validate(); err != nil {
		report(false, err.Error())
		return
	}
	if config.PasswordRef != "" {
		report(false, "MQTT password reference is unavailable")
		return
	}
	baseIdentity := hooks.Identity
	epoch := rand.Text()
	hooks.Identity = func() Identity {
		identity := baseIdentity()
		identity.BootID += ":" + epoch
		return identity
	}
	seen := make(map[string]int64)
	for backoff := time.Second; ctx.Err() == nil; backoff = min(backoff*2, 30*time.Second) {
		started := time.Now()
		err := session(ctx, config, hooks, epoch, seen, report)
		if ctx.Err() != nil {
			return
		}
		report(false, err.Error())
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func waitToken(ctx context.Context, token mqtt.Token, limit time.Duration) error {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("MQTT acknowledgement timed out")
	case <-token.Done():
		return token.Error()
	}
}

func session(ctx context.Context, cfg Config, h Hooks, epoch string, seen map[string]int64, report func(bool, string)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	endpoint, _ := cfg.URL() // Validated before starting the reconnect loop.
	tlsConfig, err := brokerTLS(endpoint.Hostname(), cfg.CAFile)
	if err != nil {
		return err
	}
	commandTopic, availability := cfg.Topic+"/command", cfg.Topic+"/availability"
	incoming, lost := make(chan delivery, 8), make(chan error, 1)
	// A topic is a deployment identity, independent of hostname or process.
	options := mqtt.NewClientOptions().AddBroker(endpoint.String()).
		SetProtocolVersion(4).SetClientID(clientID(cfg.Topic)).
		SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).
		SetKeepAlive(30*time.Second).SetPingTimeout(5*time.Second).
		SetConnectTimeout(5*time.Second).SetWriteTimeout(2*time.Second).
		SetUsername(cfg.Username).SetPassword(cfg.Password).
		SetWill(availability, "offline", 1, true).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			select {
			case lost <- err:
			default:
			}
			cancel()
		})
	var mu sync.Mutex
	var raw net.Conn
	closeWire := func() {
		mu.Lock()
		defer mu.Unlock()
		if raw != nil {
			_ = raw.Close()
		}
	}
	options.SetCustomOpenConnectionFn(func(u *url.URL, _ mqtt.ClientOptions) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		var conn net.Conn
		var err error
		if u.Scheme == "tls" {
			tlsDialer := tls.Dialer{NetDialer: dialer, Config: tlsConfig}
			conn, err = tlsDialer.DialContext(ctx, "tcp", u.Host)
		} else {
			conn, err = dialer.DialContext(ctx, "tcp", u.Host)
		}
		if err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			_ = conn.Close()
			return nil, ctx.Err()
		}
		raw = conn
		return &packetConn{Conn: conn}, nil
	})
	options.SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) {
		if m.Topic() != commandTopic || len(m.Payload()) > maxCommandBytes || ctx.Err() != nil {
			return
		}
		select {
		case incoming <- delivery{append([]byte(nil), m.Payload()...), m.Retained(), m.Duplicate()}:
		default: // Bounded queue; never block the reader or retry a mutation.
		}
	})
	client := mqtt.NewClient(options)
	defer func() {
		if client.IsConnectionOpen() {
			// On graceful shutdown publish to the original topic. If the
			// write fails, drop TCP first so the broker emits the Will.
			if waitToken(context.Background(), client.Publish(availability, 1, true, "offline"), 500*time.Millisecond) != nil {
				closeWire()
			}
		}
		client.Disconnect(250)
		cancel()
		closeWire()
	}()
	if err := waitToken(ctx, client.Connect(), 7*time.Second); err != nil {
		return fmt.Errorf("MQTT broker connection: %w", err)
	}
	token := client.Subscribe(commandTopic, 1, nil)
	if err := waitToken(ctx, token, 2*time.Second); err != nil {
		return fmt.Errorf("MQTT subscribe: %w", err)
	}
	subscription, ok := token.(*mqtt.SubscribeToken)
	if !ok {
		return errors.New("MQTT command subscription was not acknowledged")
	}
	if qos, present := subscription.Result()[commandTopic]; !present || qos > 1 {
		return errors.New("MQTT broker refused the command subscription")
	}
	publish := func(topic string, retained bool, value any) error {
		body, err := json.Marshal(value)
		if err != nil || len(body) > maxPublishBytes {
			return errors.New("MQTT publication exceeds limits or is not serializable")
		}
		return waitToken(ctx, client.Publish(cfg.Topic+"/"+topic, 1, retained, body), 2*time.Second)
	}
	status := func() any {
		id, state := h.Observe()
		return struct {
			BoardID string `json:"board_id"`
			BootID  string `json:"boot_id"`
			UTC     int64  `json:"utc_epoch"`
			State   any    `json:"state"`
		}{id.BoardID, id.BootID + ":" + epoch, time.Now().Unix(), state}
	}
	if err := publish("status", true, status()); err != nil {
		return err
	}
	if err := waitToken(ctx, client.Publish(availability, 1, true, "online"), 2*time.Second); err != nil {
		return err
	}
	report(true, "")
	// Notifications collected without a broker are not a replay backlog.
	for n := len(h.Events); n > 0; n-- {
		<-h.Events
	}
	interval := cfg.TelemetrySeconds
	if interval == 0 {
		interval = 5
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	// Coalesce state changes and keep the final observation when activity stops.
	var due <-chan time.Time
	settle := time.NewTimer(time.Hour)
	settle.Stop()
	defer settle.Stop()
	for {
		select {
		case <-ctx.Done():
			select {
			case err := <-lost:
				return err
			default:
				return ctx.Err()
			}
		case <-h.State:
			if due == nil {
				settle.Reset(250 * time.Millisecond)
				due = settle.C
			}
		case <-due:
			due = nil
			if err := publish("status", true, status()); err != nil {
				return err
			}
		case <-ticker.C:
			if err := publish("status", true, status()); err != nil {
				return err
			}
		case event := <-h.Events:
			if err := publish("event", false, event); err != nil {
				return err
			}
		case item := <-incoming:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			command, receipt := admit(item.data, item.retained, item.duplicate, cfg, h.Identity(), seen, time.Now())
			if receipt.Accepted {
				if command.Command != "rpc" {
					receipt.Result = status()
				} else {
					deadline := minTime(time.Now().Add(5*time.Second), time.Unix(command.ExpiresAt, 0))
					call, stop := context.WithDeadline(ctx, deadline)
					result := h.Execute(call, command)
					stop()
					receipt.Accepted, receipt.Code, receipt.Result, receipt.Message = result.Accepted, result.Code, result.Result, result.Message
				}
			}
			if err := publish("event", false, receipt); err != nil {
				return err
			}
		}
	}
}

func clientID(topic string) string {
	sum := sha256.Sum256([]byte(topic))
	return "pcc" + hex.EncodeToString(sum[:10])
}

func brokerTLS(host, caFile string) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if caFile == "" {
		return config, nil
	}
	file, err := os.Open(caFile)
	if err != nil {
		return nil, errors.New("MQTT could not open the configured CA bundle")
	}
	defer file.Close()
	const maxCA = 1 << 20
	pem, err := io.ReadAll(io.LimitReader(file, maxCA+1))
	if err != nil || len(pem) > maxCA {
		return nil, errors.New("MQTT CA bundle could not be read or exceeds 1 MiB")
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("MQTT CA bundle contains no PEM certificates")
	}
	config.RootCAs = pool
	return config, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
