"""Launch and clean only task-owned internal DNS and nonroot reference runner."""
import argparse, hashlib, json, subprocess, sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[5]/'tools/parity/docker'))
import environment as setup
p=argparse.ArgumentParser();p.add_argument('--output',type=Path,required=True);p.add_argument('--receipt',type=Path,required=True);a=p.parse_args()
r=setup.prepare(a.output,a.receipt);spec=json.loads((a.output/'compose.json').read_text())
spec['services']['runner']['entrypoint']=['/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3','/source/docs/go-migration/services/federator/oracle/reference_entry.py']
setup.validate_isolation(spec);(a.output/'compose.json').write_text(json.dumps(spec,indent=2)+'\n')
c=['docker','compose','-p',r['project'],'-f',str(a.output/'compose.json')]
try:
    setup.command([*c,'up','--detach','coredns'],timeout=60)
    with (a.output/'reference.log').open('w') as log:
        done=subprocess.run([*c,'run','--rm','--no-deps','runner'],stdout=log,stderr=subprocess.STDOUT,timeout=120)
    r.update(process_exit_code=done.returncode,result='pass' if done.returncode==0 else 'failed',reference_executed=True)
    if done.returncode:raise RuntimeError('reference lane failed; inspect retained reference.log')
finally:
    done=subprocess.run([*c,'down','--volumes','--remove-orphans','--timeout','10'],capture_output=True,text=True,timeout=60)
    r['cleanup_exit_code']=done.returncode;r['compose_sha256']=hashlib.sha256((a.output/'compose.json').read_bytes()).hexdigest()
    (a.output/'environment.json').write_text(json.dumps(r,indent=2)+'\n')
    if done.returncode:raise RuntimeError('owned cleanup failed')
print(json.dumps(r,indent=2))
