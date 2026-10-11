#!/usr/bin/env python3
"""Owned Rabbit publisher proof; no API or whole-service acceptance claim."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time
import uuid

from postgres_migrations import ROOT, RUNNER, checked, run

RABBIT = 'rabbitmq@sha256:5cbd7145b0306399ad68422c3350b6cbd1bb95704b39f5896480e5b6d4238a04'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--test-binary', type=Path, required=True)
    parser.add_argument('--build-receipt', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    binary = args.test_binary.resolve(strict=True)
    output = args.output.absolute()
    if output.exists() or output.is_relative_to(ROOT):
        raise ValueError('new external output required')
    if checked(['git', '-C', str(ROOT), 'status', '--porcelain']):
        raise ValueError('commit candidate inputs first')
    revision = checked(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'])
    receipt = json.loads(args.build_receipt.read_text())
    checksum = hashlib.sha256(binary.read_bytes()).hexdigest()
    if receipt['source_revision'] != revision or receipt['binary_sha256'] != checksum:
        raise ValueError('identified exact-source tagged build required')
    output.mkdir(parents=True)
    identity = 'wire-brig-rabbit-' + uuid.uuid4().hex
    broker = identity + '-broker'
    test = identity + '-test'
    label = 'wire.migration.observation=' + identity
    report = {'result': 'failed', 'source_revision': revision,
              'test_binary_sha256': checksum, 'rabbit_image': RABBIT,
              'runner_image': RUNNER, 'scope': __doc__}
    try:
        checked(['docker', 'network', 'create', '--internal', '--label', label, identity])
        checked(['docker', 'run', '-d', '--name', broker, '--hostname', 'owned-rabbit',
                 '--network', identity, '--network-alias', 'rabbit', '--label', label,
                 '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
                 '--user', '999:999', '--memory=1g', '--cpus=2', '--pids-limit=256',
                 '--tmpfs', '/tmp:rw,nosuid,nodev,size=128m,mode=1777',
                 '--tmpfs', '/var/lib/rabbitmq:rw,nosuid,nodev,size=256m,uid=999,gid=999',
                 '--tmpfs', '/var/log/rabbitmq:rw,nosuid,nodev,size=64m,uid=999,gid=999',
                 '-e', 'RABBITMQ_DEFAULT_USER=synthetic-user',
                 '-e', 'RABBITMQ_DEFAULT_PASS=synthetic-password',
                 '-e', 'RABBITMQ_SERVER_ADDITIONAL_ERL_ARGS=+S 2:2', RABBIT])
        deadline = time.monotonic() + 90
        while run(['docker', 'exec', broker, 'rabbitmq-diagnostics', '-q', 'check_running'], 10).returncode:
            if checked(['docker', 'inspect', '--format', '{{.State.Running}}', broker]) != 'true':
                raise RuntimeError('owned broker exited during startup')
            if time.monotonic() > deadline:
                raise RuntimeError('owned broker readiness timed out')
            time.sleep(1)
        result = run(['docker', 'run', '--rm', '--name', test, '--network', identity,
                      '--label', label, '--read-only', '--cap-drop=ALL',
                      '--security-opt=no-new-privileges', '--user', '65534:65534',
                      '--memory=1g', '--cpus=2', '--pids-limit=256',
                      '--tmpfs', '/tmp:rw,nosuid,nodev,size=128m,mode=1777',
                      '--mount', f'type=bind,src={ROOT},dst=/source,readonly',
                      '--mount', f'type=bind,src={binary},dst=/candidate/tests,readonly',
                      '-e', 'RABBITMQ_USERNAME=synthetic-user',
                      '-e', 'RABBITMQ_PASSWORD=synthetic-password',
                      '-e', 'BRIG_QUALIFICATION_RABBIT_HOST=rabbit',
                      '-w', '/source/go/services/brig', '--entrypoint', '/candidate/tests',
                      RUNNER, '-test.v', '-test.run=^TestOwnedBackendNotifications$',
                      '-test.timeout=35s'], 45)
        (output / 'test.stdout').write_text(result.stdout)
        (output / 'test.stderr').write_text(result.stderr)
        report['test_exit_code'] = result.returncode
        if result.returncode:
            raise RuntimeError('owned publisher assertions failed')
        queues = json.loads(checked(['docker', 'exec', broker, 'rabbitmqctl',
                                   'list_queues', 'name', 'durable', 'auto_delete',
                                   'arguments', '--formatter', 'json']))
        (output / 'queues.json').write_text(json.dumps(queues, indent=2) + '\n')
        queue = next(q for q in queues if q['name'] == 'backend-notifications.remote.example.com')
        if not queue['durable'] or queue['auto_delete'] or sorted(queue['arguments']) != sorted([
                ['x-queue-type', 'longstr', 'quorum'],
                ['x-single-active-consumer', 'bool', True]]):
            raise RuntimeError('native queue declaration mismatch')
        report['result'] = 'pass'
    except Exception as error:
        report['failure'] = str(error)
    finally:
        logs = run(['docker', 'logs', broker])
        (output / 'broker.log').write_text(logs.stdout + logs.stderr)
        for name in (test, broker):
            if checked(['docker', 'ps', '-aq', '--filter', 'name=^/' + name + '$']):
                run(['docker', 'rm', '-f', name])
        run(['docker', 'network', 'rm', identity])
        report['cleanup'] = {
            'containers': checked(['docker', 'ps', '-aq', '--filter', 'name=' + identity]),
            'networks': checked(['docker', 'network', 'ls', '-q', '--filter', 'name=' + identity]),
            'volumes': checked(['docker', 'volume', 'ls', '-q', '--filter', 'name=' + identity])}
        if any(report['cleanup'].values()):
            report['result'] = 'failed'
        (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))
    return 0 if report['result'] == 'pass' else 1


if __name__ == '__main__':
    raise SystemExit(main())
