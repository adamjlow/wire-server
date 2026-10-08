#!/usr/bin/env python3
"""Bounded observations of the pinned cargohold reference, without a Go candidate."""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import threading
import time
from urllib.parse import urlsplit, parse_qs
import uuid

ROOT=Path('/source') if '--inside' in sys.argv else Path(__file__).resolve().parents[3]
OWNER='11111111-1111-4111-8111-111111111111'
OTHER='22222222-2222-4222-8222-222222222222'
ASSET='33333333-3333-4333-8333-333333333333'
CONVERSATION='44444444-4444-4444-8444-444444444444'
TOKEN='YWJjZGVmZ2hpamtsbW5vcA=='
WRONG_TOKEN='eHl6eHl6eHl6eHl6eHl6eA=='
DATA=b'cargo-fixture'
KEY='3-2-'+ASSET


def command(args,timeout=60):
    return subprocess.run(list(map(str,args)),check=True,text=True,capture_output=True,timeout=timeout).stdout.strip()


def request(method,path,headers=None,body=None,port=8084):
    connection=http.client.HTTPConnection('127.0.0.1',port,timeout=15)
    try:
        connection.request(method,path,body=body,headers=headers or {})
        response=connection.getresponse();data=response.read(1024*1024+1)
        if len(data)>1024*1024:raise ValueError('response exceeds capture limit')
        return response.status,dict((key.lower(),value) for key,value in response.getheaders()),data
    finally:connection.close()


def signed_location(location):
    parsed=urlsplit(location)
    if parsed.scheme!='http' or parsed.hostname!='localhost' or parsed.port!=4570:
        raise ValueError('signed fixture URL escapes isolated store')
    path=parsed.path+('?' +parsed.query if parsed.query else '')
    status,_,data=request('GET',path,port=4570)
    # Keep signing scope/TTL/target, excluding raw reusable signature values.
    query=parse_qs(parsed.query)
    return {'scheme':parsed.scheme,'host':parsed.netloc,'path':parsed.path,
            'query':{key:values for key,values in query.items() if key.lower() not in ('x-amz-signature','signature')},
            'read_status':status,'body_sha256':hashlib.sha256(data).hexdigest(),'body_length':len(data)}


def observe(case,method,path,headers=None,body=None,expected=None):
    status,response_headers,data=request(method,path,headers,body)
    record={'id':case,'method':method,'path':path,'status':status,
            'content_type':response_headers.get('content-type'),'body_sha256':hashlib.sha256(data).hexdigest(),
            'body_length':len(data),'expected_status':expected}
    if response_headers.get('content-type','').startswith('application/json'):
        record['json']=json.loads(data)
    elif len(data)<=4096:
        record['text']=data.decode('utf-8',errors='replace')
    if 'location' in response_headers:
        if status==302:record['signed_download']=signed_location(response_headers['location'])
        else:record['location']=response_headers['location']
    record['status_matches_source']=status==expected if expected is not None else None
    return record


def aws(*args):
    return json.loads(command(['aws','--endpoint-url=http://localhost:4570','--no-cli-pager','s3api',*args]) or '{}')


def seed(key,metadata):
    aws('put-object','--bucket','dummy-bucket','--key',key,'--body','/tmp/payload',
        '--content-type','application/octet-stream','--metadata',json.dumps(metadata))


def multipart(boundary='wire',second='wire',declared=None,md5=None,trailer=True,payload=DATA):
    metadata=b'{"public":false,"retention":"persistent"}'
    length=str(len(payload)) if declared is None else declared
    body=b'--'+boundary.encode()+b'\r\nContent-Type: application/json\r\nContent-Length: '+str(len(metadata)).encode()+b'\r\n\r\n'+metadata
    body+=b'\r\n--'+second.encode()+b'\r\nContent-Type: text/plain\r\nContent-Length: '+length.encode()+b'\r\n'
    if md5 is not None:body+=b'Content-MD5: '+md5.encode()+b'\r\n'
    body+=b'\r\n'+payload
    if trailer:body+=b'\r\n--wire--\r\n'
    return body


