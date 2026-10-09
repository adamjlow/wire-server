"""Run unchanged original77, then smoke exact images using the same owned stack."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / 'tools/parity/docker'))
import environment as setup

CACHE = Path('/home/operator/.cache/wire-migration/go-patched-20261009')
OUTPUT = CACHE / 'five-go'
PYTHON = '/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3'
RECEIPT = ROOT / 'docs/go-migration/services/federator/oracle/runner-build.json'
SERVICES = ('proxy', 'cargohold', 'cannon', 'gundeck')
IMAGES = {s: json.loads((CACHE / (s + '-image.json')).read_text()) for s in SERVICES}
SELECT = [(s, str(CACHE / s), str(CACHE / (s + '-image.json'))) for s in SERVICES]
SELECT.append(('federator', '/home/operator/.cache/wire-migration/federator-20261009-5',
               '/home/operator/.cache/wire-migration/federator-20261009-5-build.json'))
ORIGINAL_RUN = subprocess.run


def call(args, timeout=60):
    result = ORIGINAL_RUN(list(map(str, args)), check=True, capture_output=True,
                          text=True, timeout=timeout)
    return result.stdout.strip()


def image_smokes(compose):
    helper = None
    owned = []
    results = []
    try:
        helper = call([*compose, 'run', '--detach', '--no-deps', '--name',
                       'wire-patched-image-namespace-' + uuid.uuid4().hex,
                       '--entrypoint', PYTHON, 'runner',
                       '/source/docs/go-migration/qualifications/go-patched-20261009/image_namespace.py'])
        # Compose may print initialization messages; the returned ID must be exact.
        if len(helper) != 64 or any(c not in '0123456789abcdef' for c in helper):
            raise ValueError('invalid owned namespace ID')
        deadline = time.monotonic() + 40
        while not (OUTPUT / 'reports/patch-image-ready').exists():
            if time.monotonic() > deadline:
                raise RuntimeError('image namespace not ready')
            time.sleep(.1)
        for service, port in [('proxy', 18087), ('cargohold', 18084), ('cannon', 18083), ('gundeck', 18086)]:
            receipt = IMAGES[service]
            config = OUTPUT / 'reports/patch-image-configs' / (service + '.json')
            args = ['docker', 'create', '--name', 'wire-patched-image-' + uuid.uuid4().hex,
                    '--network', 'container:' + helper, '--user', '65534:65534',
                    '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
                    '--pids-limit=256', '--memory=512m', '--cpus=2',
                    '--tmpfs', '/tmp:rw,nosuid,nodev,size=32m,mode=1777',
                    '--mount', 'type=bind,source=' + str(ROOT) + ',target=/source,readonly',
                    '--mount', 'type=bind,source=' + str(config) + ',target=/etc/wire/' + service + '/conf/' + service + '.yaml,readonly']
            for key, value in setup.SYNTHETIC_ENV.items():
                args += ['--env', key + '=' + value]
            container = call([*args, receipt['image_id']])
            owned.append(container)
            call(['docker', 'start', container])
            deadline = time.monotonic() + 30
            while True:
                probe = ORIGINAL_RUN(['docker', 'exec', helper, PYTHON, '-c',
                    "import urllib.request; r=urllib.request.urlopen('http://127.0.0.1:" + str(port) + "/i/status',timeout=1); print(r.status)"],
                    capture_output=True, text=True, timeout=5)
                if probe.returncode == 0 and probe.stdout.strip() == '200':
                    break
                if time.monotonic() > deadline:
                    raise RuntimeError(service + ' image failed health; inspect retained log')
                if call(['docker', 'inspect', '-f', '{{.State.Running}}', container]) != 'true':
                    raise RuntimeError(service + ' image exited before health')
                time.sleep(.1)
            inspect = json.loads(call(['docker', 'inspect', container]))[0]
            assert inspect['Config']['User'] == '65534:65534'
            assert inspect['HostConfig']['ReadonlyRootfs'] is True
            assert inspect['HostConfig']['CapDrop'] == ['ALL']
            assert 'no-new-privileges' in inspect['HostConfig']['SecurityOpt']
            assert not inspect['HostConfig']['PortBindings']
            lifecycle = []
            if service == 'gundeck':
                for command, exit_code in [('sleep 10', 0), ('sleep 10; echo synthetic', 2)]:
                    started = time.monotonic()
                    hook = ORIGINAL_RUN(['docker', 'exec', container, '/bin/sh', '-c', command],
                                        capture_output=True, text=True, timeout=15)
                    seconds = time.monotonic() - started
                    assert hook.returncode == exit_code
                    if exit_code == 0:
                        assert 9.5 <= seconds <= 14
                    lifecycle.append({'command': command, 'exit_code': hook.returncode,
                                      'seconds': round(seconds, 3)})
            started = time.monotonic()
            call(['docker', 'kill', '--signal=SIGTERM', container])
            exit_code = int(call(['docker', 'wait', container], timeout=40))
            seconds = time.monotonic() - started
            assert exit_code == 0 and seconds <= 35
            (CACHE / (service + '-image.log')).write_text(call(['docker', 'logs', container]))
            results.append({'service': service, 'result': 'pass', 'image_id': receipt['image_id'],
                            'binary_sha256': receipt['binary_sha256'], 'source_revision': receipt['source_revision'],
                            'GoVersion': receipt['build_info']['GoVersion'], 'health': 200,
                            'default_config_path': True, 'user': '65534:65534', 'read_only': True,
                            'cap_drop': ['ALL'], 'no_new_privileges': True, 'host_ports': {},
                            'network': 'owned disposable namespace after original suites finish',
                            'shutdown_exit_code': exit_code, 'shutdown_seconds': round(seconds, 3),
                            'lifecycle': lifecycle})
            call(['docker', 'rm', container]); owned.remove(container)
    finally:
        for container in owned:
            (CACHE / ('failed-image-' + container[:12] + '.log')).write_text(call(['docker', 'logs', container]))
            call(['docker', 'rm', '--force', container])
        if helper:
            (OUTPUT / 'reports/patch-image-stop').touch()
            # Tunnel shutdown is bounded; stop/remove only this owned container.
            call(['docker', 'stop', '--time', '10', helper], timeout=20)
            call(['docker', 'rm', helper])
        (CACHE / 'image-smokes.json').write_text(json.dumps({'result': 'pass' if len(results) == 4 else 'failed',
            'checks': results, 'owned_cleanup': True}, indent=2) + '\n')


def original_then_images(args, *positional, **kwargs):
    result = ORIGINAL_RUN(args, *positional, **kwargs)
    if (isinstance(args, list) and args[:2] == ['docker', 'compose']
            and args[-4:] == ['run', '--rm', '--no-deps', 'runner'] and result.returncode == 0):
        image_smokes(args[:-4])
    return result


if __name__ == '__main__':
    setup.prepare(OUTPUT, RECEIPT, None, None, SELECT)
    subprocess.run = original_then_images
    try:
        result = setup.run(OUTPUT, 'proxy-cargohold-cannon-gundeck-federator')
    finally:
        subprocess.run = ORIGINAL_RUN
    print(json.dumps(result, indent=2))
