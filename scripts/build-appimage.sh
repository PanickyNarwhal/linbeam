#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${REPO_ROOT}/dist"
APPDIR="${DIST_DIR}/Linbeam.AppDir"

rm -rf "${DIST_DIR}"
mkdir -p "${APPDIR}/usr/bin"
mkdir -p "${APPDIR}/usr/share/applications"
mkdir -p "${APPDIR}/usr/share/icons/hicolor/256x256/apps"

echo "[1/4] Building static Linux amd64 binary..."
cd "${REPO_ROOT}"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "${DIST_DIR}/linbeam-linux-amd64" .
chmod 0755 "${DIST_DIR}/linbeam-linux-amd64"
cp "${DIST_DIR}/linbeam-linux-amd64" "${APPDIR}/usr/bin/linbeam"
chmod 0755 "${APPDIR}/usr/bin/linbeam"

echo "[2/4] Staging AppDir metadata and icons..."
cp "${REPO_ROOT}/pkg/desktop/revu21.png" "${APPDIR}/linbeam.png"
cp "${REPO_ROOT}/pkg/desktop/revu21.png" "${APPDIR}/.DirIcon"
cp "${REPO_ROOT}/pkg/desktop/revu21.png" "${APPDIR}/usr/share/icons/hicolor/256x256/apps/linbeam.png"

cat > "${APPDIR}/AppRun" <<'EOF'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
exec "${HERE}/usr/bin/linbeam" "$@"
EOF
chmod 0755 "${APPDIR}/AppRun"

cat > "${APPDIR}/linbeam.desktop" <<'EOF'
[Desktop Entry]
Name=Linbeam
GenericName=Bluebeam Revu 21 Deployment & Runtime Console
Comment=Install, update, and run Bluebeam Revu 21 on Linux via Proton-GE
Exec=linbeam %U
Terminal=false
Type=Application
Icon=linbeam
Categories=Office;Engineering;Graphics;Utility;
MimeType=application/pdf;x-scheme-handler/revusignin;x-scheme-handler/openspinrevu;x-scheme-handler/bluebeam;x-scheme-handler/bbstudio;
StartupWMClass=Revu.exe
EOF
cp "${APPDIR}/linbeam.desktop" "${APPDIR}/usr/share/applications/linbeam.desktop"

echo "[3/4] Packaging Linbeam-x86_64.AppImage..."
APPIMAGETOOL=""
if command -v appimagetool >/dev/null 2>&1; then
    APPIMAGETOOL="$(command -v appimagetool)"
else
    APPIMAGETOOL="${DIST_DIR}/appimagetool-x86_64.AppImage"
    curl -fsSL -o "${APPIMAGETOOL}" \
        "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage"
    chmod 0755 "${APPIMAGETOOL}"
fi

ARCH=x86_64 "${APPIMAGETOOL}" --appimage-extract-and-run "${APPDIR}" "${DIST_DIR}/Linbeam-x86_64.AppImage"
chmod 0755 "${DIST_DIR}/Linbeam-x86_64.AppImage"
rm -f "${DIST_DIR}/appimagetool-x86_64.AppImage"
rm -rf "${APPDIR}"

echo "[4/4] Generating SHA256SUMS.txt..."
(
    cd "${DIST_DIR}"
    sha256sum Linbeam-x86_64.AppImage linbeam-linux-amd64 > SHA256SUMS.txt
)

echo "Build complete:"
ls -lh "${DIST_DIR}"
