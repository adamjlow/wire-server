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


def inside(gundeck_path=None, access_switchback=False, login_contract=False, activation_contract=False, registration_contract=False, saml_registration_contract=False, public_registration_contract=False, password_reset_contract=False, password_change_contract=False, ses_feedback_contract=False, properties_contract=False, client_update_contract=False, client_registration_contract=False, client_lifecycle_contract=False, store_fixture_path=None, mls_upload_originals=False, stomp_internal_events=False, service_deletion_queue=False):
    registration_contract = registration_contract or saml_registration_contract
    if os.getuid() != 65534 or os.environ.get('WIRE_E2E_DISPOSABLE') != '1':
        raise ValueError('owned nonroot Docker runner required')
    sys.path.insert(0, '/opt/wire-e2e')
    import runtime
    processes, logs, servers = [], [], []
    report = {'result': 'failed', 'e2e_executed': False, 'scope': __doc__, 'records': []}
    active = None
    original_failures = []
    if stomp_internal_events:
        import stomp_broker

    def request(method, path, headers=None, body=None, raw_body=None, port=8082):
        connection = http.client.HTTPConnection('127.0.0.1', port, timeout=15)
        try:
            request_headers = {'Request-Id': 'synthetic-paired-read', **(headers or {})}
            if body is not None:
                body = json.dumps(body).encode()
                request_headers['Content-Type'] = 'application/json'
            if raw_body is not None:
                body = raw_body.encode()
                request_headers['Content-Type'] = 'application/json'
            connection.request(method, path, body, request_headers)
            response = connection.getresponse()
            data = response.read()
            result = {'method': method, 'path': path, 'status': response.status,
                      'content_type': response.getheader('Content-Type'), 'body': data.decode()}
            if method == 'POST' and (path in ('/i/users','/register') or path.endswith('/clients')):
                result['location'] = response.getheader('Location')
            try:
                result['json'] = json.loads(data)
            except (ValueError, UnicodeError):
                pass
            return result, response.getheader('Set-Cookie')
        finally:
            connection.close()

    def mls_row_fixture(action, phase, user=None, client=None):
        assert store_fixture_path, 'identified tagged store fixture required'
        output='/reports/mls-rows-'+phase+'.json'
        manifest=Path('/reports/mls-rows-'+phase+'-input.json')
        manifest.write_text(json.dumps({'User':user or lifecycle_user,'Client':client or lifecycle_id,'Action':action,'Rows':mls_rows,'Output':output}))
        env={**os.environ,'BRIG_STORE_CONFIG':str(runtime.WORK/'brig-static-0.json'),'BRIG_MLS_ROWS_INPUT':str(manifest)}
        with open('/reports/mls-rows-'+phase+'.log','w') as log:
            process=subprocess.run([store_fixture_path,'-test.run=^TestOwnedMLSRowFixture$','-test.v','-test.timeout=45s'],env=env,stdout=log,stderr=subprocess.STDOUT,timeout=50)
        assert process.returncode==0, 'owned MLS row fixture failed: '+phase
        rows=json.loads(Path(output).read_text())
        report['mls_rows_'+phase]=rows
        return rows

    def service_row_fixture(action, phase, actor):
        assert store_fixture_path, 'identified service fixture required'
        output = '/reports/service-rows-'+phase+'.json'
        manifest = Path('/reports/service-rows-'+phase+'-input.json')
        manifest.write_text(json.dumps({**actor, 'Action': action, 'Output': output}))
        env = {**os.environ, 'BRIG_STORE_CONFIG': str(runtime.WORK/'brig-static-0.json'),
            'BRIG_SERVICE_ROWS_INPUT': str(manifest)}
        with open('/reports/service-rows-'+phase+'.log', 'w') as log:
            process = subprocess.run([store_fixture_path,
                '-test.run=^TestOwnedServiceDeletionFixture$', '-test.v', '-test.timeout=45s'],
                env=env, stdout=log, stderr=subprocess.STDOUT, timeout=50)
        assert process.returncode == 0, 'owned service row fixture failed: '+phase
        return json.loads(Path(output).read_text())

    def service_actor(phase):
        actor = request('POST', '/i/users', body={'name': 'Synthetic service queue bot'})[0]
        assert actor['status'] == 201, 'service bot actor creation failed'
        uid = actor['json']['id']
        headers = {'Z-User': uid, 'Z-Connection': 'synthetic-service-queue'}
        client = request('POST', '/v19/clients', headers,
            {**native_body, 'label': 'synthetic-service-bot-client'})[0]
        assert client['status'] == 201, 'service bot native-compatible client creation failed'
        prop = request('PUT', '/properties/service-bot-witness', headers, {'synthetic': True})[0]
        assert prop['status'] == 200, 'service bot property creation failed'
        # A missing conversation exercises the native replay-safe Galley404
        # path. This does not establish decoded Galley200 membership events.
        fixture = {'User': uid, **{key: str(uuid.uuid4())
            for key in ('Provider', 'Service', 'Conversation', 'Team')}}
        rows = service_row_fixture('stage', phase+'-stage', fixture)
        assert rows['counts'] == {'service': 1, 'bot_association': 1, 'team_bot_association': 1,
            'whitelist': 1, 'whitelist_rev': 1, 'prefix': 1, 'tags': 7,
            'user': 1, 'clients': 1, 'prekeys': 2, 'mls_public_keys': 1, 'properties': 1}, 'service bot initial raw rows differ'
        assert rows['status'] == 0 and rows['user_type'] == 1 and rows['provider'] == fixture['Provider'] and rows['service'] == fixture['Service'], 'service bot account association differs'
        report['service_queue_'+phase+'_created'] = {'actor': actor, 'client': client,
            'property': prop, 'fixture': fixture, 'rows': rows}
        return fixture

    def enqueue_service(actor):
        before = stomp_broker.queue_state()
        stomp_broker.send(json.dumps({'type': 'service.delete',
            'provider': actor['Provider'], 'service': actor['Service']}).encode())
        return before['DequeueCount']

    def await_service(actor, phase, before=None):
        deadline = time.monotonic()+30
        while True:
            rows = service_row_fixture('read', phase, actor)
            queue = stomp_broker.queue_state()
            expected = {'service': 0, 'bot_association': 0, 'team_bot_association': 0,
                'whitelist': 0, 'whitelist_rev': 0, 'prefix': 0, 'tags': 0,
                'user': 1, 'clients': 0, 'prekeys': 0, 'mls_public_keys': 1, 'properties': 1}
            if rows['counts'] == expected and rows['status'] == 2 and queue['QueueSize'] == 0 and queue['InFlightCount'] == 0 and (before is None or queue['DequeueCount'] > before):
                break
            assert time.monotonic() < deadline, 'service bot cleanup not completed and acknowledged: '+phase
            time.sleep(0.2)
        assert rows['user_type'] == 1 and rows['provider'] == actor['Provider'] and rows['service'] == actor['Service'] and rows['name'] == 'Synthetic service queue bot', 'service deletion lost native bot identity'
        status = request('GET', '/i/users/'+actor['User']+'/status')[0]
        assert status['status'] == 200 and status['json'] == {'status': 'deleted'}, 'service bot HTTP status differs'
        report['service_queue_'+phase] = {'rows': rows, 'queue': queue, 'status': status}
        return rows

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

    def sqs(action, *parameters):
        result = subprocess.run(['aws','sqs',action,'--endpoint-url',ses_endpoint,'--region','eu-west-1',*parameters], capture_output=True, text=True, timeout=30)
        assert result.returncode == 0, 'owned SQS command failed: '+action
        return json.loads(result.stdout) if result.stdout.strip() else {}

    def feedback_observations(implementation):
        actors = []
        for kind in ('Permanent','Transient','Undetermined','Complaint'):
            email = 'feedback-'+uuid.uuid4().hex+'@example.com'
            recipient = {'emailAddress':'Synthetic Recipient <'+email+'>'}
            if kind == 'Complaint':
                body = {'notificationType':'Complaint','complaint':{'complainedRecipients':[recipient]}}
            else:
                body = {'notificationType':'Bounce','bounce':{'bounceType':kind,'bouncedRecipients':[recipient]}}
            sqs('send-message','--queue-url',ses_queue,'--message-body',json.dumps(body))
            actors.append({'kind':kind,'email':email})
        report[implementation+'_feedback_actors'] = actors
        deadline = time.monotonic()+30
        while True:
            attrs=sqs('get-queue-attributes','--queue-url',ses_queue,'--attribute-names','ApproximateNumberOfMessages','ApproximateNumberOfMessagesNotVisible')['Attributes']
            if all(int(attrs[k])==0 for k in ('ApproximateNumberOfMessages','ApproximateNumberOfMessagesNotVisible')):
                break
            assert time.monotonic()<deadline, 'feedback messages not consumed and acknowledged'
            time.sleep(0.5)
        records=[request('GET','/i/users/blacklist?'+urlencode({'email':actor['email']}))[0] for actor in actors]
        report[implementation+'_feedback_records'] = records
        assert [r['status'] for r in records]==[200,404,404,200], 'native feedback blocking policy changed'
        rejected=[]
        for actor in (actors[0],actors[3]):
            result,_=request('POST','/register',{'X-Forwarded-For':'127.0.0.42'}, {'name':'Synthetic feedback rejection','email':actor['email'],'password':'Synthetic-feedback-password!'})
            rejected.append(result)
        report[implementation+'_feedback_register_rejections']=rejected
        assert all(r['status']==403 and r['json']['label']=='blacklisted-email' for r in rejected), 'feedback blocklist admission changed'
        return actors, records, rejected

    def internal_sqs(action, *parameters):
        result = subprocess.run(['aws', 'sqs', action, '--endpoint-url',
            peers['brig']['aws']['sqsEndpoint'], '--region', 'eu-west-1',
            *parameters], capture_output=True, text=True, timeout=30)
        assert result.returncode == 0, 'owned internal SQS command failed: '+action
        return json.loads(result.stdout) if result.stdout.strip() else {}

    def create_replay_actor():
        actor = request('POST', '/i/users', body={
            'name': 'Synthetic internal queue replay', 'expires_in': 3600})[0]
        assert actor['status'] == 201, 'queue replay actor setup failed'
        actor_id = actor['json']['id']
        property_result = request('PUT', '/properties/queue-replay',
            {'Z-User': actor_id, 'Z-Connection': 'synthetic-queue-replay'},
            {'synthetic': True})[0]
        assert property_result['status'] == 200, 'queue replay property setup failed'
        return actor_id

    def enqueue_replay(actor_id):
        if stomp_internal_events:
            stomp_broker.send(json.dumps({'type': 'user.delete', 'user': actor_id}).encode())
            return
        internal_sqs('send-message', '--queue-url', internal_queue_url,
            '--message-body', json.dumps({'type': 'user.delete', 'user': actor_id}))

    def await_replay(actor_id):
        deadline = time.monotonic()+30
        while True:
            status = request('GET', '/i/users/'+actor_id+'/status')[0]
            if stomp_internal_events:
                snapshot = stomp_broker.queue_state()
                required_counts = ('QueueSize', 'InFlightCount')
                attributes = {key: snapshot[key] for key in required_counts}
            else:
                required_counts = ('ApproximateNumberOfMessages', 'ApproximateNumberOfMessagesNotVisible')
                attributes = internal_sqs('get-queue-attributes', '--queue-url',
                    internal_queue_url, '--attribute-names',
                    'ApproximateNumberOfMessages', 'ApproximateNumberOfMessagesNotVisible')['Attributes']
            if status['status'] == 200 and status['json'] == {'status': 'deleted'} and all(
                    int(attributes[key]) == 0 for key in required_counts):
                break
            assert time.monotonic() < deadline, 'queued account cleanup not completed and acknowledged'
            time.sleep(0.2)
        properties = request('GET', '/properties', {'Z-User': actor_id})[0]
        assert properties['status'] == 200 and properties['json'] == [], 'queued cleanup retained properties'
        return {'status': status, 'properties': properties, 'queue_attributes': attributes}

    try:
        servers = runtime.start_forwards()
        runtime.wait_dependencies()
        runtime.stage()
        runtime.initialize()
        peers = json.loads(Path('/fixtures/peers.json').read_text())['0']
        if stomp_internal_events:
            stomp_broker.wait_ready()
            peers['brig']['internalEvents'] = {'queueType': 'stomp', 'queueName': stomp_broker.QUEUE}
            peers['brig']['stomp'] = {'stomHost': 'activemq', 'stomPort': 61613, 'stomTls': False}
            peers['brig']['optSettings']['setStomp'] = '/fixtures/stomp-credentials.json'
            report['internal_transport'] = {'type': 'stomp', 'broker_image': stomp_broker.IMAGE,
                'broker_image_id': stomp_broker.IMAGE_ID, 'synthetic_authentication': True}
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
        extra_ports = set()
        if ses_feedback_contract or properties_contract or client_registration_contract or client_lifecycle_contract:
            # Original compiled tests require both static federator ingress
            # services, even when the selected body only uses a dynamic backend.
            # Match the existing full runtime's native backend-B/NGINX setup.
            second = json.loads(Path('/fixtures/peers.json').read_text())['1']
            for name, config in second.items():
                if name == 'wire-server-enterprise':
                    continue
                path = runtime.WORK / (name+'-static-1.json')
                path.write_text(json.dumps(config))
                args = [str(runtime.WORK/'dist'/name),'-c',str(path)]
                if name == 'background-worker':
                    args += ['--galley-config-file',str(runtime.WORK/'galley-static-1.json')]
                log = open('/reports/'+name+'-native-static-1.log','w');logs.append(log)
                processes.append(subprocess.Popen(args,cwd=runtime.WORK/'services'/name,stdout=log,stderr=subprocess.STDOUT))
                extra_ports.add(config[{'background-worker':'backgroundWorker','wire-server-enterprise':'wireServerEnterprise'}.get(name,name)]['port'])
            for index in range(2):
                conf, cwd = runtime.nginz_config(index)
                log = open('/reports/nginz-'+str(index)+'.log','w');logs.append(log)
                processes.append(subprocess.Popen(['nginx','-c',str(conf),'-g','daemon off;','-e','/dev/stdout'],cwd=cwd,stdout=log,stderr=subprocess.STDOUT))
                extra_ports.update((8098+1000*index,8443+1000*index))
        ready(active)
        pending = {config[{'background-worker': 'backgroundWorker', 'wire-server-enterprise': 'wireServerEnterprise'}.get(name, name)]['port'] for name, config in peers.items()}
        pending.update(extra_ports)
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
            if saml_registration_contract:
                owner, _ = request('POST', '/i/users', body={'name': 'Synthetic SAML team owner',
                    'email': 'synthetic-saml-owner-' + uuid.uuid4().hex + '@example.com',
                    'password': 'Synthetic-team-owner-password!',
                    'team': {'name': 'Synthetic SAML binding team', 'icon': 'default'}})
                report['registration_native_team_setup'] = owner
                assert owner['status'] == 201 and owner['json'].get('team'), 'native SAML binding team setup failed'
                registration_body.update({'managed_by': 'wire', 'team_id': owner['json']['team'],
                    'sso_id': {'tenant': 'https://synthetic-issuer.example.com', 'subject': 'synthetic-' + actor}})
            registration_reference, _ = request('POST', '/i/users', body=registration_body)
            report['registration_native_reference'] = registration_reference
            assert registration_reference['status'] == 201, 'native registration reference failed'
            assert registration_reference['json']['id'] == actor and registration_reference['location'] == actor, 'native registration actor/Location changed'
            registration_cases = [('GET', '/self', {'Z-User': actor}),
                ('GET', '/i/users/' + actor + '/features/conferenceCalling', {}),
                ('GET', '/i/users/' + actor + '/reauthenticate', {}, {'password': registration_password}),
                ('GET', '/i/users/' + actor + '/reauthenticate', {}, {'password': predecessor_password})]
            registration_reads = [request(*case)[0] for case in registration_cases]
            report['registration_native_reads'] = registration_reads
            assert [record['status'] for record in registration_reads] == [200, 200, 200, 403], 'native registration reference reads failed'
            staged, _ = request('POST', '/i/users', body={**registration_body,
                'name': 'Synthetic registration predecessor', 'password': predecessor_password})
            report['registration_native_staged_creation'] = staged
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
        if client_lifecycle_contract:
            import base64
            seed=base64.b64decode('pQABAQcCoQBYIDXdN8VlKb5lbgPmoDPLPyqNIEyShG4oT/DlW0peRRZUA6EAoQBYILLf1TIwSB62q69Ojs/X1tzJ+dYHNAw4QbW/7TC5vSZqBPY=')
            last_key=base64.b64encode(seed[:4]+bytes([0x19,0xff,0xff])+seed[5:]).decode()
            second_key=base64.b64encode(seed[:2]+bytes([2])+seed[3:4]+bytes([0x19,0xff,0xff])+seed[5:]).decode()
            ordinary_key=base64.b64encode(seed).decode()
            password='Synthetic-client-lifecycle-password!'
            actor,_=request('POST','/i/users',body={'name':'Synthetic client lifecycle','password':password})
            report['client_lifecycle_native_actor']=actor
            assert actor['status']==201, 'native client lifecycle actor setup failed'
            lifecycle_user=actor['json']['id'];lifecycle_headers={'Z-User':lifecycle_user,'Z-Connection':'synthetic-client-lifecycle'}
            native_body={'type':'permanent','lastkey':{'id':65535,'key':last_key},'prekeys':[{'id':7,'key':ordinary_key}],'label':'native-durable-client','model':'synthetic-model','class':'desktop','capabilities':['legalhold-implicit-consent'],'mls_public_keys':{'ed25519':'AQI='}}
            first,_=request('POST','/v19/clients',lifecycle_headers,native_body)
            report['client_lifecycle_native_created']=first
            assert first['status']==201, 'native client creation failed'
            lifecycle_id=first['json']['id'];lifecycle_path='/v19/clients/'+lifecycle_id
            claim_path='/v19/users/'+peers['brig']['optSettings']['setFederationDomain']+'/'+lifecycle_user+'/prekeys/'+lifecycle_id
            bundle_path='/v19/users/'+peers['brig']['optSettings']['setFederationDomain']+'/'+lifecycle_user+'/prekeys'
            claim_cases=[('GET',claim_path,lifecycle_headers),('GET',claim_path,lifecycle_headers),('GET',claim_path,lifecycle_headers),('GET',bundle_path,lifecycle_headers),('GET',bundle_path+'/f',lifecycle_headers)]
            native_claim_controls=[request(*case)[0] for case in claim_cases]
            report['prekey_native_claim_controls']=native_claim_controls
            assert [r['status'] for r in native_claim_controls]==[200,200,200,200,404], 'native claim reference changed'
            assert [r['json']['prekey']['id'] for r in native_claim_controls[:3]]==[7,65535,65535], 'native ordinary/last resort selection changed'
            local_domain=peers['brig']['optSettings']['setFederationDomain']
            # Native APIs create actual local/remote rows and conversations.
            connection_headers={'Z-User':user,'Z-Connection':'synthetic-connection-paging'}
            connection_targets=[]
            connection_created=[]
            for target_port,target_domain in [(8082,local_domain)]*3+[(9082,'b.example.com')]:
                target,_=request('POST','/i/users',body={'name':'Synthetic connection target','email':'synthetic-connection-'+uuid.uuid4().hex+'@example.com','password':password},port=target_port)
                assert target['status']==201, 'native connection target setup failed'
                target_id=target['json']['id']
                connected,_=request('POST','/v19/connections/'+target_domain+'/'+target_id,connection_headers)
                connection_created.append(connected)
                assert connected['status']==201, 'native connection creation failed'
                connection_targets.append((target_domain,target_id))
            report['connections_native_created']=connection_created
            connection_cases=[('GET','/v19/connections/'+d+'/'+u,connection_headers) for d,u in connection_targets]
            connection_cases += [('GET','/v1/connections/'+connection_targets[0][1],connection_headers),('GET','/v19/connections/'+local_domain+'/'+str(uuid.uuid4()),connection_headers)]
            connection_cases += [('GET','/v1/connections'+q,connection_headers) for q in ['', '?size=1','?size=2','?size=1&start='+connection_targets[0][1]]]
            connection_cases += [('POST','/v19/list-connections',connection_headers,{})]
            native_connection_controls=[request(*case)[0] for case in connection_cases]
            report['connections_native_controls']=native_connection_controls
            assert [r['status'] for r in native_connection_controls]==[200]*5+[404]+[200]*5, 'native connection read controls changed'
            connection_page_cases=[]
            native_connection_pages=[]
            connection_cursor=None
            for page_number in range(8):
                body={'size':1}
                if connection_cursor is not None: body['paging_state']=connection_cursor
                case=('POST','/v19/list-connections',connection_headers,body)
                page,_=request(*case)
                connection_page_cases.append(case);native_connection_pages.append(page)
                assert page['status']==200, 'native connection page failed'
                if not page['json']['has_more']: break
                connection_cursor=page['json']['paging_state']
            else: raise AssertionError('native connection pagination failed to terminate')
            report['connections_native_pages']=native_connection_pages
            # Separate actors prevent write proof from changing read fixtures.
            connection_write_actors=[]
            for _ in range(2):
                actor,_=request('POST','/i/users',body={'name':'Synthetic local connection write','email':'synthetic-local-connect-'+uuid.uuid4().hex+'@example.com','password':password})
                assert actor['status']==201, 'native local connection write actor setup failed'
                connection_write_actors.append(actor['json']['id'])
            connection_writer,connection_recipient=connection_write_actors
            connection_write_headers={'Z-User':connection_writer,'Z-Connection':'synthetic-local-create'}
            connection_create_path='/v19/connections/'+local_domain+'/'+connection_recipient
            connection_create_cases=[('POST','/v19/connections/'+local_domain+'/'+connection_writer,connection_write_headers),('POST','/v19/connections/'+local_domain+'/'+str(uuid.uuid4()),connection_write_headers),('POST',connection_create_path,lifecycle_headers)]
            native_connection_create_controls=[request(*case)[0] for case in connection_create_cases]
            report['connections_native_create_controls']=native_connection_create_controls
            profile_actor=request('POST','/i/users',body={'name':'Synthetic identityless profile','expires_in':3600})[0]
            assert profile_actor['status']==201, 'native identityless profile actor setup failed'
            profile_user=profile_actor['json']['id']
            profile_headers={'Wire-Origin-Domain':'b.example.com','X-Wire-API-Version':'3'}
            profile_cases=[('POST','/federation/get-users-by-ids',profile_headers,ids) for ids in [[],[connection_writer],[profile_user],[str(uuid.uuid4())],[connection_writer,profile_user,connection_recipient,connection_writer]]]
            native_profile_controls=[request(*case)[0] for case in profile_cases]
            report['profiles_native_controls']=native_profile_controls
            assert all(p['status']==200 for p in native_profile_controls), 'native federation profile controls failed'
            update_control_path='/v19/connections/'+local_domain+'/'+connection_targets[0][1]
            connection_update_cases=[('PUT',update_control_path,connection_headers,{'status':'pending'}),('PUT',update_control_path,connection_headers,{'status':'sent'}),('PUT','/v19/connections/'+local_domain+'/'+str(uuid.uuid4()),connection_headers,{'status':'accepted'}),('PUT',update_control_path,connection_headers,{'status':'missing-legalhold-consent'})]
            native_connection_update_controls=[request(*case)[0] for case in connection_update_cases]
            report['connections_native_update_controls']=native_connection_update_controls
            assert [r['status'] for r in native_connection_update_controls]==[403,204,403,403], 'native local update controls changed'
            assert [r['status'] for r in native_connection_create_controls]==[400,400,403], 'native local connection admission changed'
            assert sum(len(p['json']['connections']) for p in native_connection_pages)==4, 'native paged connection rows lost or repeated'
            local_multi={lifecycle_user:[lifecycle_id,'f',lifecycle_id]}
            oversized={str(uuid.uuid4()):[] for _ in range(peers['brig']['optSettings']['setMaxConvSize']+1)}
            multi_cases=[('POST','/v1/users/prekeys',lifecycle_headers,local_multi),('POST','/v3/users/list-prekeys',lifecycle_headers,{local_domain:local_multi}),('POST','/v19/users/list-prekeys',lifecycle_headers,{local_domain:local_multi}),('POST','/v19/users/list-prekeys',lifecycle_headers,{}),('POST','/v19/users/list-prekeys',lifecycle_headers,{local_domain:oversized})]
            native_multi_controls=[request(*case)[0] for case in multi_cases]
            report['prekey_native_multi_controls']=native_multi_controls
            assert [r['status'] for r in native_multi_controls]==[200,200,200,200,403], 'native multi-user controls changed'
            # Own supplemental observer: native B persists a last-resort-only
            # client so repeated native/Go outgoing calls compare identical state.
            remote_actor,_=request('POST','/i/users',body={'name':'Synthetic federation prekey actor','password':password},port=9082)
            report['prekey_remote_native_actor']=remote_actor
            assert remote_actor['status']==201, 'native remote actor setup failed'
            remote_user=remote_actor['json']['id']
            remote_headers={'Z-User':remote_user,'Z-Connection':'synthetic-federation-prekey'}
            remote_client,_=request('POST','/v19/clients',remote_headers,{**native_body,'prekeys':[]},port=9082)
            report['prekey_remote_native_client']=remote_client
            assert remote_client['status']==201, 'native remote client setup failed'
            remote_id=remote_client['json']['id']
            remote_domain=second['brig']['optSettings']['setFederationDomain']
            remote_path='/v19/users/'+remote_domain+'/'+remote_user+'/prekeys'
            mixed={local_domain:{lifecycle_user:[lifecycle_id,'f']},remote_domain:{remote_user:[remote_id,'f']}}
            federation_cases=[
                ('GET',remote_path+'/'+remote_id,lifecycle_headers),
                ('GET',remote_path,lifecycle_headers),
                ('GET',remote_path+'/f',lifecycle_headers),
                ('POST','/v3/users/list-prekeys',lifecycle_headers,mixed),
                ('POST','/v19/users/list-prekeys',lifecycle_headers,mixed),
                ('POST','/federation/claim-prekey',{'Wire-Origin-Domain':remote_domain},[lifecycle_user,lifecycle_id]),
                ('POST','/federation/claim-prekey',{'Wire-Origin-Domain':remote_domain},[lifecycle_user,'f']),
                ('POST','/federation/claim-prekey-bundle',{'Wire-Origin-Domain':remote_domain},lifecycle_user),
                ('POST','/federation/claim-multi-prekey-bundle',{'Wire-Origin-Domain':remote_domain},{lifecycle_user:[lifecycle_id,'f']})]
            missing_domain='unavailable-prekey.invalid'
            missing_path='/v19/users/'+missing_domain+'/'+remote_user+'/prekeys'
            failure_mixed={**mixed,missing_domain:{remote_user:[remote_id]}}
            federation_cases.extend([
                ('GET',missing_path+'/'+remote_id,lifecycle_headers),
                ('GET',missing_path,lifecycle_headers),
                ('POST','/v3/users/list-prekeys',lifecycle_headers,failure_mixed),
                ('POST','/v19/users/list-prekeys',lifecycle_headers,failure_mixed)])
            native_federation_controls=[request(*case)[0] for case in federation_cases]
            report['prekey_native_federation_controls']=native_federation_controls
            assert [r['status'] for r in native_federation_controls[:9]]==[200,200,404,200,200,200,200,200,200], 'native federation reference changed'
            assert all(r['status']>=400 and r['json']['label'] for r in native_federation_controls[9:12]), 'native federation missing-domain failures changed'
            assert native_federation_controls[12]['status']==200, 'native federation partial request failed'
            assert native_federation_controls[12]['json']['failed_to_list']==[{'domain':missing_domain,'id':remote_user}], 'native federation qualified failure changed'
            assert set(native_federation_controls[12]['json']['qualified_user_client_prekeys'])==set(mixed), 'native federation partial successes changed'
            public_local='/users/'+local_domain+'/'+lifecycle_user+'/clients'
            public_remote='/users/'+remote_domain+'/'+remote_user+'/clients'
            public_users=[{'domain':local_domain,'id':lifecycle_user},{'domain':remote_domain,'id':remote_user},{'domain':missing_domain,'id':remote_user}]
            public_client_cases=[
                ('GET','/v1/users/'+lifecycle_user+'/clients',{}),
                ('GET','/v15'+public_local,{}),
                ('GET','/v16'+public_local,{}),
                ('GET','/v19'+public_local+'/'+lifecycle_id,{}),
                ('GET','/v19'+public_local+'/f',{}),
                ('GET','/v15'+public_remote,{}),
                ('GET','/v19'+public_remote+'/'+remote_id,{}),
                ('GET','/v15/users/'+missing_domain+'/'+remote_user+'/clients',{}),
                ('GET','/v19/users/'+missing_domain+'/'+remote_user+'/clients/'+remote_id,{}),
                ('POST','/v1/users/list-clients',lifecycle_headers,public_users),
                ('POST','/v1/users/list-clients/v2',lifecycle_headers,{'qualified_users':public_users}),
                ('POST','/v19/users/list-clients',lifecycle_headers,{'qualified_users':public_users}),
                ('POST','/federation/get-user-clients',{'Wire-Origin-Domain':remote_domain},{'users':[lifecycle_user]}),
                ('POST','/federation/get-user-clients',{'Wire-Origin-Domain':remote_domain},{'users':[]})]
            native_public_client_controls=[request(*case)[0] for case in public_client_cases]
            report['public_client_native_controls']=native_public_client_controls
            assert [r['status'] for r in native_public_client_controls]==[200,200,404,200,404,200,200,200,404,200,200,200,200,200], 'native public client reference changed'
            assert native_public_client_controls[7]['json']==[], 'native remote failure did not become empty client list'
            assert set(native_public_client_controls[11]['json']['qualified_user_map'])=={local_domain,remote_domain}, 'native public client bulk remote omission changed'
            mls_cases=[
                ('GET','/i/mls/clients/'+lifecycle_user+'?ciphersuite=0x1',{}),
                ('GET','/i/mls/client/'+lifecycle_user+'/'+lifecycle_id+'?ciphersuite=0x1',{}),
                ('GET','/i/mls/client/'+lifecycle_user+'/f?ciphersuite=0x1',{}),
                ('GET','/i/mls/client/'+lifecycle_user+'/'+lifecycle_id+'?ciphersuite=0x2',{}),
                ('GET','/i/mls/clients/'+lifecycle_user+'?ciphersuite=0x3',{}),
                ('POST','/federation/get-mls-clients',{'Wire-Origin-Domain':remote_domain,'X-Wire-API-Version':'0'},{'user_id':lifecycle_user,'signature_scheme':'ecdsa_secp384r1_sha384'}),
                ('POST','/federation/get-mls-clients',{'Wire-Origin-Domain':remote_domain,'X-Wire-API-Version':'1'},{'user_id':lifecycle_user,'cipher_suite':1}),
                ('POST','/federation/get-mls-client',{'Wire-Origin-Domain':remote_domain,'X-Wire-API-Version':'3'},{'user_id':lifecycle_user,'client_id':lifecycle_id,'cipher_suite':1}),
                ('POST','/federation/get-mls-client',{'Wire-Origin-Domain':remote_domain,'X-Wire-API-Version':'3'},{'user_id':lifecycle_user,'client_id':'f','cipher_suite':1}),
                ('POST','/federation/get-mls-client',{'Wire-Origin-Domain':remote_domain,'X-Wire-API-Version':'3'},{'user_id':lifecycle_user,'client_id':lifecycle_id,'cipher_suite':3})]
            count_path='/mls/key-packages/self/'+lifecycle_id+'/count'
            count_cases=[('GET','/v4'+count_path+'?ciphersuite=0x1',lifecycle_headers),('GET','/v7'+count_path,lifecycle_headers),('GET','/v19'+count_path+'?ciphersuite=0x1',lifecycle_headers),('GET','/v19'+count_path+'?ciphersuite=0x3',lifecycle_headers),('GET','/v19/mls/key-packages/self/f/count?ciphersuite=0x1',lifecycle_headers)]
            if store_fixture_path:
                import struct
                anchor=int(time.time())
                def package(before,after,source=1):
                    value=bytes([0,1,0,1,0,0,0,0,1,0,0,0,0,0,0,source])
                    if source==1:value+=struct.pack('>QQ',before,after)
                    return (value+bytes(4)).hex()
                mls_rows=[{'Ref':'01','Data':package(0,anchor+3600)},{'Ref':'02','Data':package(0,1)},{'Ref':'03','Data':package(anchor+3600,anchor+7200)},{'Ref':'04','Data':package(0,0,2)},{'Ref':'05','Data':'00'}]
                staged=mls_row_fixture('stage','native-staged')
                assert staged==[{'ref':row['Ref'],'data':row['Data']} for row in mls_rows], 'native MLS stage bytes changed'
                row_count_case=('GET','/v19'+count_path+'?ciphersuite=0x1',lifecycle_headers)
                row_count_native=request(*row_count_case)[0];report['mls_rows_native_count']=row_count_native
                assert row_count_native['status']==200 and row_count_native['json']=={'count':1}, 'native usable/malformed count policy changed'
                native_rows_retained=mls_row_fixture('read','native-retained')
                assert native_rows_retained==[{'ref':row['Ref'],'data':row['Data']} for row in (mls_rows[0],mls_rows[4])], 'native MLS cleanup retention policy changed'
            if mls_upload_originals and store_fixture_path:
                signed_actor,_=request('POST','/i/users',body={'name':'Synthetic signed MLS switchback','password':password})
                assert signed_actor['status']==201, 'native signed actor setup failed'
                signed_user=signed_actor['json']['id'];signed_headers={'Z-User':signed_user,'Z-Connection':'synthetic-mls-switchback'}
                signed_client_body={**native_body,'mls_public_keys':{}}
                signed_created,_=request('POST','/v19/clients',signed_headers,signed_client_body)
                assert signed_created['status']==201, 'native signed client setup failed'
                signed_client=signed_created['json']['id']
                manifest=Path('/reports/mls-signed-material-input.json');material_path=Path('/reports/mls-signed-material.json')
                manifest.write_text(json.dumps({'User':signed_user,'Client':signed_client,'Output':str(material_path)}))
                env={**os.environ,'BRIG_STORE_CONFIG':str(runtime.WORK/'brig-static-0.json'),'BRIG_MLS_ROWS_INPUT':str(manifest)}
                with open('/reports/mls-signed-material.log','w') as log:
                    generated=subprocess.run([store_fixture_path,'-test.run=^TestOwnedMLSSignMaterial$','-test.v','-test.timeout=45s'],env=env,stdout=log,stderr=subprocess.STDOUT,timeout=50)
                assert generated.returncode==0, 'independent synthetic signed material failed'
                material=json.loads(material_path.read_text())
                signed_key,_=request('PUT','/v19/clients/'+signed_client,signed_headers,{'mls_public_keys':{'ed25519':material['public_key']}})
                assert signed_key['status']==200, 'native signed client key setup failed'
                signed_path='/v19/mls/key-packages/self/'+signed_client
                signed_count_path=signed_path+'/count?ciphersuite=1'
                signed_claim_path='/v19/mls/key-packages/claim/'+local_domain+'/'+signed_user+'?ciphersuite=1'
                signed_valid={'key_packages':[material['valid']]};signed_invalid={'key_packages':[material['invalid_protocol']]}
                signed_native_upload,_=request('POST',signed_path,signed_headers,signed_valid)
                assert signed_native_upload['status']==201, 'native signed upload failed'
                signed_native_rows=mls_row_fixture('read','signed-native-upload',signed_user,signed_client)
                assert len(signed_native_rows)==1 and signed_native_rows[0]['data']==base64.b64decode(material['valid']).hex(), 'native signed upload bytes differ'
                signed_native_rejection,_=request('PUT',signed_path+'?ciphersuites=1',signed_headers,signed_invalid)
                report['mls_signed_native_rejected_replace']=signed_native_rejection
                assert signed_native_rejection['status']==400 and signed_native_rejection['json']['label']=='mls-protocol-error' and signed_native_rejection['json']['message']=='Unsupported protocol version', 'native replacement validation priority changed'
                assert mls_row_fixture('read','signed-native-rejected-replace',signed_user,signed_client)==[], 'native rejected replacement did not delete prior state'
                restored,_=request('POST',signed_path,signed_headers,signed_valid)
                assert restored==signed_native_upload, 'native signed restoration response differs'
                assert mls_row_fixture('read','signed-native-restored',signed_user,signed_client)==signed_native_rows, 'native signed restoration bytes differ'
            native_count_controls=[request(*case)[0] for case in count_cases]
            report['mls_count_native_controls']=native_count_controls
            assert [r['status'] for r in native_count_controls]==[404,200,200,400,200], 'native public MLS count reference changed'
            assert native_count_controls[1]['json']=={'count':1 if store_fixture_path else 0}, 'native MLS count reference changed'
            native_mls_controls=[request(*case)[0] for case in mls_cases]
            report['mls_client_native_controls']=native_mls_controls
            assert [r['status'] for r in native_mls_controls]==[200,200,200,200,400,200,200,200,200,400], 'native MLS client reference changed'
            assert native_mls_controls[1]['json']['mls_signature_key']=='AQI=' and native_mls_controls[1]['json']['mls'] is bool(store_fixture_path), 'native MLS signature/count reference changed'
            assert native_mls_controls[2]['json']=={'id':'f','mls':False}, 'native missing MLS client reference changed'
            if store_fixture_path:
                delete_path='/mls/key-packages/self/'+lifecycle_id
                delete_body={'key_packages':['AQ==','AQ==','BQ==','/w==']}
                mls_delete_cases=[('DELETE','/v4'+delete_path+'?ciphersuite=1',lifecycle_headers,delete_body),('DELETE','/v7'+delete_path,lifecycle_headers,delete_body),('DELETE','/v19'+delete_path+'?ciphersuite=1',lifecycle_headers,delete_body),('DELETE','/v19/mls/key-packages/self/f?ciphersuite=1',lifecycle_headers,delete_body)]
                native_delete_controls=[request(*case)[0] for case in mls_delete_cases]
                report['mls_delete_native_controls']=native_delete_controls
                assert [r['status'] for r in native_delete_controls]==[405,201,201,201], 'native public MLS deletion reference changed'
                assert mls_row_fixture('read','native-deleted')==[], 'native reference deletion did not remove exact rows'
                mls_row_fixture('stage','native-delete-restaged')
                assert row_count_native==request(*row_count_case)[0], 'native delete restage count changed'
                assert mls_row_fixture('read','native-delete-restored')==native_rows_retained, 'native delete restage bytes changed'

            restaged,_=request('POST','/v19/clients',lifecycle_headers,{**native_body,'mls_public_keys':{}})
            report['prekey_native_restage']=restaged
            assert restaged['status']==201, 'native prekey reference restage failed'
            native_lifecycle_state=[request('GET',lifecycle_path,lifecycle_headers)[0],request('GET',lifecycle_path+'/prekeys',lifecycle_headers)[0]]
            report['client_lifecycle_native_state']=native_lifecycle_state
            new_body={'type':'permanent','lastkey':{'id':65535,'key':second_key},'prekeys':[{'id':7,'key':ordinary_key}],'label':'go-durable-client','class':'phone','capabilities':['legalhold-implicit-consent'],'mls_public_keys':{'ed25519':'AQID'}}
            absent_headers={**lifecycle_headers,'Z-User':str(uuid.uuid4())}
            lifecycle_cases=[
                ('POST','/v19/clients',absent_headers,{**new_body,'type':'legalhold'}),
                ('POST','/v19/clients',absent_headers,new_body),
                ('POST','/v19/clients',lifecycle_headers,new_body),
                ('POST','/v19/clients',lifecycle_headers,{**new_body,'password':'Synthetic-wrong-password!'}),
                ('DELETE',lifecycle_path,lifecycle_headers,{}),
                ('DELETE',lifecycle_path,lifecycle_headers,{'password':'Synthetic-wrong-password!'}),
                ('DELETE','/v19/clients/f',lifecycle_headers,{})]
            native_lifecycle_controls=[request(*case)[0] for case in lifecycle_cases]
            report['client_lifecycle_native_controls']=native_lifecycle_controls
            assert [r['status'] for r in native_lifecycle_controls]==[400,400,403,403,403,403,404], 'native lifecycle controls changed'
        if client_update_contract:
            import base64
            seed=base64.b64decode('pQABAQcCoQBYIDXdN8VlKb5lbgPmoDPLPyqNIEyShG4oT/DlW0peRRZUA6EAoQBYILLf1TIwSB62q69Ojs/X1tzJ+dYHNAw4QbW/7TC5vSZqBPY=')
            assert seed[:5]==bytes([0xa5,0,1,1,7]), 'synthetic native prekey fixture changed'
            last_key=base64.b64encode(seed[:4]+bytes([0x19,0xff,0xff])+seed[5:]).decode()
            replacement_key=base64.b64encode(seed[:2]+bytes([2])+seed[3:]).decode()
            report['client_update_native_setup']=[]
            def client_actor(name):
                actor,_=request('POST','/i/users',body={'name':'Synthetic client update '+name})
                assert actor['status']==201, 'native client update actor setup failed'
                uid=actor['json']['id'];headers={'Z-User':uid,'Z-Connection':'synthetic-update-device'}
                client,_=request('POST','/v19/clients',headers,{'type':'permanent','lastkey':{'id':65535,'key':last_key},'prekeys':[],'capabilities':['legalhold-implicit-consent'],'mls_public_keys':{'ed25519':'AQI='},'label':'synthetic-initial'})
                report['client_update_native_setup'].append({'actor':actor,'client':client})
                assert client['status']==201, 'native client fixture creation failed'
                return uid,client['json']['id'],headers
            update_user,update_id,update_headers=client_actor('shared state')
            reference_user,reference_id,reference_headers=client_actor('positive reference')
            assert update_id==reference_id, 'native prekey-text client ID derivation changed'
            update_path='/v19/clients/'+update_id
            update_cases=[
                ('PUT','/v19/clients/f',update_headers,{}),
                ('PUT',update_path,update_headers,{'label':'partial-before-prekey','prekeys':[{'id':7,'key':'invalid'}]}),
                ('GET',update_path,update_headers),
                ('PUT',update_path,update_headers,{'label':'partial-before-removal','capabilities':[]}),
                ('GET',update_path,update_headers),
                ('PUT',update_path,update_headers,{'label':'updated-label','capabilities':['legalhold-implicit-consent']}),
                ('PUT',update_path,update_headers,{'label':'partial-before-duplicate','mls_public_keys':{'ed25519':'AQI='}}),
                ('GET',update_path,update_headers),
            ]
            native_updates=[request(*case)[0] for case in update_cases]
            report['native_client_update_controls']=native_updates
            assert [r['status'] for r in native_updates]==[404,400,200,409,200,200,400,200], 'native client update contract changed'
            positive_update={'label':'go-owned-client','capabilities':['legalhold-implicit-consent','consumable-notifications'],'prekeys':[{'id':7,'key':replacement_key}],'mls_public_keys':{'ecdsa_secp256r1_sha256':'AQID'}}
            native_positive,_=request('PUT','/v19/clients/'+reference_id,reference_headers,positive_update)
            report['native_client_update_positive']=native_positive
            assert native_positive['status']==200, 'native positive client update failed'
            native_claim,_=request('GET','/v19/users/'+peers['brig']['optSettings']['setFederationDomain']+'/'+reference_user+'/prekeys/'+reference_id,reference_headers)
            report['native_client_update_claim_reference']=native_claim
            assert native_claim['status']==200 and native_claim['json']['prekey']=={'id':7,'key':replacement_key}, 'native updated prekey claim failed'
            report['client_update_consumable_config']=peers['brig']['optSettings']['setConsumableNotifications']
        if properties_contract:
            actor,_=request('POST','/i/users',body={'name':'Synthetic property state'})
            report['property_actor_setup']=actor
            assert actor['status']==201, 'native property account setup failed'
            property_user=actor['json']['id']
            property_headers={'Z-User':property_user,'Z-Connection':'synthetic-property-connection'}
            raw_property=' \n{"x":1,"x":2,"large":9007199254740993}\t'
            property_settings=peers['brig']['optSettings']
            property_max_key=property_settings.get('setPropertyMaxKeyLen',1024)
            property_max_value=property_settings.get('setPropertyMaxValueLen',524288)
            property_cases=[
                ('GET','/properties/missing',property_headers),
                ('PUT','/properties/raw',property_headers,None,raw_property),
                ('GET','/properties/raw',property_headers),
                ('HEAD','/properties/raw',property_headers),
                ('GET','/properties-values',property_headers),
                ('GET','/properties',property_headers),
                ('PUT','/properties/d%C3%B6ner',property_headers,True),
                ('PUT','/properties/missing-connection',{'Z-User':property_user},True),
                ('PUT','/properties/a%2Fb',property_headers,True),
                ('GET','/properties/a%2Fb',property_headers),
                ('PUT','/properties/bad',property_headers,None,'{'),
                ('PUT','/properties/'+('k'*(property_max_key+1)),property_headers,True),
                ('PUT','/properties/too-large',property_headers,None,' '*property_max_value+'0'),
                ('DELETE','/properties/raw',property_headers),
                ('GET','/properties/raw',property_headers),
                ('DELETE','/properties',property_headers),
            ]
            native_properties=[request(*case)[0] for case in property_cases]
            report['native_properties']=native_properties
            assert [r['status'] for r in native_properties]==[404,200,200,200,200,200,400,400,200,200,400,403,403,200,404,200], 'native property contract changed'
            assert native_properties[2]['body']==raw_property, 'native raw bytes changed'
            staged,_=request('PUT','/properties/native-durable',property_headers,None,raw_property)
            assert staged['status']==200, 'native durable property setup failed'
            native_property_state=[request('GET','/properties/native-durable',property_headers)[0],request('GET','/properties-values',property_headers)[0]]
            report['native_property_state']=native_property_state
        if ses_feedback_contract:
            native_federation,_=request('GET','/i/federation/remotes')
            report['native_federation_config']=native_federation
            assert native_federation['status']==200, 'native federation configuration read failed'
            federation_version_cases=[('POST','/federation/api-version',{'Wire-Origin-Domain':'example2.com'},{}),('POST','/federation/api-version',{},{}),('POST','/federation/api-version',{'Wire-Origin-Domain':'example2.com'},[])]
            native_federation_versions=[request(*case)[0] for case in federation_version_cases]
            report['native_federation_versions']=native_federation_versions
            assert [r['status'] for r in native_federation_versions]==[200,400,200], 'native federation version input changed'
            ses_endpoint=peers['brig']['aws']['sqsEndpoint']
            queue_name=peers['brig']['emailSMS']['email']['sesQueue']
            ses_queue=sqs('get-queue-url','--queue-name',queue_name)['QueueUrl']
            report['feedback_queue_name']=queue_name
            admin_email='feedback-api-'+uuid.uuid4().hex+'@example.com'
            admin_path='/i/users/blacklist?'+urlencode({'email':admin_email})
            admin_cases=[('GET',admin_path),('POST',admin_path),('HEAD',admin_path),('GET',admin_path),('DELETE',admin_path),('GET',admin_path),('GET','/i/users/blacklist'),('GET','/i/users/blacklist?email=invalid')]
            native_admin=[request(*case)[0] for case in admin_cases]
            report['native_blacklist_api']=native_admin
            assert [r['status'] for r in native_admin]==[404,200,200,200,200,404,400,400], 'native blacklist routes changed'
            native_feedback_actors,native_feedback,native_rejections=feedback_observations('native')
        if password_change_contract:
            change_old = 'Synthetic-change-old-password!'
            change_new = 'Synthetic-change-new-password!'
            change_actors = {}
            report['native_change_actor_setup'] = []
            for name in ('same','wrong','missing','main','first','first-reference','inactive'):
                email = 'change-'+name+'-'+uuid.uuid4().hex+'@example.com'
                body = {'name':'Synthetic password change '+name,'email':email}
                if name not in ('first','first-reference','inactive'):
                    body['password'] = change_old
                if name=='inactive':
                    body={'name':'Synthetic inactive change','managed_by':'scim','uuid':str(uuid.uuid4())}
                actor,_=request('POST','/i/users',body=body)
                report['native_change_actor_setup'].append({'actor':name,'response':actor})
                assert actor['status']==201, 'native password change actor setup failed: '+name
                change_actors[name]={'user':actor['json']['id'],'email':email}
            report['native_change_actors']=change_actors
            main_change_login, change_cookie=request('POST','/login?persist=true',body={'email':change_actors['main']['email'],'password':change_old,'label':'synthetic-change-original'})
            assert main_change_login['status']==200 and change_cookie, 'native password change cookie setup failed'
            change_cases=[
                ('HEAD','/self/password',{'Z-User':change_actors['main']['user']}),
                ('HEAD','/self/password',{'Z-User':change_actors['first']['user']}),
                ('HEAD','/self/password',{'Z-User':str(uuid.uuid4())}),
                ('PUT','/self/password',{'Z-User':change_actors['inactive']['user']},{'new_password':change_new}),
                ('PUT','/self/password',{'Z-User':change_actors['missing']['user']},{'new_password':change_new}),
                ('PUT','/self/password',{'Z-User':change_actors['wrong']['user']},{'old_password':'Synthetic-wrong-password!','new_password':change_new}),
                ('PUT','/self/password',{'Z-User':change_actors['same']['user']},{'old_password':change_old,'new_password':change_old})]
            native_change_records=[request(*case)[0] for case in change_cases]
            report['native_change_controls']=native_change_records
            assert [r['status'] for r in native_change_records]==[200,404,404,403,403,403,409], 'native password change controls changed'
            native_first,_=request('PUT','/self/password',{'Z-User':change_actors['first-reference']['user']},{'new_password':change_new})
            report['native_first_password']=native_first
            assert native_first['status']==200, 'native first password reference failed'
        reset_cases = []
        native_reset_records = []
        if password_reset_contract:
            reset_email = 'synthetic-reset-'+uuid.uuid4().hex+'@example.com'
            reset_account, _ = request('POST','/i/users',body={'name':'Synthetic reset switchback','email':reset_email,'password':'Synthetic-password-2026!'})
            report['native_reset_account'] = reset_account
            assert reset_account['status']==201, 'native reset account setup failed'
            reset_user = reset_account['json']['id']
            reset_login, reset_cookie = request('POST','/login?persist=true',body={'email':reset_email,'password':'Synthetic-password-2026!','label':'synthetic-reset-original-device'})
            report['native_reset_login'] = {'status':reset_login['status'],'cookie_issued':bool(reset_cookie)}
            assert reset_login['status']==200 and reset_cookie, 'native reset session setup failed'
            # A separate synthetic actor keeps original read/reauth controls
            # and this finite reset flow within unchanged native rate budgets.
            reset_password = 'Synthetic-reset-go-password-2026!'
            native_reset_begin, _ = request('POST','/password-reset',body={'email':reset_email})
            report['native_reset_begin'] = native_reset_begin
            assert native_reset_begin['status']==201, 'native reset initiation failed'
            native_reset_pair, _ = request('GET','/i/users/password-reset-code?'+urlencode({'email':reset_email}))
            report['native_reset_pair'] = native_reset_pair
            assert native_reset_pair['status']==200 and set(native_reset_pair['json'])=={'key','code'}, 'native reset pair missing'
            reset_pair = native_reset_pair['json']
            reset_cases = [
                ('POST','/password-reset',{}, {'email':reset_email}),
                ('POST','/password-reset',{}, {'email':'synthetic-reset-absent-'+uuid.uuid4().hex+'@example.com'}),
                ('POST','/password-reset',{}, {'phone':'synthetic unsupported phone'}),
                ('POST','/password-reset/complete',{}, {'key':'unknown-synthetic-key','code':reset_pair['code'],'password':reset_password}),
                ('POST','/password-reset/complete',{}, {'key':reset_pair['key'],'code':'wrong-synthetic-code','password':reset_password}),
                ('POST','/password-reset/complete',{}, {**reset_pair,'password':'Synthetic-password-2026!'}),
                ('POST','/password-reset/complete',{}, {'email':'synthetic-reset-absent@example.com','code':reset_pair['code'],'password':reset_password}),
                ('POST','/password-reset/complete',{}, {'phone':'+12345678','code':reset_pair['code'],'password':reset_password})]
            native_reset_records = [request(*case)[0] for case in reset_cases]
            report['native_reset_controls'] = native_reset_records
            assert [r['status'] for r in native_reset_records]==[201,201,400,400,400,409,400,400], 'native reset control contract changed'
        public_cases = []
        native_public_errors = []
        if public_registration_contract:
            public_cases = [
                ('POST','/register',{}, {'name':'Synthetic public header control'}),
                ('POST','/register',{'X-Forwarded-For':'invalid-synthetic-ip'}, {'name':'Synthetic public header control'}),
                ('POST','/register',{'X-Forwarded-For':'127.0.0.1,127.0.0.2'}, {'name':'Synthetic public header control'}),
                ('POST','/register',{'X-Forwarded-For':'fe80::1%lo'}, {'name':'Synthetic public header control'})]
            native_public_errors = [request(*case)[0] for case in public_cases]
            assert all(record['status'] == 400 for record in native_public_errors), 'native public header controls unexpectedly created accounts'
            report['native_public_header_controls'] = native_public_errors
            native_public, native_public_cookie = request('POST','/register',{'X-Forwarded-For':'127.0.0.42'},
                {'name':'Synthetic native public control','email':'synthetic-native-public-'+uuid.uuid4().hex+'@example.com','password':'Synthetic-public-password-2026!'})
            report['native_public_creation'] = native_public
            assert native_public['status'] == 201 and native_public['location'] == native_public['json']['id'] and native_public_cookie, 'native public registration baseline failed'
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
        if client_lifecycle_contract:
            if not stomp_internal_events:
                internal_queue_url = internal_sqs('get-queue-url', '--queue-name',
                    peers['brig']['internalEvents']['queueName'])['QueueUrl']
            native_replay_actor = create_replay_actor()
            enqueue_replay(native_replay_actor)
            report['internal_queue_native_replay'] = await_replay(native_replay_actor)
            pending_replay_actor = create_replay_actor()
            if service_deletion_queue:
                native_service_actor = service_actor('native')
                native_service_rows = await_service(native_service_actor, 'native-cleanup', enqueue_service(native_service_actor))
                pending_service_actor = service_actor('native-pending')
            if stomp_internal_events:
                report['stomp_native_failure_redelivery'] = stomp_broker.failure_probe()
        active.terminate()
        report['native_stop_exit_code'] = active.wait(timeout=35)
        assert report['native_stop_exit_code'] == 0, 'native Brig did not stop cleanly'
        # Listener closure is required before transferring process ownership.
        with socket.socket() as check:
            assert check.connect_ex(('127.0.0.1', 8082)) != 0, 'native listener remains'
        if client_lifecycle_contract:
            if stomp_internal_events:
                report['stomp_native_failure_cleanup'] = stomp_broker.drain_failure()
            # Enqueue only after native ownership ends: Go must consume durable
            # work for an account/properties created by the Haskell process.
            enqueue_replay(pending_replay_actor)
            if service_deletion_queue:
                pending_service_dequeue = enqueue_service(pending_service_actor)
        candidate = Path('/candidate/brig')
        report['candidate_brig_sha256'] = hashlib.sha256(candidate.read_bytes()).hexdigest()
        log = open('/reports/brig-go.log', 'w')
        logs.append(log)
        active = subprocess.Popen([str(candidate), '-c', str(runtime.WORK / 'brig-static-0.json')],
                                  cwd=runtime.WORK / 'services/brig', stdout=log, stderr=subprocess.STDOUT)
        processes.append(active)
        ready(active)
        if client_lifecycle_contract:
            report['internal_queue_go_consumes_native'] = await_replay(pending_replay_actor)
            if service_deletion_queue:
                go_service_rows = await_service(pending_service_actor, 'go-consumes-native', pending_service_dequeue)
                await_service(pending_service_actor, 'go-replays-cleanup', enqueue_service(pending_service_actor))
        go_records = [request(*case)[0] for case in cases]
        report['records'].append({'implementation': 'go', 'observations': go_records})
        mismatches = []
        for index, (native, go) in enumerate(zip(native_records, go_records)):
            fields = ('status', 'content_type', 'json' if 'json' in native else 'body')
            if any(native.get(key) != go.get(key) for key in fields):
                mismatches.append({'case': index, 'path': cases[index][1], 'fields': [key for key in fields if native.get(key) != go.get(key)]})
        report['mismatches'] = mismatches
        assert not mismatches, 'paired response mismatches'
        if client_lifecycle_contract:
            fields=lambda record: ('status','content_type','json' if 'json' in record else 'body')
            same=lambda ref,actual: all(ref.get(k)==actual.get(k) for k in fields(ref))
            go_connection_controls=[request(*case)[0] for case in connection_cases]
            go_connection_pages=[request(*case)[0] for case in connection_page_cases]
            report['connections_go_controls']=go_connection_controls
            report['connections_go_native_cursors']=go_connection_pages
            assert all(same(ref,actual) for ref,actual in zip(native_connection_controls,go_connection_controls)), 'native/Go connection reads mismatch'
            assert all(same(ref,actual) for ref,actual in zip(native_connection_pages,go_connection_pages)), 'native/Go connection paging or cursor mismatch'
            go_lifecycle_state=[request('GET',lifecycle_path,lifecycle_headers)[0],request('GET',lifecycle_path+'/prekeys',lifecycle_headers)[0]]
            report['client_lifecycle_go_reads_native']=go_lifecycle_state
            assert all(same(ref,actual) for ref,actual in zip(native_lifecycle_state,go_lifecycle_state)), 'Go lost native-created client/prekey state'
            go_lifecycle_controls=[request(*case)[0] for case in lifecycle_cases]
            report['client_lifecycle_go_controls']=go_lifecycle_controls
            lifecycle_mismatches=[{'case':i,'fields':[k for k in fields(ref) if ref.get(k)!=actual.get(k)]} for i,(ref,actual) in enumerate(zip(native_lifecycle_controls,go_lifecycle_controls)) if not same(ref,actual)]
            report['client_lifecycle_mismatches']=lifecycle_mismatches
            assert not lifecycle_mismatches, 'native lifecycle admission responses mismatch'
            go_claim_controls=[request(*case)[0] for case in claim_cases]
            report['prekey_go_claim_controls']=go_claim_controls
            assert all(same(ref,actual) for ref,actual in zip(native_claim_controls,go_claim_controls)), 'native prekey claim/bundle response mismatch'
            assert request('GET',lifecycle_path+'/prekeys',lifecycle_headers)[0]['json']==[65535], 'Go did not consume only the ordinary prekey'
            go_multi_controls=[request(*case)[0] for case in multi_cases]
            report['prekey_go_multi_controls']=go_multi_controls
            assert all(same(ref,actual) for ref,actual in zip(native_multi_controls,go_multi_controls)), 'native multi-user prekey response mismatch'
            go_federation_controls=[request(*case)[0] for case in federation_cases]
            report['prekey_go_federation_controls']=go_federation_controls
            federation_mismatches=[{'case':i,'fields':[k for k in fields(ref) if ref.get(k)!=actual.get(k)]} for i,(ref,actual) in enumerate(zip(native_federation_controls,go_federation_controls)) if not same(ref,actual)]
            report['prekey_federation_mismatches']=federation_mismatches
            assert not federation_mismatches, 'native federation prekey responses mismatch'
            go_public_client_controls=[request(*case)[0] for case in public_client_cases]
            report['public_client_go_controls']=go_public_client_controls
            public_client_mismatches=[{'case':i,'fields':[k for k in fields(ref) if ref.get(k)!=actual.get(k)]} for i,(ref,actual) in enumerate(zip(native_public_client_controls,go_public_client_controls)) if not same(ref,actual)]
            report['public_client_mismatches']=public_client_mismatches
            assert not public_client_mismatches, 'native public client responses mismatch'
            if store_fixture_path:
                assert mls_row_fixture('read','go-reads-native')==native_rows_retained, 'Go lost native MLS retained bytes'
                mls_row_fixture('stage','go-staged')
                go_row_count=request(*row_count_case)[0];report['mls_rows_go_count']=go_row_count
                assert same(row_count_native,go_row_count), 'native/Go MLS usable count mismatch'
                go_rows_retained=mls_row_fixture('read','go-retained')
                assert go_rows_retained==native_rows_retained, 'native/Go MLS retained bytes mismatch'
            go_count_controls=[request(*case)[0] for case in count_cases]
            report['mls_count_go_controls']=go_count_controls
            assert all(same(ref,actual) for ref,actual in zip(native_count_controls,go_count_controls)), 'native public MLS count controls mismatch'
            go_mls_controls=[request(*case)[0] for case in mls_cases]
            report['mls_client_go_controls']=go_mls_controls
            mls_client_mismatches=[{'case':i,'fields':[k for k in fields(ref) if ref.get(k)!=actual.get(k)]} for i,(ref,actual) in enumerate(zip(native_mls_controls,go_mls_controls)) if not same(ref,actual)]
            report['mls_client_mismatches']=mls_client_mismatches
            assert not mls_client_mismatches, 'native MLS client responses mismatch'
            if store_fixture_path:
                go_delete_controls=[request(*case)[0] for case in mls_delete_cases]
                report['mls_delete_go_controls']=go_delete_controls
                assert all(same(ref,actual) for ref,actual in zip(native_delete_controls,go_delete_controls)), 'native/Go public MLS deletion mismatch'
                assert mls_row_fixture('read','go-deleted')==[], 'Go reference deletion did not remove exact rows'
                mls_row_fixture('stage','go-delete-restaged')
                assert same(row_count_native,request(*row_count_case)[0]), 'Go delete restage count changed'
                assert mls_row_fixture('read','go-delete-restored')==native_rows_retained, 'Go delete restage bytes changed'

            if mls_upload_originals and store_fixture_path:
                assert mls_row_fixture('read','signed-go-reads-native',signed_user,signed_client)==signed_native_rows, 'Go lost signed native upload state'
                signed_go_claim,_=request('POST',signed_claim_path,signed_headers)
                report['mls_signed_go_claim_native']=signed_go_claim
                expected_entry={'user':signed_user,'domain':local_domain,'client':signed_client,'key_package_ref':base64.b64encode(bytes.fromhex(signed_native_rows[0]['ref'])).decode(),'key_package':material['valid']}
                assert signed_go_claim['status']==200 and signed_go_claim['json']=={'key_packages':[expected_entry]}, 'Go could not claim exact signed native package'
                assert mls_row_fixture('read','signed-go-consumed-native',signed_user,signed_client)==[], 'Go claim left signed native package'
                go_upload,_=request('POST',signed_path,signed_headers,signed_valid)
                assert same(signed_native_upload,go_upload), 'Go signed upload response differs'
                go_rejection,_=request('PUT',signed_path+'?ciphersuites=1',signed_headers,signed_invalid)
                report['mls_signed_go_rejected_replace']=go_rejection
                assert same(signed_native_rejection,go_rejection), 'native/Go destructive replacement error mismatch'
                assert mls_row_fixture('read','signed-go-rejected-replace',signed_user,signed_client)==[], 'Go rejected replacement kept old state'
                go_replace,_=request('PUT',signed_path+'?ciphersuites=1',signed_headers,signed_valid)
                assert same(signed_native_upload,go_replace), 'Go signed replacement response differs'
                assert mls_row_fixture('read','signed-go-replaced',signed_user,signed_client)==signed_native_rows, 'Go signed replacement changed ref or package bytes'
            if mls_upload_originals:
                import runner,harness
                required=['MLS.KeyPackage.'+name for name in ('testDeleteKeyPackages','testKeyPackageUploadNoKey','testUnsupportedCiphersuite','testClaimKeyPackagesEphemeralUser','testKeyPackageClaim','testKeyPackageSelfClaim','testKeyPackageMultipleCiphersuites','testKeyPackageRemoteClaim','testReplaceKeyPackages','testReplaceKeyPackagesV7')]+[f'MLS.KeyPackage.testKeyPackageCount[suite={suite}]' for suite in ('0x0001','0x0002','0x0005','0x0007')]
                required += ['Connection.'+name+'[domain=other]' for name in ('testCancel','testConnectFromBlocked','testConnectFromIgnored','testConnectionLimits','testSentFromBlocked','testSentFromIgnored')]+['Connection.testConnectWithRemoteUser[domain='+domain+']' for domain in ('otherdomain','owndomain')]
                assert set(required).issubset(runner.discover('brig')), 'original MLS/connection cases absent'
                link=runtime.WORK/'dist/brig';link.unlink();link.symlink_to(candidate)
                xml=Path('/reports/mls-upload-original.xml')
                with open('/reports/mls-upload-original.log','w') as original_log:
                    original=subprocess.run([runner.binary('integration')['path'],'--config',str(runtime.WORK/'services/integration.yaml'),*[arg for name in required for arg in ('--include',name)],'--xml',str(xml)],cwd=runtime.WORK,stdout=original_log,stderr=subprocess.STDOUT,timeout=1200)
                report['original_mls_upload_tests']={'required_tests':required,'process_exit_code':original.returncode,'integration_binary':runner.binary('integration'),'dynamic_brig_sha256':report['candidate_brig_sha256']}
                report['e2e_attempted']=True;report['e2e_executed']=xml.is_file()
                try:
                    report['original_mls_upload_tests']['validated_report']=harness.check_xml(xml,required)
                except Exception as error:
                    original_failures.append(str(error))
                if original.returncode != 0: original_failures.append('original compiled MLS/connection tests failed')
                report['original_mls_upload_tests']['failures']=list(original_failures)
            go_profile_controls=[request(*case)[0] for case in profile_cases]
            report['profiles_go_controls']=go_profile_controls
            assert all(same(ref,actual) for ref,actual in zip(native_profile_controls,go_profile_controls)), 'native/Go federation profile projection mismatch'
            go_connection_create_controls=[request(*case)[0] for case in connection_create_cases]
            report['connections_go_create_controls']=go_connection_create_controls
            go_connection_update_controls=[request(*case)[0] for case in connection_update_cases]
            report['connections_go_update_controls']=go_connection_update_controls
            assert all(same(ref,actual) for ref,actual in zip(native_connection_update_controls,go_connection_update_controls)), 'native/Go connection update admission mismatch'
            assert all(same(ref,actual) for ref,actual in zip(native_connection_create_controls,go_connection_create_controls)), 'native/Go connection creation admission mismatch'
            go_connection_created,_=request('POST',connection_create_path,connection_write_headers)
            report['connections_go_created']=go_connection_created
            assert go_connection_created['status']==201 and go_connection_created['json']['status']=='sent', 'Go local connection creation failed'
            assert go_connection_created['json']['from']==connection_writer and go_connection_created['json']['to']==connection_recipient, 'Go directed connection identity changed'
            reverse_path='/v19/connections/'+local_domain+'/'+connection_writer
            recipient_headers={'Z-User':connection_recipient,'Z-Connection':'synthetic-local-recipient'}
            go_connection_reverse,_=request('GET',reverse_path,recipient_headers)
            report['connections_go_reverse']=go_connection_reverse
            assert go_connection_reverse['status']==200 and go_connection_reverse['json']['status']=='pending', 'Go reverse pending row missing'
            assert go_connection_reverse['json']['qualified_conversation']==go_connection_created['json']['qualified_conversation'], 'Go connection pair has distinct conversations'
            connection_update_results=[]
            for path,headers,desired,status,actual in [(reverse_path,recipient_headers,'ignored',200,'ignored'),(reverse_path,recipient_headers,'accepted',200,'accepted'),(reverse_path,recipient_headers,'accepted',204,None),(reverse_path,recipient_headers,'blocked',200,'blocked'),(reverse_path,recipient_headers,'sent',200,'accepted'),(connection_create_path,connection_write_headers,'blocked',200,'blocked'),(connection_create_path,connection_write_headers,'accepted',200,'accepted')]:
                updated,_=request('PUT',path,headers,{'status':desired})
                connection_update_results.append(updated)
                assert updated['status']==status and (actual is None or updated['json']['status']==actual), 'Go local connection transition differs'
            report['connections_go_updates']=connection_update_results
            go_connection_created=request('GET',connection_create_path,connection_write_headers)[0]
            go_connection_reverse=request('GET',reverse_path,recipient_headers)[0]
            report['connections_go_final_pair']=[go_connection_created,go_connection_reverse]
            assert go_connection_created['json']['status']==go_connection_reverse['json']['status']=='accepted', 'Go final directed connection state differs'
            notification_deadline=time.monotonic()+5
            expected_notifications=[(connection_writer,['sent','accepted','blocked','accepted']),(connection_recipient,['pending','ignored','accepted','blocked','accepted'])]
            delivered=[]
            for actor,expected in expected_notifications:
                while True:
                    notifications,_=request('GET','/notifications?size=100',{'Z-User':actor},port=8086)
                    assert notifications['status']==200, 'connection notification read failed'
                    events=[event for notification in notifications['json']['notifications'] for event in notification['payload'] if event.get('type')=='user.connection']
                    if len(events)>=len(expected):break
                    assert time.monotonic()<notification_deadline, 'connection notifications missing'
                    time.sleep(0.05)
                assert sorted(e['connection']['status'] for e in events)==sorted(expected), 'connection notification statuses differ'
                assert all(e['connection']['from']==actor for e in events), 'connection notification recipient differs'
                delivered.append({'user':actor,'events':events})
            report['connections_go_delivered_notifications']=delivered
            remote_connection_domain,remote_connection_target=connection_targets[-1]
            remote_connection_path='/v19/connections/'+remote_connection_domain+'/'+remote_connection_target
            remote_connection_created,_=request('POST',remote_connection_path,connection_write_headers)
            report['connections_go_remote_created']=remote_connection_created
            assert remote_connection_created['status']==201 and remote_connection_created['json']['status']=='sent', 'Go remote connection creation failed'
            remote_native_reverse_path='/v19/connections/'+local_domain+'/'+connection_writer
            remote_native_headers={'Z-User':remote_connection_target,'Z-Connection':'synthetic-remote-action'}
            remote_native_reverse=request('GET',remote_native_reverse_path,remote_native_headers,port=9082)[0]
            report['connections_remote_native_reverse']=remote_native_reverse
            assert remote_native_reverse['status']==200 and remote_native_reverse['json']['status']=='pending', 'native remote pending row missing'
            assert remote_native_reverse['json']['qualified_conversation']==remote_connection_created['json']['qualified_conversation'], 'Go deterministic conversation differs from native remote ID'
            remote_connection_updates=[]
            for desired,expected in [('blocked','blocked'),('accepted','sent'),('cancelled','cancelled')]:
                response,_=request('PUT',remote_connection_path,connection_write_headers,{'status':desired})
                remote_connection_updates.append(response)
                assert response['status']==200 and response['json']['status']==expected, 'Go remote action transition differs'
            report['connections_go_remote_updates']=remote_connection_updates
            remote_connection_final=request('GET',remote_connection_path,connection_write_headers)[0]
            remote_native_final=request('GET',remote_native_reverse_path,remote_native_headers,port=9082)[0]
            report['connections_remote_native_final']=remote_native_final
            assert remote_native_final['status']==200 and remote_native_final['json']['status']=='cancelled', 'native remote rescind effect missing'
            remote_reconnect=request('POST',remote_native_reverse_path,remote_native_headers,port=9082)[0]
            report['connections_native_initiated_reconnect']=remote_reconnect
            assert remote_reconnect['status']==200 and remote_reconnect['json']['status']=='sent', 'native incoming connection action failed'
            remote_incoming=request('GET',remote_connection_path,connection_write_headers)[0]
            report['connections_go_incoming_pending']=remote_incoming
            assert remote_incoming['status']==200 and remote_incoming['json']['status']=='pending', 'Go incoming pending action missing'
            remote_accept=request('PUT',remote_connection_path,connection_write_headers,{'status':'accepted'})[0]
            report['connections_go_incoming_accept']=remote_accept
            assert remote_accept['status']==200 and remote_accept['json']['status']=='accepted', 'Go incoming accept failed'
            remote_native_accepted=request('GET',remote_native_reverse_path,remote_native_headers,port=9082)[0]
            report['connections_remote_native_accepted']=remote_native_accepted
            assert remote_native_accepted['status']==200 and remote_native_accepted['json']['status']=='accepted', 'native reciprocal accepted effect missing'
            remote_connection_final=request('GET',remote_connection_path,connection_write_headers)[0]
            go_created,_=request('POST','/v19/clients',lifecycle_headers,{**new_body,'password':password})
            report['client_lifecycle_go_created']=go_created
            assert go_created['status']==201 and go_created['location']==go_created['json']['id'], 'Go client creation failed'
            go_id=go_created['json']['id'];assert go_id!=lifecycle_id, 'synthetic distinct prekey fixture collision'
            go_path='/v19/clients/'+go_id
            go_created_state=[request('GET',go_path,lifecycle_headers)[0],request('GET',go_path+'/prekeys',lifecycle_headers)[0]]
            report['client_lifecycle_go_created_state']=go_created_state
            assert go_created_state[0]['json']==go_created['json'] and go_created_state[1]['json']==[7,65535], 'Go creation did not persist response and keys'
            deleted,_=request('DELETE',lifecycle_path,lifecycle_headers,{'password':password})
            report['client_lifecycle_go_deleted']=deleted
            assert deleted['status']==200 and deleted['body']=='' and deleted['content_type'] is None, 'Go native-client deletion failed'
            go_deleted_state=[request('GET',lifecycle_path,lifecycle_headers)[0],request('GET',lifecycle_path+'/prekeys',lifecycle_headers)[0]]
            report['client_lifecycle_go_deleted_state']=go_deleted_state
            assert go_deleted_state[0]['status']==404 and go_deleted_state[1]['json']==[], 'Go deletion retained client/prekeys'
            if service_deletion_queue:
                go_pending_service_actor = service_actor('go-pending')
            if stomp_internal_events:
                report['stomp_go_failure_redelivery'] = stomp_broker.failure_probe()
            active.terminate();report['client_lifecycle_go_stop_exit_code']=active.wait(timeout=35)
            assert report['client_lifecycle_go_stop_exit_code']==0, 'Go lifecycle stop failed'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1',8082))!=0, 'Go lifecycle listener remains'
            if stomp_internal_events:
                report['stomp_go_failure_cleanup'] = stomp_broker.drain_failure()
            if service_deletion_queue:
                go_pending_service_dequeue = enqueue_service(go_pending_service_actor)
            log=open('/reports/brig-native-client-lifecycle-switchback.log','w');logs.append(log)
            active=subprocess.Popen([str(native_brig_path),'-c',str(runtime.WORK/'brig-static-0.json')],cwd=runtime.WORK/'services/brig',stdout=log,stderr=subprocess.STDOUT)
            processes.append(active);ready(active)
            if service_deletion_queue:
                native_readback = await_service(pending_service_actor, 'native-reads-go')
                assert native_readback == go_service_rows, 'native switchback service raw state differs'
                await_service(go_pending_service_actor, 'native-consumes-go', go_pending_service_dequeue)
                await_service(pending_service_actor, 'native-replays-cleanup', enqueue_service(pending_service_actor))
            native_connection_switchback=[]
            for i,go_page in enumerate(go_connection_pages):
                body={'size':1}
                if i: body['paging_state']=go_connection_pages[i-1]['json']['paging_state']
                native_connection_switchback.append(request('POST','/v19/list-connections',connection_headers,body)[0])
            report['connections_native_switchback']=native_connection_switchback
            native_connection_creation_reads=[request('GET',connection_create_path,connection_write_headers)[0],request('GET',reverse_path,recipient_headers)[0]]
            report['connections_native_reads_go_creation']=native_connection_creation_reads
            remote_connection_switchback=request('GET',remote_connection_path,connection_write_headers)[0]
            report['connections_native_reads_go_remote']=remote_connection_switchback
            assert same(remote_connection_final,remote_connection_switchback), 'native lost Go remote connection state'
            assert all(same(ref,actual) for ref,actual in zip([go_connection_created,go_connection_reverse],native_connection_creation_reads) if ref['status']==200), 'native lost Go reverse connection row'
            forward_read=native_connection_creation_reads[0]
            assert forward_read['status']==200 and forward_read.get('content_type')==go_connection_created.get('content_type') and forward_read['json']==go_connection_created['json'], 'native lost Go created connection row'
            native_connection_accepted,_=request('POST',reverse_path,recipient_headers)
            report['connections_native_accepts_go_creation']=native_connection_accepted
            assert native_connection_accepted['status']==200 and native_connection_accepted['json']['status']=='accepted', 'native cannot accept Go-created Galley conversation'
            assert all(same(ref,actual) for ref,actual in zip(go_connection_pages,native_connection_switchback)), 'native cannot use Go connection cursors after switchback'
            native_go_state=[request('GET',go_path,lifecycle_headers)[0],request('GET',go_path+'/prekeys',lifecycle_headers)[0]]
            native_deleted_state=[request('GET',lifecycle_path,lifecycle_headers)[0],request('GET',lifecycle_path+'/prekeys',lifecycle_headers)[0]]
            report['client_lifecycle_native_reads_go']=native_go_state
            report['client_lifecycle_native_reads_deletion']=native_deleted_state
            assert all(same(ref,actual) for ref,actual in zip(go_created_state,native_go_state)), 'native lost Go-created client state'
            assert all(same(ref,actual) for ref,actual in zip(go_deleted_state,native_deleted_state)), 'native deletion response mismatch'
            claim,_=request('GET','/v19/users/'+peers['brig']['optSettings']['setFederationDomain']+'/'+lifecycle_user+'/prekeys/'+go_id,lifecycle_headers)
            report['client_lifecycle_native_claims_go_prekey']=claim
            assert claim['status']==200 and claim['json']['prekey']=={'id':7,'key':ordinary_key}, 'native cannot consume Go-created prekey'
            restored,_=request('POST','/v19/clients',lifecycle_headers,{**native_body,'prekeys':[],'mls_public_keys':{},'password':password})
            report['client_lifecycle_native_recreated_deleted']=restored
            assert restored['status']==201, 'native cannot recreate Go-deleted client'
            retained,_=request('GET',lifecycle_path,lifecycle_headers)
            report['client_lifecycle_native_retained_mls']=retained
            assert retained['status']==200 and retained['json']['mls_public_keys']=={'ed25519':'AQI='}, 'Go deletion changed native MLS retention'
            if store_fixture_path:
                native_final_count=request(*row_count_case)[0];report['mls_rows_native_switchback_count']=native_final_count
                assert same(row_count_native,native_final_count), 'native lost Go MLS count state'
                assert mls_row_fixture('read','native-switchback')==go_rows_retained, 'native lost Go retained MLS bytes'
                report['mls_rows_switchback']={'same_initialized_stores':True,'native_go_counts_equal':True,'native_go_cleanup_bytes_equal':True,'native_reads_go_retained_state':True,'expired_future_non_key_source_deleted':True,'malformed_retained':True}
            if mls_upload_originals and store_fixture_path:
                assert mls_row_fixture('read','signed-native-reads-go',signed_user,signed_client)==signed_native_rows, 'native lost Go signed replacement'
                native_signed_count,_=request('GET',signed_count_path,signed_headers)
                assert native_signed_count['status']==200 and native_signed_count['json']=={'count':1}, 'native did not count Go signed package'
                final_claim,_=request('POST',signed_claim_path,signed_headers)
                report['mls_signed_native_claims_go']=final_claim
                assert same(signed_go_claim,final_claim), 'native could not claim exact Go signed replacement'
                assert mls_row_fixture('read','signed-native-consumes-go',signed_user,signed_client)==[], 'native claim left Go signed replacement'
                report['mls_signed_switchback']={'same_initialized_stores':True,'go_claims_native_signed_upload':True,'native_claims_go_signed_replacement':True,'exact_ref_and_raw_bytes':True,'native_go_destructive_replace_failure_equal':True}
            report['client_lifecycle_switchback']={'same_initialized_stores':True,'native_accepts_go_creation':True,'native_accepts_go_deletion':True,'native_claims_go_created_prekey':True,'native_mls_retention_preserved':True}
        if client_update_contract:
            fields=lambda record: ('status','content_type','json' if 'json' in record else 'body')
            go_updates=[request(*case)[0] for case in update_cases]
            report['go_client_update_controls']=go_updates
            mismatches=[{'case':i,'fields':[k for k in fields(ref) if ref.get(k)!=actual.get(k)]} for i,(ref,actual) in enumerate(zip(native_updates,go_updates)) if any(ref.get(k)!=actual.get(k) for k in fields(ref))]
            report['client_update_mismatches']=mismatches
            go_positive,_=request('PUT',update_path,update_headers,positive_update)
            report['go_client_update_positive']=go_positive
            assert all(native_positive.get(k)==go_positive.get(k) for k in fields(native_positive)), 'native positive update response mismatch'
            go_state=[request('GET',update_path,update_headers)[0],request('GET',update_path+'/prekeys',update_headers)[0]]
            report['go_client_update_state']=go_state
            assert go_state[0]['status']==200 and go_state[0]['json']['label']=='go-owned-client', 'Go label not persisted'
            assert set(go_state[0]['json']['capabilities'])==set(positive_update['capabilities']), 'Go capability update not persisted'
            assert go_state[0]['json']['mls_public_keys']=={'ed25519':'AQI=','ecdsa_secp256r1_sha256':'AQID'}, 'Go MLS LWT update not persisted'
            assert go_state[1]['status']==200 and go_state[1]['json']==[7,65535], 'Go prekeys not persisted'
            active.terminate();report['go_client_update_stop_exit_code']=active.wait(timeout=35)
            assert report['go_client_update_stop_exit_code']==0, 'Go client update stop failed'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1',8082))!=0, 'Go client update listener remains'
            log=open('/reports/brig-native-client-update-switchback.log','w');logs.append(log)
            active=subprocess.Popen([str(native_brig_path),'-c',str(runtime.WORK/'brig-static-0.json')],cwd=runtime.WORK/'services/brig',stdout=log,stderr=subprocess.STDOUT)
            processes.append(active);ready(active)
            native_state=[request('GET',update_path,update_headers)[0],request('GET',update_path+'/prekeys',update_headers)[0]]
            report['native_reads_go_client_update']=native_state
            assert all(all(ref.get(k)==actual.get(k) for k in fields(ref)) for ref,actual in zip(go_state,native_state)), 'native lost Go client update state'
            native_claim,_=request('GET','/v19/users/'+peers['brig']['optSettings']['setFederationDomain']+'/'+update_user+'/prekeys/'+update_id,update_headers)
            report['native_claims_go_updated_prekey']=native_claim
            assert native_claim['status']==200 and native_claim['json']['prekey']=={'id':7,'key':replacement_key}, 'native could not consume Go updated prekey'
            report['client_update_switchback']={'same_initialized_stores':True,'native_accepts_go_client_state':True,'native_claims_go_updated_prekey':True}
            assert not mismatches, 'native client update responses mismatch'
        if client_registration_contract:
            import runner,harness
            required=['Client.testClientLastActive','Client.testCreateClientWithCapabilities','Client.testUpdateClientWithConsumableNotificationsCapability','Client.testGetClientCapabilitiesV7']
            assert set(required).issubset(runner.discover('brig')), 'original client cases absent'
            link=runtime.WORK/'dist/brig';link.unlink();link.symlink_to(candidate)
            xml=Path('/reports/client-registration-original.xml')
            with open('/reports/client-registration-original.log','w') as original_log:
                original=subprocess.run([runner.binary('integration')['path'],'--config',str(runtime.WORK/'services/integration.yaml'),*[arg for name in required for arg in ('--include',name)],'--xml',str(xml)],cwd=runtime.WORK,stdout=original_log,stderr=subprocess.STDOUT,timeout=1200)
            report['original_client_registration_tests']={'required_tests':required,'process_exit_code':original.returncode,'integration_binary':runner.binary('integration'),'dynamic_brig_sha256':report['candidate_brig_sha256']}
            report['e2e_attempted']=True;report['e2e_executed']=xml.is_file()
            if xml.is_file():
                report['original_client_registration_tests']['validated_report']=harness.check_xml(xml,required)
            assert original.returncode==0, 'original compiled client registration tests failed'
        if properties_contract:
            fields=lambda record: ('status','content_type','json' if 'json' in record else 'body')
            go_property_state=[request('GET','/properties/native-durable',property_headers)[0],request('GET','/properties-values',property_headers)[0]]
            report['go_reads_native_properties']=go_property_state
            assert all(all(ref.get(k)==actual.get(k) for k in fields(ref)) for ref,actual in zip(native_property_state,go_property_state)), 'Go lost native property state'
            assert go_property_state[0]['body']==raw_property, 'Go changed native raw property'
            cleared,_=request('DELETE','/properties',property_headers)
            assert cleared['status']==200, 'Go property control reset failed'
            go_properties=[request(*case)[0] for case in property_cases]
            report['go_properties']=go_properties
            mismatches=[{'case':i,'fields':[k for k in fields(ref) if ref.get(k)!=actual.get(k)]} for i,(ref,actual) in enumerate(zip(native_properties,go_properties)) if any(ref.get(k)!=actual.get(k) for k in fields(ref))]
            report['property_mismatches']=mismatches
            # Continue independent original tests/state witness before reporting
            # exact native media/parser differences, retaining every response.
            assert go_properties[2]['body']==raw_property, 'Go changed raw bytes'
            staged,_=request('PUT','/properties/go-durable',property_headers,None,raw_property)
            assert staged['status']==200, 'Go durable property write failed'
            go_written_properties=[request('GET','/properties/go-durable',property_headers)[0],request('GET','/properties-values',property_headers)[0]]
            report['go_written_properties']=go_written_properties
            import runner,harness
            required=['Property.testClearProperties','Property.testGetProperties','Property.testMaxLength','Property.testMaxProperties','Property.testPropertyNameNotAscii','Property.testSetGetDeleteProperty']
            assert set(required).issubset(runner.discover('brig')), 'original property cases absent'
            link=runtime.WORK/'dist/brig';link.unlink();link.symlink_to(candidate)
            xml=Path('/reports/properties-original.xml')
            with open('/reports/properties-original.log','w') as original_log:
                original=subprocess.run([runner.binary('integration')['path'],'--config',str(runtime.WORK/'services/integration.yaml'),'--include','Property.','--xml',str(xml)],cwd=runtime.WORK,stdout=original_log,stderr=subprocess.STDOUT,timeout=1200)
            report['original_property_tests']={'required_tests':required,'process_exit_code':original.returncode,'integration_binary':runner.binary('integration'),'dynamic_brig_sha256':report['candidate_brig_sha256']}
            report['e2e_attempted']=True;report['e2e_executed']=xml.is_file()
            if original.returncode==0: report['original_property_tests']['validated_report']=harness.check_xml(xml,required)
            active.terminate();report['go_property_stop_exit_code']=active.wait(timeout=35)
            assert report['go_property_stop_exit_code']==0, 'Go property stop failed'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1',8082))!=0, 'Go property listener remains'
            log=open('/reports/brig-native-property-switchback.log','w');logs.append(log)
            active=subprocess.Popen([str(native_brig_path),'-c',str(runtime.WORK/'brig-static-0.json')],cwd=runtime.WORK/'services/brig',stdout=log,stderr=subprocess.STDOUT)
            processes.append(active);ready(active)
            native_consumed=[request('GET','/properties/go-durable',property_headers)[0],request('GET','/properties-values',property_headers)[0]]
            report['native_reads_go_properties']=native_consumed
            assert all(all(ref.get(k)==actual.get(k) for k in fields(ref)) for ref,actual in zip(go_written_properties,native_consumed)), 'native lost Go property state'
            assert native_consumed[0]['body']==raw_property, 'native changed Go raw property'
            report['property_switchback']={'same_initialized_stores':True,'native_accepts_go_property_state':True,'raw_json_unchanged':True}
            assert original.returncode==0, 'original compiled property tests failed'
            assert not mismatches, 'native property responses mismatch'
        if ses_feedback_contract:
            go_federation,_=request('GET','/i/federation/remotes')
            report['go_federation_config']=go_federation
            assert all(native_federation.get(k)==go_federation.get(k) for k in ('status','content_type','json')), 'native federation configuration mismatch'
            go_federation_versions=[request(*case)[0] for case in federation_version_cases]
            report['go_federation_versions']=go_federation_versions
            assert all(all(ref.get(k)==actual.get(k) for k in ('status','content_type','json' if 'json' in ref else 'body')) for ref,actual in zip(native_federation_versions,go_federation_versions)), 'native federation version responses mismatch'
            go_admin=[request(*case)[0] for case in admin_cases]
            report['go_blacklist_api']=go_admin
            fields=lambda record: ('status','content_type','json' if 'json' in record else 'body')
            admin_mismatches=[{'case':i,'fields':[k for k in fields(ref) if ref.get(k)!=actual.get(k)]} for i,(ref,actual) in enumerate(zip(native_admin,go_admin)) if any(ref.get(k)!=actual.get(k) for k in fields(ref))]
            report['blacklist_api_mismatches']=admin_mismatches
            assert not admin_mismatches, 'native blacklist API mismatch'
            native_state=[request('GET','/i/users/blacklist?'+urlencode({'email':actor['email']}))[0] for actor in native_feedback_actors]
            report['go_reads_native_feedback']=native_state
            assert all(all(ref.get(k)==actual.get(k) for k in fields(ref)) for ref,actual in zip(native_feedback,native_state)), 'Go lost native feedback blocklist state'
            go_feedback_actors,go_feedback,go_rejections=feedback_observations('go')
            assert all(all(ref.get(k)==actual.get(k) for k in fields(ref)) for ref,actual in zip(native_feedback+native_rejections,go_feedback+go_rejections)), 'native feedback response mismatch'
            # Reuse the available original compiled test, not a rewritten test.
            # The legacy SES-specific service test is source-only in this image.
            import runner, harness
            case='Migration.BlockList.testBlockListMigration'
            assert case in runner.discover('brig'), 'original compiled blocklist migration test absent'
            link=runtime.WORK/'dist/brig'
            link.unlink();link.symlink_to(candidate)
            xml=Path('/reports/blacklist-original.xml')
            with open('/reports/blacklist-original.log','w') as original_log:
                original=subprocess.run([runner.binary('integration')['path'],'--config',str(runtime.WORK/'services/integration.yaml'),'--include',case,'--xml',str(xml)],cwd=runtime.WORK,stdout=original_log,stderr=subprocess.STDOUT,timeout=1200)
            report['original_blacklist_test']={'required_tests':[case],'process_exit_code':original.returncode,'integration_binary':runner.binary('integration'),'dynamic_brig_sha256':report['candidate_brig_sha256']}
            report['e2e_attempted']=True
            report['e2e_executed']=xml.is_file()
            if original.returncode==0:
                report['original_blacklist_test']['validated_report']=harness.check_xml(xml,[case])
            active.terminate()
            report['go_feedback_stop_exit_code']=active.wait(timeout=35)
            assert report['go_feedback_stop_exit_code']==0, 'Go feedback stop failed'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1',8082))!=0, 'Go feedback listener remains'
            log=open('/reports/brig-native-feedback-switchback.log','w');logs.append(log)
            active=subprocess.Popen([str(native_brig_path),'-c',str(runtime.WORK/'brig-static-0.json')],cwd=runtime.WORK/'services/brig',stdout=log,stderr=subprocess.STDOUT)
            processes.append(active);ready(active)
            native_consumed=[request('GET','/i/users/blacklist?'+urlencode({'email':actor['email']}))[0] for actor in go_feedback_actors]
            report['native_reads_go_feedback']=native_consumed
            assert all(all(ref.get(k)==actual.get(k) for k in fields(ref)) for ref,actual in zip(go_feedback,native_consumed)), 'native lost Go feedback blocklist state'
            report['feedback_switchback']={'native_and_go_consume_and_acknowledge':True,'permanent_and_complaint_block':True,'transient_and_undetermined_do_not_block':True,'native_accepts_go_blocklist_state':True,'same_initialized_stores':True}
            assert original.returncode==0, 'original compiled blacklist migration test failed'
        if password_change_contract:
            go_change_records=[request(*case)[0] for case in change_cases]
            report['go_change_controls']=go_change_records
            change_mismatches=[{'case':i,'fields':[k for k in ('status','content_type','json' if 'json' in ref else 'body') if ref.get(k)!=actual.get(k)]} for i,(ref,actual) in enumerate(zip(native_change_records,go_change_records)) if any(ref.get(k)!=actual.get(k) for k in ('status','content_type','json' if 'json' in ref else 'body'))]
            report['change_control_mismatches']=change_mismatches
            assert not change_mismatches, 'native password change responses mismatch'
            go_first,_=request('PUT','/self/password',{'Z-User':change_actors['first']['user']},{'new_password':change_new})
            report['go_first_password']=go_first
            assert all(go_first.get(k)==native_first.get(k) for k in ('status','content_type','body')), 'native first password response mismatch'
            changed,_=request('PUT','/self/password',{'Z-User':change_actors['main']['user']},{'old_password':change_old,'new_password':change_new})
            report['go_changed_password']=changed
            assert changed['status']==200, 'Go existing password change failed'
            main_actor=change_actors['main']
            cookies,_=request('GET','/cookies',{'Z-User':main_actor['user']})
            report['go_change_revoked_cookies']=cookies
            assert cookies['status']==200 and cookies['json']=={'cookies':[]}, 'Go password change retained cookies'
            old_cookie,_=request('POST','/access',{'Cookie':change_cookie.split(';',1)[0]})
            report['go_change_old_cookie']=old_cookie
            assert old_cookie['status']==403, 'Go password change kept original cookie'
            old_login,_=request('POST','/login',body={'email':main_actor['email'],'password':change_old})
            new_login,_=request('POST','/login',body={'email':main_actor['email'],'password':change_new})
            report['go_change_password_logins']=[old_login,new_login]
            assert old_login['status']==403 and new_login['status']==200 and new_login['json']['user']==main_actor['user'], 'Go changed password validation failed'
            active.terminate()
            report['go_change_stop_exit_code']=active.wait(timeout=35)
            assert report['go_change_stop_exit_code']==0, 'Go password change stop failed'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1',8082))!=0, 'Go password change listener remains'
            log=open('/reports/brig-native-change-switchback.log','w');logs.append(log)
            active=subprocess.Popen([str(native_brig_path),'-c',str(runtime.WORK/'brig-static-0.json')],cwd=runtime.WORK/'services/brig',stdout=log,stderr=subprocess.STDOUT)
            processes.append(active);ready(active)
            native_logins=[]
            for actor in (main_actor,change_actors['first']):
                login,_=request('POST','/login',body={'email':actor['email'],'password':change_new})
                assert login['status']==200 and login['json']['user']==actor['user'], 'native rejected Go first/changed password'
                native_logins.append(login)
            report['native_change_consumed_passwords']=native_logins
            native_old_cookie,_=request('POST','/access',{'Cookie':change_cookie.split(';',1)[0]})
            report['native_change_old_cookie']=native_old_cookie
            assert all(native_old_cookie.get(k)==old_cookie.get(k) for k in ('status','content_type','json')), 'native changed-cookie revocation mismatch'
            report['password_change_switchback']={'native_created_accounts':True,'go_first_and_existing_password_written':True,'native_accepts_both_go_passwords':True,'existing_cookie_revoked':True,'same_initialized_stores':True}
        if password_reset_contract:
            go_reset_records = [request(*case)[0] for case in reset_cases]
            report['go_reset_controls'] = go_reset_records
            reset_mismatches = [
                {'case':i,'fields':[key for key in ('status','content_type','json' if 'json' in reference else 'body') if reference.get(key)!=actual.get(key)]}
                for i,(reference,actual) in enumerate(zip(native_reset_records,go_reset_records))
                if any(reference.get(key)!=actual.get(key) for key in ('status','content_type','json' if 'json' in reference else 'body'))]
            report['reset_control_mismatches'] = reset_mismatches
            assert not reset_mismatches, 'native reset control mismatches'
            go_reset, _ = request('POST','/password-reset/complete',body={'email':reset_email,'code':reset_pair['code'],'password':reset_password})
            report['go_consumed_native_reset'] = go_reset
            assert go_reset['status']==200, 'Go rejected native reset code'
            go_empty_cookies, _ = request('GET','/cookies',{'Z-User':reset_user})
            report['go_reset_revoked_cookies'] = go_empty_cookies
            assert go_empty_cookies['status']==200 and go_empty_cookies['json']=={'cookies':[]}, 'Go reset did not revoke original cookies'
            old_login, _ = request('POST','/login',body={'email':reset_email,'password':'Synthetic-password-2026!'})
            report['go_reset_old_password'] = old_login
            assert old_login['status']==403, 'Go reset kept old password'
            old_cookie, _ = request('POST','/access',{'Cookie':reset_cookie.split(';',1)[0]})
            report['go_reset_old_cookie'] = old_cookie
            assert old_cookie['status']==403, 'Go reset kept original cookie'
            new_login, new_cookie = request('POST','/login?persist=true',body={'email':reset_email,'password':reset_password,'label':'synthetic-reset-device'})
            report['go_reset_new_password'] = new_login
            assert new_login['status']==200 and new_cookie and new_login['json']['user']==reset_user, 'Go reset new password rejected'
            go_reset_begin, _ = request('POST','/password-reset',body={'email':reset_email})
            report['go_reset_begin'] = go_reset_begin
            assert go_reset_begin['status']==201, 'Go fresh reset/mail initiation failed'
            go_reset_pair, _ = request('GET','/i/users/password-reset-code?'+urlencode({'email':reset_email}))
            report['go_reset_pair'] = go_reset_pair
            assert go_reset_pair['status']==200 and go_reset_pair['json']['key']==reset_pair['key'] and go_reset_pair['json']['code']!=reset_pair['code'], 'Go reset did not persist fresh random code'
            reset_reads=[('GET','/self',{'Z-User':reset_user}),('GET','/cookies',{'Z-User':reset_user})]
            reset_go_reads=[request(*case)[0] for case in reset_reads]
            report['go_reset_stored_reads']=reset_go_reads
            active.terminate()
            report['go_reset_stop_exit_code']=active.wait(timeout=35)
            assert report['go_reset_stop_exit_code']==0, 'Go reset process did not stop cleanly'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1',8082))!=0, 'Go reset listener remains'
            log=open('/reports/brig-native-reset-switchback.log','w');logs.append(log)
            active=subprocess.Popen([str(native_brig_path),'-c',str(runtime.WORK/'brig-static-0.json')],cwd=runtime.WORK/'services/brig',stdout=log,stderr=subprocess.STDOUT)
            processes.append(active);ready(active)
            native_reset_reads=[request(*case)[0] for case in reset_reads]
            report['native_reset_consumed_reads']=native_reset_reads
            for reference,actual in zip(reset_go_reads,native_reset_reads):
                assert all(reference.get(key)==actual.get(key) for key in ('status','content_type','json')), 'native reset durable profile/cookie mismatch'
            native_new_login, _ = request('POST','/login',body={'email':reset_email,'password':reset_password})
            report['native_reset_go_password']=native_new_login
            assert native_new_login['status']==200 and native_new_login['json']['user']==reset_user, 'native rejected Go reset password'
            native_old_cookie, _ = request('POST','/access',{'Cookie':reset_cookie.split(';',1)[0]})
            report['native_reset_revoked_original_cookie']=native_old_cookie
            assert all(native_old_cookie.get(key)==old_cookie.get(key) for key in ('status','content_type','json')), 'native reset revoked-cookie response differs'
            native_go_pair, _ = request('GET','/i/users/password-reset-code?'+urlencode({'email':reset_email}))
            report['native_consumed_go_reset_pair']=native_go_pair
            assert all(native_go_pair.get(key)==go_reset_pair.get(key) for key in ('status','content_type','json')), 'native rejected Go reset row'
            final_password='Synthetic-reset-native-final-2026!'
            native_completion, _ = request('POST','/password-reset/complete',body={**go_reset_pair['json'],'password':final_password})
            report['native_consumed_go_reset']=native_completion
            assert native_completion['status']==200, 'native rejected Go reset code'
            native_revoked_go_cookie, _ = request('POST','/access',{'Cookie':new_cookie.split(';',1)[0]})
            report['native_reset_revoked_go_cookie']=native_revoked_go_cookie
            assert native_revoked_go_cookie['status']==403, 'native reset kept Go-issued cookie'
            final_login, _ = request('POST','/login',body={'email':reset_email,'password':final_password})
            report['native_final_reset_password']=final_login
            assert final_login['status']==200 and final_login['json']['user']==reset_user, 'native final reset password rejected'
            report['password_reset_switchback']={'native_code_consumed_go':True,'go_code_consumed_native':True,'native_accepts_go_reset_password':True,'both_resets_revoke_prior_cookies':True,'same_initialized_stores':True}
        if public_registration_contract:
            go_public_errors = [request(*case)[0] for case in public_cases]
            report['go_public_header_controls'] = go_public_errors
            report['public_header_mismatches'] = [
                {'case':index,'fields':[field for field in ('status','content_type','body') if reference.get(field)!=actual.get(field)]}
                for index,(reference,actual) in enumerate(zip(native_public_errors,go_public_errors))
                if any(reference.get(field)!=actual.get(field) for field in ('status','content_type','body'))]
            public_email='synthetic-go-public-'+uuid.uuid4().hex+'@example.com'
            public_password='Synthetic-public-password-2026!'
            public_created, public_cookie = request('POST','/register',{'X-Forwarded-For':'127.0.0.42'},
                {'name':'Synthetic Go public registration','email':public_email,'password':public_password,'label':'synthetic-public-device'})
            report['go_public_creation'] = public_created
            assert public_created['status']==201 and public_cookie, 'Go public registration failed'
            public_user=public_created['json']['id']
            assert str(uuid.UUID(public_user))==public_user and public_created['location']==public_user, 'Go public UUID/Location contract failed'
            assert public_created['json']['email']==public_email and public_created['json']['name']=='Synthetic Go public registration', 'Go public request projection failed'
            from http.cookies import SimpleCookie
            def cookie_attributes(raw):
                cookie=SimpleCookie();cookie.load(raw)
                assert set(cookie)=={'zuid'}, 'public cookie name changed'
                return {key:cookie['zuid'][key] for key in ('path','httponly','secure','samesite','domain','max-age')}, bool(cookie['zuid']['expires'])
            assert cookie_attributes(native_public_cookie)==cookie_attributes(public_cookie), 'native public cookie attributes mismatch'
            pair,_=request('GET','/i/users/activation-code?'+urlencode({'email':public_email}))
            report['go_public_activation_pair_lookup']=pair
            assert pair['status']==200 and set(pair['json'])=={'key','code'}, 'Go public durable activation pair missing'
            activated,_=request('POST','/activate',body={**pair['json'],'dryrun':False})
            report['go_public_activation']=activated
            assert activated['status']==200 and activated['json']=={'email':public_email,'first':True}, 'Go public first activation failed'
            public_reads=[('GET','/self',{'Z-User':public_user}),('GET','/cookies',{'Z-User':public_user})]
            public_go_reads=[request(*case)[0] for case in public_reads]
            report['go_public_stored_reads']=public_go_reads
            active.terminate()
            report['go_public_stop_exit_code']=active.wait(timeout=35)
            assert report['go_public_stop_exit_code']==0, 'Go public process did not stop cleanly'
            with socket.socket() as check:
                assert check.connect_ex(('127.0.0.1',8082))!=0, 'Go public listener remains'
            log=open('/reports/brig-native-public-switchback.log','w');logs.append(log)
            active=subprocess.Popen([str(native_brig_path),'-c',str(runtime.WORK/'brig-static-0.json')],cwd=runtime.WORK/'services/brig',stdout=log,stderr=subprocess.STDOUT)
            processes.append(active);ready(active)
            public_native_reads=[request(*case)[0] for case in public_reads]
            report['native_public_consumed_reads']=public_native_reads
            for reference,actual in zip(public_go_reads,public_native_reads):
                assert all(reference.get(field)==actual.get(field) for field in ('status','content_type','json')), 'native public durable profile/cookie mismatch'
            consumed,_=request('POST','/access',{'Cookie':public_cookie.split(';',1)[0]})
            report['native_public_consumed_cookie']=consumed
            assert consumed['status']==200 and consumed['json']['user']==public_user, 'native rejected Go public registration cookie'
            login,_=request('POST','/login?persist=true',body={'email':public_email,'password':public_password})
            report['native_public_consumed_password']=login
            assert login['status']==200 and login['json']['user']==public_user, 'native rejected Go public registration password'
            report['public_registration_switchback']={'actual_fresh_go_account':True,'native_consumed_profile_cookie_password':True,'same_initialized_stores':True,'native_public_cookie_attributes_match':True,'creation_uuid_and_token_bytes_compared':False}
            assert not report['public_header_mismatches'], 'public header response mismatches'
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
                'scope': ('SAML internal creation/rejoin through original native Galley; no invitation/public/email activation or fresh-member/delivery proof'
                          if saml_registration_contract else 'identityless SCIM-managed internal creation; no invitation/SSO/public/email activation proof')}
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
        assert not original_failures, '; '.join(original_failures)
        if client_lifecycle_contract:
            report['internal_queue_native_switchback'] = await_replay(pending_replay_actor)
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
    parser.add_argument('--store-fixture-binary',type=Path)
    parser.add_argument('--store-fixture-receipt',type=Path)
    parser.add_argument('--store-fixture-path')
    parser.add_argument('--access-switchback', action='store_true')
    parser.add_argument('--login-contract', action='store_true')
    parser.add_argument('--activation-contract', action='store_true')
    parser.add_argument('--registration-contract', action='store_true')
    parser.add_argument('--saml-registration-contract', action='store_true')
    parser.add_argument('--public-registration-contract', action='store_true')
    parser.add_argument('--password-reset-contract', action='store_true')
    parser.add_argument('--password-change-contract', action='store_true')
    parser.add_argument('--ses-feedback-contract', action='store_true')
    parser.add_argument('--properties-contract', action='store_true')
    parser.add_argument('--client-update-contract', action='store_true')
    parser.add_argument('--client-registration-contract', action='store_true')
    parser.add_argument('--client-lifecycle-contract', action='store_true')
    parser.add_argument('--mls-upload-originals', action='store_true')
    parser.add_argument('--stomp-internal-events', action='store_true')
    parser.add_argument('--service-deletion-queue', action='store_true')
    args = parser.parse_args()
    if (args.registration_contract or args.saml_registration_contract) and (args.activation_contract or args.access_switchback or args.login_contract):
        parser.error('registration observation runs separately from credential/activation observations')
    if args.public_registration_contract and any((args.registration_contract,args.saml_registration_contract,args.activation_contract,args.access_switchback,args.login_contract)):
        parser.error('public registration observation runs separately')
    if args.password_reset_contract and any((args.registration_contract,args.saml_registration_contract,args.activation_contract,args.access_switchback,args.login_contract,args.public_registration_contract)):
        parser.error('password reset observation runs separately')
    if args.password_change_contract and any((args.registration_contract,args.saml_registration_contract,args.activation_contract,args.access_switchback,args.login_contract,args.public_registration_contract,args.password_reset_contract)):
        parser.error('password change observation runs separately')
    if args.ses_feedback_contract and any((args.registration_contract,args.saml_registration_contract,args.activation_contract,args.access_switchback,args.login_contract,args.public_registration_contract,args.password_reset_contract,args.password_change_contract)):
        parser.error('SES feedback observation runs separately')
    if args.properties_contract and any((args.registration_contract,args.saml_registration_contract,args.activation_contract,args.access_switchback,args.login_contract,args.public_registration_contract,args.password_reset_contract,args.password_change_contract,args.ses_feedback_contract)):
        parser.error('properties observation runs separately')
    if args.client_update_contract and any((args.registration_contract,args.saml_registration_contract,args.activation_contract,args.access_switchback,args.login_contract,args.public_registration_contract,args.password_reset_contract,args.password_change_contract,args.ses_feedback_contract,args.properties_contract)):
        parser.error('client update observation runs separately')
    if args.client_registration_contract and any((args.registration_contract,args.saml_registration_contract,args.activation_contract,args.access_switchback,args.login_contract,args.public_registration_contract,args.password_reset_contract,args.password_change_contract,args.ses_feedback_contract,args.properties_contract,args.client_update_contract)):
        parser.error('client registration observation runs separately')
    if args.client_lifecycle_contract and any((args.registration_contract,args.saml_registration_contract,args.activation_contract,args.access_switchback,args.login_contract,args.public_registration_contract,args.password_reset_contract,args.password_change_contract,args.ses_feedback_contract,args.properties_contract,args.client_update_contract,args.client_registration_contract)):
        parser.error('client lifecycle observation runs separately')
    if args.mls_upload_originals and not args.client_lifecycle_contract:
        parser.error('MLS upload originals require client lifecycle observation')
    if args.stomp_internal_events and not args.client_lifecycle_contract:
        parser.error('STOMP qualification requires durable client/queue lifecycle observation')
    if args.service_deletion_queue and (not args.stomp_internal_events or not (args.store_fixture_path if args.in_container else args.store_fixture_binary)):
        parser.error('service deletion queue proof requires STOMP and identified tagged fixture')
    if args.in_container:
        return inside(args.gundeck_path, args.access_switchback, args.login_contract, args.activation_contract, args.registration_contract, args.saml_registration_contract, args.public_registration_contract, args.password_reset_contract, args.password_change_contract, args.ses_feedback_contract, args.properties_contract, args.client_update_contract, args.client_registration_contract, args.client_lifecycle_contract, args.store_fixture_path, args.mls_upload_originals, args.stomp_internal_events, args.service_deletion_queue)
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
    if args.saml_registration_contract:
        runner['command'].append('--saml-registration-contract')
    if args.public_registration_contract:
        runner['command'].append('--public-registration-contract')
    if args.password_reset_contract:
        runner['command'].append('--password-reset-contract')
    if args.password_change_contract:
        runner['command'].append('--password-change-contract')
    if args.ses_feedback_contract:
        runner['command'].append('--ses-feedback-contract')
    if args.properties_contract:
        runner['command'].append('--properties-contract')
    if args.client_update_contract:
        runner['command'].append('--client-update-contract')
    if args.client_registration_contract:
        runner['command'].append('--client-registration-contract')
    if args.client_lifecycle_contract:
        runner['command'].append('--client-lifecycle-contract')
    if args.mls_upload_originals:
        runner['command'].append('--mls-upload-originals')
    if args.service_deletion_queue:
        runner['command'].append('--service-deletion-queue')
    if args.stomp_internal_events:
        import stomp_broker
        stomp_broker.prepare(spec, output)
        assert checked(['docker', 'image', 'inspect', stomp_broker.IMAGE, '--format', '{{.Id}}']) == stomp_broker.IMAGE_ID
        runner['command'].append('--stomp-internal-events')
    if args.gundeck_binary:
        runner['command'] += ['--gundeck-path', '/candidate/gundeck']
        runner['volumes'].append({'type': 'bind', 'source': str(args.gundeck_binary.resolve(strict=True)),
                                  'target': '/candidate/gundeck', 'read_only': True})
    runner['volumes'].append({'type': 'bind', 'source': str(args.binary.resolve(strict=True)),
                              'target': '/candidate/brig', 'read_only': True})
    if bool(args.store_fixture_binary)!=bool(args.store_fixture_receipt):
        raise ValueError('identified store fixture binary and receipt required together')
    if args.store_fixture_binary:
        if not args.client_lifecycle_contract:raise ValueError('MLS store fixture requires client lifecycle observation')
        fixture=environment.identified_candidate(args.store_fixture_binary,args.store_fixture_receipt)
        assert json.loads(args.store_fixture_receipt.read_text())['source_revision']==revision, 'current store fixture required'
        fixture_copy=output/'fixtures'/'brig-stores.test'
        shutil.copyfile(args.store_fixture_binary.resolve(strict=True),fixture_copy)
        fixture_copy.chmod(0o555)
        assert hashlib.sha256(fixture_copy.read_bytes()).hexdigest()==fixture['sha256'], 'copied store fixture identity required'
        runner['command']+=['--store-fixture-path','/fixtures/brig-stores.test']
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
        if args.stomp_internal_events:
            checked(command + ['up', '-d', 'activemq'], timeout=90)
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
                                     stdout=log, stderr=subprocess.STDOUT, timeout=1500 if args.ses_feedback_contract or args.properties_contract or args.client_registration_contract or args.mls_upload_originals else 900)
        result['runner_exit_code'] = process.returncode
        report = json.loads((output / 'reports/paired-reads.json').read_text())
        result['e2e_executed'] = report.get('e2e_executed', False)
        if process.returncode == 0 and report['result'] == 'pass':
            result['result'] = 'pass'
    except Exception as error:
        result['failure'] = str(error)
    finally:
        if args.stomp_internal_events:
            try:
                captured = subprocess.run(command + ['logs', 'activemq'], capture_output=True, text=True, timeout=30)
                (output / 'activemq.log').write_text(captured.stdout+captured.stderr)
                result['broker_log_capture_exit_code'] = captured.returncode
            except subprocess.TimeoutExpired:
                result['broker_log_capture_failed'] = True
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
