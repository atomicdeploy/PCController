package artifacts

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInspectHostExecutableDerivesCurrentPlatform(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := InspectHostExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Platform() != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("identity=%#v runtime=%s/%s", identity, runtime.GOOS, runtime.GOARCH)
	}
}

func TestStoreRejectsNonExecutableAndFalsePlatformMetadata(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Put(strings.NewReader("not an executable"), PutOptions{
		Kind: KindHostExecutable, Name: "host.bin",
	}); err == nil || !strings.Contains(err.Error(), "PE, ELF, or Mach-O") {
		t.Fatalf("non-executable err=%v", err)
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	wrong := "linux/amd64"
	if runtime.GOOS == "linux" {
		wrong = "windows/amd64"
	}
	if _, err := store.Put(file, PutOptions{
		Kind: KindHostExecutable, Name: filepath.Base(path), Platform: wrong,
	}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("false platform err=%v", err)
	}
}

func TestStoreAcceptsPEWithOpaqueOptionalCOFFMetadata(t *testing.T) {
	image := opaquePackedPEImage()
	path := filepath.Join(t.TempDir(), "packed-host.exe")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	if parsed, err := pe.Open(path); err == nil {
		_ = parsed.Close()
		t.Fatal("fixture unexpectedly has parseable COFF string-table metadata")
	} else if !strings.Contains(err.Error(), "string table length") {
		t.Fatalf("fixture did not reproduce packed PE metadata failure: %v", err)
	}

	identity, err := InspectHostExecutable(path)
	if err != nil {
		t.Fatalf("inspect packed PE identity: %v", err)
	}
	if identity != (ExecutableIdentity{Format: "pe", OS: "windows", Arch: "amd64"}) {
		t.Fatalf("identity=%#v", identity)
	}

	digest := sha256.Sum256(image)
	store := newTestStore(t)
	descriptor, err := store.PutFile(path, PutOptions{
		Kind: KindHostExecutable, Name: filepath.Base(path),
		ExpectedSHA256: hex.EncodeToString(digest[:]), ExpectedBytes: int64(len(image)),
		Platform: "windows/amd64", Source: "packed-host-test", Current: true,
	})
	if err != nil {
		t.Fatalf("store packed PE: %v", err)
	}
	if descriptor.SHA256 != hex.EncodeToString(digest[:]) || descriptor.Bytes != int64(len(image)) {
		t.Fatalf("descriptor=%#v", descriptor)
	}
	_, storedFile, err := store.Open(KindHostExecutable, descriptor.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer storedFile.Close()
	stored, err := io.ReadAll(storedFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, image) {
		t.Fatal("content-addressed packed executable differs from its source")
	}
}

func TestInspectHostExecutableRejectsInvalidLoaderMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte) []byte
		want   string
	}{
		{
			name: "DLL",
			mutate: func(image []byte) []byte {
				binary.LittleEndian.PutUint16(image[0x80+4+18:0x80+4+20], 0x2022)
				return image
			},
			want: "DLL",
		},
		{
			name: "truncated optional header",
			mutate: func(image []byte) []byte {
				binary.LittleEndian.PutUint16(image[0x80+4+16:0x80+4+18], 2)
				return image
			},
			want: "truncated optional header",
		},
		{
			name: "truncated section table",
			mutate: func(image []byte) []byte {
				return image[:0x80+4+20+0xf0+39]
			},
			want: "truncated section table",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image := test.mutate(opaquePackedPEImage())
			path := filepath.Join(t.TempDir(), "invalid.exe")
			if err := os.WriteFile(path, image, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := InspectHostExecutable(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want substring %q", err, test.want)
			}
		})
	}
}

// Set PCCONTROLLER_TEST_PACKED_EXE to exercise the exact output of the host
// packager through the same Store.PutFile path used during service startup.
func TestStoreAcceptsExternalPackedHostExecutable(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("PCCONTROLLER_TEST_PACKED_EXE"))
	if path == "" {
		t.Skip("PCCONTROLLER_TEST_PACKED_EXE is not set")
	}
	if parsed, err := pe.Open(path); err != nil {
		t.Logf("debug/pe rejection reproduced and intentionally bypassed: %v", err)
	} else {
		_ = parsed.Close()
		t.Log("debug/pe accepted this packer output; exercising registration anyway")
	}
	identity, err := InspectHostExecutable(path)
	if err != nil {
		t.Fatalf("inspect packed host executable: %v", err)
	}
	if identity.Platform() != "windows/"+runtime.GOARCH {
		t.Fatalf("identity=%#v runtime arch=%s", identity, runtime.GOARCH)
	}
	store := newTestStore(t)
	descriptor, err := store.PutFile(path, PutOptions{
		Kind: KindHostExecutable, Name: filepath.Base(path),
		Platform: identity.Platform(), Source: "external-packed-host-test", Current: true,
	})
	if err != nil {
		t.Fatalf("register packed host executable: %v", err)
	}
	current, err := store.Current(KindHostExecutable)
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.SHA256 != descriptor.SHA256 || current.Platform != identity.Platform() {
		t.Fatalf("current=%#v descriptor=%#v", current, descriptor)
	}
}

func TestStorePutFileStillRejectsUnreadableHostSource(t *testing.T) {
	store := newTestStore(t)
	missing := filepath.Join(t.TempDir(), "unreadable-host.exe")
	if _, err := store.PutFile(missing, PutOptions{
		Kind: KindHostExecutable, Name: filepath.Base(missing),
	}); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreadable source err=%v", err)
	}
}

// opaquePackedPEImage models a runnable PE whose unused COFF symbol pointer
// makes debug/pe seek beyond EOF for a string table, as packed binaries can.
func opaquePackedPEImage() []byte {
	const peOffset = 0x80
	image := make([]byte, 0x200)
	copy(image[0:2], "MZ")
	binary.LittleEndian.PutUint32(image[0x3c:0x40], peOffset)
	copy(image[peOffset:peOffset+4], "PE\x00\x00")
	coff := image[peOffset+4 : peOffset+24]
	binary.LittleEndian.PutUint16(coff[0:2], 0x8664) // AMD64
	binary.LittleEndian.PutUint16(coff[2:4], 1)      // one section
	// One complete 18-byte COFF symbol ends exactly at EOF, leaving the
	// advertised four-byte string-table length outside the readable image.
	binary.LittleEndian.PutUint32(coff[8:12], uint32(len(image)-18))
	binary.LittleEndian.PutUint32(coff[12:16], 1)
	binary.LittleEndian.PutUint16(coff[16:18], 0x00f0)
	binary.LittleEndian.PutUint16(coff[18:20], 0x0022) // executable image
	binary.LittleEndian.PutUint16(image[peOffset+24:peOffset+26], 0x020b)
	return image
}
