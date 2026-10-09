"""Finite real-process forwarding observations, only in owned nonroot runner."""
import argparse, base64, concurrent.futures, hashlib, http.client, http.server, json, os
from pathlib import Path
import shutil, subprocess, tempfile, threading, time, urllib.parse

class Peer(http.server.BaseHTTPRequestHandler):
    protocol_version='HTTP/1.1'
    def log_message(self,*args):pass
    def do_GET(self):
        body=json.dumps({'strategy':self.server.strategy,'remotes':self.server.remotes,'update_interval':10}).encode()
        self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
    def do_POST(self):
        data=self.rfile.read(int(self.headers.get('Content-Length','0')))
        self.server.calls.append({'path':self.path,'body':base64.b64encode(data).decode(),'headers':{k.lower():v for k,v in self.headers.items() if k.lower() in ('content-type','wire-origin-domain','request-id','x-wire-api-version','wire-origin-ip','authorization','x-unwanted')}})
        body=b'\x00\xffbinary-response'
        self.send_response(self.server.status);self.send_header('Content-Type','application/octet-stream');self.send_header('X-Unwanted','drop');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)

def request(port,path,method='GET',body=b'',headers=None):
    c=http.client.HTTPConnection('127.0.0.1',port,timeout=12)
    try:
        c.request(method,path,body,headers or {});r=c.getresponse();data=r.read()
        content=r.getheader('Content-Type');value={'status':r.status,'content_type':content,'body':base64.b64encode(data).decode(),'unwanted':r.getheader('X-Unwanted'),'remote_ip':r.getheader('Wire-Remote-IP')}
        try:value['json']=json.loads(data);del value['body']
        except (ValueError,UnicodeError):pass
        return value
    finally:c.close()

