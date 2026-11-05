//go:build windows

package shellintegration

import (
	"errors"
	"strings"
	"testing"
	"unsafe"
)

func TestRegisterShellReportsJumpListPartialFailure(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	jumpFailure := errors.New("injected Jump List policy failure")
	err := registerShellWithStore(
		store,
		executable,
		func(string) error { return jumpFailure },
	)
	if !IsPartial(err) || !errors.Is(err, jumpFailure) {
		t.Fatalf("partial failure = %v", err)
	}
	owned, probeErr := isRegisteredWithStore(store, executable)
	if probeErr != nil || !owned {
		t.Fatalf("URI phase was not retained: owned=%t err=%v", owned, probeErr)
	}
}

func TestUnregisterShellAttemptsEveryOwnedCleanupPhase(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	if err := registerWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	store.keys[appUserModelRegistryPath] = true
	store.values[appUserModelRegistryPath] = map[string]string{
		appUserModelActivator: legacyToastActivatorID,
	}
	store.keys[legacyToastActivatorPath] = true
	store.keys[legacyToastServerPath] = true
	store.values[legacyToastServerPath] = map[string]string{"": executable}

	var jumpCalls int
	if err := unregisterShellWithStore(store, executable, func() error {
		jumpCalls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if jumpCalls != 1 {
		t.Fatalf("Jump List delete calls = %d, want 1", jumpCalls)
	}
	if store.keys[protocolRootPath] ||
		store.keys[appUserModelRegistryPath] ||
		store.keys[legacyToastActivatorPath] {
		t.Fatalf("owned shell metadata remains: %v", store.keys)
	}
}

func TestUnregisterShellNeverTouchesForeignOwner(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	store.keys[protocolRootPath] = true
	store.keys[protocolOpenPath] = true
	store.values[protocolOpenPath] = map[string]string{
		"": `"C:\Foreign\PCControllerDesktop.exe" /uri "%1"`,
	}

	var jumpCalls int
	err := unregisterShellWithStore(store, executable, func() error {
		jumpCalls++
		return nil
	})
	if !errors.Is(err, ErrRegistrationOwned) {
		t.Fatalf("error = %v, want ownership conflict", err)
	}
	if jumpCalls != 0 || len(store.deletions) != 0 {
		t.Fatalf(
			"foreign shell state was touched: jump=%d deletions=%v",
			jumpCalls,
			store.deletions,
		)
	}
}

type ownershipTransferStore struct {
	*memoryAssociationStore
	replacement string
	ownerReads  int
}

func (store *ownershipTransferStore) GetString(
	path,
	name string,
) (string, bool, error) {
	value, exists, err := store.memoryAssociationStore.GetString(path, name)
	if err == nil && path == protocolOpenPath && name == "" {
		store.ownerReads++
		if store.ownerReads == 1 {
			store.memoryAssociationStore.values[protocolOpenPath][""] =
				store.replacement
		}
	}
	return value, exists, err
}

func TestUnregisterShellRechecksOwnerBeforeSharedCleanup(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	base := newMemoryAssociationStore()
	if err := registerWithStore(base, executable); err != nil {
		t.Fatal(err)
	}
	base.keys[appUserModelRegistryPath] = true
	base.values[appUserModelRegistryPath] = map[string]string{
		appUserModelActivator: legacyToastActivatorID,
	}
	base.keys[legacyToastActivatorPath] = true
	base.keys[legacyToastServerPath] = true
	base.values[legacyToastServerPath] = map[string]string{"": executable}

	foreign := `"C:\NewOwner\PCControllerDesktop.exe" /uri "%1"`
	store := &ownershipTransferStore{
		memoryAssociationStore: base,
		replacement:            foreign,
	}
	var jumpCalls int
	err := unregisterShellWithStore(store, executable, func() error {
		jumpCalls++
		return nil
	})
	if !errors.Is(err, ErrRegistrationOwned) {
		t.Fatalf("ownership-transfer error = %v, want ownership conflict", err)
	}
	if store.ownerReads != 2 {
		t.Fatalf("protocol owner reads = %d, want initial probe plus recheck", store.ownerReads)
	}
	if jumpCalls != 0 {
		t.Fatalf("Jump List cleanup ran %d times after ownership transfer", jumpCalls)
	}
	if len(base.deletions) != 0 {
		t.Fatalf("shared registry state was deleted after ownership transfer: %v", base.deletions)
	}
	assertStoredString(t, base, protocolOpenPath, "", foreign)
	for _, path := range []string{
		appUserModelRegistryPath,
		legacyToastActivatorPath,
		legacyToastServerPath,
	} {
		if !base.keys[path] {
			t.Errorf("shared metadata %s was removed after ownership transfer", path)
		}
	}
}

func TestUnregisterShellReportsPartialButStillRemovesURI(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	if err := registerWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	jumpFailure := errors.New("injected delete failure")
	err := unregisterShellWithStore(
		store,
		executable,
		func() error { return jumpFailure },
	)
	if !IsPartial(err) || !errors.Is(err, jumpFailure) {
		t.Fatalf("partial failure = %v", err)
	}
	if store.keys[protocolRootPath] {
		t.Fatal("URI registration remained after independent Jump List failure")
	}
}

func TestUnregisterShellReportsTotalFailureWhenNothingCompleted(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	if err := registerWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	store.keys[appUserModelRegistryPath] = true
	store.values[appUserModelRegistryPath] = map[string]string{
		appUserModelActivator: legacyToastActivatorID,
	}
	store.keys[legacyToastActivatorPath] = true
	store.keys[legacyToastServerPath] = true
	store.values[legacyToastServerPath] = map[string]string{"": executable}

	jumpFailure := errors.New("injected Jump List failure")
	metadataFailure := errors.New("injected metadata failure")
	protocolFailure := errors.New("injected protocol failure")
	store.failDelete = map[string]error{
		legacyToastActivatorPath: metadataFailure,
		appUserModelRegistryPath: metadataFailure,
		protocolRootPath:         protocolFailure,
	}
	err := unregisterShellWithStore(
		store,
		executable,
		func() error { return jumpFailure },
	)
	if err == nil || IsPartial(err) {
		t.Fatalf("total failure = %v, want non-partial error", err)
	}
	for _, cause := range []error{
		jumpFailure,
		metadataFailure,
		protocolFailure,
	} {
		if !errors.Is(err, cause) {
			t.Errorf("total failure does not contain %v: %v", cause, err)
		}
	}
}

func TestCleanupLegacyMetadataDeletesOnlyOwnedActivator(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	store.keys[appUserModelRegistryPath] = true
	store.values[appUserModelRegistryPath] = map[string]string{
		appUserModelActivator: strings.ToLower(legacyToastActivatorID),
	}
	store.keys[legacyToastActivatorPath] = true
	store.keys[legacyToastServerPath] = true
	store.values[legacyToastServerPath] = map[string]string{
		"": `"` + executable + `"`,
	}

	if err := cleanupLegacyMetadataWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	if store.keys[appUserModelRegistryPath] ||
		store.keys[legacyToastActivatorPath] ||
		store.keys[legacyToastServerPath] {
		t.Fatalf("owned metadata remains: keys=%v", store.keys)
	}
}

func TestCleanupLegacyMetadataPreservesForeignActivatorAndAppID(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	store.keys[appUserModelRegistryPath] = true
	store.values[appUserModelRegistryPath] = map[string]string{
		appUserModelActivator: legacyToastActivatorID,
	}
	store.keys[legacyToastActivatorPath] = true
	store.keys[legacyToastServerPath] = true
	foreign := `C:\PortableElsewhere\PCControllerDesktop.exe`
	store.values[legacyToastServerPath] = map[string]string{"": foreign}

	if err := cleanupLegacyMetadataWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	if !store.keys[appUserModelRegistryPath] ||
		!store.keys[legacyToastActivatorPath] ||
		!store.keys[legacyToastServerPath] {
		t.Fatalf("foreign metadata was removed: keys=%v", store.keys)
	}
	if len(store.deletions) != 0 {
		t.Fatalf("foreign metadata deletions = %v", store.deletions)
	}
}

func TestCleanupLegacyMetadataRemovesStaleMatchingAppIDWithoutActivator(t *testing.T) {
	executable := makeTestExecutable(t, "PCControllerDesktop.exe")
	store := newMemoryAssociationStore()
	store.keys[appUserModelRegistryPath] = true
	store.values[appUserModelRegistryPath] = map[string]string{
		appUserModelActivator: legacyToastActivatorID,
	}

	if err := cleanupLegacyMetadataWithStore(store, executable); err != nil {
		t.Fatal(err)
	}
	if store.keys[appUserModelRegistryPath] {
		t.Fatal("stale matching AppUserModelID metadata remains")
	}
}

func TestActivationOwnershipRequiresExactExecutableOnly(t *testing.T) {
	t.Parallel()

	executable := `C:\Program Files\PCControllerDesktop\PCControllerDesktop.exe`
	for _, owned := range []string{
		executable,
		`"` + executable + `"`,
		strings.ToUpper(executable),
	} {
		if !activationOwnedBy(owned, executable) {
			t.Errorf("owned activation command rejected: %q", owned)
		}
	}
	for _, foreign := range []string{
		`C:\Other\PCControllerDesktop.exe`,
		`"` + executable + `" /background`,
		executable + " /com-server",
	} {
		if activationOwnedBy(foreign, executable) {
			t.Errorf("foreign activation command accepted: %q", foreign)
		}
	}
}

func TestNativeJumpListLayoutsAndStableIdentifiers(t *testing.T) {
	t.Parallel()

	if unsafe.Sizeof(propertyKey{}) != 20 {
		t.Fatalf("PROPERTYKEY size = %d, want 20", unsafe.Sizeof(propertyKey{}))
	}
	wantVariantSize := uintptr(16)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		wantVariantSize = 24
	}
	if unsafe.Sizeof(propVariant{}) != wantVariantSize {
		t.Fatalf(
			"PROPVARIANT size = %d, want %d",
			unsafe.Sizeof(propVariant{}),
			wantVariantSize,
		)
	}
	if pkeyTitle.PropertyID != 2 ||
		pkeyTitle.FormatID.Data1 != 0xF29F85E0 {
		t.Fatalf("PKEY_Title changed: %+v", pkeyTitle)
	}
	if clsidDestinationList.Data1 != 0x77F10CF0 ||
		iidCustomDestinationList.Data1 != 0x6332DEBF ||
		clsidEnumerableObjectCollection.Data1 != 0x2D3468C1 ||
		iidObjectCollection.Data1 != 0x5632B1A4 ||
		iidObjectArray.Data1 != 0x92CA9DCD {
		t.Fatal("one or more Jump List COM identifiers changed")
	}
}
