"""Reuse the owned Docker fixture with only Cassandra for an early process proof."""
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
    for name in ('output','runner-receipt','candidate','candidate-receipt'):parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--suite',choices=('early','middleware','forms','signatures','tokens','idps','delete','login','scim-groups','scim-users','remaining-routes'),default='early')
    parser.add_argument('--signature-fixtures',type=Path)
    args=parser.parse_args()
    if args.suite=='signatures' and args.signature_fixtures is None:parser.error('signature fixtures executable required')
    selected=environment.identified_candidate(args.candidate,args.candidate_receipt)
    record=environment.prepare(args.output,args.runner_receipt)
    output=args.output.resolve();compose_path=output/'compose.json';spec=json.loads(compose_path.read_text())
    if args.suite in ('early','middleware','forms','signatures'):
        spec['services']={name:service for name,service in spec['services'].items() if name in ('cassandra','runner')}
        spec['networks']={'demo_wire':spec['networks']['demo_wire']}
    runner=spec['services']['runner']
    if args.suite in ('early','middleware','forms','signatures'):runner['networks']=['demo_wire']
    runner['entrypoint']=['/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3']
    runner['command']=['/source/go/services/spar/qualification/'+('early.py' if args.suite in ('early','middleware','forms','signatures') else 'tokens.py')]
    if args.suite=='middleware':runner['command']+=['--middleware']
    if args.suite=='forms':runner['command']+=['--forms']
    if args.suite=='signatures':
        runner['command']+=['--signatures']
        # Use the existing approved read-only /fixtures mount. Do not broaden
        # protected isolation rules to introduce another arbitrary host bind.
        fixture_dir=Path(tempfile.mkdtemp(prefix='wire-spar-signatures-',dir='/dev/shm'))
        fixture_dir.chmod(0o755)
        for fixture in (output/'fixtures').iterdir():shutil.copy2(fixture,fixture_dir/fixture.name)
        shutil.copy2(args.signature_fixtures.resolve(strict=True),fixture_dir/'signature-fixtures.test')
        for volume in runner['volumes']:
            if volume['type']=='bind' and volume['target']=='/fixtures':volume['source']=str(fixture_dir)
    if args.suite not in ('early','middleware','forms','signatures'):runner['command']+=['--suite',args.suite]
    runner['volumes'].append({'type':'bind','source':selected['path'],'target':'/candidate/spar','read_only':True})
    environment.validate_isolation(spec)
    compose_path.write_text(json.dumps(spec,indent=2)+'\n')
    if args.suite=='signatures':
        fixture_receipt=json.loads(args.candidate_receipt.read_text())['fixture_executable']
        if fixture_receipt['sha256']!=hashlib.sha256(args.signature_fixtures.read_bytes()).hexdigest():raise ValueError('signature fixture executable receipt mismatch')
        record['signature_fixture_executable']=fixture_receipt
    record.update(kind='early-native-Go-Spar-process' if args.suite in ('early','middleware','forms','signatures') else 'original-Go-Spar-token-tests',candidate=selected,compose_sha256=hashlib.sha256(compose_path.read_bytes()).hexdigest())
    compose=['docker','compose','-p',record['project'],'-f',str(compose_path)]
    code=1
    try:
        dependencies=['cassandra'] if args.suite in ('early','middleware','forms','signatures') else [name for name in spec['services'] if name not in ('runner','redis-cluster')]
        subprocess.run([*compose,'up','--detach',*dependencies],check=True,timeout=120,capture_output=True)
        if args.suite not in ('early','middleware','forms','signatures'):
            # Same dependency sequence as protected environment.run: creation
            # before Redis nodes are ready silently leaves native presence broken.
            deadline=time.monotonic()+90
            while True:
                ping=subprocess.run([*compose,'exec','-T','redis-node-1','redis-cli','--tls','--cacert','/usr/local/etc/redis/ca.pem','-a','very-secure-redis-cluster-password','-h','172.20.0.36','-p','6378','ping'],timeout=10,capture_output=True)
                if ping.returncode==0:break
                if time.monotonic()>deadline:raise ValueError('Redis nodes unavailable')
                time.sleep(1)
            with (output/'redis-cluster-bootstrap.log').open('w')as log:
                subprocess.run([*compose,'run','--rm','--no-deps','redis-cluster'],stdout=log,stderr=subprocess.STDOUT,check=True,timeout=90)
        with (output/'controller.log').open('w')as log:
            result=subprocess.run([*compose,'run','--rm','--no-deps','runner'],stdout=log,stderr=subprocess.STDOUT,timeout=240 if args.suite in ('early','middleware','forms','signatures') else 900)
        code=result.returncode;record['process_exit_code']=code
        observed=json.loads((output/'reports/result.json').read_text());record['result']=observed['result']
        if code!=0 or observed['result']!='pass':code=1
    finally:
        cleanup=subprocess.run([*compose,'down','--volumes','--remove-orphans','--timeout','10'],capture_output=True,text=True,timeout=90)
        record['cleanup_exit_code']=cleanup.returncode
        remaining={}
        for name,cmd in [('containers',['docker','ps','-aq']),('networks',['docker','network','ls','-q']),('volumes',['docker','volume','ls','-q'])]:
            remaining[name]=subprocess.check_output(cmd+['--filter','label=com.docker.compose.project='+record['project']],text=True).split()
        record['post_cleanup']=remaining
        if cleanup.returncode!=0 or any(remaining.values()):record['result']='fail';code=1
        (output/'environment.json').write_text(json.dumps(record,indent=2)+'\n')
        if args.suite=='signatures':shutil.rmtree(fixture_dir)
    print(json.dumps({'result':record['result'],'process_exit_code':record.get('process_exit_code'),'cleanup_exit_code':record['cleanup_exit_code'],'project':record['project']},indent=2));return code

if __name__=='__main__':raise SystemExit(main())
