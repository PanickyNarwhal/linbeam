#!/usr/bin/env python3
import glob
import logging
import os
import shlex
import shutil
import subprocess
import sys

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

WINE_EXECUTABLES = ("wine64", "wine")
SEVENZIP_EXECUTABLES = ("7z", "7za", "7zz")
REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def _resolve_dir(dir_path):
    if os.path.isdir(dir_path):
        return os.path.abspath(dir_path)
    repo_candidate = os.path.join(REPO_ROOT, dir_path)
    if os.path.isdir(repo_candidate):
        return repo_candidate
    return os.path.abspath(dir_path)


VBS_MSI_PATCHER = r"""On Error Resume Next
If WScript.Arguments.Count < 1 Then WScript.Quit 1
Dim msiPath : msiPath = WScript.Arguments(0)
Dim installer : Set installer = CreateObject("WindowsInstaller.Installer")
If Err.Number <> 0 Then WScript.Quit 2
Dim db : Set db = installer.OpenDatabase(msiPath, 1)
If Err.Number <> 0 Then WScript.Quit 3

Sub RunSql(sql)
    On Error Resume Next
    Dim view : Set view = db.OpenView(sql)
    If Err.Number = 0 Then
        view.Execute
        view.Close
    End If
    Err.Clear
End Sub

Dim actions : actions = Array( _
    "Set_REBOOT_NEEDED_Prop_PS", _
    "Set_LANGUAGE_CHOICE_Prop_PS", _
    "CloseRevu_PS", _
    "KillBbPrint_PS", _
    "ExpandWebView2", _
    "Call_LaunchShellExplorer", _
    "RunBbPrint", _
    "RunBBPrint_ARM64" _
)

Dim act
For Each act In actions
    RunSql "DELETE FROM `InstallExecuteSequence` WHERE `Action`='" & act & "'"
    RunSql "DELETE FROM `InstallUISequence` WHERE `Action`='" & act & "'"
Next

RunSql "UPDATE `CustomAction` SET `Type`=65 WHERE `Action`='PreInstall'"
RunSql "UPDATE `CustomAction` SET `Type`=65 WHERE `Action`='PostInstall'"
RunSql "UPDATE `CustomAction` SET `Type`=1665 WHERE `Action`='InvokeGeneralAdmin'"
RunSql "UPDATE `CustomAction` SET `Type`=1665 WHERE `Action`='InvokeGeneralAdminPlugins'"
RunSql "UPDATE `CustomAction` SET `Type`=1666 WHERE `Action`='Call_LaunchShellExplorer'"
RunSql "DELETE FROM `LaunchCondition` WHERE `Condition`='WIN10_RELEASEID>=1607 Or WIN10_RELEASEID=0'"
RunSql "UPDATE `Property` SET `Value`='1' WHERE `Property`='IGNORE_RBT'"
RunSql "INSERT INTO `Property` (`Property`, `Value`) VALUES ('LANGUAGE_CHOICE', 'en')"
RunSql "UPDATE `Property` SET `Value`='en' WHERE `Property`='LANGUAGE_CHOICE'"

db.Commit
If Err.Number <> 0 Then WScript.Quit 4
WScript.Quit 0
"""


def get_wine_executable():
    for binary in WINE_EXECUTABLES:
        if shutil.which(binary):
            logging.info("Using %s for Wine commands.", binary)
            return binary

    logging.critical("I could not find a Wine executable on the path.")
    return None


def get_host_7z_executable():
    for binary in SEVENZIP_EXECUTABLES:
        if shutil.which(binary):
            return binary
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


