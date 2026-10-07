#!/usr/bin/env python3
"""Isolated synthetic reference/candidate capture, never a full parity verifier."""

import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import hashlib
import gzip
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
    if role not in {"reference", "candidate", "control"} or not re.fullmatch(r"[a-f0-9]{64}", container):
        raise ValueError("invalid internal fixture client endpoint")
    payload = {"method": method, "target": target, "headers": list(headers),
               "body_base64": base64.b64encode(body).decode()}
    return json.loads(command(["docker", "exec", "--interactive", container, "/provider", "-client", role],
                              timeout=35, input=json.dumps(payload)))


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


def scenarios(suite="initial"):
    if suite not in {"initial", "extended", "operations"}:
        raise ValueError("unknown case suite")
    cases = []

    def add(name, target, method="GET", status=200, provider_statuses=None,
            provider_headers=None, body=b"", headers=None):
        cases.append({"id": name, "method": method, "target": target,
                      "headers": headers if headers is not None else [["Request-Id", "synthetic-reference-case"]],
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
    if suite == "extended":
        # Reuse request specifications from integration/test/Test/Proxy.hs;
        # provider effects validate the forwarded values against real Haskell.
        for name, target, method, body in (
            ("upstream-test-giphy", paths["giphy"] + "?q=monday&limit=100&offset=0", "GET", b""),
            ("upstream-test-youtube", "/proxy/youtube/v3/wef?gnarz=true", "GET", b""),
            ("upstream-test-google-geocode", "/proxy/googlemaps/maps/api/geocode/path_segment?geocode=true", "GET", b""),
            ("upstream-test-google-static", paths["staticmap"] + "?staticmap=true", "GET", b""),
            ("upstream-test-spotify", "/proxy/spotify/api/token", "POST", b'{"v":"my-spotify-body"}'),
            ("upstream-test-soundcloud-resolve", "/proxy/soundcloud/resolve?url=https%3A%2F%2Fmy.url", "GET", b"")):
            add(name, target, method=method, body=body)
        for status in (500, 503):
            add("giphy-upstream-" + str(status), paths["giphy"], provider_statuses=[status], status=status)
            add("spotify-upstream-" + str(status), "/proxy/spotify/api/token", provider_statuses=[status], status=status)
        for family, path in (("giphy", paths["giphy"]), ("spotify", "/proxy/spotify/api/token"),
                             ("resolve", "/proxy/soundcloud/resolve?url=synthetic")):
            for fault in ("close", "incomplete-headers"):
                add(family + "-transport-" + fault, path, status=502 if family == "giphy" else 500)
                cases[-1]["provider"]["fault"] = fault
            add(family + "-redirect", path, status=302 if family == "giphy" else 200, provider_statuses=[302, 200],
                provider_headers={"Content-Type": ["application/json"], "Location": ["https://provider:8443/redirect-target"]})
        for family, path in (("giphy", paths["giphy"]), ("spotify", "/proxy/spotify/api/token")):
            add(family + "-gzip-response", path, provider_headers={"Content-Type": ["application/json"], "Content-Encoding": ["gzip"]})
            cases[-1]["provider"]["body"] = base64.b64encode(gzip.compress(b'{"provider":"synthetic"}', mtime=0)).decode()
        for family, path in (("giphy", paths["giphy"]), ("spotify", "/proxy/spotify/api/token")):
            add(family + "-response-timeout", path, status=502 if family == "giphy" else 500)
            cases[-1]["provider"]["delay_ms"] = 6000
            add(family + "-structured-error", path, status=503, provider_statuses=[503])
            cases[-1]["provider"]["body"] = base64.b64encode(b'{"code":503,"label":"custom","message":"<synthetic>&","data":{"domain":"EXAMPLE.COM","path":"/synthetic"},"inner":{"code":500,"label":"inner","message":"detail"}}').decode()
        for family, path in (("spotify", "/proxy/spotify/api/token"), ("resolve", "/proxy/soundcloud/resolve?url=synthetic")):
            add(family + "-redirect-loop", path, status=500, provider_statuses=[302],
                provider_headers={"Location": ["https://provider:8443/loop"]})
        headers = [["Request-Id", "synthetic-reference-case"], ["Content-Encoding", "gzip"], ["Content-Type", "application/json"]]
        add("spotify-gzip-request", "/proxy/spotify/api/token", method="POST", body=gzip.compress(b'{"v":"my-spotify-body"}', mtime=0), headers=headers)
        add("spotify-malformed-gzip", "/proxy/spotify/api/token", method="POST", body=b"invalid gzip", headers=headers, status=400)
        add("unknown-malformed-gzip", "/unknown", body=b"invalid gzip", headers=headers, status=404)
        add("health-head", "/i/status", method="HEAD")
        add("health-post", "/i/status", method="POST", status=405)
        add("giphy-head", paths["giphy"], method="HEAD", status=405)
        add("version-unicode", "/v%EF%BC%91" + paths["giphy"], status=404)
        add("version-bare-v", "/v" + paths["giphy"], status=404)
        add("version-leading-zero", "/v00" + paths["giphy"], status=404)
        add("request-id-missing", paths["giphy"], headers=[])
        add("spotify-repeated-headers", "/proxy/spotify/api/token", method="POST", body=b"synthetic",
            headers=[["Request-Id", "synthetic-reference-case"], ["Accept", "application/json"], ["Accept", "text/plain"],
                     ["Content-Type", "application/json"], ["Content-Type", "text/plain"]])
    if suite == "operations":
        return [case for case in cases if case["id"] in {"health", "unknown-route", "giphy-forward"}]
    return cases


def candidate_receipt(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 1024 * 1024:
        raise ValueError("candidate receipt must be a bounded regular file")
    receipt = json.loads(path.read_text())
    if set(receipt) != {"source_revision", "binary_sha256", "image_id", "build_info"}:
        raise ValueError("invalid candidate receipt fields")
    for field, pattern in (("source_revision", r"[a-f0-9]{40}"),
                           ("binary_sha256", r"[a-f0-9]{64}"), ("image_id", r"sha256:[a-f0-9]{64}")):
        if not isinstance(receipt[field], str) or not re.fullmatch(pattern, receipt[field]):
            raise ValueError("invalid candidate identity")
    build = receipt["build_info"]
    settings = {v["Key"]: v["Value"] for v in build.get("Settings", [])}
    if (build.get("GoVersion") != "go1.26.8" or
        build.get("Path") != "github.com/adamjlow/wire-server/go/services/proxy/cmd/proxy" or
        settings.get("vcs.revision") != receipt["source_revision"] or
        settings.get("vcs.modified") != "false" or settings.get("CGO_ENABLED") != "0"):
        raise ValueError("candidate compiler or clean-source build identity mismatch")
    return receipt


def operational_checks(provider, service_ids, files, launch, report, mode="full"):
    profile = {"statuses": [200], "headers": {"Content-Type": ["application/json"]},
               "body": base64.b64encode(b'{"provider":"synthetic"}').decode()}
    headers = [["Request-Id", "synthetic-operation"]]

    def observed(role, target="/proxy/giphy/v1/gifs/search"):
        request(provider, "control", "POST", "/reset", [["Content-Type", "application/json"]], json.dumps(profile).encode())
        response = request(provider, role, "GET", target, headers)
        effects = request(provider, "control", "GET", "/observations")
        return {"response": response, "effects": json.loads(base64.b64decode(effects["body_base64"]))}

    operations = report["operations"] = {"mode": mode}
    updated = 'secrets {\n' + '\n'.join(f' {name} = "' +
        ("synthetic-updated-giphy-secret" if name == "giphy" else f"synthetic-{name}-secret") + '"'
        for name in ("youtube", "googlemaps", "soundcloud", "giphy", "spotify")) + '\n}\n'
    secret_file = files / "secrets.conf"
    if mode == "full":
        # Bounded smoke test, not a production capacity benchmark. Thresholds
        # are declared in the plan before capture. Client startup is included.
        profile["delay_ms"] = 0
        performance = operations["performance"] = {}
        for role, cid in service_ids.items():
            request(provider, "control", "POST", "/reset", [["Content-Type", "application/json"]], json.dumps(profile).encode())
            def timed(_):
                started = time.monotonic()
                response = request(provider, role, "GET", "/proxy/giphy/v1/gifs/search", headers)
                return {"seconds": time.monotonic() - started, "status": response["status"], "body_base64": response["body_base64"]}
            with ThreadPoolExecutor(max_workers=4) as executor:
                samples = list(executor.map(timed, range(40)))
            memory = json.loads(command(["docker", "stats", "--no-stream", "--format", "{{json .}}", cid]))["MemUsage"]
            performance[role] = {"samples": samples, "memory_usage": memory}
    if mode in {"full", "reload"}:
        operations["before_reload"] = {role: observed(role) for role in service_ids}
        secret_file.chmod(0o644); secret_file.write_text(updated); secret_file.chmod(0o444)
        started = time.monotonic()
        while True:
            pair = {role: observed(role) for role in service_ids}
            if all(v["response"]["status"] == 200 and len(v["effects"]) == 1 and
                   "api_key=synthetic-updated-giphy-secret" in v["effects"][0]["target"] for v in pair.values()):
                operations["valid_reload"] = pair
                operations["valid_reload_elapsed_seconds"] = time.monotonic() - started
                break
            if time.monotonic() - started > 75:
                raise TimeoutError("valid synthetic secret reload did not complete")
            time.sleep(2)
        print("Both runtimes loaded the changed synthetic secret", flush=True)

        baseline_logs = {role: command(["docker", "logs", cid]).count("Failed reloading config") for role, cid in service_ids.items()}
        secret_file.chmod(0o644); secret_file.write_text('secrets {\n giphy = [invalid-synthetic\n'); secret_file.chmod(0o444)
        started = time.monotonic()
        while not all(command(["docker", "logs", cid]).count("Failed reloading config") > baseline_logs[role]
                      for role, cid in service_ids.items()):
            if time.monotonic() - started > 75:
                raise TimeoutError("invalid synthetic reload was not observed")
            time.sleep(2)
        operations["invalid_reload_retention"] = {role: observed(role) for role in service_ids}
        operations["invalid_reload_elapsed_seconds"] = time.monotonic() - started
        print("Both runtimes rejected invalid reload and kept the last valid secret", flush=True)

    if mode == "reload":
        return

    # Restart both identical images with the same disabled-version configuration.
    secret_file.chmod(0o644); secret_file.write_text(updated); secret_file.chmod(0o444)
    config_file = files / "proxy.yaml"
    config = json.loads(config_file.read_text())
    config["disabledAPIVersions"] = ["v0", "development"]
    config["logLevel"] = "Info"; config["logFormat"] = "StructuredJSON"
    config_file.chmod(0o644); config_file.write_text(json.dumps(config)); config_file.chmod(0o444)
    operations["variant_config_sha256"] = sha(config_file)
    replacement = {}
    for role, cid in service_ids.items():
        command(["docker", "stop", "--time", "35", cid])
        operations.setdefault("normal_shutdown", {})[role] = json.loads(command(["docker", "inspect", cid]))[0]["State"]["ExitCode"]
        # Keep stopped originals for final log capture/cleanup; use distinct names
        # but retain the fixed role's network alias after disconnecting originals.
        command(["docker", "network", "disconnect", report["run_id"], cid])
        image = REFERENCE if role == "reference" else report["candidate"]["image_id"]
        replacement[role] = launch(role, image, ["--env", "SYSTEM_CERTIFICATE_PATH=/fixture/ca.pem", "--env", "SSL_CERT_FILE=/fixture/ca.pem"], suffix="-variant")
        ready(provider, role, "/i/status")
    operations["disabled_versions"] = {role: {version: observed(role, "/" + version + "/proxy/giphy/v1/gifs/search")
        for version in ("v0", "v19", "v1")} for role in replacement}

    profile["delay_ms"] = 500
    request(provider, "control", "POST", "/reset", [["Content-Type", "application/json"]], json.dumps(profile).encode())
    with ThreadPoolExecutor(max_workers=2) as executor:
        pending = {role: executor.submit(request, provider, role, "GET", "/proxy/giphy/v1/gifs/search", headers) for role in replacement}
        deadline = time.monotonic() + 5
        while True:
            effects = request(provider, "control", "GET", "/observations")
            if len(json.loads(base64.b64decode(effects["body_base64"]))) == len(pending):
                break
            if time.monotonic() > deadline:
                raise TimeoutError("shutdown request did not reach provider")
            time.sleep(0.05)
        operations["inflight_active_before_signal"] = all(not result.done() for result in pending.values())
        if not operations["inflight_active_before_signal"]:
            raise ValueError("shutdown fixture responses completed before signal; no drain evidence")
        for cid in replacement.values():
            command(["docker", "kill", "--signal=TERM", cid])
        operations["inflight_shutdown"] = {role: result.result(timeout=15) for role, result in pending.items()}
    for role, cid in replacement.items():
        command(["docker", "wait", cid], timeout=40)
        operations.setdefault("inflight_exit_codes", {})[role] = json.loads(command(["docker", "inspect", cid]))[0]["State"]["ExitCode"]
        operations.setdefault("structured_logs", {})[role] = command(["docker", "logs", cid])[-65536:]
    # The endpoint selected for Go is restarted with the pinned Haskell image,
    # unchanged configuration and synthetic secrets. Proxy owns no stored data.
    command(["docker", "network", "disconnect", report["run_id"], replacement["candidate"]])
    switched = launch("candidate", REFERENCE, ["--env", "SYSTEM_CERTIFICATE_PATH=/fixture/ca.pem", "--env", "SSL_CERT_FILE=/fixture/ca.pem"], suffix="-switchback")
    ready(provider, "candidate", "/i/status")
    profile["delay_ms"] = 0
    operations["switchback"] = {"image": REFERENCE, "image_id": command(["docker", "inspect", "--format", "{{.Image}}", switched]),
        "config_sha256": sha(config_file), "observation": observed("candidate", "/v1/proxy/giphy/v1/gifs/search")}
    print("Disabled/development versions and in-flight SIGTERM shutdown captured", flush=True)


def capture(binary, output, candidate=None, suite="initial", operations=False):
    scenarios(suite)
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
    report["suite"] = suite
    try:
        if candidate is not None:
            receipt = candidate_receipt(candidate)
            command(["git", "cat-file", "-e", receipt["source_revision"] + "^{commit}"])
            image_config = json.loads(command(["docker", "image", "inspect", receipt["image_id"]]))[0]
            cfg = image_config["Config"]
            if (image_config["Id"] != receipt["image_id"] or cfg["User"] != "65534:65534" or
                cfg["Entrypoint"] != ["/proxy"] or
                cfg.get("Labels", {}).get("org.opencontainers.image.revision") != receipt["source_revision"]):
                raise ValueError("candidate image configuration identity mismatch")
            report["evidence_kind"] = "initial-live-proxy-comparison-capture"
            report["candidate"] = receipt
        build = json.loads(command([str(binary), "-build-info"]))
        if build["GoVersion"] != "go1.26.8" or build["Path"] != "wire.local/proxy-reference-provider":
            raise ValueError("provider compiler or module identity mismatch")
        report["provider_build_info"] = build
        report["fixture_revision"] = command(["git", "rev-parse", "HEAD"])
        with tempfile.TemporaryDirectory(prefix=run_id + "-") as temporary:
            files = Path(temporary)
            files.chmod(0o755)
            if candidate is not None:
                verification_id = command(["docker", "create", "--name", run_id + "-verify", receipt["image_id"]])
                if not re.fullmatch(r"[a-f0-9]{64}", verification_id):
                    raise ValueError("invalid candidate verification container identity")
                containers.append(verification_id)
                command(["docker", "cp", verification_id + ":/proxy", str(files / "candidate-binary")])
                if sha(files / "candidate-binary") != receipt["binary_sha256"]:
                    raise ValueError("candidate image binary does not match build receipt")
                (files / "candidate-binary").unlink()
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

            def launch(name, image_name, extra=(), suffix=""):
                container = command(["docker", "run", "--detach", "--name", run_id + "-" + name + suffix,
                                     "--network", run_id, "--network-alias", name,
                                     "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
                                     "--pids-limit=512", "--memory=1g", "--cpus=2",
                                     "--mount", "type=bind,source=" + str(files) + ",target=/fixture,readonly",
                                     *extra, image_name,
                                     *(["-c", "/fixture/proxy.yaml"] if name in {"reference", "candidate"} else [])])
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
            if candidate is not None:
                candidate_id = launch("candidate", receipt["image_id"], ["--env", "SSL_CERT_FILE=/fixture/ca.pem"])
                ready(provider_id, "candidate", "/i/status")
                if command(["docker", "inspect", "--format", "{{.Image}}", candidate_id]) != receipt["image_id"]:
                    raise ValueError("running candidate image mismatch")
            cases = scenarios(suite)
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
                if candidate is not None:
                    reset = request(provider_id, "control", "POST", "/reset", [["Content-Type", "application/json"]],
                                    json.dumps(case["provider"]).encode())
                    if reset["status"] != 200:
                        raise ValueError("candidate provider reset failed")
                    case["candidate_response"] = request(provider_id, "candidate", case["method"], case["target"],
                                                        case["headers"], base64.b64decode(case["body_base64"], validate=True))
                    effects = request(provider_id, "control", "GET", "/observations")
                    if effects["status"] != 200:
                        raise ValueError("candidate provider capture failed")
                    case["candidate_effects"] = json.loads(base64.b64decode(effects["body_base64"]))
                report["cases"].append(case)
                print(case["id"], response["status"], "provider attempts", len(case["effects"]), flush=True)
            report["capture_complete"] = len(report["cases"]) == len(cases)
            report["source_status_mismatches"] = sum(not c["source_status_matched"] for c in report["cases"])
            report["reference_metrics"] = request(provider_id, "reference", "GET", "/i/metrics")
            if candidate is not None:
                report["candidate_metrics"] = request(provider_id, "candidate", "GET", "/i/metrics")
            report["reference_logs"] = command(["docker", "logs", reference_id])[-65536:]
            if operations:
                if candidate is None:
                    raise ValueError("operational comparison requires both runtimes")
                operational_checks(provider_id, {"reference": reference_id, "candidate": candidate_id}, files, launch, report, operations)
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
    parser.add_argument("--candidate-receipt", type=Path, help="optional validated local Go image/build receipt")
    parser.add_argument("--suite", choices=("initial", "extended", "operations"), default="initial")
    parser.add_argument("--operations", nargs="?", const="full", choices=("full", "reload", "shutdown"), help="compare real reload, disabled versions or SIGTERM drain")
    args = parser.parse_args()
    try:
        report = capture(args.provider_binary, args.output, args.candidate_receipt, args.suite, args.operations)
    except (OSError, ValueError, TimeoutError, subprocess.SubprocessError):
        print("Reference characterization failed; partial evidence retained, no parity claimed")
        return 2
    return 0 if report["capture_complete"] and not report["source_status_mismatches"] and not report.get("cleanup_failed") else 1


if __name__ == "__main__":
    raise SystemExit(main())
