package artifacts

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/programmer"
)

const (
	metadataSchema            = 1
	maximumDescriptorMetadata = 16
	maximumMetadataKeyBytes   = 64
	maximumMetadataValueBytes = 256
)

var descriptorMetadataKey = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)

type Store struct {
	root     string
	blobs    string
	metadata string
	mu       sync.Mutex
}

type storedMetadata struct {
	Schema     int        `json:"schema"`
	Descriptor Descriptor `json:"artifact"`
	Blob       string     `json:"blob"`
}

type currentState struct {
	Schema int             `json:"schema"`
	Kinds  map[Kind]string `json:"kinds"`
}

func NewStore(root string) (*Store, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." || !filepath.IsAbs(root) {
		return nil, errors.New("artifact store root must be an absolute path")
	}
	store := &Store{
		root: root, blobs: filepath.Join(root, "blobs", "sha256"),
		metadata: filepath.Join(root, "metadata"),
	}
	for _, directory := range []string{store.root, store.blobs, store.metadata} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create artifact store %q: %w", directory, err)
		}
	}
	return store, nil
}

func (store *Store) Root() string { return store.root }

func (store *Store) PutFile(path string, options PutOptions) (Descriptor, error) {
	file, err := os.Open(path)
	if err != nil {
		return Descriptor{}, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	if strings.TrimSpace(options.Name) == "" {
		options.Name = filepath.Base(path)
	}
	return store.Put(file, options)
}

func (store *Store) Put(input io.Reader, options PutOptions) (Descriptor, error) {
	if input == nil {
		return Descriptor{}, errors.New("artifact content is required")
	}
	if !ValidKind(options.Kind) {
		return Descriptor{}, fmt.Errorf("unsupported artifact kind %q", options.Kind)
	}
	name, err := safeArtifactName(options.Name, options.Kind)
	if err != nil {
		return Descriptor{}, err
	}
	limit := maxBytes(options.Kind)
	temporary, err := os.CreateTemp(store.root, ".artifact-*.upload")
	if err != nil {
		return Descriptor{}, fmt.Errorf("create artifact staging file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return Descriptor{}, err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(input, limit+1))
	if copyErr != nil {
		_ = temporary.Close()
		return Descriptor{}, fmt.Errorf("store artifact content: %w", copyErr)
	}
	if written > limit {
		_ = temporary.Close()
		return Descriptor{}, fmt.Errorf("%s artifact exceeds %d-byte limit", options.Kind, limit)
	}
	if written == 0 {
		_ = temporary.Close()
		return Descriptor{}, errors.New("artifact is empty")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return Descriptor{}, err
	}
	if err := temporary.Close(); err != nil {
		return Descriptor{}, err
	}
	if options.ExpectedBytes > 0 && options.ExpectedBytes != written {
		return Descriptor{}, fmt.Errorf("artifact size mismatch: expected %d, received %d", options.ExpectedBytes, written)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if strings.TrimSpace(options.ExpectedSHA256) != "" {
		expected, hashErr := normalizeSHA256(options.ExpectedSHA256)
		if hashErr != nil {
			return Descriptor{}, hashErr
		}
		if digest != expected {
			return Descriptor{}, fmt.Errorf("artifact SHA-256 mismatch: expected %s, received %s", expected, digest)
		}
	}
	if err := validateArtifactFile(temporaryPath, options.Kind); err != nil {
		return Descriptor{}, err
	}
	if options.Kind == KindHostExecutable {
		identity, inspectErr := InspectHostExecutable(temporaryPath)
		if inspectErr != nil {
			return Descriptor{}, inspectErr
		}
		detected := identity.Platform()
		if declared := strings.TrimSpace(options.Platform); declared != "" && declared != detected {
			return Descriptor{}, fmt.Errorf(
				"host artifact platform %q does not match its %s header %q",
				declared, identity.Format, detected,
			)
		}
		options.Platform = detected
	}
	metadata, err := validateDescriptorMetadata(options.Metadata)
	if err != nil {
		return Descriptor{}, err
	}

	destination := filepath.Join(store.blobs, digest[:2], digest)
	if err := publishImmutable(temporaryPath, destination, digest, written); err != nil {
		return Descriptor{}, err
	}
	descriptor := Descriptor{
		Kind: options.Kind, Name: name, SHA256: digest, Bytes: written,
		CreatedAt: time.Now().UTC(), Source: normalizedSource(options.Source),
		MediaType: mediaType(options.Kind), BuildHash: strings.ToUpper(strings.TrimSpace(options.BuildHash)),
		BuildTimestamp: strings.TrimSpace(options.BuildTimestamp), PackedTimestamp: options.PackedTimestamp,
		Platform: strings.TrimSpace(options.Platform), Embedded: options.Embedded,
		Current: options.Current, VerifiedReadback: options.VerifiedReadback,
		Metadata: metadata, LocalPath: destination,
	}
	if descriptor.Kind == KindHostExecutable && descriptor.Platform == "" {
		descriptor.Platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	if err := store.writeMetadata(descriptor, destination); err != nil {
		return Descriptor{}, err
	}
	if descriptor.Current {
		if err := store.SetCurrent(descriptor.Kind, descriptor.SHA256); err != nil {
			return Descriptor{}, err
		}
	}
	return publicDescriptor(descriptor), nil
}

func validateDescriptorMetadata(values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > maximumDescriptorMetadata {
		return nil, fmt.Errorf(
			"artifact metadata has %d entries; maximum is %d",
			len(values), maximumDescriptorMetadata,
		)
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		lower := strings.ToLower(key)
		if len(key) == 0 || len(key) > maximumMetadataKeyBytes ||
			!descriptorMetadataKey.MatchString(key) {
			return nil, fmt.Errorf("artifact metadata key %q is invalid", key)
		}
		for _, forbidden := range []string{
			"authorization", "cookie", "credential", "password", "secret", "token",
		} {
			if strings.Contains(lower, forbidden) {
				return nil, fmt.Errorf("artifact metadata key %q may contain credentials", key)
			}
		}
		if len(value) == 0 || len(value) > maximumMetadataValueBytes ||
			strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("artifact metadata value for %q is invalid", key)
		}
		result[key] = value
	}
	return result, nil
}

func (store *Store) Get(kind Kind, digest string) (Descriptor, error) {
	kind, err := canonicalStoreKind(kind)
	if err != nil {
		return Descriptor{}, err
	}
	normalized, err := normalizeSHA256(digest)
	if err != nil {
		return Descriptor{}, err
	}
	metadataPath, err := metadataRelativePath(kind, normalized)
	if err != nil {
		return Descriptor{}, err
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return Descriptor{}, err
	}
	defer root.Close()
	content, err := root.ReadFile(metadataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Descriptor{}, os.ErrNotExist
		}
		return Descriptor{}, err
	}
	var metadata storedMetadata
	if err := strictJSON(content, &metadata); err != nil {
		return Descriptor{}, fmt.Errorf("decode artifact metadata: %w", err)
	}
	if metadata.Schema != metadataSchema || metadata.Descriptor.Kind != kind || metadata.Descriptor.SHA256 != normalized {
		return Descriptor{}, errors.New("artifact metadata identity mismatch")
	}
	blob, err := store.resolveBlob(metadata.Blob)
	if err != nil {
		return Descriptor{}, err
	}
	if err := verifyRegularFile(blob, normalized, metadata.Descriptor.Bytes); err != nil {
		return Descriptor{}, err
	}
	metadata.Descriptor.LocalPath = blob
	metadata.Descriptor.Current = store.isCurrent(kind, normalized)
	return metadata.Descriptor, nil
}

func (store *Store) Open(kind Kind, digest string) (Descriptor, *os.File, error) {
	descriptor, err := store.Get(kind, digest)
	if err != nil {
		return Descriptor{}, nil, err
	}
	file, err := os.Open(descriptor.LocalPath)
	if err != nil {
		return Descriptor{}, nil, err
	}
	return publicDescriptor(descriptor), file, nil
}

func (store *Store) List(kind *Kind) ([]Descriptor, error) {
	kinds := supportedKinds
	if kind != nil {
		canonical, err := canonicalStoreKind(*kind)
		if err != nil {
			return nil, err
		}
		kinds = []Kind{canonical}
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var result []Descriptor
	for _, candidate := range kinds {
		directory, pathErr := metadataRelativeDirectory(candidate)
		if pathErr != nil {
			return nil, pathErr
		}
		directoryFile, err := root.Open(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entries, err := directoryFile.ReadDir(-1)
		_ = directoryFile.Close()
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			descriptor, getErr := store.Get(candidate, strings.TrimSuffix(entry.Name(), ".json"))
			if getErr != nil {
				return nil, getErr
			}
			result = append(result, publicDescriptor(descriptor))
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].SHA256 < result[right].SHA256
		}
		return result[left].CreatedAt.After(result[right].CreatedAt)
	})
	return result, nil
}

func (store *Store) SetCurrent(kind Kind, digest string) error {
	if _, err := store.Get(kind, digest); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state, _ := store.loadCurrentLocked()
	if state.Kinds == nil {
		state.Kinds = make(map[Kind]string)
	}
	state.Schema = metadataSchema
	state.Kinds[kind] = digest
	return store.writeJSONAtomic("current.json", state)
}

func (store *Store) Current(kind Kind) (*Descriptor, error) {
	store.mu.Lock()
	state, err := store.loadCurrentLocked()
	store.mu.Unlock()
	if err != nil {
		return nil, err
	}
	digest := state.Kinds[kind]
	if digest == "" {
		return nil, nil
	}
	descriptor, err := store.Get(kind, digest)
	if err != nil {
		return nil, err
	}
	value := publicDescriptor(descriptor)
	return &value, nil
}

func (store *Store) writeMetadata(descriptor Descriptor, blob string) error {
	kind, err := canonicalStoreKind(descriptor.Kind)
	if err != nil {
		return err
	}
	descriptor.Kind = kind
	directory, err := metadataRelativeDirectory(kind)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	relative, err := filepath.Rel(store.root, blob)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("artifact blob is outside the store")
	}
	path, err := metadataRelativePath(descriptor.Kind, descriptor.SHA256)
	if err != nil {
		return err
	}
	if existing, readErr := root.ReadFile(path); readErr == nil {
		var metadata storedMetadata
		if strictJSON(existing, &metadata) == nil && metadata.Descriptor.SHA256 == descriptor.SHA256 {
			// Refresh descriptive metadata without creating another content blob.
			descriptor.CreatedAt = metadata.Descriptor.CreatedAt
			merged := make(map[string]string, len(metadata.Descriptor.Metadata)+len(descriptor.Metadata))
			for key, value := range metadata.Descriptor.Metadata {
				merged[key] = value
			}
			for key, value := range descriptor.Metadata {
				merged[key] = value
			}
			var mergeErr error
			descriptor.Metadata, mergeErr = validateDescriptorMetadata(merged)
			if mergeErr != nil {
				return fmt.Errorf("merge artifact provenance metadata: %w", mergeErr)
			}
		}
	}
	descriptor.LocalPath = ""
	return store.writeJSONAtomic(path, storedMetadata{Schema: metadataSchema, Descriptor: descriptor, Blob: relative})
}

func canonicalStoreKind(kind Kind) (Kind, error) {
	switch kind {
	case KindFirmware:
		return KindFirmware, nil
	case KindEEPROM:
		return KindEEPROM, nil
	case KindFlashBackup:
		return KindFlashBackup, nil
	case KindHostExecutable:
		return KindHostExecutable, nil
	default:
		return "", fmt.Errorf("unsupported artifact kind %q", kind)
	}
}

func metadataRelativeDirectory(kind Kind) (string, error) {
	canonical, err := canonicalStoreKind(kind)
	if err != nil {
		return "", err
	}
	return filepath.Join("metadata", string(canonical)), nil
}

func metadataRelativePath(kind Kind, digest string) (string, error) {
	directory, err := metadataRelativeDirectory(kind)
	if err != nil {
		return "", err
	}
	normalized, err := normalizeSHA256(digest)
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, normalized+".json")
	if !filepath.IsLocal(path) {
		return "", errors.New("artifact metadata path escapes the store")
	}
	return path, nil
}

