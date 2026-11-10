#!/usr/bin/env bash
set -euo pipefail

echo "===================================================="
echo "   Linbeam — Full Environment Scrub & Reset         "
echo "===================================================="

KEEP_RUNNERS=0
if [[ "${1:-}" == "--keep-proton" || "${1:-}" == "--keep-runners" ]]; then
    KEEP_RUNNERS=1
fi

echo "[1/4] Stopping any running Wine / Proton processes..."
wineserver -k 2>/dev/null || true
WINEPREFIX="$HOME/.wine-bluebeam" wineserver -k 2>/dev/null || true
WINEPREFIX="$HOME/.local/share/linbeam/prefix/pfx" wineserver -k 2>/dev/null || true
pkill -f "Revu.exe" 2>/dev/null || true
pkill -f "Bluebeam" 2>/dev/null || true

echo "[2/4] Removing temporary installer & legacy prefix files..."
rm -rf "$HOME/.wine-bluebeam"
rm -rf /tmp/linbeam* /tmp/msi_patcher* /tmp/bb_setup_files* /tmp/revu_install.log /tmp/ocr_install.log 2>/dev/null || true

echo "[3/4] Removing Linbeam config, cache, and Proton prefix..."
rm -rf "$HOME/.config/linbeam"
rm -rf "$HOME/.cache/linbeam"
if [[ "$KEEP_RUNNERS" -eq 1 ]]; then
    echo "      (Keeping downloaded Proton-GE runners in ~/.local/share/linbeam/runners)"
    rm -rf "$HOME/.local/share/linbeam/prefix"
else
    rm -rf "$HOME/.local/share/linbeam"
fi

echo "[4/4] Removing desktop shortcuts, MIME associations, and Wine menu entries..."
rm -f "$HOME/.local/share/applications/linbeam.desktop"
rm -f "$HOME/.local/share/applications/linbeam-manager.desktop"
rm -f "$HOME/.local/share/applications/bluebeam"*.desktop
rm -rf "$HOME/.local/share/applications/wine/Programs/Bluebeam"*
rm -f "$HOME/.local/share/icons/hicolor/scalable/apps/linbeam.svg"
rm -f "$HOME/.local/share/icons/hicolor/256x256/apps/linbeam.png"
rm -f "$HOME/.local/share/pixmaps/linbeam.png"
rm -f "$HOME/Desktop/Bluebeam"*.desktop "$HOME/Desktop/Bluebeam"*.lnk 2>/dev/null || true

if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
fi

echo "Done. Environment is scrubbed and ready for a fresh Linbeam deployment."
