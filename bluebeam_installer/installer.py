import logging
import os
import shlex
import shutil
import subprocess
import sys

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

WINE_EXECUTABLES = ("wine64", "wine")


def get_wine_executable():
    for binary in WINE_EXECUTABLES:
        if shutil.which(binary):
            logging.info("Using %s for Wine commands.", binary)
            return binary

    logging.critical("I could not find a Wine executable on the path.")
    return None


def to_wine_path(path, env=None):
    if not path:
        return path

    if shutil.which("winepath") is None:
        return path

    try:
        result = subprocess.run(
            ["winepath", "-w", path],
            check=True,
            capture_output=True,
            text=True,
            env=env if env is not None else os.environ.copy(),
        )
    except (subprocess.CalledProcessError, FileNotFoundError):
        return path

    return result.stdout.strip() or path


def execute_msi(msi_path, env, properties=""):
    if not os.path.exists(msi_path):
        logging.critical("CRITICAL: MSI payload not found at %s", msi_path)
        return False
        
    file_name = os.path.basename(msi_path)
    logging.info("Starting Wine installation for %s...", file_name)
    
    wine_path = f"Z:{msi_path.replace('/', '\\')}"
    
    cmd = ["wine", "msiexec", "/i", wine_path, "/qn", "/norestart"]
    if properties:
        cmd.extend(properties.split())
    
    try:
        subprocess.run(cmd, env=env, check=True, stdout=subprocess.DEVNULL)
        logging.info("Success: %s installed.", file_name)
        return True
    except subprocess.CalledProcessError as e:
        if e.returncode == 3010:
            logging.info("Success: %s installed (ignoring pending Windows reboot).", file_name)
            return True
        else:
            logging.critical("CRITICAL: Installation failed for %s with exit code: %s", file_name, e.returncode)
            return False


def deploy_bluebeam_suite(env, payload_dir="payloads"):
    abs_payload_dir = os.path.abspath(payload_dir)
    revu_msi = os.path.join(abs_payload_dir, "Bluebeam Revu x64 21.msi")
    ocr_msi = os.path.join(abs_payload_dir, "BluebeamOCR x64 21.msi")
    revu_properties = "BB_AUTO_UPDATE=0 DISABLE_WELCOME=1 BB_DISABLEANALYTICS=1"

    logging.info("Starting the main Revu install.")
    if not execute_msi(revu_msi, env, revu_properties):
        return False

    logging.info("Starting the OCR install.")
    if not execute_msi(ocr_msi, env):
        return False

    return True


def inject_custom_assets(env, assets_dir="assets", profile_name="GilmoreHomesStandard"):
    if not os.path.exists(assets_dir):
        logging.info("No custom assets folder was found, so I am skipping that part.")
        return True

    prefix = env.get("WINEPREFIX")
    if not prefix:
        logging.error("The Wine prefix was not set, so I cannot copy assets over.")
        return False

    wine_c_drive = os.path.join(prefix, "drive_c")
    wine_profile_dir = os.path.join(
        wine_c_drive,
        "ProgramData",
        "Bluebeam Software",
        "Bluebeam Revu",
        "21",
        "Revu",
        "Profiles",
    )
    windows_exe_path = r"C:\Program Files\Bluebeam Software\Bluebeam Revu\21\Revu\Revu.exe"

    logging.info("Staging the custom assets in the Wine profile.")
    os.makedirs(wine_profile_dir, exist_ok=True)

    profile_staged_path = None

    for item in os.listdir(assets_dir):
        source = os.path.join(assets_dir, item)
        destination = os.path.join(wine_profile_dir, item)

        try:
            if os.path.isfile(source):
                shutil.copy2(source, destination)
                logging.info("Copied %s into the Wine profile.", item)

                if item.endswith(".bpx"):
                    profile_staged_path = rf"C:\ProgramData\Bluebeam Software\Bluebeam Revu\21\Revu\Profiles\{item}"
        except Exception as exc:
            logging.error("Could not copy %s: %s", item, exc)

    if profile_staged_path:
        logging.info("Registering %s as the active profile.", profile_name)
        cmd = f'wine "{windows_exe_path}" /s /bpximport:"{profile_staged_path}" /bpxactive:{profile_name}'

        try:
            subprocess.run(cmd, shell=True, env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            logging.info("The profile was registered.")
        except subprocess.CalledProcessError as exc:
            logging.error("The profile registration failed with exit code %s", exc.returncode)

    return True


def run_installation(env):
    if not deploy_bluebeam_suite(env):
        return False

    inject_custom_assets(env)
    return True


if __name__ == "__main__":
    logging.warning("This module is not meant to be run by itself.")
    sys.exit(1)
