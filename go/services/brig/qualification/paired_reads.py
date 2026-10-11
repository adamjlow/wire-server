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
from urllib.parse import urlencode

ROOT = Path(__file__).resolve().parents[4]
IMAGE = 'sha256:f487c90fd364aac9daee5a1ce6e60fdba0ab74ab0c671ab0de0198e8f05659a9'
PYTHON = '/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3'


def checked(args, timeout=30):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError('owned command failed: ' + result.stderr[-1000:])
    return result.stdout.strip()


def inside(gundeck_path=None, access_switchback=False, login_contract=False, activation_contract=False, registration_contract=False):
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
            if method == 'POST' and path == '/i/users':
                result['location'] = response.getheader('Location')
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
        registration_body, registration_reference, registration_reads = None, None, None
        if registration_contract:
            actor = str(uuid.uuid4())
            registration_password = 'Synthetic-registration-password-A!'
            predecessor_password = 'Synthetic-registration-password-B!'
            registration_body = {'name': 'Synthetic registration reference', 'uuid': actor,
                'managed_by': 'scim', 'password': registration_password, 'supported_protocols': []}
            registration_reference, _ = request('POST', '/i/users', body=registration_body)
            assert registration_reference['status'] == 201, 'native registration reference failed'
            assert registration_reference['json']['id'] == actor and registration_reference['location'] == actor, 'native registration actor/Location changed'
            registration_cases = [('GET', '/self', {'Z-User': actor}),
                ('GET', '/i/users/' + actor + '/features/conferenceCalling', {}),
                ('GET', '/i/users/' + actor + '/reauthenticate', {}, {'password': registration_password}),
                ('GET', '/i/users/' + actor + '/reauthenticate', {}, {'password': predecessor_password})]
            registration_reads = [request(*case)[0] for case in registration_cases]
            assert [record['status'] for record in registration_reads] == [200, 200, 200, 403], 'native registration reference reads failed'
            staged, _ = request('POST', '/i/users', body={**registration_body,
                'name': 'Synthetic registration predecessor', 'password': predecessor_password})
            assert staged['status'] == 201, 'native same-actor registration staging failed'
            staged_profile, _ = request('GET', '/self', {'Z-User': actor})
            old_password, _ = request(*registration_cases[2])
            staged_password, _ = request(*registration_cases[3])
            assert staged_profile['status'] == 200 and staged_profile['json']['name'] == 'Synthetic registration predecessor', 'native predecessor profile not persisted'
            assert old_password['status'] == 403 and staged_password['status'] == 200, 'native predecessor password not persisted'
            report['registration_native_reference'] = registration_reference
            report['registration_native_reads'] = registration_reads
            report['registration_native_staging'] = {'same_actor': True, 'profile_changed': True,
                'reference_password_rejected_status': old_password['status'], 'predecessor_password_accepted_status': staged_password['status']}
        login, cookie = request('POST', '/login?persist=true', body={
            'email': created['json']['email'], 'password': 'Synthetic-password-2026!',
            'label': 'synthetic-paired-device'})
        # Token bytes are synthetic but unnecessary for read parity evidence.
        report['native_setup'].append({'operation': 'persistent login', 'status': login['status'], 'cookie_issued': bool(cookie)})
        assert login['status'] == 200 and cookie, 'native session setup failed'
        activation_pair = None
        if activation_contract:
            original_email = created['json']['email']
            activation_email = 'synthetic-activation-' + uuid.uuid4().hex + '@example.com'

            def stage_email(email):
                pending, _ = request('PUT', '/access/self/email', {
                    'Cookie': cookie.split(';', 1)[0],
                    'Authorization': 'Bearer ' + login['json']['access_token']}, {'email': email})
                assert pending['status'] == 202, 'native pending email setup failed'
                pair, _ = request('GET', '/i/users/activation-code?' + urlencode({'email': email}))
                assert pair['status'] == 200 and set(pair['json']) == {'key', 'code'}, 'native activation pair lookup failed'
                return pair['json']

            pair = stage_email(activation_email)
            native_activation, _ = request('POST', '/activate', body={**pair, 'dryrun': False})
            assert native_activation['status'] == 200 and native_activation['json'] == {'email': activation_email, 'first': False}, 'native email activation reference failed'
            native_repeat, _ = request('POST', '/activate', body={**pair, 'dryrun': False})
            assert native_repeat['status'] == 204, 'native activation repeat reference failed'
            # Restore through original APIs, then stage the identical target.
            # Go must perform the same transition, rather than consume an
            # already-activated row or weaken comparison of dynamic fields.
            restore_pair = stage_email(original_email)
            restored, _ = request('POST', '/activate', body={**restore_pair, 'dryrun': False})
            assert restored['status'] == 200, 'native original identity restore failed'
            activation_pair = stage_email(activation_email)
            report['native_activation_reference'] = native_activation
            report['native_activation_repeat_reference'] = native_repeat
        reference_access = None
        second_login, second_cookie = None, None
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
        if login_contract:
            cases += [('POST', '/login', {}, {'email': created['json']['email'], 'password': 'Wrong-synthetic-password'}),
                      ('POST', '/login', {}, {'email': 'synthetic-absent-' + user + '@example.com', 'password': 'Synthetic-password-2026!'}),
                      ('POST', '/login?persist=invalid-synthetic', {}, {'email': created['json']['email'], 'password': 'Synthetic-password-2026!'})]
        if activation_contract:
            cases += [('GET', '/activate?key=synthetic-missing&code=000000', {}),
                      ('HEAD', '/activate?key=synthetic-missing&code=000000', {}),
                      ('POST', '/activate', {}, {'key': 'synthetic-missing', 'code': '000000', 'dryrun': True})]
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
        if registration_contract:
            def same_registration_response(reference, actual, creation=False):
                fields = ('status', 'content_type', 'json' if 'json' in reference else 'body')
                if creation:
                    fields += ('location',)
                assert all(reference.get(field) == actual.get(field) for field in fields), 'registration response mismatch: ' + ','.join(field for field in fields if reference.get(field) != actual.get(field))

            go_created, _ = request('POST', '/i/users', body=registration_body)
            report['registration_go_creation'] = go_created
            same_registration_response(registration_reference, go_created, True)
            registration_go_reads = [request(*case)[0] for case in registration_cases]
            report['registration_go_reads'] = registration_go_reads
            for reference, actual in zip(registration_reads, registration_go_reads):
                same_registration_response(reference, actual)
            active.terminate()
            report['go_registration_stop_exit_code'] = active.wait(timeout=35)
            assert report['go_registration_stop_exit_code'] == 0, 'Go registration process did not stop cleanly'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'Go registration listener remains'
            log = open('/reports/brig-native-registration-switchback.log', 'w')
            logs.append(log)
            active = subprocess.Popen([str(native_brig_path), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                      cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
            processes.append(active)
            ready(active)
            registration_native_consumed = [request(*case)[0] for case in registration_cases]
            report['registration_native_consumed'] = registration_native_consumed
            for reference, actual in zip(registration_reads, registration_native_consumed):
                same_registration_response(reference, actual)
            report['registration_switchback'] = {'same_actor_and_initialized_stores': True,
                'native_creation_then_native_predecessor_then_go_recreation': True,
                'exact_creation_status_media_json_location': True,
                'native_consumed_restored_profile_password_and_conference_feature': True,
                'native_rejected_predecessor_password': True,
                'dynamic_fields_normalized': False,
                'scope': 'identityless SCIM-managed internal creation; no invitation/SSO/public/email activation proof'}
        if activation_contract:
            # Native fixture activation codes live four seconds. Establish a
            # fresh account before closing peer clients, then issue its first
            # sessions/code only after their idle connections have drained.
            active.terminate()
            report['go_activation_reads_stop_exit_code'] = active.wait(timeout=35)
            assert report['go_activation_reads_stop_exit_code'] == 0, 'Go activation read process did not stop cleanly'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'Go activation read listener remains'
            log = open('/reports/brig-native-activation-setup.log', 'w')
            logs.append(log)
            active = subprocess.Popen([str(native_brig_path), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                      cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
            processes.append(active)
            ready(active)
            handoff, _ = request('POST', '/i/users', body={'name': 'Synthetic activation handoff',
                'email': 'synthetic-activation-origin-' + uuid.uuid4().hex + '@example.com',
                'password': 'Synthetic-password-2026!'})
            assert handoff['status'] == 201, 'native fresh activation account setup failed'
            report['activation_reference_actor'] = user
            user, original_email = handoff['json']['id'], handoff['json']['email']
            peer_processes = [process for process in processes if process is not active and process.poll() is None]
            for process in peer_processes:
                process.terminate()
            peer_exits = [process.wait(timeout=35) for process in peer_processes]
            report['activation_quiesced_peer_exit_codes'] = peer_exits
            assert all(code == 0 for code in peer_exits), 'activation peers did not quiesce cleanly'
            # Keep required profile/notification dependencies live without
            # restarting background clients that held idle Brig connections.
            restarted_peers = []
            for name in ('galley', 'gundeck'):
                executable = gundeck_path if name == 'gundeck' and gundeck_path else str(runtime.WORK / 'dist' / name)
                log = open('/reports/' + name + '-activation-restarted.log', 'w')
                logs.append(log)
                process = subprocess.Popen([executable, '-c', str(runtime.WORK / (name + '-static-0.json'))],
                                           cwd=runtime.WORK / 'services' / name, stdout=log, stderr=subprocess.STDOUT)
                processes.append(process)
                restarted_peers.append((process, peers[name][name]['port']))
            deadline = time.monotonic() + 120
            for process, port in restarted_peers:
                while True:
                    assert process.poll() is None, 'required activation peer exited'
                    try:
                        with socket.create_connection(('127.0.0.1', port), timeout=1):
                            break
                    except OSError:
                        assert time.monotonic() < deadline, 'required activation peer readiness failed'
                        time.sleep(0.5)
            report['activation_required_peers_restarted'] = ['galley', 'gundeck']
            login, cookie = request('POST', '/login?persist=true', body={'email': original_email,
                'password': 'Synthetic-password-2026!', 'label': 'synthetic-activation-handoff'})
            assert login['status'] == 200 and cookie, 'native fresh activation session setup failed'
            activation_pair = stage_email(activation_email)
            native_dryrun, _ = request('POST', '/activate', body={**activation_pair, 'dryrun': True})
            assert native_dryrun['status'] == 200 and native_dryrun['body'] == '', 'native fresh activation dryrun failed'
            handoff_started = time.monotonic()
            active.terminate()
            report['native_activation_setup_stop_exit_code'] = active.wait(timeout=35)
            assert report['native_activation_setup_stop_exit_code'] == 0, 'native activation setup did not stop cleanly'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'native activation setup listener remains'
            log = open('/reports/brig-go-activation.log', 'w')
            logs.append(log)
            active = subprocess.Popen([str(candidate), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                      cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
            processes.append(active)
            ready(active)
            report['activation_native_to_go_seconds'] = time.monotonic() - handoff_started
            go_dryrun, _ = request('POST', '/activate', body={**activation_pair, 'dryrun': True})
            report['activation_dryrun_pair'] = [native_dryrun, go_dryrun]
            assert all(native_dryrun.get(key) == go_dryrun.get(key) for key in ('status', 'content_type', 'body')), 'native/Go fresh dryrun mismatch'
            go_activation, _ = request('POST', '/activate', body={**activation_pair, 'dryrun': False})
            go_repeat, _ = request('POST', '/activate', body={**activation_pair, 'dryrun': False})
            for name, native, go in [('activation', native_activation, go_activation), ('repeat', native_repeat, go_repeat)]:
                fields = ('status', 'content_type', 'json' if 'json' in native else 'body')
                report['go_' + name + '_observation'] = go
                assert all(native.get(key) == go.get(key) for key in fields), 'native/Go ' + name + ' response mismatch'
            go_profile, _ = request('GET', '/self', {'Z-User': user})
            assert go_profile['status'] == 200 and go_profile['json']['email'] == activation_email and 'email_unvalidated' not in go_profile['json'], 'Go activated profile/clear pending state failed'
            active.terminate()
            report['go_activation_stop_exit_code'] = active.wait(timeout=35)
            assert report['go_activation_stop_exit_code'] == 0, 'Go activation process did not stop cleanly'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'Go activation listener remains'
            log = open('/reports/brig-native-activation-switchback.log', 'w')
            logs.append(log)
            active = subprocess.Popen([str(native_brig_path), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                      cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
            processes.append(active)
            ready(active)
            native_profile, _ = request('GET', '/self', {'Z-User': user})
            assert native_profile['status'] == 200 and native_profile['json'] == go_profile['json'], 'native Brig rejected Go-activated profile state'
            consumed_repeat, _ = request('POST', '/activate', body={**activation_pair, 'dryrun': False})
            assert all(consumed_repeat.get(key) == native_repeat.get(key) for key in ('status', 'content_type', 'body')), 'native repeat-link state differs after Go activation'
            new_login, new_cookie = request('POST', '/login?persist=true', body={'email': activation_email, 'password': 'Synthetic-password-2026!', 'label': 'synthetic-activated-device'})
            assert new_login['status'] == 200 and new_cookie, 'native Brig cannot log in using Go-activated email'
            old_login, _ = request('POST', '/login', body={'email': original_email, 'password': 'Synthetic-password-2026!'})
            assert old_login['status'] == 403 and old_login['json']['label'] == 'invalid-credentials', 'native Brig accepted released old email key'
            report['activation_switchback'] = {
                'same_initialized_stores': True, 'native_restored_and_restaged_same_target': True,
                'go_activation_status': go_activation['status'], 'go_repeat_status': go_repeat['status'],
                'native_consumed_profile_status': native_profile['status'],
                'native_repeat_status': consumed_repeat['status'], 'native_new_email_login_status': new_login['status'],
                'native_released_old_email_login_status': old_login['status'],
                'profile_exactly_matches': True, 'key_and_code_recorded': False,
                'handoff_actor': user, 'response_reference_actor_distinct': True,
                'required_profile_and_notification_peers_live': True}
        if access_switchback:
            # The fixture has a ten-second inactivity policy. Password checks
            # deliberately precede this separate account/session handoff; do
            # not stretch that policy or age credentials through those checks.
            active.terminate()
            report['go_reads_stop_exit_code'] = active.wait(timeout=35)
            assert report['go_reads_stop_exit_code'] == 0, 'Go read process did not stop cleanly'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'Go read listener remains'
            log = open('/reports/brig-native-credential-setup.log', 'w')
            logs.append(log)
            active = subprocess.Popen([str(native_brig_path), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                      cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
            processes.append(active)
            ready(active)
            created, _ = request('POST', '/i/users', body={'name': 'Synthetic credential handoff',
                'email': 'synthetic-handoff-' + uuid.uuid4().hex + '@example.com',
                'password': 'Synthetic-password-2026!'})
            assert created['status'] == 201, 'native handoff account setup failed'
            # Quiesce peer clients before issuing short-lived inactivity
            # witnesses. Native Warp otherwise drains their idle connections
            # for its full shutdown interval. This non-team handoff uses real
            # Cassandra/Elasticsearch, without a Galley/Gundeck request.
            peer_processes = [process for process in processes
                              if process is not active and process.poll() is None]
            for process in peer_processes:
                process.terminate()
            peer_exits = [process.wait(timeout=35) for process in peer_processes]
            report['quiesced_peer_exit_codes'] = peer_exits
            assert all(code == 0 for code in peer_exits), 'peer did not quiesce cleanly'
            login, cookie = request('POST', '/login?persist=true', body={
                'email': created['json']['email'], 'password': 'Synthetic-password-2026!',
                'label': 'synthetic-paired-device'})
            assert login['status'] == 200 and cookie, 'native handoff session setup failed'
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
            report['handoff_cookie_observation'] = request('GET', '/cookies', {'Z-User': created['json']['id']})[0]
            transition_started = time.monotonic()
            active.terminate()
            report['native_credentials_stop_exit_code'] = active.wait(timeout=35)
            assert report['native_credentials_stop_exit_code'] == 0, 'native credential process did not stop cleanly'
            report['native_credentials_stop_seconds'] = time.monotonic() - transition_started
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'native credential listener remains'
            log = open('/reports/brig-go-credentials.log', 'w')
            logs.append(log)
            active = subprocess.Popen([str(candidate), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                      cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
            processes.append(active)
            ready(active)
            report['native_to_go_transition_seconds'] = time.monotonic() - transition_started
            renewed, replacement = request('POST', '/access', {
                'Cookie': cookie.split(';', 1)[0],
                'Authorization': 'Bearer ' + reference_access['json']['access_token']})
            report['go_renewal_observation'] = {
                'status': renewed['status'],
                'content_type': renewed['content_type'],
                'json': {key: value for key, value in renewed.get('json', {}).items()
                         if key != 'access_token'},
                'replacement_cookie_issued': bool(replacement),
                'observed_at': time.time()}
            assert renewed['status'] == 200, 'Go renewal of native-issued credentials failed'
            native_json, go_json = reference_access['json'], renewed['json']
            assert native_json.keys() == go_json.keys(), 'renewal response field set mismatch'
            assert renewed['content_type'] == reference_access['content_type'], 'renewal media type mismatch'
            assert all(go_json[key] == native_json[key] for key in ('user', 'token_type', 'expires_in')), 'renewal response contract mismatch'
            assert isinstance(go_json['access_token'], str) and go_json['access_token'], 'Go access token missing'
            if replacement:
                cookie = replacement
            go_login, go_login_cookie = None, None
            if login_contract:
                go_login, go_login_cookie = request('POST', '/login?persist=true', body={
                    'email': created['json']['email'], 'password': 'Synthetic-password-2026!',
                    'label': 'synthetic-go-login-device'})
                report['go_login_observation'] = {
                    'status': go_login['status'], 'content_type': go_login['content_type'],
                    'json': {key: value for key, value in go_login.get('json', {}).items() if key != 'access_token'},
                    'cookie_issued': bool(go_login_cookie)}
                assert go_login['status'] == 200 and go_login_cookie, 'Go login on native-created account failed'
                assert go_login['json'].keys() == native_json.keys(), 'login response fields mismatch'
                assert go_login['content_type'] == reference_access['content_type'], 'login media type mismatch'
                assert all(go_login['json'][key] == native_json[key] for key in ('user', 'token_type', 'expires_in')), 'login response contract mismatch'
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
            if login_contract:
                login_consumed, _ = request('POST', '/access', {
                    'Cookie': go_login_cookie.split(';', 1)[0],
                    'Authorization': 'Bearer ' + go_login['json']['access_token']})
                report['native_consumed_go_login'] = {
                    'status': login_consumed['status'],
                    'json': {key: value for key, value in login_consumed.get('json', {}).items() if key != 'access_token'}}
                assert login_consumed['status'] == 200, 'native Brig rejected Go login cookie/token/store state'
                assert all(login_consumed['json'][key] == native_json[key] for key in ('user', 'token_type', 'expires_in')), 'native Go-login consumption contract mismatch'
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
    parser.add_argument('--login-contract', action='store_true')
    parser.add_argument('--activation-contract', action='store_true')
    parser.add_argument('--registration-contract', action='store_true')
    args = parser.parse_args()
    if args.registration_contract and (args.activation_contract or args.access_switchback or args.login_contract):
        parser.error('registration observation runs separately from credential/activation observations')
    if args.in_container:
        return inside(args.gundeck_path, args.access_switchback, args.login_contract, args.activation_contract, args.registration_contract)
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
    if args.login_contract:
        if not args.access_switchback:
            raise ValueError('login qualification requires credential switchback')
        runner['command'].append('--login-contract')
    if args.activation_contract:
        runner['command'].append('--activation-contract')
    if args.registration_contract:
        runner['command'].append('--registration-contract')
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
        # Use the existing full e2e controller's dependency sequence. Starting
        # the one-off cluster job with detached services does not establish
        # completed cluster initialization before native/Go peer startup.
        checked(command + ['up', '-d', *[name for name in environment.DEPENDENCIES if name != 'redis-cluster']], timeout=240)
        deadline = time.monotonic() + 90
        while True:
            try:
                checked(command + ['exec', '-T', 'redis-node-1', 'redis-cli', '--tls', '--cacert',
                                   '/usr/local/etc/redis/ca.pem', '-a', 'very-secure-redis-cluster-password',
                                   '-h', '172.20.0.36', '-p', '6378', 'ping'], timeout=10)
                break
            except RuntimeError:
                if time.monotonic() > deadline:
                    raise ValueError('owned Redis nodes unavailable')
                time.sleep(1)
        initialized = checked(command + ['run', '--rm', '--no-deps', 'redis-cluster'], timeout=90)
        (output / 'redis-cluster-initialization.log').write_text(initialized + '\n')
        result['redis_cluster_initialization_completed'] = True
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