func (store *Store) resolveBlob(relative string) (string, error) {
	relative = filepath.Clean(strings.TrimSpace(relative))
	if relative == "" || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("artifact metadata contains an unsafe blob path")
	}
	path := filepath.Join(store.root, relative)
	rel, err := filepath.Rel(store.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("artifact blob escapes the store")
	}
	return path, nil
}

func (store *Store) isCurrent(kind Kind, digest string) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	state, err := store.loadCurrentLocked()
	return err == nil && state.Kinds[kind] == digest
}

func (store *Store) loadCurrentLocked() (currentState, error) {
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return currentState{}, err
	}
	defer root.Close()
	content, err := root.ReadFile("current.json")
	if errors.Is(err, os.ErrNotExist) {
		return currentState{Schema: metadataSchema, Kinds: make(map[Kind]string)}, nil
	}
	if err != nil {
		return currentState{}, err
	}
	var state currentState
	if err := strictJSON(content, &state); err != nil {
		return currentState{}, err
	}
	if state.Schema != metadataSchema {
		return currentState{}, fmt.Errorf("unsupported artifact current-state schema %d", state.Schema)
	}
	if state.Kinds == nil {
		state.Kinds = make(map[Kind]string)
	}
	return state, nil
}

func safeArtifactName(value string, kind Kind) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = string(kind)
	}
	if filepath.Base(value) != value || value == "." || value == ".." || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("artifact name must be one safe filename")
	}
	if len(value) > 180 {
		return "", errors.New("artifact name exceeds 180 characters")
	}
	return value, nil
}

