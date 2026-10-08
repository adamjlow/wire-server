#!/usr/bin/env python3
"""Run original service tests in a private, disposable Docker Compose project."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import uuid

from control import ROOT, FILES, command, destination

DEPENDENCIES = ('fake_dynamodb', 'fake_sqs', 'fake_localstack', 'fake_s3',
                'redis-master', 'elasticsearch', 'postgres', 'cassandra',
                'rabbitmq', 'coredns', 'redis-cluster', *('redis-node-'+str(i) for i in range(1,7)))
SERVICES = {'brig':8082, 'cannon':8083, 'cargohold':8084, 'galley':8085,
            'gundeck':8086, 'proxy':8087, 'spar':8088, 'background-worker':8089,
            'stern':8091, 'federator':8097, 'wire-server-enterprise':8079}
SYNTHETIC_ENV = {'RABBITMQ_USERNAME':'guest', 'RABBITMQ_PASSWORD':'alpaca-grapefruit',
                 'AWS_ACCESS_KEY_ID':'GKdeadbeefcafebabe0000000000000000',
                 'AWS_SECRET_ACCESS_KEY':'deadbeefcafebabe0000000000000000deadbeefcafebabe0000000000000000',
                 'AWS_REGION':'eu-west-1', 'AWS_DEFAULT_REGION':'eu-west-1',
                 'AWS_EC2_METADATA_DISABLED':'true', 'HOME':'/tmp/home',
                 'WIRE_E2E_DISPOSABLE':'1', 'TEST_TIMEOUT_SECONDS':'60',
                 'REDIS_PASSWORD':'very-secure-redis-cluster-password',
                 'REDIS_ADDITIONAL_WRITE_PASSWORD':'very-secure-redis-master-password'}


def yaml_json(path):
    """Use already-required Compose's YAML parser, without a host YAML package."""
    with tempfile.TemporaryDirectory(prefix='wire-e2e-yaml-') as tmp:
        wrapper = Path(tmp)/'config.yaml'
        wrapper.write_text('x-wire:\n'+''.join('  '+line+'\n' for line in path.read_text().splitlines())+
                           '\nservices:\n  parser:\n    image: scratch\n')
        return json.loads(command(['docker','compose','-f',wrapper,'config','--format','json']))['x-wire']


def set_field(config, path, value):
    keys = path.split('.')
    for key in keys[:-1]:
        config = config.setdefault(key, {})
    config[keys[-1]] = value


def absolute_fixtures(value, directory):
    if isinstance(value, dict):
        return {key:absolute_fixtures(item,directory) for key,item in value.items()}
    if isinstance(value, list):
        return [absolute_fixtures(item,directory) for item in value]
    if isinstance(value,str) and not value.startswith('/'):
        local = directory/value
        if local.exists():
            resolved = local.resolve()
            if not resolved.is_relative_to(ROOT):
                raise ValueError('fixture escapes checkout')
            return '/source/'+str(resolved.relative_to(ROOT))
    return value


