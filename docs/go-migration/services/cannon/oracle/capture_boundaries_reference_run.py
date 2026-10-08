#!/usr/bin/env python3
"""Finite uncovered cannon stimuli; private container with synthetic fixtures only.

The caller supplies an identified binary; source expectations and reference output
are frozen before Go consumes them. This starts only its own side process, fake
presence endpoint and synthetic temporary queue sockets. It never edits original
suite assertions or touches its users/queues. Final cleanup terminates owned PID.
"""
import base64
import concurrent.futures
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time

UID = '11111111-1111-4111-8111-111111111111'
PORT, PRESENCE_PORT = 16083, 16086
records, posts, entries = [], [], {}
fail_presence = False

def request(method, path, body=None, headers=None):
    conn = http.client.HTTPConnection('127.0.0.1', PORT, timeout=5)
    try:
        conn.request(method, path, body, headers or {})
        response = conn.getresponse(); data = response.read()
        try:value = json.loads(data)
        except ValueError:value = data.decode('utf8')
        return {'status':response.status,'content_type':response.getheader('Content-Type'),'body':value}
    finally:conn.close()

class Presence(http.server.BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_POST(self):
        value = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        posts.append(value)
        if not fail_presence:entries[value['device_id']] = value
        self.send_response(503 if fail_presence else 201);self.send_header('Content-Length','0');self.end_headers()
    def do_GET(self):
        data = json.dumps(list(entries.values())).encode()
        self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)

class WS:
    def __init__(self,path,conn='fixture',origin=None):
        self.sock = socket.create_connection(('127.0.0.1',PORT),timeout=5)
        # A real 16-byte key; fixed synthetic value makes no security identity.
        key = base64.b64encode(b'synthetic-key-16!').decode()
        headers = f'GET {path} HTTP/1.1\r\nHost: 127.0.0.1:{PORT}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\nZ-User: {UID}\r\nZ-Connection: {conn}\r\n'
        if origin:headers += 'Origin: '+origin+'\r\n'
        self.sock.sendall((headers+'\r\n').encode())
        data = b''
        while not data.endswith(b'\r\n\r\n'):data += self.exact(1)
        self.status = int(data.split(b' ')[1]);assert self.status == 101,data
    def exact(self,n):
        data = b''
        while len(data)<n:
            chunk = self.sock.recv(n-len(data))
            if not chunk:raise EOFError('socket closed')
            data += chunk
        return data
    def send(self,opcode,value):
        data = value.encode() if isinstance(value,str) else value
        assert len(data)<126
        mask=b'\x11\x22\x33\x44'
        self.sock.sendall(bytes([0x80|opcode,0x80|len(data)])+mask+bytes(x^mask[i%4] for i,x in enumerate(data)))
    def recv(self):
        a,b=self.exact(2);size=b&127
        if size==126:size=struct.unpack('!H',self.exact(2))[0]
        if size==127:size=struct.unpack('!Q',self.exact(8))[0]
        mask=self.exact(4) if b&128 else None
        data=self.exact(size)
        if mask:data=bytes(x^mask[i%4] for i,x in enumerate(data))
        if a&15==8:return {'opcode':8,'code':struct.unpack('!H',data[:2])[0] if len(data)>=2 else None,'reason':data[2:].decode()}
        try:value=json.loads(data)
        except ValueError:value=data.decode()
        return {'opcode':a&15,'payload':value}
    def close(self):self.sock.close()

def until(check):
    deadline=time.monotonic()+5
    while time.monotonic()<deadline:
        value=check()
        if value:return value
        time.sleep(.02)
    raise TimeoutError('bounded assertion did not become ready')

def record(name,value):records.append({'name':name,'observed':value})

