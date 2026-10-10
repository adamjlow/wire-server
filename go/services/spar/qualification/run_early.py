"""Reuse the owned Docker fixture with only Cassandra for an early process proof."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT=Path(__file__).resolve().parents[4]
sys.path.insert(0,str(ROOT/'tools/parity/docker'))
import environment


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('output','runner-receipt','candidate','candidate-receipt'):parser.add_argument('--'+name,type=Path,required=True)
    args=parser.parse_args()
    selected=environment.identified_candidate(args.candidate,args.candidate_receipt)
    record=environment.prepare(args.output,args.runner_receipt)
    output=args.output.resolve();compose_path=output/'compose.json';spec=json.loads(compose_path.read_text())
    spec['services']={name:service for name,service in spec['services'].items() if name in ('cassandra','runner')}
    spec['networks']={'demo_wire':spec['networks']['demo_wire']}
    runner=spec['services']['runner'];runner['networks']=['demo_wire']
    runner['entrypoint']=['/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3']
    runner['command']=['/source/go/services/spar/qualification/early.py']
    runner['volumes'].append({'type':'bind','source':selected['path'],'target':'/candidate/spar','read_only':True})
    environment.validate_isolation(spec)
    compose_path.write_text(json.dumps(spec,indent=2)+'\n')
    record.update(kind='early-native-Go-Spar-process',candidate=selected,compose_sha256=hashlib.sha256(compose_path.read_bytes()).hexdigest())
    compose=['docker','compose','-p',record['project'],'-f',str(compose_path)]
    code=1
    try:
        subprocess.run([*compose,'up','--detach','cassandra'],check=True,timeout=90,capture_output=True)
        with (output/'controller.log').open('w')as log:
            result=subprocess.run([*compose,'run','--rm','--no-deps','runner'],stdout=log,stderr=subprocess.STDOUT,timeout=240)
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
    print(json.dumps({'result':record['result'],'process_exit_code':record.get('process_exit_code'),'cleanup_exit_code':record['cleanup_exit_code'],'project':record['project']},indent=2));return code

if __name__=='__main__':raise SystemExit(main())