def patch_msi_for_wine(staged_msi_path, env):
    prefix = env.get("WINEPREFIX")
    if not prefix or not os.path.exists(staged_msi_path):
        return False

    wine_executable = get_wine_executable()
    if wine_executable is None:
        return False

    stage_dir = os.path.dirname(staged_msi_path)
    safe_name = os.path.basename(staged_msi_path)
    wine_msi_path = f"C:\\linbeam_temp\\{safe_name}"

    cs_source = os.path.join(os.path.dirname(os.path.abspath(__file__)), "msi_patcher.cs")
    staged_cs = os.path.join(stage_dir, "msi_patcher.cs")
    staged_exe = os.path.join(stage_dir, "msi_patcher.exe")

    csc_candidates = [
        (
            os.path.join(prefix, "drive_c", "windows", "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe"),
            r"C:\windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe",
        ),
        (
            os.path.join(prefix, "drive_c", "windows", "Microsoft.NET", "Framework", "v4.0.30319", "csc.exe"),
            r"C:\windows\Microsoft.NET\Framework\v4.0.30319\csc.exe",
        ),
    ]

    if os.path.isfile(cs_source) and not os.path.isfile(staged_exe):
        for host_csc, wine_csc in csc_candidates:
            if os.path.isfile(host_csc):
                try:
                    shutil.copy2(cs_source, staged_cs)
                    subprocess.run(
                        [
                            wine_executable,
                            wine_csc,
                            "/nologo",
                            "/optimize+",
                            r"/out:C:\linbeam_temp\msi_patcher.exe",
                            r"C:\linbeam_temp\msi_patcher.cs",
                        ],
                        env=env,
                        check=True,
                        capture_output=True,
                        text=True,
                    )
                    break
                except subprocess.CalledProcessError as exc:
                    logging.warning("Could not compile msi_patcher.cs with %s: %s", wine_csc, exc)

    if os.path.isfile(staged_exe):
        try:
            logging.info("Patching staged MSI %s for Wine compatibility...", safe_name)
            result = subprocess.run(
                [
                    wine_executable,
                    r"C:\linbeam_temp\msi_patcher.exe",
                    wine_msi_path,
                    r"C:\linbeam_temp",
                ],
                env=env,
                check=True,
                capture_output=True,
                text=True,
            )
            if result.stdout:
                logging.info(result.stdout.strip())
            return True
        except subprocess.CalledProcessError as exc:
            logging.warning("C# MSI patcher returned non-zero (%s); trying VBScript fallback.", exc.returncode)

    staged_vbs = os.path.join(stage_dir, "patch_msi.vbs")
    try:
        with open(staged_vbs, "w", encoding="utf-8") as handle:
            handle.write(VBS_MSI_PATCHER)
        subprocess.run(
            [
                wine_executable,
                "cscript",
                "//Nologo",
                r"C:\linbeam_temp\patch_msi.vbs",
                wine_msi_path,
            ],
            env=env,
            check=True,
            capture_output=True,
            text=True,
        )
        logging.info("Patched staged MSI %s using VBScript fallback.", safe_name)
        return True
    except Exception as exc:
        logging.warning("Could not patch MSI %s (continuing anyway): %s", safe_name, exc)
        return False


def log_msi_failure_details(log_file_path):
    if not os.path.isfile(log_file_path):
        return

    content = None
    for encoding in ("utf-16-le", "utf-8", "latin-1"):
        try:
            with open(log_file_path, "r", encoding=encoding) as handle:
                text = handle.read()
                if "MSI (" in text or "Action " in text or "Property(" in text:
                    content = text
                    break
        except Exception:
            continue

    if not content:
        return

    lines = [line.strip() for line in content.splitlines() if line.strip()]
    error_lines = [
        line
        for line in lines
        if "Return value 3" in line or "Error " in line or "cannot continue" in line or "Unable to run" in line
    ]

    if error_lines:
        logging.error("MSI log error highlights:")
        for line in error_lines[-15:]:
            logging.error("  %s", line)
    else:
        logging.error("Last lines of MSI log:")
        for line in lines[-15:]:
            logging.error("  %s", line)


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
    host_log_path = os.path.join(stage_dir, f"{safe_name}.log")
    logging.info("Staging payload into Wine C: drive as %s...", safe_name)

    try:
        shutil.copy2(msi_path, staged_msi_path)
    except Exception as e:
        logging.critical("CRITICAL: Failed to stage payload in Wine prefix: %s", e)
        return False

    patch_msi_for_wine(staged_msi_path, env)

    wine_executable = get_wine_executable()
    if wine_executable is None:
        return False

    wine_msi_path = f"C:\\linbeam_temp\\{safe_name}"
    wine_log_path = f"C:\\linbeam_temp\\{safe_name}.log"
    cmd = [
        wine_executable,
        "msiexec",
        "/i",
        wine_msi_path,
        "/qn",
        "/norestart",
        "/L*v",
        wine_log_path,
    ]
    if properties:
        cmd.extend(shlex.split(properties))

    try:
        logging.info("Executing msiexec for %s...", original_name)
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

        log_msi_failure_details(host_log_path)
        logging.critical("CRITICAL: Installation failed for %s with exit code: %s", original_name, e.returncode)
        return False
    finally:
        if os.path.exists(staged_msi_path):
            os.remove(staged_msi_path)


