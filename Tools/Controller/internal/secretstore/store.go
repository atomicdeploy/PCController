// Package secretstore resolves durable operating-system and transient
// environment-backed secret references without exposing their values.
package secretstore

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	// MaxSecretBytes is the conservative Credential Manager generic-credential
	// blob limit used consistently by every backend.
	MaxSecretBytes    = 2560
	maxReferenceBytes = 160
)

var (
	// ErrUnavailable means the requested operating-system vault backend is not
	// available on the current platform or user session.
	ErrUnavailable = errors.New("operating-system secret store is unavailable")
	// ErrNotFound means a syntactically valid reference has no stored value.
	ErrNotFound       = errors.New("secret reference was not found")
	secretNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// Status describes the backend without revealing secret names or values.
type Status struct {
	Provider  string `json:"provider"`
	Available bool   `json:"available"`
	Durable   bool   `json:"durable"`
	Scope     string `json:"scope"`
}

// Backend stores named secrets for one operating-system user.
type Backend interface {
	Status() Status
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}

// Resolver combines an OS vault backend with explicitly named environment
// variables. It never falls back from one scheme to another.
type Resolver struct {
	backend Backend
}

// New creates a resolver whose OS namespace is stable for this application.
func New(namespace string) *Resolver {
	return &Resolver{backend: newPlatformBackend(namespace)}
}

// NewWithBackend injects a backend for deterministic tests.
func NewWithBackend(backend Backend) *Resolver {
	if backend == nil {
		backend = unavailableBackend{}
	}
	return &Resolver{backend: backend}
}

// Status reports only backend capability metadata.
func (resolver *Resolver) Status() Status {
	if resolver == nil || resolver.backend == nil {
		return unavailableBackend{}.Status()
	}
	return resolver.backend.Status()
}

// Resolve loads one explicit os: or env: reference.
func (resolver *Resolver) Resolve(reference string) (string, error) {
	scheme, name, err := ParseReference(reference)
	if err != nil {
		return "", err
	}
	switch scheme {
	case "env":
		value, present := os.LookupEnv(name)
		if !present || value == "" {
			return "", fmt.Errorf("resolve %s: %w", reference, ErrNotFound)
		}
		if err := ValidateValue(value); err != nil {
			return "", fmt.Errorf("resolve %s: %w", reference, err)
		}
		return value, nil
	case "os":
		if resolver == nil || resolver.backend == nil {
			return "", fmt.Errorf("resolve %s: %w", reference, ErrUnavailable)
		}
		value, err := resolver.backend.Get(name)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", reference, err)
		}
		if err := ValidateValue(value); err != nil {
			return "", fmt.Errorf("resolve %s: %w", reference, err)
		}
		return value, nil
	default:
		panic("validated secret reference returned an unknown scheme")
	}
}

// Set writes one durable os: reference. Environment references are controlled
// only by the process environment and cannot be mutated by the application.
func (resolver *Resolver) Set(reference, value string) error {
	scheme, name, err := ParseReference(reference)
	if err != nil {
		return err
	}
	if scheme != "os" {
		return errors.New("only os: references can be written; set env: references in the process environment")
	}
	if err := ValidateValue(value); err != nil {
		return err
	}
	if resolver == nil || resolver.backend == nil {
		return ErrUnavailable
	}
	return resolver.backend.Set(name, value)
}

// Delete removes one durable os: reference. Environment variables remain
// owned by the process launcher.
func (resolver *Resolver) Delete(reference string) error {
	scheme, name, err := ParseReference(reference)
	if err != nil {
		return err
	}
	if scheme != "os" {
		return errors.New("only os: references can be deleted")
	}
	if resolver == nil || resolver.backend == nil {
		return ErrUnavailable
	}
	return resolver.backend.Delete(name)
}

// ParseReference validates and separates an explicit secret-reference scheme.
func ParseReference(reference string) (scheme, name string, err error) {
	if reference != strings.TrimSpace(reference) || len(reference) > maxReferenceBytes {
		return "", "", errors.New("secret reference must be trimmed and at most 160 bytes")
	}
	scheme, name, found := strings.Cut(reference, ":")
	scheme = strings.ToLower(scheme)
	if !found || (scheme != "os" && scheme != "env") {
		return "", "", errors.New("secret reference must use the os: or env: scheme")
	}
	if !secretNamePattern.MatchString(name) {
		return "", "", errors.New("secret reference name must start with an alphanumeric character and contain only letters, digits, dot, underscore, slash, or hyphen")
	}
	if scheme == "env" && strings.Contains(name, "/") {
		return "", "", errors.New("environment secret names cannot contain a slash")
	}
	return scheme, name, nil
}

// ValidateReference verifies a reference without resolving it.
func ValidateReference(reference string) error {
	_, _, err := ParseReference(reference)
	return err
}

// ValidateValue applies the common bounded text contract before values enter
// a backend or runtime configuration.
func ValidateValue(value string) error {
	if value == "" {
		return errors.New("secret value must not be empty")
	}
	if len(value) > MaxSecretBytes {
		return fmt.Errorf("secret value exceeds %d bytes", MaxSecretBytes)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return errors.New("secret value must not contain control characters")
		}
	}
	return nil
}

type unavailableBackend struct{}

func (unavailableBackend) Status() Status {
	return Status{Provider: "unavailable", Scope: "current-user"}
}
func (unavailableBackend) Get(string) (string, error) { return "", ErrUnavailable }
func (unavailableBackend) Set(string, string) error   { return ErrUnavailable }
func (unavailableBackend) Delete(string) error        { return ErrUnavailable }