def run(binary):
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned nonroot runner required')
    cfg=json.loads(Path('/fixtures/configs.json').read_text())['federator']
    servers=[];records=[]
    for port in (17182,17184,17185):
        s=http.server.ThreadingHTTPServer(('127.0.0.1',port),Peer);s.daemon_threads=True;s.strategy='allowAll';s.remotes=[];s.calls=[];s.status=200
        threading.Thread(target=s.serve_forever,daemon=True).start();servers.append(s)
    cfg.update(federatorInternal={'host':'127.0.0.1','port':16197},federatorExternal={'host':'127.0.0.1','port':16198},brig={'host':'127.0.0.1','port':17182},cargohold={'host':'127.0.0.1','port':17184},galley={'host':'127.0.0.1','port':17185})
    def record(name,port,path,method='GET',body=b'',headers=None):records.append({'name':name,'observed':request(port,path,method,body,headers)})
    def timed_health():
        start=time.monotonic();v=request(16197,'/i/status?standalone')
        if v['status']!=200:raise ValueError('smoke failed')
        return (time.monotonic()-start)*1000
    process=None
    try:
        with tempfile.TemporaryDirectory(prefix='federator-capture-') as tmp:
            ca=Path(tmp)/'remote-ca.pem';original_ca=Path(cfg['optSettings']['remoteCAStore']).read_bytes();ca.write_bytes(original_ca);cfg['optSettings']['remoteCAStore']=str(ca)
            conf=Path(tmp)/'config.json';conf.write_text(json.dumps(cfg))
            with Path('/reports/federator-side.log').open('w') as log:
                process=subprocess.Popen([str(binary),'-c',str(conf)],cwd='/tmp/wire-e2e/services/federator',stdout=log,stderr=log)
                deadline=time.monotonic()+20
                while True:
                    if process.poll() is not None:raise ValueError('side federator exited')
                    try:
                        if request(16197,'/i/status?standalone')['status']==200:break
                    except OSError:pass
                    if time.monotonic()>deadline:raise ValueError('side startup timed out')
                    time.sleep(.1)
                record('internal-health',16197,'/i/status');record('external-health',16198,'/i/status');record('standalone',16197,'/i/status?standalone')
                for name,path,method in [('missing','/unknown','GET'),('component','/rpc/example.com/unknown/foo','POST'),('empty-rpc','/rpc/example.com/brig/','POST'),('bad-rpc','/rpc/example.com/brig/foo.bar','POST'),('extra-segment','/rpc/example.com/brig/foo/bar','POST'),('query','/rpc/example.com/brig/foo?x=1','POST'),('method','/rpc/example.com/brig/foo','GET')]:record(name,16197,path,method)
                for rpc in ('remote-success','remote-error','remote-text'):
                    record(rpc,16197,'/rpc/example.com/brig/'+rpc,'POST',b'{}',{'Wire-Origin-Domain':'example.com'})
                record('dns-missing',16197,'/rpc/missing.example.com/brig/foo','POST')
                cert=urllib.parse.quote(Path(cfg['optSettings']['clientCertificate']).read_text(),safe='')
                headers={'Wire-Origin-Domain':'example.com','X-SSL-Certificate':cert,'Wire-Origin-Request-Id':'synthetic-request','X-Wire-API-Version':'1','X-Forwarded-For':'unknown, [2001:db8::1]:443, 192.0.2.1','Authorization':'synthetic-drop','X-Unwanted':'drop'}
                for name,edit in [('missing-origin',{'Wire-Origin-Domain':None}),('missing-cert',{'X-SSL-Certificate':None}),('invalid-cert',{'X-SSL-Certificate':'invalid'}),('multiple-cert',{'X-SSL-Certificate':cert+cert}),('mismatch-cert',{'X-SSL-Certificate':urllib.parse.quote(Path('/source/services/federator/test/resources/unit/example.com.pem').read_text(),safe='')})]:
                    h=dict(headers)
                    for k,v in edit.items():
                        if v is None:h.pop(k)
                        else:h[k]=v
                    record(name,16198,'/federation/brig/foo','POST',b'{}',h)
                for component,peer in zip(('brig','cargohold','galley'),servers,strict=True):
                    record('inward-'+component,16198,'/federation/'+component+'/echo','POST',b'\x00\xffrequest',headers)
                    records.append({'name':'forward-'+component,'observed':peer.calls[-1] if peer.calls else None})
                servers[0].strategy='allowNone';record('denied-incoming',16198,'/federation/brig/foo','POST',b'{}',headers);record('denied-outgoing',16197,'/rpc/example.com/brig/foo','POST')
                servers[0].strategy='allowDynamic';record('dynamic-denied',16197,'/rpc/example.com/brig/foo','POST')
                servers[0].remotes=[{'domain':'example.com','search_policy':'full_search','restriction':{'tag':'allow_all','value':None}}]
                record('dynamic-allowed',16198,'/federation/brig/foo','POST',b'{}',headers)
                servers[0].strategy='allowAll';servers[1].status=418;record('inward-error',16198,'/federation/cargohold/foo','POST',b'{}',headers)
                ca.write_bytes(Path('/source/services/federator/test/resources/unit/unit-ca.pem').read_bytes())
                deadline=time.monotonic()+3
                while True:
                    value=request(16197,'/rpc/example.com/brig/remote-success','POST',b'{}')
                    if value['status']==525:break
                    if time.monotonic()>deadline:raise ValueError('reference did not reload changed CA')
                    time.sleep(.1)
                # Native OpenSSL/Go diagnostics differ; freeze status/label and no body
                # passthrough. Other errors, headers and values stay exact.
                value['json']['message']='<verified TLS failure>'
                records.append({'name':'untrusted-tls','observed':value})
                ca.write_bytes(original_ca);time.sleep(.4)
                record('restored-tls',16197,'/rpc/example.com/brig/remote-success','POST',b'{}')
                ca.write_bytes(b'not a CA');time.sleep(.4)
                record('invalid-reload-retains-context',16197,'/rpc/example.com/brig/remote-success','POST',b'{}')
                started=time.monotonic()
                with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                    timings=list(pool.map(lambda _: timed_health(),range(40)))
                rss=int(next(line.split()[1] for line in Path('/proc/'+str(process.pid)+'/status').read_text().splitlines() if line.startswith('VmRSS:')))
                records.append({'name':'smoke','observed':{'requests':40,'concurrency':2,'rss_kib':rss,'p95_ms':round(sorted(timings)[37],3)}})

                started=time.monotonic();process.terminate();code=process.wait(timeout=4)
                records.append({'name':'shutdown','observed':{'exit_code':code,'seconds':round(time.monotonic()-started,3)}})
    finally:
        if process is not None and process.poll() is None:process.kill();process.wait(timeout=4)
        for s in servers:s.shutdown();s.server_close()
    return {'kind':'finite-federator-forwarding','binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest(),'script_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'records':records}

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--binary',default=shutil.which('federator'));a=p.parse_args();print(json.dumps(run(a.binary),indent=2))
