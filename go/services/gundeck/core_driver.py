#!/usr/bin/env python3
"""Run bounded reference/candidate diagnostics with the owned disposable stack."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import time
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/'tools/parity/docker'))
import environment as setup

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--output',required=True,type=Path)
parser.add_argument('--receipt',required=True,type=Path)
parser.add_argument('--candidate',required=True,type=Path)
parser.add_argument('--candidate-receipt',required=True,type=Path)
parser.add_argument('--four-go',action='store_true',help='execute original72 and supplements with all four Go services')
parser.add_argument('--select',nargs=3,action='append',default=[],metavar=('SERVICE','BINARY','RECEIPT'))
args=parser.parse_args()
selection=setup.identified_candidate(args.candidate,args.candidate_receipt)
peers=[]
if args.four_go:
    peers=[*args.select,('gundeck',str(args.candidate),str(args.candidate_receipt))]
    if {item[0] for item in peers}!={'proxy','cargohold','cannon','gundeck'}:raise ValueError('four exact selections required')
record=setup.prepare(args.output,args.receipt,None,None,peers)
record["side_candidate"]=selection
spec=json.loads((args.output/'compose.json').read_text())
spec['services']['runner']['volumes'].append({'type':'bind','source':str(args.candidate.resolve()),'target':'/candidate/gundeck','read_only':True})
spec['services']['runner']['entrypoint']=['/bin/bash','-c','for focus_python in /nix/store/*-python3-*/bin/python3; do if [[ -x "$$focus_python" ]]; then exec "$$focus_python" /source/go/services/gundeck/core_capture.py; fi; done; exit 2']
if args.four_go:
    selected={'gundeck':'/candidate/gundeck'}
    for name,path,receipt in args.select:
        spec['services']['runner']['volumes'].append({'type':'bind','source':str(Path(path).resolve()),'target':'/candidate/'+name,'read_only':True})
        selected[name]='/candidate/'+name
    spec['services']['runner']['environment']['WIRE_GUNDECK_FINAL_SELECTIONS']=json.dumps(selected)
    spec['services']['runner']['entrypoint']=[value.replace('core_capture.py','final_capture.py') for value in spec['services']['runner']['entrypoint']]

setup.validate_isolation(spec)
(args.output/'compose.json').write_text(json.dumps(spec,indent=2)+'\n')
record.update(purpose=('original72 with four Go services and Haskell peers, frozen91 and real switchback' if args.four_go else 'supplemental differential comparison; no original e2e or whole-service qualification'),compose_sha256=hashlib.sha256((args.output/'compose.json').read_bytes()).hexdigest())
compose=['docker','compose','-p',record['project'],'-f',str(args.output/'compose.json')]
try:
    setup.command([*compose,'up','--detach',*[n for n in setup.DEPENDENCIES if n!='redis-cluster']],timeout=240)
    deadline=time.monotonic()+90
    while True:
        try:
            setup.command([*compose,'exec','-T','redis-node-1','redis-cli','--tls','--cacert','/usr/local/etc/redis/ca.pem','-a','very-secure-redis-cluster-password','-h','172.20.0.36','-p','6378','ping'],timeout=10)
            break
        except subprocess.CalledProcessError:
            if time.monotonic()>deadline:raise RuntimeError('owned Redis unavailable')
            time.sleep(1)
    setup.command([*compose,'run','--rm','--no-deps','redis-cluster'],timeout=90)
    with (args.output/'focus.log').open('x') as log:
        process=subprocess.Popen([*compose,'run','--rm','--no-deps','runner'],stdout=log,stderr=subprocess.STDOUT)
        try:
            deadline=time.monotonic()+(1600 if args.four_go else 600)
            while not (args.output/'reports/seed-requested').exists():
                if process.poll() is not None:raise RuntimeError('runner exited before seed')
                if time.monotonic()>deadline:raise RuntimeError('runner initialization timed out')
                time.sleep(.5)
            seed=(setup.ROOT/'docs/go-migration/services/gundeck/oracle/storage-seed.cql').read_bytes()
            subprocess.run([*compose,'exec','-T','cassandra','cqlsh'],input=seed,check=True,timeout=30)
            (args.output/'reports/seed-complete').touch()
            process.wait(timeout=180)
            done=process
        finally:
            if process.poll() is None:
                process.terminate()
                try:process.wait(timeout=10)
                except subprocess.TimeoutExpired:process.kill();process.wait(timeout=10)
    if args.four_go and done.returncode==0:
        result=json.loads((args.output/'reports/final.json').read_text())
        if result['original']['result']!='pass' or result['supplemental']['result']!='pass':raise RuntimeError('final evidence failed')
        counts=[result['original']['suites'][name]['validated_report']['tests'] for name in ('proxy','cargohold','gundeck')]
        if counts!=[5,27,40] or len(result['supplemental']['comparisons'])!=91:raise RuntimeError('finite final matrix incomplete')
        record.update(e2e_executed=True,result='pass',original_tests=72,supplemental_checks=91,switchback=result['supplemental']['switchback'])
    record.update(process_exit_code=done.returncode,result=('pass' if args.four_go else 'diagnostic-executed') if done.returncode==0 else 'failed')
    if done.returncode:raise RuntimeError('focused diagnostic failed to execute')
finally:
    cleanup_cql=(setup.ROOT/'docs/go-migration/services/gundeck/oracle/storage-cleanup.cql').read_bytes()
    subprocess.run([*compose,'exec','-T','cassandra','cqlsh'],input=cleanup_cql,timeout=30)
    with (args.output/'dependencies.log').open('w') as log:
        subprocess.run([*compose,'logs','--no-color','--tail','100'],stdout=log,stderr=subprocess.STDOUT,timeout=30)
    cleanup=subprocess.run([*compose,'down','--volumes','--remove-orphans','--timeout','10'],capture_output=True,text=True,timeout=90)
    record['cleanup_exit_code']=cleanup.returncode
    (args.output/'environment.json').write_text(json.dumps(record,indent=2)+'\n')
    if cleanup.returncode:raise RuntimeError('owned stack cleanup failed')
print(json.dumps(record,indent=2))
