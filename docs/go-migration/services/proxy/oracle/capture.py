#!/usr/bin/env python3
"""Isolated synthetic Haskell characterization, never a parity verifier."""

import argparse
import base64
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parent
SOURCE = "f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e"
REFERENCE = "quay.io/wire/proxy@sha256:460629f6358c93892c3fd6ae5177c685bd787212900339a3bdab2530f875a8f4"


def command(args, timeout=60, input=None):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          text=True, timeout=timeout, input=input).stdout.strip()


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def request(container, role, method, target, headers=(), body=b""):
    if role not in {"reference", "control"} or not re.fullmatch(r"[a-f0-9]{64}", container):
        raise ValueError("invalid internal fixture client endpoint")
    payload = {"method": method, "target": target, "headers": list(headers),
               "body_base64": base64.b64encode(body).decode()}
    return json.loads(command(["docker", "exec", "--interactive", container, "/provider", "-client", role],
                              timeout=20, input=json.dumps(payload)))


def ready(container, role, path):
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            if request(container, role, "GET", path)["status"] == 200:
                return
        except (OSError, subprocess.SubprocessError):
            pass
        time.sleep(0.2)
    raise TimeoutError("isolated fixture did not become ready")


def scenarios():
    cases = []

    def add(name, target, method="GET", status=200, provider_statuses=None,
            provider_headers=None, body=b"", headers=None):
        cases.append({"id": name, "method": method, "target": target,
                      "headers": headers or [["Request-Id", "synthetic-reference-case"]],
                      "body_base64": base64.b64encode(body).decode(), "expected_status": status,
                      "provider": {"statuses": provider_statuses or [200],
                                   "headers": provider_headers or {"Content-Type": ["application/json"], "X-Provider": ["synthetic"]},
                                   "body": base64.b64encode(b'{"provider":"synthetic"}').decode()}})

    add("health", "/i/status")
    add("unknown-route", "/not-a-route", status=404)
    paths = {"giphy": "/proxy/giphy/v1/gifs/search", "youtube": "/proxy/youtube/v3/search",
             "staticmap": "/proxy/googlemaps/api/staticmap",
             "geocode": "/proxy/googlemaps/maps/api/geocode/search"}
    for name, path in paths.items():
        add(name + "-forward", path + "?q=one+two&tag=a&tag=b&bare&ACCESS_TOKEN=discard&access_token=discard2")
        add(name + "-post", path, method="POST", status=405)
        add(name + "-mixed-get", path, method="gEt")
        add(name + "-credentials", path + "?key=caller&api_key=caller&key=again&x=%2F%FF")
        add(name + "-retry-502", path, provider_statuses=[502, 502, 200])
        add(name + "-terminal-502", path, provider_statuses=[502], status=502)
    add("staticmap-extra-path", paths["staticmap"] + "/extra", status=404)
    add("giphy-upstream-404", paths["giphy"], provider_statuses=[404], status=404)
    add("giphy-escaped-path", "/proxy/giphy/v1/gifs/a%2Fb%20c?q=%26%3D")
    for name, path in {"spotify": "/proxy/spotify/api/token", "resolve": "/proxy/soundcloud/resolve?url=https%3A%2F%2Fsynthetic.invalid%2Fsong"}.items():
        add(name + "-get", path)
        add(name + "-post", path, method="POST", body=b"synthetic=body",
            headers=[["Request-Id", "synthetic-reference-case"], ["Accept", "application/json"],
                     ["Content-Type", "application/x-www-form-urlencoded"],
                     ["Authorization", "synthetic-client-credential"], ["X-Client", "exclude-me"]])
        add(name + "-extra-path", path.split("?")[0] + "/extra", status=404)
    add("resolve-missing-url", "/proxy/soundcloud/resolve", status=400)
    stream = "/proxy/soundcloud/stream?url=https%3A%2F%2Fprovider%3A8443%2Faudio"
    for method in ("GET", "POST"):
        add("stream-" + method.lower(), stream, method=method, status=302,
            provider_statuses=[302], provider_headers={"Location": ["https://synthetic.invalid/download"]})
    add("stream-missing-location", stream, provider_statuses=[302], status=500)
    add("stream-not-302", stream, status=500)
    add("stream-wrong-host", "/proxy/soundcloud/stream?url=https%3A%2F%2Fother.invalid%2Faudio", status=500)
    add("stream-http-rejected", "/proxy/soundcloud/stream?url=http%3A%2F%2Fprovider%3A8443%2Faudio", status=500)
    add("stream-missing-url", "/proxy/soundcloud/stream", status=400)
    for version in range(20):
        add("version-" + str(version), "/v" + str(version) + paths["giphy"])
    add("version-unknown", "/v99" + paths["giphy"], status=404)
    add("version-internal", "/v0/i/status", status=404)
    return cases


