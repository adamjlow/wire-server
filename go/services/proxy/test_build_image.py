"""Image packaging boundary tests; Docker and compilation are never launched."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("proxy_build_image", Path(__file__).with_name("build_image.py"))
build_image = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(build_image)


class BuildBoundaryTests(unittest.TestCase):
    def test_receipts_and_symlink_parents_cannot_overwrite_files(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            existing = root / "receipt.json"
            existing.write_text("existing")
            with self.assertRaises(ValueError): build_image.valid_output(existing)
            parent = root / "alias"
            parent.symlink_to(root, target_is_directory=True)
            with self.assertRaises(ValueError): build_image.valid_output(parent / "new.json")
            self.assertEqual("existing", existing.read_text())

    def test_existing_ca_receipt_rejected_before_any_commands(self):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "receipt.json"
            p.with_suffix(".ca.json").write_text("existing")
            with patch.object(build_image, "command") as command:
                with self.assertRaises(ValueError): build_image.build(p)
                command.assert_not_called()

    def test_dirty_checkout_cannot_produce_clean_image_evidence(self):
        with tempfile.TemporaryDirectory() as d, patch.object(build_image, "command", return_value="modified") as command:
            with self.assertRaises(ValueError): build_image.build(Path(d)/"receipt.json")
            self.assertEqual(1, command.call_count)

if __name__ == "__main__": unittest.main()
