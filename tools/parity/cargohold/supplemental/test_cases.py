import copy
from pathlib import Path
import sys
import unittest
sys.path.insert(0,str(Path(__file__).resolve().parent))
from cases import normalize_signed

class SignedNormalizationTests(unittest.TestCase):
    def result(self):
        return {'read_status':200,'scheme':'http','host':'localhost:4570','path':'/bucket/key','body_sha256':'fixture',
                'query':{'X-Amz-Algorithm':['AWS4-HMAC-SHA256'],'X-Amz-Date':['20261008T120000Z'],
                         'X-Amz-Credential':['synthetic/20261008/eu-west-1/s3/aws4_request'],
                         'X-Amz-SignedHeaders':['host'],'X-Amz-Expires':['300'],'fixed-marker':['retained']}}
    def helper(self,result):
        class Ref:
            def signed_location(self,location):return copy.deepcopy(result)
        return Ref()
    def test_preserves_fixed_fields_and_verified_bytes(self):
        raw=self.result();result=normalize_signed(self.helper(raw),'owned-url')
        self.assertEqual(result['query']['fixed-marker'],['retained'])
        self.assertEqual(result['query']['X-Amz-Expires'],['300'])
        self.assertEqual(result['host'],raw['host'])
        self.assertEqual(result['body_sha256'],raw['body_sha256'])
        self.assertNotIn('X-Amz-Date',result['query'])
        self.assertEqual(result['query']['X-Amz-Credential'],['synthetic/<validated-signing-date>/eu-west-1/s3/aws4_request'])
    def test_invalid_signature_clock_and_scope_cannot_normalize(self):
        for change in [lambda r:r.update(read_status=403),lambda r:r['query'].update({'X-Amz-Date':['bad']}),
                       lambda r:r['query'].update({'X-Amz-Credential':['synthetic/20261009/eu-west-1/s3/aws4_request']}),
                       lambda r:r['query'].update({'X-Amz-Algorithm':['incorrect']})]:
            raw=self.result();change(raw)
            with self.assertRaises(ValueError):normalize_signed(self.helper(raw),'owned-url')

if __name__=='__main__':unittest.main()
