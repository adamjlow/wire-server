"""Bounded stale-delivery regression against real Redis and the final binary."""
import hashlib,http.client,http.server,json,os,subprocess,sys,tempfile,threading,time
from pathlib import Path
sys.path.insert(0,'/source/docs/go-migration/services/gundeck/oracle')
from capture_http_presence import redis_command
assert os.getuid()==65534 and os.environ.get('WIRE_E2E_DISPOSABLE')=='1'
user='cccccccc-cccc-4ccc-8ccc-cccccccccccc';entered=threading.Event();release=threading.Event();servers=[];process=None
class Brig(http.server.BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  self.rfile.read(int(self.headers.get('Content-Length','0')));raw=json.dumps({user:[]}).encode();self.send_response(200);self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
class Cannon(http.server.BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  body=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))));entered.set();assert release.wait(3)
  items=[{'notif_id':item['notification']['id'],'target':target,'status':'push_status_gone'} for item in body['bulkpush_req'] for target in item['targets']]
  assert len(items)==2 and {item['target']['conn_id'] for item in items}=={'a-stale','z-control'}
  items.sort(key=lambda item:item['target']['conn_id'])
  raw=json.dumps({'bulkpush_resp':items}).encode();self.send_response(200);self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
def request(method,path,body=None):
 c=http.client.HTTPConnection('127.0.0.1',16196,timeout=5)
 try:
  c.request(method,path,None if body is None else json.dumps(body),{'Content-Type':'application/json','Z-User':user});r=c.getresponse();raw=r.read(65537);assert len(raw)<=65536
  return r.status,json.loads(raw) if raw else None
 finally:c.close()
def presence(device,suffix='old'):return {'user_id':user,'device_id':device,'client_id':'abcd','resource':'http://127.0.0.1:16200/'+suffix}
try:
 for cls,port in [(Brig,16201),(Cannon,16200)]:
  server=http.server.ThreadingHTTPServer(('127.0.0.1',port),cls);servers.append(server);threading.Thread(target=server.serve_forever,daemon=True).start()
 config=json.loads(Path('/fixtures/peers.json').read_text())['0']['gundeck'];config['gundeck']['port']=16196;config['brig']={'host':'127.0.0.1','port':16201}
 with tempfile.TemporaryDirectory(prefix='gundeck-generation-') as temp,Path('/reports/generation.log').open('w') as log:
  path=Path(temp)/'config.json';path.write_text(json.dumps(config));process=subprocess.Popen(['/candidate/gundeck','-c',str(path)],stdout=log,stderr=log)
  deadline=time.monotonic()+30
  while True:
   assert process.poll() is None
   try:
    if request('GET','/i/status')[0]==200:break
   except OSError:pass
   assert time.monotonic()<deadline;time.sleep(.1)
  for device in ['a-stale','z-control']:assert request('POST','/i/presences',presence(device))[0]==201
  push={'recipients':[{'user_id':user,'route':'direct','clients':[]}],'origin':user,'payload':[{'type':'synthetic.generation'}]}
  assert request('POST','/i/push/v2',[push])[0]==200 and entered.wait(3)
  fresh=presence('a-stale','new');fresh['client_id']='dcba';assert request('POST','/i/presences',fresh)[0]==201
  release.set();deadline=time.monotonic()+3
  while True:
   code,values=request('GET','/i/presences/'+user);assert code==200
   if len(values)==1:break
   assert time.monotonic()<deadline;time.sleep(.02)
  assert values[0]['device_id']=='a-stale' and values[0]['resource']==fresh['resource'] and values[0]['client_id']=='dcba'
  assert request('DELETE','/i/user')[0]==200;redis_command('DEL','user:'+user)
  process.terminate();process.wait(timeout=4);assert process.returncode==0
  report={'result':'pass','candidate_sha256':hashlib.sha256(Path('/candidate/gundeck').read_bytes()).hexdigest(),'script_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'checks':['old gone response cannot delete replacement resource/client/generation','unchanged control presence is deleted, proving cleanup completed','owned Cassandra/Redis user cleanup','clean bounded shutdown'],'shutdown_exit_code':process.returncode}
  Path('/reports/generation.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
finally:
 release.set()
 if process and process.poll() is None:
  process.terminate()
  try:process.wait(timeout=4)
  except subprocess.TimeoutExpired:process.kill();process.wait(timeout=4)
 for server in servers:server.shutdown();server.server_close()
