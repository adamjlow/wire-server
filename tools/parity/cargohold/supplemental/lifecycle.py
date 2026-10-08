"""Finite real client cancellation, CLI/config and loaded SIGTERM drain checks."""
import copy
import hashlib
import http.client
import json
from pathlib import Path
import socket
import subprocess
import time
from cases import OWNER, OTHER, ASSET, DATA
from extended import FaultStore


def run(ref,ports,start,stop,request,store_effects,hs,go):
    records=[];cancellations={};store=FaultStore()
    import threading
    threading.Thread(target=store.serve_forever,daemon=True).start()
    fixture=None;fixture_log=None
    def persist():
        Path('/reports/lifecycle-partial.json').write_text(json.dumps({'cases':records,'whole_service_parity':False},indent=2)+'\n')
    try:
        # Disconnect after a genuine S3 multipart part was sent; inspect completed/abort effects.
        for kind in ports:
            stop(hs);stop(go)
            options={'aws':{'s3Endpoint':'http://localhost:4571','s3DownloadEndpoint':'http://localhost:4570'}}
            hs=start('haskell',overrides=options);go=start('go',overrides=options)
            before={o['Key'] for o in ref.aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
            store.reset(None)
            full=ref.multipart(payload=b'x'*(24*1024*1024))
            client=socket.create_connection(('127.0.0.1',ports[kind]),timeout=15)
            try:
                headers=('POST /bot/assets HTTP/1.1\r\nHost: localhost\r\nZ-Bot: '+OWNER+'\r\nZ-Type: bot\r\nRequest-Id: cancellation-'+kind+'\r\nContent-Type: multipart/mixed; boundary=wire\r\nContent-Length: '+str(len(full))+'\r\n\r\n').encode()
                client.sendall(headers+full[:7*1024*1024])
                deadline=time.monotonic()+10
                while 'part' not in store.trace:
                    if time.monotonic()>deadline:raise ValueError('client cancellation did not reach real upload part')
                    time.sleep(.05)
            finally:client.close()
            deadline=time.monotonic()+15
            while True:
                uploads=ref.aws('list-multipart-uploads','--bucket','dummy-bucket').get('Uploads',[])
                if not uploads and ('abort' in store.trace or 'complete' in store.trace):break
                if time.monotonic()>deadline:break
                time.sleep(.1)
            effects=store_effects(before)
            cancellations[kind]={'objects':effects,'open_upload_count':len(uploads),'s3_operations':list(store.trace)}
            for upload in uploads:
                parts=ref.aws('list-parts','--bucket','dummy-bucket','--key',upload['Key'],'--upload-id',upload['UploadId']).get('Parts',[])
                cancellations[kind]['stored_part_sizes']=[part['Size'] for part in parts]
                # The owned reference leak is observed before task-only cleanup.
                ref.aws('abort-multipart-upload','--bucket','dummy-bucket','--key',upload['Key'],'--upload-id',upload['UploadId'])
            records.append({'id':'client-cancellation-'+kind,'response':cancellations[kind],'equal':not effects})
            persist()
        records.append({'id':'client-cancellation-effects','responses':cancellations,'equal':cancellations['haskell']==cancellations['go']})
        persist()
        # Share real peers through h2c; hold the stream after one flushed chunk.
        store.shutdown();store.server_close();store=None
        fixture_log=open('/reports/lifecycle-federator.log','w')
        fixture=subprocess.Popen(['/candidate/federator'],stdout=fixture_log,stderr=subprocess.STDOUT)
        deadline=time.monotonic()+5
        while True:
            try:
                if ref.request('POST','/control',{'Content-Type':'application/json'},b'{"hold":false}',port=8097)[0]==200:break
            except OSError:pass
            if time.monotonic()>deadline:raise ValueError('drain fixture not ready')
            time.sleep(.1)
        for kind in ports:
            stop(hs);stop(go);hs=start('haskell');go=start('go')
            peer='go' if kind=='haskell' else 'haskell';proc=hs if kind=='haskell' else go
            payload=b'd'*(2*1024*1024)
            Path('/tmp/drain-payload').write_bytes(payload)
            ref.aws('put-object','--bucket','dummy-bucket','--key','v3/persistent/'+ASSET,'--body','/tmp/drain-payload',
                    '--content-type','application/octet-stream','--metadata',json.dumps({'user':OWNER,'token':ref.TOKEN}))
            control=json.dumps({'target':str(ports[peer]),'hold':True}).encode()
            if ref.request('POST','/control',{'Content-Type':'application/json'},control,port=8097)[0]!=200:raise ValueError('hold control failed')
            client=http.client.HTTPConnection('127.0.0.1',ports[kind],timeout=30)
            try:
                client.request('GET','/assets/v4/foreign.example.com/3-2-'+ASSET,headers={'Z-User':OTHER,'Asset-Token':ref.TOKEN,'Request-Id':'drain-'+kind})
                response=client.getresponse();prefix=response.read(4096)
                if response.status!=200 or prefix!=payload[:4096]:raise ValueError('active drain stream not established')
                started=time.monotonic();proc.terminate()
                accepting=True;until=time.monotonic()+5
                while accepting and time.monotonic()<until:
                    try:accepting=ref.request('GET','/i/status',port=ports[kind])[0]==200
                    except OSError:accepting=False
                    if accepting:time.sleep(.02)
                alive_while_held=proc.poll() is None
                ref.request('POST','/control',{'Content-Type':'application/json'},b'{"hold":false}',port=8097)
                digest=hashlib.sha256(prefix);length=len(prefix);error=None
                try:
                    while chunk:=response.read(65536):
                        length+=len(chunk);digest.update(chunk)
                except (OSError,http.client.HTTPException) as e:error=type(e).__name__
                client.close()
                proc.wait(timeout=30)
                elapsed=time.monotonic()-started
                complete=length==len(payload) and digest.hexdigest()==hashlib.sha256(payload).hexdigest()
                records.append({'id':'active-download-drain-'+kind,'new_requests_rejected':not accepting,'alive_while_held':alive_while_held,
                                'bytes':length,'expected_bytes':len(payload),'sha256_matches':complete,'exit_code':proc.returncode,'seconds':elapsed,
                                'read_error':error,'equal':not accepting and alive_while_held and complete and proc.returncode==0 and elapsed<=30})
                persist()
            finally:
                client.close();ref.request('POST','/control',{'Content-Type':'application/json'},b'{"hold":false}',port=8097)
        # CLI help and rejection of malformed config are shape/exit contracts, not language usage text.
        actual={}
        invalid=Path('/tmp/invalid-config.json');invalid.write_text('{"aws":{}}')
        for kind in ports:
            binary='/sbin/cargohold' if kind=='haskell' else '/candidate/cargohold'
            help_result=subprocess.run([binary,'--help'],capture_output=True,timeout=15)
            text=(help_result.stdout+help_result.stderr).decode()
            bad=subprocess.run([binary,'--config-file',str(invalid)],capture_output=True,timeout=15)
            actual[kind]={'help_exit_zero':help_result.returncode==0,'config_flag_documented':'config-file' in text,'invalid_config_rejected':bad.returncode!=0}
        records.append({'id':'cli-config-startup','responses':actual,'equal':actual['haskell']==actual['go'] and all(actual['go'].values())})
    finally:
        if store:store.shutdown();store.server_close()
        if fixture:fixture.terminate();fixture.wait(timeout=5)
        if fixture_log:fixture_log.close()
    return records
