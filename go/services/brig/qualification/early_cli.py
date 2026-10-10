#!/usr/bin/env python3
"""Identified initial CLI observations; not backend/API parity qualification."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import uuid

ROOT=Path(__file__).resolve().parents[4]
IMAGE='sha256:f487c90fd364aac9daee5a1ce6e60fdba0ab74ab0c671ab0de0198e8f05659a9'
NATIVE='/nix/store/f6l2f0l8l2r6z9jbg937q96k6056iqfn-brig/bin/brig'

def command(args):
    return subprocess.check_output(args,text=True,timeout=30).strip()

def observe(implementation,binary,args):
    name='wire-brig-cli-'+uuid.uuid4().hex
    cmd=['docker','run','--rm','--name',name,'--network=none','--read-only',
         '--cap-drop=ALL','--security-opt=no-new-privileges','--user','65534:65534',
         '--memory=256m','--cpus=1','--pids-limit=64',
         '--tmpfs','/tmp:rw,nosuid,nodev,size=32m,mode=1777']
    executable=NATIVE
    if implementation=='go':
        cmd+=['--mount','type=bind,src='+str(binary)+',dst=/candidate/brig,readonly']
        executable='/candidate/brig'
    cmd+=['--entrypoint',executable,IMAGE,*args]
    try:
        result=subprocess.run(cmd,text=True,capture_output=True,timeout=30)
        return {'implementation':implementation,'args':args,'exit_code':result.returncode,
                'stdout':result.stdout,'stderr':result.stderr}
    finally:
        subprocess.run(['docker','rm','--force',name],capture_output=True,timeout=15)
        if command(['docker','container','ls','--all','--quiet','--filter','name=^/'+name+'$']):
            raise ValueError('owned CLI container remains')

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary',type=Path,required=True)
    parser.add_argument('--build-receipt',type=Path,required=True)
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    binary=args.binary.resolve(strict=True);output=args.output.absolute()
    if output.exists() or output.is_relative_to(ROOT):raise ValueError('new external output required')
    if command(['git','-C',str(ROOT),'status','--porcelain']):raise ValueError('commit initial executable inputs first')
    revision=command(['git','-C',str(ROOT),'rev-parse','HEAD'])
    receipt=json.loads(args.build_receipt.read_text())
    checksum=hashlib.sha256(binary.read_bytes()).hexdigest()
    if receipt['source_revision']!=revision or receipt['binary_sha256']!=checksum or not re.fullmatch('[0-9a-f]{40}',revision):raise ValueError('candidate receipt mismatch')
    records=[]
    for case,expected in [(['--help'],0),(['migrate-postgres','--help'],0),(['--config-file','/tmp/nonexistent-synthetic-brig.yaml'],1)]:
        for implementation in ['haskell','go']:
            record=observe(implementation,binary,case);record['expected_exit_code']=expected
            record['pass']=record['exit_code']==expected
            if '--help' in case:
                text=record['stdout']+record['stderr']
                record['pass']=record['pass'] and all(word in text for word in (['config-file','migrate-postgres'] if case==['--help'] else ['dbname','reset']))
            records.append(record)
    report={'result':'pass' if all(r['pass'] for r in records) else 'failed',
            'scope':'Initial CLI help/config failure only; no health/API/e2e/state/operations/whole-service parity claim.',
            'e2e_executed':False,'source_revision':revision,'binary_sha256':checksum,
            'native_runner_image':IMAGE,'native_brig_path':NATIVE,'records':records,
            'owned_containers_removed':True}
    output.write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({'result':report['result'],'observations':len(records),'e2e_executed':False}))
    return 0 if report['result']=='pass' else 1

if __name__=='__main__':raise SystemExit(main())
