import os
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from bluebeam_installer import installer, wine_manager


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
        wine_log_path = r"C:\linbeam_temp\Bluebeam_Revu_x64_21.msi.log"

        with patch("bluebeam_installer.installer.get_wine_executable", return_value="wine64"), \
             patch("bluebeam_installer.installer.patch_msi_for_wine", return_value=True) as patch_msi, \
             patch("bluebeam_installer.installer.shutil.copy2") as copy2, \
             patch("bluebeam_installer.installer.os.makedirs"), \
             patch("bluebeam_installer.installer.os.path.exists", return_value=True), \
             patch("bluebeam_installer.installer.os.remove"), \
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
            patch_msi.assert_called_once_with(staged_msi_path, env)
            run.assert_called_once_with(
                [
                    "wine64",
                    "msiexec",
                    "/i",
                    wine_path,
                    "/qn",
                    "/norestart",
                    "/L*v",
                    wine_log_path,
                    "BB_AUTO_UPDATE=0",
                    "DISABLE_WELCOME=1",
                ],
                env=env,
                check=True,
                capture_output=True,
                text=True,
            )
            copy2.assert_called_once_with(msi_path, staged_msi_path)

    def test_flatten_single_root_folder_moves_nested_contents_up(self):
        temp_root = tempfile.mkdtemp()
        try:
            nested = os.path.join(temp_root, "Microsoft.WebView2.FixedVersionRuntime.x64")
            os.makedirs(os.path.join(nested, "ebwebview"), exist_ok=True)
            marker = os.path.join(nested, "msedgewebview2.exe")
            with open(marker, "w", encoding="utf-8") as handle:
                handle.write("stub")

            installer._flatten_single_root_folder(temp_root)

            self.assertTrue(os.path.isfile(os.path.join(temp_root, "msedgewebview2.exe")))
            self.assertTrue(os.path.isdir(os.path.join(temp_root, "ebwebview")))
            self.assertFalse(os.path.exists(nested))
        finally:
            shutil.rmtree(temp_root, ignore_errors=True)

    def test_wine_env_includes_webview2_sandbox_args(self):
        env = wine_manager.get_wine_env("/tmp/test-prefix")
        self.assertIn("--no-sandbox", env.get("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", ""))


if __name__ == "__main__":
    unittest.main()
