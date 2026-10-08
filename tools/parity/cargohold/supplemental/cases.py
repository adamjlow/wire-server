"""Source-derived finite supplement: exact errors/effects and real service switchback."""
import base64
import copy
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit
from datetime import datetime
import re
import uuid

ROOT=Path('/source')
OWNER='11111111-1111-4111-8111-111111111111'
OTHER='22222222-2222-4222-8222-222222222222'
ASSET='33333333-3333-4333-8333-333333333333'
CONV='44444444-4444-4444-8444-444444444444'
DATA=b'cargo-fixture'


def normalize_signed(ref,location):
    # The independent helper restricts the destination and fetches signed bytes.
    result=ref.signed_location(location)
    if result['read_status']!=200:raise ValueError('signed read did not verify')
    query=result['query']
    if query.get('X-Amz-Algorithm')!=['AWS4-HMAC-SHA256']:raise ValueError('invalid signing algorithm')
    dates=query.get('X-Amz-Date',[])
    if len(dates)!=1:raise ValueError('missing signing date')
    datetime.strptime(dates[0],'%Y%m%dT%H%M%SZ')
    credentials=query.get('X-Amz-Credential',[])
    if len(credentials)!=1:raise ValueError('missing signing scope')
    parts=credentials[0].split('/')
    if len(parts)!=5 or not re.fullmatch(r'[0-9]{8}',parts[1]) or parts[1]!=dates[0][:8] or parts[3:]!=['s3','aws4_request']:
        raise ValueError('invalid signing scope')
    if not query.get('X-Amz-SignedHeaders') or not query.get('X-Amz-Expires'):raise ValueError('missing signing headers/expiry')
    query.pop('X-Amz-Date')
    query['X-Amz-Credential']=[parts[0]+'/<validated-signing-date>/'+ '/'.join(parts[2:])]
    return result


