import os, resource, socket, socketserver, threading, unittest, sys
sys.path.insert(0,"/opt/wire-e2e")
import runtime
class HighFD(unittest.TestCase):
    def test_high_descriptor_tunnel_preserves_payload_and_half_close(self):
        limits = resource.getrlimit(resource.RLIMIT_NOFILE)
        handles = []
        servers = []
        workers = []
        try:
            resource.setrlimit(resource.RLIMIT_NOFILE, (max(limits[0], 2048), limits[1]))
            while not handles or handles[-1] < 1100:
                handles.append(os.open('/dev/null', os.O_RDONLY))
    
            class Echo(socketserver.BaseRequestHandler):
    
                def handle(self):
                    data = bytearray()
                    while (chunk := self.request.recv(1024)):
                        data.extend(chunk)
                    self.request.sendall(bytes(data) + b':finished')
            upstream = runtime.TCPServer(('127.0.0.1', 0), Echo)
            servers.append(upstream)
            tunnel = runtime.TCPServer(('127.0.0.1', 0), runtime.Forward)
            servers.append(tunnel)
            tunnel.destination = upstream.server_address
            for server in servers:
                self.assertGreater(server.fileno(), 1023)
                worker = threading.Thread(target=server.serve_forever, daemon=True)
                worker.start()
                workers.append(worker)
            with socket.create_connection(tunnel.server_address, timeout=2) as client:
                self.assertGreater(client.fileno(), 1023)
                payload = b'synthetic-high-fd' * 1024
                client.sendall(payload)
                client.shutdown(socket.SHUT_WR)
                response = bytearray()
                while (chunk := client.recv(1024)):
                    response.extend(chunk)
                self.assertEqual(payload + b':finished', bytes(response))
        finally:
            for server in servers[:len(workers)]:
                server.shutdown()
            for worker in workers:
                worker.join(timeout=2)
            for server in servers:
                server.server_close()
            for handle in handles:
                os.close(handle)
            resource.setrlimit(resource.RLIMIT_NOFILE, limits)

assert resource.getrlimit(resource.RLIMIT_NOFILE)==(8192,8192)
unittest.main()
