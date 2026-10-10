"""Private dependency setup boundaries; these are not e2e execution evidence."""
import copy
import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

FILES=Path(__file__).resolve().parent
sys.path.insert(0,str(FILES))
import environment


class EnvironmentTests(unittest.TestCase):
    def spec(self):
        return {'name':'wire-e2e-'+'a'*32,'networks':{'private':{'internal':True}},
                'volumes':{'owned':{}},'services':{'test':{'image':'sha256:'+'b'*64,'networks':{'private':None}}}}

    def test_federator_mount_is_exact_and_read_only(self):
        spec=self.spec();spec['services']['test']['volumes']=[{'type':'bind','source':'/tmp/identified-federator','target':'/candidate/federator','read_only':True}]
        environment.validate_isolation(spec)
        for field,value in (('target','/candidate/brig'),('target','/candidate/federator-extra'),('read_only',False)):
            wrong=copy.deepcopy(spec);wrong['services']['test']['volumes'][0][field]=value
            with self.subTest(field=field,value=value),self.assertRaises(ValueError):environment.validate_isolation(wrong)

    def test_background_worker_mount_is_exact_and_read_only(self):
        spec=self.spec();spec['services']['test']['volumes']=[{'type':'bind','source':'/tmp/identified-worker','target':'/candidate/background-worker','read_only':True}]
        environment.validate_isolation(spec)
        for field,value in (('target','/candidate/background-worker-extra'),('target','/candidate/brig'),('read_only',False)):
            wrong=copy.deepcopy(spec);wrong['services']['test']['volumes'][0][field]=value
            with self.subTest(field=field,value=value),self.assertRaises(ValueError):environment.validate_isolation(wrong)

    def test_host_exposure_global_names_and_shared_resources_are_rejected(self):
        for field,value in (('ports',['9042:9042']),('privileged',True),('container_name','shared'),
                            ('network_mode','host'),('pid','host'),('devices',['/dev/vda'])):
            spec=self.spec();spec['services']['test'][field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):environment.validate_isolation(spec)
        for section in ('networks','volumes'):
            spec=self.spec();key=next(iter(spec[section]));spec[section][key]['external']=True
            with self.subTest(section=section),self.assertRaises(ValueError):environment.validate_isolation(spec)
        spec=self.spec();spec['services']['test']['volumes']=[{'type':'bind','source':'/var/run/docker.sock','target':'/docker.sock'}]
        with self.assertRaises(ValueError):environment.validate_isolation(spec)

    def test_implicit_or_foreign_networks_are_rejected(self):
        for networks in (None,{}, {'default':None}, {'private':None,'foreign':None}):
            spec=self.spec();spec['services']['test']['networks']=networks
            with self.subTest(networks=networks),self.assertRaises(ValueError):environment.validate_isolation(spec)

    def test_dns_fixture_uses_owned_storage_and_explicit_private_network(self):
        upstream={'services':{name:{'image':'synthetic:test','networks':{'demo_wire':None}} for name in environment.DEPENDENCIES}}
        upstream['services']['dns-server']['networks']={'default':None}
        with tempfile.TemporaryDirectory() as tmp:
            spec=environment.private_compose(upstream,'wire-e2e-'+'a'*32,Path(tmp),'sha256:'+'b'*64)
        dns=spec['services']['dns-server']
        self.assertEqual({'demo_wire':None},dns['networks'])
        self.assertEqual([{'type':'volume','source':'dns-server-data','target':'/etc/dns'}],dns['volumes'])
        self.assertEqual({},spec['volumes']['dns-server-data'])
        self.assertNotIn('ports',dns)
        self.assertTrue(all(n['internal'] for n in spec['networks'].values()))
        self.assertEqual({'nofile':{'soft':8192,'hard':8192}},spec['services']['runner']['ulimits'])
        self.assertTrue(all('ulimits' not in service for name,service in spec['services'].items() if name!='runner'))

    def test_peer_b_storage_queues_and_ports_are_isolated_from_a(self):
        original={'brig':{'port':8082},'cassandra':{'keyspace':'brig_test'}}
        before=copy.deepcopy(original)
        a=environment.peer_config(original,'brig',0);b=environment.peer_config(original,'brig',1)
        self.assertEqual(before,original)
        self.assertEqual(('brig_test','brig_test2'),(a['cassandra']['keyspace'],b['cassandra']['keyspace']))
        self.assertEqual((8082,9082),(a['brig']['port'],b['brig']['port']))
        self.assertNotEqual(a['internalEvents']['queueName'],b['internalEvents']['queueName'])
        self.assertNotEqual(a['postgresql']['dbname'],b['postgresql']['dbname'])
        self.assertNotEqual(a['rabbitmq']['vHost'],b['rabbitmq']['vHost'])

    def test_fixture_symlink_outside_checkout_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory=Path(tmp);outside=directory/'outside';outside.write_text('synthetic')
            root=directory/'root';root.mkdir();(root/'link').symlink_to(outside)
            from unittest.mock import patch
            with patch.object(environment,'ROOT',root),self.assertRaises(ValueError):
                environment.absolute_fixtures({'key':'link'},root)

    def test_cluster_and_s3_inputs_retain_original_peer_modes(self):
        base={'redis':{'connectionMode':'cluster','enableTls':True},'aws':{'s3Bucket':'dummy-bucket'}}
        self.assertEqual(base['redis'],environment.peer_config(base,'gundeck',1)['redis'])
        self.assertEqual('dummy-bucket2',environment.peer_config(base,'cargohold',1)['aws']['s3Bucket'])

    def test_log_timeout_still_cleans_only_the_owned_project(self):
        import json
        import subprocess
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as tmp:
            output=Path(tmp);(output/'reports').mkdir()
            spec=self.spec();spec['services']={name:{'image':'synthetic:test','networks':{'private':None}} for name in environment.DEPENDENCIES}
            spec['services']['runner']={'image':'sha256:'+'b'*64,'networks':{'private':None}}
            raw=json.dumps(spec);(output/'compose.json').write_text(raw)
            record={'project':spec['name'],'source_revision':'c'*40,'files_sha256':{},
                    'compose_sha256':environment.hashlib.sha256(raw.encode()).hexdigest()}
            (output/'environment.json').write_text(json.dumps(record))
            (output/'reports/proxy.run.json').write_text(json.dumps({'result':'pass','e2e_executed':True}))
            def command(args,**kwargs):
                if args[:3]==['git','rev-parse','HEAD']:return 'c'*40
                if args[:3]==['git','status','--porcelain']:return ''
                if args[:3]==['docker','network','ls']:return 'owned-test-network'
                if args[:3]==['docker','network','inspect']:return '[]'
                if args[:3]==['docker','image','inspect']:return json.dumps([{'RepoDigests':['synthetic@sha256:'+'d'*64]}])
                return 'PONG'
            calls=[]
            def execute(args,**kwargs):
                calls.append(args)
                if 'logs' in args:raise subprocess.TimeoutExpired(args,30)
                return subprocess.CompletedProcess(args,0)
            with patch.object(environment,'command',side_effect=command),patch.object(environment.subprocess,'run',side_effect=execute),self.assertRaises(subprocess.TimeoutExpired):
                environment.run(output,'proxy')
            self.assertEqual('down',calls[-1][6])
            self.assertEqual(spec['name'],calls[-1][3])
            self.assertIn('--volumes',calls[-1])
            self.assertEqual(0,json.loads((output/'environment.json').read_text())['cleanup_exit_code'])

if __name__=='__main__':unittest.main()
