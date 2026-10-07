"""Unit tests for synthetic reference orchestration; Docker is never launched."""

import importlib.util
from pathlib import Path
import tempfile
import json
import subprocess
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("proxy_capture", Path(__file__).with_name("capture.py"))
capture = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(capture)


class BoundaryTests(unittest.TestCase):
    def test_case_ids_unique_and_complete_provider_version_baseline(self):
        cases = capture.scenarios()
        ids = {case["id"] for case in cases}
        self.assertEqual(len(ids), len(cases))
        for prefix in ("giphy", "youtube", "staticmap", "geocode", "spotify", "resolve", "stream"):
            self.assertTrue(any(value.startswith(prefix + "-") for value in ids))
        for version in range(20):
            self.assertIn("version-" + str(version), ids)
        self.assertIn("stream-http-rejected", ids)
        self.assertIn("stream-wrong-host", ids)

    def test_external_roles_and_container_targets_rejected_before_command(self):
        for container, role in (("a" * 64, "production"), ("localhost", "reference"), ("http://production.invalid", "control")):
            with self.assertRaises(ValueError):
                capture.request(container, role, "GET", "/")

    def test_client_uses_argument_vector_and_body_stays_stdin_data(self):
        with patch.object(capture, "command", return_value='{"status":200}') as command:
            self.assertEqual({"status": 200}, capture.request("a" * 64, "control", "POST", "/reset", body=b"synthetic"))
            args, kwargs = command.call_args
            self.assertEqual(["docker", "exec", "--interactive", "a" * 64, "/provider", "-client", "control"], args[0])
            self.assertIn('"target": "/reset"', kwargs["input"])

    def test_artifacts_and_binary_cannot_follow_symlinks(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "binary"
            binary.write_bytes(b"synthetic")
            alias = root / "alias"
            alias.symlink_to(binary)
            with self.assertRaises(ValueError):
                capture.capture(alias, root / "output")
            parent = root / "parent"
            parent.symlink_to(root, target_is_directory=True)
            with self.assertRaises(ValueError):
                capture.capture(binary, parent / "output")

    def test_existing_artifact_directory_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "binary"
            binary.write_bytes(b"synthetic")
            with patch.object(capture, "command") as command:
                with self.assertRaises(FileExistsError):
                    capture.capture(binary, Path(directory))
                command.assert_not_called()

    def test_failed_reference_launch_cleans_only_its_owned_resources(self):
        calls = []

        def command(args, **kwargs):
            calls.append(args)
            if args[-1] == "-build-info":
                return json.dumps({"GoVersion": "go1.26.8", "Path": "wire.local/proxy-reference-provider"})
            if args[:3] == ["git", "rev-parse", "HEAD"]:
                return "a" * 40
            if args[:2] == ["docker", "build"]:
                return "sha256:" + "b" * 64
            if args[:3] == ["docker", "network", "inspect"]:
                return '[{"Internal":true}]'
            if args[:2] == ["docker", "run"]:
                name = args[args.index("--name") + 1]
                if name.endswith("-reference"):
                    raise subprocess.CalledProcessError(1, args)
                return "c" * 64
            return ""

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "binary"
            binary.write_bytes(b"synthetic")
            with patch.object(capture, "command", side_effect=command), \
                    patch.object(capture, "ready"), patch.object(capture, "sha", return_value="d" * 64), \
                    patch.object(capture.subprocess, "run", return_value=SimpleNamespace(stdout="", stderr="")):
                with self.assertRaises(subprocess.CalledProcessError):
                    capture.capture(binary, root / "output")
            self.assertIn(["docker", "rm", "--force", "c" * 64], calls)
            removals = [args for args in calls if args[:3] == ["docker", "network", "rm"]]
            self.assertEqual(1, len(removals))
            self.assertTrue(removals[0][3].startswith("wire-proxy-reference-"))
            report = json.loads((root / "output/reference.json").read_text())
            self.assertFalse(report["parity_verified"])
            self.assertEqual([], report["cases"])
            self.assertEqual("CalledProcessError", report["failure_category"])


if __name__ == "__main__":
    unittest.main()
