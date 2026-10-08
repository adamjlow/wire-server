"""Finite operations fixture run beside the owned synthetic native capture."""
import concurrent.futures
import gzip
import http.client
import json
from pathlib import Path
import subprocess
import tempfile
import time


def run(port, binary, config, pid):
    records=[]
    def req(method,path,body=None,headers=None):
        conn=http.client.HTTPConnection('127.0.0.1',port,timeout=5)
        try:
            conn.request(method,path,body,headers or {})
            r=conn.getresponse();raw=r.read(1048577)
            if len(raw)>1048576:raise ValueError('operations response bound exceeded')
            if r.getheader('Content-Encoding')=='gzip':raw=gzip.decompress(raw)
            try:parsed=json.loads(raw)
            except (ValueError,UnicodeDecodeError):parsed=raw.decode()
            return {'status':r.status,'content_type':r.getheader('Content-Type'),'content_encoding':r.getheader('Content-Encoding'),'body':parsed}
        finally:conn.close()
    def record(name,value):records.append({'name':name,'observed':value})
    result=req('POST','/i/push/v2',gzip.compress(b'[]'),{'Content-Type':'application/json','Content-Encoding':'gzip'})
    record('gzip-request',result)
    metrics=req('GET','/i/metrics')
    # Runtime GC/thread/counter values are implementation-specific; retain names/types
    # for the service native outcome counters and a real public route response.
    types={}
    for line in metrics['body'].splitlines():
        if line.startswith('# TYPE push_native_'):
            _,_,name,kind=line.split();types[name]=kind
    # Haskell unsafeRegister counters are lazy: only exercised outcomes appear.
    if metrics['status']!=200 or not types or set(types.values())!={'counter'}:
        raise ValueError('native metrics surface missing: '+str(metrics['status'])+' '+json.dumps(types))
    record('native-metric-types',types)
    started=time.monotonic()
    def once(_):
        before=time.monotonic();value=req('GET','/i/status')
        if value['status']!=200:raise ValueError('smoke request failed')
        return (time.monotonic()-before)*1000
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:latencies=list(pool.map(once,range(40)))
    rss=next(int(line.split()[1]) for line in Path('/proc/'+str(pid)+'/status').read_text().splitlines() if line.startswith('VmRSS:'))
    record('smoke',{'requests':40,'concurrency':2,'p95_ms':sorted(latencies)[37],'rss_kib':rss})
    with tempfile.TemporaryDirectory(prefix='gundeck-config-') as tmp:
        for name,keys in [('missing-bind',['gundeck']),('missing-redis',['redis']),('missing-aws',['aws']),('missing-rabbit',['rabbitmq']),('missing-ttl',['settings','notificationTTL'])]:
            value=json.loads(json.dumps(config));target=value
            for key in keys[:-1]:target=target[key]
            target.pop(keys[-1]);path=Path(tmp)/'config.json';path.write_text(json.dumps(value))
            result=subprocess.run([str(binary),'-c',str(path)],cwd='/tmp/wire-e2e/services/gundeck',capture_output=True,text=True,timeout=5)
            record(name,{'exit_code':result.returncode,'rejected':result.returncode!=0})
    help_result=subprocess.run([str(binary),'--help'],capture_output=True,text=True,timeout=5)
    record('cli-help',{'exit_code':help_result.returncode,'mentions_config':'--config-file' in help_result.stdout,'default_path_visible':'/etc/wire/gundeck/conf/gundeck.yaml' in help_result.stdout})
    return records
