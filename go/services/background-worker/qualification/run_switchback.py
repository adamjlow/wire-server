#!/usr/bin/env python3
"""Actual Haskell-Go-Haskell owners, one unchanged private storage namespace."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import time
from run_operations import ROOT, SCRIPT, PYTHON, call, environment
NATIVE='/nix/store/16b8f2naixj573wx924avn2y36mn0yvv-background-worker/bin/background-worker'
USER='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1'
RETURNING='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2'

def main():
    parser=argparse.ArgumentParser()
    for name in ('output','runner-receipt','image-receipt','sink','storage'):parser.add_argument('--'+name,type=Path,required=True)
    a=parser.parse_args();receipt=json.loads(a.image_receipt.read_text());image=receipt['image_id']
    if not re.fullmatch('sha256:[0-9a-f]{64}',image):raise ValueError('exact candidate image required')
    record=environment.prepare(a.output,a.runner_receipt)
    record['qualification_artifacts']={}
    for name,path in [('notification-sink',a.sink),('switchback-storage',a.storage)]:
        target=a.output/'fixtures'/name;shutil.copyfile(path,target);target.chmod(0o555)
        record['qualification_artifacts'][name]=hashlib.sha256(target.read_bytes()).hexdigest()
    compose=['docker','compose','--project-name',record['project'],'--file',a.output/'compose.json']
    helper=None;owner=None;owners=[];snapshots={};report={'result':'incomplete','environment':record,'candidate':receipt,'owners':owners,'snapshots':snapshots}
    def state(action):return json.loads(call(['docker','exec',helper,PYTHON,SCRIPT+'state.py',action],40))
    def snapshot(name):
        value={'storage':state('snapshot'),'rabbit':state('queue'),'sink':state('sink')};snapshots[name]=value
        (a.output/'reports/switchback-progress.json').write_text(json.dumps(report,indent=2)+'\n');return value
    def await_value(action,check,seconds=30):
        deadline=time.monotonic()+seconds
        while True:
            value=state(action)
            if check(value):return value
            if time.monotonic()>deadline:raise ValueError('state did not converge: '+action)
            time.sleep(.2)
    def start(kind,label):
        nonlocal owner
        if owner is not None:raise ValueError('multiple workload owners forbidden')
        args=['docker','create','--name',record['project']+'-'+label,'--label','wire.migration.owner='+record['project'],
              '--network','container:'+helper,'--user','65534:65534','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges',
              '--memory','2g','--cpus','2','--pids-limit','512','--tmpfs','/tmp:rw,nosuid,nodev,size=128m,mode=1777','--workdir','/tmp',
              '--mount','type=bind,source='+str(ROOT)+',target=/source,readonly']
        for name in ('background-worker','galley'):
            args.extend(['--mount','type=bind,source='+str(a.output/'reports/image-configs'/name/'conf')+',target=/etc/wire/'+name+'/conf,readonly'])
        for key,value in environment.SYNTHETIC_ENV.items():args.extend(['--env',key+'='+value])
        if kind=='haskell':args.extend(['--entrypoint',NATIVE,record['runner_image_id'],'-c','/etc/wire/background-worker/conf/background-worker.yaml','--galley-config-file','/etc/wire/galley/conf/galley.yaml'])
        else:args.append(image)
        owner=call(args);call(['docker','start',owner]);owners.append({'label':label,'implementation':kind,'container_id':owner})
        deadline=time.monotonic()+60
        probe='import urllib.request; r=urllib.request.urlopen("http://127.0.0.1:16189/i/status",timeout=2); assert r.status==200 and r.read()==b""'
        while True:
            status=json.loads(call(['docker','inspect',owner]))[0]['State']
            if not status['Running']:raise ValueError(label+' exited during startup')
            attempt=subprocess.run(['docker','exec',helper,PYTHON,'-c',probe],capture_output=True,timeout=5)
            if attempt.returncode==0:return
            if time.monotonic()>deadline:raise ValueError(label+' readiness timed out')
            time.sleep(.2)
    def stop(label):
        nonlocal owner
        started=time.monotonic();call(['docker','kill','--signal','TERM',owner]);code=int(call(['docker','wait',owner],35))
        owners[-1]['shutdown']={'exit_code':code,'seconds':time.monotonic()-started}
        logs=subprocess.run(['docker','logs',owner],capture_output=True,text=True,timeout=30)
        (a.output/'reports'/(label+'.log')).write_text(logs.stdout+logs.stderr)
        call(['docker','rm',owner]);owner=None
        if code!=0:raise ValueError(label+' did not exit cleanly')
    try:
        call([*compose,'up','--detach',*environment.DEPENDENCIES],120)
        helper=call([*compose,'run','--detach','--no-deps','--entrypoint',PYTHON,'runner',SCRIPT+'namespace.py','switchback'])
        if not re.fullmatch('[0-9a-f]{64}',helper):raise ValueError('invalid owned namespace')
        deadline=time.monotonic()+900
        while not (a.output/'reports/namespace-ready').exists():
            if not json.loads(call(['docker','inspect',helper]))[0]['State']['Running']:raise ValueError('namespace setup exited')
            if time.monotonic()>deadline:raise ValueError('namespace setup timeout')
            time.sleep(2)
        hash_script='import hashlib;print(hashlib.sha256(open("'+NATIVE+'","rb").read()).hexdigest())'
        report['reference_binary']={'path':NATIVE,'sha256':call(['docker','exec',helper,PYTHON,'-c',hash_script]),'image':record['runner_image_id']}
        if report['reference_binary']['sha256']!='985aab36d8e92baa59eb3f8da9851f7f2b7be35f201fe2cea63ddf395d350cb0':raise ValueError('reference binary changed')
        start('haskell','haskell-first')
        # First owner bootstraps native Arbiter schema; seed native durable state.
        state('seed');state('seed-rabbit');state('job')
        call(['docker','exec','--detach',helper,PYTHON,SCRIPT+'hold.py'])
        deadline=time.monotonic()+10
        while not (a.output/'reports/lock-ready').exists():
            if time.monotonic()>deadline:raise ValueError('row lock not acquired')
            time.sleep(.1)
        state('activate-meeting')
        await_value('snapshot',lambda v:any(j['attempts']==1 and j['claimed_by'] and j['not_visible_until'] for j in v['meeting_jobs']))
        await_value('queue',lambda v:v['notifications'].get('messages_unacknowledged')==1 and v['notifications'].get('messages_ready')==1 and v['jobs'].get('messages',0)==0)
        first=snapshot('haskell-claimed')
        if not first['storage']['pending'] or USER not in first['storage']['cassandra_users'] or len(first['storage']['conversation_jobs'])!=1:raise ValueError('handover durable prerequisites absent')
        stop('haskell-first')
        await_value('queue',lambda v:v['notifications'].get('messages_ready')==2 and v['notifications'].get('messages_unacknowledged')==0)
        stopped=snapshot('haskell-stopped')
        if not stopped['storage']['meeting_jobs']:raise ValueError('inflight scheduled job not preserved')
        state('migration-on');state('release-scheduled');state('job')
        start('go','go-middle')
        prior_attempts=stopped['storage']['meeting_jobs'][0]['attempts']
        claimed=await_value('snapshot',lambda v:any(j['attempts']>prior_attempts and j['claimed_by'] for j in v['meeting_jobs']))
        snapshots['go-claimed-storage']=claimed
        if claimed['meeting_jobs'][0]['claimed_by']==first['storage']['meeting_jobs'][0]['claimed_by']:raise ValueError('scheduled owner did not change')
        (a.output/'reports/lock-release').touch()
        completed=await_value('snapshot',lambda v:not v['meeting_jobs'] and not v['conversation_jobs'] and not v['meetings'] and not v['codes'] and not v['pending'] and USER not in v['cassandra_users'])
        if completed['users'][0]['name']!='synthetic preserved partial copy' or not completed['meeting_archive'] or not completed['conversation_archive']:raise ValueError('Go durable effects/partial copy mismatch')
        await_value('sink',lambda v:v['entered']>=2)
        await_value('queue',lambda v:v['jobs'].get('messages',0)==0 and v['notifications'].get('messages_unacknowledged')==1)
        snapshot('go-completed-storage-notification-held');stop('go-middle')
        await_value('queue',lambda v:v['notifications'].get('messages_ready')==2 and v['notifications'].get('messages_unacknowledged')==0)
        state('seed-return');state('activate-meeting');state('job');state('accept')
        snapshot('go-stopped-haskell-return-pending')
        start('haskell','haskell-return')
        await_value('snapshot',lambda v:not v['meeting_jobs'] and not v['meetings'] and not v['codes'] and not v['pending'] and not v['cassandra_users'] and len(v['users'])==2)
        await_value('queue',lambda v:v['notifications'].get('messages',0)==0 and v['jobs'].get('messages',0)==0)
        final=snapshot('haskell-return-completed')
        accepted=[json.loads(x['body'])['phase'] for x in final['sink']['deliveries'] if x['accepted']]
        if sorted(accepted)!=['first','second'] or final['sink']['entered']<4:raise ValueError('notification delivery/redelivery effects mismatch')
        migrated={x['id']:x for x in final['storage']['users']}
        if migrated[USER]['name']!='synthetic preserved partial copy' or migrated[RETURNING]['name']!='synthetic Haskell continuation':raise ValueError('return Haskell storage compatibility mismatch')
        stop('haskell-return');report['result']='pass'
    except Exception as error:
        report['error']=str(error);raise
    finally:
        (a.output/'reports/lock-release').touch()
        if owner:
            logs=subprocess.run(['docker','logs',owner],capture_output=True,text=True,timeout=30)
            (a.output/'reports/failed-owner.log').write_text(logs.stdout+logs.stderr)
            call(['docker','rm','--force',owner])
        if helper:
            logs=subprocess.run(['docker','logs',helper],capture_output=True,text=True,timeout=30)
            (a.output/'reports/namespace.log').write_text(logs.stdout+logs.stderr)
            call(['docker','rm','--force',helper])
        cleanup=subprocess.run([*map(str,compose),'down','--volumes','--remove-orphans'],capture_output=True,text=True,timeout=120)
        report['cleanup_exit_code']=cleanup.returncode
        remaining={kind:call(['docker',kind,'ls','--quiet','--filter','label=com.docker.compose.project='+record['project']]) for kind in ('container','network','volume')}
        report['remaining_owned_resources']=remaining
        if cleanup.returncode or any(remaining.values()):report['result']='fail'
        (a.output/'result.json').write_text(json.dumps(report,indent=2)+'\n')
    if report['result']!='pass':raise ValueError('owned cleanup failed')
    print(json.dumps({'result':'pass','output':str(a.output),'owners':owners}))
if __name__=='__main__':main()
