import sys,socket,socketserver,threading,resource,os,json,errno,time
sys.path.insert(0,'/opt/wire-e2e')
import runtime
original=resource.getrlimit(resource.RLIMIT_NOFILE)
resource.setrlimit(resource.RLIMIT_NOFILE,(128,original[1]))
class Echo(socketserver.BaseRequestHandler):
 def handle(self):
  data=self.request.recv(100)
  self.request.sendall(data)
upstream=runtime.TCPServer(('127.0.0.1',0),Echo)
forward=runtime.TCPServer(('127.0.0.1',0),runtime.Forward);forward.destination=upstream.server_address
for server in [upstream,forward]:threading.Thread(target=server.serve_forever,daemon=True).start()
errors=[];create_connection=socket.create_connection
# Observe exactly the caught OSError; forwarding decisions stay unmodified.
def observed(*args,**kwargs):
 try:return create_connection(*args,**kwargs)
 except OSError as e:errors.append(e.errno);raise
socket.create_connection=observed
client=socket.socket();client.settimeout(3)
holds=[]
try:
 while True:
  try:holds.append(os.open('/dev/null',os.O_RDONLY))
  except OSError as e:
   assert e.errno==errno.EMFILE;break
 # Reserve one accepted-client FD, leaving no outbound-connection FD.
 os.close(holds.pop())
 client.connect(forward.server_address);client.sendall(b'synthetic-request')
 try:
  got=client.recv(100);closed=got==b''
 except ConnectionResetError:closed=True
 assert closed,'exhausted forwarder did not close connection'
 assert errno.EMFILE in errors,'expected caught descriptor exhaustion'
finally:
 client.close()
 for fd in holds:os.close(fd)
# Same original forwarder passes once resources are available.
with create_connection(forward.server_address,timeout=3) as client:
 client.sendall(b'synthetic-request');assert client.recv(100)==b'synthetic-request'
for server in [forward,upstream]:server.shutdown();server.server_close()
print(json.dumps({'original_soft_limit':original[0],'scaled_fixture_soft_limit':128,'caught_errno':errors,'exhausted_connection_closed':closed,'normal_echo_pass':True,'original_run_attribution':'not proven; original run did not capture FD high-water','runtime_code':'pinned image original Forward, observed create_connection exceptions without changing forwarding decisions'}))