def execute():
    spec=importlib.util.spec_from_file_location('reference', ROOT/'tools/parity/cargohold/reference.py')
    ref=importlib.util.module_from_spec(spec);spec.loader.exec_module(ref)
    sys.path.insert(0,'/opt/wire-e2e');import runtime
    bridge=runtime.TCPServer(('127.0.0.1',4570),runtime.Forward);bridge.destination=('store',3900)
    threading.Thread(target=bridge.serve_forever,daemon=True).start()
    Path('/tmp/home').mkdir();Path('/tmp/payload').write_bytes(DATA)
    class Brig(BaseHTTPRequestHandler):
        def do_POST(self):
            n=int(self.headers.get('Content-Length','0'));body=json.loads(self.rfile.read(n))
            user={'id':body['ids'][0],'qualified_id':{'id':body['ids'][0],'domain':'example.com'},'type':'regular',
                  'name':'Synthetic fixture','accent_id':0,'status':'active','locale':'en','supported_protocols':['proteus']}
            result=json.dumps([user]).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(result)));self.end_headers();self.wfile.write(result)
        def log_message(self,*args): pass
    brig=ThreadingHTTPServer(('127.0.0.1',8082),Brig);threading.Thread(target=brig.serve_forever,daemon=True).start()
    deadline=time.monotonic()+30
    while True:
        try:ref.aws('head-bucket','--bucket','dummy-bucket');break
        except subprocess.CalledProcessError:
            if time.monotonic()>deadline:raise
            time.sleep(.2)
    config=json.loads(Path('/fixtures/config.json').read_text())
    ports={'haskell':8084,'go':8085};processes=[];logs=[];records=[]
    def start(kind,audit=False,overrides=None):
        cfg=copy.deepcopy(config);cfg['cargohold']['port']=ports[kind];cfg['settings']['assetAuditLogEnabled']=audit
        cfg['logLevel']='Info';cfg['logFormat']='StructuredJSON'
        if overrides:
            for section,values in overrides.items():cfg[section].update(values)
        path=Path('/tmp/config-'+kind+'.json');path.write_text(json.dumps(cfg))
        log=open('/reports/'+kind+('-audit' if audit else '')+'.log','w');logs.append(log)
        executable='/sbin/cargohold' if kind=='haskell' else '/candidate/cargohold'
        proc=subprocess.Popen([executable,'-c',str(path)],stdout=log,stderr=subprocess.STDOUT);processes.append(proc)
        until=time.monotonic()+30
        while True:
            if proc.poll() is not None:raise ValueError(kind+' exited')
            try:
                if ref.request('GET','/i/status',port=ports[kind])[0]==200:return proc
            except OSError:pass
            if time.monotonic()>until:raise ValueError(kind+' not ready')
            time.sleep(.1)
    def stop(proc):
        if proc.poll() is None:proc.terminate()
        try:proc.wait(timeout=5)
        except subprocess.TimeoutExpired:proc.kill();proc.wait(timeout=5)
    def request(kind,method,path,headers=None,body=None):
        status,h,data=ref.request(method,path,headers,body,port=ports[kind])
        result={'status':status,'content_type':h.get('content-type'),'body':data.decode('utf-8',errors='replace')}
        if h.get('content-type','').startswith('application/json'):
            result['body']=json.loads(data)
            if isinstance(result['body'],str) and path.startswith('/i/assets/'):
                result['body']=normalize_signed(ref,result['body'])
        if 'location' in h:
            location=h['location']
            if status==302:
                result['signed']=normalize_signed(ref,location)
            else:result['location']=location
        return result
    def compare(name,method,path,headers=None,body=None):
        actual={kind:request(kind,method,path,headers,body) for kind in ports}
        match=actual['haskell']==actual['go'];records.append({'id':name,'responses':actual,'equal':match})
    def store_effects(before):
        after={obj['Key'] for obj in ref.aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
        effects=[]
        for key in sorted(after-before):
            head=ref.aws('head-object','--bucket','dummy-bucket','--key',key)
            ref.aws('get-object','--bucket','dummy-bucket','--key',key,'/tmp/stored')
            metadata=dict(head.get('Metadata',{}))
            if 'token' in metadata:
                if len(base64.urlsafe_b64decode(metadata['token']))!=16:raise ValueError('bad generated token')
                metadata['token']='<validated-16-byte-token>'
            effects.append({'size':head['ContentLength'],'type':head['ContentType'],'metadata':metadata,
                            'sha256':hashlib.sha256(Path('/tmp/stored').read_bytes()).hexdigest()})
        return effects
    try:
        hs=start('haskell');go=start('go')
        if os.environ.get('CARGOHOLD_SUPPLEMENT_GROUP') in ('extended','configuration','mixed','operations','lifecycle','schema'):
            if os.environ['CARGOHOLD_SUPPLEMENT_GROUP']=='extended':
                from extended import run
            elif os.environ['CARGOHOLD_SUPPLEMENT_GROUP']=='configuration':
                from configuration import run
            elif os.environ['CARGOHOLD_SUPPLEMENT_GROUP']=='mixed':
                from mixed import run
            elif os.environ['CARGOHOLD_SUPPLEMENT_GROUP']=='operations':
                from operations import run
            elif os.environ['CARGOHOLD_SUPPLEMENT_GROUP']=='lifecycle':
                from lifecycle import run
            else:
                from schema_cases import run
            records=run(ref,ports,start,stop,request,store_effects,hs,go)
            result={'result':'supplemental-observed','whole_service_parity':False,'cases':records,
                    'mismatches':[r['id'] for r in records if not r['equal']]}
            Path('/reports/supplemental.json').write_text(json.dumps(result,indent=2)+'\n')
            print(json.dumps({'cases':len(records),'mismatches':result['mismatches']}))
            return
        headers={'Z-Bot':OWNER,'Z-Type':'bot','Content-Type':'multipart/mixed; boundary=wire'}
        original=ref.multipart()
        cases=[('missing-first-length',original.replace(b'Content-Length: 41\r\n',b'',1)),
               ('duplicate-payload-length',original.replace(b'Content-Length: 13\r\n',b'Content-Length: 13\r\nContent-Length: 13\r\n')),
               ('malformed-payload-length',original.replace(b'Content-Length: 13',b'Content-Length: invalid')),
               ('malformed-settings-json',original.replace(b'{"public":false',b'{"public":falsX')),
               ('missing-payload-header-terminator',original.replace(b'Content-Length: 13\r\n\r\n',b'Content-Length: 13\r\n')),
               ('missing-payload-length',original.replace(b'Content-Length: 13\r\n',b'')),
               ('duplicate-content-type',original.replace(b'Content-Type: text/plain',b'Content-Type: text/plain\r\nContent-Type: text/plain')),
               ('missing-metadata-part',b'--wire\r\nContent-Type: text/plain\r\nContent-Length: 13\r\n\r\n'+DATA),
               ('metadata-wrong-order',original.replace(b'application/json',b'application/binary'))]
        for name,body in cases:
            effects={}
            for kind in ports:
                before={o['Key'] for o in ref.aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
                response=request(kind,'POST','/bot/assets',headers,body)
                effects[kind]={'response':response,'objects':store_effects(before),'open_uploads':ref.aws('list-multipart-uploads','--bucket','dummy-bucket').get('Uploads',[])}
            records.append({'id':name,'responses':effects,'equal':effects['haskell']==effects['go']})
        key='3-2-'+ASSET
        ref.seed('v3/persistent/'+ASSET,{'user':OWNER,'token':ref.TOKEN})
        compare('token-header-over-query','GET','/assets/v4/example.com/'+key+'?asset_token='+ref.TOKEN,{'Z-User':OTHER,'Asset-Token':ref.WRONG_TOKEN})
        compare('non-owner-mutation','POST','/assets/v3/'+key+'/token',{'Z-User':OTHER})
        compare('malformed-uuid','GET','/bot/assets/invalid',{'Z-Bot':OWNER,'Z-Type':'bot'})
        compare('malformed-token','GET','/assets/v4/example.com/'+key,{'Z-User':OTHER,'Asset-Token':'!!!'})
        compare('internal-signed-url','GET','/i/assets/'+key)
        compare('remote-delete','DELETE','/assets/v4/foreign.example.com/'+key,{'Z-User':OWNER})
        ref.seed(ASSET,{'zasset':'public=true'})
        compare('legacy-plain','GET','/assets/'+ASSET+'?conv_id='+CONV,{'Z-User':OWNER})
        compare('legacy-conversation-plain','GET','/conversations/'+CONV+'/assets/'+ASSET,{'Z-User':OWNER})
        compare('missing-legacy','GET','/assets/'+OTHER+'?conv_id='+CONV,{'Z-User':OWNER})
        def body_with(settings):
            raw=json.dumps(settings,separators=(',',':')).encode()
            return b'--wire\r\nContent-Type: application/json\r\nContent-Length: '+str(len(raw)).encode()+b'\r\n\r\n'+raw+b'\r\n--wire\r\nContent-Type: text/plain\r\nContent-Length: 13\r\n\r\n'+DATA+b'\r\n--wire--\r\n'
        def upload_compare(name,settings,principal='bot',extra=None):
            outcomes={}
            for kind in ports:
                before={o['Key'] for o in ref.aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
                h={'Z-'+principal:OWNER,'Content-Type':'multipart/mixed; boundary=wire','Request-Id':name}
                if principal!='user':h['Z-Type']=principal
                if extra:h.update(extra)
                path='/assets/v3' if principal=='user' else '/'+principal+'/assets'
                started=time.time();response=request(kind,'POST',path,h,body_with(settings))
                if response['status']==201:
                    asset=response['body'];parts=asset['key'].split('-',2)
                    if parts[0]!='3' or uuid.UUID(parts[2]).version!=4:raise ValueError('generated key is not UUIDv4')
                    if response['location'].split('/')[-1]!=asset['key']:raise ValueError('Location/key mismatch')
                    normalized_key='3-'+parts[1]+'-<validated-uuidv4>'
                    response['location']=response['location'].replace(asset['key'],normalized_key);asset['key']=normalized_key
                    if 'token' in asset:
                        if len(base64.urlsafe_b64decode(asset['token']))!=16:raise ValueError('invalid generated token')
                        asset['token']='<validated-16-byte-token>'
                    if 'expires' in asset:
                        if not re.fullmatch(r'.*\.[0-9]{3}Z',asset['expires']):raise ValueError('expiration lacks millisecond precision')
                        duration=28*86400 if settings['retention']=='volatile' else 365*86400
                        actual=datetime.fromisoformat(asset['expires'].replace('Z','+00:00')).timestamp()
                        if abs(actual-(started+duration))>2:raise ValueError('incorrect expiration duration')
                        asset['expires']='<validated-retention-expiry>'
                outcomes[kind]={'response':response,'objects':store_effects(before),'open_uploads':ref.aws('list-multipart-uploads','--bucket','dummy-bucket').get('Uploads',[])}
            records.append({'id':name,'responses':outcomes,'equal':outcomes['haskell']==outcomes['go']})
        for retention in ['eternal','persistent','volatile','eternal-infrequent_access','expiring']:
            upload_compare('retention-'+retention,{'public':False,'retention':retention})
        for principal in ['user','bot','provider']:
            upload_compare('principal-precedence-'+principal,{'public':False},principal,
                {'Z-Bot':OTHER,'Z-Provider':OTHER,'Z-User':OTHER,'Z-'+principal:OWNER})
        # Shared bucket switchback: writes through one real service, reads/changes/deletes through the other.
        for writer,reader in [('haskell','go'),('go','haskell')]:
            for principal in ['user','bot','provider']:
                path='/assets/v3' if principal=='user' else '/'+principal+'/assets'
                h={'Z-'+principal:OWNER,'Content-Type':'multipart/mixed; boundary=wire'}
                if principal!='user':h['Z-Type']=principal
                status,upload_headers,data=ref.request('POST',path,h,ref.multipart(),port=ports[writer])
                if status!=201:raise ValueError('switchback upload '+writer+'/'+principal+' '+str(status)+' '+data.decode())
                asset=json.loads(data);location='/'+upload_headers['location'];read=request(reader,'GET',location,h)
                if read['status']!=302 or read['signed']['read_status']!=200 or read['signed']['body_sha256']!=hashlib.sha256(DATA).hexdigest():raise ValueError('switchback bytes failed')
                key=asset['key'];object_key='v3/persistent/'+key.split('-',2)[2]
                if principal=='user':
                    renew=request(reader,'POST','/assets/v3/'+key+'/token',h)
                    if renew['status']!=200:raise ValueError('switchback token copy failed')
                    new=renew['body']['token']
                    reject=request(writer,'GET',location,{'Z-User':OTHER,'Asset-Token':asset['token']})
                    accept=request(writer,'GET',location,{'Z-User':OTHER,'Asset-Token':new})
                    if reject['status']!=404 or accept['status']!=302:raise ValueError('switchback token authorization failed')
                deleted=request(reader,'DELETE',location,h)
                if deleted['status']!=200 or ref.aws('list-multipart-uploads','--bucket','dummy-bucket').get('Uploads',[]):raise ValueError('switchback delete failed')
                if request(writer,'GET',location,h)['status']!=404:raise ValueError('switchback object remains')
                records.append({'id':'switchback-'+writer+'-'+principal,'equal':True,'bytes_token_delete_effects':True})
        # Seeded raw/encoded audit data and audit-required uploads use both real runtimes.
        stop(hs);stop(go);hs=start('haskell',True);go=start('go',True)
        compare('audit-missing','POST','/bot/assets',headers,ref.multipart())
        audit={'convId':{'id':CONV,'domain':'example.com'},'filename':'synthetic-file.txt','filetype':'text/plain'}
        upload_compare('audit-enabled-valid',dict(audit,public=False))
        upload_compare('audit-null-conversation',dict(audit,convId=None,public=False))
        # Log schema/value comparison for a fixed synthetic request; no signed URLs or secrets are logged.
        for log in logs:log.flush()
        events={}
        for kind in ports:
            found=[]
            for line in Path('/reports/'+kind+'-audit.log').read_text().splitlines():
                try:event=json.loads(line)
                except ValueError:continue
                if event.get('event')=='file-upload' and event.get('file.name')=='synthetic-file.txt':
                    event=dict(event);event.pop('time',None)
                    if 'upload.path' in event:
                        key=event['upload.path'].split('/')[-1]
                        if uuid.UUID(key).version!=4:raise ValueError('invalid audit path UUID')
                        event['upload.path']='v3/persistent/<validated-uuidv4>'
                    found.append(event)
            events[kind]=found
        records.append({'id':'audit-upload-log','responses':events,'equal':bool(events['haskell']) and events['haskell']==events['go']})
        for encoding in ['raw','encoded']:
            value=json.dumps(audit,separators=(',',':'))
            if encoding=='encoded':
                from urllib.parse import quote
                value=quote(value,safe='')
            ref.seed('v3/persistent/'+ASSET,{'user':OWNER,'token':ref.TOKEN,'wire-metadata':value})
            compare('audit-seeded-'+encoding,'GET','/assets/v4/example.com/3-2-'+ASSET,{'Z-User':OWNER})

        result={'result':'supplemental-observed','whole_service_parity':False,'cases':records,
                'mismatches':[r['id'] for r in records if not r['equal']]}
        Path('/reports/supplemental.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps({'cases':len(records),'mismatches':result['mismatches'],'whole_service_parity':False}))
    finally:
        for proc in processes:stop(proc)
        for log in logs:log.close()
        brig.shutdown();brig.server_close();bridge.shutdown();bridge.server_close()
