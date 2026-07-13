import logging
import shutil
import sys

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

REQUIRED_BINARIES = [
    "wine",
    "wine64",
    "winetricks",
    "cabextract",
]


def detect_package_manager():
    for package_manager in ("apt", "dnf", "pacman", "zypper", "yum"):
        if shutil.which(package_manager):
            return package_manager

    return "unknown"


def check_dependencies():
    missing = []
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
