"""Private dependencies/configuration for the one declared operations smoke."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time
sys.path.insert(0, '/opt/wire-e2e')
import runtime
if os.getuid() != 65534 or os.environ.get('WIRE_E2E_DISPOSABLE') != '1':
    raise ValueError('owned nonroot namespace required')
servers=[]
helper=None
try:
    servers=runtime.start_forwards()
    runtime.wait_dependencies()
    runtime.stage()
    runtime.initialize()
    configs=json.loads(Path('/fixtures/peers.json').read_text())['0']
    worker=configs['background-worker']
    worker['backgroundWorker']['port']=16189
    worker['federatorInternal']['port']=16197
    out=Path('/reports/image-configs')
    for name,config in [('background-worker',worker),('galley',configs['galley'])]:
        directory=out/name/'conf';directory.mkdir(parents=True)
        path=directory/(name+'.yaml');path.write_text(json.dumps(config)+'\n');path.chmod(0o444)
    helper=subprocess.Popen(['/fixtures/notification-sink'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    Path('/reports/namespace-ready').touch()
    deadline=time.monotonic()+600
    while not Path('/reports/namespace-stop').exists():
        if helper.poll() is not None:raise ValueError('notification sink exited')
        if time.monotonic()>deadline:raise ValueError('bounded namespace expired')
        time.sleep(.1)
finally:
    if helper is not None and helper.poll() is None:
        helper.terminate();helper.wait(timeout=5)
    for server in servers:server.shutdown();server.server_close()
