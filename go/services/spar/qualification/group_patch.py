"""Actual native/Go unsupported Group PATCH boundary in owned Cassandra only."""
import hashlib
import http.client as client
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import threading
import time
import uuid

sys.path.insert(0,'/opt/wire-e2e')
import runtime


def request(port,method,path,body=None,headers=None):
    connection=client.HTTPConnection('127.0.0.1',port,timeout=5)
    try:
        if isinstance(body,dict):body=json.dumps(body)
        connection.request(method,path,body,{'Content-Type':'application/json','Request-Id':'synthetic-group-patch',**(headers or {})})
        response=connection.getresponse();data=response.read(1048577).decode()
        try:data=json.loads(data)
        except json.JSONDecodeError:pass
        return {'status':response.status,'headers':{k.lower():response.getheader(k) for k in ('Content-Type','Cache-Control','Pragma','Set-Cookie','Allow')},'body':data}
    finally:connection.close()


def wait(process,port,path):
    deadline=time.monotonic()+45
    while True:
        if process.poll()is not None:raise ValueError('qualification process exited before readiness')
        try:
            reply=request(port,'GET',path)
            if reply['status']==200:return reply
        except OSError:pass
        if time.monotonic()>deadline:raise ValueError('qualification readiness timeout')
        time.sleep(.1)


def main():
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned nonroot namespace required')
    processes=[];logs=[];forward=None;output={'result':'running','kind':'actual-native-Go-group-PATCH','e2e_executed':False,'services':{}}
    try:
        forward=runtime.TCPServer(('127.0.0.1',9042),runtime.Forward);forward.destination=('cassandra',9042);threading.Thread(target=forward.serve_forever,daemon=True).start()
        deadline=time.monotonic()+120
        while True:
            try:
                with socket.create_connection(('cassandra',9042),timeout=1):break
            except OSError:pass
            if time.monotonic()>deadline:raise ValueError('owned Cassandra not ready')
            time.sleep(.5)
        log=open('/reports/schema.log','w');logs.append(log);subprocess.run(['spar-schema','--keyspace','spar_test','--replication-factor','1','--reset'],stdout=log,stderr=subprocess.STDOUT,timeout=90,check=True)
        config=Path('/tmp/spar-group-patch.json');config.write_text(json.dumps(json.loads(Path('/fixtures/peers.json').read_text())['0']['spar']))
        log=open('/reports/fixture.log','w');logs.append(log)
        fixture=subprocess.Popen(['/fixtures/switchback-fixtures.test','-test.run=^TestNativeDigestAndLargeAssertionCompatibility$','-test.timeout=180s'],env={**os.environ,'SPAR_SWITCHBACK_FIXTURE_CONFIG':str(config)},stdout=log,stderr=subprocess.STDOUT);processes.append(fixture)
        metadata=wait(fixture,8123,'/metadata')['body'];issuer=re.search(r'entityID="([^"]+)"',metadata).group(1)
        bound=request(8123,'POST','/state',{'action':'bind','team':str(uuid.uuid4()),'idp':'00000000-0000-0000-0000-000000000000','user':str(uuid.uuid4()),'external':'synthetic-patch@example.test','issuer':issuer})
        if bound['status']!=200:raise ValueError('synthetic token fixture binding failed')
        token='synthetic-switchback-'+uuid.uuid4().hex
        seeded=request(8123,'POST','/state',{'action':'legacy-token','token':token,'tokenID':str(uuid.uuid4()),'noIdP':True})
        if seeded['status']!=200:raise ValueError('historical synthetic token seed failed')
        path='/scim/v2/Groups/'+str(uuid.uuid4())
        cases=[('valid-json',json.dumps({'schemas':['urn:ietf:params:scim:api:messages:2.0:PatchOp'],'Operations':[{'op':'replace','path':'displayName','value':'synthetic'}]}),'application/scim+json'),('invalid-json','{','application/scim+json'),('unsupported-media','{}','application/octet-stream')]
        for name,binary in [('native','spar'),('go','/candidate/spar')]:
            log=open('/reports/'+name+'.log','w');logs.append(log);process=subprocess.Popen([binary,'-c',str(config)],stdout=log,stderr=subprocess.STDOUT);processes.append(process);wait(process,8088,'/i/status')
            replies={label:request(8088,'PATCH',path,body,{'Authorization':'Bearer '+token,'Content-Type':media,'Accept':'application/scim+json'}) for label,body,media in cases}
            process.send_signal(signal.SIGTERM);code=process.wait(timeout=35)
            if code!=0:raise ValueError('Spar failed clean shutdown')
            executable=Path('/nix/store/vknmv1ds2q8pq74q06ia1c77mzcn2kg4-spar/bin/spar') if name=='native' else Path(binary)
            output['services'][name]={'sha256':hashlib.sha256(executable.read_bytes()).hexdigest(),'responses':replies,'SIGTERM_exit_code':code}
        def project(reply):
            result=json.loads(json.dumps(reply))
            if isinstance(result['body'],dict) and str(result['body'].get('detail','')).startswith('Prelude.undefined'):
                # Source locations in the runtime call stack are incidental;
                # preserve the exception kind and every other response field.
                result['body']['detail']=result['body']['detail'].splitlines()[0]
            return result
        native=output['services']['native']['responses'];go=output['services']['go']['responses']
        native_detail=native['valid-json']['body']
        if 'Prelude.undefined' not in json.dumps(native_detail) and 'Prelude.undefined' not in Path('/reports/native.log').read_text():
            raise ValueError('native valid request did not prove unsupported Group PATCH handler')
        output['differences']={name:{'native':project(native[name]),'go':project(go[name])} for name,_,_ in cases if project(native[name])!=project(go[name])}
        output['result']='fail' if output['differences'] else 'pass'
        output['scope']='Three actual unsupported GroupPATCH body/media/error comparisons with real historical noIdP token/CQL; not original225 or whole qualification. Raw responses retained; only Prelude.undefined call-stack tail projected.'
        fixture.send_signal(signal.SIGTERM);output['fixture_exit_code']=fixture.wait(timeout=15)
        if output['fixture_exit_code']!=0:output['result']='fail'
    except Exception as error:output.update(result='fail',error=str(error)[:300])
    finally:
        for process in processes:
            if process.poll()is None:process.terminate()
        for process in processes:
            try:process.wait(timeout=5)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
        for log in logs:log.close()
        if forward:forward.server_close()
        Path('/reports/result.json').write_text(json.dumps(output,indent=2)+'\n')
    return 0 if output['result']=='pass' else 1


if __name__=='__main__':raise SystemExit(main())