def inside():
    if os.getuid()!=65534:raise ValueError('reference runner must be non-root')
    sys.path.insert(0,'/opt/wire-e2e')
    import runtime
    server=runtime.TCPServer(('127.0.0.1',4570),runtime.Forward)
    server.destination=('store',3900)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    Path('/tmp/home').mkdir();Path('/tmp/payload').write_bytes(DATA)
    deadline=time.monotonic()+30
    while True:
        try:aws('head-bucket','--bucket','dummy-bucket');break
        except subprocess.CalledProcessError:
            if time.monotonic()>deadline:raise
            time.sleep(.2)
    log=open('/reports/service.log','w')
    process=subprocess.Popen(['cargohold','-c','/fixtures/config.json'],stdout=log,stderr=subprocess.STDOUT)
    observations=[]
    try:
        while True:
            if process.poll() is not None:raise ValueError('Haskell cargohold exited')
            try:
                if request('GET','/i/status')[0] in (200,204):break
            except OSError:pass
            if time.monotonic()>deadline:raise ValueError('Haskell cargohold not ready')
            time.sleep(.1)
        seed('v3/persistent/'+ASSET,{'user':OWNER,'token':TOKEN})
        for who,label,token,available in ((OWNER,'matching-uuid-missing-token',None,True),
                                        (OWNER,'matching-uuid-wrong-token',WRONG_TOKEN,True),
                                        (OTHER,'different-uuid-missing-token',None,False),
                                        (OTHER,'different-uuid-wrong-token',WRONG_TOKEN,False),
                                        (OTHER,'different-uuid-correct-token',TOKEN,True)):
            body=json.dumps({'user':who,'key':KEY,'token':token}).encode()
            headers={'Content-Type':'application/json','Wire-Origin-Domain':'foreign.example.com'}
            get=observe('federation-get-'+label,'POST','/federation/get-asset',headers,body,200)
            get['expected_available']=available;get['available_matches_source']=get.get('json',{}).get('available')==available
            observations.append(get)
            stream=observe('federation-stream-'+label,'POST','/federation/stream-asset',headers,body,200 if available else 404)
            stream['authorized_bytes_match']=stream['body_sha256']==hashlib.sha256(DATA).hexdigest() if available else None
            observations.append(stream)
        legacy='otr/'+CONVERSATION+'/'+ASSET
        legacy_path='/conversations/'+CONVERSATION+'/otr/assets/'+ASSET
        for label,metadata,who,status in (('same-requester',{'user':OWNER},OWNER,302),
                                        ('different-requester',{'user':OWNER},OTHER,302),
                                        ('missing-stored-user',{},OWNER,404),
                                        ('malformed-stored-user',{'user':'invalid'},OWNER,404)):
            seed(legacy,metadata)
            observations.append(observe('legacy-otr-'+label,'GET',legacy_path,{'Z-User':who},expected=status))
        cases=(('canonical',{},201),('unequal-delimiters',{'second':'different'},201),
               ('wrong-md5',{'md5':'AAAAAAAAAAAAAAAAAAAAAA=='},201),
               ('decimal-length-suffix',{'declared':str(len(DATA))+'junk'},201),
               ('missing-trailer',{'trailer':False},201),
               ('incomplete-data',{'payload':DATA[:-3],'declared':str(len(DATA)),'trailer':False},400))
        for label,kwargs,status in cases:
            before={obj['Key'] for obj in aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
            record=observe('multipart-'+label,'POST','/bot/assets',
                           {'Z-Bot':OWNER,'Z-Type':'bot','Content-Type':'multipart/mixed; boundary=wire'},multipart(**kwargs),status)
            after={obj['Key'] for obj in aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
            effects=[]
            for key in sorted(after-before):
                head=aws('head-object','--bucket','dummy-bucket','--key',key)
                aws('get-object','--bucket','dummy-bucket','--key',key,'/tmp/stored')
                stored=Path('/tmp/stored').read_bytes()
                effects.append({'key':key,'size':head['ContentLength'],'content_type':head.get('ContentType'),
                                'metadata':head.get('Metadata'),'body_sha256':hashlib.sha256(stored).hexdigest()})
            record['created_objects']=effects;record['multipart_uploads']=aws('list-multipart-uploads','--bucket','dummy-bucket').get('Uploads',[])
            observations.append(record)
        binary=Path('/sbin/cargohold').resolve()
        result={'schema_version':1,'result':'reference-observed','candidate':None,'parity_verified':False,
                'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),
                'config_sha256':hashlib.sha256(Path('/fixtures/config.json').read_bytes()).hexdigest(),
                'observations':observations}
        Path('/reports/reference.json').write_text(json.dumps(result,indent=2)+'\n')
        if any(item.get('status_matches_source') is False or item.get('available_matches_source') is False or item.get('authorized_bytes_match') is False for item in observations):
            raise ValueError('observations differ from source expectations; inspect retained capture')
        print(json.dumps({'result':'reference-observed','cases':len(observations),'parity_verified':False}))
    finally:
        if process.poll() is None:process.terminate()
        try:process.wait(timeout=5)
        except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
        log.close();server.server_close()


def host(output):
    sys.path.insert(0,str(ROOT/'tools/parity/docker'))
    import environment
    output=environment.destination(output)
    if command(['git','status','--porcelain']):raise ValueError('commit characterization inputs first')
    revision=command(['git','rev-parse','HEAD'])
    output.mkdir();(output/'reports').mkdir();(output/'reports').chmod(0o777)
    (output/'fixtures').mkdir()
    config=environment.yaml_json(ROOT/'services/cargohold/cargohold.integration.yaml')
    (output/'fixtures/config.json').write_text(json.dumps(config,indent=2)+'\n')
    runner=json.loads((ROOT/'tools/parity/docker/environment-build-receipt.json').read_text())['image_id']
    store=json.loads((ROOT/'tools/parity/docker/baselines/haskell-cargohold-environment.json').read_text())['dependencies']['fake_s3']
    name='wire-cargohold-reference-'+uuid.uuid4().hex
    owned=[];network=None
    try:
        network=command(['docker','network','create','--internal',name])
        store_id=command(['docker','create','--name',name+'-store','--network',network,'--network-alias','store',
            '--user','65534:65534','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges',
            '--pids-limit=128','--memory=512m','--cpus=1','--tmpfs','/tmp:rw,nosuid,nodev,size=128m,mode=1777',
            '--env','GARAGE_DEFAULT_ACCESS_KEY='+environment.SYNTHETIC_ENV['AWS_ACCESS_KEY_ID'],
            '--env','GARAGE_DEFAULT_SECRET_KEY='+environment.SYNTHETIC_ENV['AWS_SECRET_ACCESS_KEY'],
            '--env','GARAGE_DEFAULT_BUCKET=dummy-bucket','--mount','type=bind,source='+str(ROOT/'deploy/dockerephemeral/docker/garage.toml')+',target=/etc/garage.toml,readonly',
            store,'/garage','server','--single-node','--default-bucket'])
        owned.append(store_id);command(['docker','start',store_id])
        env={key:value for key,value in environment.SYNTHETIC_ENV.items() if key.startswith('AWS_') or key=='HOME'}
        args=['docker','create','--name',name+'-runner','--network',network,'--user','65534:65534',
              '--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--pids-limit=256','--memory=1g','--cpus=2',
              '--tmpfs','/tmp:rw,nosuid,nodev,size=128m,mode=1777','--entrypoint','/bin/bash']
        for key,value in env.items():args+=['--env',key+'='+value]
        for source,target,readonly in ((Path(__file__).parent,'/probe',True),(output/'fixtures','/fixtures',True),(output/'reports','/reports',False)):
            args+=['--mount','type=bind,source='+str(source)+',target='+target+(',readonly' if readonly else '')]
        args += [runner,'-c','for runner_python in /nix/store/*-python3-*/bin/python3; do if [[ -x "$runner_python" ]]; then exec "$runner_python" /probe/reference.py --inside; fi; done; exit 2']
        runner_id=command(args);owned.append(runner_id)
        with (output/'capture.log').open('x') as stream:
            process=subprocess.run(['docker','start','--attach',runner_id],stdout=stream,stderr=subprocess.STDOUT,timeout=180)
        state=json.loads(command(['docker','inspect',runner_id]))[0]
        report={'source_revision':revision,'runner_image_id':runner,'store_image':store,
                'reference_source_revision':'f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e',
                'script_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                'process_exit_code':state['State']['ExitCode'],'internal_network':True,'published_ports':False}
        (output/'run.json').write_text(json.dumps(report,indent=2)+'\n')
        if process.returncode!=0 or report['process_exit_code']!=0:raise ValueError('reference capture failed; inspect retained logs/capture')
        return report
    finally:
        for identity in reversed(owned):
            subprocess.run(['docker','rm','--force',identity],check=True,capture_output=True,timeout=15)
        if network:subprocess.run(['docker','network','rm',network],check=True,capture_output=True,timeout=15)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output',type=Path);parser.add_argument('--inside',action='store_true');args=parser.parse_args()
    try:
        if args.inside:inside()
        elif args.output:print(json.dumps(host(args.output),indent=2))
        else:parser.error('--output required')
        return 0
    except (OSError,ValueError,KeyError,subprocess.SubprocessError) as error:
        print(json.dumps({'result':'failed','error':str(error)[:400],'parity_verified':False}));return 2

if __name__=='__main__':raise SystemExit(main())
