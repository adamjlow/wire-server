#!/usr/bin/env python3
"""Opt-in PID1 supervision of adopted native Nginx test workers only."""
import hashlib
import http.client as http_client
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

REPORTS=Path('/reports')


def context(child=False):
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1' or (os.getppid()!=1 if child else os.getpid()!=1):
        raise ValueError('requires nonroot disposable Docker PID1 supervision')


def worker(pid):
    root=Path('/proc')/str(pid)
    try:
        if (root/'comm').read_text().strip()!='nginx':return None
        fields=(root/'stat').read_text().rsplit(')',1)[1].split()
        if int(fields[1])!=os.getpid():return None
        status=(root/'status').read_text()
        uid=next(line for line in status.splitlines() if line.startswith('Uid:')).split()[1:]
        if uid!=['65534']*4:return None
        command=(root/'cmdline').read_bytes().split(b'\0',1)[0]
        if not command.startswith(b'nginx: worker process'):return None
        executable=(root/'exe').resolve(strict=True)
        if not str(executable).startswith('/nix/store/') or 'nginx' not in executable.name:return None
        return {'pid':pid,'parent_pid':int(fields[1]),'start_ticks':int(fields[19]),'uid':65534,'command':command.decode(),'executable':str(executable),'executable_sha256':hashlib.sha256(executable.read_bytes()).hexdigest()}
    except (FileNotFoundError,ProcessLookupError):return None


def supervise(args,probe=False):
    context();report={'result':'running','kind':'owned-native-Nginx-orphan-supervision','uid':os.getuid(),'pid':os.getpid(),'signals':[],'scope':'Adopted native nginx workers only; original service/test code, assertions and deadlines unchanged.'}
    command=[sys.executable,__file__,'--probe-child'] if probe else [sys.executable,'/opt/wire-e2e/runner.py',*args]
    if not probe and (not args or args[0]!='environment'):raise ValueError('only original owned environment runner accepted')
    child=subprocess.Popen(command);pending={};fatal=None
    def forward(sig,frame):
        if child.poll()is None:child.send_signal(sig)
    signal.signal(signal.SIGTERM,forward);signal.signal(signal.SIGINT,forward)
    def scan():
        for path in Path('/proc').iterdir():
            if not path.name.isdigit():continue
            pid=int(path.name)
            if pid in pending:continue
            info=worker(pid)
            if info is None:continue
            if len(report['signals'])>=256:raise ValueError('bounded orphan cleanup limit exceeded')
            fd=os.pidfd_open(pid)
            try:
                again=worker(pid)
                if again is None or again['start_ticks']!=info['start_ticks']:continue
                signal.pidfd_send_signal(fd,signal.SIGTERM)
            finally:os.close(fd)
            info.update(signal='SIGTERM',signal_monotonic=time.monotonic());report['signals'].append(info);pending[pid]=info
        for pid,info in list(pending.items()):
            try:waited,status=os.waitpid(pid,os.WNOHANG)
            except ChildProcessError:raise ValueError('adopted worker was not a supervised child')
            if waited:
                info.update(exit_code=os.waitstatus_to_exitcode(status),reaped=True);del pending[pid]
            elif time.monotonic()-info['signal_monotonic']>5:raise ValueError('orphan worker did not terminate within five seconds')
        (REPORTS/'orphan-nginx-supervision-progress.json').write_text(json.dumps(report,indent=2)+'\n')
    try:
        while child.poll()is None:
            if not probe or (REPORTS/'release-orphan-supervision').exists():scan()
            time.sleep(.1)
        # Reap already adopted workers after the original runner finishes.
        deadline=time.monotonic()+5
        while True:
            scan()
            if not pending:break
            if time.monotonic()>deadline:raise ValueError('orphan cleanup did not finish')
            time.sleep(.1)
        report.update(original_exit_code=child.returncode,result='pass' if child.returncode==0 else 'original-run-failed')
    except Exception as error:
        fatal=str(error)[:300];report.update(result='fail',error=fatal)
        if child.poll()is None:child.terminate();child.wait(timeout=10)
    finally:
        (REPORTS/'orphan-nginx-supervision.json').write_text(json.dumps(report,indent=2)+'\n')
    return child.returncode if not fatal else 2


