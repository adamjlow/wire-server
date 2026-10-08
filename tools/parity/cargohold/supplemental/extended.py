"""Remaining bounded source-derived auth, streaming, configuration and S3 fault cases."""
import base64
import gzip
import hashlib
import http.client
import json
import threading
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit
from cases import OWNER, OTHER, ASSET, CONV, DATA


class FaultStore(ThreadingHTTPServer):
    """Forward to the owned real store, injecting one configured S3 operation."""
    def __init__(self):
        super().__init__(('127.0.0.1',4571), FaultRequest)
        self.mode=None
        self.trace=[]
        self.injected=0

    def reset(self, mode):
        self.mode=mode;self.trace=[];self.injected=0


class FaultRequest(BaseHTTPRequestHandler):
    def do_HEAD(self): self.forward()
    def do_GET(self): self.forward()
    def do_POST(self): self.forward()
    def do_PUT(self): self.forward()
    def do_DELETE(self): self.forward()
    def log_message(self,*args): pass
    def forward(self):
        query=parse_qs(urlsplit(self.path).query,keep_blank_values=True)
        operation=self.command
        if 'uploadId' in query:
            operation={'PUT':'part','POST':'complete','DELETE':'abort'}.get(self.command,self.command)
        elif 'uploads' in query and self.command=='POST':operation='create'
        self.server.trace.append(operation)
        n=int(self.headers.get('Content-Length','0'))
        if n>32*1024*1024:raise ValueError('fixture request exceeded bounded size')
        data=self.rfile.read(n)
        mode=self.server.mode
        inject=(mode in ('missing','denied','throttled') and operation=='HEAD' or mode==operation)
        if mode=='throttled' and self.server.injected:inject=False
        if inject:
            self.server.injected+=1
            code,status=('NoSuchKey',404) if mode=='missing' else ('RequestThrottled',503) if mode=='throttled' else ('AccessDenied',403)
            body=('<Error><Code>'+code+'</Code><Message>Synthetic fault</Message><RequestId>fixture</RequestId></Error>').encode()
            self.send_response(status);self.send_header('Content-Type','application/xml');self.send_header('Content-Length',str(len(body)));self.end_headers()
            if self.command!='HEAD':self.wfile.write(body)
            return
        upstream=http.client.HTTPConnection('store',3900,timeout=15)
        try:
            headers={k:v for k,v in self.headers.items() if k.lower() not in ('connection','expect')}
            upstream.request(self.command,self.path,data,headers)
            response=upstream.getresponse();body=response.read(28*1024*1024+1)
            if len(body)>28*1024*1024:raise ValueError('fixture response exceeded bounded size')
            self.send_response(response.status)
            for k,v in response.getheaders():
                if k.lower() not in ('connection','transfer-encoding','content-length'):self.send_header(k,v)
            # A HEAD response carries the object's length while having no body.
            self.send_header('Content-Length',response.getheader('Content-Length',str(len(body))))
            self.end_headers()
            if self.command!='HEAD':self.wfile.write(body)
        finally:upstream.close()


def normalize_upload(response):
    if response['status']!=201:return
    asset=response['body'];key=asset['key'];parts=key.split('-',2)
    if parts[:2]!=['3','2'] or uuid.UUID(parts[2]).version!=4:raise ValueError('invalid generated persistent asset key')
    if response['location'].split('/')[-1]!=key:raise ValueError('Location/key mismatch')
    asset['key']='3-2-<validated-uuidv4>';response['location']=response['location'].replace(key,asset['key'])
    if 'token' in asset:
        if len(base64.urlsafe_b64decode(asset['token']))!=16:raise ValueError('invalid token')
        asset['token']='<validated-16-byte-token>'


