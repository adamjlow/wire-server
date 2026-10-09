"""Real whole-process Haskell/Go/Haskell and one in-flight Go drain."""
import concurrent.futures, hashlib, json, os, shutil, subprocess, tempfile, threading, time, urllib.parse
from pathlib import Path
import http.server
import capture

class DelayedPeer(capture.Peer):
    def do_POST(self):
        if self.server.delay:
            self.server.entered.set();time.sleep(.3)
        super().do_POST()

def run(candidate):
    cfg=json.loads(Path('/fixtures/configs.json').read_text())['federator']
    cfg.update(federatorInternal={'host':'127.0.0.1','port':17297},federatorExternal={'host':'127.0.0.1','port':17298})
    cfg['brig']={'host':'127.0.0.1','port':17282}
    peer=http.server.ThreadingHTTPServer(('127.0.0.1',17282),DelayedPeer)
    peer.daemon_threads=True;peer.strategy='allowAll';peer.remotes=[];peer.calls=[];peer.status=200;peer.delay=False;peer.entered=threading.Event()
    threading.Thread(target=peer.serve_forever,daemon=True).start()
    headers={'Wire-Origin-Domain':'example.com','X-SSL-Certificate':urllib.parse.quote(Path(cfg['optSettings']['clientCertificate']).read_text(),safe=''),'Wire-Origin-Request-Id':'synthetic-switchback'}
    phases=[];process=None
    try:
        with tempfile.TemporaryDirectory(prefix='federator-switchback-') as tmp:
            conf=Path(tmp)/'config.json';conf.write_text(json.dumps(cfg))
            config_hash=hashlib.sha256(conf.read_bytes()).hexdigest()
            for index,binary in enumerate((shutil.which('federator'),str(candidate),shutil.which('federator'))):
                with Path('/reports/switchback-'+str(index)+'.log').open('w') as log:
                    process=subprocess.Popen([binary,'-c',str(conf)],cwd='/tmp/wire-e2e/services/federator',stdout=log,stderr=log)
                    deadline=time.monotonic()+15
                    while True:
                        if process.poll() is not None:raise ValueError('switchback service exited')
                        try:
                            if capture.request(17297,'/i/status')['status']==200:break
                        except OSError:pass
                        if time.monotonic()>deadline:raise ValueError('switchback startup timed out')
                        time.sleep(.1)
                    inward=capture.request(17298,'/federation/brig/echo','POST',b'synthetic-switchback',headers)
                    outward=capture.request(17297,'/rpc/example.com/brig/remote-success','POST',b'{}')
                    if inward['status']!=200 or outward['status']!=200 or outward['remote_ip']!='127.0.0.1':raise ValueError('switchback contract failed')
                    phase={'implementation':('go' if index==1 else 'haskell'),'config_sha256':config_hash,'binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest(),'incoming':inward,'outgoing':outward}
                    if index==1:
                        peer.delay=True
                        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
                            future=pool.submit(capture.request,17298,'/federation/brig/echo','POST',b'in-flight-synthetic',headers)
                            if not peer.entered.wait(timeout=3):raise ValueError('stream did not reach peer')
                            started=time.monotonic();process.terminate();code=process.wait(timeout=4);result=future.result(timeout=4)
                            phase['loaded_shutdown']={'exit_code':code,'seconds':round(time.monotonic()-started,3),'response':result}
                            if code!=0 or result['status']!=200:raise ValueError('loaded drain lost response')
                        peer.delay=False
                    else:process.terminate();phase['exit_code']=process.wait(timeout=4)
                    phases.append(phase)
                    if hashlib.sha256(conf.read_bytes()).hexdigest()!=config_hash:raise ValueError('switchback config mutated')
    finally:
        if process is not None and process.poll() is None:process.kill();process.wait(timeout=4)
        peer.shutdown();peer.server_close()
    return {'result':'pass','phases':phases}
