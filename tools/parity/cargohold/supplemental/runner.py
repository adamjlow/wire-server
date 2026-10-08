#!/usr/bin/env python3
"""Finite reference-owned supplemental comparison on real Haskell/Go services and private S3."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time
import uuid

ROOT = Path('/source') if '--inside' in sys.argv else Path(__file__).resolve().parents[4]


def call(args, timeout=60):
    return subprocess.run(list(map(str, args)), check=True, text=True,
                          capture_output=True, timeout=timeout).stdout.strip()


def inside():
    if os.getuid()!=65534: raise ValueError('runner must be non-root')
    sys.path.insert(0,'/source/tools/parity/cargohold/supplemental')
    from cases import execute
    execute()


def host(binary, output):
    sys.path.insert(0, str(ROOT/'tools/parity/docker'))
    import environment
    output = environment.destination(output)
    if call(['git', 'status', '--porcelain']):
        raise ValueError('commit candidate inputs before smoke')
    binary = binary.resolve(strict=True)
    output.mkdir()
    (output/'reports').mkdir()
    (output/'reports').chmod(0o777)
    (output/'fixtures').mkdir()
    config = environment.yaml_json(ROOT/'services/cargohold/cargohold.integration.yaml')
    (output/'fixtures/config.json').write_text(json.dumps(config)+'\n')
    runner = json.loads((ROOT/'tools/parity/docker/environment-build-receipt.json').read_text())['image_id']
    store = json.loads((ROOT/'tools/parity/docker/baselines/haskell-cargohold-environment.json').read_text())['dependencies']['fake_s3']
    name = 'wire-cargo-supplemental-'+uuid.uuid4().hex
    network = None
    owned = []
    try:
        network = call(['docker', 'network', 'create', '--internal', name])
        common = ['--network', network, '--user', '65534:65534', '--read-only', '--cap-drop=ALL',
                  '--security-opt=no-new-privileges', '--pids-limit=256', '--memory=1g', '--cpus=2',
                  '--tmpfs', '/tmp:rw,nosuid,nodev,size=128m,mode=1777']
        store_id = call(['docker', 'create', '--name', name+'-store', *common, '--network-alias', 'store',
            '--env', 'GARAGE_DEFAULT_ACCESS_KEY='+environment.SYNTHETIC_ENV['AWS_ACCESS_KEY_ID'],
            '--env', 'GARAGE_DEFAULT_SECRET_KEY='+environment.SYNTHETIC_ENV['AWS_SECRET_ACCESS_KEY'],
            '--env', 'GARAGE_DEFAULT_BUCKET=dummy-bucket', '--mount', 'type=bind,source='+str(ROOT/'deploy/dockerephemeral/docker/garage.toml')+',target=/etc/garage.toml,readonly',
            store, '/garage', 'server', '--single-node', '--default-bucket'])
        owned.append(store_id)
        call(['docker', 'start', store_id])
        args = ['docker', 'create', '--name', name+'-runner', *common, '--entrypoint', '/bin/bash']
        for key, value in environment.SYNTHETIC_ENV.items():
            if key.startswith('AWS_') or key == 'HOME':
                args += ['--env', key+'='+value]
        for source, target, readonly in [(ROOT, '/source', True), (binary, '/candidate/cargohold', True),
            (Path(__file__).parent.resolve(), '/probe', True), (output/'fixtures', '/fixtures', True), (output/'reports', '/reports', False)]:
            args += ['--mount', 'type=bind,source='+str(source)+',target='+target+(',readonly' if readonly else '')]
        args += [runner, '-c', 'for runner_python in /nix/store/*-python3-*/bin/python3; do if [[ -x "$runner_python" ]]; then exec "$runner_python" /probe/runner.py --inside; fi; done; exit 2']
        identity = call(args)
        owned.append(identity)
        with (output/'capture.log').open('x') as stream:
            result = subprocess.run(['docker', 'start', '--attach', identity], stdout=stream, stderr=subprocess.STDOUT, timeout=180)
        receipt = {'source_revision': call(['git', 'rev-parse', 'HEAD']), 'runner_image': runner, 'store_image': store,
                   'candidate_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'exit_code': result.returncode,
                   'internal_network': True, 'published_ports': False, 'whole_service_parity': False}
        (output/'run.json').write_text(json.dumps(receipt, indent=2)+'\n')
        if result.returncode:
            raise ValueError('supplemental run failed; inspect capture.log/reports')
    finally:
        for identity in reversed(owned):
            call(['docker', 'rm', '--force', identity], timeout=15)
        if network:
            call(['docker', 'network', 'rm', network], timeout=15)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--inside', action='store_true')
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if args.inside:
        inside()
    elif args.binary and args.output:
        host(args.binary, args.output)
    else:
        parser.error('--binary and --output required')


if __name__ == '__main__':
    main()
