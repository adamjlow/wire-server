#!/bin/bash
set -euo pipefail
# Python is already in the pinned integration image's runtime closure (AWS CLI).
# Find it from that immutable image rather than installing anything on the host.
for runner_python in /nix/store/*-python3-*/bin/python3; do
    if [[ -x "$runner_python" ]]; then
        exec "$runner_python" /opt/wire-e2e/runner.py "$@"
    fi
done
printf '%s\n' 'Runner image has no Python runtime; no e2e success claimed.' >&2
exit 2
