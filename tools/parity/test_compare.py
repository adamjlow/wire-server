import base64
from copy import deepcopy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

spec = importlib.util.spec_from_file_location("parity_compare", Path(__file__).with_name("compare.py"))
compare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compare)


def encoded(value):
    return base64.b64encode(value).decode("ascii")


def example():
    """Synthetic schema example only: no service, image or source was executed."""
    def implementation(language, source, image):
        return {"implementation": language, "source_revision": source * 40,
                "image_digest": "sha256:" + image * 64, "config_sha256": "c" * 64,
                "provenance": {"verified": True, "source_revision": source * 40,
                               "image_digest": "sha256:" + image * 64, "receipt_sha256": "d" * 64}}

    manifest = {
        "schema_version": 1, "service": "proxy", "upstream_revision": "a" * 40,
        "contract_revision": "b" * 40, "fixture_revision": "c" * 40,
        "comparator_sha256": hashlib.sha256(Path(compare.__file__).read_bytes()).hexdigest(),
        "seed": 1, "clock": "2026-10-07T00:00:00Z", "data_classification": "synthetic",
        "environment": {"id": "synthetic-self-test", "isolated": True,
                        "reference_endpoint": "http://127.0.0.1:18080",
                        "candidate_endpoint": "http://127.0.0.1:18081"},
        "dependency_images": [],
        "scenarios": [{"id": "search", "steps": [{"id": "request", "body_mode": "bytes",
                                                    "assertions": ["expected-response"]}]}],
        "reference": implementation("haskell", "d", "a"),
        "candidate": implementation("go", "e", "b"),
    }
    step = {"id": "request",
            "request": {"method": "GET", "target": "/proxy/giphy/v1/gifs/search?q=cats",
                        "headers": [["Accept", "application/json"]], "body_base64": ""},
            "response": {"status": 200, "headers": [["Content-Type", "application/json"]],
                         "body_base64": encoded(b'{}')},
            "effects": [{"type": "http", "method": "GET", "path": "/v1/gifs/search",
                         "query": [["api_key", "synthetic-key"], ["q", "cats"]]}],
            "assertions": [{"id": "expected-response", "result": "pass"}]}
    captures = []
    for role in ("reference", "candidate"):
        trace = {key: manifest[role][key] for key in ("implementation", "source_revision", "image_digest", "config_sha256")}
        trace.update(schema_version=1, role=role, contract_revision=manifest["contract_revision"],
                     fixture_revision=manifest["fixture_revision"], data_classification="synthetic",
                     scenarios=[{"id": "search", "result": "pass", "steps": [deepcopy(step)]}])
        captures.append(trace)
    return manifest, *captures


