package updater

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/PanickyNarwhal/linbeam/pkg/config"
	"github.com/PanickyNarwhal/linbeam/pkg/desktop"
	"github.com/PanickyNarwhal/linbeam/pkg/installer"
	"github.com/PanickyNarwhal/linbeam/pkg/proton"
)

const (
	githubLatestReleaseAPI   = "https://api.github.com/repos/PanickyNarwhal/linbeam/releases/latest"
	githubRawBinaryRootURL   = "https://github.com/PanickyNarwhal/linbeam/raw/main/linbeam"
	githubRawBinaryLegacyURL = "https://github.com/PanickyNarwhal/linbeam/raw/main/linbeam-go/linbeam"
	userAgent                = "Linbeam-Updater/1.0"
)

// UpdateResult summarizes what was updated during an UpdateAll run.
type UpdateResult struct {
	LauncherUpdated bool   `json:"launcher_updated"`
	GitUpdated      bool   `json:"git_updated"`
	RunnerName      string `json:"runner_name"`
	ShortcutUpdated bool   `json:"shortcut_updated"`
	PrefixUpdated   bool   `json:"prefix_updated"`
}

// Manager coordinates self-updating the Linbeam binary, Proton-GE runner, and prefix configuration.
type Manager struct {
	Config   *config.Config
	Engine   *proton.Engine
	Bluebeam *installer.BluebeamManager
}

func NewManager(cfg *config.Config, engine *proton.Engine, bm *installer.BluebeamManager) *Manager {
	return &Manager{
		Config:   cfg,
		Engine:   engine,
		Bluebeam: bm,
	}
}

