"""Bounded private fixture actions; no production endpoints or oracle edits."""
import json
import os
from pathlib import Path
import subprocess
import sys
import urllib.request
import uuid
from operations import rabbit, publish, queue
QUEUE='backend-notifications.switchback.synthetic'
def action(name):
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned namespace required')
    if name=='queue':
        return {'notifications':queue(QUEUE),'jobs':queue('background-jobs')}
    if name=='seed-rabbit':
        rabbit('PUT','queues/backendA/'+QUEUE,{'durable':True,'auto_delete':False,'arguments':{'x-queue-type':'quorum','x-single-active-consumer':True}})
        for phase in ('first','second'):
            publish(QUEUE,{'ownDomain':'example.com','targetComponent':'galley','path':'qualification-delivery','body':json.dumps({'phase':phase})})
        return {'published_notifications':2}
    if name=='job':
        value={'id':str(uuid.uuid4()),'requestId':'synthetic-switchback','payload':{'type':'sync-user-group','payload':{'team_id':str(uuid.uuid4()),'user_group_id':str(uuid.uuid4())}}}
        publish('background-jobs',value);return {'published_job':value}
    if name=='sink':
        with urllib.request.urlopen('http://127.0.0.1:16197/qualification/status',timeout=5) as response:return json.load(response)
    if name=='accept':
        with urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:16197/qualification/accept',method='POST'),timeout=5) as response:return {'status':response.status}
    if name=='migration-on':
        path=Path('/reports/image-configs/background-worker/conf/background-worker.yaml')
        value=json.loads(path.read_text());value['migrateUsers']=True
        # Namespace user owns this configuration; temporarily restore owner write.
        path.chmod(0o644);path.write_text(json.dumps(value)+'\n');path.chmod(0o444)
        return {'migrateUsers':True}
    result=subprocess.run(['/fixtures/switchback-storage',name],capture_output=True,text=True,timeout=30)
    if result.returncode:raise ValueError('storage fixture failed: '+result.stderr[-600:])
    return json.loads(result.stdout) if result.stdout else {'action':name,'exit_code':0}
if __name__=='__main__':print(json.dumps(action(sys.argv[1])))
