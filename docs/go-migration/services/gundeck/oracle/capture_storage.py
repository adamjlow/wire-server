"""Finite seeded-storage reference comparison in private owned runner only."""
import hashlib
import http.client
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

USER = '55555555-5555-4555-8555-555555555555'


def notif(number):
    return f'{number:08x}-0000-1000-8000-000000000001'


def run(binary):
    if os.environ.get('WIRE_E2E_DISPOSABLE') != '1':
        raise ValueError('requires private owned fixture')
    records = []
    config = json.loads(Path('/fixtures/peers.json').read_text())['0']['gundeck']
    config['gundeck']['port'] = 16186
    config['settings']['internalPageSize'] = 2
    config['settings']['maxPayloadLoadSize'] = 25
    config['logLevel'] = 'Warn'
    with tempfile.TemporaryDirectory(prefix='gundeck-storage-') as temp:
        conf = Path(temp)/'config.json';conf.write_text(json.dumps(config))
        def request(name, path, method='GET'):
            conn = http.client.HTTPConnection('127.0.0.1', 16186, timeout=5)
            try:
                conn.request(method, path, headers={'Z-User': USER, 'Request-Id': 'gundeck-storage-fixture'})
                response = conn.getresponse();raw = response.read(65537)
                if len(raw)>65536:raise ValueError('fixture response exceeded bound')
                try:value=json.loads(raw)
                except (ValueError,UnicodeDecodeError):value=raw.decode()
                if isinstance(value,dict) and 'time' in value:
                    from datetime import datetime,timezone
                    stamp=datetime.fromisoformat(value['time'].replace('Z','+00:00'))
                    if abs((datetime.now(timezone.utc)-stamp).total_seconds())>10:raise ValueError('clock outside fixture bound')
                    value['time']='<validated-current-UTC-second>'
                observed={'status':response.status,'content_type':response.getheader('Content-Type'),'body':value}
                if name:records.append({'name':name,'observed':observed})
                return observed
            finally:conn.close()
        with open('/reports/gundeck-storage-side.log','w') as log:
            process=subprocess.Popen([str(binary),'-c',str(conf)],cwd='/tmp/wire-e2e/services/gundeck',stdout=log,stderr=log)
            try:
                deadline=time.monotonic()+30
                while True:
                    if process.poll() is not None:raise ValueError('side gundeck exited during startup')
                    try:
                        if request(None,'/i/status')['status']==200:break
                    except OSError:pass
                    if time.monotonic()>deadline:raise ValueError('side gundeck readiness failed')
                    time.sleep(.1)
                for name,path in [
                    ('page-byte-limit','/v9/notifications'),
                    ('page-byte-limit-client','/v9/notifications?client=abcd'),
                    ('last-all','/v9/notifications/last'),
                    ('last-client-scans-skipped-rows','/v9/notifications/last?client=abcd'),
                    ('referenced-payload','/v9/notifications/'+notif(4)),
                    ('missing-referenced-payload','/v9/notifications/'+notif(5)),
                    ('malformed-inline-payload','/v9/notifications/'+notif(6)),
                    ('null-payload','/v9/notifications/'+notif(7)),
                    ('wrong-client-notification','/v9/notifications/'+notif(3)+'?client=abcd'),
                    ('since-filtered-client','/v9/notifications?since='+notif(3)+'&client=abcd'),
                ]:request(name,path)
                request('user-cleanup','/i/user','DELETE')
                request('after-user-cleanup','/v9/notifications')
                started=time.monotonic();process.terminate();process.wait(timeout=4)
                records.append({'name':'idle-shutdown','observed':{'exit_code':process.returncode,'seconds':time.monotonic()-started,'bound_seconds':4}})
            finally:
                if process.poll() is None:
                    process.terminate()
                    try:process.wait(timeout=4)
                    except subprocess.TimeoutExpired:process.kill();process.wait(timeout=4)
    return {'schema_version':1,'kind':'finite-seeded-storage-characterization','binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'script_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'config_sha256':hashlib.sha256(json.dumps(config).encode()).hexdigest(),'records':records,'cleanup':'side process reaped, own synthetic notifications removed via user cleanup; fixture payload row removed by outer CQL cleanup'}


if __name__=='__main__':
    import argparse
    parser=argparse.ArgumentParser();parser.add_argument('--binary',type=Path)
    args=parser.parse_args();binary=(args.binary or Path(shutil.which('gundeck'))).resolve(strict=True)
    print(json.dumps(run(binary),indent=2))
