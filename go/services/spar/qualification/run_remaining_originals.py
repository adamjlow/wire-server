"""Run the remaining193 unchanged original cases with seven exact Go peers."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import shutil
import tempfile
import sys
import time

ROOT=Path(__file__).resolve().parents[4]
sys.path.insert(0,str(ROOT/'tools/parity/docker'))
import environment


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('output','runner-receipt'):parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--select',action='append',nargs=3,required=True,metavar=('SERVICE','BINARY','RECEIPT'))
    args=parser.parse_args()
    if sorted(item[0]for item in args.select)!=sorted(('proxy','cargohold','cannon','gundeck','federator','background-worker','spar')):parser.error('exactly seven distinct service selections required')
    if shutil.disk_usage(args.output.parent).free<2*1024**3:raise ValueError('need at least 2 GiB free for disposable dependency data')
    record=environment.prepare(args.output,args.runner_receipt,peers=[(name,Path(binary),Path(receipt))for name,binary,receipt in args.select])
    selected=record['peer_candidates']
    output=args.output.resolve();compose_path=output/'compose.json';spec=json.loads(compose_path.read_text())
    for name in environment.DEPENDENCIES:
        inspection=json.loads(subprocess.check_output(['docker','image','inspect',spec['services'][name]['image']],text=True))[0]
        if not inspection.get('RepoDigests'):raise ValueError('dependency requires immutable digest')
        spec['services'][name]['image']=inspection['RepoDigests'][0]
    record['dependencies']={name:spec['services'][name]['image'] for name in environment.DEPENDENCIES}
    runner=spec['services']['runner'];runner['entrypoint']=['/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3']
    runner['command']=['/source/go/services/spar/qualification/remaining_originals.py']
    for name,identified in selected.items():runner['volumes'].append({'type':'bind','source':identified['path'],'target':'/candidate/'+name,'read_only':True})
    environment.validate_isolation(spec);compose_path.write_text(json.dumps(spec,indent=2)+'\n')
    record.update(kind='remaining-original193-seven-Go',compose_sha256=hashlib.sha256(compose_path.read_bytes()).hexdigest())
    compose=['docker','compose','-p',record['project'],'-f',str(compose_path)]
    code=1
    try:
        dependencies=[name for name in spec['services'] if name not in ('runner','redis-cluster')]
        subprocess.run([*compose,'up','--detach',*dependencies],check=True,timeout=120,capture_output=True)
        deadline=time.monotonic()+90
        while True:
            ping=subprocess.run([*compose,'exec','-T','redis-node-1','redis-cli','--tls','--cacert','/usr/local/etc/redis/ca.pem','-a','very-secure-redis-cluster-password','-h','172.20.0.36','-p','6378','ping'],timeout=10,capture_output=True)
            if ping.returncode==0:break
            if time.monotonic()>deadline:raise ValueError('Redis nodes unavailable')
            time.sleep(1)
        with (output/'redis-cluster-bootstrap.log').open('w')as log:subprocess.run([*compose,'run','--rm','--no-deps','redis-cluster'],stdout=log,stderr=subprocess.STDOUT,check=True,timeout=90)
        with (output/'controller.log').open('w')as log:completed=subprocess.run([*compose,'run','--rm','--no-deps','runner'],stdout=log,stderr=subprocess.STDOUT,timeout=1800)
        code=completed.returncode;record['process_exit_code']=code
        observed=json.loads((output/'reports/result.json').read_text());record['result']=observed['result']
        for name,identified in selected.items():
            if observed.get('implementations',{}).get(name,{}).get('sha256')!=identified['sha256']:raise ValueError('actual Go peer identity mismatch: '+name)
        if code or observed['result']!='pass':code=1
    except Exception as error:record.update(result='fail',error=str(error)[:300]);code=1
    finally:
        cleanup=subprocess.run([*compose,'down','--volumes','--remove-orphans','--timeout','10'],capture_output=True,text=True,timeout=90);record['cleanup_exit_code']=cleanup.returncode
        remaining={}
        for name,cmd in [('containers',['docker','ps','-aq']),('networks',['docker','network','ls','-q']),('volumes',['docker','volume','ls','-q'])]:remaining[name]=subprocess.check_output(cmd+['--filter','label=com.docker.compose.project='+record['project']],text=True).split()
        record['post_cleanup']=remaining
        if cleanup.returncode or any(remaining.values()):record['result']='fail';code=1
        (output/'environment.json').write_text(json.dumps(record,indent=2)+'\n')
    print(json.dumps({'result':record['result'],'project':record['project'],'cleanup_exit_code':record['cleanup_exit_code']}));return code


if __name__=='__main__':raise SystemExit(main())
