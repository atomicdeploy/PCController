//go:build windows

package shellintegration

import (
	"errors"
	"strings"
	"testing"
)

type memoryAssociationStore struct {
	keys       map[string]bool
	values     map[string]map[string]string
	writes     []registrationValue
	deletions  []string
	failPath   string
	failDelete map[string]error
	failReads  bool
	failExists bool
}

func newMemoryAssociationStore() *memoryAssociationStore {
	return &memoryAssociationStore{
		keys:   make(map[string]bool),
		values: make(map[string]map[string]string),
	}
}

func (s *memoryAssociationStore) KeyExists(path string) (bool, error) {
	if s.failExists {
		return false, errors.New("injected exists failure")
	}
	return s.keys[path], nil
}

func (s *memoryAssociationStore) GetString(
	path,
	name string,
) (string, bool, error) {
	if s.failReads {
		return "", false, errors.New("injected read failure")
	}
	values, ok := s.values[path]
	if !ok {
		return "", false, nil
	}
	value, ok := values[name]
	return value, ok, nil
}

func (s *memoryAssociationStore) SetString(path, name, value string) error {
	if path == s.failPath {
		return errors.New("injected write failure")
	}
	s.keys[path] = true
	if s.values[path] == nil {
		s.values[path] = make(map[string]string)
	}
	s.values[path][name] = value
	s.writes = append(s.writes, registrationValue{path: path, name: name, value: value})
	return nil
}

