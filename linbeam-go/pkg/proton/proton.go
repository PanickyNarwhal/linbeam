package proton

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/PanickyNarwhal/linbeam/pkg/config"
)

// RunnerInfo represents an installed Proton or Wine runner.
type RunnerInfo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	WineBin    string `json:"wine_bin"`
	Wine64Bin  string `json:"wine64_bin"`
	ServerBin  string `json:"server_bin"`
	ProtonPath string `json:"proton_path"`
	Source     string `json:"source"`
}

// Engine manages the Proton-GE runner and Wine prefix for Bluebeam Revu 21.
type Engine struct {
	Config *config.Config
}

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{Config: cfg}
}

// WinePrefixPath returns the actual Wine prefix directory (inside pfx/).
func (e *Engine) WinePrefixPath() string {
	return filepath.Join(e.Config.PrefixDir, "pfx")
}

// DriveCPath returns the drive_c directory inside the Wine prefix.
func (e *Engine) DriveCPath() string {
	return filepath.Join(e.WinePrefixPath(), "drive_c")
}

// UnixToWinePath converts a Linux filesystem path to a Wine Z: path.
func (e *Engine) UnixToWinePath(unixPath string) string {
	abs, err := filepath.Abs(unixPath)
	if err != nil {
		abs = unixPath
	}
	winSlashes := strings.ReplaceAll(abs, "/", `\`)
	return `Z:` + winSlashes
}

// DiscoverRunners scans known directories for Proton-GE and Wine runners.
func (e *Engine) DiscoverRunners() ([]RunnerInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	searchDirs := []struct {
		dir    string
		source string
	}{
		{e.Config.RunnersDir, "linbeam"},
		{filepath.Join(home, ".steam", "root", "compatibilitytools.d"), "steam"},
		{filepath.Join(home, ".steam", "steam", "compatibilitytools.d"), "steam"},
		{filepath.Join(home, ".local", "share", "Steam", "compatibilitytools.d"), "steam"},
		{filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam", "compatibilitytools.d"), "flatpak-steam"},
		{filepath.Join(home, ".local", "share", "lutris", "runners", "wine"), "lutris"},
		{filepath.Join(home, ".config", "heroic", "tools", "proton"), "heroic"},
	}

	seen := make(map[string]bool)
	var runners []RunnerInfo

	for _, sd := range searchDirs {
		entries, err := os.ReadDir(sd.dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			runnerPath := filepath.Join(sd.dir, entry.Name())
			absPath, err := filepath.EvalSymlinks(runnerPath)
			if err != nil {
				absPath = runnerPath
			}
			if seen[absPath] {
				continue
			}

			if info, ok := inspectRunnerDir(entry.Name(), runnerPath, sd.source); ok {
				seen[absPath] = true
				runners = append(runners, info)
			}
		}
	}

	sort.Slice(runners, func(i, j int) bool {
		return runners[i].Name > runners[j].Name
	})

	return runners, nil
}

func inspectRunnerDir(name, dir, source string) (RunnerInfo, bool) {
	info := RunnerInfo{
		Name:   name,
		Path:   dir,
		Source: source,
	}

	protonScript := filepath.Join(dir, "proton")
	if fileExists(protonScript) {
		info.ProtonPath = protonScript
	}

	subDirs := []string{"files", "dist", ""}
	for _, sub := range subDirs {
		base := dir
		if sub != "" {
			base = filepath.Join(dir, sub)
		}
		wineBin := filepath.Join(base, "bin", "wine")
		wine64Bin := filepath.Join(base, "bin", "wine64")
		serverBin := filepath.Join(base, "bin", "wineserver")

		if fileExists(wineBin) || fileExists(wine64Bin) {
			if fileExists(wineBin) {
				info.WineBin = wineBin
			} else {
				info.WineBin = wine64Bin
			}
			if fileExists(wine64Bin) {
				info.Wine64Bin = wine64Bin
			} else {
				info.Wine64Bin = wineBin
			}
			if fileExists(serverBin) {
				info.ServerBin = serverBin
			}
			return info, true
		}
	}

	return info, false
}

// GetActiveRunner returns the currently configured runner or the newest available one.
func (e *Engine) GetActiveRunner() (*RunnerInfo, error) {
	runners, err := e.DiscoverRunners()
	if err != nil {
		return nil, err
	}
	if len(runners) == 0 {
		return nil, fmt.Errorf("no Proton-GE or Wine runners found; download Proton-GE first")
	}

	if e.Config.SelectedRunner != "" {
		for _, r := range runners {
			if r.Name == e.Config.SelectedRunner || r.Path == e.Config.SelectedRunner {
				return &r, nil
			}
		}
	}

	return &runners[0], nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// DownloadLatestGEProton downloads and extracts the latest GE-Proton build into RunnersDir.
func (e *Engine) DownloadLatestGEProton(progress func(stage string, percent int)) (*RunnerInfo, error) {
	if progress != nil {
		progress("Querying GitHub for latest GE-Proton release...", 2)
	}

	req, err := http.NewRequest("GET", "https://api.github.com/repos/GloriousEggroll/proton-ge-custom/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Linbeam-Installer/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GE-Proton releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release JSON: %w", err)
	}

	var downloadURL, assetName string
	var assetSize int64
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, ".tar.gz") && !strings.Contains(asset.Name, "sha512") {
			downloadURL = asset.BrowserDownloadURL
			assetName = asset.Name
			assetSize = asset.Size
			break
		}
	}

	if downloadURL == "" {
		return nil, fmt.Errorf("no .tar.gz asset found in release %s", release.TagName)
	}

	destDir := filepath.Join(e.Config.RunnersDir, release.TagName)
	if info, ok := inspectRunnerDir(release.TagName, destDir, "linbeam"); ok {
		if progress != nil {
			progress(fmt.Sprintf("%s is already installed.", release.TagName), 100)
		}
		e.Config.SelectedRunner = info.Name
		_ = e.Config.Save()
		return &info, nil
	}

	if err := e.Config.EnsureDirs(); err != nil {
		return nil, err
	}

	tarPath := filepath.Join(e.Config.CacheDir, assetName)
	if progress != nil {
		progress(fmt.Sprintf("Downloading %s...", assetName), 5)
	}

	if err := downloadFileWithProgress(downloadURL, tarPath, assetSize, func(pct int, mbDone, mbTotal int64) {
		if progress != nil {
			scaled := 5 + (pct * 75 / 100)
			progress(fmt.Sprintf("Downloading %s (%d MB / %d MB)...", release.TagName, mbDone, mbTotal), scaled)
		}
	}); err != nil {
		return nil, err
	}

	if progress != nil {
		progress(fmt.Sprintf("Extracting %s to %s...", assetName, e.Config.RunnersDir), 82)
	}

	cmd := exec.Command("tar", "-xzf", tarPath, "-C", e.Config.RunnersDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to extract %s: %w (%s)", tarPath, err, string(output))
	}

	_ = os.Remove(tarPath)

	if progress != nil {
		progress("Finalizing Proton-GE installation...", 98)
	}

	info, ok := inspectRunnerDir(release.TagName, destDir, "linbeam")
	if !ok {
		runners, _ := e.DiscoverRunners()
		if len(runners) > 0 {
			info = runners[0]
		} else {
			return nil, fmt.Errorf("extracted %s, but could not locate wine binaries inside it", release.TagName)
		}
	}

	e.Config.SelectedRunner = info.Name
	_ = e.Config.Save()

	if progress != nil {
		progress(fmt.Sprintf("Installed %s!", info.Name), 100)
	}

	return &info, nil
}

func downloadFileWithProgress(url, destPath string, expectedSize int64, onProgress func(pct int, mbDone, mbTotal int64)) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Linbeam-Installer/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if total <= 0 {
		total = expectedSize
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 64*1024)
	var downloaded int64
	lastPct := -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)
			if total > 0 && onProgress != nil {
				pct := int(downloaded * 100 / total)
				if pct != lastPct {
					lastPct = pct
					onProgress(pct, downloaded/(1024*1024), total/(1024*1024))
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return nil
}

// getEnv builds the Wine/Proton environment variables for child processes.
func (e *Engine) getEnv(runner *RunnerInfo) []string {
	env := os.Environ()
	pfx := e.WinePrefixPath()
	_ = os.MkdirAll(pfx, 0755)

	binDir := filepath.Dir(runner.WineBin)
	baseDir := filepath.Dir(binDir)
	lib64Dir := filepath.Join(baseDir, "lib64")
	libDir := filepath.Join(baseDir, "lib")

	currentPath := os.Getenv("PATH")
	newPath := fmt.Sprintf("%s:%s", binDir, currentPath)

	currentLD := os.Getenv("LD_LIBRARY_PATH")
	newLD := fmt.Sprintf("%s:%s:%s", lib64Dir, libDir, currentLD)

	// Configure DLL overrides for .NET 4.8, DXVK, UCRT, and disable builtin Gecko mshtml
	// so Bluebeam uses its FixedVersion WebView2 runtime with sandbox disabled.
	overrides := map[string]string{
		"WINEPREFIX":                              pfx,
		"STEAM_COMPAT_DATA_PATH":                  e.Config.PrefixDir,
		"WINEARCH":                                "win64",
		"WINE":                                    runner.WineBin,
		"WINE64":                                  runner.Wine64Bin,
		"WINESERVER":                              runner.ServerBin,
		"PATH":                                    newPath,
		"LD_LIBRARY_PATH":                         newLD,
		"WINEDLLOVERRIDES":                        "mscoree=n;mshtml=;d3d11=n,b;dxgi=n,b;d3d9=n,b;ucrtbase=n,b",
		"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS":   "--no-sandbox --disable-gpu",
		"WINEESYNC":                               "1",
		"WINEFSYNC":                               "1",
		"PROTON_NO_ESYNC":                         "0",
		"WINEDEBUG":                               "-all",
	}

	filtered := make([]string, 0, len(env)+len(overrides))
	for _, item := range env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			if _, exists := overrides[parts[0]]; exists {
				continue
			}
		}
		filtered = append(filtered, item)
	}

	for k, v := range overrides {
		if v != "" {
			filtered = append(filtered, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return filtered
}

// InitPrefix initializes the Wine prefix synchronously.
func (e *Engine) InitPrefix() error {
	return e.InitPrefixStream(nil)
}

// InitPrefixStream initializes the Wine prefix, installs DXVK, synthesizes WPF core fonts,
// and applies Windows 10 + Bluebeam registry configurations.
func (e *Engine) InitPrefixStream(onLine func(string)) error {
	runner, err := e.GetActiveRunner()
	if err != nil {
		return err
	}

	emit := func(msg string) {
		if onLine != nil {
			onLine(msg)
		}
	}

	pfx := e.WinePrefixPath()
	if err := os.MkdirAll(pfx, 0755); err != nil {
		return err
	}

	emit(fmt.Sprintf("Initializing 64-bit Proton prefix at %s using %s...", pfx, runner.Name))
	if err := e.RunWineStream(onLine, "wineboot", "-u"); err != nil {
		return fmt.Errorf("wineboot failed: %w", err)
	}

	emit("Installing bundled DXVK (Direct3D 9/11 over Vulkan) from Proton-GE...")
	e.installDXVK(runner, emit)

	emit("Synthesizing core Windows OpenType/TrueType fonts for WPF & PDF rendering...")
	e.ensureCoreFonts(emit)

	emit("Configuring Windows 10 (Build 19045), PowerShell 5.1, VC++ 14, and WebView2 registry keys...")
	if err := e.SetWindows10(onLine); err != nil {
		emit(fmt.Sprintf("Warning: SetWindows10 returned: %v", err))
	}

	if err := e.ApplyBluebeamRegistryOverrides(onLine); err != nil {
		emit(fmt.Sprintf("Warning: ApplyBluebeamRegistryOverrides returned: %v", err))
	}

	if err := e.ApplyDarkTheme(onLine); err != nil {
		emit(fmt.Sprintf("Warning: ApplyDarkTheme returned: %v", err))
	}

	emit("Waiting for wineserver to flush prefix registry...")
	_ = e.WaitServer()

	emit("Proton-GE prefix ready.")
	return nil
}

// installDXVK copies DXVK DLLs from the Proton-GE runner into system32 and syswow64.
func (e *Engine) installDXVK(runner *RunnerInfo, emit func(string)) {
	binDir := filepath.Dir(runner.WineBin)
	filesDir := filepath.Dir(binDir)
	pfx := e.WinePrefixPath()

	dxvkDirs := []struct {
		src string
		dst string
	}{
		{
			src: filepath.Join(filesDir, "lib64", "wine", "dxvk"),
			dst: filepath.Join(pfx, "drive_c", "windows", "system32"),
		},
		{
			src: filepath.Join(filesDir, "lib", "wine", "dxvk"),
			dst: filepath.Join(pfx, "drive_c", "windows", "syswow64"),
		},
	}

	dlls := []string{"d3d11.dll", "d3d9.dll", "dxgi.dll"}
	installed := 0

	for _, pair := range dxvkDirs {
		if !dirExists(pair.src) || !dirExists(pair.dst) {
			continue
		}
		for _, dll := range dlls {
			srcFile := filepath.Join(pair.src, dll)
			dstFile := filepath.Join(pair.dst, dll)
			if fileExists(srcFile) {
				_ = os.Remove(dstFile)
				if err := copyFile(srcFile, dstFile); err == nil {
					installed++
				}
			}
		}
	}

	if installed > 0 {
		emit(fmt.Sprintf("Installed %d DXVK DLLs into prefix.", installed))
	} else {
		emit("No bundled DXVK directory found in runner (using Proton default D3D).")
	}
}

// ensureCoreFonts populates C:\windows\Fonts with valid TrueType fonts whose internal
// OpenType name tables match standard Windows font family names (Segoe UI, Arial, Tahoma, etc.).
func (e *Engine) ensureCoreFonts(emit func(string)) {
	pfx := e.WinePrefixPath()
	winFontsDir := filepath.Join(pfx, "drive_c", "windows", "Fonts")
	_ = os.MkdirAll(winFontsDir, 0755)

	sansCandidates := []string{
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/liberation-sans/LiberationSans-Regular.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/noto/NotoSans-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
		"/usr/share/fonts/gnu-free/FreeSans.ttf",
	}
	sansBoldCandidates := []string{
		"/usr/share/fonts/TTF/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/liberation-sans/LiberationSans-Bold.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf",
		"/usr/share/fonts/noto/NotoSans-Bold.ttf",
	}
	monoCandidates := []string{
		"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/liberation-mono/LiberationMono-Regular.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
	}
	serifCandidates := []string{
		"/usr/share/fonts/TTF/DejaVuSerif.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf",
		"/usr/share/fonts/liberation-serif/LiberationSerif-Regular.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSerif-Regular.ttf",
	}

	runner, err := e.GetActiveRunner()
	if err == nil {
		filesDir := filepath.Dir(filepath.Dir(runner.WineBin))
		protonFonts := filepath.Join(filesDir, "share", "fonts")
		sansCandidates = append(sansCandidates,
			filepath.Join(protonFonts, "LiberationSans-Regular.ttf"),
			filepath.Join(protonFonts, "wine_tahoma.ttf"),
		)
		sansBoldCandidates = append(sansBoldCandidates,
			filepath.Join(protonFonts, "LiberationSans-Bold.ttf"),
			filepath.Join(protonFonts, "wine_tahomabd.ttf"),
		)
		monoCandidates = append(monoCandidates, filepath.Join(protonFonts, "LiberationMono-Regular.ttf"))
		serifCandidates = append(serifCandidates, filepath.Join(protonFonts, "LiberationSerif-Regular.ttf"))
	}

	pickFirst := func(paths []string) string {
		for _, p := range paths {
			if fileExists(p) {
				return p
			}
		}
		return ""
	}

	sansSrc := pickFirst(sansCandidates)
	sansBoldSrc := pickFirst(sansBoldCandidates)
	if sansBoldSrc == "" {
		sansBoldSrc = sansSrc
	}
	monoSrc := pickFirst(monoCandidates)
	if monoSrc == "" {
		monoSrc = sansSrc
	}
	serifSrc := pickFirst(serifCandidates)
	if serifSrc == "" {
		serifSrc = sansSrc
	}

	if sansSrc == "" {
		emit("Warning: No system TrueType fonts found to populate C:\\windows\\Fonts.")
		return
	}

	type fontSpec struct {
		file       string
		src        string
		regName    string
		familyName string
		subFamily  string
	}

	specs := []fontSpec{
		{"segoeui.ttf", sansSrc, "Segoe UI (TrueType)", "Segoe UI", "Regular"},
		{"segoeuib.ttf", sansBoldSrc, "Segoe UI Bold (TrueType)", "Segoe UI", "Bold"},
		{"segoeuii.ttf", sansSrc, "Segoe UI Italic (TrueType)", "Segoe UI", "Italic"},
		{"segoeuil.ttf", sansSrc, "Segoe UI Light (TrueType)", "Segoe UI Light", "Regular"},
		{"seguisb.ttf", sansBoldSrc, "Segoe UI Semibold (TrueType)", "Segoe UI Semibold", "Regular"},
		{"arial.ttf", sansSrc, "Arial (TrueType)", "Arial", "Regular"},
		{"arialbd.ttf", sansBoldSrc, "Arial Bold (TrueType)", "Arial", "Bold"},
		{"tahoma.ttf", sansSrc, "Tahoma (TrueType)", "Tahoma", "Regular"},
		{"tahomabd.ttf", sansBoldSrc, "Tahoma Bold (TrueType)", "Tahoma", "Bold"},
		{"times.ttf", serifSrc, "Times New Roman (TrueType)", "Times New Roman", "Regular"},
		{"cour.ttf", monoSrc, "Courier New (TrueType)", "Courier New", "Regular"},
		{"consola.ttf", monoSrc, "Consolas (TrueType)", "Consolas", "Regular"},
		{"verdana.ttf", sansSrc, "Verdana (TrueType)", "Verdana", "Regular"},
		{"calibri.ttf", sansSrc, "Calibri (TrueType)", "Calibri", "Regular"},
		{"calibrib.ttf", sansBoldSrc, "Calibri Bold (TrueType)", "Calibri", "Bold"},
		{"trebuc.ttf", sansSrc, "Trebuchet MS (TrueType)", "Trebuchet MS", "Regular"},
		{"micross.ttf", sansSrc, "Microsoft Sans Serif (TrueType)", "Microsoft Sans Serif", "Regular"},
	}

	copied := 0
	for _, spec := range specs {
		dst := filepath.Join(winFontsDir, spec.file)
		srcBytes, err := os.ReadFile(spec.src)
		if err != nil {
			continue
		}
		patched, err := BuildTTFWithFamilyName(srcBytes, spec.familyName, spec.subFamily)
		if err != nil {
			patched = srcBytes
		}
		if err := os.WriteFile(dst, patched, 0644); err == nil {
			copied++
		}
	}

	regKeys := []string{
		`HKLM\Software\Microsoft\Windows NT\CurrentVersion\Fonts`,
		`HKLM\Software\Wow6432Node\Microsoft\Windows NT\CurrentVersion\Fonts`,
	}
	for _, key := range regKeys {
		for _, spec := range specs {
			_ = e.RunWineStream(nil, "reg", "add", key, "/v", spec.regName, "/t", "REG_SZ", "/d", spec.file, "/f")
		}
	}

	emit(fmt.Sprintf("Synthesized and registered %d core Windows TrueType fonts.", copied))
}

// BuildTTFWithFamilyName rewrites the OpenType 'name' table of a TrueType font so WPF
// and DirectWrite identify it as the target Windows font family (e.g. "Segoe UI").
func BuildTTFWithFamilyName(ttf []byte, family, subfamily string) ([]byte, error) {
	if len(ttf) < 12 {
		return nil, fmt.Errorf("TTF too short")
	}
	numTables := int(binary.BigEndian.Uint16(ttf[4:6]))
	if len(ttf) < 12+numTables*16 {
		return nil, fmt.Errorf("TTF table directory truncated")
	}

	nameTableIdx := -1
	for i := 0; i < numTables; i++ {
		recOff := 12 + i*16
		tag := string(ttf[recOff : recOff+4])
		if tag == "name" {
			nameTableIdx = i
			break
		}
	}
	if nameTableIdx < 0 {
		return nil, fmt.Errorf("no name table found")
	}

	fullName := family
	if subfamily != "" && !strings.EqualFold(subfamily, "Regular") {
		fullName = family + " " + subfamily
	}
	postScript := strings.ReplaceAll(fullName, " ", "")

	records := []struct {
		nameID uint16
		val    string
	}{
		{1, family},
		{2, subfamily},
		{3, fullName + ";Linbeam"},
		{4, fullName},
		{6, postScript},
		{16, family},
		{17, subfamily},
	}

	type rawRec struct {
		platformID uint16
		encodingID uint16
		langID     uint16
		nameID     uint16
		data       []byte
	}

	var rawRecs []rawRec
	for _, r := range records {
		rawRecs = append(rawRecs, rawRec{
			platformID: 1,
			encodingID: 0,
			langID:     0,
			nameID:     r.nameID,
			data:       []byte(r.val),
		})
	}
	for _, r := range records {
		u16 := utf16.Encode([]rune(r.val))
		b := make([]byte, len(u16)*2)
		for i, v := range u16 {
			binary.BigEndian.PutUint16(b[i*2:], v)
		}
		rawRecs = append(rawRecs, rawRec{
			platformID: 3,
			encodingID: 1,
			langID:     0x0409,
			nameID:     r.nameID,
			data:       b,
		})
	}

	count := len(rawRecs)
	stringOffset := 6 + count*12
	var strPool []byte
	nameHdr := make([]byte, stringOffset)
	binary.BigEndian.PutUint16(nameHdr[0:2], 0)
	binary.BigEndian.PutUint16(nameHdr[2:4], uint16(count))
	binary.BigEndian.PutUint16(nameHdr[4:6], uint16(stringOffset))

	for i, rr := range rawRecs {
		off := 6 + i*12
		binary.BigEndian.PutUint16(nameHdr[off:off+2], rr.platformID)
		binary.BigEndian.PutUint16(nameHdr[off+2:off+4], rr.encodingID)
		binary.BigEndian.PutUint16(nameHdr[off+4:off+6], rr.langID)
		binary.BigEndian.PutUint16(nameHdr[off+6:off+8], rr.nameID)
		binary.BigEndian.PutUint16(nameHdr[off+8:off+10], uint16(len(rr.data)))
		binary.BigEndian.PutUint16(nameHdr[off+10:off+12], uint16(len(strPool)))
		strPool = append(strPool, rr.data...)
	}

	newNameTable := append(nameHdr, strPool...)
	out := make([]byte, len(ttf))
	copy(out, ttf)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	newOffset := uint32(len(out))
	newLength := uint32(len(newNameTable))

	paddedName := make([]byte, len(newNameTable))
	copy(paddedName, newNameTable)
	for len(paddedName)%4 != 0 {
		paddedName = append(paddedName, 0)
	}

	var sum uint32
	for i := 0; i < len(paddedName); i += 4 {
		sum += binary.BigEndian.Uint32(paddedName[i : i+4])
	}

	out = append(out, paddedName...)
	recOff := 12 + nameTableIdx*16
	binary.BigEndian.PutUint32(out[recOff+4:recOff+8], sum)
	binary.BigEndian.PutUint32(out[recOff+8:recOff+12], newOffset)
	binary.BigEndian.PutUint32(out[recOff+12:recOff+16], newLength)

	return out, nil
}

// SetWindows10 sets the Wine prefix OS version to Windows 10 Pro (Build 19045) in both 64-bit and 32-bit views.
func (e *Engine) SetWindows10(onLine func(string)) error {
	ntKeys := []string{
		`HKLM\Software\Microsoft\Windows NT\CurrentVersion`,
		`HKLM\Software\Wow6432Node\Microsoft\Windows NT\CurrentVersion`,
	}
	for _, ntKey := range ntKeys {
		cmds := [][]string{
			{"reg", "add", ntKey, "/v", "ProductName", "/t", "REG_SZ", "/d", "Windows 10 Pro", "/f"},
			{"reg", "add", ntKey, "/v", "CSDVersion", "/t", "REG_SZ", "/d", "", "/f"},
			{"reg", "add", ntKey, "/v", "CurrentVersion", "/t", "REG_SZ", "/d", "6.3", "/f"},
			{"reg", "add", ntKey, "/v", "CurrentBuild", "/t", "REG_SZ", "/d", "19045", "/f"},
			{"reg", "add", ntKey, "/v", "CurrentBuildNumber", "/t", "REG_SZ", "/d", "19045", "/f"},
			{"reg", "add", ntKey, "/v", "CurrentMajorVersionNumber", "/t", "REG_DWORD", "/d", "10", "/f"},
			{"reg", "add", ntKey, "/v", "CurrentMinorVersionNumber", "/t", "REG_DWORD", "/d", "0", "/f"},
			{"reg", "add", ntKey, "/v", "EditionID", "/t", "REG_SZ", "/d", "Professional", "/f"},
			{"reg", "add", ntKey, "/v", "InstallationType", "/t", "REG_SZ", "/d", "Client", "/f"},
			{"reg", "add", ntKey, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", "22H2", "/f"},
			{"reg", "add", ntKey, "/v", "ReleaseId", "/t", "REG_SZ", "/d", "2009", "/f"},
		}
		for _, args := range cmds {
			_ = e.RunWineStream(onLine, args[0], args[1:]...)
		}
	}

	sysCtrl := `HKLM\System\CurrentControlSet\Control\Windows`
	_ = e.RunWineStream(onLine, "reg", "add", sysCtrl, "/v", "CSDVersion", "/t", "REG_DWORD", "/d", "0", "/f")

	return nil
}

// ApplyBluebeamRegistryOverrides sets PowerShell 5.1, VC++ 14.40, and WebView2 environment registry keys.
func (e *Engine) ApplyBluebeamRegistryOverrides(onLine func(string)) error {
	// 1. Spoof Windows PowerShell 5.1 in both 64-bit and 32-bit registry views
	psRoots := []string{
		`HKLM\Software\Microsoft\PowerShell`,
		`HKLM\Software\Wow6432Node\Microsoft\PowerShell`,
	}
	for _, root := range psRoots {
		cmds := [][]string{
			{"reg", "add", root + `\1`, "/v", "Install", "/t", "REG_DWORD", "/d", "1", "/f"},
			{"reg", "add", root + `\1\PowerShellEngine`, "/v", "PowerShellVersion", "/t", "REG_SZ", "/d", "2.0", "/f"},
			{"reg", "add", root + `\1\PowerShellEngine`, "/v", "RuntimeVersion", "/t", "REG_SZ", "/d", "v2.0.50727", "/f"},
			{"reg", "add", root + `\1\PowerShellEngine`, "/v", "ApplicationBase", "/t", "REG_SZ", "/d", `C:\Windows\System32\WindowsPowerShell\v1.0`, "/f"},
			{"reg", "add", root + `\3`, "/v", "Install", "/t", "REG_DWORD", "/d", "1", "/f"},
			{"reg", "add", root + `\3\PowerShellEngine`, "/v", "PowerShellVersion", "/t", "REG_SZ", "/d", "5.1.19041.1", "/f"},
			{"reg", "add", root + `\3\PowerShellEngine`, "/v", "RuntimeVersion", "/t", "REG_SZ", "/d", "v4.0.30319", "/f"},
			{"reg", "add", root + `\3\PowerShellEngine`, "/v", "CompatibleVersion", "/t", "REG_SZ", "/d", "1.0, 2.0, 3.0, 4.0, 5.0, 5.1", "/f"},
			{"reg", "add", root + `\3\PowerShellEngine`, "/v", "ApplicationBase", "/t", "REG_SZ", "/d", `C:\Windows\System32\WindowsPowerShell\v1.0`, "/f"},
		}
		for _, args := range cmds {
			_ = e.RunWineStream(onLine, args[0], args[1:]...)
		}
	}

	// 2. Spoof Visual C++ 2015-2022 Runtime v14.40.33810.00 in both 64-bit and Wow6432Node views
	vcKeys := []string{
		`HKLM\SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\X64`,
		`HKLM\SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64`,
		`HKLM\SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\X86`,
		`HKLM\SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x86`,
		`HKLM\SOFTWARE\Wow6432Node\Microsoft\VisualStudio\14.0\VC\Runtimes\X64`,
		`HKLM\SOFTWARE\Wow6432Node\Microsoft\VisualStudio\14.0\VC\Runtimes\x64`,
		`HKLM\SOFTWARE\Wow6432Node\Microsoft\VisualStudio\14.0\VC\Runtimes\X86`,
		`HKLM\SOFTWARE\Wow6432Node\Microsoft\VisualStudio\14.0\VC\Runtimes\x86`,
	}
	for _, key := range vcKeys {
		cmds := [][]string{
			{"reg", "add", key, "/v", "Version", "/t", "REG_SZ", "/d", "v14.40.33810.00", "/f"},
			{"reg", "add", key, "/v", "Installed", "/t", "REG_DWORD", "/d", "1", "/f"},
			{"reg", "add", key, "/v", "Major", "/t", "REG_DWORD", "/d", "14", "/f"},
			{"reg", "add", key, "/v", "Minor", "/t", "REG_DWORD", "/d", "40", "/f"},
			{"reg", "add", key, "/v", "Bld", "/t", "REG_DWORD", "/d", "33810", "/f"},
			{"reg", "add", key, "/v", "Rbld", "/t", "REG_DWORD", "/d", "0", "/f"},
		}
		for _, args := range cmds {
			_ = e.RunWineStream(onLine, args[0], args[1:]...)
		}
	}

	// 3. Configure WebView2 sandbox & GPU flags in Wine environment registry
	envKeys := []string{
		`HKCU\Environment`,
		`HKLM\System\CurrentControlSet\Control\Session Manager\Environment`,
	}
	for _, key := range envKeys {
		_ = e.RunWineStream(onLine, "reg", "add", key, "/v", "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "/t", "REG_SZ", "/d", "--no-sandbox --disable-gpu", "/f")
	}

	// 4. Enable ClearType / subpixel font smoothing for sharp PDF text rendering
	desktopKey := `HKCU\Control Panel\Desktop`
	_ = e.RunWineStream(onLine, "reg", "add", desktopKey, "/v", "FontSmoothing", "/t", "REG_SZ", "/d", "2", "/f")
	_ = e.RunWineStream(onLine, "reg", "add", desktopKey, "/v", "FontSmoothingType", "/t", "REG_DWORD", "/d", "2", "/f")
	_ = e.RunWineStream(onLine, "reg", "add", desktopKey, "/v", "FontSmoothingGamma", "/t", "REG_DWORD", "/d", "1400", "/f")

	return nil
}

// ApplyDarkTheme sets dark mode and dark title bars in the Wine prefix.
func (e *Engine) ApplyDarkTheme(onLine func(string)) error {
	personalize := `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	_ = e.RunWineStream(onLine, "reg", "add", personalize, "/v", "AppsUseLightTheme", "/t", "REG_DWORD", "/d", "0", "/f")
	_ = e.RunWineStream(onLine, "reg", "add", personalize, "/v", "SystemUsesLightTheme", "/t", "REG_DWORD", "/d", "0", "/f")
	return nil
}

// RunWine executes a program inside the Wine prefix and streams output to stdout/stderr.
func (e *Engine) RunWine(exePath string, args ...string) error {
	runner, err := e.GetActiveRunner()
	if err != nil {
		return err
	}

	cmdArgs := append([]string{exePath}, args...)
	cmd := exec.Command(runner.WineBin, cmdArgs...)
	cmd.Env = e.getEnv(runner)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// RunWineStream executes a program inside the Wine prefix and passes each output line to onLine.
func (e *Engine) RunWineStream(onLine func(string), exePath string, args ...string) error {
	runner, err := e.GetActiveRunner()
	if err != nil {
		return err
	}

	cmdArgs := append([]string{exePath}, args...)
	cmd := exec.Command(runner.WineBin, cmdArgs...)
	cmd.Env = e.getEnv(runner)
	return runCommandStream(cmd, onLine)
}

// EnsureWinetricks finds system winetricks or downloads a standalone copy into CacheDir.
func (e *Engine) EnsureWinetricks(onLine func(string)) (string, error) {
	if path, err := exec.LookPath("winetricks"); err == nil {
		return path, nil
	}

	cached := filepath.Join(e.Config.CacheDir, "winetricks")
	if fileExists(cached) {
		_ = os.Chmod(cached, 0755)
		return cached, nil
	}

	if onLine != nil {
		onLine("System winetricks not found; downloading standalone winetricks script...")
	}

	if err := os.MkdirAll(e.Config.CacheDir, 0755); err != nil {
		return "", err
	}

	err := downloadFileWithProgress(
		"https://raw.githubusercontent.com/Winetricks/winetricks/master/src/winetricks",
		cached,
		0,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to download winetricks: %w", err)
	}

	if err := os.Chmod(cached, 0755); err != nil {
		return "", err
	}
	return cached, nil
}

// RunWinetricks executes winetricks verbs against the Proton-GE prefix.
func (e *Engine) RunWinetricks(onLine func(string), verbs ...string) error {
	runner, err := e.GetActiveRunner()
	if err != nil {
		return err
	}

	wtBin, err := e.EnsureWinetricks(onLine)
	if err != nil {
		return err
	}

	args := append([]string{"-q"}, verbs...)
	cmd := exec.Command(wtBin, args...)
	cmd.Env = e.getEnv(runner)
	return runCommandStream(cmd, onLine)
}

// LaunchWinecfg opens winecfg in the Proton-GE prefix.
func (e *Engine) LaunchWinecfg() error {
	return e.RunWine("winecfg")
}

// KillServer terminates all Wine processes in the prefix.
func (e *Engine) KillServer() error {
	runner, err := e.GetActiveRunner()
	if err != nil {
		return err
	}
	if runner.ServerBin == "" {
		return nil
	}
	cmd := exec.Command(runner.ServerBin, "-k")
	cmd.Env = e.getEnv(runner)
	return cmd.Run()
}

// WaitServer waits for active Wine processes in the prefix to exit cleanly.
func (e *Engine) WaitServer() error {
	runner, err := e.GetActiveRunner()
	if err != nil {
		return err
	}
	if runner.ServerBin == "" {
		return nil
	}
	cmd := exec.Command(runner.ServerBin, "-w")
	cmd.Env = e.getEnv(runner)
	return cmd.Run()
}

func runCommandStream(cmd *exec.Cmd, onLine func(string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			// Filter noisy Wine fixme/err spam while preserving installer and msiexec messages
			if strings.Contains(line, ":fixme:") || strings.Contains(line, "wine: Read access denied") {
				continue
			}
			if onLine != nil {
				onLine(line)
			}
		}
	}

	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	wg.Wait()

	return cmd.Wait()
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
