package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const linbeamSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#0066CC"/>
      <stop offset="100%" stop-color="#003D82"/>
    </linearGradient>
  </defs>
  <rect width="128" height="128" rx="26" fill="url(#bg)"/>
  <path d="M34 30h38c14 0 23 7 23 18 0 7-4 13-11 15 9 2 14 9 14 18 0 13-10 21-26 21H34V30zm16 14v15h19c6 0 10-3 10-7.5S75 44 69 44H50zm0 28v16h21c7 0 11-3 11-8s-4-8-11-8H50z" fill="#FFFFFF"/>
  <circle cx="96" cy="32" r="8" fill="#38BDF8"/>
</svg>`

// CreateShortcut installs the Linux .desktop entries, SVG icon, and MIME/protocol associations.
func CreateShortcut() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to evaluate executable symlinks: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	appsDir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		return err
	}

	iconDir := filepath.Join(home, ".local", "share", "icons", "hicolor", "scalable", "apps")
	_ = os.MkdirAll(iconDir, 0755)
	iconPath := filepath.Join(iconDir, "linbeam.svg")
	_ = os.WriteFile(iconPath, []byte(linbeamSVG), 0644)

	revuDesktopPath := filepath.Join(appsDir, "linbeam.desktop")
	revuContent := fmt.Sprintf(`[Desktop Entry]
Name=Bluebeam Revu 21 (Linbeam)
Comment=Run Bluebeam Revu 21 on Linux via Proton-GE
Exec="%s" launch %%F
Terminal=false
Type=Application
Icon=%s
Categories=Office;Viewer;Engineering;Graphics;
MimeType=application/pdf;x-scheme-handler/bluebeam;x-scheme-handler/bbstudio;
StartupWMClass=Revu.exe
`, exePath, iconPath)

	if err := os.WriteFile(revuDesktopPath, []byte(revuContent), 0755); err != nil {
		return err
	}

	mgrDesktopPath := filepath.Join(appsDir, "linbeam-manager.desktop")
	mgrContent := fmt.Sprintf(`[Desktop Entry]
Name=Linbeam Setup & Manager
Comment=Manage Proton-GE prefix and Bluebeam Revu 21 installation
Exec="%s" --gui
Terminal=false
Type=Application
Icon=%s
Categories=Utility;System;
`, exePath, iconPath)

	_ = os.WriteFile(mgrDesktopPath, []byte(mgrContent), 0755)

	_ = exec.Command("xdg-mime", "default", "linbeam.desktop", "x-scheme-handler/bluebeam").Run()
	_ = exec.Command("xdg-mime", "default", "linbeam.desktop", "x-scheme-handler/bbstudio").Run()
	_ = exec.Command("update-desktop-database", appsDir).Run()

	return nil
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
