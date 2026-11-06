package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf16"
)

const (
	imageFileDLL             = 0x2000
	exportDirectoryIndex     = 0
	resourceDirectoryIndex   = 2
	exportDirectoryByteCount = 40
)

var expectedExports = []string{
	"PCControllerDesktop_ABIVersion",
	"PCControllerDesktop_EnsurePrimaryW",
	"PCControllerDesktop_ExecuteUtf8",
	"PCControllerDesktop_ExecuteW",
	"PCControllerDesktop_Free",
	"PCControllerDesktop_GetState",
	"PCControllerDesktop_IsRunning",
	"PCControllerDesktop_Off",
	"PCControllerDesktop_On",
	"PCControllerDesktop_Toggle",
}

var allowedSystemImports = map[string]bool{
	"advapi32.dll": true,
	"kernel32.dll": true,
	"msvcrt.dll":   true,
	"ntdll.dll":    true,
	"ole32.dll":    true,
	"shell32.dll":  true,
	"user32.dll":   true,
	"winmm.dll":    true,
	"ws2_32.dll":   true,
}

type options struct {
	filePath     string
	architecture string
	fileVersion  string
}

func main() {
	var opts options
	flag.StringVar(&opts.filePath, "file", "", "PCControllerDesktop.dll to inspect")
	flag.StringVar(
		&opts.architecture,
		"arch",
		"amd64",
		"expected architecture: amd64 or 386",
	)
	flag.StringVar(
		&opts.fileVersion,
		"file-version",
		"",
		"expected four-part Windows file version",
	)
	flag.Parse()

	if err := verify(opts); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "verify-dll: %v\n", err)
		os.Exit(1)
	}
	_, _ = fmt.Fprintf(
		os.Stdout,
		"Verified Windows/%s PCControllerDesktop SDK DLL ABI and resources (%s).\n",
		opts.architecture,
		opts.fileVersion,
	)
}

func verify(opts options) error {
	if strings.TrimSpace(opts.filePath) == "" {
		return errors.New("-file is required")
	}
	if strings.TrimSpace(opts.fileVersion) == "" {
		return errors.New("-file-version is required")
	}
	expectedMachine, err := machineForArchitecture(opts.architecture)
	if err != nil {
		return err
	}
	image, err := pe.Open(opts.filePath)
	if err != nil {
		return fmt.Errorf("open PE: %w", err)
	}
	defer image.Close()

	if image.FileHeader.Machine != expectedMachine {
		return fmt.Errorf(
			"machine is %#x, expected %#x",
			image.FileHeader.Machine,
			expectedMachine,
		)
	}
	if image.FileHeader.Characteristics&imageFileDLL == 0 {
		return errors.New("PE image is not marked as a DLL")
	}
	directories, err := dataDirectories(image)
	if err != nil {
		return err
	}
	if len(directories) <= resourceDirectoryIndex ||
		directories[resourceDirectoryIndex].VirtualAddress == 0 ||
		directories[resourceDirectoryIndex].Size == 0 {
		return errors.New("DLL has no resource directory")
	}

	exports, err := exportedNames(image, directories)
	if err != nil {
		return err
	}
	if err := compareExports(exports, expectedExports); err != nil {
		return err
	}
	imports, err := image.ImportedLibraries()
	if err != nil {
		return fmt.Errorf("read imported libraries: %w", err)
	}
	for _, library := range imports {
		normalized := strings.ToLower(strings.TrimSpace(library))
		if !allowedSystemImports[normalized] {
			return fmt.Errorf("unexpected non-system DLL dependency %q", library)
		}
	}

	data, err := os.ReadFile(opts.filePath)
	if err != nil {
		return fmt.Errorf("read DLL bytes: %w", err)
	}
	for _, value := range []string{
		"PCController Native SDK",
		"PCControllerDesktop.dll",
		opts.fileVersion,
	} {
		if !bytes.Contains(data, utf16LE(value)) {
			return fmt.Errorf("version resource does not contain %q", value)
		}
	}
	return nil
}

