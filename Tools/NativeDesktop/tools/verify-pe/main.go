package main

import (
	"bytes"
	standardbuildinfo "debug/buildinfo"
	"debug/pe"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"

	"pccontroller.local/desktop/internal/buildinfo"
	"pccontroller.local/desktop/internal/terminal"
)

const resourceDirectoryIndex = 2

const (
	assemblyV1Namespace       = "urn:schemas-microsoft-com:asm.v1"
	assemblyV3Namespace       = "urn:schemas-microsoft-com:asm.v3"
	windowsSettings2024Schema = "http://schemas.microsoft.com/SMI/2024/WindowsSettings"
	applicationIconResourceID = 32512
)

var resourceTypeNames = map[uint32]string{
	3:  "icon image",
	14: "icon group",
	16: "version information",
	24: "application manifest",
}

type options struct {
	filePath     string
	architecture string
	edition      string
	fileVersion  string
	buildNumber  uint
	packed       bool
}

type executable struct {
	machine       uint16
	subsystem     uint16
	packed        bool
	resourceData  []byte
	resourceTypes map[uint32]bool
	iconGroupIDs  map[uint32]bool
	fileData      []byte
	buildTags     string
	debugSections bool
	symbolCount   int
}

type peSection struct {
	name           string
	virtualAddress uint32
	virtualSize    uint32
	rawOffset      uint32
	rawSize        uint32
}

type peLayout struct {
	machine      uint16
	subsystem    uint16
	packed       bool
	resourceRVA  uint32
	resourceSize uint32
	sections     []peSection
}

type applicationManifest struct {
	XMLName      xml.Name              `xml:"urn:schemas-microsoft-com:asm.v1 assembly"`
	Applications []manifestApplication `xml:"urn:schemas-microsoft-com:asm.v3 application"`
}

type manifestApplication struct {
	WindowsSettings []manifestWindowsSettings `xml:"urn:schemas-microsoft-com:asm.v3 windowsSettings"`
}

type manifestWindowsSettings struct {
	ConsoleAllocationPolicies []string `xml:"http://schemas.microsoft.com/SMI/2024/WindowsSettings consoleAllocationPolicy"`
}

func main() {
	var opts options
	flag.StringVar(&opts.filePath, "file", "dist/PCControllerDesktop.exe", "Windows executable to inspect")
	flag.StringVar(&opts.architecture, "arch", "amd64", "expected architecture: amd64, 386, or arm64")
	flag.StringVar(&opts.edition, "edition", "", "expected edition: develop or production")
	flag.StringVar(&opts.fileVersion, "file-version", "", "expected four-part Windows file version")
	flag.UintVar(&opts.buildNumber, "build-number", 0, "fourth version component when -file-version is omitted")
	flag.BoolVar(&opts.packed, "packed", false, "verify packed PE/resources without scanning compressed application data")
	flag.Parse()

	if opts.fileVersion == "" {
		opts.fileVersion = fmt.Sprintf("%s.%d", buildinfo.Version, opts.buildNumber)
	}
	if err := verify(opts); err != nil {
		printStatus(
			os.Stderr,
			terminal.BoldRed,
			"❌",
			"[ERROR]",
			"PE verification failed: "+err.Error(),
		)
		os.Exit(1)
	}
	printStatus(
		os.Stdout,
		terminal.BoldGreen,
		"✅",
		"[OK]",
		fmt.Sprintf(
			"Verified %s as Windows/%s %sconsole-subsystem %s with required Windows resources",
			opts.filePath,
			opts.architecture,
			editionLabel(opts.edition),
			opts.fileVersion,
		),
	)
}

