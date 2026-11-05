//go:build windows

package comserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	classesRoot = `Software\Classes`

	imageFileMachineUnknown = 0x0000
	imageFileMachineI386    = 0x014c
	imageFileMachineARMNT   = 0x01c4
	imageFileMachineIA64    = 0x0200
	imageFileMachineAMD64   = 0x8664
	imageFileMachineARM64   = 0xaa64

	processorArchitectureIntel = 0
	processorArchitectureARM   = 5
	processorArchitectureIA64  = 6
	processorArchitectureAMD64 = 9
	processorArchitectureARM64 = 12
)

var (
	comRegistryKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	comRegistryAdvapi32 = windows.NewLazySystemDLL("advapi32.dll")

	comRegistryGetNativeSystemInfo = comRegistryKernel32.NewProc(
		"GetNativeSystemInfo",
	)
	comRegistryDeleteKeyEx = comRegistryAdvapi32.NewProc(
		"RegDeleteKeyExW",
	)
)

type registryValue struct {
	path  string
	name  string
	value string
}

type registryView struct {
	access uint32
	label  string
}

type nativeSystemInfo struct {
	processorArchitecture uint16
	reserved              uint16
	pageSize              uint32
	minimumAddress        uintptr
	maximumAddress        uintptr
	activeProcessorMask   uintptr
	numberOfProcessors    uint32
	processorType         uint32
	allocationGranularity uint32
	processorLevel        uint16
	processorRevision     uint16
}

type registryStringReader func(
	path string,
	name string,
	view uint32,
) (string, bool, error)

type localServerCommandReader func(
	clsidRoot string,
	view uint32,
) (string, bool, error)

type classViewDeleter func(path string, view uint32) error

type registryTreeAPI interface {
	OpenKey(
		parent registry.Key,
		path string,
		access uint32,
	) (registry.Key, error)
	ReadSubKeyNames(key registry.Key) ([]string, error)
	CloseKey(key registry.Key) error
	DeleteKey(parent registry.Key, path string, view uint32) error
}

type windowsRegistryTreeAPI struct{}

