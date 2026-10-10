"""Run five original migration cases in a fresh native or Go worker namespace."""
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
    parser.add_argument('--implementation',choices=('native','go'),required=True)
    parser.add_argument('--candidate',type=Path);parser.add_argument('--candidate-receipt',type=Path)
    args=parser.parse_args()
    if args.implementation=='go' and (args.candidate is None or args.candidate_receipt is None):parser.error('Go worker executable and receipt required')
    selected=environment.identified_candidate(args.candidate,args.candidate_receipt) if args.implementation=='go' else None
    record=environment.prepare(args.output,args.runner_receipt)
    output=args.output.resolve();compose_path=output/'compose.json';spec=json.loads(compose_path.read_text())
    for name in environment.DEPENDENCIES:
        inspection=json.loads(subprocess.check_output(['docker','image','inspect',spec['services'][name]['image']],text=True))[0]
        if not inspection.get('RepoDigests'):raise ValueError('dependency requires immutable digest')
        spec['services'][name]['image']=inspection['RepoDigests'][0]
    record['dependencies']={name:spec['services'][name]['image'] for name in environment.DEPENDENCIES}
    runner=spec['services']['runner'];runner['entrypoint']=['/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3']
    runner['command']=['/source/go/services/spar/qualification/worker_codes.py','--implementation',args.implementation]
    if selected:runner['volumes'].append({'type':'bind','source':selected['path'],'target':'/candidate/background-worker','read_only':True})
    environment.validate_isolation(spec);compose_path.write_text(json.dumps(spec,indent=2)+'\n')
    record.update(kind='original-worker-codes-diagnosis',implementation=args.implementation,candidate=selected,compose_sha256=hashlib.sha256(compose_path.read_bytes()).hexdigest())
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
        with (output/'controller.log').open('w')as log:completed=subprocess.run([*compose,'run','--rm','--no-deps','runner'],stdout=log,stderr=subprocess.STDOUT,timeout=900)
        code=completed.returncode;record['process_exit_code']=code
        observed=json.loads((output/'reports/result.json').read_text());record['result']=observed['result']
        if selected and observed.get('implementations',{}).get('background-worker',{}).get('sha256')!=selected['sha256']:raise ValueError('actual Go worker identity mismatch')
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
