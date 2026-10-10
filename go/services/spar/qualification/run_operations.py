#!/usr/bin/env python3
"""Qualify the exact optional Spar image in an owned private Cassandra namespace."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

ROOT=Path(__file__).resolve().parents[4]
sys.path.insert(0,str(ROOT/'tools/parity/docker'))
import environment
PYTHON='/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3'
SCRIPT='/source/go/services/spar/qualification/operations.py'


def call(args,timeout=90):return subprocess.check_output(list(map(str,args)),stderr=subprocess.PIPE,text=True,timeout=timeout).strip()


def sample(container):
    inspected=json.loads(call(['docker','inspect',container]))[0]
    if not inspected['State']['Running']:raise ValueError('image process not live during sample')
    pid=inspected['State']['Pid'];base=Path('/proc')/str(pid);fields=(base/'stat').read_text().rsplit(')',1)[1].split()
    rss=int(re.search(r'^VmRSS:\s+(\d+) kB$',(base/'status').read_text(),re.M)[1])*1024
    return {'pid':pid,'rss_bytes':rss,'cpu_seconds':(int(fields[11])+int(fields[12]))/os.sysconf('SC_CLK_TCK'),'monotonic':time.monotonic()}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('output','runner-receipt','image-receipt'):parser.add_argument('--'+name,type=Path,required=True)
    a=parser.parse_args();receipt=json.loads(a.image_receipt.read_text());image=receipt['image_id']
    if not re.fullmatch('sha256:[a-f0-9]{64}',image):raise ValueError('immutable image required')
    record=environment.prepare(a.output,a.runner_receipt);compose_path=a.output/'compose.json';spec=json.loads(compose_path.read_text())
    spec['services']={name:service for name,service in spec['services'].items() if name in ('cassandra','runner')};spec['networks']={'demo_wire':spec['networks']['demo_wire']}
    runner=spec['services']['runner'];runner['networks']=['demo_wire'];runner['entrypoint']=[PYTHON];runner['command']=[SCRIPT,'setup'];environment.validate_isolation(spec);compose_path.write_text(json.dumps(spec,indent=2)+'\n')
    compose=['docker','compose','--project-name',record['project'],'--file',compose_path]
    helper=None;candidate=None;loader=None;report={'result':'incomplete','environment':record,'image':receipt};reports=a.output/'reports'
    def run_phase(phase):
        result=subprocess.run(['docker','exec',helper,PYTHON,SCRIPT,phase],capture_output=True,text=True,timeout=60)
        (reports/(phase+'-process.json')).write_text(json.dumps({'exit_code':result.returncode,'stdout':result.stdout,'stderr':result.stderr},indent=2)+'\n')
        if result.returncode:raise ValueError('image '+phase+' failed; retained process receipt')
    try:
        call([*compose,'up','--detach','cassandra'],120)
        helper=call([*compose,'run','--detach','--no-deps','runner'])
        if not re.fullmatch('[a-f0-9]{64}',helper):raise ValueError('owned namespace identity missing')
        deadline=time.monotonic()+220
        while not (reports/'namespace-ready').exists():
            state=json.loads(call(['docker','inspect',helper]))[0]['State']
            if not state['Running']:raise ValueError('namespace setup exited '+str(state['ExitCode']))
            if time.monotonic()>deadline:raise ValueError('namespace setup timed out')
            time.sleep(.5)
        config=reports/'image-configs/spar/conf/spar.yaml';report['config_sha256']=hashlib.sha256(config.read_bytes()).hexdigest()
        candidate=call(['docker','create','--name',record['project']+'-spar-image','--label','com.docker.compose.project='+record['project'],'--label','wire.migration.owner='+record['project'],'--network','container:'+helper,'--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--memory','256m','--cpus','1','--pids-limit','128','--mount','type=bind,source='+str(config.parent)+',target=/etc/wire/spar/conf,readonly',image])
        inspected=json.loads(call(['docker','inspect',candidate]))[0]
        if inspected['Config']['User']!='65534:65534' or inspected['Config']['Entrypoint']!=['/spar'] or inspected['Config']['Cmd'] or inspected['HostConfig']['PortBindings'] or not inspected['HostConfig']['ReadonlyRootfs']:raise ValueError('default image isolation mismatch')
        report['container_config']={key:inspected['Config'][key] for key in ('User','Entrypoint','Cmd')}
        call(['docker','start',candidate]);before=sample(candidate);run_phase('smoke');after=sample(candidate)
        cpu=after['cpu_seconds']-before['cpu_seconds'];report['resources']={'before':before,'after':after,'cpu_seconds_delta':cpu,'limits':{'rss_bytes':256*1024*1024,'smoke_cpu_seconds':10,'container_cpus':1}}
        if cpu>10 or max(before['rss_bytes'],after['rss_bytes'])>=256*1024*1024:raise ValueError('declared resource smoke exceeded limits')
        report['smoke']=json.loads((reports/'operations-smoke.json').read_text())
        loader=subprocess.Popen(['docker','exec',helper,PYTHON,SCRIPT,'load'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        deadline=time.monotonic()+10
        while not (reports/'loaded-before.json').exists():
            if loader.poll()is not None:raise ValueError('loaded fixture exited before observation')
            if time.monotonic()>deadline:raise ValueError('loaded fixture timeout')
            time.sleep(.02)
        report['loaded_before']=json.loads((reports/'loaded-before.json').read_text());started=time.monotonic();call(['docker','kill','--signal','TERM',candidate]);run_phase('closed')
        if not json.loads(call(['docker','inspect',candidate]))[0]['State']['Running']:raise ValueError('image exited before accepted requests were released')
        (reports/'release-loaded').touch();stdout,stderr=loader.communicate(timeout=20)
        (reports/'load-process.json').write_text(json.dumps({'exit_code':loader.returncode,'stdout':stdout,'stderr':stderr},indent=2)+'\n')
        if loader.returncode:raise ValueError('accepted image requests failed while draining')
        code=call(['docker','wait',candidate],35);elapsed=time.monotonic()-started
        report['shutdown']={'exit_code':int(code),'seconds':elapsed,'grace_seconds':30,'limit_seconds':35,'listener_closed_before_release':True,'image_live_before_release':True}
        if code!='0' or elapsed>35:raise ValueError('loaded shutdown failed')
        report['loaded_after']=json.loads((reports/'loaded-after.json').read_text());report['result']='pass'
    except Exception as error:report.update(result='fail',error=str(error)[:500])
    finally:
        (reports/'release-loaded').touch()
        if loader and loader.poll()is None:
            try:loader.communicate(timeout=20)
            except subprocess.TimeoutExpired:loader.terminate();loader.communicate(timeout=5)
        for name,container in [('spar-image',candidate),('namespace',helper)]:
            if container:
                logs=subprocess.run(['docker','logs',container],capture_output=True,text=True,timeout=30);(reports/(name+'.log')).write_text(logs.stdout+logs.stderr)
        if candidate:call(['docker','rm','--force',candidate])
        (reports/'namespace-stop').touch()
        if helper:
            report['namespace_exit_code']=int(call(['docker','wait',helper],15));call(['docker','rm',helper])
            if report['namespace_exit_code']!=0:report['result']='fail'
        cleanup=subprocess.run(list(map(str,[*compose,'down','--volumes','--remove-orphans','--timeout','10'])),capture_output=True,text=True,timeout=90)
        report['cleanup_exit_code']=cleanup.returncode
        remaining={kind:call(['docker',kind,'ls','--quiet','--filter','label=com.docker.compose.project='+record['project']]).split() for kind in ('container','network','volume')};report['post_cleanup']=remaining
        if cleanup.returncode or any(remaining.values()):report['result']='fail'
        (a.output/'result.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({'result':report['result'],'shutdown':report.get('shutdown'),'cleanup_exit_code':report['cleanup_exit_code'],'project':record['project']}));return 0 if report['result']=='pass' else 1


if __name__=='__main__':raise SystemExit(main())
