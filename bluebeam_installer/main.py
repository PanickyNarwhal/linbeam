import logging
import os
import stat
import sys

import installer
import system_checks
import wine_manager

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")


def generate_desktop_shortcut(env):
    prefix = env.get("WINEPREFIX")
    if not prefix:
        logging.error("The Wine prefix was not set, so I could not make a shortcut.")
        return False

    shortcut_dir = os.path.expanduser("~/.local/share/applications")
    os.makedirs(shortcut_dir, exist_ok=True)

    desktop_file = os.path.join(shortcut_dir, "bluebeam-revu.desktop")
    exe_path = f"{prefix}/drive_c/Program Files/Bluebeam Software/Bluebeam Revu/21/Revu/Revu.exe"

    desktop_entry = f"""[Desktop Entry]
Name=Bluebeam Revu 21
Comment=PDF Markup and Editing
Exec=env WINEPREFIX=\"{prefix}\" WINEARCH=\"win64\" wine \"{exe_path}\"
Type=Application
Categories=Office;
Terminal=false
Icon=bluebeam-revu
"""

    try:
        with open(desktop_file, "w", encoding="utf-8") as handle:
            handle.write(desktop_entry)

        mode = os.stat(desktop_file).st_mode
        os.chmod(desktop_file, mode | stat.S_IEXEC)
        logging.info("The desktop shortcut is in place at %s", desktop_file)
        return True
    except Exception as exc:
        logging.error("I had trouble writing the desktop shortcut: %s", exc)
        return False


def main():
    logging.info("Starting the Bluebeam Revu 21 setup.")

    if not system_checks.run_system_flight_check():
        logging.critical("The system checks failed, so I am stopping here.")
        return 1

    env = wine_manager.build_wine_environment()
    if not env:
        logging.critical("The Wine setup did not finish, so I am stopping here.")
        return 1

    if not installer.run_installation(env):
        logging.critical("The Bluebeam install did not finish, so I am stopping here.")
        return 1

    generate_desktop_shortcut(env)

    logging.info("The setup is done. You should be able to start Bluebeam from your app menu.")
    return 0


if __name__ == "__main__":
    sys.exit(main())