func (s *memoryAssociationStore) DeleteTree(path string) error {
	s.deletions = append(s.deletions, path)
	if err := s.failDelete[path]; err != nil {
		return err
	}
	for key := range s.keys {
		if key == path || strings.HasPrefix(key, path+`\`) {
			delete(s.keys, key)
			delete(s.values, key)
		}
	}
	return nil
}

func TestRegisterWithStoreCreatesExactAssociation(t *testing.T) {
	executable := makeTestExecutable(t, "PCController.exe")
	store := newMemoryAssociationStore()

	if err := registerWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	command, err := RegistrationCommand(executable)
	if err != nil {
		t.Fatal(err)
	}
	assertStoredString(t, store, protocolRootPath, "", protocolDisplayName)
	assertStoredString(t, store, protocolRootPath, protocolURLValueName, "")
	assertStoredString(t, store, protocolIconPath, "", `"`+executable+`",0`)
	assertStoredString(t, store, protocolOpenPath, "", command)
	if len(store.writes) != 4 {
		t.Fatalf("write count = %d, want 4", len(store.writes))
	}
	if store.writes[len(store.writes)-1].path != protocolOpenPath {
		t.Fatalf("ownership command was not written last: %#v", store.writes)
	}
}

func TestRegisterWithStoreIsIdempotentForSameOwner(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	if err := registerWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	store.writes = nil
	if err := registerWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	if len(store.writes) != 4 {
		t.Fatalf("refresh write count = %d, want 4", len(store.writes))
	}
}

func TestRegisterWithStorePreservesDifferentOwner(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	foreign := `"C:\Other\PCControllerDesktop.exe" /uri "%1"`
	store.keys[protocolRootPath] = true
	store.keys[protocolOpenPath] = true
	store.values[protocolOpenPath] = map[string]string{"": foreign}

	err := registerWithStore(store, executable)
	if !errors.Is(err, ErrRegistrationOwned) {
		t.Fatalf("error = %v, want ErrRegistrationOwned", err)
	}
	if len(store.writes) != 0 || len(store.deletions) != 0 {
		t.Fatalf("conflicting association was mutated: writes=%v deletes=%v", store.writes, store.deletions)
	}
	assertStoredString(t, store, protocolOpenPath, "", foreign)
}

func TestRegisterWithStoreTreatsOwnerlessExistingTreeAsConflict(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	store.keys[protocolRootPath] = true
	store.values[protocolRootPath] = map[string]string{"": "Another handler"}

	err := registerWithStore(store, executable)
	if !errors.Is(err, ErrRegistrationOwned) {
		t.Fatalf("error = %v, want ErrRegistrationOwned", err)
	}
	if len(store.writes) != 0 {
		t.Fatalf("ownerless existing tree was overwritten: %v", store.writes)
	}
}

func TestRegisterWithStoreCleansNewPartialTreeAfterFailure(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	store.failPath = protocolIconPath

	err := registerWithStore(store, executable)
	if err == nil || !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("error = %v", err)
	}
	if exists, _ := store.KeyExists(protocolRootPath); exists {
		t.Fatal("partial new protocol tree remains after write failure")
	}
	if len(store.deletions) != 1 || store.deletions[0] != protocolRootPath {
		t.Fatalf("cleanup deletions = %v", store.deletions)
	}
}

func TestUnregisterWithStoreDeletesOnlySameOwner(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	if err := registerWithStore(store, executable); err != nil {
		t.Fatal(err)
	}

	if err := unregisterWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	if exists, _ := store.KeyExists(protocolRootPath); exists {
		t.Fatal("owned protocol tree still exists")
	}
	if len(store.deletions) != 1 || store.deletions[0] != protocolRootPath {
		t.Fatalf("deletions = %v", store.deletions)
	}
}

func TestUnregisterWithStorePreservesDifferentOwner(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	foreign := `"C:\Moved\PCControllerDesktop.exe" /uri "%1"`
	store.keys[protocolRootPath] = true
	store.keys[protocolOpenPath] = true
	store.values[protocolOpenPath] = map[string]string{"": foreign}

	err := unregisterWithStore(store, executable)
	if !errors.Is(err, ErrRegistrationOwned) {
		t.Fatalf("error = %v, want ErrRegistrationOwned", err)
	}
	if len(store.deletions) != 0 {
		t.Fatalf("foreign association was deleted: %v", store.deletions)
	}
	assertStoredString(t, store, protocolOpenPath, "", foreign)
}

func TestUnregisterWithStoreIsIdempotentWhenAbsent(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	if err := unregisterWithStore(newMemoryAssociationStore(), executable); err != nil {
		t.Fatal(err)
	}
}

func TestIsRegisteredWithStoreIsReadOnlyAndOwnerSpecific(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()

	registered, err := isRegisteredWithStore(store, executable)
	if err != nil {
		t.Fatal(err)
	}
	if registered {
		t.Fatal("missing association reported as registered")
	}

	if err := registerWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	store.writes = nil
	registered, err = isRegisteredWithStore(store, executable)
	if err != nil {
		t.Fatal(err)
	}
	if !registered {
		t.Fatal("owned association was not reported")
	}
	if len(store.writes) != 0 || len(store.deletions) != 0 {
		t.Fatalf("read-only probe mutated the store: writes=%v deletes=%v", store.writes, store.deletions)
	}

	store.values[protocolOpenPath][""] = `"C:\Other\PCControllerDesktop.exe" /uri "%1"`
	registered, err = isRegisteredWithStore(store, executable)
	if err != nil {
		t.Fatal(err)
	}
	if registered {
		t.Fatal("foreign association reported as owned")
	}
}

func TestRegistryHelpersRejectNilOrSurfaceStoreErrors(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	if err := registerWithStore(nil, executable); err == nil {
		t.Fatal("nil register store was accepted")
	}
	if err := unregisterWithStore(nil, executable); err == nil {
		t.Fatal("nil unregister store was accepted")
	}
	if _, err := isRegisteredWithStore(nil, executable); err == nil {
		t.Fatal("nil registration probe store was accepted")
	}

	store := newMemoryAssociationStore()
	store.failExists = true
	if err := registerWithStore(store, executable); err == nil {
		t.Fatal("KeyExists error was ignored")
	}
}

func assertStoredString(
	t *testing.T,
	store *memoryAssociationStore,
	path,
	name,
	want string,
) {
	t.Helper()
	got, exists, err := store.GetString(path, name)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || got != want {
		t.Fatalf(
			"registry[%s][%q] = (%q, %t), want (%q, true)",
			path,
			name,
			got,
			exists,
			want,
		)
	}
}

var _ associationStore = (*memoryAssociationStore)(nil)
