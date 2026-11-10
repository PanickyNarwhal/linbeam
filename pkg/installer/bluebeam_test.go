package installer

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PanickyNarwhal/linbeam/pkg/config"
	"github.com/PanickyNarwhal/linbeam/pkg/proton"
)

func TestExtractDeploymentZipAndAutoSelect(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "MSIBluebeamRevu21x64.zip")

	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	files := []string{
		"MSIBluebeamRevu21.11.0x64/Bluebeam Revu x64 21.msi",
		"MSIBluebeamRevu21.11.0x64/BluebeamOCR x64 21.msi",
		"MSIBluebeamRevu21.11.0x64/vc_redist.x64.exe",
		"MSIBluebeamRevu21.11.0x64/ndp48-x86-x64-allos-enu.exe",
	}
	for _, name := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("dummy-payload"))
	}
	_ = zw.Close()
	_ = zf.Close()

	cfg := &config.Config{
		DataDir:    tmp,
		PrefixDir:  filepath.Join(tmp, "prefix"),
		RunnersDir: filepath.Join(tmp, "runners"),
		CacheDir:   filepath.Join(tmp, "cache"),
	}
	if err := extractDeploymentZip(zipPath, cfg.ManagedPayloadsDir(), nil); err != nil {
		t.Fatalf("extractDeploymentZip failed: %v", err)
	}

	info := cfg.AutoDetectPayloads()
	if !strings.HasSuffix(info.RevuMSI, "Bluebeam Revu x64 21.msi") {
		t.Fatalf("expected Bluebeam Revu x64 21.msi to be auto-selected, got %q", info.RevuMSI)
	}
	if !strings.HasSuffix(info.OCRMSI, "BluebeamOCR x64 21.msi") {
		t.Fatalf("expected BluebeamOCR x64 21.msi to be auto-selected, got %q", info.OCRMSI)
	}
	if !strings.HasSuffix(info.DotNet48Exe, "ndp48-x86-x64-allos-enu.exe") {
		t.Fatalf("expected ndp48-x86-x64-allos-enu.exe to be detected, got %q", info.DotNet48Exe)
	}
}

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

func assertValidWellFormedXML(t *testing.T, data []byte) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("invalid XML produced: %v\nXML:\n%s", err, string(data))
		}
	}
}

func TestPatchUserPreferencesXML(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		mustPreserve string
	}{
		{
			name:         "empty input generates valid Revu 21 template",
			input:        "",
			mustPreserve: "<ScaleUnits>Inches</ScaleUnits>",
		},
		{
			name: "injects Revu SignInMethod into stock MSI UserPreferences.xml",
			input: `<?xml version="1.0" encoding="utf-8"?>
<RevuUserPreferences Version="2">
  <Record Key="ToolChest">
    <ItemSize>48</ItemSize>
  </Record>
  <Record Key="PreferencesDictionary">
    <General>
      <RevuAppDefaultModeInt>0</RevuAppDefaultModeInt>
    </General>
  </Record>
</RevuUserPreferences>`,
			mustPreserve: "<ItemSize>48</ItemSize>",
		},
		{
			name: "overrides ModalBrowser inside existing Revu block while preserving sibling keys",
			input: `<?xml version="1.0" encoding="utf-8"?>
<RevuUserPreferences Version="2">
  <Record Key="PreferencesDictionary">
    <Revu>
      <CustomSetting>KeepMe</CustomSetting>
      <SignInMethod>ModalBrowser</SignInMethod>
    </Revu>
  </Record>
</RevuUserPreferences>`,
			mustPreserve: "<CustomSetting>KeepMe</CustomSetting>",
		},
		{
			name: "expands self-closing PreferencesDictionary record",
			input: `<?xml version="1.0" encoding="utf-8"?>
<RevuUserPreferences Version="2">
  <Record Key="RecentFileList" />
  <Record Key="PreferencesDictionary" />
</RevuUserPreferences>`,
			mustPreserve: `<Record Key="RecentFileList" />`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := PatchUserPreferencesXML([]byte(tc.input))
			assertValidWellFormedXML(t, out)
			s := string(out)
			if !strings.Contains(s, "<SignInMethod>SystemBrowser</SignInMethod>") {
				t.Fatalf("expected <SignInMethod>SystemBrowser</SignInMethod>, got:\n%s", s)
			}
			if strings.Contains(s, "ModalBrowser") {
				t.Fatalf("expected ModalBrowser to be replaced, got:\n%s", s)
			}
			if tc.mustPreserve != "" && !strings.Contains(s, tc.mustPreserve) {
				t.Fatalf("expected %q to be preserved, got:\n%s", tc.mustPreserve, s)
			}
		})
	}
}

func TestEnsureSignInMethodInDriveC(t *testing.T) {
	driveC := filepath.Join(t.TempDir(), "drive_c")
	existingRoaming := filepath.Join(driveC, "users", "steamuser", "AppData", "Roaming", "Bluebeam Software", "Bluebeam Revu", "21", "Revu", "UserPreferences.xml")
	if err := os.MkdirAll(filepath.Dir(existingRoaming), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(driveC, "users", "Public"), 0755); err != nil {
		t.Fatal(err)
	}

	initialXML := `<?xml version="1.0" encoding="utf-8"?>
<RevuUserPreferences Version="2">
  <Record Key="PreferencesDictionary">
    <Revu>
      <SignInMethod>ModalBrowser</SignInMethod>
    </Revu>
  </Record>
</RevuUserPreferences>`
	if err := os.WriteFile(existingRoaming, []byte(initialXML), 0644); err != nil {
		t.Fatal(err)
	}

	updated := EnsureSignInMethodInDriveC(driveC)
	if updated != 2 {
		t.Fatalf("expected 2 files updated (ProgramData + steamuser), got %d", updated)
	}

	// Second call should be idempotent (0 files rewritten)
	if second := EnsureSignInMethodInDriveC(driveC); second != 0 {
		t.Fatalf("expected second EnsureSignInMethodInDriveC call to rewrite 0 files, got %d", second)
	}

	roamingBytes, err := os.ReadFile(existingRoaming)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(roamingBytes), "<SignInMethod>SystemBrowser</SignInMethod>") {
		t.Fatalf("expected SystemBrowser in steamuser UserPreferences.xml, got:\n%s", string(roamingBytes))
	}

	publicPrefs := filepath.Join(driveC, "users", "Public", "AppData", "Roaming", "Bluebeam Software", "Bluebeam Revu", "21", "Revu", "UserPreferences.xml")
	if fileExists(publicPrefs) {
		t.Fatal("expected Public profile to be skipped")
	}
}
