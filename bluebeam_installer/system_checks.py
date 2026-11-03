#!/usr/bin/env python3
import logging
import shutil
import sys

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

REQUIRED_BINARIES = [
    "winetricks",
    "cabextract",
]

WINE_BINARIES = (
    "wine",
    "wine64",
)

SEVENZIP_BINARIES = (
    "7z",
    "7za",
    "7zz",
)


def detect_package_manager():
    for package_manager in ("apt", "dnf", "pacman", "zypper", "yum"):
        if shutil.which(package_manager):
            return package_manager

    return "unknown"


def get_7z_executable():
    for binary in SEVENZIP_BINARIES:
        if shutil.which(binary):
            return binary
    return None


def check_dependencies():
    missing = []
    if not any(shutil.which(binary) is not None for binary in WINE_BINARIES):
        missing.append("wine")

    for binary in REQUIRED_BINARIES:
        if shutil.which(binary) is None:
            missing.append(binary)

    return missing


def get_install_command(missing_deps, package_manager):
    if not missing_deps:
        return None

    pkg_list = " ".join(missing_deps)
    commands = {
        "apt": f"sudo apt update && sudo apt install {pkg_list}",
        "dnf": f"sudo dnf install {pkg_list}",
        "pacman": f"sudo pacman -S {pkg_list}",
        "zypper": f"sudo zypper install {pkg_list}",
        "yum": f"sudo yum install {pkg_list}",
    }

    return commands.get(package_manager, f"Please install these manually: {pkg_list}")


def run_system_flight_check():
    logging.info("Running host system flight check...")

    missing = check_dependencies()
    if not missing:
        if get_7z_executable() is None:
            logging.info(
                "Optional host 7z binary not found; WebView2 extraction will fall back to Wine's bundled 7z.exe."
            )
        logging.info("Looks good. All the host bits are there.")
        return True

    pkg_manager = detect_package_manager()
    install_cmd = get_install_command(missing, pkg_manager)

    logging.critical("Missing required system dependencies.")
    logging.critical("Missing: %s", ", ".join(missing))

    if pkg_manager != "unknown":
        logging.info("Detected package manager: %s", pkg_manager)
        logging.info("Run the following command to fix it:")
        print(f"\n    {install_cmd}\n")
    else:
        logging.warning("Could not detect a package manager, so this may need a manual install.")

    return False


def main():
    return run_system_flight_check()


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
