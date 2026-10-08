"""Capture isolation checks; not Haskell parity evidence."""
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('cargo_reference',Path(__file__).with_name('reference.py'))
reference=importlib.util.module_from_spec(spec);spec.loader.exec_module(reference)


class ReferenceTests(unittest.TestCase):
    def test_signed_fixture_download_cannot_escape_private_store(self):
        for url in ('https://example.com/private','http://localhost:80/private','http://127.0.0.1:4570/private'):
            with self.subTest(url=url),patch.object(reference,'request') as request,self.assertRaises(ValueError):
                reference.signed_location(url)
            request.assert_not_called()

    def test_relative_upload_location_is_retained_without_network_follow(self):
        reply=(201,{'content-type':'application/json','location':'/bot/assets/3-2-synthetic'},b'{"key":"synthetic"}')
        with patch.object(reference,'request',return_value=reply),patch.object(reference,'signed_location') as follow:
            record=reference.observe('synthetic','POST','/bot/assets',expected=201)
        self.assertEqual('/bot/assets/3-2-synthetic',record['location']);follow.assert_not_called()

    def test_raw_signatures_are_not_retained_in_observed_download(self):
        url='http://localhost:4570/dummy-bucket/object?X-Amz-Expires=300&X-Amz-Signature=synthetic-signature'
        with patch.object(reference,'request',return_value=(200,{},reference.DATA)) as request:
            result=reference.signed_location(url)
        self.assertEqual({'Host':'localhost:4570'},request.call_args.args[2]);self.assertNotIn('X-Amz-Signature',result['query']);self.assertEqual(['300'],result['query']['X-Amz-Expires'])

    def test_incomplete_upload_fixture_contains_only_short_declared_body(self):
        body=reference.multipart(payload=reference.DATA[:-3],declared=str(len(reference.DATA)),trailer=False)
        self.assertTrue(body.endswith(reference.DATA[:-3]));self.assertNotIn(b'--wire--',body)
        self.assertIn(b'Content-Length: '+str(len(reference.DATA)).encode()+b'\r\n',body)

if __name__=='__main__':unittest.main()
