package desktop

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed revu21.png
var revu21IconPNG []byte

// ResolveExecutablePath returns the persistent executable path on disk.
// When running inside a Linux AppImage, it returns the outer .AppImage file path ($APPIMAGE)
// rather than the ephemeral FUSE mount inside /tmp/.mount_*.
func ResolveExecutablePath() (string, error) {
	if appImg := strings.TrimSpace(os.Getenv("APPIMAGE")); appImg != "" {
		if resolved, err := filepath.EvalSymlinks(appImg); err == nil {
			return resolved, nil
		}
		return appImg, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve current executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate executable symlinks: %w", err)
	}
	return exePath, nil
}

// CreateShortcut installs the Linux .desktop entries, official Bluebeam Revu 21 PNG icon,
// and XDG MIME/protocol associations (including revusignin:// OAuth sign-in callbacks).
func CreateShortcut() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	exePath, err := ResolveExecutablePath()
	if err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		return err
	}

	// Remove any legacy SVG icon so the official PNG icon takes precedence
	_ = os.Remove(filepath.Join(home, ".local", "share", "icons", "hicolor", "scalable", "apps", "linbeam.svg"))

	iconDir := filepath.Join(home, ".local", "share", "icons", "hicolor", "256x256", "apps")
	if err := os.MkdirAll(iconDir, 0755); err != nil {
		return err
	}
	iconPath := filepath.Join(iconDir, "linbeam.png")
	if err := os.WriteFile(iconPath, revu21IconPNG, 0644); err != nil {
		return err
	}

	pixmapDir := filepath.Join(home, ".local", "share", "pixmaps")
	_ = os.MkdirAll(pixmapDir, 0755)
	_ = os.WriteFile(filepath.Join(pixmapDir, "linbeam.png"), revu21IconPNG, 0644)

	revuDesktopPath := filepath.Join(appsDir, "linbeam.desktop")
	revuContent := fmt.Sprintf(`[Desktop Entry]
Name=Bluebeam Revu 21
GenericName=PDF CAD & Markup Editor
Comment=Run Bluebeam Revu 21 on Linux via Proton-GE
Exec="%s" launch %%U
Terminal=false
Type=Application
Icon=%s
Categories=Office;Viewer;Engineering;Graphics;
MimeType=application/pdf;x-scheme-handler/revusignin;x-scheme-handler/openspinrevu;x-scheme-handler/bluebeam;x-scheme-handler/bbstudio;
StartupWMClass=Revu.exe
`, exePath, iconPath)

	if err := os.WriteFile(revuDesktopPath, []byte(revuContent), 0755); err != nil {
		return err
	}

	mgrDesktopPath := filepath.Join(appsDir, "linbeam-manager.desktop")
	mgrContent := fmt.Sprintf(`[Desktop Entry]
Name=Linbeam Deployment Manager
Comment=Configure Proton-GE prefix and Bluebeam Revu 21 installation
Exec="%s" --gui
Terminal=false
Type=Application
Icon=%s
Categories=Utility;System;
`, exePath, iconPath)

	_ = os.WriteFile(mgrDesktopPath, []byte(mgrContent), 0755)

	schemeHandlers := []string{
		"x-scheme-handler/revusignin",
		"x-scheme-handler/openspinrevu",
		"x-scheme-handler/bluebeam",
		"x-scheme-handler/bbstudio",
	}
	for _, scheme := range schemeHandlers {
		_ = exec.Command("xdg-mime", "default", "linbeam.desktop", scheme).Run()
	}

	_ = exec.Command("gtk-update-icon-cache", "-f", "-t", filepath.Join(home, ".local", "share", "icons", "hicolor")).Run()
	_ = exec.Command("update-desktop-database", appsDir).Run()

	return nil
}

// EnsureProtocolHandlers ensures linbeam.desktop exists and has x-scheme-handler/revusignin
// registered with XDG so native Linux browser OAuth callbacks return to Bluebeam Revu 21.
func EnsureProtocolHandlers() {
	if runtime.GOOS != "linux" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	revuDesktopPath := filepath.Join(home, ".local", "share", "applications", "linbeam.desktop")
	data, err := os.ReadFile(revuDesktopPath)
	s := string(data)
	if err != nil ||
		!strings.Contains(s, "x-scheme-handler/revusignin") ||
		!strings.Contains(s, "%U") ||
		strings.Contains(s, "/.mount_") {
		_ = CreateShortcut()
	}
}

// ShortcutExists returns true if ~/.local/share/applications/linbeam.desktop exists.
func ShortcutExists() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	path := filepath.Join(home, ".local", "share", "applications", "linbeam.desktop")
	_, err = os.Stat(path)
	return err == nil
}
