package installer

import (
	"archive/zip"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/PanickyNarwhal/linbeam/pkg/config"
	"github.com/PanickyNarwhal/linbeam/pkg/proton"
)

//go:embed msi_patcher.cs
var embeddedMsiPatcherCS []byte

const (
	// BluebeamMSIDeployURL is the official Bluebeam x64 MSI deployment package endpoint.
	BluebeamMSIDeployURL = "https://bluebeam.com/MSIdeployx64"
	vcRedistX64URL       = "https://aka.ms/vs/17/release/vc_redist.x64.exe"
	vcRedistX86URL       = "https://aka.ms/vs/17/release/vc_redist.x86.exe"
	browserUserAgent     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
)

// BluebeamManager handles downloading, patching, installing, configuring, and launching Bluebeam Revu 21.
type BluebeamManager struct {
	Config *config.Config
	Engine *proton.Engine
}

func NewBluebeamManager(cfg *config.Config, engine *proton.Engine) *BluebeamManager {
	return &BluebeamManager{
		Config: cfg,
		Engine: engine,
	}
}

// DownloadAndExtractDeploymentZip downloads the official 64-bit MSI deployment package
// from https://bluebeam.com/MSIdeployx64, unpacks the archive into ~/.local/share/linbeam/payloads,
// and auto-selects Bluebeam Revu x64 21.msi, BluebeamOCR x64 21.msi, and VC++ redistributables.
func (bm *BluebeamManager) DownloadAndExtractDeploymentZip(progress func(stage string, percent int)) (*config.PayloadInfo, error) {
	emit := func(stage string, pct int) {
		if progress != nil {
			progress(stage, pct)
		}
	}

	if err := bm.Config.EnsureDirs(); err != nil {
		return nil, err
	}

	destDir := bm.Config.ManagedPayloadsDir()
	zipPath := filepath.Join(bm.Config.CacheDir, "MSIBluebeamRevu21x64.zip")

	emit("Connecting to https://bluebeam.com/MSIdeployx64...", 2)

	req, err := http.NewRequest("GET", BluebeamMSIDeployURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "*/*")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Bluebeam download server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bluebeam download server returned HTTP %d", resp.StatusCode)
	}

	totalBytes := resp.ContentLength
	out, err := os.Create(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary ZIP file: %w", err)
	}

	buf := make([]byte, 128*1024)
	var downloaded int64
	lastPct := -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				_ = out.Close()
				return nil, writeErr
			}
			downloaded += int64(n)
			if totalBytes > 0 {
				pct := int(downloaded * 100 / totalBytes)
				if pct != lastPct {
					lastPct = pct
					scaled := 4 + (pct * 74 / 100)
					emit(fmt.Sprintf(
						"Downloading Bluebeam x64 MSI Deployment ZIP (%d MB / %d MB)...",
						downloaded/(1024*1024),
						totalBytes/(1024*1024),
					), scaled)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = out.Close()
			return nil, fmt.Errorf("error reading deployment stream: %w", readErr)
		}
	}
	if err := out.Close(); err != nil {
		return nil, err
	}

	emit("Unpacking Bluebeam MSI deployment archive...", 80)
	if err := extractDeploymentZip(zipPath, destDir, func(fileName string, idx, total int) {
		scaled := 80 + (idx * 18 / maxInt(total, 1))
		emit(fmt.Sprintf("Extracting %s (%d/%d)...", fileName, idx, total), scaled)
	}); err != nil {
		return nil, fmt.Errorf("failed to unpack Bluebeam deployment ZIP: %w", err)
	}

	// Remove the 2.5 GB ZIP archive after extraction
	_ = os.Remove(zipPath)

	info := bm.Config.AutoDetectPayloads()
	_ = bm.Config.Save()

	if info.RevuMSI == "" {
		return &info, fmt.Errorf("archive extracted to %s, but Bluebeam Revu MSI was not found inside", destDir)
	}

	emit(fmt.Sprintf("Selected %s and %s", filepath.Base(info.RevuMSI), filepath.Base(info.OCRMSI)), 100)
	return &info, nil
}

