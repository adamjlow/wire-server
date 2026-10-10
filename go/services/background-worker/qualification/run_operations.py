#!/usr/bin/env python3
"""Owned Docker smoke using the exact qualified optional image; always clean up."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
ROOT=Path(__file__).resolve().parents[4]
sys.path.insert(0,str(ROOT/'tools/parity/docker'))
import environment
PYTHON='/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3'
SCRIPT='/source/go/services/background-worker/qualification/'
def call(args,timeout=90):
    result=subprocess.run(list(map(str,args)),capture_output=True,text=True,timeout=timeout)
    if result.returncode:raise ValueError('command failed: '+str(args[:4])+' '+result.stderr[-1000:])
    return result.stdout.strip()
def sample(container):
    pid=json.loads(call(['docker','inspect',container]))[0]['State']['Pid']
    base=Path('/proc')/str(pid)
    fields=(base/'stat').read_text().rsplit(')',1)[1].split()
    rss=int(re.search(r'^VmRSS:\s+(\d+) kB$',(base/'status').read_text(),re.M)[1])*1024
    return {'pid':pid,'rss_bytes':rss,'cpu_seconds':(int(fields[11])+int(fields[12]))/os.sysconf('SC_CLK_TCK'),'monotonic':time.monotonic()}
def main():
    parser=argparse.ArgumentParser()
    for name in ('output','runner-receipt','image-receipt','sink'):parser.add_argument('--'+name,type=Path,required=True)
    a=parser.parse_args();receipt=json.loads(a.image_receipt.read_text());image=receipt['image_id']
    if not re.fullmatch('sha256:[0-9a-f]{64}',image):raise ValueError('exact image required')
    record=environment.prepare(a.output,a.runner_receipt)
    sink=a.output/'fixtures/notification-sink';shutil.copyfile(a.sink,sink);sink.chmod(0o555)
    record['qualification_sink_sha256']=hashlib.sha256(sink.read_bytes()).hexdigest()
    compose=['docker','compose','--project-name',record['project'],'--file',a.output/'compose.json']
    helper=None;candidate=None;report={'result':'incomplete','environment':record,'image':receipt}
    try:
        call([*compose,'up','--detach',*environment.DEPENDENCIES],120)
        helper=call([*compose,'run','--detach','--no-deps','--entrypoint',PYTHON,'runner',SCRIPT+'namespace.py'])
        if not re.fullmatch('[0-9a-f]{64}',helper):raise ValueError('invalid owned namespace')
        deadline=time.monotonic()+900
        while not (a.output/'reports/namespace-ready').exists():
            state=json.loads(call(['docker','inspect',helper]))[0]['State']
            if not state['Running']:raise ValueError('setup exited '+str(state['ExitCode']))
            if time.monotonic()>deadline:raise ValueError('setup timed out')
            time.sleep(2)
        args=['docker','create','--name',record['project']+'-image','--label','wire.migration.owner='+record['project'],
              '--network','container:'+helper,'--read-only','--cap-drop=ALL','--security-opt=no-new-privileges',
              '--memory','1g','--cpus','2','--pids-limit','512','--mount','type=bind,source='+str(ROOT)+',target=/source,readonly']
        for name in ('background-worker','galley'):
            args.extend(['--mount','type=bind,source='+str(a.output/'reports/image-configs'/name/'conf')+',target=/etc/wire/'+name+'/conf,readonly'])
        for key,value in environment.SYNTHETIC_ENV.items():args.extend(['--env',key+'='+value])
        candidate=call([*args,image]);inspected=json.loads(call(['docker','inspect',candidate]))[0]
        if inspected['Config']['User']!='65534:65534' or inspected['Config']['Cmd'] or inspected['HostConfig']['PortBindings']:raise ValueError('default-image isolation mismatch')
        report['container_config']={key:inspected['Config'][key] for key in ('User','Entrypoint','Cmd')}
        call(['docker','start',candidate]);before=sample(candidate)
        result=subprocess.run(['docker','exec',helper,PYTHON,SCRIPT+'operations.py'],capture_output=True,text=True,timeout=130)
        (a.output/'reports/operations-process.json').write_text(json.dumps({'exit_code':result.returncode,'stdout':result.stdout,'stderr':result.stderr},indent=2)+'\n')
        if result.returncode:raise ValueError('operations failed; see process receipt')
        after=sample(candidate);report['resources']={'before':before,'after':after,'cpu_seconds_delta':after['cpu_seconds']-before['cpu_seconds']}
        report['smoke']=json.loads((a.output/'reports/operations-smoke.json').read_text())
        report['loaded_before']=json.loads((a.output/'reports/loaded-before.json').read_text())
        started=time.monotonic();call(['docker','kill','--signal','TERM',candidate]);code=call(['docker','wait',candidate],35);elapsed=time.monotonic()-started
        report['shutdown']={'exit_code':int(code),'seconds':elapsed,'configured_grace_seconds':30,'limit_seconds':35}
        if code!='0' or elapsed>35:raise ValueError('loaded shutdown failed')
        probe='import sys,json;sys.path.insert(0,"'+SCRIPT+'");from operations import queue,QUEUE;print(json.dumps(queue(QUEUE)))'
        deadline=time.monotonic()+10
        while True:
            state=json.loads(call(['docker','exec',helper,PYTHON,'-c',probe]))
            if state.get('messages_ready')==2 and state.get('messages_unacknowledged')==0:break
            if time.monotonic()>deadline:raise ValueError('unsent notifications not preserved')
            time.sleep(.2)
        report['loaded_after']=state;report['result']='pass'
    except Exception as error:
        report['error']=str(error)
        raise
    finally:
        for name,container in [('worker',candidate),('namespace',helper)]:
            if container:
                logs=subprocess.run(['docker','logs',container],capture_output=True,text=True,timeout=30)
                (a.output/'reports'/(name+'.log')).write_text(logs.stdout+logs.stderr)
        if candidate:call(['docker','rm','--force',candidate])
        (a.output/'reports/namespace-stop').touch()
        if helper:call(['docker','rm','--force',helper])
        cleanup=subprocess.run([*map(str,compose),'down','--volumes','--remove-orphans'],capture_output=True,text=True,timeout=120)
        report['cleanup_exit_code']=cleanup.returncode
        remaining={kind:call(['docker',kind,'ls','--quiet','--filter','label=com.docker.compose.project='+record['project']]) for kind in ('container','network','volume')}
        report['remaining_owned_resources']=remaining
        if cleanup.returncode or any(remaining.values()):report['result']='fail'
        (a.output/'result.json').write_text(json.dumps(report,indent=2)+'\n')
    if report['result']!='pass':raise ValueError('cleanup failed')
    print(json.dumps({'result':report['result'],'shutdown':report['shutdown'],'output':str(a.output)}))
if __name__=='__main__':main()