def peer_config(base, service, index):
    """Static A/B values from Testlib.RunServices and defaultOverrides.

    Dynamic peers continue to use the original compiled ModService code.
    """
    config = copy.deepcopy(base)
    domain = ('example.com','b.example.com')[index]
    suffix = ('','2')[index]
    for name,port in SERVICES.items():
        key = {'background-worker':'backgroundWorker','wire-server-enterprise':'wireServerEnterprise'}.get(name,name)
        config[key] = {'host':'127.0.0.1','port':port+1000*index}
        if name=='cannon':config[key]['externalHost']='127.0.0.1'
    config['nginz']={'host':'127.0.0.1','port':8080+1000*index}
    config['federatorInternal']={'host':'127.0.0.1','port':8097+1000*index}
    config['federatorExternal']={'host':'127.0.0.1','port':8098+1000*index}
    if service in ('brig','galley','spar','gundeck','cannon','background-worker'):
        space = 'gundeck' if service in ('cannon','background-worker') else service
        set_field(config,'cassandra.keyspace',space+'_test'+suffix)
    if service in ('brig','galley','background-worker'):
        set_field(config,'postgresql.dbname',('backendA','backendB')[index])
    if service in ('brig','galley','gundeck','cannon','background-worker'):
        set_field(config,'rabbitmq.vHost',('backendA','backendB')[index])
    if service=='brig':
        for key,value in {'elasticsearch.index':('directory_test','directory2_test')[index],
                          'optSettings.setFederationDomain':domain,'optSettings.setFederationDomainConfigs':[],
                          'aws.userJournalQueue':'integration-user-events'+suffix+'.fifo',
                          'aws.prekeyTable':'integration-brig-prekeys'+suffix,
                          'internalEvents.queueName':'integration-brig-events-internal'+suffix,
                          'emailSMS.email.sesQueue':'integration-brig-events'+suffix,
                          'emailSMS.general.emailSender':'backend-integration'+suffix+'@wire.com'}.items():
            set_field(config,key,value)
    elif service=='galley':
        set_field(config,'settings.featureFlags.classifiedDomains.config.domains',[domain])
        set_field(config,'journal.queueName','integration-team-events'+suffix+'.fifo')
        for algorithm in ('ed25519','ecdsa_secp256r1_sha256','ecdsa_secp384r1_sha384','ecdsa_secp521r1_sha512'):
            set_field(config,'settings.mlsPrivateKeyPaths.removal.'+algorithm,
                      '/source/services/galley/test/resources/'+('backendA','backendB')[index]+'/'+algorithm+'.pem')
    elif service=='cargohold':set_field(config,'aws.s3Bucket','dummy-bucket'+suffix)
    elif service=='gundeck':set_field(config,'aws.queueName','integration-gundeck-events'+suffix)
    elif service=='background-worker':
        set_field(config,'federationDomain',domain)
        set_field(config,'cassandraGalley.keyspace','galley_test'+suffix)
        set_field(config,'cassandraBrig.keyspace','brig_test'+suffix)
    elif service=='federator':set_field(config,'optSettings.setFederationDomain',domain)
    elif service=='spar':
        set_field(config,'saml.spHost','127.0.0.1');set_field(config,'saml.spPort',8088+1000*index)
    if service in ('galley','cargohold','gundeck'):set_field(config,'settings.federationDomain',domain)
    config['logLevel']='Warn'
    return config


def prepare_fixtures(output):
    fixtures=output/'fixtures';fixtures.mkdir()
    configs={}
    for service in SERVICES:
        original=ROOT/'services'/service/(service+'.integration.yaml')
        if service=='wire-server-enterprise' and not original.exists():
            # Public chart documents this peer's config; private submodule is
            # unavailable. This is fixture setup, never a substitute executable.
            config={'wireServerEnterprise':{'host':'127.0.0.1','port':8079},'logLevel':'Warn',
                    'logFormat':'StructuredJSON','logNetStrings':False,
                    'dnsOverHttpsUrl':'http://localhost:5381/dns-query'}
        else:config=absolute_fixtures(yaml_json(original),original.parent)
        configs[service]=config
    (fixtures/'configs.json').write_text(json.dumps(configs,indent=2)+'\n')
    static={str(i):{service:peer_config(config,service,i) for service,config in configs.items()} for i in range(2)}
    (fixtures/'peers.json').write_text(json.dumps(static,indent=2)+'\n')
    integration=yaml_json(ROOT/'services/integration.yaml')
    # Upstream local integration config skips admin TLS verification; use its
    # actual trusted fixture CA in this disposable environment instead.
    integration['rabbitmq']['insecureSkipVerifyTls']=False
    integration['rabbitmq']['caCert']='/source/deploy/dockerephemeral/rabbitmq-config/certificates/ca.pem'
    (fixtures/'integration.json').write_text(json.dumps(integration,indent=2)+'\n')


