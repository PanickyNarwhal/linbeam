package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PanickyNarwhal/linbeam/pkg/config"
	"github.com/PanickyNarwhal/linbeam/pkg/proton"
)

func TestEmbeddedMsiPatcherPresent(t *testing.T) {
	if len(embeddedMsiPatcherCS) < 1000 {
		t.Fatalf("expected embedded msi_patcher.cs to be populated, got %d bytes", len(embeddedMsiPatcherCS))
	}
	content := string(embeddedMsiPatcherCS)
	for _, action := range []string{"Set_REBOOT_NEEDED_Prop_PS", "Set_LANGUAGE_CHOICE_Prop_PS", "ExpandWebView2", "SetupFile1"} {
		if !strings.Contains(content, action) {
			t.Errorf("expected msi_patcher.cs to contain %q", action)
		}
	}
}

func TestFlattenSingleSubdir(t *testing.T) {
	tmp := t.TempDir()
	wv2Dir := filepath.Join(tmp, "WebView2Runtime")
	nested := filepath.Join(wv2Dir, "Microsoft.WebView2.FixedVersionRuntime.124.0.x64")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	exeFile := filepath.Join(nested, "msedgewebview2.exe")
	if err := os.WriteFile(exeFile, []byte("stub"), 0644); err != nil {
		t.Fatal(err)
	}

	flattenSingleSubdir(wv2Dir, nil)

	flattenedExe := filepath.Join(wv2Dir, "msedgewebview2.exe")
	if !fileExists(flattenedExe) {
		t.Fatalf("expected msedgewebview2.exe to be moved to %s", flattenedExe)
	}
}

func TestFindRevuExecutable(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		DataDir:   tmp,
		PrefixDir: filepath.Join(tmp, "prefix"),
	}
	eng := proton.NewEngine(cfg)
	bm := NewBluebeamManager(cfg, eng)

	if _, ok := bm.FindRevuExecutable(); ok {
		t.Fatal("expected FindRevuExecutable to return false on empty prefix")
	}

	revuExe := filepath.Join(eng.DriveCPath(), "Program Files", "Bluebeam Software", "Bluebeam Revu", "21", "Revu", "Revu.exe")
	if err := os.MkdirAll(filepath.Dir(revuExe), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(revuExe, []byte("MZ"), 0755); err != nil {
		t.Fatal(err)
	}

	found, ok := bm.FindRevuExecutable()
	if !ok || found != revuExe {
		t.Fatalf("expected %s, got %s (ok=%v)", revuExe, found, ok)
	}
}
