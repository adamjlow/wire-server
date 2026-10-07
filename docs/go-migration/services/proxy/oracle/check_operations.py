#!/usr/bin/env python3
"""Verify captured operational checks; success is bounded to documented coverage."""
import argparse
import base64
from datetime import datetime
import json
import math
from pathlib import Path
import re

import capture
import compare

require = compare.require


def metrics(response):
    require(response['status'] == 200 and response['headers'].get('Content-Type') == ['text/plain; version=0.0.4'], 'metrics endpoint contract')
    schema, samples = {}, {}
    for line in base64.b64decode(response['body_base64'], validate=True).decode().splitlines():
        if line.startswith('# '):
            _, kind, name, description = line.split(' ', 3)
            require((kind, name) not in schema, 'duplicate metric metadata')
            schema[kind, name] = description
            continue
        match = re.fullmatch(r'(\w+)(?:\{(.*)\})? ([^ ]+)', line)
        require(match is not None, 'invalid metric sample')
        name, raw, number = match.groups()
        labels = tuple(sorted(re.findall(r'(\w+)="([^"]*)"', raw or '')))
        require(raw is None or ','.join(f'{k}="{v}"' for k, v in re.findall(r'(\w+)="([^"]*)"', raw)) == raw, 'invalid metric labels')
        value = float(number)
        require(math.isfinite(value) and value >= 0 and (name, labels) not in samples, 'invalid/duplicate metric value')
        samples[name, labels] = value
    expected = {'net_errors', 'net_connections', 'http_request_duration_seconds', 'gc_bytes_allocated_total',
                'gc_bytes_used_max', 'gc_bytes_used_current', 'gc_seconds_cpu', 'gc_seconds_wall'}
    require({name for kind, name in schema if kind == 'TYPE'} == expected, 'metric names missing/extra')
    for (name, labels), count in samples.items():
        if name != 'http_request_duration_seconds_count':
            continue
        previous = 0
        for bound in ('.005', '.01', '.025', '.05', '.1', '.25', '.5', '1.0', '2.5', '5.0', '10.0', '+Inf'):
            # Haskell uses leading zero in decimal labels.
            key = ('http_request_duration_seconds_bucket', tuple(sorted((*labels, ('le', '0'+bound if bound.startswith('.') else bound)))))
            require(key in samples and previous <= samples[key] <= count, 'invalid latency bucket')
            previous = samples[key]
        require(previous == count and ('http_request_duration_seconds_sum', labels) in samples, 'histogram count/sum missing')
    return schema, samples


