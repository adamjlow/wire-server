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
                'volumes':{'owned':{}},'services':{'test':{'image':'sha256:'+'b'*64}}}

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

if __name__=='__main__':unittest.main()
