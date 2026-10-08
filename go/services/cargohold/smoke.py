#!/usr/bin/env python3
"""Private real-S3 smoke of the initial candidate slice; not whole-service parity."""
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

ROOT = Path('/source') if '--inside' in sys.argv else Path(__file__).resolve().parents[3]


def call(args, timeout=60):
    return subprocess.run(list(map(str, args)), check=True, text=True,
                          capture_output=True, timeout=timeout).stdout.strip()


def inside():
    if os.getuid() != 65534:
        raise ValueError('smoke runner must be non-root')
    spec = importlib.util.spec_from_file_location('reference', ROOT/'tools/parity/cargohold/reference.py')
    ref = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(ref)
    sys.path.insert(0, '/opt/wire-e2e')
    import runtime
    bridge = runtime.TCPServer(('127.0.0.1', 4570), runtime.Forward)
    bridge.destination = ('store', 3900)
    threading.Thread(target=bridge.serve_forever, daemon=True).start()
    Path('/tmp/home').mkdir()
    deadline = time.monotonic() + 30
    while True:
        try:
            ref.aws('head-bucket', '--bucket', 'dummy-bucket')
            break
        except subprocess.CalledProcessError:
            if time.monotonic() > deadline:
                raise
            time.sleep(.2)
    records = []
    with open('/reports/service.log', 'w') as log:
        process = subprocess.Popen(['/candidate/cargohold', '-c', '/fixtures/config.json'], stdout=log, stderr=subprocess.STDOUT)
        try:
            while True:
                if process.poll() is not None:
                    raise ValueError('candidate exited')
                try:
                    if ref.request('GET', '/i/status')[0] == 200:
                        break
                except OSError:
                    pass
                if time.monotonic() > deadline:
                    raise ValueError('candidate not ready')
                time.sleep(.1)
            captures = json.loads((ROOT/'tools/parity/cargohold/reference.json').read_text())['observations']
            cases = [('canonical', {}), ('unequal-delimiters', {'second': 'different'}),
                     ('wrong-md5', {'md5': 'AAAAAAAAAAAAAAAAAAAAAA=='}),
                     ('decimal-length-suffix', {'declared': str(len(ref.DATA))+'junk'}),
                     ('missing-trailer', {'trailer': False}),
                     ('incomplete-data', {'payload': ref.DATA[:-3], 'declared': str(len(ref.DATA)), 'trailer': False})]
            for label, kwargs in cases:
                before = {obj['Key'] for obj in ref.aws('list-objects-v2', '--bucket', 'dummy-bucket').get('Contents', [])}
                status, headers, body = ref.request('POST', '/bot/assets',
                    {'Z-Bot': ref.OWNER, 'Z-Type': 'bot', 'Content-Type': 'multipart/mixed; boundary=wire'}, ref.multipart(**kwargs))
                expected = next(item for item in captures if item['id'] == 'multipart-'+label)
                if status != expected['status']:
                    raise ValueError(f'{label}: status {status}, expected {expected["status"]}; body {body[:200]!r}')
                after = {obj['Key'] for obj in ref.aws('list-objects-v2', '--bucket', 'dummy-bucket').get('Contents', [])}
                if len(after-before) != 1:
                    raise ValueError('upload did not persist exactly one object')
                key = next(iter(after-before))
                head = ref.aws('head-object', '--bucket', 'dummy-bucket', '--key', key)
                ref.aws('get-object', '--bucket', 'dummy-bucket', '--key', key, '/tmp/stored')
                effects = expected['created_objects'][0]
                if head['ContentLength'] != effects['size'] or head['ContentType'] != effects['content_type']:
                    raise ValueError('object size/type mismatch')
                if hashlib.sha256(Path('/tmp/stored').read_bytes()).hexdigest() != effects['body_sha256']:
                    raise ValueError('stored payload mismatch')
                if head['Metadata'].get('bot') != ref.OWNER or not head['Metadata'].get('token'):
                    raise ValueError('principal/token metadata lost')
                if ref.aws('list-multipart-uploads', '--bucket', 'dummy-bucket').get('Uploads', []):
                    raise ValueError('multipart upload leaked')
                record = {'case': label, 'status': status, 'size': head['ContentLength'], 'effects_match_reference': True}
                if status == 201:
                    location = headers['location']
                    if not location.startswith('bot/assets/3-2-'):
                        raise ValueError('wrong relative Location')
                    download = ref.observe(label+'-download', 'GET', '/'+location,
                                           {'Z-Bot': ref.OWNER, 'Z-Type': 'bot'}, expected=302)
                    if not download['authorized_bytes_match']:
                        raise ValueError('real signed download failed')
                    status, _, _ = ref.request('DELETE', '/'+location, {'Z-Bot': ref.OWNER, 'Z-Type': 'bot'})
                    if status != 200 or key in {o['Key'] for o in ref.aws('list-objects-v2', '--bucket', 'dummy-bucket').get('Contents', [])}:
                        raise ValueError('delete effect failed')
                    record['signed_download_and_delete'] = True
                records.append(record)
            Path('/reports/smoke.json').write_text(json.dumps({'result': 'initial-slice-pass', 'whole_service_parity': False,
                'binary_sha256': hashlib.sha256(Path('/candidate/cargohold').read_bytes()).hexdigest(), 'cases': records}, indent=2)+'\n')
        finally:
            if process.poll() is None:
                process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            bridge.shutdown()
            bridge.server_close()


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
    name = 'wire-cargo-slice-'+uuid.uuid4().hex
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
            (Path(__file__).resolve(), '/candidate/smoke.py', True), (output/'fixtures', '/fixtures', True), (output/'reports', '/reports', False)]:
            args += ['--mount', 'type=bind,source='+str(source)+',target='+target+(',readonly' if readonly else '')]
        args += [runner, '-c', 'for runner_python in /nix/store/*-python3-*/bin/python3; do if [[ -x "$runner_python" ]]; then exec "$runner_python" /candidate/smoke.py --inside; fi; done; exit 2']
        identity = call(args)
        owned.append(identity)
        with (output/'capture.log').open('x') as stream:
            result = subprocess.run(['docker', 'start', '--attach', identity], stdout=stream, stderr=subprocess.STDOUT, timeout=180)
        receipt = {'source_revision': call(['git', 'rev-parse', 'HEAD']), 'runner_image': runner, 'store_image': store,
                   'candidate_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'exit_code': result.returncode,
                   'internal_network': True, 'published_ports': False, 'whole_service_parity': False}
        (output/'run.json').write_text(json.dumps(receipt, indent=2)+'\n')
        if result.returncode:
            raise ValueError('initial slice smoke failed; inspect capture.log/service.log')
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
