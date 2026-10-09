"""Keep owned dependency tunnels alive for image smoke after original suites exit."""
import json
import os
from pathlib import Path
import sys
import time

sys.path.insert(0, '/opt/wire-e2e')
import runtime

if os.getuid() != 65534 or os.environ.get('WIRE_E2E_DISPOSABLE') != '1':
    raise RuntimeError('owned private namespace required')
servers = runtime.start_forwards()
runtime.wait_dependencies()
peers = json.loads(Path('/fixtures/peers.json').read_text())['0']
out = Path('/reports/patch-image-configs')
out.mkdir()
for service, port in [('proxy', 18087), ('cargohold', 18084), ('cannon', 18083), ('gundeck', 18086)]:
    config = peers[service]
    config[service]['host'] = '127.0.0.1'
    config[service]['port'] = port
    (out / (service + '.json')).write_text(json.dumps(config) + '\n')
Path('/reports/patch-image-ready').touch()
deadline = time.monotonic() + 180
while not Path('/reports/patch-image-stop').exists():
    if time.monotonic() > deadline:
        raise RuntimeError('bounded image namespace expired')
    time.sleep(.1)
for server in servers:
    server.shutdown()
    server.server_close()