class ComparisonTests(unittest.TestCase):
    def setUp(self):
        self.manifest, self.reference, self.candidate = example()

    def report(self):
        return compare.compare(self.manifest, self.reference, self.candidate)

    def candidate_step(self):
        return self.candidate["scenarios"][0]["steps"][0]

    def reference_step(self):
        return self.reference["scenarios"][0]["steps"][0]

    def test_identical_complete_captures_are_comparison_only(self):
        report = self.report()
        self.assertTrue(report["comparison_passed"])
        self.assertFalse(report["parity_verified"])
        self.assertEqual(report["coverage"], {"required": 1, "pass": 1, "fail": 0, "blocked": 0, "skipped": 0})
        self.assertEqual(report["provenance"], "supplied-declarations-only")

    def test_corrupted_status_body_headers_and_effects_each_fail(self):
        mutations = (
            lambda step: step["response"].update(status=403),
            lambda step: step["response"].update(body_base64=encoded(b'{"wrong":true}')),
            lambda step: step["response"]["headers"].append(["Location", "/wrong-object"]),
            lambda step: step["effects"][0].update(path="/wrong-path"),
        )
        for mutate in mutations:
            with self.subTest(mutation=mutate):
                _, _, self.candidate = example()
                mutate(self.candidate_step())
                self.assertFalse(self.report()["comparison_passed"])
                self.assertEqual(self.report()["coverage"]["fail"], 1)

    def test_changed_stimulus_does_not_establish_parity(self):
        self.candidate_step()["request"]["target"] += "&access_token=unexpected"
        self.assertFalse(self.report()["comparison_passed"])

    def test_header_case_is_protocol_semantics_but_repeated_value_order_is_preserved(self):
        self.candidate_step()["response"]["headers"][0][0] = "content-type"
        self.assertTrue(self.report()["comparison_passed"])
        self.reference_step()["response"]["headers"] += [["Set-Cookie", "a=1"], ["Set-Cookie", "b=2"]]
        self.candidate_step()["response"]["headers"] += [["set-cookie", "b=2"], ["set-cookie", "a=1"]]
        self.assertFalse(self.report()["comparison_passed"])

    def test_date_and_other_headers_are_not_silently_ignored(self):
        self.candidate_step()["response"]["headers"].append(["Date", "test-date"])
        self.assertFalse(self.report()["comparison_passed"])

    def test_query_order_and_duplicate_effects_are_preserved(self):
        original = deepcopy(self.candidate_step()["effects"])
        self.candidate_step()["effects"][0]["query"].reverse()
        self.assertFalse(self.report()["comparison_passed"])
        self.candidate_step()["effects"] = original + deepcopy(original)
        self.assertFalse(self.report()["comparison_passed"])

    def test_side_effect_boolean_is_not_number(self):
        self.reference_step()["effects"] = [{"available": True}]
        self.candidate_step()["effects"] = [{"available": 1}]
        self.assertFalse(self.report()["comparison_passed"])

    def test_only_json_object_member_order_is_permitted(self):
        self.manifest["scenarios"][0]["steps"][0]["body_mode"] = "json-object-order"
        self.reference_step()["response"]["body_base64"] = encoded(b'{"a":1,"b":null}')
        self.candidate_step()["response"]["body_base64"] = encoded(b'{"b":null,"a":1}')
        self.assertTrue(self.report()["comparison_passed"])

    def test_json_semantic_mismatches_and_invalid_bodies_are_not_masked(self):
        self.manifest["scenarios"][0]["steps"][0]["body_mode"] = "json-object-order"
        pairs = [(b'{"a":null}', b'{}'), (b'{"a":true}', b'{"a":1}'),
                 (b'{"a":1}', b'{"a":1.0}'), (b'{"a":1.0}', b'{"a":1e0}'),
                 (b'[1,2]', b'[2,1]'), (b'{"a":1}', b'{"a":"1"}'),
                 (b'{"a":1,"a":2}', b'{"a":2}'), (b'NaN', b'NaN'),
                 (b'{}', '{}'.encode('utf-16'))]
        for left, right in pairs:
            with self.subTest(left=left, right=right):
                self.reference_step()["response"]["body_base64"] = encoded(left)
                self.candidate_step()["response"]["body_base64"] = encoded(right)
                self.assertFalse(self.report()["comparison_passed"])

    def test_unverified_provenance_blocks_even_identical_captures(self):
        self.manifest["reference"]["provenance"].update(verified=False, receipt_sha256=None)
        report = self.report()
        self.assertFalse(report["comparison_passed"])
        self.assertEqual(report["coverage"]["blocked"], 1)

    def test_identity_mismatches_are_rejected(self):
        mutations = (
            lambda: self.candidate.update(source_revision="f" * 40),
            lambda: self.candidate.update(image_digest="sha256:" + "f" * 64),
            lambda: self.candidate.update(config_sha256="f" * 64),
            lambda: self.candidate.update(fixture_revision="f" * 40),
            lambda: self.candidate.update(implementation="haskell"),
            lambda: self.manifest.update(comparator_sha256="f" * 64),
            lambda: self.manifest["reference"]["provenance"].update(source_revision="f" * 40),
        )
        for mutate in mutations:
            with self.subTest(mutation=mutate):
                self.manifest, self.reference, self.candidate = example()
                mutate()
                with self.assertRaises(compare.Invalid):
                    self.report()

    def test_missing_skipped_blocked_and_failed_cases_cannot_pass(self):
        for result in ("skipped", "blocked", "fail"):
            self.candidate["scenarios"][0]["result"] = result
            self.assertFalse(self.report()["comparison_passed"])
        self.candidate["scenarios"] = []
        self.assertEqual(self.report()["coverage"]["blocked"], 1)

    def test_missing_steps_and_assertions_cannot_pass(self):
        self.candidate_step()["assertions"] = []
        self.assertFalse(self.report()["comparison_passed"])
        self.candidate["scenarios"][0]["steps"] = []
        self.assertFalse(self.report()["comparison_passed"])

    def test_step_order_is_not_normalized(self):
        self.manifest["scenarios"][0]["steps"].append({"id": "second", "body_mode": "bytes", "assertions": ["expected-response"]})
        for trace in (self.reference, self.candidate):
            second = deepcopy(trace["scenarios"][0]["steps"][0]); second["id"] = "second"
            trace["scenarios"][0]["steps"].append(second)
        self.candidate["scenarios"][0]["steps"].reverse()
        self.assertFalse(self.report()["comparison_passed"])

    def test_failed_assertion_cannot_hide_under_passed_scenario(self):
        self.candidate_step()["assertions"][0]["result"] = "fail"
        self.assertFalse(self.report()["comparison_passed"])

    def test_empty_required_scenarios_and_steps_are_rejected(self):
        self.manifest["scenarios"][0]["steps"] = []
        with self.assertRaises(compare.Invalid):
            self.report()
        self.manifest["scenarios"] = []
        with self.assertRaises(compare.Invalid):
            self.report()

    def test_duplicate_unknown_scenarios_steps_and_fields_are_rejected(self):
        mutations = (
            lambda: self.candidate["scenarios"].append(deepcopy(self.candidate["scenarios"][0])),
            lambda: self.candidate_step().update(id="unknown"),
            lambda: self.candidate_step().update(exclusion="body"),
            lambda: self.manifest.update(ignore_headers=["Content-Type"]),
        )
        for mutate in mutations:
            with self.subTest(mutation=mutate):
                self.manifest, self.reference, self.candidate = example()
                mutate()
                with self.assertRaises(compare.Invalid):
                    self.report()

    def test_non_loopback_credentials_and_shared_endpoints_are_rejected(self):
        for endpoint in ("http://production.example:8080", "http://localhost:8080", "http://127.0.0.1:8080/path",
                         "http://name:password@127.0.0.1:8080", "http://127.0.0.1:80", "http://127.0.0.1:18080"):
            with self.subTest(endpoint=endpoint):
                self.manifest["environment"]["candidate_endpoint"] = endpoint
                with self.assertRaises(compare.Invalid):
                    self.report()

    def test_production_and_nonisolated_declarations_are_rejected(self):
        self.manifest["data_classification"] = "production"
        with self.assertRaises(compare.Invalid):
            self.report()
        self.manifest["data_classification"] = "synthetic"
        self.manifest["environment"]["isolated"] = False
        with self.assertRaises(compare.Invalid):
            self.report()

    def test_body_header_status_and_content_length_validation(self):
        for change in (lambda response: response.update(body_base64="!!!"),
                       lambda response: response.update(status=True),
                       lambda response: response["headers"].append(["Content-Length", "99"]),
                       lambda response: response["headers"].append(["Bad\nName", "bad"]),
                       lambda response: response["headers"].append(["X-Test", "bad\r\nvalue"])):
            self.manifest, self.reference, self.candidate = example()
            change(self.candidate_step()["response"])
            with self.assertRaises(compare.Invalid):
                self.report()

    def test_head_entity_length_is_not_mistaken_for_body_size(self):
        for step in (self.reference_step(), self.candidate_step()):
            step["request"]["method"] = "HEAD"
            step["response"]["headers"].append(["Content-Length", "500"])
            step["response"]["body_base64"] = ""
        self.assertTrue(self.report()["comparison_passed"])

    def test_reports_do_not_echo_capture_values(self):
        marker = "synthetic-sensitive-marker"
        self.candidate_step()["response"]["headers"].append(["Authorization", marker])
        self.candidate_step()["response"]["body_base64"] = encoded(marker.encode())
        self.candidate_step()["effects"].append({"credential": marker})
        report = self.report()
        self.assertFalse(report["comparison_passed"])
        self.assertNotIn(marker, json.dumps(report))
        self.assertNotIn(encoded(marker.encode()), json.dumps(report))

    def test_parser_rejects_duplicates_nonfinite_overflow_and_huge_metadata_integers(self):
        for data in ('{"a":1,"a":2}', '{"a":NaN}', '{"a":1e999}', '{"a":' + '1' * 1000 + '}'):
            with self.subTest(data=data[:30]), self.assertRaises(compare.Invalid):
                compare.parse(data)

    def test_candidate_cannot_claim_this_comparator(self):
        root = Path(__file__).resolve().parents[2]
        validator_spec = importlib.util.spec_from_file_location("migration_policy", root / "tools/migration/check.py")
        validator = importlib.util.module_from_spec(validator_spec)
        validator_spec.loader.exec_module(validator)
        services = validator.load(root, "services")
        # Synthetic implementing state isolates oracle scope from lifecycle checks;
        # no live service state or evidence is changed by this unit test.
        services["proxy"]["stage"] = "implementing"
        task = {"schema_version": 1, "id": "unsafe-candidate", "title": "Synthetic scope rejection",
                "service": "proxy", "kind": "implementation", "status": "active",
                "owner": "synthetic-test", "branch": "synthetic-test", "depends_on": [],
                "write_paths": [".migration/tasks/unsafe-candidate.json", "tools/parity"],
                "plan": "docs/go-migration/plans/parity-comparator.md",
                "acceptance_criteria": [{"id": "test", "description": "Synthetic", "required_evidence": "Synthetic"}],
                "evidence": [], "review": None, "handoff": "Synthetic test only"}
        with self.assertRaisesRegex(validator.Invalid, "protected oracle"):
            validator.task_shape(root, task["id"], task, services)


