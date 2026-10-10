"""Finite actual native/Go startup and HTTP comparison in an owned namespace."""
import contextlib
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


def request(method,path):
    connection=http.client.HTTPConnection('127.0.0.1',8088,timeout=5)
    try:
        connection.request(method,path,headers={'Request-Id':'spar-early-synthetic'})
        response=connection.getresponse();body=response.read(1024*1024+1)
        if len(body)>1024*1024:raise ValueError('unexpected large reference response')
        text=body.decode('utf-8')
        try:value=json.loads(text)
        except json.JSONDecodeError:value=text
        headers={name.lower():response.getheader(name) for name in ('Content-Type','Cache-Control','Pragma','Request-Id','Allow')}
        return {'status':response.status,'headers':headers,'body':value}
    finally:connection.close()


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
            begin=time.monotonic();process.send_signal(signal.SIGTERM);code=process.wait(timeout=35)
            if code!=0:raise ValueError(name+' failed clean SIGTERM')
            return {'responses':replies,'stop_exit_code':code,'stop_seconds':time.monotonic()-begin,'binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest()}
        finally:
            if process.poll()is None:
                process.kill();process.wait(timeout=10)


def main():
    if os.getuid()!=65534:raise ValueError('nonroot namespace required')
    output={'kind':'early-process-comparison','result':'running','e2e_executed':False,'scope':'Configuration, real schema23 startup, health/method/version/errors and SIGTERM only; no SSO/SCIM qualification.'}
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
        output['result']='pass' if not output['differences'] else 'fail'
        return 0 if output['result']=='pass' else 1
    except Exception as error:
        output.update(result='fail',error_category=type(error).__name__,error=str(error)[:300]);return 1
    finally:
        if forward is not None:forward.shutdown();forward.server_close()
        Path('/reports/result.json').write_text(json.dumps(output,indent=2)+'\n')

if __name__=='__main__':raise SystemExit(main())
