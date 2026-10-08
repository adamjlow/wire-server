"""Real gundeck against bounded loopback SNS/SQS synthetic protocol providers."""
import argparse
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading
import time
import urllib.parse
import uuid
from xml.sax.saxutils import escape

USER = '88888888-8888-4888-8888-888888888888'
OTHER = '99999999-9999-4999-8999-999999999999'


class Providers:
    def __init__(self):
        self.endpoints = {}
        self.calls = []
        self.messages = []
        self.deleted = []
        self.publish_error = None
        self.counter = 0
        self.signed = True
        self.lock = threading.RLock()

    def handler(self, service):
        state = self
        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_POST(self):
                length = int(self.headers.get('Content-Length', '0'))
                if not 0 <= length <= 65536:
                    self.send_error(413); return
                raw = self.rfile.read(length)
                params = {k: v[-1] for k, v in urllib.parse.parse_qs(raw.decode(), keep_blank_values=True).items()}
                action = params.get('Action')
                if not action:
                    self.send_error(400); return
                with state.lock:
                    state.signed &= self.headers.get('Authorization', '').startswith('AWS4-HMAC-SHA256 ')
                    status, content = state.action(service, action, params)
                namespace = 'http://sns.amazonaws.com/doc/2010-03-31/' if service == 'sns' else 'http://queue.amazonaws.com/doc/2012-11-05/'
                if status == 200:
                    result = f'<{action}Response xmlns="{namespace}"><{action}Result>{content}</{action}Result><ResponseMetadata><RequestId>synthetic</RequestId></ResponseMetadata></{action}Response>'
                else:
                    code, message = content
                    result = f'<ErrorResponse xmlns="{namespace}"><Error><Type>Sender</Type><Code>{escape(code)}</Code><Message>{escape(message)}</Message></Error><RequestId>synthetic</RequestId></ErrorResponse>'
                body = result.encode()
                self.send_response(status);self.send_header('Content-Type', 'text/xml');self.send_header('Content-Length', str(len(body)));self.end_headers();self.wfile.write(body)
        return Handler

    def action(self, service, action, p):
        if service == 'sqs':
            if action == 'GetQueueUrl': return 200, '<QueueUrl>http://127.0.0.1:16188/queue</QueueUrl>'
            if action == 'ReceiveMessage':
                if p.get('MaxNumberOfMessages') != '10' or p.get('WaitTimeSeconds') != '20':
                    raise ValueError('SQS receive options differ')
                if not self.messages: return 200, ''
                entry = self.messages[0];entry['receives'] += 1
                return 200, '<Message><MessageId>synthetic</MessageId><ReceiptHandle>'+entry['receipt']+'</ReceiptHandle><MD5OfBody>'+hashlib.md5(entry['body'].encode()).hexdigest()+'</MD5OfBody><Body>'+escape(entry['body'])+'</Body></Message>'
            if action == 'DeleteMessage':
                receipt = p['ReceiptHandle'];self.deleted.append(receipt);self.messages = [m for m in self.messages if m['receipt'] != receipt]
                return 200, ''
            raise ValueError('unexpected synthetic SQS action')
        self.calls.append({'action': action, 'params': {k:v for k,v in p.items() if k not in ('Action','Version')}})
        if action == 'CreatePlatformEndpoint':
            token = p['Token']
            if token == 'invalid':return 400, ('InvalidParameter','Invalid parameter: Token')
            if token == 'too-long':return 400, ('InvalidParameter','Invalid parameter: Token Reason: must be at most 8192 bytes long in UTF-8 encoding')
            if '/integration-unknown' in p['PlatformApplicationArn']:return 404, ('NotFound','Application does not exist')
            existing = next((a for a,e in self.endpoints.items() if e['Token'] == token), None)
            if existing:
                return 400, ('InvalidParameter', 'Invalid parameter: Token Reason: Endpoint '+existing+' already exists with the same Token, but different attributes.')
            self.counter += 1
            app = p['PlatformApplicationArn'].split(':app/')[1]
            arn = 'arn:aws:sns:eu-west-1:123456789012:endpoint/'+app+'/'+str(uuid.UUID(int=self.counter))
            self.endpoints[arn] = {'Token':token,'Enabled':'true','CustomUserData':p.get('CustomUserData','')}
            return 200, '<EndpointArn>'+arn+'</EndpointArn>'
        arn = p.get('EndpointArn', p.get('TargetArn'))
        if action == 'GetEndpointAttributes':
            if arn not in self.endpoints:return 404, ('NotFound','Endpoint not found')
            return 200, '<Attributes>'+''.join('<entry><key>'+escape(k)+'</key><value>'+escape(v)+'</value></entry>' for k,v in sorted(self.endpoints[arn].items()))+'</Attributes>'
        if action == 'SetEndpointAttributes':
            if arn not in self.endpoints:return 404, ('NotFound','Endpoint not found')
            i=1
            while f'Attributes.entry.{i}.key' in p:
                self.endpoints[arn][p[f'Attributes.entry.{i}.key']] = p[f'Attributes.entry.{i}.value'];i+=1
            return 200, ''
        if action == 'DeleteEndpoint':self.endpoints.pop(arn,None);return 200, ''
        if action == 'Publish':
            if self.publish_error:return self.publish_error
            return 200, '<MessageId>synthetic-message</MessageId>'
        raise ValueError('unexpected synthetic SNS action')


