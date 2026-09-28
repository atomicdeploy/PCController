package appconfig

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func validateAllowedOriginPattern(value string) error {
	if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, " \t\r\n/?#@\\") {
		return fmt.Errorf("use an exact HOST:PORT or HOST:* value")
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil || host == "" || strings.ContainsAny(host, "*?[]") {
		return fmt.Errorf("use a non-wildcard host and an exact port or *")
	}
	if parsed := net.ParseIP(host); parsed == nil {
		if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
			return fmt.Errorf("hostname must not start or end with a dot")
		}
		for _, label := range strings.Split(host, ".") {
			if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return fmt.Errorf("hostname contains an invalid label")
			}
			for _, character := range label {
				if (character >= 'a' && character <= 'z') ||
					(character >= 'A' && character <= 'Z') ||
					(character >= '0' && character <= '9') || character == '-' {
					continue
				}
				return fmt.Errorf("hostname contains an invalid character")
			}
		}
	}
	if port == "*" {
		return nil
	}
	numericPort, err := strconv.Atoi(port)
	if err != nil || numericPort < 1 || numericPort > 65535 {
		return fmt.Errorf("port must be 1..65535 or *")
	}
	return nil
}
