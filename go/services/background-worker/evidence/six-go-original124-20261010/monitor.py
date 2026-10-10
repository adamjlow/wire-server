import datetime,json,subprocess,time
from pathlib import Path
cache=Path('/home/operator/.cache/wire-migration/background-worker-20261009')
out=cache/'six-go-original124'
project=json.loads((out/'environment.json').read_text())['project']
probe=r'''
import json,os,resource
from pathlib import Path
processes=[]
for p in Path('/proc').iterdir():
 if not p.name.isdigit():continue
 try:
  status={line.split(':',1)[0]:line.split(':',1)[1].strip() for line in (p/'status').read_text().splitlines() if ':' in line}
  limits=[line for line in (p/'limits').read_text().splitlines() if line.startswith('Max open files')]
  processes.append({'pid':int(p.name),'name':status['Name'],'fd':len(list((p/'fd').iterdir())),'threads':int(status['Threads']),'rss_kib':int(status.get('VmRSS','0 kB').split()[0]),'nofile':limits})
 except (OSError,KeyError,ValueError):pass
cgroup={}
for name in ('memory.current','memory.events','memory.peak','pids.current','pids.events'):
 try:cgroup[name]=(Path('/sys/fs/cgroup')/name).read_text().strip()
 except OSError:pass
states={}
for filename in ('/proc/net/tcp','/proc/net/tcp6'):
 try:
  for line in Path(filename).read_text().splitlines()[1:]:
   state=line.split()[3];states[state]=states.get(state,0)+1
 except OSError:pass
print(json.dumps({'processes':processes,'cgroup':cgroup,'tcp_states':states,'monitor_nofile':resource.getrlimit(resource.RLIMIT_NOFILE)}))
'''
samples=cache/'six-go-monitor.jsonl'
started=time.time()
with (cache/'six-go-controller.log').open('w') as log, samples.open('x') as journal:
 proc=subprocess.Popen(['python3','tools/parity/docker/environment.py','run','--output',str(out),'--service','proxy-cargohold-cannon-gundeck-federator-background-worker'],stdout=log,stderr=subprocess.STDOUT)
 while True:
  sample={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'elapsed_seconds':round(time.time()-started,3)}
  try:
   ids=subprocess.run(['docker','ps','-q','--filter','label=com.docker.compose.project='+project,'--filter','label=com.docker.compose.service=runner'],capture_output=True,text=True,check=True,timeout=10).stdout.split()
   if ids:
    sample['container']=ids[0]
    observed=subprocess.run(['docker','exec','-i',ids[0],'/nix/store/60m4rxhg2fldqaak400c0lry96ijrzqn-python3-3.13.13/bin/python3','-'],input=probe,capture_output=True,text=True,timeout=15)
    sample['probe_exit_code']=observed.returncode
    if observed.returncode==0:sample['observed']=json.loads(observed.stdout)
    else:sample['probe_error']=observed.stderr[-400:]
  except (subprocess.SubprocessError,ValueError) as error:sample['monitor_error']=str(error)
  journal.write(json.dumps(sample)+'\n');journal.flush()
  try:
   result=proc.wait(timeout=5)
   break
  except subprocess.TimeoutExpired:pass
terminal={'project':project,'controller_exit_code':result,'elapsed_seconds':round(time.time()-started,3),'monitor_file':str(samples),'sample_interval_seconds':5,'monitoring':'readonly /proc and cgroup; no application environment or command lines'}
(cache/'six-go-controller-result.json').write_text(json.dumps(terminal,indent=2)+'\n')
print(json.dumps(terminal))
raise SystemExit(result)
