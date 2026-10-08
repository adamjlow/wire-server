"""Read-only characterization of synthetic original-test broker queues."""
import base64
import json
import os
import ssl
import urllib.request

if os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise RuntimeError('private fixture required')
context=ssl.create_default_context(cafile='/source/deploy/dockerephemeral/rabbitmq-config/certificates/ca.pem')
# Python 3.13 adds optional strict extension checking. The upstream synthetic
# CA predates that extension requirement; retain required chain and hostname
# verification, matching the qualified curl/Haskell fixture clients.
context.verify_flags &= ~ssl.VERIFY_X509_STRICT
assert context.verify_mode == ssl.CERT_REQUIRED and context.check_hostname
credentials=(os.environ['RABBITMQ_USERNAME']+':'+os.environ['RABBITMQ_PASSWORD']).encode()
request=urllib.request.Request('https://localhost:15671/api/queues',headers={'Authorization':'Basic '+base64.b64encode(credentials).decode()})
with urllib.request.urlopen(request,context=context,timeout=5) as response:queues=json.load(response)
flags=[]
temporary=[]
for queue in queues:
    if queue['name'].startswith('user-notifications.'):
        value={key:queue[key] for key in ('durable','auto_delete','exclusive','arguments')}
        if value not in flags:flags.append(value)
for queue in queues:
    if queue['exclusive'] and queue['auto_delete']:
        value={key:queue[key] for key in ('durable','auto_delete','exclusive','arguments')}
        if value not in temporary:temporary.append(value)
print(json.dumps({'temporary_queue_flags':temporary,'schema_version':1,'kind':'read-only-original-queue-flags','client_queue_flags':flags,'result':'captured' if flags else 'unavailable'},indent=2))
