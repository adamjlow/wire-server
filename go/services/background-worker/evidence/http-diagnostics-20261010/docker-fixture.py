import subprocess,json,uuid,time,pathlib,hashlib
cache=pathlib.Path('/home/operator/.cache/wire-migration/background-worker-20261009')
binary=cache/'http-diagnostics-scheduled-check'
owner='wire-worker-http-diagnostics-'+uuid.uuid4().hex
label='wire.migration.owner='+owner
net=owner+'-net';vol=owner+'-pg';pg=owner+'-postgres';check=owner+'-check'
pgimage='postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24'
runner='sha256:cd03212430eef6cfde0324b50885c12980ee211483ab27dde6ee730252966cd2'
log=[];cleanup=[];result={'owner':owner,'postgres_image':pgimage,'runner_image':runner,'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'test':'TestScheduledExecutionDockerStorage','test_tag':'schemacheck'}
def run(args,timeout=90,required=True):
 p=subprocess.run(args,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=timeout)
 log.append({'command':args,'exit_code':p.returncode,'output':p.stdout})
 if required and p.returncode:raise RuntimeError(p.stdout)
 return p
try:
 run(['docker','network','create','--internal','--label',label,net])
 run(['docker','volume','create','--label',label,vol])
 run(['docker','run','-d','--name',pg,'--label',label,'--network',net,'--network-alias','postgres','--memory','512m','-e','POSTGRES_PASSWORD=synthetic-schema-only','-e','POSTGRES_DB=backendA','--mount','type=volume,src='+vol+',dst=/var/lib/postgresql/data',pgimage])
 for i in range(60):
  if run(['docker','exec',pg,'pg_isready','-h','127.0.0.1','-U','postgres','-d','backendA'],required=False).returncode==0:break
  time.sleep(.5)
 else:raise RuntimeError('TCP readiness timeout')
 p=run(['docker','run','--rm','--name',check,'--label',label,'--network',net,'--user','65532:65532','--cap-drop','ALL','--security-opt','no-new-privileges','--read-only','--tmpfs','/tmp:rw,size=16m','-e','WIRE_SCHEMA_CHECK_SYNTHETIC=1','--mount','type=bind,src='+str(binary)+',dst=/qualifier/schema-check,readonly','--mount','type=bind,src=/home/operator/wire-server/libs/wire-subsystems/postgres-migrations,dst=/reference/migrations,readonly','--entrypoint','/qualifier/schema-check',runner,'-test.run','^TestScheduledExecutionDockerStorage$','-test.v'],required=False)
 result.update(test_exit_code=p.returncode,result='pass' if p.returncode==0 else 'fail');print(p.stdout,flush=True)
except Exception as e:
 result.update(result='fail',error=str(e));print(str(e),flush=True)
finally:
 for args in [['docker','rm','-f',check],['docker','rm','-f',pg],['docker','volume','rm',vol],['docker','network','rm',net]]:
  p=run(args,required=False);cleanup.append({'command':args,'exit_code':p.returncode,'output':p.stdout})
 result['cleanup']=cleanup;result['remaining_owned']={}
 for kind in ['container','network','volume']:
  p=run(['docker',kind,'ls','-q','--filter','label='+label],required=False)
  result['remaining_owned'][kind]=p.stdout.strip()
 (cache/'http-diagnostics-docker.json').write_text(json.dumps(result,indent=2)+'\n')
 (cache/'http-diagnostics-docker-log.json').write_text(json.dumps(log,indent=2)+'\n')
print(json.dumps(result),flush=True)
raise SystemExit(0 if result['result']=='pass' and all(x['exit_code']==0 for x in cleanup) and not any(result['remaining_owned'].values()) else 1)
