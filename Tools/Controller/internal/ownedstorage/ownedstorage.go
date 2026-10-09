// Package ownedstorage defines the durable ownership marker used by host data
// roots that may later be recursively purged.
package ownedstorage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pccontroller.local/controller/internal/pathguard"
	"pccontroller.local/controller/internal/productidentity"
)

const (
	MarkerName   = ".pccontroller-data-owner.json"
	markerFormat = "pccontroller-host-data-owner/v1"
)

var ErrNotOwned = errors.New("host data root is not owned by this user and product")

type marker struct {
	Format       string    `json:"format"`
	ProductAppID string    `json:"product_app_id"`
	OwnerID      string    `json:"owner_id"`
	DataRoot     string    `json:"data_root"`
	CreatedAt    time.Time `json:"created_at"`
}

// Ensure creates a marker only for a new or empty directory. An arbitrary
// non-empty override is never silently adopted as recursively deletable data.
func Ensure(root string) error {
	owner, err := CurrentOwnerID()
	if err != nil {
		return err
	}
	return EnsureFor(root, owner)
}

// EnsureFor supports installer services with an injected owner identity while
// keeping production ownership resolution centralized in CurrentOwnerID.
func EnsureFor(root, owner string) error {
	if strings.TrimSpace(owner) == "" {
		return errors.New("host data owner identity is unavailable")
	}
	resolved, err := pathguard.CleanAbsolute(root)
	if err != nil {
		return err
	}
	root = resolved
	if err := pathguard.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := VerifyFor(root, owner); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("%w: refusing to adopt non-empty unmarked root %s", ErrNotOwned, root)
	}
	value := marker{
		Format: markerFormat, ProductAppID: productidentity.StableAppID,
		OwnerID: owner, DataRoot: root, CreatedAt: time.Now().UTC(),
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	temporary, err := os.CreateTemp(root, ".data-owner-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	markerPath := filepath.Join(root, MarkerName)
	if err := os.Rename(temporaryPath, markerPath); err != nil {
		return err
	}
	cleanup = false
	if err := pathguard.ValidateComponents(markerPath, false); err != nil {
		return err
	}
	return VerifyFor(root, owner)
}

func Verify(root string) error {
	owner, err := CurrentOwnerID()
	if err != nil {
		return err
	}
	return VerifyFor(root, owner)
}

// VerifyFor is exported for installer services whose test owner is injected.
func VerifyFor(root, owner string) error {
	root, err := pathguard.CleanAbsolute(root)
	if err != nil {
		return err
	}
	if err := pathguard.ValidateComponents(root, false); err != nil {
		return err
	}
	path := filepath.Join(root, MarkerName)
	if err := pathguard.ValidateComponents(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.Join(ErrNotOwned, os.ErrNotExist)
		}
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.Join(ErrNotOwned, os.ErrNotExist)
		}
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var value marker
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("%w: invalid marker: %v", ErrNotOwned, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: marker contains trailing data", ErrNotOwned)
	}
	if value.Format != markerFormat || value.ProductAppID != productidentity.StableAppID ||
		strings.TrimSpace(value.OwnerID) == "" || value.OwnerID != owner || !samePath(value.DataRoot, root) {
		return ErrNotOwned
	}
	return nil
}

func samePath(left, right string) bool {
	left, leftErr := pathguard.CleanAbsolute(left)
	right, rightErr := pathguard.CleanAbsolute(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(left, right)
}
