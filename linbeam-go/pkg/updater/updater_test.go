package updater

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestIsValidAMD64ELF(t *testing.T) {
	tmp := t.TempDir()

	valid := make([]byte, 24)
	copy(valid[0:4], []byte{0x7f, 'E', 'L', 'F'})
	binary.LittleEndian.PutUint16(valid[18:20], 62)
	validPath := filepath.Join(tmp, "linbeam_amd64")
	if err := os.WriteFile(validPath, valid, 0755); err != nil {
		t.Fatal(err)
	}
	if !isValidAMD64ELF(validPath) {
		t.Fatalf("expected valid x86_64 ELF binary to pass verification")
	}

	htmlPage := filepath.Join(tmp, "not_elf.html")
	if err := os.WriteFile(htmlPage, []byte("<!DOCTYPE html><html>404 Not Found</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if isValidAMD64ELF(htmlPage) {
		t.Fatalf("expected HTML file to be rejected by ELF check")
	}
}

func TestFindGitRepoRoot(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	subDir := filepath.Join(tmp, "linbeam-go")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	root, ok := findGitRepoRoot(subDir)
	if !ok || root != tmp {
		t.Fatalf("expected git repo root %s, got %s (ok=%v)", tmp, root, ok)
	}
}