func machineForArchitecture(architecture string) (uint16, error) {
	switch strings.ToLower(strings.TrimSpace(architecture)) {
	case "amd64":
		return pe.IMAGE_FILE_MACHINE_AMD64, nil
	case "386":
		return pe.IMAGE_FILE_MACHINE_I386, nil
	default:
		return 0, fmt.Errorf("unsupported SDK architecture %q", architecture)
	}
}

func dataDirectories(image *pe.File) ([]pe.DataDirectory, error) {
	switch header := image.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		return header.DataDirectory[:], nil
	case *pe.OptionalHeader64:
		return header.DataDirectory[:], nil
	default:
		return nil, errors.New("unsupported PE optional header")
	}
}

func exportedNames(
	image *pe.File,
	directories []pe.DataDirectory,
) ([]string, error) {
	if len(directories) <= exportDirectoryIndex {
		return nil, errors.New("PE has no export data-directory entry")
	}
	directory := directories[exportDirectoryIndex]
	if directory.VirtualAddress == 0 ||
		directory.Size < exportDirectoryByteCount {
		return nil, errors.New("DLL has no export directory")
	}
	header, err := bytesAtRVA(
		image,
		directory.VirtualAddress,
		exportDirectoryByteCount,
	)
	if err != nil {
		return nil, fmt.Errorf("read export directory: %w", err)
	}
	nameCount := binary.LittleEndian.Uint32(header[24:28])
	nameTableRVA := binary.LittleEndian.Uint32(header[32:36])
	if nameCount == 0 || nameCount > 1024 {
		return nil, fmt.Errorf("invalid export-name count %d", nameCount)
	}
	table, err := bytesAtRVA(image, nameTableRVA, uint64(nameCount)*4)
	if err != nil {
		return nil, fmt.Errorf("read export-name table: %w", err)
	}
	names := make([]string, 0, nameCount)
	for index := uint32(0); index < nameCount; index++ {
		offset := index * 4
		nameRVA := binary.LittleEndian.Uint32(table[offset : offset+4])
		name, readErr := cStringAtRVA(image, nameRVA, 256)
		if readErr != nil {
			return nil, fmt.Errorf("read export name %d: %w", index, readErr)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func bytesAtRVA(
	image *pe.File,
	rva uint32,
	size uint64,
) ([]byte, error) {
	for _, section := range image.Sections {
		span := section.VirtualSize
		if section.Size > span {
			span = section.Size
		}
		if rva < section.VirtualAddress ||
			uint64(rva-section.VirtualAddress)+size > uint64(span) {
			continue
		}
		data, err := section.Data()
		if err != nil {
			return nil, err
		}
		offset := uint64(rva - section.VirtualAddress)
		if offset+size > uint64(len(data)) {
			return nil, errors.New("RVA extends beyond section data")
		}
		return data[offset : offset+size], nil
	}
	return nil, fmt.Errorf("RVA %#x is outside every section", rva)
}

func cStringAtRVA(
	image *pe.File,
	rva uint32,
	maximum int,
) (string, error) {
	data, err := bytesAtRVA(image, rva, 1)
	if err != nil {
		return "", err
	}
	_ = data
	for length := 1; length <= maximum; length++ {
		candidate, readErr := bytesAtRVA(image, rva, uint64(length))
		if readErr != nil {
			return "", readErr
		}
		if candidate[length-1] == 0 {
			return string(candidate[:length-1]), nil
		}
	}
	return "", errors.New("export name is not NUL terminated")
}

func compareExports(actual, expected []string) error {
	actual = append([]string(nil), actual...)
	expected = append([]string(nil), expected...)
	sort.Strings(actual)
	sort.Strings(expected)
	if len(actual) != len(expected) {
		return fmt.Errorf(
			"exports are %v, expected exactly %v",
			actual,
			expected,
		)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf(
				"exports are %v, expected exactly %v",
				actual,
				expected,
			)
		}
	}
	return nil
}

func utf16LE(value string) []byte {
	units := utf16.Encode([]rune(value))
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	return data
}