def capture(binary, output):
    binary = Path(binary).absolute()
    if not binary.is_file() or binary.is_symlink():
        raise ValueError("provider binary must be a regular non-symlink file")
    for parent in output.absolute().parents:
        if parent.is_symlink():
            raise ValueError("symlink artifact parent forbidden")
    output.mkdir(parents=True, exist_ok=False)
    run_id = "wire-proxy-reference-" + uuid.uuid4().hex
    containers, network_created = [], False
    report = {"schema_version": 1, "evidence_kind": "haskell-reference-characterization",
              "parity_verified": False, "source_revision": SOURCE, "reference_image": REFERENCE,
              "provenance_method": "trusted-publisher-image-config-matches-source-evaluated-nix-output",
              "run_id": run_id, "started_at": datetime.now(timezone.utc).isoformat(),
              "runner_sha256": sha(__file__), "provider_binary_sha256": sha(binary),
              "provider_source_sha256": sha(ROOT / "provider/main.go"), "cases": []}
    try:
        build = json.loads(command([str(binary), "-build-info"]))
        if build["GoVersion"] != "go1.26.8" or build["Path"] != "wire.local/proxy-reference-provider":
            raise ValueError("provider compiler or module identity mismatch")
        report["provider_build_info"] = build
        report["fixture_revision"] = command(["git", "rev-parse", "HEAD"])
        with tempfile.TemporaryDirectory(prefix=run_id + "-") as temporary:
            files = Path(temporary)
            files.chmod(0o755)
            shutil.copyfile(binary, files / "provider")
            (files / "provider").chmod(0o555)
            command([str(binary), "-init", str(files)])
            (files / "Dockerfile").write_text("FROM scratch\nCOPY provider /provider\nUSER 65534:65534\nENTRYPOINT [\"/provider\"]\n")
            image = command(["docker", "build", "--network=none", "--quiet", str(files)])
            if not re.fullmatch(r"sha256:[a-f0-9]{64}", image):
                raise ValueError("fixture image lacks content identity")
            report["provider_image_id"] = image
            report["test_ca_sha256"] = sha(files / "ca.pem")
            (files / "secrets.conf").write_text('secrets {\n' + '\n'.join(
                f'    {name} = "synthetic-{name}-secret"' for name in ("youtube", "googlemaps", "soundcloud", "giphy", "spotify")) + '\n}\n')
            config = {"proxy": {"host": "0.0.0.0", "port": 8080}, "secretsConfig": "/fixture/secrets.conf",
                      "httpPoolSize": 10, "maxConns": 100, "logLevel": "Warn", "logNetStrings": False,
                      "disabledAPIVersions": [], "disableTlsForTest": False}
            for endpoint in ("giphyEndpoint", "youtubeEndpoint", "googleMapsEndpoint", "spotifyEndpoint", "soundcloudEndpoint"):
                config[endpoint] = {"host": "provider", "port": 8443}
            (files / "proxy.yaml").write_text(json.dumps(config))
            for name in ("secrets.conf", "proxy.yaml"):
                (files / name).chmod(0o444)
            report["config_sha256"] = sha(files / "proxy.yaml")
            command(["docker", "network", "create", "--internal", run_id])
            network_created = True
            if json.loads(command(["docker", "network", "inspect", run_id]))[0]["Internal"] is not True:
                raise ValueError("network is not isolated")

            def launch(name, image_name, extra=()):
                container = command(["docker", "run", "--detach", "--name", run_id + "-" + name,
                                     "--network", run_id, "--network-alias", name,
                                     "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
                                     "--pids-limit=512", "--memory=1g", "--cpus=2",
                                     "--mount", "type=bind,source=" + str(files) + ",target=/fixture,readonly",
                                     *extra, image_name,
                                     *(["-c", "/fixture/proxy.yaml"] if name == "reference" else [])])
                if not re.fullmatch(r"[a-f0-9]{64}", container):
                    raise ValueError("unexpected container identity")
                containers.append(container)
                return container

            provider_id = launch("provider", image)
            ready(provider_id, "control", "/observations")
            reference_id = launch("reference", REFERENCE,
                ["--env", "SYSTEM_CERTIFICATE_PATH=/fixture/ca.pem", "--env", "SSL_CERT_FILE=/fixture/ca.pem"])
            ready(provider_id, "reference", "/i/status")
            report["reference_image_id"] = command(["docker", "inspect", "--format", "{{.Image}}", reference_id])
            cases = scenarios()
            report["scenario_sha256"] = hashlib.sha256(json.dumps(cases, sort_keys=True).encode()).hexdigest()
            for case in cases:
                reset = request(provider_id, "control", "POST", "/reset", [["Content-Type", "application/json"]],
                                json.dumps(case["provider"]).encode())
                if reset["status"] != 200:
                    raise ValueError("provider reset failed")
                response = request(provider_id, "reference", case["method"], case["target"], case["headers"],
                                   base64.b64decode(case["body_base64"], validate=True))
                effects = request(provider_id, "control", "GET", "/observations")
                if effects["status"] != 200:
                    raise ValueError("provider capture failed")
                case["response"] = response
                case["effects"] = json.loads(base64.b64decode(effects["body_base64"]))
                case["source_status_matched"] = response["status"] == case["expected_status"]
                report["cases"].append(case)
                print(case["id"], response["status"], "provider attempts", len(case["effects"]), flush=True)
            report["capture_complete"] = len(report["cases"]) == len(cases)
            report["source_status_mismatches"] = sum(not c["source_status_matched"] for c in report["cases"])
            report["reference_logs"] = command(["docker", "logs", reference_id])[-65536:]
    except (OSError, ValueError, TimeoutError, subprocess.SubprocessError) as error:
        report["failure_category"] = type(error).__name__
        if isinstance(error, (ValueError, TimeoutError)):
            report["failure_message"] = str(error)
        if isinstance(error, subprocess.CalledProcessError):
            report["failed_command"] = error.cmd[:2]
            report["process_exit_code"] = error.returncode
        raise
    finally:
        for container in reversed(containers):
            try:
                logs = subprocess.run(["docker", "logs", "--tail", "100", container],
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                      text=True, timeout=10)
                report.setdefault("container_logs", {})[container] = (logs.stdout + logs.stderr)[-65536:]
            except (OSError, subprocess.SubprocessError):
                report.setdefault("log_capture_failed", []).append(container)
            try:
                command(["docker", "rm", "--force", container])
            except (OSError, subprocess.SubprocessError):
                report.setdefault("cleanup_failed", []).append(container)
        if network_created:
            try:
                command(["docker", "network", "rm", run_id])
            except (OSError, subprocess.SubprocessError):
                report.setdefault("cleanup_failed", []).append(run_id)
        report["finished_at"] = datetime.now(timezone.utc).isoformat()
        (output / "reference.json").write_text(json.dumps(report, indent=2) + "\n")
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--provider-binary", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        report = capture(args.provider_binary, args.output)
    except (OSError, ValueError, TimeoutError, subprocess.SubprocessError):
        print("Reference characterization failed; partial evidence retained, no parity claimed")
        return 2
    return 0 if report["capture_complete"] and not report["source_status_mismatches"] and not report.get("cleanup_failed") else 1


if __name__ == "__main__":
    raise SystemExit(main())
