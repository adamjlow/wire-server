#!/usr/bin/env python3
"""Compare live initial proxy captures; success is coverage-specific, not full parity."""
import argparse
from datetime import datetime, timedelta, timezone
from email.utils import parsedate_to_datetime, format_datetime
import hashlib
import importlib.util
import json
from pathlib import Path
import sys

import capture

SPEC = importlib.util.spec_from_file_location("parity_foundation", Path(__file__).resolve().parents[5] / "tools/parity/compare.py")
foundation = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(foundation)
require = foundation.require


def response_view(response, start, finish, method):
    foundation.fields(response, "status headers body_base64 protocol content_length transfer_encoding close uncompressed trailers")
    require(type(response["status"]) is int and 200 <= response["status"] <= 599, "invalid status")
    payload = foundation.body(response["body_base64"])
    headers = foundation.headers([[k, v] for k, values in response["headers"].items() for v in values])
    require(response["protocol"] == "HTTP/1.1", "unexpected protocol")
    require(response["uncompressed"] is False and type(response["close"]) is bool, "unexpected decompression/connection metadata")
    length = response["content_length"]
    transfer = response["transfer_encoding"]
    require(type(length) is int and length >= -1 and transfer in ([], ["chunked"]), "invalid framing")
    if transfer:
        require(length == -1 and "content-length" not in headers, "ambiguous chunked framing")
    elif length < 0:
        require(response["close"] is True, "unframed persistent response")
    if "content-length" in headers:
        require(headers["content-length"] == [str(length)] and length >= 0, "inconsistent content length")
    if length >= 0 and method != "HEAD" and response["status"] not in (204, 304):
        require(length == len(payload), "content length does not match body")
    dates = headers.get("date")
    require(type(dates) is list and len(dates) == 1, "missing or duplicate Date")
    try:
        date = parsedate_to_datetime(dates[0])
        require(date.tzinfo == timezone.utc and format_datetime(date, usegmt=True) == dates[0], "invalid HTTP Date")
        require(start - timedelta(seconds=2) <= date <= finish + timedelta(seconds=2), "Date outside capture window")
    except (TypeError, ValueError, OverflowError) as error:
        raise foundation.Invalid("invalid HTTP Date") from error
    # Only validated Date and equivalent framing vary. Every application header,
    # ordered duplicate and trailer remains in the comparison.
    headers.pop("date")
    headers.pop("content-length", None)
    trailers = foundation.headers([[k, v] for k, values in (response["trailers"] or {}).items() for v in values])
    return {"status": response["status"], "headers": headers, "body": payload,
            "close": response["close"], "trailers": trailers}


def compare(report):
    require(report.get("evidence_kind") == "initial-live-proxy-comparison-capture" and report.get("parity_verified") is False, "wrong capture kind")
    require(report.get("capture_complete") is True and report.get("source_status_mismatches") == 0 and
            not report.get("failure_category") and not report.get("cleanup_failed"), "incomplete or failed capture")
    require(report.get("source_revision") == capture.SOURCE and report.get("reference_image") == capture.REFERENCE, "reference identity mismatch")
    for k in ("runner_sha256", "provider_binary_sha256", "provider_source_sha256", "config_sha256", "test_ca_sha256"):
        foundation.digest(report.get(k))
    foundation.digest(report.get("fixture_revision"), foundation.SHA)
    candidate = report.get("candidate", {})
    foundation.digest(candidate.get("source_revision"), foundation.SHA)
    foundation.digest(candidate.get("binary_sha256"))
    require(candidate.get("image_id", "").startswith("sha256:"), "missing immutable candidate image")
    foundation.digest(candidate["image_id"][7:])
    expected = capture.scenarios()
    scenario_hash = hashlib.sha256(json.dumps(expected, sort_keys=True).encode()).hexdigest()
    require(report.get("scenario_sha256") == scenario_hash, "scenario identity mismatch")
    cases = foundation.indexed(report.get("cases"))
    require(set(cases) == {c["id"] for c in expected}, "missing or additional cases")
    start, finish = (datetime.fromisoformat(report[k]) for k in ("started_at", "finished_at"))
    require(start.tzinfo is not None and finish >= start, "invalid capture window")
    results = []
    for stimulus in expected:
        case = cases[stimulus["id"]]
        require(all(foundation.equal(case.get(k), v) for k, v in stimulus.items()), "changed stimulus")
        require(case.get("source_status_matched") is True and case["response"]["status"] == stimulus["expected_status"], "source status mismatch")
        left = response_view(case["response"], start, finish, case["method"])
        right = response_view(case["candidate_response"], start, finish, case["method"])
        differences = [k for k in left if not foundation.equal(left[k], right[k])]
        require(type(case.get("effects")) is list and type(case.get("candidate_effects")) is list, "missing effects")
        if not foundation.equal(case["effects"], case["candidate_effects"]):
            differences.append("effects")
        results.append({"id": case["id"], "result": "fail" if differences else "pass", "differences": differences})
    return {"schema_version": 1, "parity_verified": False, "coverage": "initial-live-proxy-cases",
            "reference_source": report["source_revision"], "reference_image": report["reference_image"],
            "candidate_source": candidate["source_revision"], "candidate_image_id": candidate["image_id"],
            "fixture_revision": report["fixture_revision"], "cases": results,
            "passed": sum(c["result"] == "pass" for c in results),
            "failed": sum(c["result"] == "fail" for c in results)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("capture", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    try:
        data, _ = foundation.load_file(args.capture)
        result = compare(data)
        result["capture_sha256"] = capture.sha(args.capture)
        result["comparator_sha256"] = capture.sha(__file__)
        with args.output.open("x") as output:
            json.dump(result, output, indent=2)
            output.write("\n")
        print(f"Initial cases: {result['passed']} pass, {result['failed']} fail; full parity remains unverified")
        return int(result["failed"] > 0)
    except (ValueError, OSError, KeyError, TypeError) as error:
        print("Invalid/incomplete capture or unusable output:", type(error).__name__, file=sys.stderr)
        return 2

if __name__ == "__main__":
    raise SystemExit(main())