func verify(opts options) error {
	if opts.edition != "" &&
		opts.edition != buildinfo.EditionDevelop &&
		opts.edition != buildinfo.EditionProduction {
		return fmt.Errorf("unsupported build edition %q", opts.edition)
	}
	expectedMachine, err := machineForArchitecture(opts.architecture)
	if err != nil {
		return err
	}
	image, err := inspectExecutable(opts.filePath)
	if err != nil {
		return err
	}
	if image.machine != expectedMachine {
		return fmt.Errorf(
			"machine is %#x, expected %#x for %s",
			image.machine,
			expectedMachine,
			opts.architecture,
		)
	}
	if err := verifySubsystem(image.subsystem); err != nil {
		return err
	}
	for resourceType, name := range resourceTypeNames {
		if !image.resourceTypes[resourceType] {
			return fmt.Errorf("resource table is missing %s (type %d)", name, resourceType)
		}
	}
	if !image.iconGroupIDs[applicationIconResourceID] {
		return fmt.Errorf(
			"icon group %d (IDI_APPLICATION) is missing; the window class would use the generic system icon",
			applicationIconResourceID,
		)
	}

	resourceASCII := []string{
		"DRSDavidSoft.PCController",
		`version="` + opts.fileVersion + `"`,
	}
	for _, expected := range resourceASCII {
		if !bytes.Contains(image.resourceData, []byte(expected)) {
			return fmt.Errorf("resource data does not contain %q", expected)
		}
	}
	if err := verifyDetachedConsolePolicy(image.resourceData); err != nil {
		return err
	}
	resourceUTF16 := []string{
		"PCController",
		"PCControllerDesktop.exe",
		opts.fileVersion,
	}
	for _, expected := range resourceUTF16 {
		if !bytes.Contains(image.resourceData, utf16LE(expected)) {
			return fmt.Errorf("version resource does not contain %q", expected)
		}
	}

	if !opts.packed && !image.packed {
		webMarkers := []string{
			"manifest.webmanifest",
			"theme-init.js",
			"sw.js",
		}
		for _, marker := range webMarkers {
			if !bytes.Contains(image.fileData, []byte(marker)) {
				return fmt.Errorf("embedded web distribution marker %q is absent", marker)
			}
		}
		if opts.edition != "" {
			if err := verifyEdition(image, opts.edition); err != nil {
				return err
			}
		}
	} else if opts.edition != "" {
		return errors.New(
			"edition payload cannot be verified after executable compression",
		)
	}
	return nil
}

func editionLabel(edition string) string {
	if edition == "" {
		return ""
	}
	return edition + " "
}

func verifyEdition(image executable, edition string) error {
	var expectedTag string
	var oppositeTag string
	var marker string
	switch edition {
	case buildinfo.EditionDevelop:
		expectedTag = "pccontroller_desktop_develop"
		oppositeTag = "pccontroller_desktop_production"
		marker = `{"edition":"develop","developer_tools":true}`
		if !image.debugSections || image.symbolCount == 0 {
			return errors.New(
				"development edition must retain native symbols and DWARF sections",
			)
		}
	case buildinfo.EditionProduction:
		expectedTag = "pccontroller_desktop_production"
		oppositeTag = "pccontroller_desktop_develop"
		marker = `{"edition":"production","developer_tools":false}`
		if image.debugSections || image.symbolCount != 0 {
			return errors.New(
				"production edition must strip native symbols and DWARF sections",
			)
		}
	default:
		return fmt.Errorf("unsupported build edition %q", edition)
	}
	if !commaListContains(image.buildTags, expectedTag) {
		return fmt.Errorf(
			"Go build tags %q do not identify the %s edition (%s)",
			image.buildTags,
			edition,
			expectedTag,
		)
	}
	if commaListContains(image.buildTags, oppositeTag) {
		return fmt.Errorf(
			"Go build tags %q contain both edition tags",
			image.buildTags,
		)
	}
	if !bytes.Contains(image.fileData, []byte(marker)) {
		return fmt.Errorf(
			"embedded Web UI does not contain the %s edition marker",
			edition,
		)
	}
	return nil
}

func commaListContains(value, expected string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) == expected {
			return true
		}
	}
	return false
}

func verifyDetachedConsolePolicy(resourceData []byte) error {
	manifestXML, err := extractManifestXML(resourceData)
	if err != nil {
		return err
	}
	var manifest applicationManifest
	if err := xml.Unmarshal(manifestXML, &manifest); err != nil {
		return fmt.Errorf("parse embedded application manifest: %w", err)
	}
	for _, application := range manifest.Applications {
		for _, settings := range application.WindowsSettings {
			for _, policy := range settings.ConsoleAllocationPolicies {
				if strings.EqualFold(strings.TrimSpace(policy), "detached") {
					return nil
				}
			}
		}
	}
	return fmt.Errorf(
		"application manifest must place consoleAllocationPolicy=detached under "+
			"%s application/windowsSettings with the %s namespace",
		assemblyV3Namespace,
		windowsSettings2024Schema,
	)
}

