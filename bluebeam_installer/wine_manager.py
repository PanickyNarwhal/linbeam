import logging
import os
import subprocess
import sys

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")


def get_wine_env(prefix_path="~/.local/share/bluebeam-wine"):
    env = os.environ.copy()
    env["WINEPREFIX"] = os.path.expanduser(prefix_path)
    env["WINEARCH"] = "win64"
    env["WINEDEBUG"] = "-all"
    return env


def init_prefix(env):
    prefix = env["WINEPREFIX"]
    logging.info("Setting up a fresh Wine prefix at %s", prefix)

    try:
        subprocess.run(["wineboot", "-u"], env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except FileNotFoundError:
        logging.critical("I could not find wineboot on this machine.")
        return False
    except subprocess.CalledProcessError:
        logging.critical("Wine had a problem while making the prefix.")
        return False

    logging.info("The Wine prefix is ready.")
    return True


def inject_dependencies(env):
    logging.info("Installing the Windows pieces Wine needs for this setup.")
    packages = ["corefonts", "vcrun2022", "dotnet48", "gdiplus"]
    cmd = ["winetricks", "-q"] + packages

    try:
        subprocess.run(cmd, env=env, check=True)
    except FileNotFoundError:
        logging.critical("I could not find winetricks.")
        return False
    except subprocess.CalledProcessError as exc:
        logging.critical("Winetricks stopped with exit code %s", exc.returncode)
        return False

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