def _flatten_single_root_folder(target_dir):
    if not os.path.isdir(target_dir):
        return

    entries = os.listdir(target_dir)
    if len(entries) != 1:
        return

    single_root = os.path.join(target_dir, entries[0])
    if not os.path.isdir(single_root):
        return

    for item in os.listdir(single_root):
        src = os.path.join(single_root, item)
        dst = os.path.join(target_dir, item)
        if os.path.exists(dst):
            if os.path.isdir(dst):
                shutil.rmtree(dst)
            else:
                os.remove(dst)
        shutil.move(src, dst)

    os.rmdir(single_root)


def extract_bundled_webview2(env):
    prefix = env.get("WINEPREFIX")
    if not prefix:
        return False

    drive_c = os.path.join(prefix, "drive_c")
    temp_patterns = [
        os.path.join(drive_c, "users", "*", "Temp"),
        os.path.join(drive_c, "users", "*", "AppData", "Local", "Temp"),
        os.path.join(drive_c, "windows", "temp"),
        os.path.join(drive_c, "windows", "Temp"),
    ]

    temp_dirs = []
    for pattern in temp_patterns:
        for matched in glob.glob(pattern):
            if os.path.isdir(matched) and matched not in temp_dirs:
                temp_dirs.append(matched)

    targets = [
        (
            "Microsoft.WebView2.FixedVersionRuntime.x64.7z",
            os.path.join(
                drive_c,
                "Program Files",
                "Bluebeam Software",
                "Bluebeam Revu",
                "21",
                "Revu",
                "WebView2Runtime",
            ),
        ),
        (
            "Microsoft.WebView2.FixedVersionRuntime.x86.7z",
            os.path.join(
                drive_c,
                "Program Files (x86)",
                "Bluebeam Software",
                "Bluebeam Revu",
                "21",
                "Revu",
                "WebView2Runtime",
            ),
        ),
    ]

    host_7z = get_host_7z_executable()
    wine_executable = get_wine_executable()
    extracted_any = False

    for archive_name, dest_dir in targets:
        archive_path = None
        archive_dir = None
        for temp_dir in temp_dirs:
            candidate = os.path.join(temp_dir, archive_name)
            if os.path.isfile(candidate):
                archive_path = candidate
                archive_dir = temp_dir
                break

        if not archive_path:
            continue

        logging.info("Extracting bundled %s into %s...", archive_name, dest_dir)
        if os.path.isdir(dest_dir):
            shutil.rmtree(dest_dir, ignore_errors=True)
        os.makedirs(dest_dir, exist_ok=True)

        extracted = False
        if host_7z:
            try:
                subprocess.run(
                    [host_7z, "x", archive_path, f"-o{dest_dir}", "-y"],
                    check=True,
                    capture_output=True,
                    text=True,
                )
                extracted = True
            except subprocess.CalledProcessError as exc:
                logging.warning("Host %s extraction failed (%s); trying Wine 7z.exe.", host_7z, exc.returncode)

        if not extracted and wine_executable and archive_dir:
            bundled_7z_exe = os.path.join(archive_dir, "7z.exe")
            if os.path.isfile(bundled_7z_exe):
                try:
                    subprocess.run(
                        [
                            wine_executable,
                            to_wine_path(bundled_7z_exe, env),
                            "x",
                            to_wine_path(archive_path, env),
                            f"-o{to_wine_path(dest_dir, env)}",
                            "-y",
                        ],
                        env=env,
                        check=True,
                        capture_output=True,
                        text=True,
                    )
                    extracted = True
                except subprocess.CalledProcessError as exc:
                    logging.error("Wine 7z.exe failed to extract %s: %s", archive_name, exc)

        if extracted:
            _flatten_single_root_folder(dest_dir)
            extracted_any = True
            logging.info("Bundled WebView2 runtime extracted to %s.", dest_dir)
            try:
                os.remove(archive_path)
            except OSError:
                pass

    return extracted_any


