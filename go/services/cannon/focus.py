"""Diagnose two original timing-sensitive cases; never qualify a whole service."""
import hashlib
import json
import os
from pathlib import Path
import sys
sys.path.insert(0, '/opt/wire-e2e')
import runner
import runtime

if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':
    raise RuntimeError('owned private runner required')
CASES=['Events.testEventsForSpecificClients','Events.testSingleConsumer']
original_suite=runner.suite
original_names=runner.names
candidate=Path('/candidate/cannon')
results=[]

def focused(service, unused_candidate, config, unused_output, implementations=None):
    runner.names=lambda name: CASES if name=='cannon' else original_names(name)
    for label in (('go',) if os.environ.get('WIRE_CANNON_FOCUS_CANDIDATE_ONLY')=='1' else ('haskell','go')):
        if label=='go':
            link=runtime.WORK/'dist/cannon';link.unlink();link.symlink_to(candidate)
        # These two unchanged tests start their own dynamic backend; static peers
        # remain Haskell in both phases. Do not describe this as all-Go selection.
        report,code=original_suite('cannon',None,config,Path('/reports')/(label+'-focus.xml'))
        results.append({'implementation':label,'dynamic_cannon_sha256':hashlib.sha256((runtime.WORK/'dist/cannon').read_bytes()).hexdigest(),'cases':CASES,'exit_code':code,'report':report})
    receipt={'purpose':'diagnostic-only; full original35/67 not executed here','results':results,'candidate_sha256':hashlib.sha256(candidate.read_bytes()).hexdigest()}
    Path('/reports/focus.json').write_text(json.dumps(receipt,indent=2)+'\n')
    return receipt,0
runner.suite=focused
report,code=runtime.run('cannon')
print(json.dumps(report,indent=2))
raise SystemExit(code)
