package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/productidentity"
)

const hostInstanceRecordSchema = 1

var errHostInstanceOwned = errors.New("another per-user controller host is starting or running")

type hostInstancePaths struct {
	LockName   string
	LockPath   string
	RecordPath string
}

type hostInstanceIdentity struct {
	ID        string
	Token     string
	PID       int
	Surface   string
	StartedAt time.Time
}

type hostInstanceRecord struct {
	Schema          int       `json:"schema"`
	InstanceID      string    `json:"instance_id"`
	PID             int       `json:"pid"`
	Executable      string    `json:"executable"`
	Surface         string    `json:"surface"`
	Listen          string    `json:"listen"`
	WebSocketPath   string    `json:"websocket_path"`
	SocketIOPath    string    `json:"socket_io_path"`
	DelegationToken string    `json:"delegation_token"`
	StartedAt       time.Time `json:"started_at"`
}

type platformHostInstanceLock interface {
	Close() error
}

type hostInstanceClaim struct {
	paths      hostInstancePaths
	identity   hostInstanceIdentity
	startedAt  time.Time
	lock       platformHostInstanceLock
	closeOnce  sync.Once
	closeError error
}

func defaultHostInstancePaths() (hostInstancePaths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return hostInstancePaths{}, fmt.Errorf("locate per-user host state: %w", err)
	}
	directory := filepath.Join(base, productidentity.ConfigDirectory)
	userKey, err := platformHostInstanceUserKey()
	if err != nil {
		return hostInstancePaths{}, fmt.Errorf("resolve per-user host identity: %w", err)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(userKey)))
	return hostInstancePaths{
		LockName: productidentity.StableAppID + ".Host." + hex.EncodeToString(sum[:8]),
		LockPath: filepath.Join(directory, "host-instance.lock"),
		RecordPath: filepath.Join(
			directory,
			"host-instance.json",
		),
	}, nil
}

func claimHostInstance(paths hostInstancePaths, surface string) (*hostInstanceClaim, error) {
	if strings.TrimSpace(paths.LockName) == "" || strings.TrimSpace(paths.LockPath) == "" ||
		strings.TrimSpace(paths.RecordPath) == "" {
		return nil, errors.New("per-user host paths are incomplete")
	}
	if err := os.MkdirAll(filepath.Dir(paths.RecordPath), 0o700); err != nil {
		return nil, fmt.Errorf("create per-user host state directory: %w", err)
	}
	lock, acquired, err := platformTryHostInstanceLock(paths.LockName, paths.LockPath)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, errHostInstanceOwned
	}
	id, err := newHostInstanceID()
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	token, err := newHostInstanceToken()
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	startedAt := time.Now().UTC()
	return &hostInstanceClaim{
		paths: paths,
		identity: hostInstanceIdentity{
			ID: id, Token: token, PID: os.Getpid(), Surface: normalizeHostSurface(surface),
			StartedAt: startedAt,
		},
		startedAt: startedAt,
		lock:      lock,
	}, nil
}

func newHostInstanceID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate per-user host instance id: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func newHostInstanceToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate per-user host delegation token: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func normalizeHostSurface(surface string) string {
	switch strings.ToLower(strings.TrimSpace(surface)) {
	case "web", "tui", "shell", "exec", "batch", "monitor", "reset":
		return strings.ToLower(strings.TrimSpace(surface))
	default:
		return "host"
	}
}

func (claim *hostInstanceClaim) publish(listener net.Listener, endpoint primaryEndpointConfig) error {
	if claim == nil || claim.lock == nil {
		return errors.New("per-user host claim is unavailable")
	}
	if listener == nil {
		return errors.New("per-user host listener is unavailable")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve host executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve absolute host executable: %w", err)
	}
	listen, err := localHostDialAddress(listener.Addr().String())
	if err != nil {
		return err
	}
	record := hostInstanceRecord{
		Schema:          hostInstanceRecordSchema,
		InstanceID:      claim.identity.ID,
		PID:             claim.identity.PID,
		Executable:      executable,
		Surface:         claim.identity.Surface,
		Listen:          listen,
		WebSocketPath:   endpoint.WebSocketPath,
		SocketIOPath:    endpoint.SocketIOPath,
		DelegationToken: claim.identity.Token,
		StartedAt:       claim.startedAt,
	}
	if err := validateHostInstanceRecord(record); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode per-user host record: %w", err)
	}
	encoded = append(encoded, '\n')
	temporary := claim.paths.RecordPath + "." + claim.identity.ID + ".tmp"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return fmt.Errorf("write per-user host record: %w", err)
	}
	if err := os.Rename(temporary, claim.paths.RecordPath); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish per-user host record: %w", err)
	}
	return nil
}

