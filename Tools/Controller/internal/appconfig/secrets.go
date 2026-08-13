package appconfig

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"pccontroller.local/controller/internal/secretstore"
)

// SecretUse describes one configured secret location without its value.
type SecretUse struct {
	Path      string `json:"path"`
	Reference string `json:"reference,omitempty"`
	Source    string `json:"source"`
	Present   bool   `json:"present"`
	Error     string `json:"error,omitempty"`
}

// SecretStatus describes the vault and configured uses without enumerating
// unrelated vault entries or exposing secret material.
type SecretStatus struct {
	Backend secretstore.Status `json:"backend"`
	Uses    []SecretUse        `json:"uses"`
}

// Runtime resolves configured references into an isolated in-memory copy.
func (store *Store) Runtime() (Config, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return resolveConfigSecrets(store.effectiveLocked(), store.secrets)
}

// CurrentRuntime returns a fail-closed runtime view. Call Runtime when the
// caller can surface resolution errors; this helper is for long-lived request
// paths that must immediately stop remote/integration access on vault failure.
func (store *Store) CurrentRuntime() Config {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.runtimeLocked()
}

// Redacted returns a serializable config view with plaintext secret material
// absent while preserving non-secret references for diagnostics.
func (store *Store) Redacted() Config {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return Redacted(store.effectiveLocked())
}

// Redacted removes every known plaintext authentication value from a copy.
func Redacted(value Config) Config {
	result := clone(value)
	result.IPC.AuthToken = ""
	for index := range result.Integrations.WebSocketClients {
		result.Integrations.WebSocketClients[index].AuthToken = ""
	}
	for index := range result.Integrations.OutboundWebhooks {
		webhook := &result.Integrations.OutboundWebhooks[index]
		webhook.SigningSecret = ""
		for key := range webhook.Headers {
			if sensitiveHeaderName(key) {
				delete(webhook.Headers, key)
			}
		}
	}
	return result
}

// SecretsStatus reports configured references and legacy plaintext presence
// without returning any value or vault-wide credential list.
func (store *Store) SecretsStatus() SecretStatus {
	store.mu.RLock()
	value := clone(store.value)
	resolver := store.secrets
	store.mu.RUnlock()
	status := SecretStatus{}
	if resolver != nil {
		status.Backend = resolver.Status()
	}
	for _, use := range configuredSecretUses(value) {
		if use.Reference == "" {
			use.Source, use.Present = "plaintext-config", true
			status.Uses = append(status.Uses, use)
			continue
		}
		scheme, _, _ := secretstore.ParseReference(use.Reference)
		use.Source = scheme
		var err error
		if resolver == nil {
			err = secretstore.ErrUnavailable
		} else {
			_, err = resolver.Resolve(use.Reference)
		}
		use.Present = err == nil
		if err != nil {
			use.Error = err.Error()
		}
		status.Uses = append(status.Uses, use)
	}
	sort.Slice(status.Uses, func(i, j int) bool { return status.Uses[i].Path < status.Uses[j].Path })
	return status
}

// SetSecret stores a durable value and notifies runtime-only subscribers when
// the active config references it. It never edits the config file.
func (store *Store) SetSecret(reference, value string) error {
	if store == nil || store.secrets == nil {
		return secretstore.ErrUnavailable
	}
	if err := store.secrets.Set(reference, value); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, err := resolveConfigSecrets(store.value, store.secrets); err != nil {
		return fmt.Errorf("secret stored but active configuration remains unresolved: %w", err)
	}
	store.notifyRuntimeLocked(store.value)
	return nil
}

// ResolveSecret returns one explicitly requested secret for a short-lived
// client operation. It does not expose or enumerate unrelated vault entries.
func (store *Store) ResolveSecret(reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", errors.New("secret reference is required")
	}
	if store == nil || store.secrets == nil {
		return "", secretstore.ErrUnavailable
	}
	return store.secrets.Resolve(reference)
}

// DeleteSecret removes an unreferenced durable value. The caller must first
// remove every config reference, preventing a running service from silently
// losing authentication or signing.
func (store *Store) DeleteSecret(reference string) error {
	if store == nil || store.secrets == nil {
		return secretstore.ErrUnavailable
	}
	store.mu.RLock()
	uses := configuredSecretUses(store.value)
	store.mu.RUnlock()
	for _, use := range uses {
		if use.Reference == reference {
			return fmt.Errorf("secret %s is still referenced by %s; remove the config reference first", reference, use.Path)
		}
	}
	return store.secrets.Delete(reference)
}

