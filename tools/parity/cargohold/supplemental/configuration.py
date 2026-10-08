"""Finite configured endpoint/addressing and CloudFront comparisons on real services."""
import base64
import copy
import glob
import hashlib
import http.client
import json
from pathlib import Path
import shutil
import subprocess
import time
from urllib.parse import urlsplit, parse_qs
from cases import OWNER, ASSET, DATA


def run(ref,ports,start,stop,request,store_effects,hs,go):
    records=[]
    original_signed=ref.signed_location
    allowed={'localhost:4570','store:3900','s3.garage.localhost:3900','dummy-bucket.s3.garage.localhost:3900'}
    def signed(location):
        url=urlsplit(location)
        if url.scheme!='http' or url.netloc not in allowed:raise ValueError('signed URL escapes owned fixture endpoints')
        client=http.client.HTTPConnection(url.hostname,url.port,timeout=15)
        try:
            client.request('GET',url.path+'?'+url.query,headers={'Host':url.netloc})
            r=client.getresponse();body=r.read(1024*1024+1)
            if len(body)>1024*1024:raise ValueError('fixture signed read exceeds capture bound')
            query=parse_qs(url.query)
            return {'scheme':url.scheme,'host':url.netloc,'path':url.path,
                    'query':{k:v for k,v in query.items() if k.lower() not in ('x-amz-signature','signature')},
                    'read_status':r.status,'body_sha256':hashlib.sha256(body).hexdigest(),'body_length':len(body)}
        finally:client.close()
    def compare(name,headers=None):
        actual={kind:request(kind,'GET','/assets/v4/example.com/3-2-'+ASSET,headers or {'Z-User':OWNER}) for kind in ports}
        records.append({'id':name,'responses':actual,'equal':actual['haskell']==actual['go']})
    ref.signed_location=signed
    ref.seed('v3/persistent/'+ASSET,{'user':OWNER})
    try:
        for style in ['path','auto','virtual']:
            stop(hs);stop(go)
            options={'aws':{'s3Endpoint':'http://s3.garage.localhost:3900','s3AddressingStyle':style}}
            hs=start('haskell',overrides=options);go=start('go',overrides=options)
            compare('addressing-'+style)
        stop(hs);stop(go)
        options={'aws':{'s3DownloadEndpoint':'http://store:3900'}}
        hs=start('haskell',overrides=options);go=start('go',overrides=options)
        compare('download-endpoint-override')
        stop(hs);stop(go)
        options={'aws':{'multiIngress':{'mapped.example.com':'http://store:3900'}}}
        hs=start('haskell',overrides=options);go=start('go',overrides=options)
        for label,headers in [('mapped',{'Z-User':OWNER,'Z-Host':'mapped.example.com'}),
                               ('unmapped',{'Z-User':OWNER,'Z-Host':'unmapped.example.com'}),('missing',{'Z-User':OWNER})]:
            compare('multi-ingress-'+label,headers)
        stop(hs);stop(go)
        hs=start('haskell',overrides={'aws':{'s3Compatibility':'scality-ring'}})
        go=start('go',overrides={'aws':{'s3Compatibility':'scality-ring'}})
        compare('scality-signed-download')
        from extended import normalize_upload
        results={}
        for kind in ports:
            before={o['Key'] for o in ref.aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
            response=request(kind,'POST','/bot/assets',{'Z-Bot':OWNER,'Z-Type':'bot','Content-Type':'multipart/mixed; boundary=wire'},ref.multipart())
            normalize_upload(response)
            results[kind]={'response':response,'objects':store_effects(before),'open_uploads':ref.aws('list-multipart-uploads','--bucket','dummy-bucket').get('Uploads',[])}
        records.append({'id':'scality-upload','responses':results,'equal':results['haskell']==results['go']})
        stop(hs);stop(go)
        openssl=shutil.which('openssl')
        if not openssl:
            openssl=next((p for p in glob.glob('/nix/store/*-openssl-*/bin/openssl') if Path(p).is_file()),None)
        if not openssl:raise ValueError('qualified container lacks independent OpenSSL verifier')
        subprocess.run([openssl,'genrsa','-out','/tmp/cloudfront.pem','2048'],check=True,capture_output=True,timeout=15)
        subprocess.run([openssl,'rsa','-in','/tmp/cloudfront.pem','-pubout','-out','/tmp/cloudfront-public.pem'],check=True,capture_output=True,timeout=15)
        options={'aws':{'cloudFront':{'domain':'cdn.example.com','keyPairId':'SYNTHETIC-KEY-ID','privateKey':'/tmp/cloudfront.pem'}}}
        hs=start('haskell',overrides=options);go=start('go',overrides=options)
        results={}
        for kind in ports:
            before=time.time()
            status,headers,_=ref.request('GET','/assets/v4/example.com/3-2-'+ASSET,{'Z-User':OWNER},port=ports[kind])
            if status!=302:raise ValueError('CloudFront did not redirect')
            url=urlsplit(headers['location']);q=parse_qs(url.query)
            if url.scheme!='https' or url.netloc!='cdn.example.com' or q['Key-Pair-Id']!=['SYNTHETIC-KEY-ID']:raise ValueError('incorrect CloudFront target/key')
            expires=int(q['Expires'][0])
            if abs(expires-(before+300))>2:raise ValueError('incorrect CloudFront expiry')
            if set(q)!={'Expires','Signature','Key-Pair-Id'}:raise ValueError('unexpected CloudFront signing fields')
            signature=base64.b64decode(q['Signature'][0].translate(str.maketrans('-_~','+=/')),validate=True)
            Path('/tmp/signature').write_bytes(signature)
            resource=url.scheme+'://'+url.netloc+url.path
            policy='{"Statement":[{"Resource":"'+resource+'","Condition":{"DateLessThan":{"AWS:EpochTime":'+str(expires)+'}}}]}'
            Path('/tmp/policy').write_text(policy)
            subprocess.run([openssl,'dgst','-sha1','-verify','/tmp/cloudfront-public.pem','-signature','/tmp/signature','/tmp/policy'],check=True,capture_output=True,timeout=15)
            Path('/tmp/policy').write_text(policy+'tampered')
            invalid=subprocess.run([openssl,'dgst','-sha1','-verify','/tmp/cloudfront-public.pem','-signature','/tmp/signature','/tmp/policy'],capture_output=True,timeout=15)
            if invalid.returncode==0:raise ValueError('CloudFront verifier accepted tampering')
            results[kind]={'status':status,'scheme':url.scheme,'host':url.netloc,'path':url.path,'key_pair_id':q['Key-Pair-Id'][0],
                           'expiry':'<validated-300-second-clock>','signature_verified':True,'tamper_rejected':True}
        records.append({'id':'cloudfront-signature','responses':results,'equal':results['haskell']==results['go']})
        stop(hs);stop(go)
        Path('/tmp/cloudfront-invalid.pem').write_text('synthetic invalid key')
        cfg=json.loads(Path('/fixtures/config.json').read_text());cfg['aws']['cloudFront']={'domain':'cdn.example.com','keyPairId':'SYNTHETIC-KEY-ID','privateKey':'/tmp/cloudfront-invalid.pem'}
        Path('/tmp/invalid-key-config.json').write_text(json.dumps(cfg))
        results={}
        for kind in ports:
            binary='/sbin/cargohold' if kind=='haskell' else '/candidate/cargohold'
            r=subprocess.run([binary,'-c','/tmp/invalid-key-config.json'],capture_output=True,timeout=15)
            results[kind]={'startup_rejected':r.returncode!=0}
        records.append({'id':'cloudfront-invalid-key','responses':results,'equal':all(v['startup_rejected'] for v in results.values())})
    finally:ref.signed_location=original_signed
    return records