func extractDeploymentZip(zipPath, destDir string, onFile func(name string, idx, total int)) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	var files []*zip.File
	for _, f := range r.File {
		if !f.FileInfo().IsDir() {
			files = append(files, f)
		}
	}

	for i, f := range files {
		baseName := filepath.Base(strings.ReplaceAll(f.Name, `\`, `/`))
		if baseName == "" || baseName == "." {
			continue
		}
		if onFile != nil {
			onFile(baseName, i+1, len(files))
		}

		targetPath := filepath.Join(destDir, baseName)
		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			_ = rc.Close()
			return err
		}

		_, copyErr := io.Copy(outFile, rc)
		_ = outFile.Close()
		_ = rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}

	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// FindRevuExecutable searches the Proton-GE prefix for Revu.exe.
func (bm *BluebeamManager) FindRevuExecutable() (string, bool) {
	if bm.Config.RevuExePath != "" && fileExists(bm.Config.RevuExePath) {
		return bm.Config.RevuExePath, true
	}

	driveC := bm.Engine.DriveCPath()
	if !dirExists(driveC) {
		return "", false
	}

	knownPaths := []string{
		filepath.Join(driveC, "Program Files", "Bluebeam Software", "Bluebeam Revu", "21", "Revu", "Revu.exe"),
		filepath.Join(driveC, "Program Files", "Bluebeam Software", "Bluebeam Revu", "20", "Revu", "Revu.exe"),
	}

	for _, p := range knownPaths {
		if fileExists(p) {
			return p, true
		}
	}

	bbRoot := filepath.Join(driveC, "Program Files", "Bluebeam Software")
	if !dirExists(bbRoot) {
		return "", false
	}

	var foundPath string
	_ = filepath.WalkDir(bbRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "Revu.exe") {
			foundPath = path
			return filepath.SkipAll
		}
		return nil
	})

	if foundPath != "" {
		return foundPath, true
	}
	return "", false
}

// HasDotNet48 checks whether .NET Framework 4.x (with csc.exe) is installed in the prefix.
func (bm *BluebeamManager) HasDotNet48() bool {
	driveC := bm.Engine.DriveCPath()
	csc64 := filepath.Join(driveC, "windows", "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe")
	mscorlib64 := filepath.Join(driveC, "windows", "Microsoft.NET", "Framework64", "v4.0.30319", "mscorlib.dll")
	if !fileExists(csc64) || !fileExists(mscorlib64) {
		return false
	}
	if st, err := os.Stat(mscorlib64); err == nil && st.Size() > 3*1024*1024 {
		return true
	}
	return false
}

// InstallPrerequisites initializes the Proton-GE prefix, installs VC++ 2015-2022 (x64 & x86),
// and installs .NET Framework 4.8.
func (bm *BluebeamManager) InstallPrerequisites(progress func(stage string, percent int)) error {
	emit := func(stage string, pct int) {
		if progress != nil {
			progress(stage, pct)
		}
	}

	emit("Step 1/4: Initializing Proton-GE (amd64) prefix, DXVK & WPF fonts...", 5)
	if err := bm.Engine.InitPrefixStream(func(line string) {
		emit("[Proton] "+line, 10)
	}); err != nil {
		return fmt.Errorf("prefix initialization failed: %w", err)
	}

	payloads := bm.Config.DiscoverPayloads()

	// Install VC++ Redistributables (x64 and x86)
	emit("Step 2/4: Installing Visual C++ 2015-2022 Redistributables...", 18)
	vcX64 := payloads.VCRedistX64
	if vcX64 == "" || !fileExists(vcX64) {
		vcX64 = filepath.Join(bm.Config.CacheDir, "vc_redist.x64.exe")
		if !fileExists(vcX64) {
			emit("Downloading vc_redist.x64.exe from Microsoft...", 20)
			if err := downloadSimple(vcRedistX64URL, vcX64); err != nil {
				emit(fmt.Sprintf("Warning: could not download vc_redist.x64.exe: %v", err), 21)
			}
		}
	}
	if fileExists(vcX64) {
		emit("Running vc_redist.x64.exe /install /quiet /norestart...", 22)
		_ = bm.Engine.RunWineStream(func(line string) {
			emit("[VC++ x64] "+line, 24)
		}, vcX64, "/install", "/quiet", "/norestart")
	}

	vcX86 := payloads.VCRedistX86
	if vcX86 == "" || !fileExists(vcX86) {
		vcX86 = filepath.Join(bm.Config.CacheDir, "vc_redist.x86.exe")
		if !fileExists(vcX86) {
			emit("Downloading vc_redist.x86.exe from Microsoft...", 26)
			if err := downloadSimple(vcRedistX86URL, vcX86); err != nil {
				emit(fmt.Sprintf("Warning: could not download vc_redist.x86.exe: %v", err), 27)
			}
		}
	}
	if fileExists(vcX86) {
		emit("Running vc_redist.x86.exe /install /quiet /norestart...", 28)
		_ = bm.Engine.RunWineStream(func(line string) {
			emit("[VC++ x86] "+line, 30)
		}, vcX86, "/install", "/quiet", "/norestart")
	}

	// Install .NET Framework 4.8 if not already installed
	if bm.HasDotNet48() {
		emit("Step 3/4: Microsoft .NET Framework 4.8 already installed in prefix.", 42)
	} else {
		emit("Step 3/4: Installing Microsoft .NET Framework 4.8 via winetricks (this takes 3-5 minutes)...", 32)
		if err := bm.Engine.RunWinetricks(func(line string) {
			emit("[dotnet48] "+line, 38)
		}, "dotnet48"); err != nil {
			emit(fmt.Sprintf("Note: winetricks dotnet48 exited with: %v (verifying installation...)", err), 41)
		}
		// Fallback to bundled ndp48-x86-x64-allos-enu.exe if extracted from Bluebeam ZIP
		if !bm.HasDotNet48() && payloads.DotNet48Exe != "" && fileExists(payloads.DotNet48Exe) {
			emit("Running bundled ndp48-x86-x64-allos-enu.exe offline installer...", 41)
			_ = bm.Engine.RunWineStream(func(line string) {
				emit("[ndp48] "+line, 42)
			}, payloads.DotNet48Exe, "/q", "/norestart")
			_ = bm.Engine.WaitServer()
		}
	}

	// Re-apply Windows 10 (19045) + Bluebeam registry overrides because dotnet48 resets OS version to Win7
	emit("Step 4/4: Re-applying Windows 10 (Build 19045) & Bluebeam registry overrides...", 44)
	_ = bm.Engine.SetWindows10(nil)
	_ = bm.Engine.ApplyBluebeamRegistryOverrides(nil)
	_ = bm.Engine.ApplyDarkTheme(nil)
	_ = bm.Engine.WaitServer()

	emit("Prerequisites ready.", 48)
	return nil
}

// CompileAndPatchMSI compiles the embedded msi_patcher.cs inside the Proton-GE prefix
// and patches a copy of the target MSI to remove failing InstallShield PowerShell custom actions.
func (bm *BluebeamManager) CompileAndPatchMSI(srcMsiPath, extractDir string, emit func(string)) (string, error) {
	if !fileExists(srcMsiPath) {
		return "", fmt.Errorf("MSI file not found: %s", srcMsiPath)
	}

	driveC := bm.Engine.DriveCPath()
	winTempDir := filepath.Join(driveC, "windows", "temp")
	if err := os.MkdirAll(winTempDir, 0755); err != nil {
		return "", err
	}
	if extractDir != "" {
		_ = os.MkdirAll(extractDir, 0755)
	}

	patchedBase := "patched_" + strings.ReplaceAll(filepath.Base(srcMsiPath), " ", "_")
	patchedLinuxPath := filepath.Join(winTempDir, patchedBase)
	emit(fmt.Sprintf("Copying %s to %s for Wine MSI patching...", filepath.Base(srcMsiPath), patchedBase))
	if err := copyFile(srcMsiPath, patchedLinuxPath); err != nil {
		return "", fmt.Errorf("failed to copy MSI for patching: %w", err)
	}

	csLinuxPath := filepath.Join(winTempDir, "msi_patcher.cs")
	exeLinuxPath := filepath.Join(winTempDir, "msi_patcher.exe")
	if err := os.WriteFile(csLinuxPath, embeddedMsiPatcherCS, 0644); err != nil {
		return "", fmt.Errorf("failed to write msi_patcher.cs: %w", err)
	}

	if !fileExists(exeLinuxPath) {
		cscCandidates := []string{
			filepath.Join(driveC, "windows", "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe"),
			filepath.Join(driveC, "windows", "Microsoft.NET", "Framework", "v4.0.30319", "csc.exe"),
		}
		var cscPath string
		for _, c := range cscCandidates {
			if fileExists(c) {
				cscPath = c
				break
			}
		}
		if cscPath == "" {
			return "", fmt.Errorf("csc.exe not found in prefix; ensure .NET Framework 4.8 is installed")
		}

		emit("Compiling embedded msi_patcher.cs with .NET Framework csc.exe...")
		err := bm.Engine.RunWineStream(emit, cscPath,
			"/nologo",
			`/out:C:\windows\temp\msi_patcher.exe`,
			`C:\windows\temp\msi_patcher.cs`,
		)
		if err != nil || !fileExists(exeLinuxPath) {
			return "", fmt.Errorf("failed to compile msi_patcher.cs: %w", err)
		}
	}

	patchedWinePath := `C:\windows\temp\` + patchedBase
	extractWinePath := `C:\windows\temp\bb_setup_files`
	emit(fmt.Sprintf("Patching MSI tables in %s...", patchedBase))
	if err := bm.Engine.RunWineStream(emit, `C:\windows\temp\msi_patcher.exe`, patchedWinePath, extractWinePath); err != nil {
		return "", fmt.Errorf("msi_patcher.exe failed on %s: %w", patchedBase, err)
	}

	return patchedLinuxPath, nil
}

// InstallFull runs Deployment Download (if needed) -> Prerequisites -> MSI Patching -> OCR & Revu 21 MSI Install -> WebView2 & Safe.enx Post-Install.
func (bm *BluebeamManager) InstallFull(revuMsiPath, ocrMsiPath, bpxProfilePath string, progress func(stage string, percent int)) error {
	emit := func(stage string, pct int) {
		if progress != nil {
			progress(stage, pct)
		}
	}

	payloads := bm.Config.DiscoverPayloads()
	if revuMsiPath == "" {
		revuMsiPath = payloads.RevuMSI
	}
	if ocrMsiPath == "" {
		ocrMsiPath = payloads.OCRMSI
	}
	if bpxProfilePath == "" {
		bpxProfilePath = payloads.BPXProfile
	}

	// Automatically download and unpack from https://bluebeam.com/MSIdeployx64 if MSI is not present locally
	if revuMsiPath == "" || !fileExists(revuMsiPath) {
		emit("Local Bluebeam MSI not found; downloading official package from https://bluebeam.com/MSIdeployx64...", 2)
		dlInfo, err := bm.DownloadAndExtractDeploymentZip(func(stage string, pct int) {
			emit(stage, pct/3)
		})
		if err != nil {
			return fmt.Errorf("failed to download Bluebeam deployment package: %w", err)
		}
		revuMsiPath = dlInfo.RevuMSI
		if ocrMsiPath == "" {
			ocrMsiPath = dlInfo.OCRMSI
		}
	}

	if err := bm.InstallPrerequisites(progress); err != nil {
		return err
	}

	driveC := bm.Engine.DriveCPath()
	extractDir := filepath.Join(driveC, "windows", "temp", "bb_setup_files")
	_ = os.MkdirAll(extractDir, 0755)

	// Optional: Patch and install BluebeamOCR x64 21.msi if present
	if ocrMsiPath != "" && fileExists(ocrMsiPath) {
		emit("Patching BluebeamOCR x64 21.msi...", 50)
		patchedOCR, err := bm.CompileAndPatchMSI(ocrMsiPath, "", func(line string) {
			emit("[MSI Patcher] "+line, 52)
		})
		if err != nil {
			emit(fmt.Sprintf("Warning patching OCR MSI: %v (attempting direct install)", err), 53)
			patchedOCR = ocrMsiPath
		}

		ocrWinePath := bm.Engine.UnixToWinePath(patchedOCR)
		emit("Installing BluebeamOCR x64 21.msi...", 55)
		_ = bm.Engine.RunWineStream(func(line string) {
			emit("[OCR msiexec] "+line, 58)
		}, "msiexec", "/i", ocrWinePath, "/qn", "/norestart",
			`Transforms=""`,
			"IGNORE_VCREDIST=1",
			"IGNORE_RBT=1",
			"REBOOT=ReallySuppress",
		)
		_ = bm.Engine.WaitServer()
		if patchedOCR != ocrMsiPath {
			_ = os.Remove(patchedOCR)
		}
	}

	// Patch and install Bluebeam Revu x64 21.msi
	emit("Patching Bluebeam Revu x64 21.msi & extracting ISSetupFile streams...", 62)
	patchedRevu, err := bm.CompileAndPatchMSI(revuMsiPath, extractDir, func(line string) {
		emit("[MSI Patcher] "+line, 65)
	})
	if err != nil {
		return fmt.Errorf("failed to patch Bluebeam Revu 21 MSI: %w", err)
	}

	revuWinePath := bm.Engine.UnixToWinePath(patchedRevu)
	emit("Running msiexec for Bluebeam Revu x64 21.msi (unpacking 1.4 GB of components)...", 68)
	msiErr := bm.Engine.RunWineStream(func(line string) {
		emit("[Revu msiexec] "+line, 74)
	}, "msiexec", "/i", revuWinePath, "/qn", "/norestart",
		`Transforms=""`,
		"IGNORE_VCREDIST=1",
		"IGNORE_RBT=1",
		"REBOOT_NEEDED=0",
		"LANGUAGE_CHOICE=en",
		"LAUNCH_BB_ADMIN=0",
		"BB_AUTO_UPDATE=0",
		"DISABLEANALYTICS=1",
		"DESKTOP_SHORTCUTS=0",
		"REBOOT=ReallySuppress",
		`/L*v`, `C:\windows\temp\revu_install.log`,
	)
	_ = bm.Engine.WaitServer()

	_ = os.Remove(patchedRevu)

	exePath, found := bm.FindRevuExecutable()
	if !found {
		diag := bm.readMsiLogFailure()
		if msiErr != nil {
			return fmt.Errorf("msiexec failed (%v). %s", msiErr, diag)
		}
		return fmt.Errorf("msiexec finished, but Revu.exe was not found in prefix. %s", diag)
	}

	emit("Configuring WebView2 FixedVersionRuntime, Safe.enx license file & profile...", 84)
	bm.ConfigurePostInstall(filepath.Dir(exePath), extractDir, bpxProfilePath, func(line string) {
		emit("[Post-Install] "+line, 90)
	})

	bm.Config.RevuInstalled = true
	bm.Config.RevuExePath = exePath
	bm.Config.FirstRunComplete = true
	_ = bm.Config.Save()

	emit("Bluebeam Revu 21 installed and configured.", 100)
	return nil
}

// ConfigurePostInstall unpacks the bundled WebView2 FixedVersionRuntime, runs Bluebeam Admin Install.exe
// to generate Safe.enx, and imports any .bpx custom profile.
func (bm *BluebeamManager) ConfigurePostInstall(revuDir, extractDir, bpxProfilePath string, emit func(string)) {
	driveC := bm.Engine.DriveCPath()

	wv2TargetDir := filepath.Join(revuDir, "WebView2Runtime")
	wv2Exe := filepath.Join(wv2TargetDir, "msedgewebview2.exe")
	if !fileExists(wv2Exe) {
		archive7z, sevenZipExe := bm.findExtractedWebView2Archive()
		if archive7z != "" {
			emit(fmt.Sprintf("Found bundled WebView2 archive: %s", filepath.Base(archive7z)))
			_ = os.MkdirAll(wv2TargetDir, 0755)

			extracted := false
			if host7z, err := exec.LookPath("7z"); err == nil {
				emit("Extracting WebView2 FixedVersionRuntime using host 7z...")
				cmd := exec.Command(host7z, "x", archive7z, "-o"+wv2TargetDir, "-y")
				if err := cmd.Run(); err == nil {
					extracted = true
				}
			} else if host7za, err := exec.LookPath("7za"); err == nil {
				emit("Extracting WebView2 FixedVersionRuntime using host 7za...")
				cmd := exec.Command(host7za, "x", archive7z, "-o"+wv2TargetDir, "-y")
				if err := cmd.Run(); err == nil {
					extracted = true
				}
			}

			if !extracted && sevenZipExe != "" && fileExists(sevenZipExe) {
				emit("Extracting WebView2 FixedVersionRuntime using MSI's bundled 7z.exe in Proton-GE...")
				_ = bm.Engine.RunWineStream(emit,
					bm.Engine.UnixToWinePath(sevenZipExe),
					"x",
					bm.Engine.UnixToWinePath(archive7z),
					"-o"+bm.Engine.UnixToWinePath(wv2TargetDir),
					"-y",
				)
			}

			flattenSingleSubdir(wv2TargetDir, emit)
			_ = os.Remove(archive7z)
		} else {
			emit("Note: Bundled WebView2 .7z archive not found in Temp; skipping extraction.")
		}
	} else {
		emit("WebView2 FixedVersionRuntime is already unpacked in Revu\\WebView2Runtime.")
	}

	adminExeSrc := filepath.Join(extractDir, "Bluebeam Admin Install.exe")
	adminCfgSrc := filepath.Join(extractDir, "Bluebeam Admin Install.exe.config")
	if fileExists(adminExeSrc) {
		adminExeDst := filepath.Join(revuDir, "Bluebeam Admin Install.exe")
		adminCfgDst := filepath.Join(revuDir, "Bluebeam Admin Install.exe.config")
		_ = copyFile(adminExeSrc, adminExeDst)
		if fileExists(adminCfgSrc) {
			_ = copyFile(adminCfgSrc, adminCfgDst)
		}

		emit("Running Bluebeam Admin Install.exe to initialize Safe.enx & default settings...")
		_ = bm.Engine.RunWineStream(emit,
			bm.Engine.UnixToWinePath(adminExeDst),
			"/YEAR:21",
			"/DPS:Letter",
			"/FEATURES:65535",
			"/DISABLEANALYTICS",
			"/NODESKREVU",
		)
		_ = bm.Engine.WaitServer()
	}

	safeEnx := filepath.Join(driveC, "ProgramData", "Bluebeam Software", "Bluebeam Revu", "21", "Revu", "Safe.enx")
	if fileExists(safeEnx) {
		emit("Verified Safe.enx license configuration file in ProgramData.")
	}

	if bpxProfilePath != "" && fileExists(bpxProfilePath) {
		bpxName := filepath.Base(bpxProfilePath)
		progDataDir := filepath.Join(driveC, "ProgramData", "Bluebeam Software", "Bluebeam Revu", "21", "Revu")
		_ = os.MkdirAll(progDataDir, 0755)
		_ = copyFile(bpxProfilePath, filepath.Join(progDataDir, bpxName))

		usersDir := filepath.Join(driveC, "users")
		if entries, err := os.ReadDir(usersDir); err == nil {
			for _, u := range entries {
				if !u.IsDir() || strings.EqualFold(u.Name(), "Public") {
					continue
				}
				roamingDir := filepath.Join(usersDir, u.Name(), "AppData", "Roaming", "Bluebeam Software", "Revu", "21")
				_ = os.MkdirAll(roamingDir, 0755)
				_ = copyFile(bpxProfilePath, filepath.Join(roamingDir, bpxName))
			}
		}
		emit(fmt.Sprintf("Imported Bluebeam profile: %s", bpxName))
	}
}

func (bm *BluebeamManager) findExtractedWebView2Archive() (archivePath string, sevenZipExe string) {
	driveC := bm.Engine.DriveCPath()
	var candidateDirs []string

	candidateDirs = append(candidateDirs, filepath.Join(driveC, "windows", "temp"))
	usersDir := filepath.Join(driveC, "users")
	if entries, err := os.ReadDir(usersDir); err == nil {
		for _, u := range entries {
			if u.IsDir() {
				candidateDirs = append(candidateDirs,
					filepath.Join(usersDir, u.Name(), "Temp"),
					filepath.Join(usersDir, u.Name(), "AppData", "Local", "Temp"),
				)
			}
		}
	}

	for _, dir := range candidateDirs {
		arc := filepath.Join(dir, "Microsoft.WebView2.FixedVersionRuntime.x64.7z")
		sz := filepath.Join(dir, "7z.exe")
		if fileExists(arc) {
			if fileExists(sz) {
				return arc, sz
			}
			return arc, ""
		}
	}
	return "", ""
}

func flattenSingleSubdir(targetDir string, emit func(string)) {
	entries, err := os.ReadDir(targetDir)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return
	}
	nestedDir := filepath.Join(targetDir, entries[0].Name())
	subEntries, err := os.ReadDir(nestedDir)
	if err != nil {
		return
	}

	for _, se := range subEntries {
		oldPath := filepath.Join(nestedDir, se.Name())
		newPath := filepath.Join(targetDir, se.Name())
		_ = os.Rename(oldPath, newPath)
	}
	_ = os.RemoveAll(nestedDir)
	if emit != nil {
		emit("Flattened WebView2FixedVersionRuntime directory structure.")
	}
}

func (bm *BluebeamManager) readMsiLogFailure() string {
	logPath := filepath.Join(bm.Engine.DriveCPath(), "windows", "temp", "revu_install.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return "No revu_install.log found."
	}
	cleaned := strings.ReplaceAll(string(data), "\x00", "")
	lines := strings.Split(cleaned, "\n")
	var contextLines []string
	for i, line := range lines {
		if strings.Contains(line, "Return value 3") || strings.Contains(line, "Error 1603") {
			start := i - 4
			if start < 0 {
				start = 0
			}
			end := i + 2
			if end > len(lines) {
				end = len(lines)
			}
			for _, l := range lines[start:end] {
				contextLines = append(contextLines, strings.TrimSpace(l))
			}
			break
		}
	}
	if len(contextLines) > 0 {
		return "MSI Log Failure Context: " + strings.Join(contextLines, " | ")
	}
	return "Check C:\\windows\\temp\\revu_install.log inside the prefix for details."
}

// Launch starts Bluebeam Revu 21 inside the Proton-GE prefix, converting any Linux file arguments to Wine paths.
func (bm *BluebeamManager) Launch(args ...string) error {
	exePath, ok := bm.FindRevuExecutable()
	if !ok {
		return fmt.Errorf("Bluebeam Revu 21 executable not found in prefix. Run 'linbeam setup' first")
	}

	if !bm.Config.RevuInstalled || bm.Config.RevuExePath != exePath {
		bm.Config.RevuInstalled = true
		bm.Config.RevuExePath = exePath
		_ = bm.Config.Save()
	}

	var wineArgs []string
	for _, a := range args {
		if a == "" {
			continue
		}
		if strings.HasPrefix(a, "/") && fileExists(a) {
			wineArgs = append(wineArgs, bm.Engine.UnixToWinePath(a))
		} else {
			wineArgs = append(wineArgs, a)
		}
	}

	return bm.Engine.RunWine(exePath, wineArgs...)
}

func downloadSimple(url, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(resp.Body)
	return err
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
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Close()
}
