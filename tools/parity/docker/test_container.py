"""Image/preflight boundaries: inspection never establishes e2e test success."""
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import socket
import socketserver
import threading
import time
import unittest
from unittest.mock import patch

FILES=Path(__file__).resolve().parent
sys.path.insert(0,str(FILES.parent))

def module(name):
    spec=importlib.util.spec_from_file_location(name,FILES/(name+'.py'))
    result=importlib.util.module_from_spec(spec);spec.loader.exec_module(result);return result

control=module('control')
runner=module('runner')
runtime=module('runtime')


class ContainerTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup);self.root=Path(self.tmp.name)

    def test_artifacts_do_not_overwrite_or_follow_symlink_parents(self):
        output=self.root/'existing';output.write_text('owned')
        with self.assertRaises(ValueError):control.destination(output)
        link=self.root/'link';link.symlink_to(self.root,target_is_directory=True)
        with self.assertRaises(ValueError):control.destination(link/'new')

    def test_combined_selection_requires_exactly_two_executable_candidates(self):
        executable=self.root/'candidate';executable.write_text('synthetic');executable.chmod(0o755)
        pairs=[('proxy',str(executable)),('cargohold',str(executable))]
        self.assertEqual({'proxy','cargohold'},set(runtime.selections('proxy-cargohold',None,pairs)))
        for peers in (pairs[:1],pairs+[pairs[0]],pairs+[('brig',str(executable))]):
            with self.assertRaises(ValueError):runtime.selections('proxy-cargohold',None,peers)
        with self.assertRaises(ValueError):runtime.selections('proxy-cargohold',executable,pairs)
        executable.chmod(0o644)
        with self.assertRaises(ValueError):runtime.selections('proxy-cargohold',None,pairs)

    def test_three_service_selection_cannot_omit_cannon(self):
        executable=self.root/'candidate';executable.write_text('synthetic');executable.chmod(0o755)
        pairs=[(name,str(executable)) for name in ('proxy','cargohold','cannon')]
        self.assertEqual({'proxy','cargohold','cannon'},set(runtime.selections('proxy-cargohold-cannon',None,pairs)))
        with self.assertRaises(ValueError):runtime.selections('proxy-cargohold-cannon',None,pairs[:2])
        self.assertEqual({'cannon'},set(runtime.selections('cannon',executable,[])))

    def test_four_service_selection_requires_exactly_four_candidates(self):
        executable=self.root/'candidate';executable.write_text('synthetic');executable.chmod(0o755)
        pairs=[(name,str(executable)) for name in ('proxy','cargohold','cannon','gundeck')]
        self.assertEqual({'proxy','cargohold','cannon','gundeck'},set(runtime.selections('proxy-cargohold-cannon-gundeck',None,pairs)))
        for invalid in (pairs[:3],pairs+[pairs[-1]],pairs+[('brig',str(executable))]):
            with self.assertRaises(ValueError):runtime.selections('proxy-cargohold-cannon-gundeck',None,invalid)
        self.assertEqual({'gundeck'},set(runtime.selections('gundeck',executable,[])))
        with self.assertRaises(ValueError):runtime.selections('proxy-cargohold-cannon-gundeck',executable,pairs)

    def test_six_service_selection_rejects_missing_duplicate_foreign_and_ambiguous(self):
        executable=self.root/'candidate';executable.write_text('synthetic');executable.chmod(0o755)
        names=('proxy','cargohold','cannon','gundeck','federator','background-worker')
        pairs=[(name,str(executable)) for name in names]
        profile='proxy-cargohold-cannon-gundeck-federator-background-worker'
        self.assertEqual(set(names),set(runtime.selections(profile,None,pairs)))
        for invalid in (pairs[:-1],pairs+[pairs[-1]],pairs+[('brig',str(executable))]):
            with self.assertRaises(ValueError):runtime.selections(profile,None,invalid)
        with self.assertRaises(ValueError):runtime.selections(profile,executable,pairs)
        self.assertEqual({'background-worker'},set(runtime.selections('background-worker',executable,[])))
        self.assertEqual({},runtime.selections('background-worker',None,[]))

    def test_background_worker_catalogue_matches_actual_compiled_discovery(self):
        discovered=json.loads((FILES.parents[2]/'docs/go-migration/services/background-worker/test-discovery.json').read_text())['worker_cases']
        names=runner.names('background-worker')
        self.assertEqual(47,len(names));self.assertEqual(47,len(set(names)))
        self.assertEqual(set(discovered),set(names))

    def test_gundeck_catalogue_keeps_original_cannon_cases_and_adds_provider_cases(self):
        original=runner.CATALOGUE
        try:
            runner.CATALOGUE=FILES.parent/'e2e-tests.json'
            names=runner.names('gundeck')
            self.assertEqual(40,len(names));self.assertEqual(40,len(set(names)))
            self.assertTrue(set(runner.names('cannon')).issubset(names))
            self.assertIn('PushToken.testRegisterPushToken',names)
            self.assertIn('PushToken.testVoipTokenRegistrationFails',names)
        finally:runner.CATALOGUE=original

    def test_persistent_tunnel_survives_idle_polls_and_preserves_half_close(self):
        class Echo(socketserver.BaseRequestHandler):
            def handle(self):
                time.sleep(0.12)
                self.request.sendall(b'ready')
                data = self.request.recv(1024)
                self.request.sendall(data)
                self.assert_eof = self.request.recv(1024)
                self.request.sendall(b'finished')

        class FastPoll(runtime.Forward):
            poll_interval = 0.02

        with runtime.TCPServer(('127.0.0.1', 0), Echo) as upstream, runtime.TCPServer(('127.0.0.1', 0), FastPoll) as tunnel:
            tunnel.destination = upstream.server_address
            workers = [threading.Thread(target=server.serve_forever, daemon=True) for server in (upstream, tunnel)]
            for worker in workers:worker.start()
            try:
                with socket.create_connection(tunnel.server_address, timeout=2) as client:
                    self.assertEqual(b'ready', client.recv(1024))
                    client.sendall(b'query')
                    self.assertEqual(b'query', client.recv(1024))
                    client.shutdown(socket.SHUT_WR)
                    self.assertEqual(b'finished', client.recv(1024))
                    self.assertEqual(b'', client.recv(1024))
            finally:
                for server in (upstream, tunnel):server.shutdown()
                for worker in workers:worker.join(timeout=2)

    def test_foreign_image_receipt_rejected_before_starting_container(self):
        receipt=self.root/'receipt.json';image='sha256:'+'a'*64
        receipt.write_text(json.dumps({'image_id':image,'source_revision':'b'*40}))
        actual={'Id':image,'Config':{'User':'0','Labels':{'org.opencontainers.image.revision':'b'*40}}}
        with patch.object(control,'command',return_value=json.dumps([actual])) as cmd:
            with self.assertRaises(ValueError):control.preflight(receipt,self.root/'out')
            self.assertEqual(1,cmd.call_count)

    def test_compiled_catalogue_discovery_rejects_missing_tests(self):
        with patch.object(runner,'names',return_value=['Proxy.required']),patch.object(runner,'binary',return_value={'path':'/synthetic'}),patch.object(runner.subprocess,'run',return_value=runner.subprocess.CompletedProcess([],0,'Proxy.different','')):
            with self.assertRaises(ValueError):runner.discover('proxy')

    def test_discovery_is_not_an_e2e_pass(self):
        with patch.object(runner,'binary',return_value={'path':'/synthetic','sha256':'a'*64}),patch.object(runner,'discover',return_value=['required']):
            report=runner.preflight()
            self.assertFalse(report['e2e_executed']);self.assertFalse(report['environment_ready'])
            self.assertEqual('preflight-pass',report['result'])

    def test_suite_will_not_run_without_disposable_environment(self):
        config=self.root/'integration.yaml';config.write_text('synthetic')
        with patch.dict(runner.os.environ,{},clear=True),patch.object(runner,'discover') as discover:
            with self.assertRaises(ValueError):runner.suite('proxy',None,config,self.root/'report.xml')
            discover.assert_not_called()

    def test_empty_successful_process_report_is_rejected(self):
        config=self.root/'integration.yaml';config.write_text('synthetic');output=self.root/'report.xml'
        def execute(*args,**kwargs):
            output.write_text('<testsuites><testsuite tests="0"/></testsuites>')
            return runner.subprocess.CompletedProcess([],0)
        with patch.dict(runner.os.environ,{'WIRE_E2E_DISPOSABLE':'1'}),patch.object(runner,'discover',return_value=['required']),patch.object(runner,'binary',return_value={'path':'/synthetic'}),patch.object(runner.subprocess,'run',side_effect=execute):
            with self.assertRaises(ValueError):runner.suite('proxy',None,config,output)
        self.assertEqual('failed',json.loads(output.with_suffix('.run.json').read_text())['result'])