def check(report):
    require(report.get('capture_complete') is True and report.get('source_status_mismatches') == 0 and not report.get('failure_category') and not report.get('cleanup_failed'), 'failed capture')
    require(report.get('source_revision') == capture.SOURCE and report.get('reference_image') == capture.REFERENCE, 'reference identity')
    start, finish = (datetime.fromisoformat(report[key]) for key in ('started_at', 'finished_at'))
    def view(response):
        return compare.response_view(response, start, finish, 'GET')
    def pair(observations, status=200, key=None):
        require(set(observations) == {'reference', 'candidate'}, 'missing runtime')
        left, right = (observations[role] for role in ('reference', 'candidate'))
        require(view(left['response']) == view(right['response']) and left['response']['status'] == status, 'operational API mismatch')
        require(left['effects'] == right['effects'], 'operational provider effects differ')
        if key is not None:
            require(len(left['effects']) == 1 and 'api_key='+key in left['effects'][0]['target'], 'secret not applied')
        elif status == 404:
            require(left['effects'] == [], 'disabled route contacted provider')
    reference_schema, reference_samples = metrics(report['reference_metrics'])
    candidate_schema, candidate_samples = metrics(report['candidate_metrics'])
    require(reference_schema == candidate_schema, 'metric schemas differ')
    exact = lambda values: {key: value for key, value in values.items() if key[0] in {'net_errors', 'http_request_duration_seconds_count'}}
    require(exact(reference_samples) == exact(candidate_samples), 'application metric counters/labels differ')
    require(set(reference_samples) == set(candidate_samples), 'metric sample labels differ')
    operations = report['operations']
    require(operations['mode'] == 'full', 'full operational run required')
    pair(operations['before_reload'], key='synthetic-giphy-secret')
    for name in ('valid_reload', 'invalid_reload_retention'):
        pair(operations[name], key='synthetic-updated-giphy-secret')
    for name in ('valid_reload_elapsed_seconds', 'invalid_reload_elapsed_seconds'):
        require(0 < operations[name] <= 75, 'reload deadline missed')
    for version, status in (('v0', 404), ('v19', 404), ('v1', 200)):
        pair({role: operations['disabled_versions'][role][version] for role in ('reference', 'candidate')}, status)
    require(operations['normal_shutdown'] == operations['inflight_exit_codes'] == {'reference': 0, 'candidate': 0}, 'unclean shutdown')
    require(operations['inflight_active_before_signal'] is True, 'no active request at signal')
    drained = operations['inflight_shutdown']
    require(view(drained['reference']) == view(drained['candidate']) and drained['reference']['status'] == 200, 'in-flight drain mismatch')
    expected_log = {'level': 'Info', 'msgs': ['Listening on 0.0.0.0:8080']}
    require(all([json.loads(line) for line in logs.splitlines()] == [expected_log] for logs in operations['structured_logs'].values()) and set(operations['structured_logs']) == {'reference', 'candidate'}, 'startup log schema mismatch')
    switch = operations['switchback']
    require(switch['image'] == capture.REFERENCE and switch['image_id'] == report['reference_image_id'] and switch['config_sha256'] == operations['variant_config_sha256'], 'switchback image/config mismatch')
    pair({'reference': operations['disabled_versions']['reference']['v1'], 'candidate': switch['observation']})
    p95 = {}
    for role, performance in operations['performance'].items():
        samples = performance['samples']
        require(len(samples) == 40 and all(sample['status'] == 200 and base64.b64decode(sample['body_base64'], validate=True) == b'{"provider":"synthetic"}' for sample in samples), 'load responses missing/failed')
        times = sorted(sample['seconds'] for sample in samples)
        require(all(math.isfinite(value) and value > 0 for value in times), 'invalid timings')
        p95[role] = times[37]
    require(set(p95) == {'reference', 'candidate'} and p95['candidate'] <= max(1, 2*p95['reference']), 'p95 regression')
    memory = operations['performance']['candidate']['memory_usage'].split(' / ')[0]
    match = re.fullmatch(r'([0-9.]+)(B|KiB|MiB|GiB)', memory)
    require(match is not None, 'unrecognized memory units')
    memory_bytes = float(match[1]) * {'B': 1, 'KiB': 1024, 'MiB': 1024**2, 'GiB': 1024**3}[match[2]]
    require(memory_bytes < 50*1024**2, 'chart memory limit exceeded')
    return {'schema_version': 1, 'result': 'pass', 'parity_verified': False,
            'coverage': ['application-metrics', 'secret-reload', 'invalid-reload-retention', 'disabled-versions', 'configured-startup-log', 'SIGTERM-drain', 'image-switchback', 'bounded-load-smoke'],
            'candidate': report['candidate'], 'reference_image': capture.REFERENCE,
            'p95_seconds': p95, 'candidate_memory_bytes': memory_bytes,
            'limitations': ['Native GC and latency/connection measurements are not numerically equivalent.', '40 requests at concurrency four are not a production capacity benchmark.', 'Original Haskell integration executable and full-backend matrix were not run.']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('capture', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        report, digest = compare.foundation.load_file(args.capture)
        result = check(report)
        result['capture_sha256'] = digest
        with args.output.open('x') as output:
            json.dump(result, output, indent=2); output.write('\n')
        print('Operational comparison passed:', ', '.join(result['coverage']))
        return 0
    except (OSError, ValueError, KeyError, TypeError) as error:
        print('Operational comparison failed:', str(error))
        return 1

if __name__ == '__main__':
    raise SystemExit(main())
