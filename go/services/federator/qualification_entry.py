import json, os, runpy, sys
from pathlib import Path
sys.path.insert(0,'/source/docs/go-migration/services/federator/oracle')
import capture
sys.path.insert(0,'/source/go/services/federator')
import switchback
original=capture.run

def qualified(binary):
    observations=original(binary)
    rollback=switchback.run(binary)
    Path('/reports/switchback.json').write_text(json.dumps(rollback,indent=2)+'\n')
    return observations
capture.run=qualified
runpy.run_path('/source/docs/go-migration/services/federator/oracle/reference_entry.py',run_name='__main__')
