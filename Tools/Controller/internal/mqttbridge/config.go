// Package mqttbridge implements the opt-in outbound MQTT transport.
package mqttbridge

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Config struct {
	Enabled          bool   `json:"enabled"`
	Broker           string `json:"broker,omitempty"`
	Topic            string `json:"topic,omitempty"`
	Username         string `json:"username,omitempty"`
	PasswordRef      string `json:"password_ref,omitempty"`
	CAFile           string `json:"ca_file,omitempty"`
	Password         string `json:"-"`
	TelemetrySeconds int    `json:"telemetry_seconds,omitempty"`
	AllowCommands    bool   `json:"allow_commands"`
}

func (c Config) Validate() error {
	if c.TelemetrySeconds < 0 || c.TelemetrySeconds > 3600 {
		return errors.New("MQTT telemetry_seconds must be 1..3600, or 0 for the default")
	}
	if len(c.Username) > 256 || strings.ContainsAny(c.Username, "\x00\r\n") || (c.PasswordRef != "" || c.Password != "") && c.Username == "" {
		return errors.New("MQTT password requires a valid username")
	}
	if len(c.Password) > 4096 {
		return errors.New("MQTT password exceeds 4096 bytes")
	}
	if c.Broker != "" || c.Enabled || c.CAFile != "" {
		u, err := c.URL()
		if err != nil {
			return err
		}
		if c.CAFile != "" && u.Scheme != "tls" {
			return errors.New("MQTT CA file requires a TLS broker")
		}
	}
	if c.Topic != "" || c.Enabled {
		if len(c.Topic) == 0 || len(c.Topic) > 160 {
			return errors.New("MQTT topic prefix must be 1..160 ASCII characters")
		}
		for _, part := range strings.Split(c.Topic, "/") {
			if part == "" || !identifier(part) {
				return errors.New("MQTT topic segments must contain only ASCII letters, digits, underscore, hyphen or dot")
			}
		}
	}
	return nil
}

func (c Config) URL() (*url.URL, error) {
	u, err := url.Parse(c.Broker)
	if err != nil || len(c.Broker) > 512 || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("MQTT broker must be mqtts://HOST[:PORT] without credentials, path or query")
	}
	port := u.Port()
	switch u.Scheme {
	case "mqtts", "tls":
		u.Scheme = "tls"
		if port == "" {
			port = "8883"
		}
	case "mqtt", "tcp":
		if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
			return nil, errors.New("plaintext MQTT requires a literal loopback IP; use mqtts for remote brokers")
		}
		u.Scheme = "tcp"
		if port == "" {
			port = "1883"
		}
	default:
		return nil, errors.New("MQTT broker scheme must be mqtts or mqtt (loopback only)")
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return nil, errors.New("invalid MQTT broker port")
	}
	u.Host = net.JoinHostPort(u.Hostname(), port)
	return u, nil
}

func identifier(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

type Status struct {
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}
