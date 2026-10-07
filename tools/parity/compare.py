#!/usr/bin/env python3
"""Compare bounded synthetic captures offline; never claim verified runtime parity."""

import argparse
import base64
import binascii
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
import sys
from urllib.parse import urlsplit
import uuid

MAX_FILE = 32 * 1024 * 1024
MAX_BODY = 8 * 1024 * 1024
SHA = re.compile(r"[a-f0-9]{40}\Z")
HASH = re.compile(r"[a-f0-9]{64}\Z")
ID = re.compile(r"[a-z0-9]+(?:-[a-z0-9]+)*\Z")
TOKEN = re.compile(r"[!#$%&'*+.^_`|~0-9A-Za-z-]+\Z")
RESULTS = {"pass", "fail", "blocked", "skipped"}


class Invalid(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Invalid(message)


def fields(value, names):
    require(type(value) is dict and set(value) == set(names.split()), "invalid object fields")


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON member")
        result[key] = value
    return result


def parse(data, **kwargs):
    def bad_constant(_):
        raise Invalid("non-finite JSON number")
    def bounded_int(text):
        require(len(text) <= 21, "JSON integer exceeds metadata limit")
        return int(text)
    def finite_float(text):
        require(len(text) <= 128, "JSON float exceeds metadata limit")
        value = float(text)
        require(math.isfinite(value), "non-finite JSON number")
        return value
    options = {"parse_int": bounded_int, "parse_float": finite_float, **kwargs}
    try:
        return json.loads(data, object_pairs_hook=unique, parse_constant=bad_constant, **options)
    except (ValueError, UnicodeError, RecursionError) as error:
        raise Invalid("invalid JSON input") from error


def digest(value, pattern=HASH):
    require(type(value) is str and pattern.fullmatch(value) is not None, "invalid pinned digest")


def identifier(value):
    require(type(value) is str and len(value) <= 80 and ID.fullmatch(value), "invalid identifier")


def indexed(items):
    require(type(items) is list and len(items) <= 10000, "invalid observation list")
    result = {}
    for item in items:
        require(type(item) is dict and "id" in item, "observation requires id")
        identifier(item["id"])
        require(item["id"] not in result, "duplicate observation id")
        result[item["id"]] = item
    return result


def endpoint(value):
    require(type(value) is str and not any(c.isspace() for c in value), "invalid test endpoint")
    try:
        url = urlsplit(value)
        require(url.scheme in {"http", "https"} and url.hostname in {"127.0.0.1", "::1"}
                and url.port is not None and 1024 <= url.port <= 65535
                and not url.username and not url.password and url.path in {"", "/"}
                and not url.query and not url.fragment, "endpoint must be literal loopback")
    except ValueError as error:
        raise Invalid("invalid test endpoint") from error


def implementation(value, language):
    fields(value, "implementation source_revision image_digest config_sha256 provenance")
    require(value["implementation"] == language, "wrong reference/candidate language")
    digest(value["source_revision"], SHA)
    require(type(value["image_digest"]) is str and value["image_digest"].startswith("sha256:"),
            "image requires immutable digest")
    digest(value["image_digest"][7:])
    digest(value["config_sha256"])
    proof = value["provenance"]
    fields(proof, "verified source_revision image_digest receipt_sha256")
    require(type(proof["verified"]) is bool, "provenance must declare verified boolean")
    require(proof["source_revision"] == value["source_revision"]
            and proof["image_digest"] == value["image_digest"], "provenance identity mismatch")
    if proof["verified"] or proof["receipt_sha256"] is not None:
        digest(proof["receipt_sha256"])


def manifest_shape(value):
    fields(value, "schema_version service upstream_revision contract_revision fixture_revision "
           "comparator_sha256 seed clock environment dependency_images scenarios reference candidate "
           "data_classification")
    require(type(value["schema_version"]) is int and value["schema_version"] == 1, "unsupported schema")
    require(value["service"] in {"proxy", "cargohold"}, "unsupported service")
    require(value["data_classification"] == "synthetic", "only synthetic captures are permitted")
    for key in ("upstream_revision", "contract_revision", "fixture_revision"):
        digest(value[key], SHA)
    digest(value["comparator_sha256"])
    require(value["comparator_sha256"] == hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "comparator digest mismatch")
    require(type(value["seed"]) is int and 0 <= value["seed"] < 2**64, "invalid deterministic seed")
    require(type(value["clock"]) is str and re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z", value["clock"]),
            "clock must be explicit UTC")
    try:
        datetime.strptime(value["clock"], "%Y-%m-%dT%H:%M:%SZ")
    except ValueError as error:
        raise Invalid("invalid UTC clock") from error
    env = value["environment"]
    fields(env, "id isolated reference_endpoint candidate_endpoint")
    identifier(env["id"])
    require(env["isolated"] is True, "environment must be isolated")
    endpoint(env["reference_endpoint"])
    endpoint(env["candidate_endpoint"])
    require(env["reference_endpoint"].rstrip("/") != env["candidate_endpoint"].rstrip("/"),
            "reference and candidate endpoints must differ")
    implementation(value["reference"], "haskell")
    implementation(value["candidate"], "go")
    for dependency in indexed(value["dependency_images"]).values():
        fields(dependency, "id digest")
        require(type(dependency["digest"]) is str and dependency["digest"].startswith("sha256:"),
                "dependency requires immutable digest")
        digest(dependency["digest"][7:])
    scenarios = indexed(value["scenarios"])
    require(scenarios, "required scenarios cannot be empty")
    for scenario in scenarios.values():
        fields(scenario, "id steps")
        require(indexed(scenario["steps"]), "required steps cannot be empty")
        for step in scenario["steps"]:
            fields(step, "id body_mode assertions")
            require(step["body_mode"] in {"bytes", "json-object-order"}, "unknown body comparison mode")
            require(type(step["assertions"]) is list and step["assertions"], "required assertions cannot be empty")
            for assertion in step["assertions"]:
                identifier(assertion)
            require(len(step["assertions"]) == len(set(step["assertions"])), "duplicate required assertion")
    return scenarios


def body(value):
    require(type(value) is str and len(value) <= ((MAX_BODY + 2) // 3) * 4, "body exceeds inline limit")
    try:
        decoded = base64.b64decode(value, validate=True)
    except (binascii.Error, ValueError) as error:
        raise Invalid("invalid body base64") from error
    require(len(decoded) <= MAX_BODY, "body exceeds inline limit")
    require(base64.b64encode(decoded).decode("ascii") == value, "non-canonical body base64")
    return decoded


def headers(value):
    require(type(value) is list and len(value) <= 1024, "invalid headers")
    result = {}
    for pair in value:
        require(type(pair) is list and len(pair) == 2 and all(type(s) is str for s in pair), "invalid header pair")
        name, text = pair
        require(len(name) <= 256 and TOKEN.fullmatch(name), "invalid header name")
        require(len(text) <= 65536 and not any(c in text for c in "\r\n\x00"), "invalid header value")
        result.setdefault(name.lower(), []).append(text)
    return result


def step_shape(value):
    fields(value, "id request response effects assertions")
    request = value["request"]
    fields(request, "method target headers body_base64")
    require(type(request["method"]) is str and TOKEN.fullmatch(request["method"]), "invalid request method")
    require(type(request["target"]) is str and request["target"].startswith("/")
            and len(request["target"]) <= 65536 and not any(c.isspace() or ord(c) < 32 for c in request["target"]),
            "invalid request target")
    headers(request["headers"])
    body(request["body_base64"])
    response = value["response"]
    fields(response, "status headers body_base64")
    require(type(response["status"]) is int and 100 <= response["status"] <= 599, "invalid HTTP status")
    parsed_headers = headers(response["headers"])
    payload = body(response["body_base64"])
    if "content-length" in parsed_headers:
        lengths = parsed_headers["content-length"]
        require(len(lengths) == 1 and re.fullmatch(r"[0-9]{1,12}", lengths[0]), "invalid content length")
        if request["method"] != "HEAD" and response["status"] not in {204, 304} and response["status"] >= 200:
            require(int(lengths[0]) == len(payload), "content length differs from captured body")
    require(type(value["effects"]) is list, "effects must be ordered observations")
    for assertion in indexed(value["assertions"]).values():
        fields(assertion, "id result")
        require(assertion["result"] in RESULTS, "invalid assertion result")


def trace_shape(value, manifest, role, required):
    fields(value, "schema_version role implementation source_revision image_digest config_sha256 "
           "contract_revision fixture_revision data_classification scenarios")
    require(type(value["schema_version"]) is int and value["schema_version"] == 1, "unsupported capture schema")
    require(value["role"] == role and value["data_classification"] == "synthetic", "invalid capture role/classification")
    for key in ("implementation", "source_revision", "image_digest", "config_sha256"):
        require(value[key] == manifest[role][key], "capture implementation identity mismatch")
    for key in ("contract_revision", "fixture_revision"):
        require(value[key] == manifest[key], "capture oracle identity mismatch")
    observations = indexed(value["scenarios"])
    require(set(observations) <= set(required), "unexpected captured scenario")
    for scenario in observations.values():
        fields(scenario, "id result steps")
        require(scenario["result"] in RESULTS, "invalid scenario result")
        steps = indexed(scenario["steps"])
        expected_steps = indexed(required[scenario["id"]]["steps"])
        require(set(steps) <= set(expected_steps), "unexpected captured step")
        for step in steps.values():
            step_shape(step)
            assertions = indexed(step["assertions"])
            require(set(assertions) <= set(expected_steps[step["id"]]["assertions"]), "unexpected assertion")
    return observations


class Number(str):
    """Preserve JSON numeric spelling; bool, number and string never compare equal."""


def equal(left, right):
    if type(left) is not type(right):
        return False
    if type(left) is dict:
        return left.keys() == right.keys() and all(equal(left[k], right[k]) for k in left)
    if type(left) is list:
        return len(left) == len(right) and all(equal(a, b) for a, b in zip(left, right))
    return left == right


def compare_step(expected, reference, candidate):
    differences = []
    for observation in (reference, candidate):
        assertions = indexed(observation["assertions"])
        if set(assertions) != set(expected["assertions"]):
            differences.append("missing-assertion")
        if any(item["result"] != "pass" for item in assertions.values()):
            differences.append("assertion-not-passed")
    for key in ("method", "target", "body_base64"):
        if reference["request"][key] != candidate["request"][key]:
            differences.append("stimulus-mismatch")
    if headers(reference["request"]["headers"]) != headers(candidate["request"]["headers"]):
        differences.append("stimulus-headers")
    left, right = reference["response"], candidate["response"]
    if left["status"] != right["status"]:
        differences.append("status")
    if headers(left["headers"]) != headers(right["headers"]):
        differences.append("headers")
    left_body, right_body = body(left["body_base64"]), body(right["body_base64"])
    if expected["body_mode"] == "json-object-order":
        try:
            left_body = parse(left_body.decode("utf-8"), parse_int=Number, parse_float=Number)
            right_body = parse(right_body.decode("utf-8"), parse_int=Number, parse_float=Number)
        except (Invalid, UnicodeError):
            differences.append("invalid-json-body")
    if not equal(left_body, right_body):
        differences.append("body")
    if not equal(reference["effects"], candidate["effects"]):
        differences.append("effects")
    return sorted(set(differences))


def compare(manifest, reference, candidate):
    required = manifest_shape(manifest)
    traces = {role: trace_shape(trace, manifest, role, required)
              for role, trace in (("reference", reference), ("candidate", candidate))}
    results = []
    provenance = all(manifest[role]["provenance"]["verified"] for role in traces)
    for scenario in required.values():
        result = {"id": scenario["id"], "result": "pass", "differences": []}
        captured = [traces[role].get(scenario["id"]) for role in traces]
        if not provenance or any(value is None for value in captured):
            result.update(result="blocked", differences=["unverified-provenance" if not provenance else "missing-scenario"])
        elif any(value["result"] != "pass" for value in captured):
            nonpasses = {value["result"] for value in captured} - {"pass"}
            result.update(result="fail" if "fail" in nonpasses else "blocked" if "blocked" in nonpasses else "skipped",
                          differences=["capture-not-passed"])
        else:
            steps = [indexed(value["steps"]) for value in captured]
            expected_order = [step["id"] for step in scenario["steps"]]
            if any(list(items) != expected_order for items in steps):
                result.update(result="blocked", differences=["missing-or-reordered-step"])
            else:
                for step in scenario["steps"]:
                    differences = compare_step(step, steps[0][step["id"]], steps[1][step["id"]])
                    if differences:
                        result["result"] = "fail"
                        result["differences"].append({"step": step["id"], "categories": differences})
        results.append(result)
    totals = {value: sum(result["result"] == value for result in results) for value in RESULTS}
    return {"schema_version": 1, "run_id": str(uuid.uuid4()), "evidence_kind": "offline-capture-comparison",
            "parity_verified": False, "provenance": "supplied-declarations-only",
            "comparison_passed": totals["pass"] == len(required),
            "manifest": manifest, "scenarios": results, "coverage": {"required": len(required), **totals}}


def load_file(path):
    path = Path(path)
    for item in (path, *path.parents):
        require(not item.is_symlink(), "symlink inputs are forbidden")
    descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as stream:
        require(stat.S_ISREG(os.fstat(stream.fileno()).st_mode), "capture must be a regular file")
        data = stream.read(MAX_FILE + 1)
    require(len(data) <= MAX_FILE, "capture file exceeds limit")
    return parse(data), hashlib.sha256(data).hexdigest()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest")
    parser.add_argument("reference")
    parser.add_argument("candidate")
    parser.add_argument("report", help="new report file; existing files are never overwritten")
    args = parser.parse_args(argv)
    try:
        inputs = [load_file(path) for path in (args.manifest, args.reference, args.candidate)]
        report = compare(*(item[0] for item in inputs))
        report["input_sha256"] = dict(zip(("manifest", "reference", "candidate"), (item[1] for item in inputs)))
        report["reported_at"] = datetime.now(timezone.utc).isoformat()
        destination = Path(args.report)
        for item in (destination, *destination.parents):
            require(not item.is_symlink(), "symlink report paths are forbidden")
        with destination.open("x") as stream:
            json.dump(report, stream, indent=2, allow_nan=False)
            stream.write("\n")
        print("Offline comparison passed" if report["comparison_passed"] else "Offline comparison did not pass")
        return 0 if report["comparison_passed"] else 1
    except (Invalid, OSError, RecursionError, TypeError) as error:
        # Never echo untrusted capture/body/header values in diagnostics.
        print("Offline comparison rejected input or report destination", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
