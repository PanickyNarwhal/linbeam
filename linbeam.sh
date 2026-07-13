#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=================================================="
echo "  Starting the Bluebeam Revu 21 setup on Linux..."
echo "=================================================="

VENV_DIR=".venv"

if ! command -v python3 >/dev/null 2>&1; then
    echo "[ERROR] Python 3 is not available on this machine."
    exit 1
fi

if ! python3 -c "import venv" &>/dev/null; then
    echo "[ERROR] The Python venv module is missing."
    echo "On Debian or Ubuntu, this is usually fixed with: sudo apt install python3-venv"
    exit 1
fi

if [ ! -d "$VENV_DIR" ]; then
    echo "[INFO] Creating a clean virtual environment for the installer..."
    python3 -m venv "$VENV_DIR"
fi

# shellcheck source=/dev/null
source "$VENV_DIR/bin/activate"

echo "[INFO] Preparing the Python environment..."
python3 -m pip install --upgrade pip --quiet
if [ -f "requirements.txt" ]; then
    python3 -m pip install -r requirements.txt --quiet
fi

echo "[INFO] Launching the installer..."
python3 -m bluebeam_installer.main

deactivate || true

echo "=================================================="
echo "  Setup finished. Thanks for waiting."
echo "=================================================="