func (claim *hostInstanceClaim) Close() error {
	if claim == nil {
		return nil
	}
	claim.closeOnce.Do(func() {
		record, err := readHostInstanceRecord(claim.paths.RecordPath)
		if err == nil && record.InstanceID == claim.identity.ID {
			if removeErr := os.Remove(claim.paths.RecordPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				claim.closeError = errors.Join(claim.closeError, removeErr)
			}
		}
		if claim.lock != nil {
			claim.closeError = errors.Join(claim.closeError, claim.lock.Close())
		}
	})
	return claim.closeError
}

func resolveHostInstance(ctx context.Context, paths hostInstancePaths) (hostInstanceRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		record, err := readHostInstanceRecord(paths.RecordPath)
		if err == nil {
			if verifyErr := verifyHostInstanceRecord(ctx, record); verifyErr == nil {
				return record, nil
			} else {
				lastErr = verifyErr
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return hostInstanceRecord{}, fmt.Errorf("resolve running per-user host: %w", errors.Join(ctx.Err(), lastErr))
			}
			return hostInstanceRecord{}, fmt.Errorf("resolve running per-user host: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func readHostInstanceRecord(path string) (hostInstanceRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return hostInstanceRecord{}, err
	}
	var record hostInstanceRecord
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return hostInstanceRecord{}, fmt.Errorf("decode per-user host record: %w", err)
	}
	if err := validateHostInstanceRecord(record); err != nil {
		return hostInstanceRecord{}, err
	}
	return record, nil
}

func validateHostInstanceRecord(record hostInstanceRecord) error {
	if record.Schema != hostInstanceRecordSchema {
		return fmt.Errorf("per-user host record schema %d is unsupported", record.Schema)
	}
	decodedID, err := hex.DecodeString(record.InstanceID)
	if err != nil || len(decodedID) != 16 {
		return errors.New("per-user host record has an invalid instance id")
	}
	decodedToken, err := hex.DecodeString(record.DelegationToken)
	if err != nil || len(decodedToken) != 32 {
		return errors.New("per-user host record has an invalid delegation token")
	}
	if record.PID <= 0 || strings.TrimSpace(record.Executable) == "" || !filepath.IsAbs(record.Executable) {
		return errors.New("per-user host record has an invalid process identity")
	}
	if normalizeHostSurface(record.Surface) != record.Surface {
		return errors.New("per-user host record has an invalid surface")
	}
	if _, err := localHostDialAddress(record.Listen); err != nil {
		return err
	}
	if !strings.HasPrefix(record.WebSocketPath, "/") || !strings.HasPrefix(record.SocketIOPath, "/") {
		return errors.New("per-user host record has invalid IPC paths")
	}
	if record.StartedAt.IsZero() {
		return errors.New("per-user host record has no start time")
	}
	return nil
}

func verifyHostInstanceRecord(parent context.Context, record hostInstanceRecord) error {
	if err := validateHostInstanceRecord(record); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 350*time.Millisecond)
	defer cancel()
	var result struct {
		OK         bool   `json:"ok"`
		InstanceID string `json:"instance_id"`
		PID        int    `json:"process_id"`
		Surface    string `json:"surface"`
	}
	if err := callPrimaryAtAuthenticated(
		ctx,
		record.Listen,
		record.DelegationToken,
		"controller.ping",
		map[string]any{},
		&result,
	); err != nil {
		return err
	}
	if !result.OK || result.InstanceID != record.InstanceID || result.PID != record.PID ||
		normalizeHostSurface(result.Surface) != record.Surface {
		return errors.New("per-user host record does not match the live controller host")
	}
	return nil
}

func localHostDialAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return "", fmt.Errorf("validate per-user host endpoint: %w", err)
	}
	if strings.TrimSpace(port) == "" || port == "0" {
		return "", errors.New("per-user host endpoint must have a bound TCP port")
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if !localHostAddress(host) {
		return "", errors.New("per-user host endpoint is not assigned to this computer")
	}
	return net.JoinHostPort(host, port), nil
}

func localHostAddress(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		value, _, splitErr := net.ParseCIDR(address.String())
		if splitErr == nil && value.Equal(ip) {
			return true
		}
	}
	return false
}

func recordPrimaryEndpoint(record hostInstanceRecord) primaryEndpointConfig {
	return primaryEndpointConfig{
		Listen: record.Listen, WebSocketPath: record.WebSocketPath,
		SocketIOPath: record.SocketIOPath, AuthToken: record.DelegationToken,
	}
}

func claimOrResolveHostInstance(
	ctx context.Context,
	surface string,
) (*hostInstanceClaim, *hostInstanceRecord, error) {
	paths, err := defaultHostInstancePaths()
	if err != nil {
		return nil, nil, err
	}
	claim, err := claimHostInstance(paths, surface)
	if err == nil {
		return claim, nil, nil
	}
	if !errors.Is(err, errHostInstanceOwned) {
		return nil, nil, err
	}
	record, err := resolveHostInstance(ctx, paths)
	if err != nil {
		return nil, nil, err
	}
	endpoint := recordPrimaryEndpoint(record)
	primaryEndpoint.Store(endpoint)
	return nil, &record, nil
}
