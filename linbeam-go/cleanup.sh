#!/usr/bin/env bash
set -euo pipefail

echo "===================================================="
echo "   Linbeam — Full Environment Scrub & Reset         "
echo "===================================================="

KEEP_RUNNERS=0
if [[ "${1:-}" == "--keep-proton" || "${1:-}" == "--keep-runners" ]]; then
    KEEP_RUNNERS=1
fi

echo "[1/5] Stopping any running Wine / Proton processes..."
wineserver -k 2>/dev/null || true
WINEPREFIX="$HOME/.wine-bluebeam" wineserver -k 2>/dev/null || true
WINEPREFIX="$HOME/.local/share/linbeam/prefix/pfx" wineserver -k 2>/dev/null || true
pkill -f "Revu.exe" 2>/dev/null || true
pkill -f "Bluebeam" 2>/dev/null || true

echo "[2/5] Removing old Python/Bash Linbeam prefix (~/.wine-bluebeam) & temp files..."
rm -rf "$HOME/.wine-bluebeam"
rm -rf /tmp/linbeam* /tmp/msi_patcher* /tmp/bb_setup_files* /tmp/revu_install.log /tmp/ocr_install.log 2>/dev/null || true

# Clean up Python virtualenv and caches if inside the repo
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
rm -rf "$SCRIPT_DIR/.venv" "$SCRIPT_DIR/venv" "$SCRIPT_DIR/__pycache__" "$SCRIPT_DIR/bluebeam_installer/__pycache__" "$SCRIPT_DIR/.pytest_cache" 2>/dev/null || true

echo "[3/5] Removing Linbeam Go config, cache, and Proton prefix..."
rm -rf "$HOME/.config/linbeam"
rm -rf "$HOME/.cache/linbeam"
if [[ "$KEEP_RUNNERS" -eq 1 ]]; then
    echo "      (Keeping downloaded Proton-GE runners in ~/.local/share/linbeam/runners)"
    rm -rf "$HOME/.local/share/linbeam/prefix"
else
    rm -rf "$HOME/.local/share/linbeam"
fi

echo "[4/5] Removing desktop shortcuts, MIME associations, and Wine menu entries..."
rm -f "$HOME/.local/share/applications/linbeam.desktop"
rm -f "$HOME/.local/share/applications/linbeam-manager.desktop"
rm -f "$HOME/.local/share/applications/bluebeam"*.desktop
rm -rf "$HOME/.local/share/applications/wine/Programs/Bluebeam"*
rm -f "$HOME/.local/share/icons/hicolor/scalable/apps/linbeam.svg"
rm -f "$HOME/Desktop/Bluebeam"*.desktop "$HOME/Desktop/Bluebeam"*.lnk 2>/dev/null || true

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
fi

echo "[5/5] Done! Your system is completely scrubbed and ready for a fresh Linbeam test."
