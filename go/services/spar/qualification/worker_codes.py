"""Bounded unchanged original code-migration diagnosis, native or Go worker."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
sys.path.insert(0,'/opt/wire-e2e')
import harness
import runner
import runtime


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--implementation',choices=('native','go'),required=True);a=parser.parse_args()
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned nonroot namespace required')
    tests=[name for name in runner.discover('background-worker') if name.startswith('Migration.ConversationCodes.')]
    if len(tests)!=5:raise ValueError('expected four original variants and expiration original')
    output={'kind':'original-code-migration-diagnosis','implementation':a.implementation,'required_tests':tests,'result':'running','e2e_executed':False,'scope':'Five unchanged originals with all Haskell peers; bounded attribution only, not full225 acceptance.'}
    processes=[];logs=[];servers=[]
    try:
        servers=runtime.start_forwards();runtime.wait_dependencies();config=runtime.stage();runtime.initialize()
        if a.implementation=='go':
            link=runtime.WORK/'dist/background-worker';link.unlink();link.symlink_to('/candidate/background-worker')
        output['implementations']={link.name:{'implementation':'go' if a.implementation=='go' and link.name=='background-worker' else 'haskell','path':str(link.resolve(strict=True)),'sha256':hashlib.sha256(link.read_bytes()).hexdigest()} for link in (runtime.WORK/'dist').iterdir() if link.name not in ('integration','nginx')}
        runtime.start_peers(processes,logs);os.chdir(runtime.WORK)
        xml=Path('/reports/codes.xml');binary=runner.binary('integration');output['integration']=binary
        completed=subprocess.run([binary['path'],'--config',str(config),*[arg for name in tests for arg in ('--include',name)],'--xml',str(xml)],timeout=600)
        output.update(e2e_executed=True,process_exit_code=completed.returncode)
        if completed.returncode or not xml.is_file():raise ValueError('original code-migration execution failed')
        output['validated_report']=harness.check_xml(xml,tests);output['result']='pass'
    except Exception as error:output.update(result='fail',error_category=type(error).__name__,error=str(error)[:300])
    finally:
        for process in processes:
            if process.poll()is None:process.terminate()
        for process in processes:
            try:process.wait(timeout=5)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
        for log in logs:log.close()
        for server in servers:server.server_close()
        Path('/reports/result.json').write_text(json.dumps(output,indent=2)+'\n')
    return 0 if output['result']=='pass' else 1


if __name__=='__main__':raise SystemExit(main())
