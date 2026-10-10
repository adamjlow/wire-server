"""Finite actual image smoke, executed only inside the owned runner namespace."""
import concurrent.futures
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.parse
import urllib.request
import uuid

REPORT=Path('/reports')
BASE='http://127.0.0.1:16189'
QUEUE='backend-notifications.operations.synthetic'

def request(path):
    with urllib.request.urlopen(BASE+path,timeout=5) as response:
        return response.status,response.read(1048577)

def rabbit(method,path,body=None):
    args=['curl','--fail','--silent','--show-error','--max-time','5',
          '--cacert','/source/deploy/dockerephemeral/rabbitmq-config/certificates/ca.pem',
          '--user','guest:alpaca-grapefruit','--request',method,
          '--header','Content-Type: application/json','https://localhost:15671/api/'+path]
    if body is not None:args.extend(['--data-binary','@-'])
    result=subprocess.run(args,input=None if body is None else json.dumps(body).encode(),capture_output=True,timeout=6)
    if result.returncode:raise ValueError('verified owned Rabbit request failed')
    return json.loads(result.stdout) if result.stdout else None

def queue(name):return rabbit('GET','queues/backendA/'+urllib.parse.quote(name,safe=''))
def publish(name,value):
    result=rabbit('POST','exchanges/backendA/amq.default/publish',{'routing_key':name,'properties':{'delivery_mode':2},'payload':json.dumps(value),'payload_encoding':'string'})
    if not result['routed']:raise ValueError('fixture job was not routed')

def metric(text,name,tag=None):
    key=name if tag is None else name+'{job_type="'+tag+'"}'
    for line in text.splitlines():
        if line.startswith(key+' '):return int(line.split()[-1])
    return 0

def run():
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned nonroot namespace required')
    deadline=time.monotonic()+60
    while True:
        try:
            if request('/i/status/workers')[0]==200:break
        except OSError:pass
        if time.monotonic()>deadline:raise ValueError('image readiness timed out')
        time.sleep(.1)
    initial=request('/i/metrics')[1].decode()
    started=time.monotonic()
    def health(_):
        begin=time.monotonic();status,body=request('/i/status')
        if status!=200 or body:raise ValueError('health contract mismatch')
        return time.monotonic()-begin
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        latencies=list(pool.map(health,range(100)))
    for i in range(10):
        payload={'team_id':str(uuid.uuid4()),'user_group_id':str(uuid.uuid4())}
        tag='sync-user-group' if i<5 else 'sync-user-group-and-channel'
        if i>=5:payload['conv_id']=str(uuid.uuid4())
        publish('background-jobs',{'id':str(uuid.uuid4()),'requestId':'synthetic-operations','payload':{'type':tag,'payload':payload}})
    while True:
        metrics=request('/i/metrics')[1].decode()
        counts={tag:metric(metrics,'wire_background_jobs_succeeded_total',tag)-metric(initial,'wire_background_jobs_succeeded_total',tag) for tag in ('sync-user-group','sync-user-group-and-channel')}
        state=queue('background-jobs')
        if counts=={'sync-user-group':5,'sync-user-group-and-channel':5} and metric(metrics,'wire_background_jobs_workers_busy')==0 and state.get('messages',0)==0:break
        if time.monotonic()-started>30:raise ValueError('declared jobs/health smoke exceeded30s')
        time.sleep(.1)
    for tag in counts:
        for counter in ('received','started'):
            if metric(metrics,'wire_background_jobs_'+counter+'_total',tag)-metric(initial,'wire_background_jobs_'+counter+'_total',tag)!=5:raise ValueError('job counter mismatch')
        for counter in ('failed','invalid'):
            if metric(metrics,'wire_background_jobs_'+counter+'_total',tag)!=metric(initial,'wire_background_jobs_'+counter+'_total',tag):raise ValueError('job failure counter changed')
    sorted_latency=sorted(latencies)
    smoke={'result':'pass','requests':100,'concurrency':8,'jobs':counts,'busy':0,'queue':state,'seconds':time.monotonic()-started,'latency_seconds':{'min':min(latencies),'max':max(latencies),'p50':sorted_latency[49],'p95':sorted_latency[94]},'metrics':metrics}
    (REPORT/'operations-smoke.json').write_text(json.dumps(smoke,indent=2)+'\n')
    rabbit('PUT','queues/backendA/'+QUEUE,{'durable':True,'auto_delete':False,'arguments':{'x-queue-type':'quorum','x-single-active-consumer':True}})
    notice={'ownDomain':'example.com','targetComponent':'galley','path':'qualification-delivery','body':'{}'}
    for _ in range(2):publish(QUEUE,notice)
    deadline=time.monotonic()+30
    while True:
        state=queue(QUEUE)
        with urllib.request.urlopen('http://127.0.0.1:16197/qualification/status',timeout=5) as response:entered=json.load(response)['entered']
        if entered>=1 and state.get('messages_unacknowledged',0)==1 and state.get('messages_ready',0)==1:break
        if time.monotonic()>deadline:raise ValueError('loaded notification not observed')
        time.sleep(.1)
    (REPORT/'loaded-before.json').write_text(json.dumps({'sink_entered':entered,'queue':state},indent=2)+'\n')

if __name__=='__main__':run()