def finalize_bluebeam_admin_setup(env):
    prefix = env.get("WINEPREFIX")
    if not prefix:
        return False

    stage_dir = os.path.join(prefix, "drive_c", "linbeam_temp")
    revu_dir = os.path.join(
        prefix,
        "drive_c",
        "Program Files",
        "Bluebeam Software",
        "Bluebeam Revu",
        "21",
        "Revu",
    )
    if not os.path.isdir(revu_dir):
        return False

    admin_exe = os.path.join(stage_dir, "Bluebeam Admin Install.exe")
    admin_cfg = os.path.join(stage_dir, "Bluebeam Admin Install.exe.config")
    if not os.path.isfile(admin_exe):
        return False

    target_exe = os.path.join(revu_dir, "Bluebeam Admin Install.exe")
    target_cfg = os.path.join(revu_dir, "Bluebeam Admin Install.exe.config")

    try:
        shutil.copy2(admin_exe, target_exe)
        if os.path.isfile(admin_cfg):
            shutil.copy2(admin_cfg, target_cfg)
    except OSError as exc:
        logging.warning("Could not copy Bluebeam Admin Install.exe into Revu folder: %s", exc)
        return False

    wine_executable = get_wine_executable()
    if wine_executable is None:
        return False

    cmd = [
        wine_executable,
        r"C:\Program Files\Bluebeam Software\Bluebeam Revu\21\Revu\Bluebeam Admin Install.exe",
        "/YEAR:21",
        "/DPS:Letter",
        "/FEATURES:65535",
        "/DISABLEANALYTICS",
        "/NODESKREVU",
    ]
    try:
        logging.info("Running Bluebeam Admin Install.exe to generate Safe.enx and finalize settings...")
        subprocess.run(cmd, env=env, check=False, capture_output=True, text=True)
        return True
    except Exception as exc:
        logging.warning("Bluebeam Admin Install.exe encountered an issue: %s", exc)
        return False


def deploy_bluebeam_suite(env, payload_dir="payloads"):
    abs_payload_dir = _resolve_dir(payload_dir)
    revu_msi = os.path.join(abs_payload_dir, "Bluebeam Revu x64 21.msi")
    ocr_msi = os.path.join(abs_payload_dir, "BluebeamOCR x64 21.msi")
    revu_properties = (
        "BB_AUTO_UPDATE=0 DISABLE_WELCOME=1 BB_DISABLEANALYTICS=1 "
        "IGNORE_RBT=1 LANGUAGE_CHOICE=en"
    )

    logging.info("Starting the main Revu install.")
    if not execute_msi(revu_msi, env, revu_properties):
        return False

    extract_bundled_webview2(env)
    finalize_bluebeam_admin_setup(env)

    logging.info("Starting the OCR install.")
    if not execute_msi(ocr_msi, env):
        return False

    return True


def inject_custom_assets(env, assets_dir="assets", profile_name="GilmoreHomesStandard"):
    resolved_assets_dir = _resolve_dir(assets_dir)
    if not os.path.exists(resolved_assets_dir):
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

    for item in os.listdir(resolved_assets_dir):
        source = os.path.join(resolved_assets_dir, item)
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
    try:
        from . import main as _main_mod
    except ImportError:
        import main as _main_mod
    sys.exit(_main_mod.main())
