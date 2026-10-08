"""One real Redis/Cassandra/SNS/RabbitMQ switchback and bounded loaded shutdown."""
import base64
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import shutil
import ssl
import subprocess
import tempfile
import threading
import time
import urllib.parse
import urllib.request

from capture_native import Providers as State
from capture_http_presence import redis_command

USER='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
LOADED='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'
QUEUE='user-notifications.'+USER+'.abcd'
PORT=16186


def run(binary):
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('private owned runner required')
    state=State();servers=[];records=[];process=None
    modern=False;entered=threading.Event();release=threading.Event()
    config=json.loads(Path('/fixtures/peers.json').read_text())['0']['gundeck']
    config['gundeck']['port']=PORT;config['brig']={'host':'127.0.0.1','port':16189}
    config['aws']['snsEndpoint']='http://127.0.0.1:16187';config['aws']['sqsEndpoint']='http://127.0.0.1:16188'
    config['settings']['consumableNotifications']=True
    class Brig(http.server.BaseHTTPRequestHandler):
        def log_message(self,*unused):pass
        def do_POST(self):
            length=int(self.headers.get('Content-Length','0'))
            if length>65536:self.send_error(413);return
            body=self.rfile.read(length)
            if self.path=='/i/clients/full':
                users=json.loads(body)['users']
                if any(user not in (USER,LOADED) for user in users):raise ValueError('unowned fixture user')
                response={user:[{'id':'abcd','type':'permanent','time':'2026-10-08T00:00:00Z','capabilities':['consumable-notifications'] if modern else [],'mls_public_keys':{}}] for user in users}
            else:response={}
            raw=json.dumps(response).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
    class Cannon(http.server.BaseHTTPRequestHandler):
        def log_message(self,*unused):pass
        def do_POST(self):
            length=int(self.headers.get('Content-Length','0'))
            if length>65536:self.send_error(413);return
            body=json.loads(self.rfile.read(length));entered.set()
            if not release.wait(3):raise ValueError('loaded fixture was not released')
            result={'bulkpush_resp':[{'notif_id':item['notification']['id'],'target':target,'status':'push_status_ok'} for item in body['bulkpush_req'] for target in item['targets']]}
            raw=json.dumps(result).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
    def request(method,path,body=None,user=USER):
        connection=http.client.HTTPConnection('127.0.0.1',PORT,timeout=5)
        try:
            connection.request(method,path,None if body is None else json.dumps(body),{'Z-User':user,'Z-Connection':'switchback','Content-Type':'application/json'})
            response=connection.getresponse();raw=response.read(65537)
            if len(raw)>65536:raise ValueError('switchback response exceeds bound')
            try:value=json.loads(raw)
            except ValueError:value=raw.decode()
            return response.status,value
        finally:connection.close()
    def check(name,passed):
        records.append({'name':name,'pass':bool(passed)})
        Path('/reports/switchback-progress.json').write_text(json.dumps({'records':records},indent=2)+'\n')
        if not passed:raise ValueError('switchback failed: '+name)
    def notice(label,user=USER):return {'recipients':[{'user_id':user,'route':'direct','clients':[]}],'origin':user,'origin_connection':'switchback','payload':[{'type':'synthetic.switchback','phase':label}]}
    def presence(user=USER,connection='switchback'):return {'user_id':user,'device_id':connection,'client_id':'abcd','resource':'http://127.0.0.1:16190/i/push/'+user+'/'+connection}
    def page(user=USER):
        code,value=request('GET','/v9/notifications',user=user)
        if code!=200:raise ValueError('switchback notification lookup failed')
        return value['notifications']
    ca='/source/deploy/dockerephemeral/rabbitmq-config/certificates/ca.pem'
    tls=ssl.create_default_context(cafile=ca)
    auth=base64.b64encode((os.environ['RABBITMQ_USERNAME']+':'+os.environ['RABBITMQ_PASSWORD']).encode()).decode()
    management='https://localhost:15671/api/queues/'+urllib.parse.quote(config['rabbitmq']['vHost'],safe='')+'/'+urllib.parse.quote(QUEUE,safe='')
    def rabbit(method,suffix='',body=None):
        req=urllib.request.Request(management+suffix,data=None if body is None else json.dumps(body).encode(),method=method,headers={'Authorization':'Basic '+auth,'Content-Type':'application/json'})
        with urllib.request.urlopen(req,context=tls,timeout=5) as response:
            raw=response.read(65537)
            if len(raw)>65536:raise ValueError('broker fixture response exceeds bound')
            return None if not raw else json.loads(raw)
    def queue_message():
        deadline=time.monotonic()+5
        while True:
            messages=rabbit('POST','/get',{'count':1,'ackmode':'ack_requeue_false','encoding':'auto','truncate':65536})
            if messages:return messages[0]
            if time.monotonic()>deadline:raise ValueError('persisted queue notification absent')
            time.sleep(.05)
    def start(executable,label,path,log):
        nonlocal process
        process=subprocess.Popen([str(executable),'-c',str(path)],cwd='/tmp/wire-e2e/services/gundeck',stdout=log,stderr=log)
        deadline=time.monotonic()+30
        while True:
            if process.poll() is not None:raise ValueError(label+' startup exited')
            try:
                if request('GET','/i/status')[0]==200:return
            except OSError:pass
            if time.monotonic()>deadline:raise ValueError(label+' startup timed out')
            time.sleep(.1)
    def stop(label):
        nonlocal process
        started=time.monotonic();process.terminate();process.wait(timeout=4)
        records.append({'name':label+'-shutdown','exit_code':process.returncode,'seconds':time.monotonic()-started})
        if label.startswith('go') and process.returncode!=0:raise ValueError('Go switchback shutdown failed')
        process=None
    try:
        for name,port in [('sns',16187),('sqs',16188)]:
            server=http.server.ThreadingHTTPServer(('127.0.0.1',port),state.handler(name));servers.append(server);threading.Thread(target=server.serve_forever,daemon=True).start()
        for handler,port in [(Brig,16189),(Cannon,16190)]:
            server=http.server.ThreadingHTTPServer(('127.0.0.1',port),handler);servers.append(server);threading.Thread(target=server.serve_forever,daemon=True).start()
        with tempfile.TemporaryDirectory(prefix='gundeck-switchback-') as temp,Path('/reports/switchback.log').open('w') as log:
            path=Path(temp)/'config.json';path.write_text(json.dumps(config))
            haskell=Path(shutil.which('gundeck')).resolve(strict=True)
            token={'transport':'GCM','app':'test','token':'synthetic-switchback','client':'abcd'}
            start(haskell,'haskell',path,log)
            check('haskell-presence-write',request('POST','/i/presences',presence())[0]==201)
            check('haskell-token-write',request('POST','/v9/push/tokens',token)==(201,token))
            check('haskell-notification-write',request('POST','/i/push/v2',[notice('haskell')])[0]==200)
            original=page();check('haskell-notification-persisted',len(original)==1)
            check('haskell-queue-register',request('POST','/i/users/'+USER+'/clients/abcd/consumable-notifications')[0]==204)
            modern=True
            check('haskell-queue-publish',request('POST','/i/push/v2',[notice('haskell-queue')])[0]==200)
            message=queue_message();check('haskell-queue-body-properties',json.loads(message['payload'])['payload']==notice('haskell-queue')['payload'] and message['properties']['delivery_mode']==2 and message['properties']['expiration']==str(config['settings']['notificationTTL']*1000))
            stop('haskell-before-go')
            modern=False
            start(binary,'go',path,log)
            check('go-reads-haskell-notifications',page()==original)
            code,values=request('GET','/i/presences/'+USER);check('go-reads-haskell-presence',code==200 and len(values)==1 and values[0]['client_id']=='abcd')
            check('go-reads-haskell-token',request('GET','/v9/push/tokens')==(200,{'tokens':[token]}))
            check('go-notification-write',request('POST','/i/push/v2',[notice('go')])[0]==200)
            check('go-token-upsert',request('POST','/v9/push/tokens',token)==(201,token))
            check('go-presence-upsert',request('POST','/i/presences',presence())[0]==201)
            expected=page();check('go-notifications-preserved',len(expected)==2 and expected[0]==original[0])
            modern=True
            check('go-register-existing-queue',request('POST','/i/users/'+USER+'/clients/abcd/consumable-notifications')[0]==204)
            check('go-queue-publish',request('POST','/i/push/v2',[notice('go-queue')])[0]==200)
            message=queue_message();check('go-queue-body-properties',json.loads(message['payload'])['payload']==notice('go-queue')['payload'] and message['properties']['delivery_mode']==2 and message['properties']['expiration']==str(config['settings']['notificationTTL']*1000))
            modern=False
            check('loaded-presence-write',request('POST','/i/presences',presence(LOADED,'loaded'),LOADED)[0]==201)
            push=notice('loaded',LOADED);push.pop('origin_connection')
            check('loaded-push-accepted',request('POST','/i/push/v2',[push],LOADED)[0]==200)
            check('loaded-delivery-entered',entered.wait(3))
            started=time.monotonic();process.terminate();threading.Timer(.5,release.set).start();process.wait(timeout=4)
            elapsed=time.monotonic()-started;check('go-loaded-shutdown',process.returncode==0 and elapsed<=4);records.append({'name':'go-loaded-shutdown-measurement','seconds':elapsed,'exit_code':process.returncode});process=None
            start(haskell,'haskell-after-go',path,log)
            check('haskell-reads-go-notifications',page()==expected)
            check('haskell-reads-go-token',request('GET','/v9/push/tokens')==(200,{'tokens':[token]}))
            code,values=request('GET','/i/presences/'+USER);check('haskell-reads-go-presence',code==200 and len(values)==1 and values[0]['client_id']=='abcd')
            check('haskell-reads-loaded-notification',len(page(LOADED))==1)
            check('broker-queue-survives-runtimes',rabbit('GET')['durable'] is True)
            for user in (USER,LOADED):check('cleanup-'+user,request('DELETE','/i/user',user=user)[0]==200);redis_command('DEL','user:'+user)
            check('endpoints-cleaned',not state.endpoints)
            rabbit('DELETE');stop('haskell-after-go')
            return {'result':'pass','candidate_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'reference_sha256':hashlib.sha256(haskell.read_bytes()).hexdigest(),'records':records,'cleanup':'owned users/endpoints/Redis keys/queue removed; owned volume cleanup remains fallback'}
    finally:
        release.set()
        if process is not None and process.poll() is None:
            process.terminate()
            try:process.wait(timeout=4)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=4)
        for server in servers:server.shutdown();server.server_close()