func extractManifestXML(resourceData []byte) ([]byte, error) {
	start := bytes.Index(resourceData, []byte("<assembly"))
	if start < 0 {
		return nil, errors.New("resource data does not contain an application manifest")
	}
	const closingElement = "</assembly>"
	relativeEnd := bytes.Index(resourceData[start:], []byte(closingElement))
	if relativeEnd < 0 {
		return nil, errors.New("embedded application manifest is truncated")
	}
	end := start + relativeEnd + len(closingElement)
	return resourceData[start:end], nil
}

func verifySubsystem(actual uint16) error {
	if actual == pe.IMAGE_SUBSYSTEM_WINDOWS_CUI {
		return nil
	}
	return fmt.Errorf(
		"subsystem is %d, expected Windows console/CUI (%d)",
		actual,
		pe.IMAGE_SUBSYSTEM_WINDOWS_CUI,
	)
}

func inspectExecutable(path string) (executable, error) {
	fileData, err := os.ReadFile(path)
	if err != nil {
		return executable{}, fmt.Errorf("read executable: %w", err)
	}
	layout, err := parsePELayout(fileData)
	if err != nil {
		return executable{}, fmt.Errorf("parse PE image: %w", err)
	}
	resourceData, err := readResourceData(fileData, layout)
	if err != nil {
		return executable{}, fmt.Errorf("read resource directory: %w", err)
	}
	resourceTypes, err := readResourceTypes(resourceData)
	if err != nil {
		return executable{}, err
	}
	iconGroupIDs, err := readResourceIDs(resourceData, 14)
	if err != nil {
		return executable{}, fmt.Errorf("read icon-group resources: %w", err)
	}
	result := executable{
		machine:       layout.machine,
		subsystem:     layout.subsystem,
		packed:        layout.packed,
		resourceData:  resourceData,
		resourceTypes: resourceTypes,
		iconGroupIDs:  iconGroupIDs,
		fileData:      fileData,
		symbolCount:   -1,
	}
	if !layout.packed {
		peFile, openErr := pe.Open(path)
		if openErr != nil {
			return executable{}, fmt.Errorf("open PE debug metadata: %w", openErr)
		}
		for _, section := range peFile.Sections {
			if strings.HasPrefix(strings.ToLower(section.Name), ".debug_") {
				result.debugSections = true
				break
			}
		}
		result.symbolCount = len(peFile.Symbols)
		if closeErr := peFile.Close(); closeErr != nil {
			return executable{}, fmt.Errorf("close PE debug metadata: %w", closeErr)
		}

		metadata, metadataErr := standardbuildinfo.ReadFile(path)
		if metadataErr != nil {
			return executable{}, fmt.Errorf("read Go build metadata: %w", metadataErr)
		}
		for _, setting := range metadata.Settings {
			if setting.Key == "-tags" {
				result.buildTags = setting.Value
				break
			}
		}
	}
	return result, nil
}

