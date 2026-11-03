import logging
import os
import shutil
import subprocess
import sys

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

WEBVIEW2_ARGS = "--no-sandbox --disable-gpu"


def get_wine_binary():
    for binary in ("wine64", "wine"):
        if shutil.which(binary):
            return binary
    return "wine"


def get_wine_env(prefix_path="~/.local/share/bluebeam-wine"):
    env = os.environ.copy()
    env["WINEPREFIX"] = os.path.expanduser(prefix_path)
    env["WINEARCH"] = "win64"
    env["WINEDEBUG"] = "-all"
    env["WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"] = WEBVIEW2_ARGS
    return env


def init_prefix(env):
    prefix = env["WINEPREFIX"]
    parent_dir = os.path.dirname(prefix)
    if parent_dir:
        os.makedirs(parent_dir, exist_ok=True)

    logging.info("Setting up a fresh Wine prefix at %s", prefix)

    try:
        subprocess.run(
            ["wineboot", "-u"],
            env=env,
            check=True,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    except FileNotFoundError:
        logging.critical("I could not find wineboot on this machine.")
        return False
    except subprocess.CalledProcessError:
        logging.critical("Wine had a problem while making the prefix.")
        return False

    logging.info("The Wine prefix is ready.")
    return True


def configure_prefix_registry(env):
    wine_bin = get_wine_binary()
    reg_entries = [
        (
            r"HKLM\Software\Microsoft\Windows NT\CurrentVersion",
            "ReleaseId",
            "REG_SZ",
            "2009",
        ),
        (
            r"HKLM\Software\Microsoft\Windows NT\CurrentVersion",
            "CurrentBuild",
            "REG_SZ",
            "19045",
        ),
        (
            r"HKLM\Software\Microsoft\Windows NT\CurrentVersion",
            "CurrentBuildNumber",
            "REG_SZ",
            "19045",
        ),
        (
            r"HKLM\Software\Microsoft\PowerShell\3\PowerShellEngine",
            "PowerShellVersion",
            "REG_SZ",
            "5.1.19041.1",
        ),
        (
            r"HKCU\Environment",
            "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
            "REG_SZ",
            WEBVIEW2_ARGS,
        ),
    ]

    for key, value_name, reg_type, data in reg_entries:
        cmd = [
            wine_bin,
            "reg",
            "add",
            key,
            "/v",
            value_name,
            "/t",
            reg_type,
            "/d",
            data,
            "/f",
        ]
        try:
            subprocess.run(
                cmd,
                env=env,
                check=True,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
        except (FileNotFoundError, subprocess.CalledProcessError) as exc:
            logging.warning("Could not set registry value %s\\%s: %s", key, value_name, exc)

    return True


def install_local_vc_redist(env, payload_dir="payloads"):
    prefix = env.get("WINEPREFIX")
    if not prefix or not os.path.isdir(payload_dir):
        return False

    wine_bin = get_wine_binary()
    stage_dir = os.path.join(prefix, "drive_c", "linbeam_temp")
    os.makedirs(stage_dir, exist_ok=True)

    installed_x64 = False
    for exe_name in ("vc_redist.x86.exe", "vc_redist.x64.exe"):
        src_path = os.path.join(payload_dir, exe_name)
        if not os.path.isfile(src_path):
            continue

        staged_path = os.path.join(stage_dir, exe_name)
        logging.info("Installing local %s into Wine prefix...", exe_name)
        try:
            shutil.copy2(src_path, staged_path)
            wine_path = f"C:\\linbeam_temp\\{exe_name}"
            result = subprocess.run(
                [wine_bin, wine_path, "/install", "/quiet", "/norestart"],
                env=env,
                capture_output=True,
                text=True,
            )
            if result.returncode in (0, 1638, 3010):
                logging.info("Installed local %s (exit code %s).", exe_name, result.returncode)
                if exe_name == "vc_redist.x64.exe":
                    installed_x64 = True
            else:
                logging.warning(
                    "Local %s returned exit code %s; will fall back to winetricks vcrun2022.",
                    exe_name,
                    result.returncode,
                )
        except Exception as exc:
            logging.warning("Could not run local %s: %s", exe_name, exc)
        finally:
            if os.path.exists(staged_path):
                try:
                    os.remove(staged_path)
                except OSError:
                    pass

    return installed_x64


def inject_dependencies(env, payload_dir="payloads"):
    logging.info("Installing the Windows pieces Wine needs for this setup.")

    local_vc_installed = install_local_vc_redist(env, payload_dir=payload_dir)
    packages = ["corefonts", "dotnet48", "gdiplus"]
    if not local_vc_installed:
        packages.insert(1, "vcrun2022")

    cmd = ["winetricks", "-q"] + packages

    try:
        subprocess.run(cmd, env=env, check=True)
    except FileNotFoundError:
        logging.critical("I could not find winetricks.")
        return False
    except subprocess.CalledProcessError as exc:
        logging.critical("Winetricks stopped with exit code %s", exc.returncode)
        return False

    configure_prefix_registry(env)
    logging.info("The extra Windows pieces were installed.")
    return True


def build_wine_environment():
    env = get_wine_env()

    if not init_prefix(env):
        return None

    if not inject_dependencies(env):
        return None

    return env


def main():
    env = build_wine_environment()
    if env is None:
        logging.critical("The Wine setup did not finish.")
        return 1

    logging.info("Everything looks set up.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
