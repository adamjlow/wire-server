#!/usr/bin/env python3
"""Finite native -> Go read/reauthentication observations on native-created state."""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[4]
IMAGE = 'sha256:f487c90fd364aac9daee5a1ce6e60fdba0ab74ab0c671ab0de0198e8f05659a9'
PYTHON = '/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3'


def checked(args, timeout=30):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError('owned command failed: ' + result.stderr[-1000:])
    return result.stdout.strip()


def inside(gundeck_path=None, access_switchback=False):
    if os.getuid() != 65534 or os.environ.get('WIRE_E2E_DISPOSABLE') != '1':
        raise ValueError('owned nonroot Docker runner required')
    sys.path.insert(0, '/opt/wire-e2e')
    import runtime
    processes, logs, servers = [], [], []
    report = {'result': 'failed', 'e2e_executed': False, 'scope': __doc__, 'records': []}
    active = None

    def request(method, path, headers=None, body=None):
        connection = http.client.HTTPConnection('127.0.0.1', 8082, timeout=15)
        try:
            request_headers = {'Request-Id': 'synthetic-paired-read', **(headers or {})}
            if body is not None:
                body = json.dumps(body).encode()
                request_headers['Content-Type'] = 'application/json'
            connection.request(method, path, body, request_headers)
            response = connection.getresponse()
            data = response.read()
            result = {'method': method, 'path': path, 'status': response.status,
                      'content_type': response.getheader('Content-Type'), 'body': data.decode()}
            try:
                result['json'] = json.loads(data)
            except (ValueError, UnicodeError):
                pass
            return result, response.getheader('Set-Cookie')
        finally:
            connection.close()

    def ready(process):
        for _ in range(60):
            if process.poll() is not None:
                raise AssertionError('Brig process exited during startup')
            try:
                result, _ = request('GET', '/i/status')
                if result['status'] == 200:
                    return
            except OSError:
                pass
            time.sleep(0.5)
        raise AssertionError('Brig readiness failed')

    try:
        servers = runtime.start_forwards()
        runtime.wait_dependencies()
        runtime.stage()
        runtime.initialize()
        peers = json.loads(Path('/fixtures/peers.json').read_text())['0']
        # Direct Brig HTTP needs its configured native peers, but no Nginx or
        # second backend. Original end-to-end suites remain separate/full.
        for name, config in peers.items():
            path = runtime.WORK / (name + '-static-0.json')
            path.write_text(json.dumps(config))
            args = [str(runtime.WORK / 'dist' / name), '-c', str(path)]
            if name == 'gundeck' and gundeck_path:
                args[0] = gundeck_path
                report['go_gundeck_sha256'] = hashlib.sha256(Path(gundeck_path).read_bytes()).hexdigest()
            if name == 'background-worker':
                args += ['--galley-config-file', str(runtime.WORK / 'galley-static-0.json')]
            log = open('/reports/' + name + ('-go.log' if name == 'gundeck' and gundeck_path else '-native.log'), 'w')
            logs.append(log)
            process = subprocess.Popen(args, cwd=runtime.WORK / 'services' / name,
                                       stdout=log, stderr=subprocess.STDOUT)
            processes.append(process)
            if name == 'brig':
                active = process
                native_brig_path = Path(args[0]).resolve()
                report['native_brig'] = {'path': str(native_brig_path), 'sha256': hashlib.sha256(native_brig_path.read_bytes()).hexdigest()}
        ready(active)
        pending = {config[{'background-worker': 'backgroundWorker', 'wire-server-enterprise': 'wireServerEnterprise'}.get(name, name)]['port'] for name, config in peers.items()}
        deadline = time.monotonic() + 120
        while pending and time.monotonic() < deadline:
            assert all(process.poll() is None for process in processes), 'configured native peer exited'
            for port in tuple(pending):
                try:
                    with socket.create_connection(('127.0.0.1', port), timeout=1):
                        pending.remove(port)
                except OSError:
                    pass
            if pending:
                time.sleep(0.5)
        assert not pending, 'configured native peer readiness failed'
        created, _ = request('POST', '/i/users', body={'name': 'Synthetic parity reader',
            'email': 'synthetic-parity-' + uuid.uuid4().hex + '@example.com',
            'password': 'Synthetic-password-2026!'})
        report['native_setup'] = [created]
        assert created['status'] == 201, 'native account setup failed'
        user = created['json']['id']
        login, cookie = request('POST', '/login?persist=true', body={
            'email': created['json']['email'], 'password': 'Synthetic-password-2026!',
            'label': 'synthetic-paired-device'})
        # Token bytes are synthetic but unnecessary for read parity evidence.
        report['native_setup'].append({'operation': 'persistent login', 'status': login['status'], 'cookie_issued': bool(cookie)})
        assert login['status'] == 200 and cookie, 'native session setup failed'
        reference_access = None
        second_login, second_cookie = None, None
        if access_switchback:
            second_login, second_cookie = request('POST', '/login?persist=true', body={
                'email': created['json']['email'], 'password': 'Synthetic-password-2026!',
                'label': 'synthetic-revoked-device'})
            assert second_login['status'] == 200 and second_cookie, 'native second session setup failed'
            reference_access, replacement = request('POST', '/access', {
                'Cookie': cookie.split(';', 1)[0],
                'Authorization': 'Bearer ' + login['json']['access_token']})
            assert reference_access['status'] == 200, 'native renewal baseline failed'
            if replacement:
                cookie = replacement
        cases = [('GET', '/i/status', {}), ('HEAD', '/i/status', {}),
                 ('GET', '/api-version', {}), ('GET', '/self', {'Z-User': user}),
                 ('HEAD', '/self', {'Z-User': user}), ('GET', '/cookies', {'Z-User': user}),
                 ('GET', '/cookies?labels=synthetic-paired-device', {'Z-User': user}),
                 ('GET', '/cookies?labels=absent', {'Z-User': user}),
                 ('GET', '/i/users/' + user + '/status', {}), ('GET', '/self', {}),
                 ('GET', '/self', {'Z-User': 'invalid-synthetic-uuid'}),
                 ('GET', '/self', {'Z-User': '00000000000000000000000000000001'}),
                 ('GET', '/v1/self', {'Z-User': user}),
                 ('GET', '/i/users/' + user + '/reauthenticate', {}, {'password': 'Synthetic-password-2026!'}),
                 ('GET', '/i/users/' + user + '/reauthenticate', {}, {'password': 'Wrong-synthetic-password'}),
                 ('GET', '/i/users/' + user + '/reauthenticate', {}, {}),
                 ('GET', '/i/users/' + user + '/reauthenticate', {}, {'password': 'Synthetic-password-2026!', 'action': 'login'})]
        if access_switchback:
            cases += [('POST', '/access', {}), ('POST', '/access/logout', {}),
                      ('POST', '/access', {'Cookie': 'zuid=invalid-synthetic-token'}),
                      ('POST', '/access', {'Authorization': 'Basic synthetic'})]
        native_records = [request(*case)[0] for case in cases]
        report['records'].append({'implementation': 'haskell', 'observations': native_records})
        active.terminate()
        report['native_stop_exit_code'] = active.wait(timeout=35)
        assert report['native_stop_exit_code'] == 0, 'native Brig did not stop cleanly'
        # Listener closure is required before transferring process ownership.
        with socket.socket() as check:
            assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'native listener remains'
        candidate = Path('/candidate/brig')
        report['candidate_brig_sha256'] = hashlib.sha256(candidate.read_bytes()).hexdigest()
        log = open('/reports/brig-go.log', 'w')
        logs.append(log)
        active = subprocess.Popen([str(candidate), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                  cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
        processes.append(active)
        ready(active)
        go_records = [request(*case)[0] for case in cases]
        report['records'].append({'implementation': 'go', 'observations': go_records})
        mismatches = []
        for index, (native, go) in enumerate(zip(native_records, go_records)):
            fields = ('status', 'content_type', 'json' if 'json' in native else 'body')
            if any(native.get(key) != go.get(key) for key in fields):
                mismatches.append({'case': index, 'path': cases[index][1], 'fields': [key for key in fields if native.get(key) != go.get(key)]})
        report['mismatches'] = mismatches
        assert not mismatches, 'paired response mismatches'
        if access_switchback:
            renewed, replacement = request('POST', '/access', {
                'Cookie': cookie.split(';', 1)[0],
                'Authorization': 'Bearer ' + reference_access['json']['access_token']})
            assert renewed['status'] == 200, 'Go renewal of native-issued credentials failed'
            native_json, go_json = reference_access['json'], renewed['json']
            assert native_json.keys() == go_json.keys(), 'renewal response field set mismatch'
            assert renewed['content_type'] == reference_access['content_type'], 'renewal media type mismatch'
            assert all(go_json[key] == native_json[key] for key in ('user', 'token_type', 'expires_in')), 'renewal response contract mismatch'
            assert isinstance(go_json['access_token'], str) and go_json['access_token'], 'Go access token missing'
            if replacement:
                cookie = replacement
            revoked, _ = request('POST', '/access/logout', {
                'Cookie': second_cookie.split(';', 1)[0],
                'Authorization': 'Bearer ' + second_login['json']['access_token']})
            assert revoked['status'] == 200 and revoked['body'] == '', 'Go logout failed'
            active.terminate()
            report['go_switchback_stop_exit_code'] = active.wait(timeout=35)
            assert report['go_switchback_stop_exit_code'] == 0, 'Go Brig did not stop cleanly'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'Go listener remains'
            log = open('/reports/brig-native-switchback.log', 'w')
            logs.append(log)
            active = subprocess.Popen([str(native_brig_path), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                      cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
            processes.append(active)
            ready(active)
            consumed, _ = request('POST', '/access', {
                'Cookie': cookie.split(';', 1)[0],
                'Authorization': 'Bearer ' + go_json['access_token']})
            assert consumed['status'] == 200, 'native Brig rejected Go-issued token/cookie/state'
            assert all(consumed['json'][key] == native_json[key] for key in ('user', 'token_type', 'expires_in')), 'native consumption changed access contract'
            rejected, _ = request('POST', '/access', {
                'Cookie': second_cookie.split(';', 1)[0],
                'Authorization': 'Bearer ' + second_login['json']['access_token']})
            assert rejected['status'] == 403 and rejected['json']['label'] == 'invalid-credentials', 'native Brig accepted Go-revoked session'
            report['access_switchback'] = {
                'same_initialized_stores': True,
                'native_renewal_status': reference_access['status'],
                'go_renewal_status': renewed['status'],
                'go_logout_status': revoked['status'],
                'native_consumed_go_credentials_status': consumed['status'],
                'native_rejected_go_revocation_status': rejected['status'],
                'response_fields': sorted(go_json.keys()),
                'user_token_type_expires_and_media_match': True,
                'token_bytes_compared': False,
                'token_validation': 'actual original native HTTP consumed Go-issued credentials'}
        report['result'] = 'pass'
    except Exception as error:
        report['failure'] = str(error)
    finally:
        exits = []
        for process in processes:
            if process.poll() is None:
                process.terminate()
            try:
                exits.append(process.wait(timeout=35))
            except subprocess.TimeoutExpired:
                process.kill()
                exits.append(process.wait(timeout=5))
                report['forced_owned_process_stop'] = True
                report['result'] = 'failed'
        report['owned_process_exit_codes'] = exits
        if any(code != 0 for code in exits):
            report['result'] = 'failed'
            report.setdefault('failure', 'owned peer/process exit was nonzero')
        for log in logs:
            log.close()
        for server in servers:
            server.server_close()
        Path('/reports/paired-reads.json').write_text(json.dumps(report, indent=2) + '\n')
    return 0 if report['result'] == 'pass' else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--in-container', action='store_true')
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--build-receipt', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--gundeck-binary', type=Path)
    parser.add_argument('--gundeck-receipt', type=Path)
    parser.add_argument('--gundeck-path')
    parser.add_argument('--access-switchback', action='store_true')
    args = parser.parse_args()
    if args.in_container:
        return inside(args.gundeck_path, args.access_switchback)
    if args.binary is None or args.build_receipt is None or args.output is None:
        parser.error('identified binary, receipt and new output required')
    sys.path.insert(0, str(ROOT / 'tools/parity/docker'))
    import environment
    output = args.output.absolute()
    if output.exists() or output.is_relative_to(ROOT):
        raise ValueError('new external output required')
    build = json.loads(args.build_receipt.read_text())
    revision = checked(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'])
    assert build['source_revision'] == revision, 'current committed executable required'
    runner_receipt = output.parent / (output.name + '-runner.json')
    runner_receipt.write_text(json.dumps({'image_id': IMAGE, 'source_revision': '3922c9e2edc13983834cab02f693fd7c22bccf42'}))
    if bool(args.gundeck_binary) != bool(args.gundeck_receipt):
        raise ValueError('identified Go Gundeck binary and receipt required together')
    peer_selection = [('gundeck', args.gundeck_binary, args.gundeck_receipt)] if args.gundeck_binary else []
    record = environment.prepare(output, runner_receipt, peers=peer_selection)
    spec = json.loads((output / 'compose.json').read_text())
    previous = json.loads((output.parent / 'native-original310-fixture-corrected/environment.json').read_text())
    for name, image in previous['dependencies'].items():
        spec['services'][name]['image'] = image
    runner = spec['services']['runner']
    runner['entrypoint'] = [PYTHON, '/source/go/services/brig/qualification/paired_reads.py']
    runner['command'] = ['--in-container']
    if args.access_switchback:
        runner['command'].append('--access-switchback')
    if args.gundeck_binary:
        runner['command'] += ['--gundeck-path', '/candidate/gundeck']
        runner['volumes'].append({'type': 'bind', 'source': str(args.gundeck_binary.resolve(strict=True)),
                                  'target': '/candidate/gundeck', 'read_only': True})
    runner['volumes'].append({'type': 'bind', 'source': str(args.binary.resolve(strict=True)),
                              'target': '/candidate/brig', 'read_only': True})
    environment.identified_candidate(args.binary, args.build_receipt)
    environment.validate_isolation(spec)
    if shutil.disk_usage(output).free < 2 * 1024**3:
        raise ValueError('owned dependency disk floor unmet')
    compose = output / 'paired-compose.json'
    compose.write_text(json.dumps(spec, indent=2) + '\n')
    command = ['docker', 'compose', '-f', str(compose), '-p', record['project']]
    result = {'result': 'failed', 'source_revision': revision, 'build': build,
              'project': record['project'], 'compose_sha256': hashlib.sha256(compose.read_bytes()).hexdigest(),
              'runner_image': IMAGE, 'e2e_executed': False}
    result['peer_candidates'] = record['peer_candidates']
    try:
        checked(command + ['up', '-d', *environment.DEPENDENCIES], timeout=180)
        with (output / 'controller.log').open('w') as log:
            process = subprocess.run(command + ['run', '--rm', '--no-deps', 'runner'],
                                     stdout=log, stderr=subprocess.STDOUT, timeout=900)
        result['runner_exit_code'] = process.returncode
        report = json.loads((output / 'reports/paired-reads.json').read_text())
        if process.returncode == 0 and report['result'] == 'pass':
            result['result'] = 'pass'
    except Exception as error:
        result['failure'] = str(error)
    finally:
        cleanup = subprocess.run(command + ['down', '--volumes', '--remove-orphans'], capture_output=True, text=True, timeout=120)
        result['cleanup_exit_code'] = cleanup.returncode
        result['remaining_resources'] = {kind: checked(['docker', kind, 'ls', *(['-a'] if kind == 'container' else []), '-q', '--filter', 'label=com.docker.compose.project=' + record['project']])
                                         for kind in ('container', 'network', 'volume')}
        if cleanup.returncode or any(result['remaining_resources'].values()):
            result['result'] = 'failed'
        (output / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))
    return 0 if result['result'] == 'pass' else 1


if __name__ == '__main__':
    raise SystemExit(main())