func parsePELayout(data []byte) (peLayout, error) {
	if len(data) < 0x40 || !bytes.Equal(data[:2], []byte("MZ")) {
		return peLayout{}, errors.New("DOS executable header is missing")
	}
	peOffset := uint64(binary.LittleEndian.Uint32(data[0x3c:0x40]))
	coffHeader, ok := sliceAt(data, peOffset, 24)
	if !ok || !bytes.Equal(coffHeader[:4], []byte{'P', 'E', 0, 0}) {
		return peLayout{}, errors.New("PE signature or COFF header is truncated")
	}
	machine := binary.LittleEndian.Uint16(coffHeader[4:6])
	sectionCount := int(binary.LittleEndian.Uint16(coffHeader[6:8]))
	optionalSize := uint64(binary.LittleEndian.Uint16(coffHeader[20:22]))
	if sectionCount <= 0 {
		return peLayout{}, errors.New("PE image has no sections")
	}
	optionalOffset := peOffset + 24
	optionalHeader, ok := sliceAt(data, optionalOffset, optionalSize)
	if !ok || len(optionalHeader) < 70 {
		return peLayout{}, errors.New("optional header is truncated")
	}

	var directoryOffset int
	var directoryCountOffset int
	switch binary.LittleEndian.Uint16(optionalHeader[:2]) {
	case 0x10b:
		directoryOffset = 96
		directoryCountOffset = 92
	case 0x20b:
		directoryOffset = 112
		directoryCountOffset = 108
	default:
		return peLayout{}, errors.New("optional header is neither PE32 nor PE32+")
	}
	if directoryCountOffset+4 > len(optionalHeader) ||
		binary.LittleEndian.Uint32(
			optionalHeader[directoryCountOffset:directoryCountOffset+4],
		) <= resourceDirectoryIndex {
		return peLayout{}, errors.New("optional header has no resource directory entry")
	}
	resourceEntryOffset := directoryOffset + resourceDirectoryIndex*8
	if resourceEntryOffset+8 > len(optionalHeader) {
		return peLayout{}, errors.New("resource data-directory entry is truncated")
	}
	resourceRVA := binary.LittleEndian.Uint32(
		optionalHeader[resourceEntryOffset : resourceEntryOffset+4],
	)
	resourceSize := binary.LittleEndian.Uint32(
		optionalHeader[resourceEntryOffset+4 : resourceEntryOffset+8],
	)
	if resourceRVA == 0 || resourceSize == 0 {
		return peLayout{}, errors.New("PE image has no resource data directory")
	}

	sectionOffset := optionalOffset + optionalSize
	sectionTable, ok := sliceAt(data, sectionOffset, uint64(sectionCount)*40)
	if !ok {
		return peLayout{}, errors.New("section table is truncated")
	}
	sections := make([]peSection, 0, sectionCount)
	packed := false
	for index := 0; index < sectionCount; index++ {
		header := sectionTable[index*40 : (index+1)*40]
		name := strings.TrimRight(string(header[:8]), "\x00")
		if isPackedSection(name) {
			packed = true
		}
		sections = append(sections, peSection{
			name:           name,
			virtualSize:    binary.LittleEndian.Uint32(header[8:12]),
			virtualAddress: binary.LittleEndian.Uint32(header[12:16]),
			rawSize:        binary.LittleEndian.Uint32(header[16:20]),
			rawOffset:      binary.LittleEndian.Uint32(header[20:24]),
		})
	}
	return peLayout{
		machine:      machine,
		subsystem:    binary.LittleEndian.Uint16(optionalHeader[68:70]),
		packed:       packed,
		resourceRVA:  resourceRVA,
		resourceSize: resourceSize,
		sections:     sections,
	}, nil
}

func isPackedSection(name string) bool {
	upperName := strings.ToUpper(strings.TrimSpace(name))
	return upperName == "UPX0" || upperName == "UPX1" ||
		strings.HasPrefix(upperName, ".UPX")
}

func readResourceData(data []byte, layout peLayout) ([]byte, error) {
	for _, section := range layout.sections {
		span := section.virtualSize
		if section.rawSize > span {
			span = section.rawSize
		}
		if layout.resourceRVA < section.virtualAddress ||
			uint64(layout.resourceRVA)+uint64(layout.resourceSize) >
				uint64(section.virtualAddress)+uint64(span) {
			continue
		}
		delta := uint64(layout.resourceRVA - section.virtualAddress)
		if delta+uint64(layout.resourceSize) > uint64(section.rawSize) {
			return nil, errors.New("resource directory extends beyond its raw section")
		}
		resourceData, ok := sliceAt(
			data,
			uint64(section.rawOffset)+delta,
			uint64(layout.resourceSize),
		)
		if !ok {
			return nil, errors.New("resource directory extends beyond the file")
		}
		return resourceData, nil
	}
	return nil, errors.New("resource directory RVA is outside every PE section")
}

