"""Disposable container initialization and original Haskell peer supervision."""
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import select
import shutil
import socket
import socketserver
import subprocess
import threading
import time
import urllib.request

FORWARDS = {9042:('cassandra',9042),5432:('postgres',5432),
            6379:('redis-master',6379),9200:('elasticsearch',9200),
            4567:('fake_dynamodb',8000),4568:('fake_sqs',4568),
            4569:('fake_localstack',4579),4575:('fake_localstack',4575),
            4570:('fake_s3',3900),5671:('rabbitmq',5671),
            15671:('rabbitmq',15671),15672:('rabbitmq',15672),9053:('coredns',53)}
SOURCE=Path('/source');WORK=Path('/tmp/wire-e2e')


def execute(args, timeout=180, cwd=None):
    print('Setup: '+' '.join(map(str,args)),flush=True)
    return subprocess.run(list(map(str,args)),check=True,timeout=timeout,cwd=cwd)


class TCPServer(socketserver.ThreadingTCPServer):
    allow_reuse_address=True
    daemon_threads=True


class Forward(socketserver.BaseRequestHandler):
    def handle(self):
        try:
            with socket.create_connection(self.server.destination,timeout=10) as remote:
                self.request.settimeout(60);remote.settimeout(60)
                peers={self.request:remote,remote:self.request}
                while peers:
                    readable,_,_=select.select(list(peers),[],[],60)
                    if not readable:return
                    for source in readable:
                        data=source.recv(65536)
                        if data:peers[source].sendall(data)
                        else:
                            peers[source].shutdown(socket.SHUT_WR)
                            del peers[source]
        except OSError:
            # Readiness probes and peers retry temporary closed connections.
            return


class UDPForward(socketserver.BaseRequestHandler):
    def handle(self):
        data,listener=self.request
        with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as remote:
            remote.settimeout(5)
            try:
                remote.sendto(data,('coredns',53))
                answer,_=remote.recvfrom(65535)
                listener.sendto(answer,self.client_address)
            except OSError:return


def start_forwards():
    servers=[]
    for port,dest in FORWARDS.items():
        server=TCPServer(('127.0.0.1',port),Forward);server.destination=dest
        threading.Thread(target=server.serve_forever,daemon=True).start();servers.append(server)
    server=socketserver.ThreadingUDPServer(('127.0.0.1',9053),UDPForward)
    server.daemon_threads=True
    threading.Thread(target=server.serve_forever,daemon=True).start();servers.append(server)
    return servers


def wait_dependencies():
    deadline=time.monotonic()+180
    pending=set(FORWARDS.values())
    while pending and time.monotonic()<deadline:
        for address in tuple(pending):
            try:
                with socket.create_connection(address,timeout=1):pending.remove(address)
            except OSError:pass
        if pending:time.sleep(1)
    if pending:raise ValueError('dependency ports unavailable: '+str(sorted(pending)))


def stage():
    WORK.mkdir()
    for item in SOURCE.iterdir():
        if item.name not in ('services','dist','.git'):
            (WORK/item.name).symlink_to(item,target_is_directory=item.is_dir())
    (WORK/'services').mkdir();(WORK/'dist').mkdir()
    configs=json.loads(Path('/fixtures/configs.json').read_text())
    for name,config in configs.items():
        directory=WORK/'services'/name;directory.mkdir()
        original=SOURCE/'services'/name
        for child in original.iterdir():
            if child.name!=name+'.integration.yaml':
                (directory/child.name).symlink_to(child,target_is_directory=child.is_dir())
        (directory/(name+'.integration.yaml')).write_text(json.dumps(config))
        (WORK/'dist'/name).symlink_to(Path(shutil.which(name)).resolve(strict=True))
    for name in ('integration','nginx'):
        (WORK/'dist'/name).symlink_to(Path(shutil.which(name)).resolve(strict=True))
    (WORK/'services/nginz').mkdir()
    (WORK/'services/nginz/integration-test').symlink_to(SOURCE/'services/nginz/integration-test',target_is_directory=True)
    config=WORK/'services/integration.yaml'
    shutil.copyfile('/fixtures/integration.json',config)
    Path('/tmp/home').mkdir()
    return config


