"""Owned lightweight DNS/TLS reference lane; no database or production targets."""
import json, os, subprocess, sys
from pathlib import Path
sys.path.insert(0,'/opt/wire-e2e')
import runtime
import capture
servers=[]
try:
    servers=runtime.start_forwards();runtime.stage()
    conf=Path('/tmp/federator-tls.conf')
    root='/source/services/nginz/integration-test/conf/nginz/'
    conf.write_text('''worker_processes 1;
pid /tmp/federator-tls.pid;
error_log stderr warn;
events { worker_connections 128; }
http { access_log off; client_body_temp_path /tmp; proxy_temp_path /tmp;
server { listen 8443 ssl; http2 on;
ssl_certificate '''+root+'''integration-leaf.pem;
ssl_certificate_key '''+root+'''integration-leaf-key.pem;
ssl_client_certificate '''+root+'''integration-ca.pem;
ssl_verify_client on;
zauth_keystore /source/services/nginz/integration-test/resources/zauth/pubkeys.txt;
zauth_acl /source/services/nginz/integration-test/conf/nginz/zauth_acl.txt;
oauth_pub_key /source/services/nginz/integration-test/resources/oauth/ed25519_public.jwk;
location / { zauth off; default_type application/octet-stream; add_header X-Unwanted keep; return 200 'remote-body'; }
location /federation/brig/remote-error { zauth off; default_type application/json; return 400 '{"code":400,"label":"example","message":"synthetic remote failure"}'; }
location /federation/brig/remote-text { zauth off; default_type text/plain; return 418 'synthetic teapot'; }
}}
''')
    with Path('/reports/nginx-side.log').open('w') as log:
        p=subprocess.Popen(['nginx','-c',str(conf),'-g','daemon off;'],stdout=log,stderr=log)
        try:
            import socket, time
            deadline=time.monotonic()+5
            while True:
                if p.poll() is not None:raise ValueError('synthetic TLS ingress exited')
                try:
                    with socket.create_connection(('127.0.0.1',8443),timeout=1):break
                except OSError:
                    if time.monotonic()>deadline:raise ValueError('synthetic TLS ingress unavailable')
                    time.sleep(.1)
            output=capture.run(os.environ.get('WIRE_FEDERATOR_BINARY') or __import__('shutil').which('federator'))
            Path('/reports/forwarding.json').write_text(json.dumps(output,indent=2)+'\n')
        finally:p.terminate();p.wait(timeout=4)
finally:
    for s in servers:s.server_close()
