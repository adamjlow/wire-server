"""Finite actual native/Go startup and HTTP comparison in an owned namespace."""
import contextlib
import gzip
import re
import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import threading
import time

sys.path.insert(0,'/opt/wire-e2e')
import runtime


def request(method,path,body=None,headers=None):
    connection=http.client.HTTPConnection('127.0.0.1',8088,timeout=5)
    try:
        connection.request(method,path,body=body,headers={'Request-Id':'spar-early-synthetic','Content-Type':'application/json',**(headers or {})})
        response=connection.getresponse();body=response.read(1024*1024+1)
        if len(body)>1024*1024:raise ValueError('unexpected large reference response')
        text=body.decode('utf-8')
        try:value=json.loads(text)
        except json.JSONDecodeError:value=text
        headers={name.lower():response.getheader(name) for name in ('Content-Type','Cache-Control','Pragma','Request-Id','Allow')}
        return {'status':response.status,'headers':headers,'body':value}
    finally:connection.close()


def metric_counts(reply):
    counts={}
    for line in reply['body'].splitlines():
        match=re.fullmatch(r'http_request_duration_seconds_count\{(.*)\} ([0-9.eE+-]+)',line)
        if match:
            labels=dict(re.findall(r'(\w+)="([^"]*)"',match[1]))
            # Startup health polling can issue a different number of requests.
            if labels.get('handler')=='/i/status' and labels.get('method')=='GET':continue
            key=tuple(labels.get(k) for k in ('handler','method','status_code'))
            counts[str(key)]=float(match[2])
    return counts