def private_compose(upstream, project, output, image):
    services={}
    for name in DEPENDENCIES:
        service=copy.deepcopy(upstream['services'][name])
        for key in ('container_name','ports','links','depends_on','healthcheck'):
            service.pop(key,None)
        service['security_opt']=['no-new-privileges:true']
        service['mem_limit']='2g' if name in ('cassandra','elasticsearch') else '1g'
        service['pids_limit']=512
        service['cpus']=2
        if name=='elasticsearch':service['tmpfs']=['/usr/share/elasticsearch/data:rw,nosuid,nodev,size=1g,uid=1000,gid=1000']
        if name=='rabbitmq':service['environment']={'RABBITMQ_USERNAME':'guest','RABBITMQ_PASSWORD':'alpaca-grapefruit'}
        for volume in service.get('volumes',[]):
            if volume['type']=='bind':volume['read_only']=True
        aliases={'fake_dynamodb':['dynamodb'],'fake_sqs':['sqs'],'fake_localstack':['ses','sns'],'fake_s3':['s3']}
        if name in aliases:service['networks']['demo_wire']={'aliases':aliases[name]}
        services[name]=service
    services['runner']={'image':image,'entrypoint':['/bin/bash','/opt/wire-e2e/entrypoint.sh'],
        'command':['environment','--service','proxy'],'user':'65534:65534',
        'environment':SYNTHETIC_ENV,'extra_hosts':['localhost.:127.0.0.1'],'read_only':True,'cap_drop':['ALL'],
        'security_opt':['no-new-privileges:true'],'mem_limit':'12g','cpus':4,'pids_limit':4096,
        'tmpfs':['/tmp:rw,nosuid,nodev,size=2g,mode=1777'],
        'networks':['demo_wire','redis','coredns'],
        'volumes':[{'type':'bind','source':str(ROOT),'target':'/source','read_only':True},
                   {'type':'bind','source':str(output/'fixtures'),'target':'/fixtures','read_only':True},
                   {'type':'bind','source':str(output/'reports'),'target':'/reports'}]}
    # No original global network/volume names; only Redis needs its upstream
    # private advertised subnet, checked for collisions before starting.
    networks={'demo_wire':{'driver':'bridge','internal':True,'ipam':{'config':[{'subnet':'172.20.2.0/24'}]}},
              'redis':{'driver':'bridge','internal':True,'ipam':{'config':[{'subnet':'172.20.0.0/24'}]}},
              'coredns':{'driver':'bridge','internal':True,'ipam':{'config':[{'subnet':'172.20.1.0/24'}]}}}
    spec={'name':project,'services':services,'networks':networks,'volumes':{name:{} for name in upstream.get('volumes',{})}}
    validate_isolation(spec)
    return spec


def validate_isolation(spec):
    if not re.fullmatch('wire-e2e-[0-9a-f]{32}',spec['name']):raise ValueError('invalid owned project name')
    for network in spec['networks'].values():
        if not network.get('internal') or network.get('external') or 'name' in network:
            raise ValueError('networks must be private and project-owned')
    for volume in spec.get('volumes',{}).values():
        if volume.get('external') or 'name' in volume:raise ValueError('volumes must be project-owned')
    for service in spec['services'].values():
        if any(service.get(key) for key in ('ports','container_name','privileged','network_mode','pid','devices')):
            raise ValueError('host exposure or global identity is forbidden')
        for volume in service.get('volumes',[]):
            if volume['type']=='bind' and volume['target']!='/reports' and not volume.get('read_only'):
                raise ValueError('source and fixtures must be read-only')
            if volume['type']=='bind' and (not Path(volume['source']).resolve().is_relative_to(ROOT) and volume['target'] not in ('/fixtures','/reports','/candidate/proxy','/candidate/cargohold','/candidate/cannon') or volume['source'].endswith('docker.sock') or volume['target']=='/'):
                raise ValueError('unsafe mount')


def identified_candidate(candidate, receipt):
    candidate=Path(candidate).resolve(strict=True)
    if not candidate.is_file() or not os.access(candidate,os.X_OK):raise ValueError('candidate must be executable')
    build=json.loads(Path(receipt).read_text())
    checksum=hashlib.sha256(candidate.read_bytes()).hexdigest()
    if build['binary_sha256']!=checksum or not re.fullmatch('[0-9a-f]{40}',build['source_revision']):
        raise ValueError('candidate build receipt mismatch')
    return {'path':str(candidate),'sha256':checksum,'source_revision':build['source_revision']}


def prepare(output, receipt_path, candidate=None, candidate_receipt=None, peers=()):
    output=destination(output)
    if command(['git','status','--porcelain']):raise ValueError('commit runner/controller inputs first')
    receipt=json.loads(receipt_path.read_text());image=receipt['image_id']
    if not re.fullmatch('sha256:[0-9a-f]{64}',image):raise ValueError('invalid runner image identity')
    inspected=json.loads(command(['docker','image','inspect',image]))[0]
    if inspected['Config']['User']!='65534:65534':raise ValueError('runner must be non-root')
    output.mkdir();(output/'reports').mkdir();(output/'reports').chmod(0o777)
    prepare_fixtures(output)
    env=dict(os.environ,**SYNTHETIC_ENV)
    # No caller-supplied Compose files, endpoints or inherited Compose project.
    upstream=json.loads(subprocess.run(['docker','compose','-f',str(ROOT/'deploy/dockerephemeral/docker-compose.yaml'),
        'config','--format','json'],env=env,check=True,capture_output=True,text=True,timeout=30).stdout)
    project='wire-e2e-'+uuid.uuid4().hex
    spec=private_compose(upstream,project,output,image)
    selected=None
    if candidate is not None:
        if candidate_receipt is None:raise ValueError('candidate requires an identified build receipt')
        selected=identified_candidate(candidate,candidate_receipt)
    peer_candidates={}
    for name,path,build_receipt in peers:
        if name not in ('proxy','cargohold','cannon') or name in peer_candidates or candidate is not None:
            raise ValueError('unsupported, duplicate or ambiguous candidate selection')
        peer_candidates[name]=identified_candidate(path,build_receipt)
    (output/'compose.json').write_text(json.dumps(spec,indent=2)+'\n')
    record={'schema_version':1,'project':project,'source_revision':command(['git','rev-parse','HEAD']),
            'runner_image_id':image,'runner_revision':receipt['source_revision'],'candidate':selected,'peer_candidates':peer_candidates,'result':'prepared','e2e_executed':False,
            'compose_sha256':hashlib.sha256((output/'compose.json').read_bytes()).hexdigest(),
            'files_sha256':{str(p.relative_to(output)):hashlib.sha256(p.read_bytes()).hexdigest()
                            for p in sorted((output/'fixtures').iterdir())}}
    (output/'environment.json').write_text(json.dumps(record,indent=2)+'\n')
    return record


