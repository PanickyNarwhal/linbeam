package proton

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestIsCompatibleX86BinaryRejectsARM64(t *testing.T) {
	tmp := t.TempDir()

	// Construct minimal 20-byte ELF header for x86_64 (EM_X86_64 = 62)
	amd64Hdr := make([]byte, 24)
	copy(amd64Hdr[0:4], []byte{0x7f, 'E', 'L', 'F'})
	binary.LittleEndian.PutUint16(amd64Hdr[18:20], 62)
	amd64Path := filepath.Join(tmp, "wine64_amd64")
	if err := os.WriteFile(amd64Path, amd64Hdr, 0755); err != nil {
		t.Fatal(err)
	}
	if !isCompatibleX86Binary(amd64Path) {
		t.Fatalf("expected x86_64 ELF binary to be accepted")
	}

	// Construct minimal 20-byte ELF header for aarch64 (EM_AARCH64 = 183)
	arm64Hdr := make([]byte, 24)
	copy(arm64Hdr[0:4], []byte{0x7f, 'E', 'L', 'F'})
	binary.LittleEndian.PutUint16(arm64Hdr[18:20], 183)
	arm64Path := filepath.Join(tmp, "wine64_arm64")
	if err := os.WriteFile(arm64Path, arm64Hdr, 0755); err != nil {
		t.Fatal(err)
	}
	if isCompatibleX86Binary(arm64Path) {
		t.Fatalf("expected aarch64 ELF binary to be rejected")
	}
}
