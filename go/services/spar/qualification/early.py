"""Finite actual native/Go startup and HTTP comparison in an owned namespace."""
import contextlib
import gzip
import base64
import urllib.parse
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
        headers={name.lower():response.getheader(name) for name in ('Content-Type','Cache-Control','Pragma','Request-Id','Allow','Set-Cookie')}
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
                middleware['metrics-GET-final']=request('GET','/i/metrics')
            signature_checks={}
            if '--signatures' in sys.argv:
                fixtures=sorted(Path('/reports/signature-fixtures').glob('*.json'))
                if len(fixtures)!=12:raise ValueError('finite signature fixture set incomplete')
                for fixture in fixtures:
                    case=json.loads(fixture.read_text())
                    idp=request('GET','/i/identity-providers/'+case['team'])
                    if idp['status']!=200 or not isinstance(idp['body'],dict) or [p.get('id') for p in idp['body'].get('providers',[])]!=[case['idp']]:raise ValueError('native stored IdP decoding failed')
                    variants={'signed':case['xml']}
                    if case['signature_accepted']:variants['tampered']=case['xml'].replace('synthetic@example.test','attacker@example.test')
                    for variant,xml in variants.items():
                        body=urllib.parse.urlencode({'SAMLResponse':base64.b64encode(xml.encode()).decode()})
                        reply=request('POST','/sso/finalize-login',body,{'Content-Type':'application/x-www-form-urlencoded'})
                        accepted=variant=='signed' and case['signature_accepted']
                        signature_checks[case['name']+'-'+variant]={'reply':reply,'fixture_sha256':hashlib.sha256(fixture.read_bytes()).hexdigest(),'signature_accepted':accepted,'idp_read_status':idp['status']}
                        Path('/reports/'+name+'-signature-replies.json').write_text(json.dumps(signature_checks,indent=2)+'\n')
                        if reply['headers']['set-cookie'] is not None:raise ValueError('qualification response created a session')
                        if accepted:
                            if reply['status']!=500 or not isinstance(reply['body'],dict) or reply['body'].get('label')!='server-error' or 'verdict format' not in reply['body'].get('message',''):raise ValueError('valid signature did not reach missing-request verdict boundary')
                        elif reply['status']!=400 or not isinstance(reply['body'],str) or 'wire:sso:error:bad-response-signature' not in reply['body']:raise ValueError('invalid signature was not rejected at verification')
            begin=time.monotonic();process.send_signal(signal.SIGTERM);code=process.wait(timeout=35)
            if code!=0:raise ValueError(name+' failed clean SIGTERM')
            return {'responses':replies,'middleware':middleware,'signature_checks':signature_checks,'stop_exit_code':code,'stop_seconds':time.monotonic()-begin,'binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest()}
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
        if '--signatures' in sys.argv:
            with Path('/reports/signature-fixtures.log').open('w') as log:
                subprocess.run(['/fixtures/signature-fixtures.test','-test.run=^(TestNativeDigestAndLargeAssertionCompatibility|TestNativeSuccessfulReferenceSelection)$','-test.timeout=90s'],env={**os.environ,'SPAR_SIGNATURE_FIXTURE_CONFIG':str(config)},stdout=log,stderr=subprocess.STDOUT,timeout=95,check=True)
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
                elif key in ('gzip-upper-settings','gzip-invalid-settings','gzip-truncated-settings'):
                    nproject={**nreply,'body':{k:nreply['body'].get(k) for k in ('code','label')} if isinstance(nreply['body'],dict) else {'plain_error':True} if nreply['status']>=400 else nreply['body']}
                    gproject={**greply,'body':{k:greply['body'].get(k) for k in ('code','label')} if isinstance(greply['body'],dict) else {'plain_error':True} if greply['status']>=400 else greply['body']}
                else:nproject=nreply;gproject=greply
                if nproject!=gproject:differences[key]={'native':nproject,'go':gproject}
            output['middleware_differences']=differences
            output['scope']='Original33 actual process HTTP replies plus declared6 gzip/readback and4 metrics probes; exact response/state/header checks, error code/label (incidental parser text excluded), normalized histogram count/name checks (latency/GC values are runtime-specific and startup health poll count excluded). Not original e2e or full operations qualification.'
        if '--signatures' in sys.argv:
            differences={}
            for key,native in output['native']['signature_checks'].items():
                go=output['go']['signature_checks'][key]
                # Authentication HTML diagnostics contain runtime-specific error
                # constructors. Check actual verification boundary/status/header
                # and no-cookie behavior; keep the full raw pages for review.
                def projection(check):
                    reply=check['reply'];body=reply['body']
                    return {'status':reply['status'],'headers':reply['headers'],'signature_accepted':check['signature_accepted'],'idp_read_status':check['idp_read_status'],'label':body.get('label') if isinstance(body,dict) else re.search(r'<title>([^<]+)</title>',body).group(1)}
                if projection(native)!=projection(go):differences[key]={'native':projection(native),'go':projection(go)}
            output['signature_differences']=differences
            output['scope']='Finite actual native/Go signature comparison:12 signed cases plus9 paired tamper controls. Native stored IdP reads, SHA1/256/512/RIPEMD160, RSA-SHA1/256, >1000nodes, failed-extra versus unique successful matching reference and unsupported transform. Accepted signatures must reach the unknown-request JSON500 boundary with no session; bad signatures400. Raw runtime-specific HTML diagnostics retained, not byte-normalized into a full-equality claim. Includes existing33 process replies. No original e2e or full durable switchback/operations claim.'
        output['result']='pass' if not output['differences'] and not output.get('middleware_differences') and not output.get('signature_differences') else 'fail'  
        return 0 if output['result']=='pass' else 1
    except Exception as error:
        output.update(result='fail',error_category=type(error).__name__,error=str(error)[:300]);return 1
    finally:
        if forward is not None:forward.shutdown();forward.server_close()
        Path('/reports/result.json').write_text(json.dumps(output,indent=2)+'\n')

if __name__=='__main__':raise SystemExit(main())