class CliTests(unittest.TestCase):
    def inputs(self, directory):
        paths = [Path(directory) / name for name in ("manifest.json", "reference.json", "candidate.json")]
        for path, data in zip(paths, example()):
            path.write_text(json.dumps(data))
        return [str(path) for path in paths]

    def invoke(self, args):
        with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
            return compare.main(args)

    def test_success_report_is_comparison_only_and_existing_report_is_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = self.inputs(tmp)
            destination = Path(tmp) / "report.json"
            self.assertEqual(self.invoke([*args, str(destination)]), 0)
            report = json.loads(destination.read_text())
            self.assertFalse(report["parity_verified"])
            self.assertEqual(set(report["input_sha256"]), {"manifest", "reference", "candidate"})
            original = destination.read_bytes()
            self.assertEqual(self.invoke([*args, str(destination)]), 2)
            self.assertEqual(destination.read_bytes(), original)

    def test_missing_capture_returns_nonzero_with_blocked_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = self.inputs(tmp)
            path = Path(args[2]); data = json.loads(path.read_text()); data["scenarios"] = []
            path.write_text(json.dumps(data))
            destination = Path(tmp) / "report.json"
            self.assertEqual(self.invoke([*args, str(destination)]), 1)
            self.assertEqual(json.loads(destination.read_text())["coverage"]["blocked"], 1)

    def test_symlink_input_and_report_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            args = self.inputs(tmp)
            link = Path(tmp) / "link.json"; link.symlink_to(args[0])
            self.assertEqual(self.invoke([str(link), *args[1:], str(Path(tmp) / "report.json")]), 2)
            self.assertEqual(self.invoke([*args, str(link)]), 2)

    def test_nonregular_and_oversized_input_are_rejected_without_blocking(self):
        with tempfile.TemporaryDirectory() as tmp:
            fifo = Path(tmp) / "fifo"; os.mkfifo(fifo)
            with self.assertRaises(compare.Invalid):
                compare.load_file(fifo)
            large = Path(tmp) / "large.json"
            with large.open("wb") as stream:
                stream.truncate(compare.MAX_FILE + 1)
            with self.assertRaises(compare.Invalid):
                compare.load_file(large)


if __name__ == "__main__":
    unittest.main()
