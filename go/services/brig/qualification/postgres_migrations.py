#!/usr/bin/env python3
"""Owned native/Go PostgreSQL migration observations; not whole Brig parity."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time
import uuid

ROOT = Path(__file__).resolve().parents[4]
RUNNER = 'sha256:f487c90fd364aac9daee5a1ce6e60fdba0ab74ab0c671ab0de0198e8f05659a9'
POSTGRES = 'postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24'
NATIVE = '/nix/store/f6l2f0l8l2r6z9jbg937q96k6056iqfn-brig/bin/brig'
NATIVE_SHA = 'c45a40da63268712a7cacdbd884287fa3dc93cec179d70323d3f22f7d5cdf1ca'
CONCURRENT = ('20260428072649-create-conv-parent-index.sql',
              '20260708090000-meetings-recurrence-eff-end-index.sql',
              '20260708100000-meetings-end-time-nonrecurring-index.sql')


def run(args, timeout=30):
    return subprocess.run(args, text=True, capture_output=True, timeout=timeout)


def checked(args, timeout=30):
    result = run(args, timeout)
    if result.returncode:
        raise RuntimeError('owned command failed: ' + result.stderr[:500])
    return result.stdout.strip()


def lock_key(name):
    # Independent transcription of the pinned native Hashable String instance.
    mask = (1 << 64) - 1

    def mix(a, b):
        product = a * b
        reverse = int.from_bytes(b.to_bytes(8, 'little'), 'big')
        z = (reverse + a + ((product >> 64) ^ (product & mask))) & mask
        z = ((z ^ (z >> 33)) * 0xff51afd7ed558ccd) & mask
        z = ((z ^ (z >> 33)) * 0xc4ceb9fe1a85ec53) & mask
        return z ^ (z >> 33)

    value = 14695981039346656037
    for char in name:
        value = mix(value, ord(char))
    value = mix(value, len(name))
    return value if value < (1 << 63) else value - (1 << 64)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--build-receipt', type=Path, required=True)
    parser.add_argument('--configs', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    output = args.output.absolute()
    if output.exists() or output.is_relative_to(ROOT):
        raise ValueError('new external output directory required')
    if checked(['git', '-C', str(ROOT), 'status', '--porcelain']):
        raise ValueError('commit executable and observation inputs first')
    revision = checked(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'])
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    receipt = json.loads(args.build_receipt.read_text())
    if receipt['source_revision'] != revision or receipt['binary_sha256'] != digest:
        raise ValueError('candidate build identity mismatch')
    configs_bytes = args.configs.read_bytes()
    config = json.loads(configs_bytes)['brig']
    # Synthetic credentials confined to an internal network and ephemeral DB.
    config['postgresql'] = {'host': 'postgres', 'port': '5432', 'user': 'wire-server',
                            'password': 'synthetic-migration-password', 'dbname': 'native_first'}
    config.pop('postgresqlPassword', None)
    output.mkdir(parents=True)
    config_file = output / 'brig.json'
    config_file.write_text(json.dumps(config, indent=2) + '\n')
    config_file.chmod(0o644)
    identity = 'wire-brig-migrations-' + uuid.uuid4().hex
    label = 'wire.migration.observation=' + identity
    network, postgres = identity, identity + '-postgres'
    report = {'result': 'failed', 'scope': __doc__, 'e2e_executed': False,
              'source_revision': revision, 'binary_sha256': digest,
              'runner_image': RUNNER, 'native_brig_sha256': NATIVE_SHA,
              'postgres_image': POSTGRES,
              'input_configs_sha256': hashlib.sha256(configs_bytes).hexdigest(),
              'fixture_sha256': hashlib.sha256(config_file.read_bytes()).hexdigest(),
              'records': [], 'snapshots': {}, 'locks': []}
    processes = []

    def sql(database, query):
        return checked(['docker', 'exec', postgres, 'psql', '-X', '-v', 'ON_ERROR_STOP=1',
                        '-U', 'wire-server', '-d', database, '-At', '-c', query])

    def snapshot(database):
        return sql(database, "SELECT coalesce(json_agg(t ORDER BY filename)::text,'[]') FROM "
                   '(SELECT filename,checksum,executed_at::text FROM schema_migrations) t')

    def schema(database):
        # pg_dump has randomized restrict markers; retain only schema SQL.
        dump = checked(['docker', 'exec', postgres, 'pg_dump', '-U', 'wire-server',
                        '-d', database, '--schema-only', '--no-owner', '--no-privileges'])
        return '\n'.join(line for line in dump.splitlines()
                         if not line.startswith(('\\restrict ', '\\unrestrict ')))

    def cli(implementation, database, extra=(), background=False):
        name = identity + '-' + uuid.uuid4().hex[:12]
        cmd = ['docker', 'run', '--rm', '--name', name, '--label', label, '--network', network,
               '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
               '--user', '65534:65534', '--memory=512m', '--cpus=1', '--pids-limit=128',
               '--tmpfs', '/tmp:rw,nosuid,nodev,size=32m,mode=1777',
               '--mount', f'type=bind,src={config_file},dst=/fixture/brig.json,readonly']
        entrypoint = NATIVE
        if implementation == 'go':
            cmd += ['--mount', f'type=bind,src={binary},dst=/candidate/brig,readonly']
            entrypoint = '/candidate/brig'
        cmd += ['--entrypoint', entrypoint, RUNNER, '--config-file', '/fixture/brig.json',
                'migrate-postgres', '--dbname', database, *extra]
        if background:
            process = subprocess.Popen(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            processes.append(process)
            return process
        result = run(cmd, 60)
        record = {'implementation': implementation, 'database': database, 'args': list(extra),
                  'exit_code': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr}
        report['records'].append(record)
        return result.returncode

    try:
        checked(['docker', 'network', 'create', '--internal', '--label', label, network])
        checked(['docker', 'run', '-d', '--name', postgres, '--label', label, '--network', network,
                 '--network-alias', 'postgres', '--read-only', '--cap-drop=ALL',
                 '--security-opt=no-new-privileges', '--user', '70:70', '--memory=1g',
                 '--cpus=2', '--pids-limit=128', '--tmpfs',
                 '/var/lib/postgresql/data:rw,nosuid,nodev,size=256m,uid=70,gid=70,mode=700',
                 '--tmpfs', '/var/run/postgresql:rw,nosuid,nodev,size=16m,uid=70,gid=70',
                 '--tmpfs', '/tmp:rw,nosuid,nodev,size=16m,mode=1777',
                 '-e', 'POSTGRES_USER=wire-server', '-e', 'POSTGRES_PASSWORD=synthetic-migration-password',
                 '-e', 'POSTGRES_DB=native_first', POSTGRES])
        for _ in range(60):
            if run(['docker', 'exec', postgres, 'pg_isready', '-U', 'wire-server']).returncode == 0:
                break
            time.sleep(0.5)
        else:
            raise AssertionError('owned PostgreSQL readiness failed')
        sql('native_first', 'CREATE DATABASE go_first')
        assert cli('haskell', 'native_first') == 0, 'native fresh migration failed'
        native = snapshot('native_first')
        report['snapshots']['native_initial'] = json.loads(native)
        assert len(json.loads(native)) == 32, 'native script count differs'
        assert cli('go', 'native_first') == 0, 'Go rejected native ledger'
        assert snapshot('native_first') == native, 'Go changed native ledger on rerun'
        assert cli('haskell', 'native_first') == 0
        assert snapshot('native_first') == native
        assert cli('go', 'go_first') == 0, 'Go fresh migration failed'
        go = snapshot('go_first')
        report['snapshots']['go_initial'] = json.loads(go)
        assert [(r['filename'], r['checksum']) for r in json.loads(native)] == [
            (r['filename'], r['checksum']) for r in json.loads(go)], 'ledger identity differs'
        assert cli('haskell', 'go_first') == 0, 'native rejected Go ledger'
        assert snapshot('go_first') == go, 'native changed Go ledger on rerun'
        native_schema, go_schema = schema('native_first'), schema('go_first')
        (output / 'native-schema.sql').write_text(native_schema + '\n')
        (output / 'go-schema.sql').write_text(go_schema + '\n')
        assert native_schema == go_schema, 'fresh native/Go database schemas differ'
        first = json.loads(go)[0]
        sql('go_first', "UPDATE schema_migrations SET checksum='synthetic-mismatch' WHERE filename='" + first['filename'] + "'")
        changed = snapshot('go_first')
        for implementation in ('haskell', 'go'):
            assert cli(implementation, 'go_first') == 1, 'checksum mismatch accepted'
            assert snapshot('go_first') == changed, 'rejected migration changed ledger'
        assert cli('go', 'go_first', ['--reset']) == 0, 'Go reset failed'
        assert cli('haskell', 'go_first') == 0, 'native rejected Go reset ledger'
        assert snapshot('native_first') == native, 'dbname override modified other database'
        # The native CLI acquires these locks even when the script is already
        # applied. Blocking on each calculated key proves the deployed binary's
        # hash seed/instance as well as the source-derived algorithm.
        for name, implementation in ((name, implementation) for name in CONCURRENT
                                     for implementation in ('haskell', 'go')):
            key = lock_key(name)
            holder = subprocess.Popen(['docker', 'exec', '-i', postgres, 'psql', '-X', '-At',
                                       '-v', 'ON_ERROR_STOP=1', '-U', 'wire-server', '-d', 'native_first'],
                                      text=True, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                      stderr=subprocess.PIPE)
            processes.append(holder)
            holder.stdin.write(f'SELECT pg_advisory_lock({key}::bigint);\n')
            holder.stdin.flush()
            for _ in range(40):
                if sql('native_first', "SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND granted") == '1':
                    break
                time.sleep(0.1)
            else:
                raise AssertionError('lock holder failed')
            migration = cli(implementation, 'native_first', background=True)
            blocked = False
            started = time.monotonic()
            for _ in range(50):
                if migration.poll() is not None:
                    break
                query = sql('native_first', "SELECT query FROM pg_stat_activity WHERE pid<>pg_backend_pid() AND query LIKE '%pg_try_advisory_lock%' LIMIT 1")
                if query and time.monotonic() - started >= 1.2:
                    blocked = True
                    break
                time.sleep(0.1)
            report['locks'].append({'script': name, 'key': key, 'implementation': implementation,
                                    'blocked': blocked})
            assert blocked, implementation + ' migration did not wait for calculated advisory key'
            holder.stdin.write(f'SELECT pg_advisory_unlock({key}::bigint);\n\\q\n')
            holder.stdin.flush()
            holder.communicate(timeout=10)
            stdout, stderr = migration.communicate(timeout=15)
            report['records'].append({'implementation': implementation, 'database': 'native_first',
                                      'lock_script': name, 'exit_code': migration.returncode,
                                      'stdout': stdout, 'stderr': stderr})
            assert holder.returncode == 0 and migration.returncode == 0
            assert snapshot('native_first') == native
        report['result'] = 'pass'
    except Exception as error:
        report['failure'] = str(error)
    finally:
        for process in processes:
            if process.poll() is None:
                process.terminate()
                try:
                    process.communicate(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.communicate(timeout=5)
        containers = checked(['docker', 'container', 'ls', '-aq', '--filter', 'label=' + label]).split()
        if containers:
            checked(['docker', 'rm', '--force', *containers])
        run(['docker', 'network', 'rm', network])
        report['cleanup'] = {
            'remaining_containers': checked(['docker', 'container', 'ls', '-aq', '--filter', 'label=' + label]),
            'remaining_networks': checked(['docker', 'network', 'ls', '-q', '--filter', 'label=' + label]),
            'remaining_volumes': checked(['docker', 'volume', 'ls', '-q', '--filter', 'label=' + label])}
        if any(report['cleanup'].values()):
            report['result'] = 'failed'
        (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({k: report.get(k) for k in ('result', 'failure', 'cleanup')}))
    return 0 if report['result'] == 'pass' else 1


if __name__ == '__main__':
    raise SystemExit(main())
