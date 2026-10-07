"""Meaningful operational mismatches must fail even when the API suite passes."""
import base64
import json
from pathlib import Path
import unittest

import check_operations


class OperationalComparisonTests(unittest.TestCase):
    def report(self):
        return json.loads((Path(__file__).parent / 'live-final.json').read_text())

    def test_retained_operational_capture_is_valid(self):
        result = check_operations.check(self.report())
        self.assertEqual('pass', result['result'])
        self.assertFalse(result['parity_verified'])

    def test_missing_coverage_bad_secret_and_shutdown_cannot_pass(self):
        for change in ('missing', 'secret', 'drain', 'exit', 'version', 'switchback', 'load', 'memory', 'latency', 'log'):
            with self.subTest(change=change):
                report = self.report()
                operations = report['operations']
                if change == 'missing': del operations['valid_reload']['candidate']
                if change == 'secret':
                    for value in operations['invalid_reload_retention'].values(): value['effects'][0]['target'] = '/incorrect'
                if change == 'drain': operations['inflight_active_before_signal'] = False
                if change == 'exit': operations['inflight_exit_codes']['candidate'] = 137
                if change == 'version': operations['disabled_versions']['candidate']['v0']['response']['status'] = 200
                if change == 'switchback': operations['switchback']['image'] = 'untrusted'
                if change == 'load': operations['performance']['candidate']['samples'].pop()
                if change == 'memory': operations['performance']['candidate']['memory_usage'] = '51MiB / 1GiB'
                if change == 'latency':
                    for sample in operations['performance']['candidate']['samples']: sample['seconds'] = 100
                if change == 'log': operations['structured_logs']['candidate'] = '{"level":"Debug","msgs":[]}'
                with self.assertRaises((ValueError, KeyError)): check_operations.check(report)

    def test_metric_counters_labels_and_invalid_samples_cannot_pass(self):
        for change in ('counter', 'labels', 'nan', 'bucket'):
            with self.subTest(change=change):
                report = self.report()
                response = report['candidate_metrics']
                body = base64.b64decode(response['body_base64']).decode()
                if change == 'counter':
                    body = '\n'.join('net_errors 999.0' if line.startswith('net_errors ') else line for line in body.splitlines())+'\n'
                if change == 'labels': body = body.replace('method="GET"', 'method="WRONG"')
                if change == 'nan': body = body.replace('net_connections 1.0', 'net_connections NaN')
                if change == 'nan' and 'NaN' not in body:
                    body = '\n'.join('net_connections NaN' if line.startswith('net_connections ') else line for line in body.splitlines())+'\n'
                if change == 'bucket':
                    lines = body.splitlines()
                    index = next(i for i,line in enumerate(lines) if line.startswith('http_request_duration_seconds_bucket'))
                    lines[index] = lines[index].rsplit(' ',1)[0]+' 999'
                    body = '\n'.join(lines)+'\n'
                response['body_base64'] = base64.b64encode(body.encode()).decode()
                with self.assertRaises(ValueError): check_operations.check(report)