def http(port):
    c=http_client.HTTPConnection('127.0.0.1',port,timeout=1)
    try:c.request('GET','/');r=c.getresponse();return r.status,r.read().decode()
    finally:c.close()


def probe_child():
    context(child=True);masters=[];report={'result':'running','kind':'actual-native-Nginx-orphan-and-healthy-worker-probe'}
    try:
        for name,port in [('orphan',18090),('healthy',18091)]:
            config=Path('/tmp')/(name+'.conf');config.write_text('worker_processes 1; pid /tmp/'+name+'.pid; error_log /dev/stderr; events { worker_connections 64; } http { access_log off; server { listen '+str(port)+'; location / { return 200 "synthetic-'+name+'"; } } }')
            p=subprocess.Popen(['nginx','-c',str(config),'-g','daemon off;','-e','/dev/stderr']);masters.append(p)
            deadline=time.monotonic()+10
            while True:
                try:
                    if http(port)==(200,'synthetic-'+name):break
                except OSError:pass
                if p.poll()is not None or time.monotonic()>deadline:raise ValueError('native probe listener failed')
                time.sleep(.05)
        orphan_master,healthy=masters
        children=[]
        for p in Path('/proc').iterdir():
            if not p.name.isdigit():continue
            try:
                fields=(p/'stat').read_text().rsplit(')',1)[1].split()
                if int(fields[1])==orphan_master.pid and (p/'comm').read_text().strip()=='nginx':children.append(int(p.name))
            except FileNotFoundError:pass
        if len(children)!=1:raise ValueError('expected one actual native worker')
        pid=children[0];orphan_master.kill();orphan_master.wait(timeout=5)
        fields=(Path('/proc')/str(pid)/'stat').read_text().rsplit(')',1)[1].split()
        if int(fields[1])!=1 or http(18090)!=(200,'synthetic-orphan'):raise ValueError('native orphan listener not demonstrated')
        report.update(orphan_master_pid=orphan_master.pid,orphan_master_exit=orphan_master.returncode,orphan_worker_pid=pid,orphan_parent_pid=1,orphan_listener_before=True,healthy_master_pid=healthy.pid)
        (REPORTS/'release-orphan-supervision').touch();deadline=time.monotonic()+10
        while True:
            try:http(18090)
            except OSError:
                if not (Path('/proc')/str(pid)).exists():break
            if time.monotonic()>deadline:raise ValueError('orphan listener/process not released')
            time.sleep(.05)
        if healthy.poll()is not None or http(18091)!=(200,'synthetic-healthy'):raise ValueError('healthy worker affected')
        healthy.send_signal(signal.SIGQUIT);code=healthy.wait(timeout=5)
        if code!=0:raise ValueError('healthy native master did not stop cleanly')
        report.update(result='pass',orphan_listener_after=False,orphan_worker_reaped=True,healthy_http_after=200,healthy_stop_exit=code)
    except Exception as error:report.update(result='fail',error=str(error)[:300])
    finally:
        for p in masters:
            if p.poll()is None:p.terminate();p.wait(timeout=5)
        (REPORTS/'orphan-nginx-probe.json').write_text(json.dumps(report,indent=2)+'\n')
    return 0 if report['result']=='pass' else 1


if __name__=='__main__':
    try:
        args=sys.argv[1:]
        raise SystemExit(probe_child() if args==['--probe-child'] else supervise([],True) if args==['--probe'] else supervise(args))
    except ValueError as error:
        print(json.dumps({'result':'fail','error':str(error)}));raise SystemExit(2)
