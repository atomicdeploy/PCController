//go:build windows

package comserver

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

func TestSameRegistrationCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		{
			name:     "exact",
			actual:   `"C:\Program Files\RayanLamp\RayanLamp.exe" /com-server`,
			expected: `"C:\Program Files\RayanLamp\RayanLamp.exe" /com-server`,
			want:     true,
		},
		{
			name:     "windows path casing",
			actual:   `"c:\program files\rayanlamp\rayanlamp.exe" /COM-SERVER`,
			expected: `"C:\Program Files\RayanLamp\RayanLamp.exe" /com-server`,
			want:     true,
		},
		{
			name:     "surrounding whitespace",
			actual:   `  "C:\Apps\RayanLamp.exe" /com-server  `,
			expected: `"C:\Apps\RayanLamp.exe" /com-server`,
			want:     true,
		},
		{
			name:     "different executable",
			actual:   `"C:\Other\RayanLamp.exe" /com-server`,
			expected: `"C:\Apps\RayanLamp.exe" /com-server`,
			want:     false,
		},
		{
			name:     "extra arguments",
			actual:   `"C:\Apps\RayanLamp.exe" /com-server /unexpected`,
			expected: `"C:\Apps\RayanLamp.exe" /com-server`,
			want:     false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := sameRegistrationCommand(test.actual, test.expected); got != test.want {
				t.Fatalf(
					"sameRegistrationCommand(%q, %q) = %v, want %v",
					test.actual,
					test.expected,
					got,
					test.want,
				)
			}
		})
	}
}

func TestRegistryViewsCoverCrossBitnessCOMClients(t *testing.T) {
	t.Parallel()

	native := registryViewsForProcess(false)
	if len(native) != 1 || native[0].access != 0 {
		t.Fatalf("32-bit Windows views = %#v, want one native view", native)
	}

	crossBitness := registryViewsForProcess(true)
	if len(crossBitness) != 2 {
		t.Fatalf("64-bit Windows views = %#v, want two views", crossBitness)
	}
	if crossBitness[0].access == 0 ||
		crossBitness[1].access == 0 ||
		crossBitness[0].access == crossBitness[1].access {
		t.Fatalf("64-bit Windows view selectors are not distinct: %#v", crossBitness)
	}
}

func TestDetect64BitWindowsUsesArchitectureAwareQueries(t *testing.T) {
	t.Parallel()

	query := func(machine uint16, err error) nativeArchitectureQuery {
		return func() (uint16, error) { return machine, err }
	}
	unavailable := errors.New("procedure unavailable")
	tests := []struct {
		name        string
		processBits int
		wow2        nativeArchitectureQuery
		native      nativeArchitectureQuery
		want64      bool
		wantErr     bool
	}{
		{
			name:        "64-bit process implies 64-bit Windows",
			processBits: 64,
			wow2:        nil,
			native:      nil,
			want64:      true,
		},
		{
			name:        "x86 process on ARM64",
			processBits: 32,
			wow2:        query(imageFileMachineARM64, nil),
			native:      nil,
			want64:      true,
		},
		{
			name:        "x86 process on AMD64",
			processBits: 32,
			wow2:        query(imageFileMachineAMD64, nil),
			native:      nil,
			want64:      true,
		},
		{
			name:        "native 32-bit Windows",
			processBits: 32,
			wow2:        query(imageFileMachineI386, nil),
			native:      nil,
			want64:      false,
		},
		{
			name:        "old Windows fallback detects ARM64",
			processBits: 32,
			wow2:        query(0, unavailable),
			native:      query(processorArchitectureARM64, nil),
			want64:      true,
		},
		{
			name:        "unknown WOW2 result falls back safely",
			processBits: 32,
			wow2:        query(0xffff, nil),
			native:      query(processorArchitectureAMD64, nil),
			want64:      true,
		},
		{
			name:        "old Windows fallback detects x86",
			processBits: 32,
			wow2:        query(0, unavailable),
			native:      query(processorArchitectureIntel, nil),
			want64:      false,
		},
		{
			name:        "unknown architectures fail closed",
			processBits: 32,
			wow2:        query(0xffff, nil),
			native:      query(0xffff, nil),
			wantErr:     true,
		},
		{
			name:        "invalid word size",
			processBits: 16,
			wantErr:     true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := detect64BitWindows(
				test.processBits,
				test.wow2,
				test.native,
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && got != test.want64 {
				t.Fatalf("64-bit Windows = %v, want %v", got, test.want64)
			}
		})
	}
}