// UpdateAll pulls the latest Linbeam launcher (via Git and/or GitHub binary download),
// updates Proton-GE (amd64) to the latest release, refreshes prefix registry/post-install
// configuration, and updates the Linux desktop launcher & icon.
func (m *Manager) UpdateAll(updateBluebeamMSI bool, progress func(stage string, percent int)) (*UpdateResult, error) {
	emit := func(stage string, pct int) {
		if progress != nil {
			progress(stage, pct)
		}
	}

	res := &UpdateResult{}

	// 1. Update Launcher Binary & Git Repository (if running inside a git clone)
	emit("Step 1/4: Checking for latest Linbeam launcher update...", 5)
	gitUpdated, binUpdated, err := m.UpdateLauncher(func(msg string) {
		emit("[Launcher] "+msg, 15)
	})
	if err != nil {
		emit(fmt.Sprintf("Launcher binary check note: %v (continuing with component updates)", err), 20)
	} else {
		res.GitUpdated = gitUpdated
		res.LauncherUpdated = binUpdated
	}

	// 2. Update Proton-GE (amd64) Runner
	emit("Step 2/4: Checking for latest GE-Proton (amd64) release...", 25)
	runner, err := m.Engine.DownloadLatestGEProton(func(stage string, pct int) {
		scaled := 25 + (pct * 40 / 100)
		emit("[Proton-GE] "+stage, scaled)
	})
	if err != nil {
		emit(fmt.Sprintf("Warning updating Proton-GE: %v", err), 65)
	} else if runner != nil {
		res.RunnerName = runner.Name
	}

	// 3. Optional: Pull latest Bluebeam Revu 21 MSI deployment package and re-apply if requested
	if updateBluebeamMSI {
		emit("Step 3/4: Pulling latest Bluebeam Revu 21 MSI package from bluebeam.com/MSIdeployx64...", 68)
		dlInfo, err := m.Bluebeam.DownloadAndExtractDeploymentZip(func(stage string, pct int) {
			scaled := 68 + (pct * 15 / 100)
			emit("[Bluebeam ZIP] "+stage, scaled)
		})
		if err != nil {
			return res, fmt.Errorf("failed to update Bluebeam MSI package: %w", err)
		}
		emit("Installing updated Bluebeam Revu 21 MSI...", 84)
		if err := m.Bluebeam.InstallFull(dlInfo.RevuMSI, dlInfo.OCRMSI, dlInfo.BPXProfile, func(stage string, pct int) {
			scaled := 84 + (pct * 10 / 100)
			emit(stage, scaled)
		}); err != nil {
			return res, err
		}
		res.PrefixUpdated = true
	} else {
		// Refresh existing prefix registry overrides & post-install if prefix already exists
		emit("Step 3/4: Refreshing Proton-GE prefix registry & WebView2 configuration...", 75)
		if dirExists(m.Engine.DriveCPath()) {
			_ = m.Engine.SetWindows10(nil)
			_ = m.Engine.ApplyBluebeamRegistryOverrides(nil)
			_ = m.Engine.ApplyDarkTheme(nil)
			if exePath, ok := m.Bluebeam.FindRevuExecutable(); ok {
				extractDir := filepath.Join(m.Engine.DriveCPath(), "windows", "temp", "bb_setup_files")
				payloads := m.Config.DiscoverPayloads()
				m.Bluebeam.ConfigurePostInstall(filepath.Dir(exePath), extractDir, payloads.BPXProfile, func(line string) {
					emit("[Prefix] "+line, 85)
				})
			}
			_ = m.Engine.WaitServer()
			res.PrefixUpdated = true
		}
	}

	// 4. Refresh Linux Desktop Launcher & Official Bluebeam Revu 21 Icon
	emit("Step 4/4: Updating Linux desktop launcher & Bluebeam Revu 21 icon...", 94)
	if err := desktop.CreateShortcut(); err == nil {
		res.ShortcutUpdated = true
	}

	// If a newer launcher binary or AppImage was written to disk during this run, invoke its repair & shortcut
	// commands so any newly added prefix or protocol migrations take effect immediately.
	if res.LauncherUpdated {
		if targetExe, exeErr := desktop.ResolveExecutablePath(); exeErr == nil && fileExists(targetExe) {
			_ = exec.Command(targetExe, "shortcut").Run()
			if dirExists(m.Engine.DriveCPath()) {
				_ = exec.Command(targetExe, "repair").Run()
			}
		}
	}

	emit("Update complete. Launcher, Proton-GE (amd64), and desktop integration are up to date.", 100)
	return res, nil
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// UpdateLauncher updates the running AppImage, git repository clone, or standalone Linux amd64 binary.
func (m *Manager) UpdateLauncher(emit func(string)) (gitUpdated bool, binaryUpdated bool, err error) {
	// 1. AppImage self-update mode ($APPIMAGE points to the outer .AppImage file on disk)
	if appImg := strings.TrimSpace(os.Getenv("APPIMAGE")); appImg != "" {
		if resolved, symErr := filepath.EvalSymlinks(appImg); symErr == nil {
			appImg = resolved
		}
		downloadURL := resolveLatestBinaryURL(true)
		emit("AppImage runtime detected; downloading latest Linbeam AppImage from GitHub Releases...")
		tmpAppImg := appImg + ".new"
		if dlErr := downloadFile(downloadURL, tmpAppImg); dlErr != nil {
			_ = os.Remove(tmpAppImg)
			return false, false, fmt.Errorf("could not download latest AppImage: %v", dlErr)
		}
		if !isValidAMD64ELF(tmpAppImg) {
			_ = os.Remove(tmpAppImg)
			return false, false, fmt.Errorf("downloaded AppImage is not a valid Linux x86_64 ELF executable")
		}
		if chErr := os.Chmod(tmpAppImg, 0755); chErr != nil {
			_ = os.Remove(tmpAppImg)
			return false, false, chErr
		}
		if rnErr := os.Rename(tmpAppImg, appImg); rnErr != nil {
			_ = os.Remove(tmpAppImg)
			return false, false, fmt.Errorf("failed to replace AppImage %s: %w", appImg, rnErr)
		}
		emit("Successfully updated AppImage at " + appImg)
		return false, true, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return false, false, err
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return false, false, err
	}

	// 2. Git clone mode: pull latest changes and rebuild (or use updated binary)
	exeDir := filepath.Dir(exePath)
	if repoRoot, ok := findGitRepoRoot(exeDir); ok {
		emit(fmt.Sprintf("Detected git repository at %s; pulling latest changes...", repoRoot))
		fetchCmd := exec.Command("git", "-C", repoRoot, "fetch", "origin")
		if out, fErr := fetchCmd.CombinedOutput(); fErr == nil {
			_ = exec.Command("git", "-C", repoRoot, "checkout", "--", ".").Run()
			pullCmd := exec.Command("git", "-C", repoRoot, "pull", "--ff-only")
			if pullOut, pErr := pullCmd.CombinedOutput(); pErr == nil {
				gitUpdated = true
				emit("Git repository updated: " + strings.TrimSpace(string(pullOut)))
				// If executable had resided inside legacy linbeam-go/ subfolder before repo root migration,
				// update exePath to the root binary if present.
				rootBin := filepath.Join(repoRoot, "linbeam")
				if !fileExists(exePath) && fileExists(rootBin) {
					exePath = rootBin
				}
			} else {
				emit(fmt.Sprintf("git pull note: %s", strings.TrimSpace(string(pullOut))))
			}
		} else {
			emit(fmt.Sprintf("git fetch note: %s", strings.TrimSpace(string(out))))
		}

		if goBin, lErr := exec.LookPath("go"); lErr == nil {
			goModDir := repoRoot
			if !fileExists(filepath.Join(goModDir, "go.mod")) && fileExists(filepath.Join(repoRoot, "linbeam-go", "go.mod")) {
				goModDir = filepath.Join(repoRoot, "linbeam-go")
			}
			if fileExists(filepath.Join(goModDir, "go.mod")) {
				emit("Host Go compiler detected; rebuilding launcher from latest source...")
				targetBin := filepath.Join(goModDir, "linbeam")
				tmpBin := targetBin + ".new"
				buildCmd := exec.Command(goBin, "build", "-ldflags=-s -w", "-o", tmpBin, ".")
				buildCmd.Dir = goModDir
				buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
				if bOut, bErr := buildCmd.CombinedOutput(); bErr == nil && isValidAMD64ELF(tmpBin) {
					_ = os.Chmod(tmpBin, 0755)
					if rErr := os.Rename(tmpBin, targetBin); rErr == nil {
						emit("Rebuilt and replaced launcher binary at " + targetBin)
						return gitUpdated, true, nil
					}
				} else if bErr != nil {
					_ = os.Remove(tmpBin)
					emit(fmt.Sprintf("Local go build skipped: %s", strings.TrimSpace(string(bOut))))
				}
			}
		}

		if gitUpdated && isValidAMD64ELF(exePath) {
			_ = os.Chmod(exePath, 0755)
			return true, true, nil
		}
	}

	// 3. Standalone binary update from GitHub Releases or raw main branch
	downloadURL := resolveLatestBinaryURL(false)
	emit("Downloading latest pre-compiled amd64 launcher from GitHub...")
	tmpBin := exePath + ".new"
	if err := downloadFile(downloadURL, tmpBin); err != nil {
		if err2 := downloadFile(githubRawBinaryRootURL, tmpBin); err2 != nil {
			if err3 := downloadFile(githubRawBinaryLegacyURL, tmpBin); err3 != nil {
				_ = os.Remove(tmpBin)
				return gitUpdated, false, fmt.Errorf("could not download remote binary: %v", err)
			}
		}
	}

	if !isValidAMD64ELF(tmpBin) {
		_ = os.Remove(tmpBin)
		return gitUpdated, false, fmt.Errorf("downloaded file is not a valid Linux x86_64 ELF executable")
	}

	if err := os.Chmod(tmpBin, 0755); err != nil {
		_ = os.Remove(tmpBin)
		return gitUpdated, false, err
	}

	if err := os.Rename(tmpBin, exePath); err != nil {
		_ = os.Remove(tmpBin)
		return gitUpdated, false, fmt.Errorf("failed to replace executable %s: %w", exePath, err)
	}

	emit("Successfully updated launcher binary at " + exePath)
	return gitUpdated, true, nil
}

func selectReleaseAssetURL(assets []releaseAsset, preferAppImage bool) string {
	isArchCompatible := func(lower string) bool {
		return !strings.Contains(lower, "aarch64") &&
			!strings.Contains(lower, "arm64") &&
			!strings.HasSuffix(lower, ".txt") &&
			!strings.HasSuffix(lower, ".sha256")
	}

	if preferAppImage {
		for _, a := range assets {
			lower := strings.ToLower(a.Name)
			if strings.HasSuffix(lower, ".appimage") && isArchCompatible(lower) {
				return a.BrowserDownloadURL
			}
		}
	} else {
		for _, a := range assets {
			lower := strings.ToLower(a.Name)
			if !strings.HasSuffix(lower, ".appimage") &&
				(strings.Contains(lower, "linbeam") || strings.Contains(lower, "linux")) &&
				isArchCompatible(lower) {
				return a.BrowserDownloadURL
			}
		}
	}

	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if (strings.Contains(lower, "linbeam") || strings.HasSuffix(lower, ".appimage")) && isArchCompatible(lower) {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

func resolveLatestBinaryURL(preferAppImage bool) string {
	req, err := http.NewRequest("GET", githubLatestReleaseAPI, nil)
	if err != nil {
		return githubRawBinaryRootURL
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubRawBinaryRootURL
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return githubRawBinaryRootURL
	}

	var rel struct {
		Assets []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return githubRawBinaryRootURL
	}

	if u := selectReleaseAssetURL(rel.Assets, preferAppImage); u != "" {
		return u
	}

	return githubRawBinaryRootURL
}

func findGitRepoRoot(startDir string) (string, bool) {
	curr := startDir
	for i := 0; i < 4; i++ {
		if dirExists(filepath.Join(curr, ".git")) {
			return curr, true
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return "", false
}

// isValidAMD64ELF verifies that a file is a Linux x86_64 ELF executable (EM_X86_64 = 62).
func isValidAMD64ELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var hdr [20]byte
	n, err := f.Read(hdr[:])
	if err != nil || n < 20 {
		return false
	}
	if hdr[0] != 0x7f || hdr[1] != 'E' || hdr[2] != 'L' || hdr[3] != 'F' {
		return false
	}
	machine := binary.LittleEndian.Uint16(hdr[18:20])
	return machine == 62
}

func downloadFile(url, dest string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
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