def run(binary):
    global fail_presence
    if os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise RuntimeError('private environment required')
    config=json.loads(Path('/fixtures/peers.json').read_text())['0']['cannon']
    config['cannon'].update(host='127.0.0.1',port=PORT,externalHost='127.0.0.1')
    config['gundeck'].update(host='127.0.0.1',port=PRESENCE_PORT)
    config['wsOpts']={'activityTimeout':200000,'pongTimeout':200000}
    server=http.server.ThreadingHTTPServer(('127.0.0.1',PRESENCE_PORT),Presence)
    worker=threading.Thread(target=server.serve_forever,daemon=True);worker.start()
    sockets=[]
    with tempfile.TemporaryDirectory(prefix='cannon-boundaries-') as directory:
        path=Path(directory)/'config.json';path.write_text(json.dumps(config))
        log=open(Path(directory)/'process.log','w+')
        process=subprocess.Popen([binary,'-c',str(path)],stdout=log,stderr=subprocess.STDOUT)
        try:
            def ready():
                if process.poll() is not None:
                    log.seek(0);raise RuntimeError(log.read()[-2000:])
                try:return request('GET','/i/status')['status']==200
                except OSError:return False
            until(ready)
            def connect(path,conn='fixture',origin=None):
                value=WS(path,conn,origin);sockets.append(value);return value
            first=connect('/await?client=abcd',origin='https://synthetic.example.invalid')
            until(lambda:posts)
            record('origin-accepted',first.status)
            record('presence-registration',posts[-1])
            for opcode in (1,2,9):
                first.send(opcode,'ping');record('pong-'+str(opcode),first.recv())
            raw=' {"raw": [1, 2], "synthetic": true} '
            record('raw-push-http',request('POST',f'/i/push/{UID}/fixture',raw,{'Content-Type':'application/json'}))
            # Preserve raw bytes independently of semantic event comparisons.
            a,b=first.exact(2);payload=first.exact(b&127)
            record('raw-push-frame',{'opcode':a&15,'bytes':payload.decode()})
            second=connect('/websocket?client=abcd')
            until(lambda:len(posts)>=2)
            first.close();time.sleep(.05)
            record('replacement-push',request('POST',f'/i/push/{UID}/fixture',raw,{'Content-Type':'application/json'}))
            record('replacement-frame',second.recv())
            notif={'id':'22222222-2222-4222-8222-222222222222','payload':[{'type':'synthetic'}]}
            targets=[{'user_id':UID,'conn_id':x} for x in ('fixture','gone')]
            record('ordered-bulk',request('POST','/i/bulkpush',json.dumps({'bulkpush_req':[{'notification':notif,'targets':targets}]}),{'Content-Type':'application/json'}))
            record('typed-bulk-frame',second.recv())
            entries['remote-only']={'user_id':UID,'device_id':'remote-only','resource':f'http://127.0.0.1:{PORT}/i/push/{UID}/remote-only','client_id':None}
            record('remote-presence',request('HEAD',f'/i/presences/{UID}/remote-only'))
            record('missing-presence',request('HEAD',f'/i/presences/{UID}/missing'))
            record('missing-push',request('POST',f'/i/push/{UID}/missing','{}',{'Content-Type':'application/json'}))
            before=len(posts);fail_presence=True
            failed=connect('/await','failed-registration')
            record('failed-registration-close',failed.recv())
            record('failed-registration-attempts',len(posts)-before)
            record('failed-registration-push',request('POST',f'/i/push/{UID}/failed-registration','{}',{'Content-Type':'application/json'}))
            fail_presence=False
            for name,body in [('malformed-ack','{'),('missing-ack-field','{"type":"ack","data":{"delivery_tag":1}}'),('unexpected-full-sync','{"type":"ack_full_sync"}')]:
                event=connect('/v9/events');event.send(1,body);record(name,event.recv());event.close()
            idle=connect('/v9/events')
            queue_capture=subprocess.run([sys.executable,str(Path(__file__).with_name('capture_queue_flags.py'))],capture_output=True,text=True,check=True,timeout=5)
            record('temporary-queue-flags',json.loads(queue_capture.stdout)['temporary_queue_flags'])
            record('event-inactivity-ping',idle.recv());record('event-inactivity-close',idle.recv());idle.close()
            def smoke(index):
                started=time.monotonic();assert request('GET','/i/status')['status']==200
                return (time.monotonic()-started)*1000
            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:latencies=sorted(pool.map(smoke,range(40)))
            record('smoke',{'requests':40,'concurrency':2,'p95_ms':latencies[37],'rss_kib':int(next(x for x in Path(f'/proc/{process.pid}/status').read_text().splitlines() if x.startswith('VmRSS:')).split()[1])})
            until(lambda:'net_websocket_clients' in str(request('GET','/i/metrics')['body']))
            metrics=request('GET','/i/metrics')
            record('classic-gauge',{'status':metrics['status'],'lines':[x for x in metrics['body'].splitlines() if 'net_websocket_clients' in x]})
            event=connect('/v9/events')
            started=time.monotonic();process.send_signal(signal.SIGTERM)
            record('drain-classic-close',second.recv())
            closure=event.recv()
            while closure['opcode']==9:
                event.send(10,closure['payload']);closure=event.recv()
            record('drain-event-close',closure)
            code=process.wait(timeout=4);record('drain',{'exit_code':code,'seconds':time.monotonic()-started,'bound_seconds':4})
            return {'schema_version':1,'kind':'finite-uncovered-boundary-capture','binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest(),'config_sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'records':records,'result':'captured'}
        finally:
            for value in sockets:value.close()
            if process.poll() is None:
                process.terminate()
                try:process.wait(timeout=5)
                except subprocess.TimeoutExpired:process.kill();process.wait()
            log.close();server.shutdown();server.server_close();worker.join(timeout=2)

if __name__=='__main__':print(json.dumps(run(sys.argv[1]),indent=2))