def run(ref,ports,start,stop,request,store_effects,hs,go):
    records=[]
    def compare(name,method,path,headers=None,body=None):
        results={k:request(k,method,path,headers,body) for k in ports}
        records.append({'id':name,'responses':results,'equal':results['haskell']==results['go']})
    def upload(name,body,chunked=False,extra=None,store=None):
        results={}
        for kind in ports:
            before={o['Key'] for o in ref.aws('list-objects-v2','--bucket','dummy-bucket').get('Contents',[])}
            headers={'Z-Bot':OWNER,'Z-Type':'bot','Content-Type':'multipart/mixed; boundary=wire','Request-Id':name}
            headers.update(extra or {})
            if store:store.reset(name.removeprefix('s3-'))
            if chunked:
                connection=http.client.HTTPConnection('127.0.0.1',ports[kind],timeout=30)
                try:
                    chunks=(body[i:i+7] for i in range(0,len(body),7))
                    connection.request('POST','/bot/assets',chunks,headers,encode_chunked=True)
                    r=connection.getresponse();raw=r.read(1024*1024)
                    response={'status':r.status,'content_type':r.getheader('Content-Type'),'body':json.loads(raw)}
                    if r.getheader('Location'):response['location']=r.getheader('Location')
                finally:connection.close()
            else:response=request(kind,'POST','/bot/assets',headers,body)
            normalize_upload(response)
            results[kind]={'response':response,'objects':store_effects(before),
                           'open_uploads':ref.aws('list-multipart-uploads','--bucket','dummy-bucket').get('Uploads',[])}
            if store:results[kind]['s3_operations']=list(store.trace)
        records.append({'id':name,'responses':results,'equal':results['haskell']==results['go']})
    key='3-2-'+ASSET
    ref.seed('v3/persistent/'+ASSET,{'user':OWNER,'token':ref.TOKEN})
    for who,label,token in [(OWNER,'owner-no-token',None),(OWNER,'owner-wrong-token',ref.WRONG_TOKEN),
                           (OTHER,'other-no-token',None),(OTHER,'other-wrong-token',ref.WRONG_TOKEN),(OTHER,'other-correct-token',ref.TOKEN)]:
        body=json.dumps({'user':who,'key':key,'token':token}).encode()
        h={'Content-Type':'application/json','Wire-Origin-Domain':'foreign.example.com','Request-Id':label}
        for route in ['get-asset','stream-asset']:compare('federation-'+route+'-'+label,'POST','/federation/'+route,h,body)
    for label,meta,who in [('same-requester',{'user':OWNER},OWNER),('different-requester',{'user':OWNER},OTHER),
                           ('missing-stored-user',{},OWNER),('malformed-stored-user',{'user':'invalid'},OWNER)]:
        ref.seed('otr/'+CONV+'/'+ASSET,meta)
        compare('legacy-otr-'+label,'GET','/conversations/'+CONV+'/otr/assets/'+ASSET,{'Z-User':who})
    for label,kwargs in [('canonical',{}),('unequal-delimiters',{'second':'different'}),('wrong-md5',{'md5':'AAAAAAAAAAAAAAAAAAAAAA=='}),
                         ('decimal-length-suffix',{'declared':'13junk'}),('missing-trailer',{'trailer':False}),
                         ('incomplete-data',{'payload':DATA[:-3],'declared':'13','trailer':False})]:
        upload('multipart-'+label,ref.multipart(**kwargs))
    upload('gzip-upload',gzip.compress(ref.multipart(),mtime=0),extra={'Content-Encoding':'gzip'})
    upload('arbitrary-chunk-boundaries',ref.multipart(),chunked=True)
    upload('multipart-24MiB',ref.multipart(payload=b'x'*(24*1024*1024)))
    # Existing source parses Asset-Token only on GET, not DELETE.
    for kind in ports:
        ref.seed('v3/persistent/'+ASSET,{'user':OWNER,'token':ref.TOKEN})
        response=request(kind,'DELETE','/assets/v4/example.com/'+key,{'Z-User':OWNER,'Asset-Token':'!!!'})
        records.append({'id':'delete-ignores-token-'+kind,'response':response,'equal':response['status']==200})
    ref.seed('v3/persistent/'+ASSET,{'user':OWNER})
    tokens={}
    for kind in ports:
        response=request(kind,'POST','/assets/v3/'+key+'/token',{'Z-User':OWNER})
        if response['status']!=200:raise ValueError('token creation failed')
        token=response['body']['token'];tokens[kind]=token
        if len(base64.urlsafe_b64decode(token))!=16:raise ValueError('bad token')
        response['body']['token']='<validated-16-byte-token>'
        records.append({'id':'token-create-'+kind,'response':response,'equal':True})
        deleted=request(kind,'DELETE','/assets/v3/'+key+'/token',{'Z-User':OWNER})
        read=request(kind,'GET','/assets/v4/example.com/'+key,{'Z-User':OTHER})
        records.append({'id':'token-delete-'+kind,'responses':[deleted,read],'equal':deleted['status']==200 and read['status']==302})
    stop(hs);stop(go)
    hs=start('haskell',overrides={'settings':{'disabledAPIVersions':['v2']}})
    go=start('go',overrides={'settings':{'disabledAPIVersions':['v2']}})
    compare('disabled-version','GET','/v2/assets/example.com/'+key,{'Z-User':OWNER})
    stop(hs);stop(go)
    store=FaultStore();threading.Thread(target=store.serve_forever,daemon=True).start()
    try:
        hs=start('haskell',overrides={'aws':{'s3Endpoint':'http://localhost:4571'}})
        go=start('go',overrides={'aws':{'s3Endpoint':'http://localhost:4571'}})
        ref.seed('v3/persistent/'+ASSET,{'user':OWNER})
        for mode in ['missing','denied','throttled']:
            results={}
            for kind in ports:
                store.reset(mode)
                # HEAD access occurs before download signing; configured download endpoint stays on 4570.
                response=request(kind,'GET','/assets/v4/example.com/'+key,{'Z-User':OWNER}) if mode!='throttled' else request(kind,'DELETE','/assets/v4/example.com/'+key,{'Z-User':OTHER})
                results[kind]={'response':response,'s3_operations':list(store.trace),'injected':store.injected}
            records.append({'id':'s3-'+mode,'responses':results,'equal':results['haskell']==results['go']})
        for mode in ['part','complete']:upload('s3-'+mode,ref.multipart(),store=store)
    finally:
        store.shutdown();store.server_close()
    return records