def run(binary):
    if os.environ.get('WIRE_E2E_DISPOSABLE') != '1':raise ValueError('requires private owned environment')
    state=Providers();records=[];servers=[];process=None
    config=json.loads(Path('/fixtures/peers.json').read_text())['0']['gundeck'];config['gundeck']['port']=16186
    config['aws']['snsEndpoint']='http://127.0.0.1:16187';config['aws']['sqsEndpoint']='http://127.0.0.1:16188';config['aws']['queueName']='synthetic-owned-feedback'
    config['settings']['sqsThrottleMillis']=50;config['settings']['consumableNotifications']=False;config['settings']['cellsEventQueue']=None;config['logLevel']='Warn'
    def req(method,path,body=None,user=USER):
        conn=http.client.HTTPConnection('127.0.0.1',16186,timeout=5)
        try:
            conn.request(method,path,None if body is None else json.dumps(body),{'Z-User':user,'Z-Connection':'fixture','Content-Type':'application/json','Request-Id':'gundeck-native-fixture'})
            response=conn.getresponse();raw=response.read(65537)
            if len(raw)>65536:raise ValueError('fixture response exceeded bound')
            try:value=json.loads(raw)
            except (ValueError,UnicodeDecodeError):value=raw.decode()
            return {'status':response.status,'content_type':response.getheader('Content-Type'),'body':value}
        finally:conn.close()
    def record(name,value):
        records.append({'name':name,'observed':value})
        Path('/reports/native-capture-progress.json').write_text(json.dumps({'records':records,'provider_calls':state.calls},indent=2)+'\n')
    def token(value,app='test',transport='GCM'):return {'transport':transport,'app':app,'token':value,'client':'abcd'}
    def wait(check):
        deadline=time.monotonic()+10
        while not check():
            if process.poll() is not None or time.monotonic()>deadline:raise ValueError('bounded synthetic effect missing')
            time.sleep(.05)
    try:
        for name,port in [('sns',16187),('sqs',16188)]:
            server=http.server.ThreadingHTTPServer(('127.0.0.1',port),state.handler(name));servers.append(server);threading.Thread(target=server.serve_forever,daemon=True).start()
        with tempfile.TemporaryDirectory(prefix='gundeck-native-') as temp,open('/reports/gundeck-native-side.log','w') as log:
            conf=Path(temp)/'config.json';conf.write_text(json.dumps(config));process=subprocess.Popen([str(binary),'-c',str(conf)],cwd='/tmp/wire-e2e/services/gundeck',stdout=log,stderr=log)
            deadline=time.monotonic()+30
            while True:
                if process.poll() is not None:raise ValueError('side gundeck startup failed')
                try:
                    if req('GET','/i/status')['status']==200:break
                except OSError:pass
                if time.monotonic()>deadline:raise ValueError('side gundeck readiness failed')
                time.sleep(.1)
            for name,t in [('invalid-token',token('invalid')),('long-token',token('too-long')),('unknown-app',token('synthetic','unknown')),('voip-rejected',token('synthetic',transport='APNS_VOIP_SANDBOX'))]:record(name,req('POST','/v9/push/tokens',t))
            record('register-gcm',req('POST','/v9/push/tokens',token('synthetic-gcm')))
            record('register-shared-gcm',req('POST','/v9/push/tokens',token('synthetic-gcm'),OTHER))
            record('list-shared-gcm',req('GET','/v9/push/tokens',user=OTHER))
            old_arn=next(a for a,e in state.endpoints.items() if e['Token']=='synthetic-gcm')
            record('replace-shared-gcm',req('POST','/v9/push/tokens',token('synthetic-replacement')))
            record('shared-replacement-migrates-other',req('GET','/v9/push/tokens',user=OTHER))
            record('shared-old-endpoint-deleted',old_arn not in state.endpoints)
            arn=next(a for a,e in state.endpoints.items() if e['Token']=='synthetic-replacement')
            record('shared-owner-metadata',sorted(state.endpoints[arn]['CustomUserData'].split(':')))
            push={'recipients':[{'user_id':USER,'route':'any','clients':[]}],'payload':[{'type':'synthetic'}]}
            before=len(state.calls);record('native-push-accepted',req('POST','/i/push/v2',[push]));wait(lambda:any(c['action']=='Publish' for c in state.calls[before:]))
            publish=next(c['params'] for c in state.calls[before:] if c['action']=='Publish')
            envelope=json.loads(publish['Message']);inner=json.loads(envelope['GCM']);nid=inner['data']['data']['id']
            if uuid.UUID(nid).version!=1 or inner['data']['user']!=USER:raise ValueError('native notice identity differs')
            inner['data']['data']['id']='<validated-v1-notification-id>'
            record('gcm-publish',{'message':inner,'structure':publish['MessageStructure'],'attribute_keys':sorted(k for k in publish if k.startswith('MessageAttributes.'))})
            before=len(state.calls);low=dict(push,native_priority='low');record('low-priority-push-accepted',req('POST','/i/push/v2',[low]));wait(lambda:any(c['action']=='Publish' for c in state.calls[before:]))
            publication=next(c['params'] for c in state.calls[before:] if c['action']=='Publish');low_inner=json.loads(json.loads(publication['Message'])['GCM'])
            if uuid.UUID(low_inner['data']['data']['id']).version!=1:raise ValueError('low notice identity differs')
            low_inner['data']['data']['id']='<validated-v1-notification-id>';record('gcm-low-priority-publish',low_inner)
            state.messages.append({'receipt':'ttl-expired','receives':0,'body':json.dumps({'EndpointArn':arn,'EventType':'DeliveryFailure','FailureType':'NotificationTTLExpired'})})
            wait(lambda:'ttl-expired' in state.deleted);record('ttl-feedback-keeps-token',req('GET','/v9/push/tokens'))
            state.messages.append({'receipt':'invalid-token','receives':0,'body':json.dumps({'EndpointArn':arn,'EventType':'DeliveryFailure','FailureType':'InvalidPlatformToken'})})
            wait(lambda:'invalid-token' in state.deleted);record('invalid-feedback-removes-token',req('GET','/v9/push/tokens'));record('shared-invalid-feedback-removes-token',req('GET','/v9/push/tokens',user=OTHER));record('feedback-deleted-receipts',sorted(state.deleted));record('feedback-endpoint-removed',arn not in state.endpoints)
            record('register-apns',req('POST','/v9/push/tokens',token('synthetic-apns',transport='APNS_SANDBOX')))
            before=len(state.calls);record('apns-push-accepted',req('POST','/i/push/v2',[push]));wait(lambda:any(c['action']=='Publish' for c in state.calls[before:]))
            publish=next(c['params'] for c in state.calls[before:] if c['action']=='Publish');inner=json.loads(json.loads(publish['Message'])['APNS_SANDBOX']);nid=inner['data']['data']['id']
            if uuid.UUID(nid).version!=1:raise ValueError('APNS notice identity differs')
            inner['data']['data']['id']='<validated-v1-notification-id>';record('apns-publish',inner)
            state.publish_error=(400,('EndpointDisabled','Endpoint is disabled'))
            record('disabled-push-accepted',req('POST','/i/push/v2',[push]));wait(lambda:not state.endpoints)
            record('disabled-push-removes-token',req('GET','/v9/push/tokens'))
            record('disabled-push-token-delete-is-absent',req('DELETE','/v9/push/tokens/synthetic-apns'));record('native-endpoints-cleaned',not state.endpoints)
            record('user-cleanup',req('DELETE','/i/user'));record('other-user-cleanup',req('DELETE','/i/user',user=OTHER));record('signed-provider-requests',state.signed)
            from capture_operations import run as capture_operations
            records.extend(capture_operations(16186,binary,config,process.pid))
            started=time.monotonic();process.terminate();process.wait(timeout=4);record('idle-shutdown',{'exit_code':process.returncode,'seconds':time.monotonic()-started,'bound_seconds':4})
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try:process.wait(timeout=4)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=4)
        for server in servers:server.shutdown();server.server_close()
    return {'schema_version':1,'kind':'finite-native-feedback-characterization','binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'script_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'config_sha256':hashlib.sha256(json.dumps(config).encode()).hexdigest(),'records':records,'provider_calls':state.calls,'cleanup':'side process reaped; loopback providers stopped; synthetic token/endpoints/own-user notifications cleaned'}


if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--binary',type=Path);args=parser.parse_args();binary=(args.binary or Path(shutil.which('gundeck'))).resolve(strict=True)
    print(json.dumps(run(binary),indent=2))
