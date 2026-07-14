import subprocess
import unittest
from unittest.mock import patch

from bluebeam_installer import installer


class InstallerTests(unittest.TestCase):
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


if __name__ == "__main__":
    unittest.main()
