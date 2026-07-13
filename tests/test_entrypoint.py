import importlib
import unittest


class EntryPointTests(unittest.TestCase):
    def test_package_entrypoint_imports(self):
        module = importlib.import_module("bluebeam_installer.main")
        self.assertTrue(callable(module.main))


if __name__ == "__main__":
    unittest.main()