def run(output,service):
    output=output.resolve(strict=True)
    record=json.loads((output/'environment.json').read_text())
    if output.is_relative_to(ROOT):raise ValueError('environment must be outside checkout')
    if hashlib.sha256((output/'compose.json').read_bytes()).hexdigest()!=record['compose_sha256']:
        raise ValueError('prepared Compose inputs changed')
    for name,checksum in record['files_sha256'].items():
        path=output/name
        if not path.resolve(strict=True).is_relative_to(output/'fixtures') or path.is_symlink() or hashlib.sha256(path.read_bytes()).hexdigest()!=checksum:
            raise ValueError('prepared fixtures changed')
    spec=json.loads((output/'compose.json').read_text());validate_isolation(spec)
    if record['source_revision']!=command(['git','rev-parse','HEAD']) or command(['git','status','--porcelain']):
        raise ValueError('environment source revision changed')
    if record['project']!=spec['name']:raise ValueError('project identity mismatch')
    if shutil.disk_usage(output).free<2*1024**3:raise ValueError('need at least 2 GiB free for disposable dependency data')
    # Check private fixed Redis/DNS fixture subnets do not overlap other stacks.
    import ipaddress
    ids=command(['docker','network','ls','--quiet']).splitlines()
    existing=json.loads(command(['docker','network','inspect',*ids]))
    for network in existing:
        for config in network.get('IPAM',{}).get('Config',[]) or []:
            subnet=config.get('Subnet')
            if subnet and ':' not in subnet and any(ipaddress.ip_network(subnet).overlaps(ipaddress.ip_network(required))
                                                   for required in ('172.20.0.0/24','172.20.1.0/24','172.20.2.0/24')):
                raise ValueError('fixture subnet overlaps existing Docker network')
    for name in DEPENDENCIES:
        image=spec['services'][name]['image']
        try:inspection=json.loads(command(['docker','image','inspect',image]))[0]
        except subprocess.CalledProcessError:
            print('Pulling upstream test dependency '+name,flush=True)
            command(['docker','pull',image],timeout=300)
            inspection=json.loads(command(['docker','image','inspect',image]))[0]
        digests=inspection.get('RepoDigests',[])
        if not digests:raise ValueError('dependency lacks immutable registry digest')
        spec['services'][name]['image']=digests[0]
        if shutil.disk_usage(output).free<2*1024**3:raise ValueError('insufficient disk after dependency pull')
    record['dependencies']={name:spec['services'][name]['image'] for name in DEPENDENCIES}
    spec['services']['runner']['command']=['environment','--service',service]
    peers=record.get('peer_candidates',{})
    if service=='proxy-cargohold' and (set(peers)!= {'proxy','cargohold'} or record.get('candidate') is not None):
        raise ValueError('combined run requires two unambiguous identified candidates')
    if service=='proxy-cargohold-cannon' and (set(peers)!={'proxy','cargohold','cannon'} or record.get('candidate') is not None):
        raise ValueError('combined run requires three unambiguous identified candidates')
    for name,selection in peers.items():
        if name not in ('proxy','cargohold','cannon'):raise ValueError('unsupported candidate selection')
        candidate=Path(selection['path'])
        if hashlib.sha256(candidate.read_bytes()).hexdigest()!=selection['sha256']:raise ValueError('candidate binary changed')
        spec['services']['runner']['command']+=['--peer-candidate',name,'/candidate/'+name]
        spec['services']['runner']['volumes'].append({'type':'bind','source':str(candidate),'target':'/candidate/'+name,'read_only':True})
    if record.get('candidate') is not None:
        candidate=Path(record['candidate']['path'])
        if hashlib.sha256(candidate.read_bytes()).hexdigest()!=record['candidate']['sha256']:
            raise ValueError('candidate binary changed')
        spec['services']['runner']['command']+=['--candidate','/candidate/'+service]
        spec['services']['runner']['volumes'].append({'type':'bind','source':str(candidate),'target':'/candidate/'+service,'read_only':True})
    validate_isolation(spec)
    (output/'compose.json').write_text(json.dumps(spec,indent=2)+'\n')
    record['compose_sha256']=hashlib.sha256((output/'compose.json').read_bytes()).hexdigest()
    compose=['docker','compose','-p',record['project'],'-f',str(output/'compose.json')]
    try:
        command([*compose,'up','--detach',*[name for name in DEPENDENCIES if name!='redis-cluster']],timeout=240)
        import time
        deadline=time.monotonic()+90
        while True:
            try:
                command([*compose,'exec','-T','redis-node-1','redis-cli','--tls','--cacert',
                         '/usr/local/etc/redis/ca.pem','-a','very-secure-redis-cluster-password',
                         '-h','172.20.0.36','-p','6378','ping'],timeout=10)
                break
            except subprocess.CalledProcessError:
                if time.monotonic()>deadline:raise ValueError('Redis nodes unavailable')
                time.sleep(1)
        command([*compose,'run','--rm','--no-deps','redis-cluster'],timeout=90)
        with (output/('baseline-'+service+'.log')).open('x') as log:
            result=subprocess.run([*compose,'run','--rm','--no-deps','runner'],stdout=log,stderr=subprocess.STDOUT,timeout=1800)
        record.update(result='pass' if result.returncode==0 else 'failed',process_exit_code=result.returncode)
        if result.returncode!=0:raise ValueError('baseline failed; inspect retained baseline log')
        suites=('proxy','cargohold','cannon') if service=='proxy-cargohold-cannon' else ('proxy','cargohold') if service=='proxy-cargohold' else (service,)
        for name in suites:
            suite=json.loads((output/'reports'/(name+'.run.json')).read_text())
            if suite.get('result')!='pass' or suite.get('e2e_executed') is not True:raise ValueError('original suite did not pass')
            for peer,selection in peers.items():
                actual=suite.get('implementations',{}).get(peer,{})
                if actual.get('implementation')!='go' or actual.get('sha256')!=selection['sha256']:
                    raise ValueError('suite did not execute selected peer binary')
        record.update(e2e_executed=True,service=service)
        return record
    except (OSError,ValueError,subprocess.SubprocessError) as error:
        record.update(result='failed',error_category=type(error).__name__)
        if isinstance(error,subprocess.CalledProcessError):
            (output/'setup-error.log').write_text(error.stderr or '')
        raise
    finally:
        try:
            with (output/'dependencies.log').open('w') as log:
                subprocess.run([*compose,'logs','--no-color','--tail','100'],stdout=log,
                               stderr=subprocess.STDOUT,timeout=30)
        finally:
            cleanup=subprocess.run([*compose,'down','--volumes','--remove-orphans','--timeout','10'],capture_output=True,text=True,timeout=90)
            record['cleanup_exit_code']=cleanup.returncode
            if cleanup.returncode!=0:record['result']='failed'
            (output/'environment.json').write_text(json.dumps(record,indent=2)+'\n')
            if cleanup.returncode!=0:raise ValueError('owned stack cleanup failed')


def main():
    parser=argparse.ArgumentParser(description=__doc__);sub=parser.add_subparsers(dest='command',required=True)
    prep=sub.add_parser('prepare');prep.add_argument('--output',type=Path,required=True);prep.add_argument('--receipt',type=Path,required=True);prep.add_argument('--candidate',type=Path);prep.add_argument('--candidate-receipt',type=Path)
    prep.add_argument('--select',nargs=3,action='append',default=[],metavar=('SERVICE','BINARY','RECEIPT'))
    execute=sub.add_parser('run');execute.add_argument('--output',type=Path,required=True);execute.add_argument('--service',choices=('proxy','cargohold','cannon','proxy-cargohold','proxy-cargohold-cannon'),required=True)
    args=parser.parse_args()
    try:
        result=prepare(args.output,args.receipt,args.candidate,args.candidate_receipt,args.select) if args.command=='prepare' else run(args.output,args.service)
        print(json.dumps(result,indent=2));return 0
    except (OSError,ValueError,KeyError,subprocess.SubprocessError) as error:
        print(json.dumps({'result':'failed','error':str(error)[:600],'e2e_executed':False}));return 2

if __name__=='__main__':raise SystemExit(main())
