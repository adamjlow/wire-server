"""Qualify remaining original suites with the same seven identified Go peers."""
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

NAMES=('proxy','cargohold','cannon','gundeck','federator','background-worker','spar')


def main():
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':raise ValueError('owned nonroot namespace required')
    selected={name:Path('/candidate/'+name).resolve(strict=True)for name in NAMES}
    processes=[];logs=[];servers=[];output={'result':'running','kind':'remaining-original193-seven-Go','e2e_executed':False,'suites':{},'diagnosis':{},'scope':'Reuse separately passing current-tuple proxy5/cargohold27; execute all remaining original193 assertions without changing names, catalogues, XML checks or selected seven hashes. Failed combined attempts remain failures.'}
    try:
        servers=runtime.start_forwards();runtime.wait_dependencies();config=runtime.stage();runtime.initialize()
        for name,path in selected.items():
            link=runtime.WORK/'dist'/name;link.unlink();link.symlink_to(path)
        implementations={link.name:{'implementation':'go' if link.name in selected else 'haskell','path':str(link.resolve(strict=True)),'sha256':hashlib.sha256(link.read_bytes()).hexdigest()}for link in (runtime.WORK/'dist').iterdir() if link.name not in ('integration','nginx')}
        output['implementations']=implementations
        runtime.start_peers(processes,logs);os.chdir(runtime.WORK)
        # The original scenario acquires a dynamic backend. Switch only its
        # staged Gundeck executable for the finite native/Go diagnosis; static
        # peers and every other dynamic selection stay as recorded above.
        case='Events.testEventsForSpecificClients'
        if case not in runner.discover('gundeck'):raise ValueError('original diagnostic case absent')
        link=runtime.WORK/'dist/gundeck'
        for mode,path in [('native',Path(runner.binary('gundeck')['path'])),('go',selected['gundeck'])]:
            link.unlink();link.symlink_to(path)
            xml=Path('/reports')/('specific-clients-'+mode+'.xml')
            log=open('/reports/specific-clients-'+mode+'.log','w');logs.append(log)
            p=subprocess.run([runner.binary('integration')['path'],'--config',str(config),'--include',case,'--xml',str(xml)],env={**os.environ,'WIRE_INTEGRATION_TEST_VERBOSITY':'1'},stdout=log,stderr=subprocess.STDOUT,timeout=180)
            report={'dynamic_gundeck':{'implementation':mode,'path':str(path),'sha256':hashlib.sha256(path.read_bytes()).hexdigest()},'other_selected_peers':implementations,'process_exit_code':p.returncode,'result':'fail','required_tests':[case]}
            if p.returncode==0:report.update(result='pass',validated_report=harness.check_xml(xml,[case]))
            output['diagnosis'][mode]=report
        # Qualification uses the complete unchanged original suites. Diagnostic
        # failures cannot be counted as passes or substitute for these reports.
        for name in ('spar','gundeck','federator','background-worker'):
            report,code=runner.suite(name,selected[name],config,Path('/reports')/(name+'.xml'),implementations)
            output['suites'][name]=report
            if code:raise ValueError('original suite failed: '+name)
        counts={name:len(report['required_tests'])for name,report in output['suites'].items()}
        if counts!={'spar':101,'gundeck':40,'federator':5,'background-worker':47}:raise ValueError('remaining original catalogue count mismatch')
        output.update(result='pass',e2e_executed=True,executions=sum(counts.values()))
    except Exception as error:output.update(result='fail',error=str(error)[:300],error_category=type(error).__name__)
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