def initialize():
    # TLS remains verified against the repository's synthetic test CA.
    ca=SOURCE/'deploy/dockerephemeral/rabbitmq-config/certificates/ca.pem'
    for vhost in ('backendA','backendB','d1.example.com','d2.example.com','d3.example.com',
                  'federation-v0','federation-v1','federation-v2'):
        execute(['curl','--fail','--retry','30','--retry-delay','1','--retry-all-errors',
                 '--cacert',ca,'-u','guest:alpaca-grapefruit','-X','PUT',
                 'https://localhost:15671/api/vhosts/'+vhost],timeout=90)
    schema_jobs=[(service,service+'_test'+suffix) for service in ('brig','galley','gundeck','spar') for suffix in ('','2')]
    schema_jobs += [(service,service+'_test_dyn_'+str(i)) for service in ('brig','galley','gundeck','spar') for i in range(1,4)]
    def migrate(job):
        service,space=job
        execute([service+'-schema','--keyspace',space,'--replication-factor','1','--reset'],timeout=300)
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        list(pool.map(migrate,schema_jobs))
    for prefix in ('directory','directory2','directory_dyn_1','directory_dyn_2','directory_dyn_3'):
        execute(['brig-index','reset','--elasticsearch-index-prefix',prefix,
                 '--elasticsearch-server','https://localhost:9200','--elasticsearch-ca-cert',
                 SOURCE/'libs/wire-subsystems/test/resources/elasticsearch-ca.pem',
                 '--elasticsearch-credentials',SOURCE/'libs/wire-subsystems/test/resources/elasticsearch-credentials.yaml'])
    execute(['bash',SOURCE/'deploy/dockerephemeral/init.sh'],timeout=180)
    for name in ('backendA','backendB','dyn-1','dyn-2','dyn-3'):
        execute(['brig','-c',WORK/'services/brig/brig.integration.yaml','migrate-postgres','--dbname',name],cwd=WORK/'services/brig')


def nginz_config(index):
    base=WORK/('nginz-'+str(index))
    shutil.copytree(SOURCE/'services/nginz/integration-test',base)
    conf=base/'conf/nginz'
    (conf/'integration.conf').write_text(f'listen {8080+1000*index};\nlisten {8099+1000*index};\nlisten {8443+1000*index} ssl;\nhttp2 on;\n')
    (conf/'pid.conf').write_text('pid '+str(base/'nginz.pid')+';\n')
    peers=json.loads(Path('/fixtures/peers.json').read_text())[str(index)]
    names={'background-worker':'backgroundWorker','wire-server-enterprise':'wireServerEnterprise'}
    upstreams=''
    for name in ('brig','cannon','cargohold','galley','gundeck','proxy','spar'):
        port=peers[name][names.get(name,name)]['port']
        upstreams+=f'upstream {name} {{ server 127.0.0.1:{port}; keepalive 32; }}\n'
    upstreams+=f'upstream federator_external {{ server 127.0.0.1:{8098+1000*index}; }}\n'
    (conf/'upstreams').write_text(upstreams)
    main=conf/'nginx.conf';main.write_text(main.read_text().replace('access_log /dev/stdout','access_log /dev/null'))
    return main,base


