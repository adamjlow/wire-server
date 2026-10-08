"""Harness failures must not masquerade as service compatibility evidence."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import harness


class HarnessTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def test_runtime_checks_are_selected_for_changed_inputs_not_bookkeeping(self):
        self.assertFalse(harness.proxy_runtime_changed(['AGENTS.md', 'go/services/proxy/README.md', '.migration/services/proxy.json', 'docs/go-migration/services/proxy/oracle/live-final.json', 'go/services/proxy/server_test.go']))
        for path in ('go/services/proxy/server.go', 'go/services/proxy/go.mod', 'go/services/proxy/build_image.py', 'docs/go-migration/services/proxy/oracle/capture.py', 'docs/go-migration/services/proxy/oracle/provider/main.go', 'tools/parity/harness.py'):
            with self.subTest(path=path): self.assertTrue(harness.proxy_runtime_changed([path]))

    def test_required_original_test_catalogue_matches_source(self):
        self.assertEqual(5,len(harness.required_tests('proxy')))
        self.assertGreater(len(harness.required_tests('cargohold')),15)
        cannon=harness.required_tests('cannon')
        self.assertEqual(35,len(cannon))
        self.assertIn('Events.testTypingIndicatorIsNotSentToOwnClient[federated=false]',cannon)
        self.assertIn('Events.testTypingIndicatorIsNotSentToOwnClient[federated=true]',cannon)
        with self.assertRaises(ValueError): harness.required_tests('unknown')

    def test_empty_missing_extra_duplicate_failed_skipped_reports_fail(self):
        expected=['Proxy.testProxyGiphy']
        xml=self.root/'report.xml'
        for body in ('','<testsuites/>',
                     '<testsuites><testsuite tests="1"><testcase name="wrong"/></testsuite></testsuites>',
                     '<testsuites><testsuite tests="2"><testcase name="Proxy.testProxyGiphy"/><testcase name="Proxy.testProxyGiphy"/></testsuite></testsuites>',
                     *[f'<testsuites><testsuite tests="1"><testcase name="Proxy.testProxyGiphy"><{kind}/></testcase></testsuite></testsuites>' for kind in ('failure','error','skipped')],
                     '<!DOCTYPE testsuites><testsuites/>'):
            xml.write_text(body)
            with self.assertRaises((ValueError,harness.ET.ParseError)): harness.check_xml(xml,expected)
        xml.write_text('<testsuites><testsuite tests="1" failures="0"><testcase name="Proxy.testProxyGiphy"/></testsuite></testsuites>')
        self.assertEqual(1,harness.check_xml(xml,expected)['tests'])

    def test_suite_count_and_failure_metadata_must_agree(self):
        xml=self.root/'report.xml'
        for metadata in ('tests="0"','tests="1" failures="1"','tests="1" skipped="1"'):
            xml.write_text(f'<testsuites><testsuite {metadata}><testcase name="required"/></testsuite></testsuites>')
            with self.assertRaises(ValueError): harness.check_xml(xml,['required'])

    def test_artifacts_cannot_overwrite_or_follow_parent_symlink(self):
        with self.assertRaises(ValueError): harness.new_directory(self.root)
        link=self.root/'link';link.symlink_to(self.root,target_is_directory=True)
        with self.assertRaises(ValueError): harness.new_directory(link/'new')
        with self.assertRaises(ValueError): harness.new_directory(harness.ROOT/'new-artifacts')

    def test_staging_changes_only_selected_binary_and_retains_real_cwd(self):
        source=self.root/'source';source.mkdir()
        (source/'services').mkdir();(source/'services/integration.yaml').write_text('synthetic config')
        for service in ('proxy','brig'):
            (source/'services'/service).mkdir()
            (source/'services'/service/(service+'.integration.yaml')).write_text('synthetic configuration')
        (source/'integration').mkdir();(source/'integration/original.hs').write_text('unchanged assertions')
        dist=self.root/'haskell';dist.mkdir()
        for name in ('integration','run-services','proxy','brig'):
            binary=dist/name;binary.write_text('synthetic '+name);binary.chmod(0o755)
        candidate=self.root/'go-proxy';candidate.write_text('synthetic go');candidate.chmod(0o755)
        output=self.root/'staged'
        with patch.object(harness,'required_tests',return_value=['Proxy.testProxyGiphy']):
            report=harness.prepare_e2e('proxy',dist,candidate,output,source)
        self.assertFalse(report['e2e_executed'])
        for role in ('reference','candidate'):
            cwd=output/role/'services/proxy'
            self.assertFalse(cwd.is_symlink())
            self.assertEqual(candidate if role=='candidate' else dist/'proxy',(cwd/'../../dist/proxy').resolve())
            self.assertEqual(dist/'brig',(output/role/'dist/brig').resolve())
            self.assertEqual('unchanged assertions',(output/role/'integration/original.hs').read_text())
        self.assertEqual('synthetic proxy',(dist/'proxy').read_text())
        self.assertEqual('synthetic config',(source/'services/integration.yaml').read_text())
        self.assertIn('--xml',report['commands']['candidate'])

    def test_missing_binary_blocks_before_creating_artifacts(self):
        dist=self.root/'empty';dist.mkdir()
        output=self.root/'not-created'
        with self.assertRaises(ValueError): harness.prepare_e2e('proxy',dist,self.root/'missing',output)
        self.assertFalse(output.exists())

    def test_failed_live_comparison_does_not_produce_pass(self):
        output=self.root/'run';output.mkdir()
        with patch.object(harness.subprocess,'check_output',side_effect=['','go version go1.26.8 linux/amd64']),patch.object(harness,'executable',return_value=Path('/synthetic-go')),patch.object(harness,'new_directory',return_value=output),patch.object(harness,'run',side_effect=[None,None,None,harness.subprocess.CalledProcessError(1,['capture'])]):
            with self.assertRaises(harness.subprocess.CalledProcessError): harness.proxy(output,'api')
        self.assertFalse((output/'result.json').exists())