// PurgeConfiguredSecrets removes only durable OS-vault entries referenced by
// this configuration. It exists for the explicit whole-settings reset path;
// normal callers must use DeleteSecret so an active reference cannot silently
// lose its value.
func (store *Store) PurgeConfiguredSecrets() ([]string, error) {
	if store == nil || store.secrets == nil {
		return nil, secretstore.ErrUnavailable
	}
	store.mu.RLock()
	uses := configuredSecretUses(store.value)
	store.mu.RUnlock()
	seen := make(map[string]bool)
	removed := make([]string, 0)
	var failures []error
	for _, use := range uses {
		reference := strings.TrimSpace(use.Reference)
		if reference == "" || seen[reference] {
			continue
		}
		scheme, _, err := secretstore.ParseReference(reference)
		if err != nil || scheme != "os" {
			continue
		}
		seen[reference] = true
		if err := store.secrets.Delete(reference); err != nil && !errors.Is(err, secretstore.ErrNotFound) {
			failures = append(failures, fmt.Errorf("delete %s: %w", reference, err))
			continue
		}
		removed = append(removed, reference)
	}
	sort.Strings(removed)
	return removed, errors.Join(failures...)
}

func resolveConfigSecrets(value Config, resolver *secretstore.Resolver) (Config, error) {
	result := clone(value)
	resolve := func(path string, plaintext *string, reference *string) error {
		if *reference == "" {
			return nil
		}
		if resolver == nil {
			return fmt.Errorf("%s: %w", path, secretstore.ErrUnavailable)
		}
		secret, err := resolver.Resolve(*reference)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		*plaintext, *reference = secret, ""
		return nil
	}
	// Inbound application authentication is dormant during alpha. Preserve the
	// reference in the persisted view, but never resolve or inject it at runtime.
	result.IPC.AuthToken, result.IPC.AuthTokenRef = "", ""
	for index := range result.Integrations.WebSocketClients {
		peer := &result.Integrations.WebSocketClients[index]
		if err := resolve(fmt.Sprintf("integrations.websocket_clients[%d].auth_token", index), &peer.AuthToken, &peer.AuthTokenRef); err != nil {
			return Config{}, err
		}
	}
	for index := range result.Integrations.OutboundWebhooks {
		webhook := &result.Integrations.OutboundWebhooks[index]
		if err := resolve(fmt.Sprintf("integrations.outbound_webhooks[%d].signing_secret", index), &webhook.SigningSecret, &webhook.SigningSecretRef); err != nil {
			return Config{}, err
		}
		if webhook.Headers == nil && len(webhook.SecretHeaders) != 0 {
			webhook.Headers = make(map[string]string, len(webhook.SecretHeaders))
		}
		for key, reference := range webhook.SecretHeaders {
			secret, err := resolver.Resolve(reference)
			if err != nil {
				return Config{}, fmt.Errorf("integrations.outbound_webhooks[%d].secret_headers[%q]: %w", index, key, err)
			}
			webhook.Headers[key] = secret
		}
		webhook.SecretHeaders = nil
	}
	if err := result.Validate(); err != nil {
		return Config{}, fmt.Errorf("resolved configuration: %w", err)
	}
	return result, nil
}

func failClosedRuntime(value Config) Config {
	result := clone(value)
	result.IPC.AuthToken, result.IPC.AuthTokenRef = "", ""
	for index := range result.Integrations.WebSocketClients {
		peer := &result.Integrations.WebSocketClients[index]
		if peer.AuthTokenRef != "" {
			peer.AuthToken, peer.AuthTokenRef, peer.Enabled = "", "", false
		}
	}
	for index := range result.Integrations.OutboundWebhooks {
		webhook := &result.Integrations.OutboundWebhooks[index]
		if webhook.SigningSecretRef != "" || len(webhook.SecretHeaders) != 0 {
			webhook.SigningSecret, webhook.SigningSecretRef, webhook.Enabled = "", "", false
			webhook.SecretHeaders = nil
		}
	}
	return result
}

func configuredSecretUses(value Config) []SecretUse {
	uses := make([]SecretUse, 0)
	appendUse := func(path, plaintext, reference string) {
		if plaintext != "" || reference != "" {
			uses = append(uses, SecretUse{Path: path, Reference: reference})
		}
	}
	appendUse("ipc.auth_token", value.IPC.AuthToken, value.IPC.AuthTokenRef)
	for index, peer := range value.Integrations.WebSocketClients {
		appendUse(fmt.Sprintf("integrations.websocket_clients[%d].auth_token", index), peer.AuthToken, peer.AuthTokenRef)
	}
	for index, webhook := range value.Integrations.OutboundWebhooks {
		appendUse(fmt.Sprintf("integrations.outbound_webhooks[%d].signing_secret", index), webhook.SigningSecret, webhook.SigningSecretRef)
		for key, reference := range webhook.SecretHeaders {
			uses = append(uses, SecretUse{
				Path:      fmt.Sprintf("integrations.outbound_webhooks[%d].secret_headers[%q]", index, key),
				Reference: reference,
			})
		}
		for key, header := range webhook.Headers {
			if header != "" && sensitiveHeaderName(key) {
				uses = append(uses, SecretUse{
					Path: fmt.Sprintf("integrations.outbound_webhooks[%d].headers[%q]", index, key),
				})
			}
		}
	}
	return uses
}

func sensitiveHeaderName(value string) bool {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
	if value == "authorization" || value == "proxy-authorization" || value == "cookie" || value == "set-cookie" {
		return true
	}
	for _, marker := range []string{"token", "secret", "api-key", "apikey", "credential"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
