#!/usr/bin/env python3
"""Pinned Brig SDK controller. Fresh output; committed clean source required."""
import subprocess, pathlib, json, uuid, hashlib, sys, os
root=pathlib.Path(__file__).resolve().parents[4]
cache=pathlib.Path(os.environ.get('WIRE_MIGRATION_CACHE',str(pathlib.Path.home()/'.cache/wire-migration'))).resolve()
out=pathlib.Path(sys.argv[1]).resolve() if len(sys.argv)==2 else None
if out is None or out.is_relative_to(root):
 raise SystemExit('usage: sdk_controller.py NEW_EXTERNAL_OUTPUT')
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()
assert out.parent.is_dir()
out.mkdir(mode=0o777)
out.chmod(0o777)
revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()
identity='brig-provider-service-update-sdk-'+uuid.uuid4().hex
label='wire-migration-task=brig-implementation'
sdk='sha256:2f9e4393295d7270538dd1c666ab1345eaec06b2c2f8c217283ba6565caee399'
runner='sha256:f487c90fd364aac9daee5a1ce6e60fdba0ab74ab0c671ab0de0198e8f05659a9'
def run(args, log):
 with (out/log).open('w') as f: return subprocess.run(args,stdout=f,stderr=subprocess.STDOUT).returncode
report={'source_revision':revision,'sdk_image':sdk,'runner_image':runner,'result':'failed'}
try:
 subprocess.run(['docker','network','create','--label',label,identity],check=True,stdout=subprocess.DEVNULL)
 cmd=['docker','run','--rm','--name',identity,'--label',label,'--network',identity,'--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--user','65534:65534','--tmpfs','/tmp:rw,exec,nosuid,nodev,size=2g,mode=1777']
 for src,dst,ro in [(root,'/source',True),(cache/'toolchains/go-security-20261009/go','/tools/go',True),(os.environ.get('WIRE_GOMODCACHE',str(pathlib.Path.home()/'go/pkg/mod')),'/gomodcache',True),(cache/'brig-20261010/sdk-go-cache-disk-20261011','/buildcache',False),(out,'/output',False)]:
  cmd+=['--mount',f'type=bind,src={src},dst={dst}'+(',readonly' if ro else '')]
 for env in ['PATH=/tools/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','CGO_ENABLED=1','GOMODCACHE=/gomodcache','GOPROXY=off','GOCACHE=/buildcache','GOFLAGS=-buildvcs=false','PYTHONDONTWRITEBYTECODE=1']:
  cmd+=['-e',env]
 cmd+=['-w','/source',sdk,'/bin/sh','-ec',"/bin/sh /source/go/services/brig/qualification/sdk_check.sh; cd /source/go/services/brig; go test -c -trimpath -buildvcs=false -tags=netgo,osusergo,brig_storage_qualification -ldflags='-linkmode external -extldflags \"-static -lz -lzstd\"' -o /output/stores.test ."]
 report['sdk_controller_exit_code']=run(cmd,'controller.log')
 if report['sdk_controller_exit_code']==0:
  cmd=['docker','run','--rm','--name',identity+'-units','--label',label,'--network','none','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--user','65534:65534','--memory=1g','--cpus=2','--pids-limit=256','--tmpfs','/tmp:rw,nosuid,nodev,size=1g,mode=1777','--mount',f'type=bind,src={root},dst=/source,readonly','--mount',f'type=bind,src={out}/brig.test,dst=/candidate/brig.test,readonly','-w','/source/go/services/brig','--entrypoint','/candidate/brig.test',runner,'-test.v','-test.timeout=5m']
  report['unchanged_runner_unit_exit_code']=run(cmd,'runtime-units.log')
  for name,key in [('brig','binary_sha256'),('brig.test','unit_binary_sha256'),('stores.test','store_binary_sha256')]: report[key]=hashlib.sha256((out/name).read_bytes()).hexdigest()
  report['toolchain']='go1.26.9'
  report['sdk_archives_manifest_sha256']=hashlib.sha256((out/'libraries.sha256').read_bytes()).hexdigest()
  report['go_checks']='pass'
  if report['unchanged_runner_unit_exit_code']==0: report['result']='pass'
except Exception as e: report['failure']=str(e)
finally:
 report['network_cleanup_exit_code']=subprocess.run(['docker','network','rm',identity],stdout=subprocess.DEVNULL).returncode
 report['remaining_owned_containers']=subprocess.check_output(['docker','ps','-aq','--filter','name='+identity],text=True).strip()
 report['remaining_owned_networks']=subprocess.check_output(['docker','network','ls','-q','--filter','name='+identity],text=True).strip()
 if report['network_cleanup_exit_code'] or report['remaining_owned_containers'] or report['remaining_owned_networks']: report['result']='failed'
 (out/'build-receipt.json').write_text(json.dumps(report,indent=2)+'\n')
 if 'store_binary_sha256' in report:
  store=dict(report); store['binary_sha256']=report['store_binary_sha256']
  (out/'stores-build-receipt.json').write_text(json.dumps(store,indent=2)+'\n')
print(json.dumps(report))
sys.exit(0 if report['result']=='pass' else 1)
