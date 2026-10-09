#!/usr/bin/env python3
"""Compare early Go process health to retained actual Haskell health capture.

No dependency stack or business readiness is claimed. Only the synthetic worker
HTTP address changes; AMQP-only mode disables the unfinished dependency workers.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--receipt', type=Path, required=True)
    parser.add_argument('--fixtures', type=Path, required=True)
    parser.add_argument('--reference', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    receipt = json.loads(args.receipt.read_text())
    if args.output.exists() or receipt['binary_sha256'] != sha(binary):
        raise ValueError('output must be new and executable must match receipt')
    configs = json.loads(args.fixtures.read_text())
    worker, galley = configs['background-worker'], configs['galley']
    if any(value for key, value in worker.items() if key.startswith('migrate')):
        raise ValueError('early comparison requires disabled data migrations')
    native = json.loads(args.reference.read_text())['/i/status']
    if native['status'] != 200 or native['body'] != '':
        raise ValueError('actual Haskell health reference is absent or differs')
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    worker['backgroundWorker'] = {'host': '127.0.0.1', 'port': port}
    worker['rabbitmq'].pop('adminHost', None)
    worker['rabbitmq'].pop('adminPort', None)
    with tempfile.TemporaryDirectory(prefix='wire-worker-early-') as temporary:
        directory = Path(temporary)
        paths = []
        for name, config in (('worker', worker), ('galley', galley)):
            path = directory / (name + '.json')
            path.write_text(json.dumps(config))
            paths.append(path)
        process = subprocess.Popen(
            [str(binary), '-c', str(paths[0]), '--galley-config-file', str(paths[1])],
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            start_new_session=True)
        observed = {}
        try:
            deadline = time.monotonic() + 5
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            while True:
                if process.poll() is not None:
                    raise ValueError('early candidate exited before health')
                try:
                    with opener.open(f'http://127.0.0.1:{port}/i/status', timeout=1) as response:
                        observed = {'status': response.status, 'body': response.read().decode()}
                    break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise ValueError('early health timeout')
                    time.sleep(.01)
            if observed != {'status': native['status'], 'body': native['body']}:
                raise ValueError('early Go health differs from actual Haskell capture')
            try:
                opener.open(f'http://127.0.0.1:{port}/i/status/workers', timeout=1)
                raise ValueError('incomplete workers reported healthy')
            except urllib.error.HTTPError as error:
                readiness = {'status': error.code, 'body': error.read().decode()}
            expected = 'These workers are not working: backend-notification-pusher, dead-user-notification-watcher, background-job-consumer'
            if readiness != {'status': 500, 'body': expected}:
                raise ValueError('unstarted source worker readiness changed')
            started = time.monotonic()
            os.killpg(process.pid, signal.SIGTERM)
            exit_code = process.wait(timeout=5)
            shutdown = time.monotonic() - started
            if exit_code:
                raise ValueError('early signal shutdown failed')
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
            process.communicate(timeout=5)
    result = {'kind': 'early-process-health-comparison', 'result': 'pass',
              'binary_sha256': sha(binary), 'source_revision': receipt['source_revision'],
              'reference_sha256': sha(args.reference), 'fixtures_sha256': sha(args.fixtures),
              'observed_health': observed, 'construction_readiness': readiness,
              'exit_code': exit_code, 'shutdown_seconds': shutdown,
              'full_service_parity': False, 'e2e_executed': False,
              'limitation': 'Health only against actual retained Haskell capture; AMQP-only mode, no dependency jobs or migrations executed. Whole-service final tests and durable switchback remain mandatory.'}
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