def observe(binary,config,name):
    with Path('/reports/'+name+'.log').open('w') as log:
        process=subprocess.Popen([binary,'-c',str(config)],stdout=log,stderr=subprocess.STDOUT)
        try:
            deadline=time.monotonic()+45
            while True:
                if process.poll()is not None:raise ValueError(name+' exited before health')
                try:
                    if request('GET','/i/status')['status']==200:break
                except (OSError,http.client.HTTPException):pass
                if time.monotonic()>deadline:raise ValueError(name+' health deadline exceeded')
                time.sleep(.1)
            replies={method+' '+path:request(method,path) for method,path in [('GET','/i/status'),('HEAD','/i/status'),('POST','/i/status'),('GET','/v0/i/status'),('GET','/v99/i/status'),('GET','/unknown')]}
            # Additional persistent handlers, using the same native database.
            for method,path,body in [('GET','/sso/settings',None),('HEAD','/sso/settings',None),('PUT','/i/sso/settings','{"default_sso_code":null}'),('PUT','/i/sso/settings','{"default_sso_code":"00000000-0000-4000-8000-000000000001"}'),('POST','/i/scim/userinfo/00000000-0000-4000-8000-000000000002',None)]:
                replies[method+' '+path+(' '+body if body else '')]=request(method,path,body)
            metadata=['ServiceProviderConfig','Schemas','ResourceTypes']+['Schemas/urn:ietf:params:scim:schemas:core:2.0:'+name for name in ('User','ServiceProviderConfig','Group','Schema','ResourceType')]+['Schemas/urn:wire:scim:schemas:profile:1.0','Schemas/urn:ietf:params:scim:schemas:extension:wire:1.0:User','Schemas/unknown']
            for path in metadata:
                for method in ('GET','HEAD'):
                    key=method+' /scim/v2/'+path
                    replies[key]=request(method,'/scim/v2/'+path)
            middleware={}
            if '--middleware' in sys.argv:
                valid=gzip.compress(b'{"default_sso_code":null}',mtime=0)
                tests=[('gzip-settings','PUT','/i/sso/settings',valid,'gzip'),
                       ('gzip-upper-settings','PUT','/i/sso/settings',valid,'GZIP'),
                       ('gzip-invalid-health','GET','/i/status',b'invalid gzip','gzip'),
                       ('gzip-invalid-unknown','GET','/unknown',b'invalid gzip','gzip'),
                       ('gzip-invalid-settings','PUT','/i/sso/settings',b'invalid gzip','gzip'),
                       ('gzip-truncated-settings','PUT','/i/sso/settings',valid[:-5],'gzip')]
                for key,method,path,body,encoding in tests:
                    middleware[key]=request(method,path,body,{'Content-Encoding':encoding})
                middleware['gzip-settings-readback']=request('GET','/sso/settings')
                for method in ('GET','POST','HEAD'):
                    key='metrics-'+method
                    middleware[key]=request(method,'/i/metrics',b'invalid gzip',{'Content-Encoding':'gzip'})
            begin=time.monotonic();process.send_signal(signal.SIGTERM);code=process.wait(timeout=35)
            if code!=0:raise ValueError(name+' failed clean SIGTERM')
            return {'responses':replies,'middleware':middleware,'stop_exit_code':code,'stop_seconds':time.monotonic()-begin,'binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest()}
        finally:
            if process.poll()is None:
                process.kill();process.wait(timeout=10)


def main():
    if os.getuid()!=65534:raise ValueError('nonroot namespace required')
    output={'kind':'early-process-comparison','result':'running','e2e_executed':False,'scope':'Configuration, real schema23 startup, health/method/version/errors, default-settings empty/clear/missing IdP, missing SCIM user metadata, public config/resources/five schemas/unknown and advertised extension GET/HEAD, SIGTERM; no complete SSO/SCIM qualification.'}
    forward=None
    try:
        forward=runtime.TCPServer(('127.0.0.1',9042),runtime.Forward);forward.destination=('cassandra',9042)
        thread=threading.Thread(target=forward.serve_forever,daemon=True);thread.start()
        deadline=time.monotonic()+120
        while True:
            try:
                with socket.create_connection(('cassandra',9042),timeout=1):break
            except OSError:
                if time.monotonic()>deadline:raise ValueError('Cassandra startup deadline exceeded')
                time.sleep(.5)
        with Path('/reports/schema.log').open('w') as log:
            subprocess.run(['spar-schema','--keyspace','spar_test','--replication-factor','1','--reset'],stdout=log,stderr=subprocess.STDOUT,timeout=90,check=True)
        config=Path('/tmp/spar-early.json')
        cfg=json.loads(Path('/fixtures/peers.json').read_text())['0']['spar'];config.write_text(json.dumps(cfg))
        native=str(Path(shutil.which('spar')).resolve(strict=True));candidate='/candidate/spar'
        output['config_sha256']=hashlib.sha256(config.read_bytes()).hexdigest()
        output['native']=observe(native,config,'native')
        output['go']=observe(candidate,config,'go')
        output['differences']={key:{'native':value,'go':output['go']['responses'][key]} for key,value in output['native']['responses'].items() if value!=output['go']['responses'][key]}
        if '--middleware' in sys.argv:
            native=output['native']['middleware'];go=output['go']['middleware'];differences={}
            # Valid decompression, lazy unread bodies, method/header behavior and
            # persisted state are exact comparisons. Parser exception text is
            # runtime-specific; error code/label/headers remain part of the check.
            for key,nreply in native.items():
                greply=go[key]
                if key.startswith('metrics-'):
                    nproject={k:v for k,v in nreply.items() if k!='body'}
                    gproject={k:v for k,v in greply.items() if k!='body'}
                    if key!='metrics-HEAD':
                        nproject['counts']=metric_counts(nreply);gproject['counts']=metric_counts(greply)
                        nproject['metric_names']=sorted(re.findall(r'^# TYPE (\S+) \S+',nreply['body'],re.M))
                        gproject['metric_names']=sorted(re.findall(r'^# TYPE (\S+) \S+',greply['body'],re.M))
                elif key in ('gzip-invalid-settings','gzip-truncated-settings'):
                    nproject={**nreply,'body':{k:nreply['body'].get(k) for k in ('code','label')} if isinstance(nreply['body'],dict) else nreply['body']}
                    gproject={**greply,'body':{k:greply['body'].get(k) for k in ('code','label')} if isinstance(greply['body'],dict) else greply['body']}
                else:nproject=nreply;gproject=greply
                if nproject!=gproject:differences[key]={'native':nproject,'go':gproject}
            output['middleware_differences']=differences
            output['scope']='Original33 actual process HTTP replies plus declared6 gzip/readback and3 metrics probes; exact response/state/header checks, error code/label (incidental parser text excluded), normalized histogram count/name checks (latency/GC values are runtime-specific and startup health poll count excluded). Not original e2e or full operations qualification.'
        output['result']='pass' if not output['differences'] and not output.get('middleware_differences') else 'fail' 
        return 0 if output['result']=='pass' else 1
    except Exception as error:
        output.update(result='fail',error_category=type(error).__name__,error=str(error)[:300]);return 1
    finally:
        if forward is not None:forward.shutdown();forward.server_close()
        Path('/reports/result.json').write_text(json.dumps(output,indent=2)+'\n')

if __name__=='__main__':raise SystemExit(main())
