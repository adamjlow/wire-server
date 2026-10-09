"""Launch and clean only task-owned internal DNS and nonroot reference runner."""
import argparse, hashlib, json, subprocess, sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/'tools/parity/docker'))
import environment as setup
p=argparse.ArgumentParser();p.add_argument('--output',type=Path,required=True);p.add_argument('--receipt',type=Path,required=True);p.add_argument('--candidate',type=Path,required=True);p.add_argument('--candidate-receipt',type=Path,required=True);a=p.parse_args()
r=setup.prepare(a.output,a.receipt,a.candidate,a.candidate_receipt);spec=json.loads((a.output/'compose.json').read_text())
spec['services']['runner']['entrypoint']=['/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3','/source/docs/go-migration/services/federator/oracle/reference_entry.py']
spec['services']['runner']['volumes'].append({'type':'bind','source':str(a.candidate.resolve()),'target':'/candidate/federator','read_only':True})
spec['services']['runner']['environment']['WIRE_FEDERATOR_BINARY']='/candidate/federator'
setup.validate_isolation(spec);(a.output/'compose.json').write_text(json.dumps(spec,indent=2)+'\n')
c=['docker','compose','-p',r['project'],'-f',str(a.output/'compose.json')]
try:
    setup.command([*c,'up','--detach','coredns'],timeout=60)
    with (a.output/'reference.log').open('w') as log:
        done=subprocess.run([*c,'run','--rm','--no-deps','runner'],stdout=log,stderr=subprocess.STDOUT,timeout=120)
    r.update(process_exit_code=done.returncode,result='pass' if done.returncode==0 else 'failed',reference_executed=True)
    if done.returncode:raise RuntimeError('candidate lane failed; inspect retained reference.log')
    actual=json.loads((a.output/'reports/forwarding.json').read_text());expected=json.loads((setup.ROOT/'docs/go-migration/services/federator/oracle/forwarding-reference.json').read_text())
    comparisons=[]
    for observed,control in zip(actual['records'],expected['records'],strict=True):
        if observed['name']!=control['name']:raise ValueError('frozen record order changed')
        name=observed['name'];value=observed['observed'];reference=control['observed']
        passed=value==reference
        if name=='shutdown':passed=value['exit_code']==0 and value['seconds']<=4
        if name=='smoke':passed=value['requests']==40 and value['concurrency']==2 and value['rss_kib']<=192*1024 and value['p95_ms']<=2*reference['p95_ms']+100
        comparisons.append({'name':name,'pass':passed})
    r.update(result='pass' if all(c['pass'] for c in comparisons) else 'failed',comparisons=comparisons)
    if r['result']!='pass':raise ValueError('frozen comparisons failed')
finally:
    done=subprocess.run([*c,'down','--volumes','--remove-orphans','--timeout','10'],capture_output=True,text=True,timeout=60)
    r['cleanup_exit_code']=done.returncode;r['compose_sha256']=hashlib.sha256((a.output/'compose.json').read_bytes()).hexdigest()
    (a.output/'environment.json').write_text(json.dumps(r,indent=2)+'\n')
    if done.returncode:raise RuntimeError('owned cleanup failed')
print(json.dumps(r,indent=2))
