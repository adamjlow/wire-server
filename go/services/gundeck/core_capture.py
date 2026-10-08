"""Compare the runnable Cassandra core with the protected Haskell captures."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import tempfile
import http.client
sys.path.insert(0,'/opt/wire-e2e')
import runner
import runtime
sys.path.insert(0,'/source/docs/go-migration/services/gundeck/oracle')
import capture_storage
import capture_http_presence
import capture_native

if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':
    raise ValueError('private owned runner required')
ORACLE=Path('/source/docs/go-migration/services/gundeck/oracle')


def core(service,candidate,config,output,implementations=None):
    binary=Path('/candidate/gundeck')
    try:
        Path('/reports/seed-requested').touch()
        deadline=time.monotonic()+40
        while not Path('/reports/seed-complete').exists():
            if time.monotonic()>deadline:raise ValueError('owned CQL seed timed out')
            time.sleep(.2)
        actual=capture_storage.run(binary)
        Path('/reports/storage-go.json').write_text(json.dumps(actual,indent=2)+'\n')
        expected=json.loads((ORACLE/'storage-reference.json').read_text())
        comparisons=[]
        for observed,reference in zip(actual['records'],expected['records'],strict=True):
            if observed['name']!=reference['name']:raise ValueError('capture ordering changed')
            if observed['name']=='idle-shutdown':
                value=observed['observed'];passed=value['exit_code']==0 and value['seconds']<=4
            else:passed=observed['observed']==reference['observed']
            comparisons.append({'name':observed['name'],'pass':passed})
        # Run the unchanged HTTP/Redis capture against a second candidate process.
        cfg=json.loads(Path('/fixtures/peers.json').read_text())['0']['gundeck']
        cfg['gundeck']['port']=16186
        with tempfile.TemporaryDirectory(prefix='gundeck-http-core-') as temp:
            conf=Path(temp)/'config.json';conf.write_text(json.dumps(cfg))
            with Path('/reports/gundeck-http-side.log').open('w') as log:
                process=subprocess.Popen([str(binary),'-c',str(conf)],cwd=runtime.WORK/'services/gundeck',stdout=log,stderr=log)
                try:
                    deadline=time.monotonic()+30
                    while True:
                        if process.poll() is not None:raise ValueError('HTTP side process exited')
                        try:
                            connection=http.client.HTTPConnection('127.0.0.1',16186,timeout=1)
                            connection.request('GET','/i/status');response=connection.getresponse();response.read();connection.close()
                            if response.status==200:break
                        except OSError:pass
                        if time.monotonic()>deadline:raise ValueError('HTTP side process startup timed out')
                        time.sleep(.1)
                    http_actual=capture_http_presence.capture(16186)
                    Path('/reports/http-go.json').write_text(json.dumps(http_actual,indent=2)+'\n')
                    http_expected=json.loads((ORACLE/'http-presence-reference.json').read_text())
                    for observed,reference in zip(http_actual['records'],http_expected['records'],strict=True):
                        if observed['name']!=reference['name']:raise ValueError('HTTP ordering changed')
                        comparisons.append({'name':observed['name'],'pass':observed['observed']==reference['observed']})
                finally:
                    if process.poll() is None:process.terminate()
                    try:process.wait(timeout=4)
                    except subprocess.TimeoutExpired:process.kill();process.wait(timeout=4)
        native_actual=capture_native.run(binary)
        Path('/reports/native-go.json').write_text(json.dumps(native_actual,indent=2)+'\n')
        native_expected=json.loads((ORACLE/'native-reference.json').read_text())
        for observed,reference in zip(native_actual['records'],native_expected['records'],strict=True):
            if observed['name']!=reference['name']:raise ValueError('native ordering changed')
            name=observed['name'];value=observed['observed'];control=reference['observed']
            if name=='idle-shutdown':passed=value['exit_code']==0 and value['seconds']<=4
            elif name=='smoke':passed=value['requests']==40 and value['concurrency']==2 and value['rss_kib']<=192*1024 and value['p95_ms']<=2*control['p95_ms']+100
            elif name=='native-metric-types':passed=all(value.get(k)==v for k,v in control.items()) and set(value.values())=={'counter'}
            else:passed=value==control
            comparisons.append({'name':'native/'+name,'pass':passed})
        report={'result' :'pass' if all(c['pass'] for c in comparisons) else 'failed',
                'purpose':'frozen91 supplemental comparison; original72 and switchback still required',
                'e2e_executed':False,'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),
                'oracle_script_sha256':actual['script_sha256'],'http_script_sha256':http_actual['script_sha256'],'native_script_sha256':native_actual['script_sha256'],'comparisons':comparisons}
        Path('/reports/core.json').write_text(json.dumps(report,indent=2)+'\n')
        return report,0 if report['result']=='pass' else 1
    finally:Path('/reports/capture-complete').touch()
runner.suite=core
report,code=runtime.run('gundeck')
print(json.dumps(report,indent=2))
raise SystemExit(code)
