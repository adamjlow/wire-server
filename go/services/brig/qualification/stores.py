#!/usr/bin/env python3
"""Actual candidate store checks on schemas created by native utilities; no API/e2e pass."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time
import uuid

from postgres_migrations import ROOT, RUNNER, POSTGRES, checked, run

CASSANDRA = 'cassandra@sha256:dad4cd7ee4281df4e96944a1faebfebd2e50ee19a9f0a1d42ac0bb17c820dd74'
PYTHON = '/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--test-binary', type=Path, required=True)
    parser.add_argument('--build-receipt', type=Path, required=True)
    parser.add_argument('--configs', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    binary = args.test_binary.resolve(strict=True)
    output = args.output.absolute()
    if output.exists() or output.is_relative_to(ROOT):
        raise ValueError('new external output required')
    if checked(['git', '-C', str(ROOT), 'status', '--porcelain']):
        raise ValueError('commit implementation and qualification inputs first')
    revision = checked(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'])
    receipt = json.loads(args.build_receipt.read_text())
    checksum = hashlib.sha256(binary.read_bytes()).hexdigest()
    if receipt['source_revision'] != revision or receipt['binary_sha256'] != checksum:
        raise ValueError('identified test build required')
    output.mkdir(parents=True)
    fixture = json.loads(args.configs.read_text())['brig']
    fixture['postgresql'] = {'host': 'postgres', 'port': '5432', 'dbname': 'owned_stores',
                             'user': 'wire-server', 'password': 'synthetic-migration-password'}
    fixture.pop('postgresqlPassword', None)
    fixture['cassandra']['endpoint']['host'] = 'cassandra'
    fixture['cassandra']['keyspace'] = 'owned_brig_stores'
    config = output / 'brig.json'
    config.write_text(json.dumps(fixture, indent=2) + '\n')
    config.chmod(0o644)
    identity = 'wire-brig-stores-' + uuid.uuid4().hex
    label = 'wire.migration.observation=' + identity
    report = {'result': 'failed', 'scope': __doc__, 'e2e_executed': False,
              'source_revision': revision, 'test_binary_sha256': checksum,
              'runner_image': RUNNER, 'postgres_image': POSTGRES, 'cassandra_image': CASSANDRA,
              'fixture_sha256': hashlib.sha256(config.read_bytes()).hexdigest()}

    def candidate_command(name, entrypoint):
        return ['docker', 'run', '--rm', '--name', identity + '-' + name, '--label', label,
                '--network', identity, '--read-only', '--cap-drop=ALL',
                '--security-opt=no-new-privileges', '--user', '65534:65534',
                '--memory=512m', '--cpus=2', '--pids-limit=256',
                '--tmpfs', '/tmp:rw,nosuid,nodev,size=64m,mode=1777', '--entrypoint', entrypoint]

    try:
        checked(['docker', 'network', 'create', '--internal', '--label', label, identity])
        postgres, cassandra = identity + '-postgres', identity + '-cassandra'
        checked(['docker', 'run', '-d', '--name', postgres, '--label', label, '--network', identity,
                 '--network-alias', 'postgres', '--read-only', '--cap-drop=ALL',
                 '--security-opt=no-new-privileges', '--user', '70:70', '--memory=1g',
                 '--cpus=2', '--pids-limit=128', '--tmpfs',
                 '/var/lib/postgresql/data:rw,nosuid,nodev,size=256m,uid=70,gid=70,mode=700',
                 '--tmpfs', '/var/run/postgresql:rw,nosuid,nodev,size=16m,uid=70,gid=70',
                 '--tmpfs', '/tmp:rw,nosuid,nodev,size=16m,mode=1777',
                 '-e', 'POSTGRES_USER=wire-server', '-e', 'POSTGRES_PASSWORD=synthetic-migration-password',
                 '-e', 'POSTGRES_DB=owned_stores', POSTGRES])
        checked(['docker', 'run', '-d', '--name', cassandra, '--label', label, '--network', identity,
                 '--network-alias', 'cassandra', '--cap-drop=ALL', '--security-opt=no-new-privileges',
                 '--user', '999:999', '--memory=2g', '--cpus=2', '--pids-limit=512',
                 '--tmpfs', '/var/lib/cassandra:rw,nosuid,nodev,size=2g,uid=999,gid=999,mode=700',
                 '-e', 'MAX_HEAP_SIZE=512M', '-e', 'HEAP_NEWSIZE=128M', CASSANDRA])
        deadline = time.monotonic() + 150
        while time.monotonic() < deadline:
            ready = run(['docker', 'exec', cassandra, 'cqlsh', '-e', 'SELECT release_version FROM system.local'], timeout=10)
            if ready.returncode == 0:
                break
            time.sleep(2)
        else:
            raise AssertionError('owned Cassandra readiness failed')
        discovery = checked(candidate_command('discover', PYTHON) + [RUNNER, '-c',
            'import json,shutil,hashlib;from pathlib import Path;p=Path(shutil.which("brig-schema")).resolve();print(json.dumps({"path":str(p),"sha256":hashlib.sha256(p.read_bytes()).hexdigest()}))'])
        report['native_schema_binary'] = json.loads(discovery)
        schema = run(candidate_command('native-schema', report['native_schema_binary']['path']) +
                     [RUNNER, '--host', 'cassandra', '--keyspace', 'owned_brig_stores', '--replication-factor', '1'], timeout=90)
        report['native_schema_exit_code'] = schema.returncode
        (output / 'native-schema.stdout').write_text(schema.stdout)
        (output / 'native-schema.stderr').write_text(schema.stderr)
        assert schema.returncode == 0, 'native schema initialization failed'
        test = run(candidate_command('test', '/candidate/stores.test') +
                   ['--mount', f'type=bind,src={binary},dst=/candidate/stores.test,readonly',
                    '--mount', f'type=bind,src={config},dst=/fixture/brig.json,readonly',
                    '-e', 'BRIG_STORE_CONFIG=/fixture/brig.json', RUNNER,
                    '-test.run=^TestOwnedDurableStores$', '-test.v', '-test.timeout=55s'], timeout=65)
        report['test_exit_code'] = test.returncode
        (output / 'test.stdout').write_text(test.stdout)
        (output / 'test.stderr').write_text(test.stderr)
        assert test.returncode == 0, 'candidate durable store assertions failed'
        report['result'] = 'pass'
    except Exception as error:
        report['failure'] = str(error)
    finally:
        for name in ('postgres', 'cassandra'):
            logs = run(['docker', 'logs', identity + '-' + name])
            (output / (name + '.log')).write_text(logs.stdout + logs.stderr)
        containers = checked(['docker', 'container', 'ls', '-aq', '--filter', 'label=' + label]).split()
        if containers:
            checked(['docker', 'rm', '--force', *containers])
        run(['docker', 'network', 'rm', identity])
        report['cleanup'] = {
            'remaining_containers': checked(['docker', 'container', 'ls', '-aq', '--filter', 'label=' + label]),
            'remaining_networks': checked(['docker', 'network', 'ls', '-q', '--filter', 'label=' + label]),
            'remaining_volumes': checked(['docker', 'volume', 'ls', '-q', '--filter', 'label=' + label])}
        if any(report['cleanup'].values()):
            report['result'] = 'failed'
        (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))
    return 0 if report['result'] == 'pass' else 1


if __name__ == '__main__':
    raise SystemExit(main())
