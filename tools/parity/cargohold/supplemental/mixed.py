"""Finite real cross-runtime federation and audit-inclusive storage switchback."""
import base64
import hashlib
import json
from pathlib import Path
import subprocess
import time
from urllib.parse import unquote
from cases import OWNER, OTHER, ASSET, CONV, DATA


def run(ref,ports,start,stop,request,store_effects,hs,go):
    records=[]
    log=open('/reports/federator.log','w')
    fixture=subprocess.Popen(['/candidate/federator'],stdout=log,stderr=subprocess.STDOUT)
    audit={'convId':{'domain':'example.com','id':CONV},'filename':'synthetic-file.txt','filetype':'text/plain'}
    raw=json.dumps(dict(audit,public=False),separators=(',',':')).encode()
    body=b'--wire\r\nContent-Type: application/json\r\nContent-Length: '+str(len(raw)).encode()+b'\r\n\r\n'+raw+b'\r\n--wire\r\nContent-Type: text/plain\r\nContent-Length: 13\r\n\r\n'+DATA+b'\r\n--wire--\r\n'
    try:
        for caller,peer in [('haskell','go'),('go','haskell')]:
            stop(hs);stop(go)
            hs=start('haskell',True,{'settings':{'federationDomain':'example.com' if caller=='haskell' else 'foreign.example.com'}})
            go=start('go',True,{'settings':{'federationDomain':'example.com' if caller=='go' else 'foreign.example.com'}})
            deadline=time.monotonic()+5
            while True:
                try:
                    status,_,_=ref.request('POST','/control',{'Content-Type':'application/json'},json.dumps({'target':str(ports[peer])}).encode(),port=8097)
                    if status==200:break
                except OSError:pass
                if fixture.poll() is not None or time.monotonic()>deadline:raise ValueError('synthetic h2c fixture not ready')
                time.sleep(.1)
            ref.seed('v3/persistent/'+ASSET,{'user':OWNER,'token':ref.TOKEN,'wire-metadata':json.dumps(audit,separators=(',',':'))})
            for label,who,token,expected in [('owner',OWNER,None,200),('token',OTHER,ref.TOKEN,200),('private-denial',OTHER,None,404)]:
                headers={'Z-User':who,'Request-Id':'mixed-'+caller+'-'+label}
                if token:headers['Asset-Token']=token
                response=request(caller,'GET','/assets/v4/foreign.example.com/3-2-'+ASSET,headers)
                equal=response['status']==expected
                if expected==200:equal=equal and response['content_type']=='application/octet-stream' and response['body']==DATA.decode()
                records.append({'id':'mixed-'+caller+'-to-'+peer+'-'+label,'response':response,'equal':equal})
        # Six switchbacks now require audit data to survive token copies as well as byte reads/deletes.
        stop(hs);stop(go);hs=start('haskell',True);go=start('go',True)
        for writer,reader in [('haskell','go'),('go','haskell')]:
            for principal in ['user','bot','provider']:
                path='/assets/v3' if principal=='user' else '/'+principal+'/assets'
                headers={'Z-'+principal:OWNER,'Content-Type':'multipart/mixed; boundary=wire','Request-Id':'audit-switchback-'+writer+'-'+principal}
                if principal!='user':headers['Z-Type']=principal
                status,h,data=ref.request('POST',path,headers,body,port=ports[writer])
                if status!=201:raise ValueError('audit switchback upload failed')
                asset=json.loads(data);location='/'+h['location'];key='v3/persistent/'+asset['key'].split('-',2)[2]
                initial=ref.aws('head-object','--bucket','dummy-bucket','--key',key)
                if json.loads(unquote(initial['Metadata']['wire-metadata']))!=audit:raise ValueError('stored audit metadata missing')
                read=request(reader,'GET',location,headers)
                if read['status']!=302 or read['signed']['body_sha256']!=hashlib.sha256(DATA).hexdigest():raise ValueError('audit switchback byte read failed')
                if principal=='user':
                    renewed=request(reader,'POST','/assets/v3/'+asset['key']+'/token',headers)
                    if renewed['status']!=200:raise ValueError('audit token renewal failed')
                    new=renewed['body']['token']
                    copied=ref.aws('head-object','--bucket','dummy-bucket','--key',key)
                    if copied['Metadata']['wire-metadata']!=initial['Metadata']['wire-metadata']:raise ValueError('token copy changed audit metadata')
                    if request(writer,'GET',location,{'Z-User':OTHER,'Asset-Token':asset['token']})['status']!=404:raise ValueError('old token still valid')
                    if request(writer,'GET',location,{'Z-User':OTHER,'Asset-Token':new})['status']!=302:raise ValueError('new token invalid')
                if request(reader,'DELETE',location,headers)['status']!=200 or request(writer,'GET',location,headers)['status']!=404:raise ValueError('audit switchback deletion failed')
                records.append({'id':'audit-switchback-'+writer+'-'+principal,'bytes_token_audit_delete_effects':True,'equal':True})
        # Seeded old raw audit JSON must be re-encoded by token copy, as S3.metaHeaders does.
        results={}
        for kind in ports:
            ref.seed('v3/persistent/'+ASSET,{'user':OWNER,'wire-metadata':json.dumps(audit,separators=(',',':'))})
            response=request(kind,'POST','/assets/v3/3-2-'+ASSET+'/token',{'Z-User':OWNER})
            if response['status']!=200:raise ValueError('seeded audit token copy failed')
            head=ref.aws('head-object','--bucket','dummy-bucket','--key','v3/persistent/'+ASSET)
            meta=head['Metadata'];token=meta.pop('token')
            if len(base64.urlsafe_b64decode(token))!=16:raise ValueError('invalid copied token')
            results[kind]={'metadata':meta,'content_type':head['ContentType']}
        records.append({'id':'seeded-audit-token-copy','responses':results,'equal':results['haskell']==results['go']})
        # Both callers use an unavailable local federator at the same endpoint.
        stop(hs);stop(go)
        hs=start('haskell',overrides={'federator':{'port':8098}});go=start('go',overrides={'federator':{'port':8098}})
        actual={kind:request(kind,'GET','/assets/v4/foreign.example.com/3-2-'+ASSET,{'Z-User':OWNER}) for kind in ports}
        records.append({'id':'remote-transport-failure','responses':actual,'equal':actual['haskell']==actual['go']})
        Path('/reports/mixed-partial.json').write_text(json.dumps({'cases':records,'whole_service_parity':False},indent=2)+'\n')
        log.flush()
        traces=[json.loads(line) for line in Path('/reports/federator.log').read_text().splitlines() if line.startswith('{')]
        required={'mixed-'+kind+'-'+label for kind in ports for label in ['owner','token','private-denial']}
        if {e['request_id'] for e in traces}!=required:raise ValueError('missing or wrong outbound request IDs')
        if any(e['protocol']!=2 or e['origin']!='example.com' for e in traces):raise ValueError('wrong federation protocol/origin')
        normalized={}
        for kind in ports:
            rows=[]
            for e in traces:
                if e['request_id'].startswith('mixed-'+kind+'-'):
                    e=dict(e);e['request_id']=e['request_id'].replace('mixed-'+kind+'-','mixed-<caller>-',1)
                    if 'target_port' in e:
                        expected=str(ports['go' if kind=='haskell' else 'haskell'])
                        if e['target_port']!=expected:raise ValueError('wrong peer target')
                        e['target_port']='<verified-peer-port>'
                    rows.append(e)
            normalized[kind]=rows
        records.append({'id':'mixed-outbound-wire','responses':normalized,'equal':normalized['haskell']==normalized['go']})
    finally:
        fixture.terminate();fixture.wait(timeout=5);log.close()
    return records
