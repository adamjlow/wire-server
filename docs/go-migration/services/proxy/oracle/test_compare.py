"""Comparison rules reject contract changes; these are runner tests, not parity."""
import base64
import copy
from datetime import datetime, timezone
import hashlib
import json
import unittest

import capture
import compare


def synthetic_report():
    cases = capture.scenarios()
    report = {"evidence_kind": "initial-live-proxy-comparison-capture", "parity_verified": False,
              "capture_complete": True, "source_status_mismatches": 0,
              "source_revision": capture.SOURCE, "reference_image": capture.REFERENCE,
              "fixture_revision": "a" * 40, "candidate": {"source_revision": "b" * 40,
              "binary_sha256": "c" * 64, "image_id": "sha256:" + "d" * 64},
              "started_at": "2026-10-07T18:00:00+00:00", "finished_at": "2026-10-07T18:00:02+00:00",
              "scenario_sha256": hashlib.sha256(json.dumps(cases, sort_keys=True).encode()).hexdigest()}
    for key in ("runner_sha256", "provider_binary_sha256", "provider_source_sha256", "config_sha256", "test_ca_sha256"):
        report[key] = "e" * 64
    for c in cases:
        c["response"] = {"status": c["expected_status"], "headers": {"Date": ["Wed, 07 Oct 2026 18:00:01 GMT"], "Content-Type": ["a", "b"]},
                         "body_base64": base64.b64encode(b"synthetic").decode(), "protocol": "HTTP/1.1",
                         "content_length": -1, "transfer_encoding": ["chunked"], "close": False,
                         "uncompressed": False, "trailers": None}
        c["candidate_response"] = copy.deepcopy(c["response"])
        c["candidate_response"]["headers"]["Content-Length"] = ["9"]
        c["candidate_response"]["content_length"] = 9
        c["candidate_response"]["transfer_encoding"] = []
        c["source_status_matched"] = True
        effect = {"method": "GET", "target": "/synthetic?a=1&a=2", "headers": {"Authorization": ["synthetic-secret"]}, "body": ""}
        c["effects"] = [effect]
        c["candidate_effects"] = copy.deepcopy(c["effects"])
    report["cases"] = cases
    return report


class ComparisonTests(unittest.TestCase):
    def test_equivalent_validated_framing_does_not_claim_full_parity(self):
        result = compare.compare(synthetic_report())
        self.assertEqual(65, result["passed"])
        self.assertEqual(0, result["failed"])
        self.assertFalse(result["parity_verified"])

    def test_changed_status_body_application_header_and_duplicate_order_fail(self):
        for key, value in (("status", 201), ("body_base64", base64.b64encode(b"different").decode()),
                           ("headers", {"Date": ["Wed, 07 Oct 2026 18:00:01 GMT"], "Content-Type": ["b", "a"], "Content-Length": ["9"]})):
            report = synthetic_report()
            report["cases"][0]["candidate_response"][key] = value
            result = compare.compare(report)
            self.assertEqual(1, result["failed"])

    def test_extra_header_and_missing_effect_or_credential_fail(self):
        for mutation in ("header", "effect", "credential", "target"):
            report = synthetic_report()
            c = report["cases"][0]
            if mutation == "header": c["candidate_response"]["headers"]["X-Extra"] = ["changed"]
            if mutation == "effect": c["candidate_effects"] = []
            if mutation == "credential": c["candidate_effects"][0]["headers"]["Authorization"] = ["wrong"]
            if mutation == "target": c["candidate_effects"][0]["target"] = "/synthetic?a=2&a=1"
            self.assertEqual(1, compare.compare(report)["failed"])

    def test_invalid_framing_and_date_cannot_be_normalized(self):
        for mutation in ("length", "chunked", "date", "weekday", "window", "decompressed"):
            report = synthetic_report()
            r = report["cases"][0]["candidate_response"]
            if mutation == "length": r["headers"]["Content-Length"] = ["8"]
            if mutation == "chunked": r["transfer_encoding"] = ["chunked"]
            if mutation == "date": r["headers"]["Date"] = ["unparseable"]
            if mutation == "weekday": r["headers"]["Date"] = ["Mon, 07 Oct 2026 18:00:01 GMT"]
            if mutation == "window": r["headers"]["Date"] = ["Wed, 07 Oct 2026 19:00:01 GMT"]
            if mutation == "decompressed": r["uncompressed"] = True
            with self.assertRaises(ValueError): compare.compare(report)

    def test_changed_stimuli_identity_or_missing_cases_are_invalid(self):
        for mutation in ("stimulus", "source", "cases", "incomplete", "cleanup"):
            report = synthetic_report()
            if mutation == "stimulus": report["cases"][0]["target"] = "/other"
            if mutation == "source": report["source_revision"] = "f" * 40
            if mutation == "cases": report["cases"].pop()
            if mutation == "incomplete": report["capture_complete"] = False
            if mutation == "cleanup": report["cleanup_failed"] = ["owned-container"]
            with self.assertRaises(ValueError): compare.compare(report)

if __name__ == "__main__": unittest.main()