func normalizedSource(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "manual"
	}
	if len(value) > 120 {
		return value[:120]
	}
	return value
}

func maxBytes(kind Kind) int64 {
	switch kind {
	case KindFirmware, KindFlashBackup:
		return 2 << 20
	case KindEEPROM:
		return 256 << 10
	case KindHostExecutable:
		return 256 << 20
	default:
		return 0
	}
}

func mediaType(kind Kind) string {
	switch kind {
	case KindFirmware, KindFlashBackup:
		return "application/vnd.intel-hex"
	case KindEEPROM:
		return "application/vnd.atmel.eeprom+intel-hex"
	default:
		return "application/octet-stream"
	}
}

func validateArtifactFile(path string, kind Kind) error {
	switch kind {
	case KindFirmware, KindFlashBackup, KindEEPROM:
		if _, err := programmer.LoadIntelHex(path); err != nil {
			return fmt.Errorf("%s artifact is not valid Intel HEX: %w", kind, err)
		}
	}
	return nil
}

func publishImmutable(source, destination, digest string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	if err := os.Link(source, destination); err != nil {
		if verifyErr := verifyRegularFile(destination, digest, size); verifyErr != nil {
			return fmt.Errorf("publish artifact blob: %w", err)
		}
	}
	return verifyRegularFile(destination, digest, size)
}