func TestNativeSystemInfoMatchesWindowsABI(t *testing.T) {
	t.Parallel()

	wantSize := uintptr(36)
	if strconv.IntSize == 64 {
		wantSize = 48
	}
	if got := unsafe.Sizeof(nativeSystemInfo{}); got != wantSize {
		t.Fatalf("nativeSystemInfo size = %d, want %d", got, wantSize)
	}
	if got := unsafe.Offsetof(nativeSystemInfo{}.processorArchitecture); got != 0 {
		t.Fatalf("processor architecture offset = %d, want 0", got)
	}
}

func TestSharedRegistrationOwnershipValidation(t *testing.T) {
	t.Parallel()

	independentRoot := classesRoot + `\` + ProgID
	versionedRoot := classesRoot + `\` + VersionedProgID
	values := map[string]string{
		strings.ToLower(independentRoot + `\CLSID`):  CLSID,
		strings.ToLower(independentRoot + `\CurVer`): VersionedProgID,
		strings.ToLower(versionedRoot + `\CLSID`):    CLSID,
	}
	reader := func(
		path string,
		_ string,
		view uint32,
	) (string, bool, error) {
		if view != 0 {
			return "", false, fmt.Errorf("unexpected shared view %#x", view)
		}
		value, exists := values[strings.ToLower(path)]
		return value, exists, nil
	}
	for _, root := range sharedRegistrationRoots() {
		if err := validateSharedRegistrationRoot(root, reader); err != nil {
			t.Fatalf("valid shared root %s rejected: %v", root, err)
		}
	}

	values[strings.ToLower(independentRoot+`\CurVer`)] = "Foreign.App.2"
	err := validateSharedRegistrationRoot(independentRoot, reader)
	if !errors.Is(err, ErrRegistrationOwnedByAnotherExecutable) {
		t.Fatalf("foreign CurVer error = %v, want ownership error", err)
	}

	delete(values, strings.ToLower(versionedRoot+`\CLSID`))
	err = validateSharedRegistrationRoot(versionedRoot, reader)
	if !errors.Is(err, ErrRegistrationOwnedByAnotherExecutable) {
		t.Fatalf("missing CLSID error = %v, want ownership error", err)
	}
}

func TestHasOwnedRegistrationProtectsIncompletePreexistingView(t *testing.T) {
	t.Parallel()

	executable := `C:\Apps\RayanLamp.exe`
	expected, err := RegistrationCommand(executable)
	if err != nil {
		t.Fatal(err)
	}
	views := registryViewsForProcess(true)
	ownedView := views[0].access
	owned, err := hasOwnedRegistrationWithReaders(
		executable,
		views,
		func(path string, view uint32) (bool, error) {
			if strings.Contains(path, `\CLSID\`) {
				return view == ownedView, nil
			}
			return false, nil
		},
		func(_ string, view uint32) (string, bool, error) {
			if view != ownedView {
				t.Fatalf("unexpected LocalServer32 read in view %#x", view)
			}
			return expected, true, nil
		},
		func(string, string, uint32) (string, bool, error) {
			t.Fatal("shared values read although no shared key exists")
			return "", false, nil
		},
	)
	if err != nil || !owned {
		t.Fatalf(
			"incomplete exact-owned registration = (%t, %v), want (true, nil)",
			owned,
			err,
		)
	}
}

func TestHasOwnedRegistrationReportsOwnedAlongsideForeignView(t *testing.T) {
	t.Parallel()

	executable := `C:\Apps\RayanLamp.exe`
	expected, err := RegistrationCommand(executable)
	if err != nil {
		t.Fatal(err)
	}
	views := registryViewsForProcess(true)
	for _, ownedIndex := range []int{0, 1} {
		ownedIndex := ownedIndex
		t.Run(fmt.Sprintf("owned view %d", ownedIndex), func(t *testing.T) {
			owned, err := hasOwnedServerRegistrationWithReaders(
				executable,
				views,
				func(path string, _ uint32) (bool, error) {
					return strings.Contains(path, `\CLSID\`), nil
				},
				func(_ string, view uint32) (string, bool, error) {
					if view == views[ownedIndex].access {
						return expected, true, nil
					}
					return `"C:\Other\RayanLamp.exe" /com-server`, true, nil
				},
				func(string, string, uint32) (string, bool, error) {
					t.Fatal("shared values read although no shared key exists")
					return "", false, nil
				},
			)
			if !owned ||
				!errors.Is(err, ErrRegistrationOwnedByAnotherExecutable) {
				t.Fatalf(
					"mixed registration = (%t, %v), want (true, ownership error)",
					owned,
					err,
				)
			}
		})
	}
}

func TestOwnedRegistrationDistinguishesSharedMetadataFromServerView(
	t *testing.T,
) {
	t.Parallel()

	executable := `C:\Apps\RayanLamp.exe`
	views := registryViewsForProcess(true)
	sharedValues := map[string]string{
		strings.ToLower(classesRoot + `\` + ProgID + `\CLSID`):          CLSID,
		strings.ToLower(classesRoot + `\` + ProgID + `\CurVer`):         VersionedProgID,
		strings.ToLower(classesRoot + `\` + VersionedProgID + `\CLSID`): CLSID,
	}
	keyExists := func(path string, _ uint32) (bool, error) {
		return !strings.Contains(path, `\CLSID\`), nil
	}
	localServer := func(string, uint32) (string, bool, error) {
		t.Fatal("LocalServer32 read without a CLSID root")
		return "", false, nil
	}
	shared := func(path, _ string, _ uint32) (string, bool, error) {
		value, exists := sharedValues[strings.ToLower(path)]
		return value, exists, nil
	}
	conservative, err := hasOwnedRegistrationWithReaders(
		executable,
		views,
		keyExists,
		localServer,
		shared,
	)
	if err != nil || !conservative {
		t.Fatalf(
			"conservative ownership = (%t, %v), want (true, nil)",
			conservative,
			err,
		)
	}
	server, err := hasOwnedServerRegistrationWithReaders(
		executable,
		views,
		keyExists,
		localServer,
		shared,
	)
	if err != nil || server {
		t.Fatalf(
			"server ownership = (%t, %v), want (false, nil)",
			server,
			err,
		)
	}
}

func TestDeleteOwnedClassViewsPreservesTransferredView(t *testing.T) {
	t.Parallel()

	// The view list represents the successful discovery pass in Unregister.
	// Returning a foreign command here deterministically simulates another
	// installer transferring ownership after discovery but before deletion.
	views := []registryView{{
		access: uint32(registry.WOW64_64KEY),
		label:  "64-bit",
	}}
	removeCalls := 0
	err := deleteOwnedClassViews(
		classesRoot+`\CLSID\`+CLSID,
		`"C:\Apps\RayanLamp.exe" /com-server`,
		views,
		func(string, uint32) (string, bool, error) {
			return `"C:\Other\RayanLamp.exe" /com-server`, true, nil
		},
		func(string, uint32) error {
			removeCalls++
			return nil
		},
	)
	if !errors.Is(err, ErrRegistrationOwnedByAnotherExecutable) {
		t.Fatalf("ownership-transfer error = %v", err)
	}
	if removeCalls != 0 {
		t.Fatalf("transferred view was deleted %d times", removeCalls)
	}
}

func TestDeleteOwnedClassViewsPreservesMissingMarker(t *testing.T) {
	t.Parallel()

	removeCalls := 0
	err := deleteOwnedClassViews(
		classesRoot+`\CLSID\`+CLSID,
		`"C:\Apps\RayanLamp.exe" /com-server`,
		[]registryView{{label: "native"}},
		func(string, uint32) (string, bool, error) {
			return "", false, nil
		},
		func(string, uint32) error {
			removeCalls++
			return nil
		},
	)
	if !errors.Is(err, ErrRegistrationOwnedByAnotherExecutable) {
		t.Fatalf("missing-marker error = %v", err)
	}
	if removeCalls != 0 {
		t.Fatalf("view with missing marker was deleted %d times", removeCalls)
	}
}

func TestDeleteOwnedClassViewsRechecksEveryView(t *testing.T) {
	t.Parallel()

	expected := `"C:\Apps\RayanLamp.exe" /com-server`
	views := registryViewsForProcess(true)
	var reads []uint32
	var removals []uint32
	err := deleteOwnedClassViews(
		classesRoot+`\CLSID\`+CLSID,
		expected,
		views,
		func(_ string, view uint32) (string, bool, error) {
			reads = append(reads, view)
			return "  " + strings.ToUpper(expected) + "  ", true, nil
		},
		func(_ string, view uint32) error {
			removals = append(removals, view)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{
		uint32(registry.WOW64_64KEY),
		uint32(registry.WOW64_32KEY),
	}
	if !reflect.DeepEqual(reads, want) {
		t.Fatalf("ownership rechecks = %#v, want %#v", reads, want)
	}
	if !reflect.DeepEqual(removals, want) {
		t.Fatalf("view deletions = %#v, want %#v", removals, want)
	}
}

func TestDeleteRegistryTreeCarriesAlternateViewRecursively(t *testing.T) {
	t.Parallel()

	const (
		rootHandle  = registry.Key(0x7001)
		childHandle = registry.Key(0x7002)
		leafHandle  = registry.Key(0x7003)
	)
	rootPath := classesRoot + `\CLSID\` + CLSID
	view := uint32(registry.WOW64_32KEY)
	api := &recordingRegistryTreeAPI{
		openResults: map[registryOpenKey]registry.Key{
			{parent: registry.CURRENT_USER, path: rootPath}: rootHandle,
			{parent: rootHandle, path: "Child"}:             childHandle,
			{parent: childHandle, path: "Leaf"}:             leafHandle,
		},
		children: map[registry.Key][]string{
			rootHandle:  {"Child"},
			childHandle: {"Leaf"},
			leafHandle:  {},
		},
	}
	if err := deleteRegistryTreeWithAPI(
		api,
		registry.CURRENT_USER,
		rootPath,
		view,
	); err != nil {
		t.Fatal(err)
	}

	for _, opened := range api.opens {
		if opened.access != registry.ALL_ACCESS|view {
			t.Fatalf(
				"open %s access = %#x, want %#x",
				opened.path,
				opened.access,
				registry.ALL_ACCESS|view,
			)
		}
	}
	wantDeletes := []registryDeleteCall{
		{parent: childHandle, path: "Leaf", view: view},
		{parent: rootHandle, path: "Child", view: view},
		{parent: registry.CURRENT_USER, path: rootPath, view: view},
	}
	if !reflect.DeepEqual(api.deletes, wantDeletes) {
		t.Fatalf("delete calls = %#v, want %#v", api.deletes, wantDeletes)
	}
	if !reflect.DeepEqual(
		api.closed,
		[]registry.Key{leafHandle, childHandle, rootHandle},
	) {
		t.Fatalf("closed handles = %#v", api.closed)
	}
}

func TestDeleteRegistryTreeViewRejectsOutsideClassesRoot(t *testing.T) {
	t.Parallel()

	err := deleteRegistryTreeView(
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		uint32(registry.WOW64_64KEY),
	)
	if err == nil || !strings.Contains(err.Error(), classesRoot) {
		t.Fatalf("outside-root deletion error = %v", err)
	}
}

type registryOpenKey struct {
	parent registry.Key
	path   string
}

type registryOpenCall struct {
	parent registry.Key
	path   string
	access uint32
}

type registryDeleteCall struct {
	parent registry.Key
	path   string
	view   uint32
}

type recordingRegistryTreeAPI struct {
	openResults map[registryOpenKey]registry.Key
	children    map[registry.Key][]string
	opens       []registryOpenCall
	deletes     []registryDeleteCall
	closed      []registry.Key
}

func (api *recordingRegistryTreeAPI) OpenKey(
	parent registry.Key,
	path string,
	access uint32,
) (registry.Key, error) {
	api.opens = append(api.opens, registryOpenCall{
		parent: parent,
		path:   path,
		access: access,
	})
	key, exists := api.openResults[registryOpenKey{parent: parent, path: path}]
	if !exists {
		return 0, registry.ErrNotExist
	}
	return key, nil
}

func (api *recordingRegistryTreeAPI) ReadSubKeyNames(
	key registry.Key,
) ([]string, error) {
	names, exists := api.children[key]
	if !exists {
		return nil, fmt.Errorf("unexpected key %#x", key)
	}
	return append([]string(nil), names...), nil
}

func (api *recordingRegistryTreeAPI) CloseKey(key registry.Key) error {
	api.closed = append(api.closed, key)
	return nil
}

func (api *recordingRegistryTreeAPI) DeleteKey(
	parent registry.Key,
	path string,
	view uint32,
) error {
	api.deletes = append(api.deletes, registryDeleteCall{
		parent: parent,
		path:   path,
		view:   view,
	})
	return nil
}
