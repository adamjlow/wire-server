#!/usr/bin/env python3
"""Explicit checks for one opt-in candidate; never execute migration task text."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parent
SERVICES = ("proxy", "cargohold", "cannon", "federator", "gundeck", "background-worker", "spar", "brig", "galley")
SCANNERS = (("govulncheck", "golang.org/x/vuln/cmd/govulncheck"),
            ("go-licenses", "github.com/google/go-licenses/v2"))
# Include the repository's own AGPL license explicitly. Generic classifier
# categories label AGPL forbidden and are unsuitable for this AGPL repository.
LICENSES = "AGPL-3.0,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MIT"


class CheckError(Exception):
    pass


def module_path(service, root=ROOT):
    if service not in (*SERVICES, "tooling"):
        raise CheckError("unknown candidate service")
    path = root / "tooling" if service == "tooling" else root / "services" / service
    for item in (root / "services", path, path / "go.mod"):
        if item.is_symlink():
            raise CheckError("candidate paths must not be symlinks")
    if not (path / "go.mod").is_file():
        raise CheckError(f"candidate module is unavailable: {service}")
    return path


def run(args, cwd, env, capture=False):
    # Argument vectors only: service/task strings cannot become shell commands.
    try:
        result = subprocess.run(args, cwd=cwd, env=env, text=True,
                                stdout=subprocess.PIPE if capture else None,
                                check=False, timeout=600)
    except subprocess.TimeoutExpired as error:
        raise CheckError(f"{args[0]} {args[1]} timed out") from error
    if result.returncode:
        raise CheckError(f"{args[0]} {args[1]} failed (exit {result.returncode})")
    return result.stdout if capture else ""


def sources(path):
    files = []
    for parent, dirs, names in os.walk(path, followlinks=False):
        base = Path(parent)
        for name in dirs + names:
            if (base / name).is_symlink():
                raise CheckError("candidate sources must not contain symlinks")
        dirs[:] = [name for name in dirs if name not in ("vendor", ".git")]
        files.extend(str(base / name) for name in names if name.endswith(".go"))
    if not files:
        raise CheckError("candidate module has no Go sources")
    return sorted(files)


def check(service, root=ROOT):
    path = module_path(service, root)
    pins = json.loads((root / "toolchain.json").read_text())
    if (root / ".go-version").read_text().strip() != pins["version"]:
        raise CheckError("Go version pins disagree")
    env = dict(os.environ, GOTOOLCHAIN="local", GOWORK="off",
               GOFLAGS="-mod=readonly", CGO_ENABLED="1")
    version = run(["go", "env", "GOVERSION"], path, env, capture=True).strip()
    if version != "go" + pins["version"]:
        raise CheckError(f"expected Go {pins['version']}; found {version}")
    files = sources(path)
    # Chunking avoids OS argument-size limits in larger future services.
    for start in range(0, len(files), 100):
        if run(["gofmt", "-l", *files[start:start + 100]], path, env, capture=True).strip():
            raise CheckError("Go formatting differs; run gofmt on candidate sources")
    run(["go", "mod", "tidy", "-diff"], path, env)
    run(["go", "mod", "verify"], path, env)
    run(["go", "vet", "./..."], path, env)
    run(["go", "test", "-race", "-count=1", "-timeout=5m", "./..."], path, env)
    with tempfile.TemporaryDirectory(prefix="wire-go-check-") as output:
        if service != "tooling":
            run(["go", "build", "-o", output + "/candidate/", "./..."], path, env)
        tools = root / "tooling"
        run(["go", "mod", "verify"], tools, env)
        for name, package in SCANNERS:
            binary = str(Path(output) / name)
            run(["go", "build", "-o", binary, package], tools, env)
        # Module-level scanning catches known vulnerabilities even in unused paths.
        run([str(Path(output) / "govulncheck"), "-scan=module"], path, env)
        # Unknown/unlisted licenses fail; reports still require compatibility review.
        packages = ["./..."] + ([package for _, package in SCANNERS] if service == "tooling" else [])
        run([str(Path(output) / "go-licenses"), "check", "--include_tests",
             "--allowed_licenses=" + LICENSES, *packages], path, env)
        run([str(Path(output) / "go-licenses"), "report", "--include_tests", *packages], path, env)
    print(f"Go module checks passed: {service}; Go {pins['version']}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("service", choices=(*SERVICES, "tooling"))
    args = parser.parse_args()
    try:
        check(args.service)
    except (CheckError, OSError, ValueError, KeyError) as error:
        print(f"Candidate checks failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