func sliceAt(data []byte, offset, size uint64) ([]byte, bool) {
	if offset > uint64(len(data)) ||
		size > uint64(len(data))-offset {
		return nil, false
	}
	return data[offset : offset+size], true
}

func readResourceTypes(data []byte) (map[uint32]bool, error) {
	if len(data) < 16 {
		return nil, errors.New("resource directory header is truncated")
	}
	namedCount := int(binary.LittleEndian.Uint16(data[12:14]))
	idCount := int(binary.LittleEndian.Uint16(data[14:16]))
	entryCount := namedCount + idCount
	if entryCount > (len(data)-16)/8 {
		return nil, errors.New("resource directory entries are truncated")
	}
	result := make(map[uint32]bool, idCount)
	for index := 0; index < entryCount; index++ {
		offset := 16 + index*8
		nameOrID := binary.LittleEndian.Uint32(data[offset : offset+4])
		if nameOrID&0x80000000 != 0 {
			continue
		}
		result[nameOrID&0xffff] = true
	}
	return result, nil
}

func readResourceIDs(data []byte, resourceType uint32) (map[uint32]bool, error) {
	if len(data) < 16 {
		return nil, errors.New("resource directory header is truncated")
	}
	namedCount := int(binary.LittleEndian.Uint16(data[12:14]))
	idCount := int(binary.LittleEndian.Uint16(data[14:16]))
	entryCount := namedCount + idCount
	if entryCount > (len(data)-16)/8 {
		return nil, errors.New("resource directory entries are truncated")
	}

	var childOffset uint32
	found := false
	for index := 0; index < entryCount; index++ {
		offset := 16 + index*8
		nameOrID := binary.LittleEndian.Uint32(data[offset : offset+4])
		if nameOrID&0x80000000 != 0 || nameOrID&0xffff != resourceType {
			continue
		}
		directory := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		if directory&0x80000000 == 0 {
			return nil, fmt.Errorf("resource type %d does not point to a directory", resourceType)
		}
		childOffset = directory & 0x7fffffff
		found = true
		break
	}
	if !found {
		return map[uint32]bool{}, nil
	}

	childHeader, ok := sliceAt(data, uint64(childOffset), 16)
	if !ok {
		return nil, fmt.Errorf("resource type %d directory header is truncated", resourceType)
	}
	childNamedCount := int(binary.LittleEndian.Uint16(childHeader[12:14]))
	childIDCount := int(binary.LittleEndian.Uint16(childHeader[14:16]))
	childEntryCount := childNamedCount + childIDCount
	childEntries, ok := sliceAt(
		data,
		uint64(childOffset)+16,
		uint64(childEntryCount)*8,
	)
	if !ok {
		return nil, fmt.Errorf("resource type %d directory entries are truncated", resourceType)
	}

	result := make(map[uint32]bool, childIDCount)
	for index := 0; index < childEntryCount; index++ {
		offset := index * 8
		nameOrID := binary.LittleEndian.Uint32(childEntries[offset : offset+4])
		if nameOrID&0x80000000 != 0 {
			continue
		}
		result[nameOrID&0xffff] = true
	}
	return result, nil
}

func machineForArchitecture(architecture string) (uint16, error) {
	switch strings.ToLower(strings.TrimSpace(architecture)) {
	case "amd64":
		return pe.IMAGE_FILE_MACHINE_AMD64, nil
	case "386":
		return pe.IMAGE_FILE_MACHINE_I386, nil
	case "arm64":
		return pe.IMAGE_FILE_MACHINE_ARM64, nil
	default:
		return 0, fmt.Errorf("unsupported architecture %q", architecture)
	}
}

func utf16LE(value string) []byte {
	codeUnits := utf16.Encode([]rune(value))
	encoded := make([]byte, len(codeUnits)*2)
	for index, codeUnit := range codeUnits {
		binary.LittleEndian.PutUint16(encoded[index*2:], codeUnit)
	}
	return encoded
}

func printStatus(
	writer io.Writer,
	style string,
	emoji string,
	ascii string,
	message string,
) {
	symbol := terminal.Symbol(writer, emoji, ascii)
	fmt.Fprintf(writer, "%s %s\n", terminal.Styled(writer, style, symbol), message)
}
