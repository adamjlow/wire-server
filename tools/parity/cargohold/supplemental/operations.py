"""One bounded 40-request/concurrency-two smoke plus source-visible application metrics."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import http.client
import json
import math
from pathlib import Path
import re
import threading
import time
from urllib.parse import urlsplit
from cases import OWNER, DATA


def run(ref,ports,start,stop,request,store_effects,hs,go):
    records=[];samples={};metrics={};peaks={};large=b'x'*(24*1024*1024)
    for kind in ports:
        stop(hs);stop(go);hs=start('haskell');go=start('go')
        process=hs if kind=='haskell' else go
        running=threading.Event();running.set();peak=[0]
        def sample_memory():
            while running.is_set():
                try:
                    text=Path('/proc/'+str(process.pid)+'/status').read_text()
                    match=re.search(r'^VmRSS:\s+([0-9]+) kB$',text,re.M)
                    if match:peak[0]=max(peak[0],int(match[1])*1024)
                except FileNotFoundError:pass
                time.sleep(.01)
        monitor=threading.Thread(target=sample_memory,daemon=True);monitor.start()
        _,_,before=ref.request('GET','/i/metrics',port=ports[kind])
        headers={'Z-Bot':OWNER,'Z-Type':'bot','Content-Type':'multipart/mixed; boundary=wire','Request-Id':'operations-'+kind}
        begin=time.monotonic();status,h,body=ref.request('POST','/bot/assets',headers,ref.multipart(payload=large),port=ports[kind]);elapsed=time.monotonic()-begin
        if status!=201:raise ValueError('24 MiB operational upload failed')
        asset=json.loads(body);key=asset['key'];path='/bot/assets/'+key
        rows=[{'operation':'24MiB-upload','status':status,'seconds':elapsed}]
        fault_lock=threading.Lock();consecutive=[0]
        def call(index):
            method,route=('HEAD','/i/status') if index<20 else ('GET',path)
            begin=time.monotonic();status,_,_=ref.request(method,route,{'Z-Bot':OWNER,'Z-Type':'bot','Request-Id':'load-'+str(index)},port=ports[kind]);elapsed=time.monotonic()-begin
            expected=200 if index<20 else 302
            with fault_lock:
                consecutive[0]=consecutive[0]+1 if status>=500 else 0
                if consecutive[0]>=3:raise ValueError('operational smoke aborted after three unexpected 5xx')
            if status!=expected:raise ValueError('unexpected load response '+str(status))
            return {'operation':'health' if index<20 else 'asset-read','status':status,'seconds':elapsed}
        with ThreadPoolExecutor(max_workers=2) as pool:rows.extend(pool.map(call,range(39)))
        if len(rows)!=40:raise ValueError('operational request count differs')
        # Fetch the actual signed 24 MiB object with bounded streaming and verify every byte.
        status,h,_=ref.request('GET',path,{'Z-Bot':OWNER,'Z-Type':'bot'},port=ports[kind])
        url=urlsplit(h.get('location',''))
        if status!=302 or url.scheme!='http' or url.netloc!='localhost:4570':raise ValueError('operational signed URL escaped fixture')
        connection=http.client.HTTPConnection('127.0.0.1',4570,timeout=15)
        try:
            connection.request('GET',url.path+'?'+url.query,headers={'Host':url.netloc});response=connection.getresponse()
            digest=hashlib.sha256();total=0
            while True:
                chunk=response.read(65536)
                if not chunk:break
                total+=len(chunk)
                if total>len(large):raise ValueError('signed read larger than uploaded object')
                digest.update(chunk)
            if response.status!=200 or total!=len(large) or digest.hexdigest()!=hashlib.sha256(large).hexdigest():raise ValueError('signed 24 MiB bytes differ')
        finally:connection.close()
        _,_,after=ref.request('GET','/i/metrics',port=ports[kind])
        running.clear();monitor.join(timeout=2)
        Path('/reports/metrics-'+kind+'-before.txt').write_bytes(before)
        Path('/reports/metrics-'+kind+'-after.txt').write_bytes(after)
        def counters(raw):
            return {m[1]:float(m[2]) for line in raw.decode().splitlines() if (m:=re.fullmatch(r'(net_s3_upload_ok|net_s3_upload_size) ([0-9.eE+-]+)',line))}
        a,b=counters(before),counters(after)
        delta={name:b.get(name,0)-a.get(name,0) for name in ['net_s3_upload_ok','net_s3_upload_size']}
        if delta!={'net_s3_upload_ok':1,'net_s3_upload_size':len(large)}:raise ValueError('S3 application counters differ from workload')
        # HELP/TYPE and HTTP instrument families are operational interfaces; GC/heap identity is excluded.
        stable={}
        for line in after.decode().splitlines():
            match=re.match(r'# (HELP|TYPE) ([^ ]+) (.*)',line)
            if match and (match[2].startswith('http_') or match[2].startswith('net_s3_') or match[2]=='aws_auth_token_secs_remaining'):
                stable[match[1]+' '+match[2]]=match[3]
        samples[kind]=rows;metrics[kind]={'definitions':stable,'s3_delta':delta};peaks[kind]=peak[0]
        records.append({'id':'operations-'+kind,'requests':40,'concurrency':2,'signed_24MiB_bytes_verified':True,'peak_rss_bytes':peak[0],'equal':True})
    p95={kind:sorted(row['seconds'] for row in rows)[math.ceil(.95*len(rows))-1] for kind,rows in samples.items()}
    records.append({'id':'operational-bounds','p95_seconds':p95,'go_rss_limit_bytes':192*1024*1024,
                    'equal':peaks['go']<=192*1024*1024 and p95['go']<=2*p95['haskell']+.1})
    records.append({'id':'application-metrics-shape','responses':metrics,'equal':metrics['haskell']==metrics['go']})
    Path('/reports/operational-samples.json').write_text(json.dumps({'samples':samples,'p95_seconds':p95,'peak_rss_bytes':peaks},indent=2)+'\n')
    return records
