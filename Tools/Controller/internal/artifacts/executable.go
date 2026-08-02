package artifacts

import (
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// ExecutableIdentity is derived from the binary header rather than a filename
// or caller-provided platform string.
type ExecutableIdentity struct {
	Format string
	OS     string
	Arch   string
}

func (identity ExecutableIdentity) Platform() string {
	if identity.OS == "" || identity.Arch == "" {
		return ""
	}
	return identity.OS + "/" + identity.Arch
}

// InspectHostExecutable accepts native PE, ELF, and Mach-O executables and
// rejects scripts, arbitrary blobs, unknown CPUs, and library-only PE images.
func InspectHostExecutable(path string) (ExecutableIdentity, error) {
	file, err := os.Open(path)
	if err != nil {
		return ExecutableIdentity{}, err
	}
	defer file.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(file, header); err != nil {
		return ExecutableIdentity{}, errors.New("host executable is too short")
	}
	switch {
	case header[0] == 'M' && header[1] == 'Z':
		return inspectPE(path)
	case string(header) == "\x7fELF":
		return inspectELF(path)
	default:
		magic := binary.BigEndian.Uint32(header)
		littleMagic := binary.LittleEndian.Uint32(header)
		if isMachOMagic(magic) || isMachOMagic(littleMagic) {
			return inspectMachO(path)
		}
	}
	return ExecutableIdentity{}, errors.New("host executable must be PE, ELF, or Mach-O")
}

func inspectPE(path string) (ExecutableIdentity, error) {
	file, err := os.Open(path)
	if err != nil {
		return ExecutableIdentity{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ExecutableIdentity{}, fmt.Errorf("inspect PE executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ExecutableIdentity{}, errors.New("PE artifact is not a regular file")
	}

	// Windows only needs the DOS pointer, PE signature, COFF identity fields,
	// and optional-header magic to identify a runnable image. In particular,
	// COFF symbol and string tables are optional debugging metadata. Packers
	// such as UPX may leave those offsets opaque even though the image remains
	// a valid Windows executable, so do not ask debug/pe to parse unused tables.
	var dosHeader [64]byte
	if _, err := io.ReadFull(file, dosHeader[:]); err != nil {
		return ExecutableIdentity{}, fmt.Errorf("parse PE DOS header: %w", err)
	}
	if dosHeader[0] != 'M' || dosHeader[1] != 'Z' {
		return ExecutableIdentity{}, errors.New("PE artifact is missing its DOS signature")
	}
	peOffset := int64(binary.LittleEndian.Uint32(dosHeader[0x3c:0x40]))
	const peIdentityHeaderBytes = int64(4 + 20)
	if peOffset < int64(len(dosHeader)) || peOffset > info.Size()-peIdentityHeaderBytes {
		return ExecutableIdentity{}, errors.New("PE artifact has an invalid header offset")
	}
	var identityHeader [peIdentityHeaderBytes]byte
	if _, err := file.ReadAt(identityHeader[:], peOffset); err != nil {
		return ExecutableIdentity{}, fmt.Errorf("parse PE executable header: %w", err)
	}
	if string(identityHeader[:4]) != "PE\x00\x00" {
		return ExecutableIdentity{}, errors.New("PE artifact is missing its executable signature")
	}
	coffHeader := identityHeader[4:]
	machine := binary.LittleEndian.Uint16(coffHeader[0:2])
	sections := binary.LittleEndian.Uint16(coffHeader[2:4])
	optionalHeaderBytes := int64(binary.LittleEndian.Uint16(coffHeader[16:18]))
	characteristics := binary.LittleEndian.Uint16(coffHeader[18:20])
	if sections == 0 {
		return ExecutableIdentity{}, errors.New("PE artifact has no executable sections")
	}
	if characteristics&0x0002 == 0 {
		return ExecutableIdentity{}, errors.New("PE artifact is not marked as an executable image")
	}
	// IMAGE_FILE_DLL means a loadable library, not a directly restartable host.
	if characteristics&0x2000 != 0 {
		return ExecutableIdentity{}, errors.New("PE artifact is a DLL, not an executable")
	}
	arch := map[uint16]string{
		0x014c: "386", 0x8664: "amd64", 0x01c0: "arm",
		0x01c4: "arm", 0xaa64: "arm64",
	}[machine]
	if arch == "" {
		return ExecutableIdentity{}, fmt.Errorf("unsupported PE machine 0x%04X", machine)
	}
	optionalOffset := peOffset + peIdentityHeaderBytes
	if optionalHeaderBytes < 2 || optionalOffset > info.Size()-optionalHeaderBytes {
		return ExecutableIdentity{}, errors.New("PE artifact has an invalid optional header")
	}
	var optionalMagic [2]byte
	if _, err := file.ReadAt(optionalMagic[:], optionalOffset); err != nil {
		return ExecutableIdentity{}, fmt.Errorf("parse PE optional header: %w", err)
	}
	minimumOptionalHeaderBytes := int64(0)
	switch binary.LittleEndian.Uint16(optionalMagic[:]) {
	case 0x010b: // PE32
		minimumOptionalHeaderBytes = 224
	case 0x020b: // PE32+
		minimumOptionalHeaderBytes = 240
	default:
		return ExecutableIdentity{}, errors.New("PE artifact has an unsupported optional header")
	}
	if optionalHeaderBytes < minimumOptionalHeaderBytes {
		return ExecutableIdentity{}, errors.New("PE artifact has a truncated optional header")
	}
	// The section table is loader-relevant, unlike the optional COFF symbol and
	// string tables. Require the declared table to be present without parsing
	// packer-specific section contents.
	const sectionHeaderBytes = int64(40)
	sectionTableOffset := optionalOffset + optionalHeaderBytes
	sectionTableBytes := int64(sections) * sectionHeaderBytes
	if sectionTableOffset > info.Size()-sectionTableBytes {
		return ExecutableIdentity{}, errors.New("PE artifact has a truncated section table")
	}
	return ExecutableIdentity{Format: "pe", OS: "windows", Arch: arch}, nil
}

func inspectELF(path string) (ExecutableIdentity, error) {
	value, err := elf.Open(path)
	if err != nil {
		return ExecutableIdentity{}, fmt.Errorf("parse ELF executable: %w", err)
	}
	defer value.Close()
	if value.Type != elf.ET_EXEC && value.Type != elf.ET_DYN {
		return ExecutableIdentity{}, fmt.Errorf("ELF type %s is not executable", value.Type)
	}
	arch := map[elf.Machine]string{
		elf.EM_386: "386", elf.EM_X86_64: "amd64", elf.EM_ARM: "arm",
		elf.EM_AARCH64: "arm64", elf.EM_RISCV: "riscv64",
	}[value.Machine]
	if arch == "" {
		return ExecutableIdentity{}, fmt.Errorf("unsupported ELF machine %s", value.Machine)
	}
	return ExecutableIdentity{Format: "elf", OS: "linux", Arch: arch}, nil
}

func inspectMachO(path string) (ExecutableIdentity, error) {
	if fat, err := macho.OpenFat(path); err == nil {
		defer fat.Close()
		if len(fat.Arches) != 1 {
			return ExecutableIdentity{}, errors.New("universal Mach-O artifacts must be split into one architecture")
		}
		arch, err := machoArchitecture(fat.Arches[0].Cpu)
		if err != nil {
			return ExecutableIdentity{}, err
		}
		if fat.Arches[0].Type != macho.TypeExec {
			return ExecutableIdentity{}, errors.New("Mach-O artifact is not an executable")
		}
		return ExecutableIdentity{Format: "macho", OS: "darwin", Arch: arch}, nil
	}
	value, err := macho.Open(path)
	if err != nil {
		return ExecutableIdentity{}, fmt.Errorf("parse Mach-O executable: %w", err)
	}
	defer value.Close()
	if value.Type != macho.TypeExec {
		return ExecutableIdentity{}, errors.New("Mach-O artifact is not an executable")
	}
	arch, err := machoArchitecture(value.Cpu)
	if err != nil {
		return ExecutableIdentity{}, err
	}
	return ExecutableIdentity{Format: "macho", OS: "darwin", Arch: arch}, nil
}

func machoArchitecture(cpu macho.Cpu) (string, error) {
	arch := map[macho.Cpu]string{
		macho.Cpu386: "386", macho.CpuAmd64: "amd64",
		macho.CpuArm: "arm", macho.CpuArm64: "arm64",
	}[cpu]
	if arch == "" {
		return "", fmt.Errorf("unsupported Mach-O CPU %s", cpu)
	}
	return arch, nil
}

func isMachOMagic(value uint32) bool {
	switch value {
	case macho.Magic32, macho.Magic64, macho.MagicFat:
		return true
	default:
		return false
	}
}
