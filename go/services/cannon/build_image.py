#!/usr/bin/env python3
"""Build a local optional cannon image; never push or deploy."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import ssl
import subprocess
import tempfile
import uuid

ROOT = Path(__file__).resolve().parent
REFERENCE = "quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27"


def command(args, cwd=None, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True, stderr=subprocess.PIPE, timeout=180).strip()


def valid_output(path):
    path = path.absolute()
    if path.exists() or path.is_symlink() or any(p.is_symlink() for p in path.parents):
        raise ValueError("image receipt must be a new file without symlink parents")
    if not path.parent.is_dir():
        raise ValueError("receipt parent must already exist")
    return path


def build(output):
    output = valid_output(Path(output))
    valid_output(output.with_suffix(".ca.json"))
    if command(["git", "status", "--porcelain"], cwd=ROOT):
        raise ValueError("image requires a clean committed checkout")
    revision = command(["git", "rev-parse", "HEAD"], cwd=ROOT)
    if not re.fullmatch(r"[a-f0-9]{40}", revision) or command(["go", "version"]) != "go version go1.26.9 linux/amd64":
        raise ValueError("image requires pinned Go 1.26.9 on linux/amd64")
    container = None
    try:
        with tempfile.TemporaryDirectory(prefix="wire-cannon-image-") as directory:
            files = Path(directory)
            env = {**os.environ, "CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOOS": "linux", "GOARCH": "amd64", "GOAMD64": "v1", "GOFLAGS": "-mod=readonly"}
            command(["go", "build", "-o", str(files / "cannon"), "./cmd/cannon"], cwd=ROOT, env=env)
            info = json.loads(command(["go", "version", "-m", "-json", str(files / "cannon")]))
            settings = {v["Key"]: v["Value"] for v in info["Settings"]}
            if (settings.get("vcs.revision") != revision or settings.get("vcs.modified") != "false" or
                settings.get("CGO_ENABLED") != "0" or info["GoVersion"] != "go1.26.9"):
                raise ValueError("compiler receipt does not match clean source")
            container = command(["docker", "create", "--name", "wire-cannon-ca-" + uuid.uuid4().hex,
                                 "--read-only", "--network=none", "--cap-drop=ALL", REFERENCE])
            if not re.fullmatch(r"[a-f0-9]{64}", container):
                container = None
                raise ValueError("invalid owned reference container identity")
            command(["docker", "cp", "--follow-link", container + ":/etc/ssl/certs/ca-bundle.crt", str(files / "ca-bundle.crt")])
            ca = files / "ca-bundle.crt"
            if ca.is_symlink() or not ca.is_file():
                raise ValueError("reference CA bundle is not a regular file")
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
            context.load_verify_locations(cafile=str(ca))
            if context.cert_store_stats()["x509_ca"] == 0:
                raise ValueError("reference bundle contains no CA certificates")
            shutil.copyfile(ROOT / "Dockerfile", files / "Dockerfile")
            image = command(["docker", "build", "--network=none", "--quiet", "--label",
                             "org.opencontainers.image.revision=" + revision, str(files)])
            if not re.fullmatch(r"sha256:[a-f0-9]{64}", image):
                raise ValueError("image has no immutable local identity")
            receipt = {"source_revision": revision, "binary_sha256": hashlib.sha256((files / "cannon").read_bytes()).hexdigest(),
                       "image_id": image, "build_info": info}
            # Separate CA receipt keeps the existing live capture identity shape.
            ca_receipt = {"source_image": REFERENCE, "sha256": hashlib.sha256(ca.read_bytes()).hexdigest(),
                          "ca_certificates": context.cert_store_stats()["x509_ca"]}
        command(["docker", "rm", container]); container = None
        with output.open("x") as stream:
            json.dump(receipt, stream, indent=2); stream.write("\n")
        with output.with_suffix(".ca.json").open("x") as stream:
            json.dump(ca_receipt, stream, indent=2); stream.write("\n")
        print("Built local optional cannon image:", image)
        return receipt
    finally:
        if container is not None:
            command(["docker", "rm", container])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--receipt", type=Path, required=True)
    args = parser.parse_args()
    try:
        build(args.receipt)
        return 0
    except (ValueError, OSError, subprocess.SubprocessError, KeyError):
        print("Cannon image build failed; no image pushed or deployment changed")
        return 2

if __name__ == "__main__": raise SystemExit(main())
