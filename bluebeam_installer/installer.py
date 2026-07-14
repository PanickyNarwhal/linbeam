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

    prefix = env.get("WINEPREFIX")
    if not prefix:
        logging.critical("CRITICAL: WINEPREFIX missing in environment.")
        return False

    original_name = os.path.basename(msi_path)
    safe_name = original_name.replace(" ", "_")
    wine_c_drive = os.path.join(prefix, "drive_c")
    stage_dir = os.path.join(wine_c_drive, "linbeam_temp")
    os.makedirs(stage_dir, exist_ok=True)

    staged_msi_path = os.path.join(stage_dir, safe_name)
    logging.info("Staging payload into Wine C: drive as %s...", safe_name)

    try:
        shutil.copy2(msi_path, staged_msi_path)
    except Exception as e:
        logging.critical("CRITICAL: Failed to stage payload in Wine prefix: %s", e)
        return False

    wine_executable = get_wine_executable()
    if wine_executable is None:
        return False

    wine_msi_path = to_wine_path(staged_msi_path, env)
    cmd = [wine_executable, "msiexec", "/i", wine_msi_path, "/qn", "/norestart"]
    if properties:
        cmd.extend(shlex.split(properties))

    try:
        logging.info("Executing msiexec...")
        result = subprocess.run(
            cmd,
            env=env,
            check=True,
            capture_output=True,
            text=True,
        )
        logging.info("Success: %s installed.", original_name)
        if result.stdout:
            logging.debug("msiexec stdout: %s", result.stdout.strip())
        return True
    except subprocess.CalledProcessError as e:
        if e.returncode == 3010:
            logging.info("Success: %s installed (ignoring pending Windows reboot).", original_name)
            return True

        if e.stdout:
            logging.error("msiexec output: %s", e.stdout.strip())
        if e.stderr:
            logging.error("msiexec error: %s", e.stderr.strip())

        logging.critical("CRITICAL: Installation failed for %s with exit code: %s", original_name, e.returncode)
        return False
    finally:
        if os.path.exists(staged_msi_path):
            os.remove(staged_msi_path)

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
        wine_executable = get_wine_executable()
        if wine_executable is None:
            return False

        cmd = [
            wine_executable,
            windows_exe_path,
            "/s",
            f"/bpximport:{profile_staged_path}",
            f"/bpxactive:{profile_name}",
        ]

        try:
            subprocess.run(cmd, env=env, check=True, capture_output=True, text=True)
            logging.info("The profile was registered.")
        except subprocess.CalledProcessError as exc:
            if exc.stdout:
                logging.error("Profile registration output: %s", exc.stdout.strip())
            if exc.stderr:
                logging.error("Profile registration error: %s", exc.stderr.strip())
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