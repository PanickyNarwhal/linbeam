import os
import subprocess
import unittest
from unittest.mock import patch

from bluebeam_installer import installer


class InstallerTests(unittest.TestCase):
    @patch("bluebeam_installer.installer.shutil.which")
    def test_get_wine_executable_prefers_wine64(self, which):
        which.side_effect = lambda arg: "/usr/bin/wine64" if arg == "wine64" else "/usr/bin/wine"

        self.assertEqual(installer.get_wine_executable(), "wine64")

    @patch("bluebeam_installer.installer.shutil.which", return_value="/usr/bin/winepath")
    @patch("bluebeam_installer.installer.subprocess.run")
    def test_to_wine_path_uses_winepath_when_available(self, run, which):
        run.return_value = subprocess.CompletedProcess(
            args=["winepath", "-w", "/tmp/Bluebeam.msi"],
            returncode=0,
            stdout="Z:\\tmp\\Bluebeam.msi\n",
            stderr="",
        )

        self.assertEqual(installer.to_wine_path("/tmp/Bluebeam.msi", env={}), "Z:\\tmp\\Bluebeam.msi")
        run.assert_called_once_with(
            ["winepath", "-w", "/tmp/Bluebeam.msi"],
            check=True,
            capture_output=True,
            text=True,
            env={},
        )

    def test_execute_msi_uses_wine64_and_shlex_properties(self):
        env = {"WINEPREFIX": "/home/user/.local/share/bluebeam-wine"}
        msi_path = "/tmp/Bluebeam Revu x64 21.msi"
        safe_name = "Bluebeam_Revu_x64_21.msi"
        staged_msi_path = os.path.join(env["WINEPREFIX"], "drive_c", "linbeam_temp", safe_name)
        wine_path = r"C:\linbeam_temp\Bluebeam_Revu_x64_21.msi"

        with patch("bluebeam_installer.installer.get_wine_executable", return_value="wine64") as get_wine_executable, \
             patch("bluebeam_installer.installer.shutil.copy2") as copy2, \
             patch("bluebeam_installer.installer.os.makedirs") as makedirs, \
             patch("bluebeam_installer.installer.os.path.exists", return_value=True) as exists, \
             patch("bluebeam_installer.installer.os.remove") as remove, \
             patch("bluebeam_installer.installer.subprocess.run") as run:
            run.return_value = subprocess.CompletedProcess(
                args=["wine64", "msiexec", "/i", wine_path],
                returncode=0,
                stdout="",
                stderr="",
            )

            success = installer.execute_msi(
                msi_path,
                env,
                "BB_AUTO_UPDATE=0 DISABLE_WELCOME=1",
            )

            self.assertTrue(success)
            run.assert_called_once_with(
                [
                    "wine64",
                    "msiexec",
                    "/i",
                    wine_path,
                    "/qn",
                    "/norestart",
                    "BB_AUTO_UPDATE=0",
                    "DISABLE_WELCOME=1",
                ],
                env=env,
                check=True,
                capture_output=True,
                text=True,
            )
            copy2.assert_called_once_with(msi_path, staged_msi_path)


if __name__ == "__main__":
    unittest.main()
