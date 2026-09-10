// Package deployment defines the explicit workflow classification used by
// every firmware upload surface. Alpha version strings never imply permission
// to skip a protected checkpoint. Physical live use does not classify firmware.
package deployment

import (
	"fmt"
	"os"
	"strings"
)

const (
	Production  = "production"
	Development = "development"
	Environment = "PCCONTROLLER_DEPLOYMENT"
)

func Normalize(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", Production:
		return Production, nil
	case Development:
		return Development, nil
	default:
		return "", fmt.Errorf("deployment must be production or development, got %q", value)
	}
}

// Resolve follows explicit request/flag > environment > persisted config >
// production. Invalid selected values fail closed, never silently downgrade.
func Resolve(configured, explicit string) (string, error) {
	value := configured
	if environment := strings.TrimSpace(os.Getenv(Environment)); environment != "" {
		value = environment
	}
	if strings.TrimSpace(explicit) != "" {
		value = explicit
	}
	return Normalize(value)
}

// EEPROM reinitialization destroys settings, so it always keeps a raw backup.
func RequiresBackup(classification string, reinitializeEEPROM bool) bool {
	return classification != Development || reinitializeEEPROM
}
