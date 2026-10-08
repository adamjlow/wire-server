"""Execute original72 unchanged, then frozen91 and real switchback in one stack."""
import json
from pathlib import Path
import os
import sys
sys.path.insert(0,'/opt/wire-e2e')
import runner
import runtime
from core_capture import core

original=runner.suite
supplemental=None

def suite(service,candidate,config,output,implementations=None):
    global supplemental
    report,code=original(service,candidate,config,output,implementations)
    if code:return report,code
    if service=='gundeck':
        supplemental,code=core(service,candidate,config,output,implementations)
        report['supplemental_result']=supplemental['result']
    return report,code

runner.suite=suite
selected=json.loads(os.environ['WIRE_GUNDECK_FINAL_SELECTIONS'])
if set(selected)!={'proxy','cargohold','cannon','gundeck'}:raise ValueError('final lane requires four Go services')
report,code=runtime.run('proxy-cargohold-cannon-gundeck',peer_candidates=[(name,Path(path)) for name,path in selected.items()])
Path('/reports/final.json').write_text(json.dumps({'original':report,'supplemental':supplemental,'exit_code':code},indent=2)+'\n')
print(json.dumps(report,indent=2))
raise SystemExit(code)
