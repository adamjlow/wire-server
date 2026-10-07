import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("candidate_check", Path(__file__).with_name("check.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class CandidateChecks(unittest.TestCase):
    def fixture(self, root):
        module = root / "services/proxy"
        module.mkdir(parents=True)
        (module / "go.mod").write_text("module test\n")
        (module / "main.go").write_text("package main\n")
        (root / "toolchain.json").write_text(json.dumps({"version": "1.26.8"}))
        (root / ".go-version").write_text("1.26.8\n")
        return module

    def test_unpinned_compiler_fails_before_checks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.fixture(root)
            with patch.object(check, "run", return_value="go1.27.1\n") as run:
                with self.assertRaisesRegex(check.CheckError, "expected Go"):
                    check.check("proxy", root)
                self.assertEqual(run.call_count, 1)

    def test_failed_vet_stops_before_tests_and_scanners(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.fixture(root)
            calls = []

            def run(args, cwd, env, capture=False):
                calls.append(args)
                if args[:2] == ["go", "env"]:
                    return "go1.26.8\n"
                if args[:2] == ["go", "vet"]:
                    raise check.CheckError("vet failed")
                return ""

            with patch.object(check, "run", side_effect=run):
                with self.assertRaisesRegex(check.CheckError, "vet failed"):
                    check.check("proxy", root)
            self.assertFalse(any(args[:2] == ["go", "test"] for args in calls))
            self.assertFalse(any("govulncheck" in args[0] for args in calls))

    def test_missing_module_fails_before_invoking_tools(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(check, "run") as run:
            with self.assertRaisesRegex(check.CheckError, "unavailable"):
                check.check("proxy", Path(tmp))
            run.assert_not_called()

    def test_traversal_and_shell_payloads_are_rejected(self):
        for service in ("../proxy", "proxy;touch /tmp/unwanted", "$(id)", ""):
            with self.subTest(service=service), self.assertRaises(check.CheckError):
                check.module_path(service)

    def test_module_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "services").mkdir()
            (root / "outside").mkdir()
            (root / "outside/go.mod").write_text("module test\n")
            (root / "services/proxy").symlink_to(root / "outside", target_is_directory=True)
            with self.assertRaisesRegex(check.CheckError, "symlinks"):
                check.module_path("proxy", root)

    def test_source_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "main.go").symlink_to("/tmp/outside.go")
            with self.assertRaisesRegex(check.CheckError, "symlinks"):
                check.sources(root)

    def test_empty_sources_are_not_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(check.CheckError, "no Go sources"):
                check.sources(Path(tmp))

    def test_failed_tool_propagates(self):
        with patch.object(check.subprocess, "run", return_value=subprocess.CompletedProcess(["go", "test"], 7)):
            with self.assertRaisesRegex(check.CheckError, "exit 7"):
                check.run(["go", "test"], Path.cwd(), {})


if __name__ == "__main__":
    unittest.main()