// Register creates an opt-in, per-user COM registration below
// HKCU\Software\Classes. Local-server CLSID data is written to both registry
// views on 64-bit Windows so 32-bit VBA/AutoHotkey and 64-bit clients can
// activate the same out-of-process executable. It never writes to HKLM.
func Register(executable string) error {
	command, err := RegistrationCommand(executable)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(strings.TrimSpace(executable))
	if err != nil {
		return fmt.Errorf("comserver: resolve executable path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("comserver: inspect executable: %w", err)
	}
	if info.IsDir() {
		return errors.New("comserver: executable path is a directory")
	}

	clsidRoot := classesRoot + `\CLSID\` + CLSID
	sharedRoots := sharedRegistrationRoots()
	sharedFresh := make(map[string]bool, len(sharedRoots))
	anySharedExisting := false
	for _, path := range sharedRoots {
		exists, existsErr := registryKeyExistsView(path, 0)
		if existsErr != nil {
			return fmt.Errorf("comserver: inspect registry key %s: %w", path, existsErr)
		}
		if exists {
			if ownershipErr := validateSharedRegistrationRoot(
				path,
				readRegistryStringView,
			); ownershipErr != nil {
				return ownershipErr
			}
		}
		sharedFresh[path] = !exists
		anySharedExisting = anySharedExisting || exists
	}
	views, err := comRegistryViews()
	if err != nil {
		return err
	}
	viewFresh := make(map[uint32]bool, len(views))
	anyClassExisting := false
	for _, view := range views {
		rootExists, existsErr := registryKeyExistsView(clsidRoot, view.access)
		if existsErr != nil {
			return fmt.Errorf(
				"comserver: inspect %s CLSID registration: %w",
				view.label,
				existsErr,
			)
		}
		actualCommand, commandExists, commandErr :=
			registeredLocalServerCommand(clsidRoot, view.access)
		if commandErr != nil {
			return commandErr
		}
		if rootExists &&
			(!commandExists || !sameRegistrationCommand(actualCommand, command)) {
			return fmt.Errorf(
				"%w in the %s registry view: registered command is %q",
				ErrRegistrationOwnedByAnotherExecutable,
				view.label,
				actualCommand,
			)
		}
		viewFresh[view.access] = !rootExists
		anyClassExisting = anyClassExisting || rootExists
	}
	if anySharedExisting && !anyClassExisting {
		return fmt.Errorf(
			"%w: ProgID keys exist without an owned LocalServer32 registration",
			ErrRegistrationOwnedByAnotherExecutable,
		)
	}

	sharedValues := sharedRegistrationValues()
	classValues := []registryValue{
		{path: clsidRoot, value: displayName},
		{path: clsidRoot + `\LocalServer32`, value: command},
		{path: clsidRoot + `\ProgID`, value: VersionedProgID},
		{path: clsidRoot + `\VersionIndependentProgID`, value: ProgID},
	}
	cleanupFresh := func(cause error) error {
		var classCleanup error
		for _, view := range views {
			if viewFresh[view.access] {
				classCleanup = errors.Join(
					classCleanup,
					deleteRegistryTreeView(clsidRoot, view.access),
				)
			}
		}
		var sharedCleanup error
		if classCleanup == nil {
			for _, path := range sharedRoots {
				if sharedFresh[path] {
					sharedCleanup = errors.Join(
						sharedCleanup,
						deleteRegistryTree(registry.CURRENT_USER, path),
					)
				}
			}
		}
		return errors.Join(cause, classCleanup, sharedCleanup)
	}

	for _, value := range sharedValues {
		if err := setRegistryStringView(value, 0); err != nil {
			return cleanupFresh(err)
		}
	}
	for _, view := range views {
		for _, value := range classValues {
			if err := setRegistryStringView(value, view.access); err != nil {
				return cleanupFresh(err)
			}
		}
		programmable, _, createErr := registry.CreateKey(
			registry.CURRENT_USER,
			clsidRoot+`\Programmable`,
			registry.WRITE|view.access,
		)
		if createErr != nil {
			return cleanupFresh(fmt.Errorf(
				"comserver: create %s Programmable registration: %w",
				view.label,
				createErr,
			))
		}
		if closeErr := programmable.Close(); closeErr != nil {
			return cleanupFresh(fmt.Errorf(
				"comserver: close %s Programmable registration: %w",
				view.label,
				closeErr,
			))
		}
	}
	return nil
}

// HasOwnedRegistration reports whether any exact-owned COM registration already
// targets executable. It is read-only and treats incomplete-but-owned state as
// present so transactional callers never roll back registration they did not
// create. A conflicting view or shared ProgID marker is an ownership error.
func HasOwnedRegistration(executable string) (bool, error) {
	views, err := comRegistryViews()
	if err != nil {
		return false, err
	}
	return hasOwnedRegistrationWithReaders(
		executable,
		views,
		registryKeyExistsView,
		registeredLocalServerCommand,
		readRegistryStringView,
	)
}

// HasOwnedServerRegistration reports whether any LocalServer32 registry view
// still points at executable. Unlike HasOwnedRegistration, shared ProgID
// aliases alone do not count. The self-uninstaller uses this distinction to
// avoid deleting an executable still referenced by one COM activation view.
func HasOwnedServerRegistration(executable string) (bool, error) {
	views, err := comRegistryViews()
	if err != nil {
		return false, err
	}
	return hasOwnedServerRegistrationWithReaders(
		executable,
		views,
		registryKeyExistsView,
		registeredLocalServerCommand,
		readRegistryStringView,
	)
}

func hasOwnedRegistrationWithReaders(
	executable string,
	views []registryView,
	keyExists registryKeyExistsReader,
	localServer localServerCommandReader,
	shared registryStringReader,
) (bool, error) {
	return scanOwnedRegistrationWithReaders(
		executable,
		views,
		keyExists,
		localServer,
		shared,
		true,
	)
}

func hasOwnedServerRegistrationWithReaders(
	executable string,
	views []registryView,
	keyExists registryKeyExistsReader,
	localServer localServerCommandReader,
	shared registryStringReader,
) (bool, error) {
	return scanOwnedRegistrationWithReaders(
		executable,
		views,
		keyExists,
		localServer,
		shared,
		false,
	)
}

func scanOwnedRegistrationWithReaders(
	executable string,
	views []registryView,
	keyExists registryKeyExistsReader,
	localServer localServerCommandReader,
	shared registryStringReader,
	countSharedOwnership bool,
) (bool, error) {
	if keyExists == nil || localServer == nil || shared == nil {
		return false, errors.New("comserver: nil owned-registration reader")
	}
	expected, err := RegistrationCommand(executable)
	if err != nil {
		return false, err
	}
	clsidRoot := classesRoot + `\CLSID\` + CLSID
	owned := false
	var joinedErr error
	for _, view := range views {
		rootExists, existsErr := keyExists(clsidRoot, view.access)
		if existsErr != nil {
			joinedErr = errors.Join(
				joinedErr,
				fmt.Errorf(
					"comserver: inspect %s CLSID registration: %w",
					view.label,
					existsErr,
				),
			)
			continue
		}
		if !rootExists {
			continue
		}
		actual, commandExists, commandErr :=
			localServer(clsidRoot, view.access)
		if commandErr != nil {
			joinedErr = errors.Join(joinedErr, commandErr)
			continue
		}
		if !commandExists || !sameRegistrationCommand(actual, expected) {
			joinedErr = errors.Join(
				joinedErr,
				fmt.Errorf(
					"%w in the %s registry view: registered command is %q",
					ErrRegistrationOwnedByAnotherExecutable,
					view.label,
					actual,
				),
			)
			continue
		}
		owned = true
	}
	for _, path := range sharedRegistrationRoots() {
		exists, existsErr := keyExists(path, 0)
		if existsErr != nil {
			joinedErr = errors.Join(
				joinedErr,
				fmt.Errorf(
					"comserver: inspect shared registration %s: %w",
					path,
					existsErr,
				),
			)
			continue
		}
		if !exists {
			continue
		}
		if sharedOwnershipErr := validateSharedRegistrationRoot(
			path,
			shared,
		); sharedOwnershipErr != nil {
			joinedErr = errors.Join(
				joinedErr,
				sharedOwnershipErr,
			)
			continue
		}
		if countSharedOwnership {
			owned = true
		}
	}
	return owned, joinedErr
}

// Unregister removes only the per-user COM registration owned by the current
// executable. A registration pointing at another RayanLamp installation is
// deliberately preserved.
func Unregister() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("comserver: locate current executable: %w", err)
	}
	return UnregisterExecutable(executable)
}

// UnregisterExecutable removes only the per-user COM registration owned by
// executable. It exists for the self-uninstaller, which may run from a
// detached temporary copy after stopping an installed RayanLamp process.
func UnregisterExecutable(executable string) error {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return errors.New("comserver: executable path is empty")
	}
	expected, err := RegistrationCommand(executable)
	if err != nil {
		return err
	}
	clsidRoot := classesRoot + `\CLSID\` + CLSID
	views, err := comRegistryViews()
	if err != nil {
		return err
	}
	ownedViews := make([]registryView, 0, len(views))
	for _, view := range views {
		rootExists, existsErr := registryKeyExistsView(clsidRoot, view.access)
		if existsErr != nil {
			return fmt.Errorf(
				"comserver: inspect %s CLSID registration: %w",
				view.label,
				existsErr,
			)
		}
		actual, commandExists, commandErr :=
			registeredLocalServerCommand(clsidRoot, view.access)
		if commandErr != nil {
			return commandErr
		}
		if !rootExists {
			continue
		}
		if !commandExists || !sameRegistrationCommand(actual, expected) {
			return fmt.Errorf(
				"%w in the %s registry view: registered command is %q",
				ErrRegistrationOwnedByAnotherExecutable,
				view.label,
				actual,
			)
		}
		ownedViews = append(ownedViews, view)
	}
	if len(ownedViews) == 0 {
		return nil
	}

	ownedSharedRoots := make(map[string]bool)
	for _, path := range sharedRegistrationRoots() {
		exists, existsErr := registryKeyExistsView(path, 0)
		if existsErr != nil {
			return fmt.Errorf(
				"comserver: inspect shared registration %s: %w",
				path,
				existsErr,
			)
		}
		if !exists {
			continue
		}
		if ownershipErr := validateSharedRegistrationRoot(
			path,
			readRegistryStringView,
		); ownershipErr != nil {
			return ownershipErr
		}
		ownedSharedRoots[path] = true
	}

	joined := deleteOwnedClassViews(
		clsidRoot,
		expected,
		ownedViews,
		registeredLocalServerCommand,
		deleteRegistryTreeView,
	)
	if joined != nil {
		// Keep the shared ProgID aliases while any owned CLSID view remains.
		// This leaves a retryable registration instead of deliberately
		// orphaning clients in the view that could not be removed.
		return joined
	}
	// Delete the versioned alias first, then the version-independent ProgID.
	// Re-read the ownership markers immediately before each destructive
	// operation so a concurrent installer cannot silently transfer ownership.
	for _, path := range []string{
		classesRoot + `\` + VersionedProgID,
		classesRoot + `\` + ProgID,
	} {
		if !ownedSharedRoots[path] {
			continue
		}
		exists, existsErr := registryKeyExistsView(path, 0)
		if existsErr != nil {
			joined = errors.Join(joined, fmt.Errorf(
				"recheck shared registration %s: %w",
				path,
				existsErr,
			))
			continue
		}
		if !exists {
			continue
		}
		if ownershipErr := validateSharedRegistrationRoot(
			path,
			readRegistryStringView,
		); ownershipErr != nil {
			joined = errors.Join(joined, ownershipErr)
			continue
		}
		if err := deleteRegistryTree(registry.CURRENT_USER, path); err != nil {
			joined = errors.Join(joined, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	return joined
}

func deleteOwnedClassViews(
	clsidRoot string,
	expectedCommand string,
	views []registryView,
	read localServerCommandReader,
	remove classViewDeleter,
) error {
	if read == nil {
		return errors.New("comserver: nil LocalServer32 reader")
	}
	if remove == nil {
		return errors.New("comserver: nil class-view deleter")
	}

	var joined error
	for _, view := range views {
		// Registry registration is not transactional. Re-read the ownership
		// marker at the destructive boundary instead of trusting the earlier
		// discovery pass. A missing or transferred marker stops further
		// deletion and always preserves the view that is no longer ours.
		actual, exists, err := read(clsidRoot, view.access)
		if err != nil {
			return errors.Join(joined, fmt.Errorf(
				"recheck %s LocalServer32 ownership: %w",
				view.label,
				err,
			))
		}
		if !exists || !sameRegistrationCommand(actual, expectedCommand) {
			return errors.Join(joined, fmt.Errorf(
				"%w in the %s registry view: registered command is %q",
				ErrRegistrationOwnedByAnotherExecutable,
				view.label,
				actual,
			))
		}
		if err := remove(clsidRoot, view.access); err != nil {
			joined = errors.Join(joined, fmt.Errorf(
				"remove %s CLSID registration: %w",
				view.label,
				err,
			))
		}
	}
	return joined
}

func sharedRegistrationRoots() []string {
	return []string{
		classesRoot + `\` + ProgID,
		classesRoot + `\` + VersionedProgID,
	}
}

func sharedRegistrationValues() []registryValue {
	return []registryValue{
		{path: classesRoot + `\` + ProgID, value: displayName},
		{path: classesRoot + `\` + ProgID + `\CLSID`, value: CLSID},
		{path: classesRoot + `\` + ProgID + `\CurVer`, value: VersionedProgID},
		{path: classesRoot + `\` + VersionedProgID, value: displayName},
		{path: classesRoot + `\` + VersionedProgID + `\CLSID`, value: CLSID},
	}
}

func sharedRegistrationOwnershipValues(root string) ([]registryValue, bool) {
	switch {
	case strings.EqualFold(root, classesRoot+`\`+ProgID):
		return []registryValue{
			{path: root + `\CLSID`, value: CLSID},
			{path: root + `\CurVer`, value: VersionedProgID},
		}, true
	case strings.EqualFold(root, classesRoot+`\`+VersionedProgID):
		return []registryValue{
			{path: root + `\CLSID`, value: CLSID},
		}, true
	default:
		return nil, false
	}
}

func validateSharedRegistrationRoot(
	root string,
	read registryStringReader,
) error {
	if read == nil {
		return errors.New("comserver: nil registry string reader")
	}
	expectedValues, known := sharedRegistrationOwnershipValues(root)
	if !known {
		return fmt.Errorf(
			"comserver: cannot validate unknown shared registration %s",
			root,
		)
	}
	for _, expected := range expectedValues {
		actual, exists, err := read(expected.path, expected.name, 0)
		if err != nil {
			return fmt.Errorf(
				"comserver: inspect shared registration %s: %w",
				expected.path,
				err,
			)
		}
		if !exists || !strings.EqualFold(
			strings.TrimSpace(actual),
			strings.TrimSpace(expected.value),
		) {
			return fmt.Errorf(
				"%w: shared registration %s contains %q, expected %q",
				ErrRegistrationOwnedByAnotherExecutable,
				expected.path,
				actual,
				expected.value,
			)
		}
	}
	return nil
}

func registeredLocalServerCommand(
	clsidRoot string,
	view uint32,
) (string, bool, error) {
	path := clsidRoot + `\LocalServer32`
	actual, exists, err := readRegistryStringView(path, "", view)
	if err != nil {
		return "", false, fmt.Errorf(
			"comserver: inspect LocalServer32 registration: %w",
			err,
		)
	}
	return actual, exists, nil
}

func readRegistryStringView(
	path string,
	name string,
	view uint32,
) (string, bool, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		path,
		registry.QUERY_VALUE|view,
	)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	actual, _, readErr := key.GetStringValue(name)
	closeErr := key.Close()
	if errors.Is(readErr, registry.ErrNotExist) {
		if closeErr != nil {
			return "", false, closeErr
		}
		return "", false, nil
	}
	if readErr != nil {
		return "", false, readErr
	}
	if closeErr != nil {
		return "", false, closeErr
	}
	return actual, true, nil
}

func registryKeyExistsView(path string, view uint32) (bool, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		path,
		registry.READ|view,
	)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := key.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func sameRegistrationCommand(actual, expected string) bool {
	return strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(expected))
}

func setRegistryStringView(value registryValue, view uint32) error {
	key, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		value.path,
		registry.WRITE|view,
	)
	if err != nil {
		return fmt.Errorf("comserver: create registry key %s: %w", value.path, err)
	}
	if err := key.SetStringValue(value.name, value.value); err != nil {
		_ = key.Close()
		return fmt.Errorf("comserver: write registry key %s: %w", value.path, err)
	}
	if err := key.Close(); err != nil {
		return fmt.Errorf("comserver: close registry key %s: %w", value.path, err)
	}
	return nil
}

func comRegistryViews() ([]registryView, error) {
	is64BitWindows, err := detect64BitWindows(
		strconv.IntSize,
		queryWow64NativeMachine,
		queryNativeProcessorArchitecture,
	)
	if err != nil {
		return nil, err
	}
	return registryViewsForProcess(is64BitWindows), nil
}

type nativeArchitectureQuery func() (uint16, error)

func detect64BitWindows(
	processBits int,
	wow64Process2 nativeArchitectureQuery,
	nativeSystemInfo nativeArchitectureQuery,
) (bool, error) {
	if processBits == 64 {
		return true, nil
	}
	if processBits != 32 {
		return false, fmt.Errorf(
			"comserver: unsupported process word size %d",
			processBits,
		)
	}

	var wow64Err error
	if wow64Process2 != nil {
		machine, err := wow64Process2()
		if err == nil {
			if is64, known := imageMachineIs64Bit(machine); known {
				return is64, nil
			}
			err = fmt.Errorf("unknown native image machine 0x%04X", machine)
		}
		wow64Err = fmt.Errorf("IsWow64Process2: %w", err)
	} else {
		wow64Err = errors.New("IsWow64Process2 query is unavailable")
	}

	var nativeErr error
	if nativeSystemInfo != nil {
		architecture, err := nativeSystemInfo()
		if err == nil {
			if is64, known := processorArchitectureIs64Bit(architecture); known {
				return is64, nil
			}
			err = fmt.Errorf(
				"unknown native processor architecture %d",
				architecture,
			)
		}
		nativeErr = fmt.Errorf("GetNativeSystemInfo: %w", err)
	} else {
		nativeErr = errors.New("GetNativeSystemInfo query is unavailable")
	}

	return false, fmt.Errorf(
		"comserver: detect native Windows architecture: %w",
		errors.Join(wow64Err, nativeErr),
	)
}

func queryWow64NativeMachine() (uint16, error) {
	var processMachine uint16
	var nativeMachine uint16
	if err := windows.IsWow64Process2(
		windows.CurrentProcess(),
		&processMachine,
		&nativeMachine,
	); err != nil {
		return imageFileMachineUnknown, err
	}
	return nativeMachine, nil
}

func queryNativeProcessorArchitecture() (uint16, error) {
	if err := comRegistryGetNativeSystemInfo.Find(); err != nil {
		return 0, err
	}
	var info nativeSystemInfo
	comRegistryGetNativeSystemInfo.Call(uintptr(unsafe.Pointer(&info)))
	return info.processorArchitecture, nil
}

func imageMachineIs64Bit(machine uint16) (is64, known bool) {
	switch machine {
	case imageFileMachineI386, imageFileMachineARMNT:
		return false, true
	case imageFileMachineIA64, imageFileMachineAMD64, imageFileMachineARM64:
		return true, true
	default:
		return false, false
	}
}

func processorArchitectureIs64Bit(
	architecture uint16,
) (is64, known bool) {
	switch architecture {
	case processorArchitectureIntel, processorArchitectureARM:
		return false, true
	case processorArchitectureIA64,
		processorArchitectureAMD64,
		processorArchitectureARM64:
		return true, true
	default:
		return false, false
	}
}

func registryViewsForProcess(is64BitWindows bool) []registryView {
	if !is64BitWindows {
		return []registryView{{label: "native"}}
	}
	return []registryView{
		{access: registry.WOW64_64KEY, label: "64-bit"},
		{access: registry.WOW64_32KEY, label: "32-bit"},
	}
}

func deleteRegistryTreeView(path string, view uint32) error {
	const prefix = classesRoot + `\`
	if !strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix)) {
		return fmt.Errorf("comserver: registry path is outside %s", classesRoot)
	}
	return deleteRegistryTreeWithAPI(
		windowsRegistryTreeAPI{},
		registry.CURRENT_USER,
		path,
		view,
	)
}

func deleteRegistryTree(parent registry.Key, path string) error {
	return deleteRegistryTreeWithAPI(
		windowsRegistryTreeAPI{},
		parent,
		path,
		0,
	)
}

func deleteRegistryTreeWithAPI(
	api registryTreeAPI,
	parent registry.Key,
	path string,
	view uint32,
) error {
	if api == nil {
		return errors.New("comserver: nil registry tree API")
	}
	key, err := api.OpenKey(
		parent,
		path,
		registry.ALL_ACCESS|view,
	)
	if isRegistryNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	names, readErr := api.ReadSubKeyNames(key)
	if readErr == nil {
		for _, name := range names {
			if err := deleteRegistryTreeWithAPI(
				api,
				key,
				name,
				view,
			); err != nil {
				_ = api.CloseKey(key)
				return err
			}
		}
	}
	closeErr := api.CloseKey(key)
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := api.DeleteKey(parent, path, view); isRegistryNotExist(err) {
		return nil
	} else {
		return err
	}
}

func (windowsRegistryTreeAPI) OpenKey(
	parent registry.Key,
	path string,
	access uint32,
) (registry.Key, error) {
	return registry.OpenKey(parent, path, access)
}

func (windowsRegistryTreeAPI) ReadSubKeyNames(
	key registry.Key,
) ([]string, error) {
	return key.ReadSubKeyNames(-1)
}

func (windowsRegistryTreeAPI) CloseKey(key registry.Key) error {
	return key.Close()
}

func (windowsRegistryTreeAPI) DeleteKey(
	parent registry.Key,
	path string,
	view uint32,
) error {
	return deleteRegistryKeyEx(parent, path, view)
}

func deleteRegistryKeyEx(
	parent registry.Key,
	path string,
	view uint32,
) error {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := comRegistryDeleteKeyEx.Find(); err != nil {
		// RegDeleteKeyExW is available on supported Windows versions. Retain
		// the non-view operation solely as a compatibility fallback for an
		// ancient 32-bit system where there is no alternate registry view.
		if view == 0 {
			return registry.DeleteKey(parent, path)
		}
		return fmt.Errorf("resolve RegDeleteKeyExW: %w", err)
	}
	result, _, _ := comRegistryDeleteKeyEx.Call(
		uintptr(parent),
		uintptr(unsafe.Pointer(pathPointer)),
		uintptr(view),
		0,
	)
	if result != 0 {
		return windows.Errno(result)
	}
	return nil
}

func isRegistryNotExist(err error) bool {
	return errors.Is(err, registry.ErrNotExist) ||
		errors.Is(err, windows.ERROR_FILE_NOT_FOUND) ||
		errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
}
