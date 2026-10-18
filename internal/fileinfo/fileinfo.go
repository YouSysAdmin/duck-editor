// Package fileinfo describes binary files for previews: executables by
// format and architecture, images by size, the rest by content type.
package fileinfo

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"image"
	_ "image/gif"  // GIF headers for DecodeConfig
	_ "image/jpeg" // JPEG headers for DecodeConfig
	_ "image/png"  // PNG headers for DecodeConfig
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
)

// Describe returns a short description of the file at path, such as
// "ELF 64-bit executable, x86-64, dynamically linked" or "PNG image,
// 640 x 480". It falls back to the detected content type.
func Describe(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}

	defer func() { _ = f.Close() }()

	for _, describe := range []func(*os.File) (string, bool){describeELF, describeMachO, describePE, describeImage} {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return "", err
		}

		if s, ok := describe(f); ok {
			return s, nil
		}
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	kind, _, _ := strings.Cut(http.DetectContentType(head[:n]), ";")

	return kind, nil
}

func describeELF(f *os.File) (string, bool) {
	ef, err := elf.NewFile(f)
	if err != nil {
		return "", false
	}

	bits := "32-bit"
	if ef.Class == elf.ELFCLASS64 {
		bits = "64-bit"
	}

	kind := map[elf.Type]string{
		elf.ET_EXEC: "executable",
		elf.ET_DYN:  "shared object",
		elf.ET_REL:  "relocatable",
		elf.ET_CORE: "core file",
	}[ef.Type]
	if kind == "" {
		kind = ef.Type.String()
	}

	parts := []string{"ELF " + bits + " " + kind, machineName(ef.Machine.String())}
	for _, p := range ef.Progs {
		if p.Type == elf.PT_INTERP {
			if ef.Type == elf.ET_DYN {
				parts[0] = "ELF " + bits + " executable"
			}

			parts = append(parts, "dynamically linked")

			break
		}
	}

	if ef.Section(".symtab") == nil {
		parts = append(parts, "stripped")
	}

	return strings.Join(parts, ", "), true
}

func describeMachO(f *os.File) (string, bool) {
	if ff, err := macho.NewFatFile(f); err == nil {
		// Subtypes such as arm64e share a CPU type, each name shows once.
		var archs []string
		for _, a := range ff.Arches {
			if name := machineName(a.Cpu.String()); !slices.Contains(archs, name) {
				archs = append(archs, name)
			}
		}

		return "Mach-O universal binary, " + strings.Join(archs, " and "), true
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", false
	}

	mf, err := macho.NewFile(f)
	if err != nil {
		return "", false
	}

	kind := map[macho.Type]string{
		macho.TypeExec:   "executable",
		macho.TypeDylib:  "dynamic library",
		macho.TypeBundle: "bundle",
		macho.TypeObj:    "object",
	}[mf.Type]
	if kind == "" {
		kind = "file"
	}

	return "Mach-O " + kind + ", " + machineName(mf.Cpu.String()), true
}

func describePE(f *os.File) (string, bool) {
	pf, err := pe.NewFile(f)
	if err != nil {
		return "", false
	}

	kind := "executable"
	if pf.Characteristics&pe.IMAGE_FILE_DLL != 0 {
		kind = "DLL"
	}

	machine := map[uint16]string{
		pe.IMAGE_FILE_MACHINE_AMD64: "x86-64",
		pe.IMAGE_FILE_MACHINE_I386:  "x86",
		pe.IMAGE_FILE_MACHINE_ARM64: "arm64",
	}[pf.Machine]
	if machine == "" {
		machine = fmt.Sprintf("machine %#x", pf.Machine)
	}

	return "PE " + kind + ", " + machine, true
}

func describeImage(f *os.File) (string, bool) {
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return "", false
	}

	return fmt.Sprintf("%s image, %d x %d", strings.ToUpper(format), cfg.Width, cfg.Height), true
}

// machineName shortens architecture names of the debug packages.
func machineName(s string) string {
	switch s {
	case "EM_X86_64", "CpuAmd64":
		return "x86-64"
	case "EM_AARCH64", "CpuArm64":
		return "arm64"
	case "EM_386", "Cpu386":
		return "x86"
	case "EM_ARM", "CpuArm":
		return "arm"
	case "EM_RISCV":
		return "riscv"
	}

	return strings.TrimPrefix(strings.TrimPrefix(s, "EM_"), "Cpu")
}
