"""Finite image smoke and loaded requests, inside the owned runner namespace."""
import argparse
import concurrent.futures
import http.client
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import threading
import time

REPORT=Path('/reports')
PORT=8088


def request(method,path,body=None):
    client=http.client.HTTPConnection('127.0.0.1',PORT,timeout=5)
    try:
        client.request(method,path,body,{'Content-Type':'application/json','Request-Id':'spar-operations-synthetic'})
        response=client.getresponse();return response.status,response.read(1048577)
    finally:client.close()


def counts(text):
    result={}
    for line in text.splitlines():
        match=re.fullmatch(r'http_request_duration_seconds_count\{(.*)\} ([0-9.eE+-]+)',line)
        if match:
            labels=dict(re.findall(r'(\w+)="([^"]*)"',match[1]));result[(labels['handler'],labels['method'],labels['status_code'])]=int(float(match[2]))
    return result


def setup():
    sys.path.insert(0,'/opt/wire-e2e');import runtime
    forward=runtime.TCPServer(('127.0.0.1',9042),runtime.Forward);forward.destination=('cassandra',9042)
    threading.Thread(target=forward.serve_forever,daemon=True).start()
    try:
        deadline=time.monotonic()+120
        while True:
            try:
                with socket.create_connection(('cassandra',9042),timeout=1):break
            except OSError:pass
            if time.monotonic()>deadline:raise ValueError('Cassandra readiness timeout')
            time.sleep(.5)
        with (REPORT/'schema.log').open('w') as log:subprocess.run(['spar-schema','--keyspace','spar_test','--replication-factor','1','--reset'],stdout=log,stderr=subprocess.STDOUT,timeout=90,check=True)
        config=REPORT/'image-configs/spar/conf';config.mkdir(parents=True)
        (config/'spar.yaml').write_text(json.dumps(json.loads(Path('/fixtures/peers.json').read_text())['0']['spar']))
        (REPORT/'namespace-ready').touch()
        deadline=time.monotonic()+300
        while not (REPORT/'namespace-stop').exists():
            if time.monotonic()>deadline:raise ValueError('owned namespace exceeded finite lifetime')
            time.sleep(.1)
    finally:forward.server_close()


def smoke():
    deadline=time.monotonic()+45
    while True:
        try:
            if request('GET','/i/status')==(200,b''):break
        except OSError:pass
        if time.monotonic()>deadline:raise ValueError('default-config image readiness timeout')
        time.sleep(.1)
    before=counts(request('GET','/i/metrics')[1].decode())
    paths=['/i/status','/sso/settings','/scim/v2/ServiceProviderConfig']
    def probe(index):
        path=paths[index%3];begin=time.monotonic();status,body=request('GET',path)
        if status!=200:raise ValueError('image request status mismatch')
        if path=='/i/status' and body or path=='/sso/settings' and json.loads(body)!={'default_sso_code':None}:raise ValueError('image health/settings contract mismatch')
        if path.endswith('ServiceProviderConfig') and not json.loads(body).get('schemas'):raise ValueError('public SCIM metadata missing')
        return time.monotonic()-begin
    started=time.monotonic()
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:latencies=list(pool.map(probe,range(100)))
    metrics=request('GET','/i/metrics')[1].decode();after=counts(metrics)
    for path,expected in zip(paths,(34,33,33),strict=True):
        if after.get((path,'GET','200'),0)-before.get((path,'GET','200'),0)!=expected:raise ValueError('route counter mismatch '+path)
    ordered=sorted(latencies);elapsed=time.monotonic()-started
    if elapsed>30 or ordered[94]>5:raise ValueError('bounded image smoke exceeded declared limits')
    (REPORT/'operations-smoke.json').write_text(json.dumps({'result':'pass','requests':100,'concurrency':8,'seconds':elapsed,'latency_seconds':{'min':ordered[0],'p50':ordered[49],'p95':ordered[94],'max':ordered[-1]},'counter_deltas':dict(zip(paths,(34,33,33),strict=True)),'metrics':metrics},indent=2)+'\n')


def load():
    clients=[]
    try:
        body=b'{"default_sso_code":null}'
        for _ in range(8):
            client=http.client.HTTPConnection('127.0.0.1',PORT,timeout=15);clients.append(client)
            client.putrequest('PUT','/i/sso/settings');client.putheader('Content-Type','application/json');client.putheader('Content-Length',str(len(body)));client.putheader('Connection','close');client.putheader('Request-Id','spar-loaded-synthetic');client.endheaders();client.send(body[:-1])
        deadline=time.monotonic()+5
        while True:
            metrics=request('GET','/i/metrics')[1].decode();match=re.search(r'^net_connections ([0-9.]+)$',metrics,re.M)
            if match and float(match[1])>=9:break
            if time.monotonic()>deadline:raise ValueError('eight loaded image connections not observed')
            time.sleep(.01)
        (REPORT/'loaded-before.json').write_text(json.dumps({'requests':8,'pending_body_bytes_per_request':1,'metrics':metrics},indent=2)+'\n')
        deadline=time.monotonic()+15
        while not (REPORT/'release-loaded').exists():
            if time.monotonic()>deadline:raise ValueError('loaded requests not released after signal')
            time.sleep(.01)
        replies=[]
        for client in clients:
            client.send(body[-1:]);response=client.getresponse();replies.append({'status':response.status,'body':response.read(1048577).decode()})
        if replies!=[{'status':200,'body':''}]*8:raise ValueError('accepted requests did not drain successfully')
        (REPORT/'loaded-after.json').write_text(json.dumps({'result':'pass','replies':replies},indent=2)+'\n')
    finally:
        for client in clients:client.close()


def closed():
    deadline=time.monotonic()+5
    while True:
        try:
            with socket.create_connection(('127.0.0.1',PORT),timeout=.2):pass
        except ConnectionRefusedError:return
        if time.monotonic()>deadline:raise ValueError('listener remained open after SIGTERM')
        time.sleep(.01)


if __name__=='__main__':
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned nonroot disposable namespace required')
    parser=argparse.ArgumentParser();parser.add_argument('phase',choices=('setup','smoke','load','closed'));args=parser.parse_args()
    globals()[args.phase]()