def start_peers(processes,logs):
    peers=json.loads(Path('/fixtures/peers.json').read_text())
    endpoints=[]
    for index in range(2):
        configs=peers[str(index)]
        for name,config in configs.items():
            path=WORK/(name+'-static-'+str(index)+'.json');path.write_text(json.dumps(config))
            args=[str(WORK/'dist'/name),'-c',str(path)]
            if name=='background-worker':args+=['--galley-config-file',str(WORK/('galley-static-'+str(index)+'.json'))]
            log=open('/reports/'+name+'-'+str(index)+'.log','w');logs.append(log)
            processes.append(subprocess.Popen(args,cwd=WORK/'services'/name,stdout=log,stderr=subprocess.STDOUT))
            key={'background-worker':'backgroundWorker','wire-server-enterprise':'wireServerEnterprise'}.get(name,name)
            endpoints.append(config[key]['port'])
        conf,cwd=nginz_config(index)
        log=open('/reports/nginz-'+str(index)+'.log','w');logs.append(log)
        processes.append(subprocess.Popen(['nginx','-c',str(conf),'-g','daemon off;','-e','/dev/stdout'],cwd=cwd,stdout=log,stderr=subprocess.STDOUT))
        endpoints += [8098+1000*index,8443+1000*index]
    deadline=time.monotonic()+120
    pending=set(endpoints)
    while pending and time.monotonic()<deadline:
        if any(process.poll() is not None for process in processes):
            raise ValueError('static Haskell peer exited; inspect retained peer logs')
        for port in tuple(pending):
            try:
                with socket.create_connection(('127.0.0.1',port),timeout=1):pending.remove(port)
            except OSError:pass
        if pending:time.sleep(1)
    if pending:raise ValueError('static peer ports unavailable: '+str(sorted(pending)))


def selections(service, candidate, peers):
    selected = {}
    for name, path in peers:
        if name not in ('proxy','cargohold','cannon') or name in selected:
            raise ValueError('unsupported or duplicate service selection')
        selected[name] = Path(path).resolve(strict=True)
    if candidate is not None:
        if service not in ('proxy','cargohold','cannon') or service in selected:
            raise ValueError('ambiguous primary candidate selection')
        selected[service] = candidate.resolve(strict=True)
    if service == 'proxy-cargohold' and set(selected) != {'proxy','cargohold'}:
        raise ValueError('combined run requires both Go components')
    if service == 'proxy-cargohold-cannon' and set(selected) != {'proxy','cargohold','cannon'}:
        raise ValueError('combined run requires all three Go components')
    for path in selected.values():
        if not path.is_file() or not os.access(path,os.X_OK):
            raise ValueError('selected candidate is not executable')
    return selected


def run(service,candidate=None,peer_candidates=()):
    if os.getuid()!=65534 or os.environ.get('WIRE_E2E_DISPOSABLE')!='1':
        raise ValueError('requires isolated non-root environment controller')
    import runner
    selected = selections(service,candidate,peer_candidates)
    processes=[];logs=[];servers=[]
    try:
        servers=start_forwards();wait_dependencies();config=stage();initialize()
        for name, path in selected.items():
            link=WORK/'dist'/name;link.unlink();link.symlink_to(path)
        implementations = {link.name: {'implementation':'go' if link.name in selected else 'haskell',
                           'path':str(link.resolve(strict=True)),
                           'sha256':hashlib.sha256(link.read_bytes()).hexdigest()}
                           for link in (WORK/'dist').iterdir() if link.name not in ('integration','nginx')}
        start_peers(processes,logs)
        os.chdir(WORK)
        suites = ('proxy','cargohold','cannon') if service == 'proxy-cargohold-cannon' else ('proxy','cargohold') if service == 'proxy-cargohold' else (service,)
        reports = {}
        for name in suites:
            report, code = runner.suite(name,selected.get(name),config,Path('/reports')/(name+'.xml'),implementations)
            reports[name] = report
            if code: return {'result':'failed','suites':reports}, code
        if service not in ('proxy-cargohold','proxy-cargohold-cannon'): return report, 0
        return {'result':'pass','e2e_executed':True,'implementations':implementations,'suites':reports}, 0
    finally:
        for process in processes:
            if process.poll() is None:process.terminate()
        for process in processes:
            try:process.wait(timeout=5)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
        for log in logs:log.close()
        for server in servers:server.server_close()
