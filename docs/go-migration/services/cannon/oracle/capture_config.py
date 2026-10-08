"""Bounded invalid-config and CLI observations against an identified binary."""
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

if os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise RuntimeError('private fixture required')
binary=sys.argv[1]
base=json.loads(Path('/fixtures/peers.json').read_text())['0']['cannon']
cases=[]
for name,key,value in [('connection-cap','rabbitMqMaxConnections',0),('channel-cap','rabbitMqMaxChannels',0)]:
    config=copy.deepcopy(base);config[key]=value;cases.append((name,config))
for name,key in [('drain-grace','gracePeriodSeconds'),('drain-interval','millisecondsBetweenBatches')]:
    config=copy.deepcopy(base);config['drainOpts'][key]=0;cases.append((name,config))
config=copy.deepcopy(base);config['cannon'].pop('externalHost',None);config['cannon'].pop('externalHostFile',None);cases.append(('missing-advertised-host',config))
config=copy.deepcopy(base);config.pop('notificationTTL');cases.append(('missing-notification-ttl',config))
records=[]
with tempfile.TemporaryDirectory(prefix='cannon-config-reference-') as directory:
    for name,config in cases:
        path=Path(directory)/'config.json';path.write_text(json.dumps(config))
        process=subprocess.run([binary,'-c',str(path)],capture_output=True,text=True,timeout=15)
        if not process.returncode:raise RuntimeError('invalid fixture unexpectedly accepted')
        # Exception/runtime backtraces vary by compiler; retain the concrete first
        # diagnostic separately from the startup rejection assertion.
        lines=(process.stdout+process.stderr).splitlines()
        records.append({'name':name,'rejected':True,'exit_code':process.returncode,'diagnostic':lines[0] if lines else ''})
    process=subprocess.run([binary,'--help'],capture_output=True,text=True,timeout=5)
    records.append({'name':'cli-help','exit_code':process.returncode,'mentions_config':('-c' in process.stdout),'default_path_visible':('/etc/wire/cannon/conf/cannon.yaml' in process.stdout)})
print(json.dumps({'schema_version':1,'kind':'reference-config-characterization','binary_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest(),'records':records,'result':'captured'},indent=2))
