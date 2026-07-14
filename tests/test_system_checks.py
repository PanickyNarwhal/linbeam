import unittest
from unittest.mock import patch

from bluebeam_installer import system_checks


class SystemChecksTests(unittest.TestCase):
    @patch("bluebeam_installer.system_checks.shutil.which")
    def test_check_dependencies_accepts_wine_when_wine64_is_missing(self, which):
        def fake_which(binary):
            if binary == "wine":
                return "/usr/bin/wine"
            if binary in {"winetricks", "cabextract"}:
                return f"/usr/bin/{binary}"
            return None

        which.side_effect = fake_which

        self.assertEqual(system_checks.check_dependencies(), [])


if __name__ == "__main__":
    unittest.main()