func verifyRegularFile(path, expectedHash string, expectedBytes int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != expectedBytes {
		return errors.New("artifact blob has the wrong type or size")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return errors.New("artifact blob SHA-256 mismatch")
	}
	return nil
}

func strictJSON(content []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains trailing data")
		}
		return err
	}
	return nil
}

func (store *Store) writeJSONAtomic(path string, value any) error {
	if !filepath.IsLocal(path) {
		return errors.New("artifact metadata path escapes the store")
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return err
	}
	defer root.Close()
	return writeRootJSONAtomic(root, path, value)
}

// writeJSONAtomic retains the package-level helper for trusted absolute state
// files while constraining the final operation to an opened parent directory.
func writeJSONAtomic(path string, value any) error {
	path = filepath.Clean(path)
	filename := filepath.Base(path)
	if filename == "." || filename == string(filepath.Separator) {
		return errors.New("JSON destination must name a file")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return writeRootJSONAtomic(root, filename, value)
}

func writeRootJSONAtomic(root *os.Root, path string, value any) error {
	directory := filepath.Dir(path)
	if err := root.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	temporaryPath, temporary, err := createRootTemp(root, directory)
	if err != nil {
		return err
	}
	defer root.Remove(temporaryPath)
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := root.Rename(temporaryPath, path); err != nil {
		_ = root.Remove(path)
		if retryErr := root.Rename(temporaryPath, path); retryErr != nil {
			return retryErr
		}
	}
	return nil
}

func createRootTemp(root *os.Root, directory string) (string, *os.File, error) {
	for range 100 {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", nil, err
		}
		path := filepath.Join(directory, ".metadata-"+hex.EncodeToString(nonce[:])+".tmp")
		file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return path, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, errors.New("cannot allocate artifact metadata staging file")
}

func publicDescriptor(value Descriptor) Descriptor {
	value.LocalPath = ""
	return value
}
