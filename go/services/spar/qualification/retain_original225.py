"""Retain six complete original suite passes; never merge/filter failed XML."""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
sys.path[:0] = [str(ROOT / 'tools/parity'), str(ROOT / 'tools/parity/docker')]
import harness
import runner

# Docker places the unchanged shared catalogue alongside runner.py; on the host
# read its actual repository location without modifying names/assertions.
runner.CATALOGUE = ROOT / 'tools/parity/e2e-tests.json'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    batches = {
        'seven-go-original225-current': ('proxy', 'cargohold'),
        'seven-go-original193-remaining': ('spar', 'gundeck', 'federator'),
        'seven-go-worker47-supervised': ('background-worker',),
    }
    result = {'result': 'pass', 'kind': 'complete-original225-batched',
              'e2e_executed': True, 'reference_revision': 'f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e',
              'candidate_revision': '40c1b2f896fe20d5d9aad4a7c90a815fcfa0e067',
              'suites': {}, 'batches': {}, 'total_tests': 0,
              'limitations': 'Six complete passing suites across three owned namespaces; earlier overall failed attempts remain failed. No filtered partial suite or synthetic merged XML. No native rebuild/unit pass or production capacity claim.'}
    expected = None
    config = None
    implementations = None
    for batch, services in batches.items():
        source = args.cache / batch
        env = json.loads((source / 'environment.json').read_text())
        assert env['cleanup_exit_code'] == 0
        signature = {name: (item['sha256'], item['source_revision']) for name, item in env['peer_candidates'].items()}
        assert set(signature) == {'proxy', 'cargohold', 'cannon', 'gundeck', 'federator', 'background-worker', 'spar'}
        assert signature['spar'] == ('9fabe438a2b52f723d698168350d90cb1157dc43f130803e841a7b224a884d42', result['candidate_revision'])
        if expected is None:
            expected = signature
            result['selected_peers'] = env['peer_candidates']
            result['dependencies'] = env['dependencies']
            result['runner_image_id'] = env['runner_image_id']
            result['runner_revision'] = env['runner_revision']
            result['fixture_hashes'] = env['files_sha256']
        assert signature == expected
        for field in ('dependencies', 'runner_image_id', 'runner_revision'):
            assert env[field] == result[field]
        assert env['files_sha256'] == result['fixture_hashes']
        remaining = {}
        for name, command in [('containers', ['docker', 'ps', '-aq']), ('networks', ['docker', 'network', 'ls', '-q']), ('volumes', ['docker', 'volume', 'ls', '-q'])]:
            remaining[name] = subprocess.check_output(command + ['--filter', 'label=com.docker.compose.project=' + env['project']], text=True).split()
        assert not any(remaining.values())
        destination = args.output / batch
        destination.mkdir()
        shutil.copyfile(source / 'environment.json', destination / 'environment.json')
        result['batches'][batch] = {'project': env['project'], 'overall_result': env['result'],
            'overall_process_exit_code': env['process_exit_code'], 'cleanup_exit_code': 0,
            'post_cleanup_independent': remaining, 'qualifying_complete_suites': list(services),
            'source_revision': env['source_revision']}
        for service in services:
            xml = source / 'reports' / (service + '.xml')
            receipt = source / 'reports' / (service + '.run.json')
            observed = json.loads(receipt.read_text())
            required = runner.names(service)
            validated = harness.check_xml(xml, required)
            assert observed['required_tests'] == required
            assert observed['result'] == 'pass' and observed['e2e_executed'] is True and observed['process_exit_code'] == 0
            assert observed['validated_report'] == validated
            for name, (sha, _) in signature.items():
                assert observed['implementations'][name]['implementation'] == 'go'
                assert observed['implementations'][name]['sha256'] == sha
            if config is None:
                config = observed['config_sha256']
                implementations = observed['implementations']
                result['implementations'] = implementations
            assert observed['config_sha256'] == config
            assert observed['implementations'] == implementations
            shutil.copyfile(xml, destination / xml.name)
            shutil.copyfile(receipt, destination / receipt.name)
            result['suites'][service] = {'batch': batch, 'result': 'pass', 'validated_report': validated,
                'receipt_sha256': hashlib.sha256(receipt.read_bytes()).hexdigest()}
            result['total_tests'] += validated['tests']
        if batch == 'seven-go-worker47-supervised':
            supervision = source / 'reports/orphan-nginx-supervision.json'
            report = json.loads(supervision.read_text())
            assert report['result'] == 'pass' and report['original_exit_code'] == 0
            shutil.copyfile(supervision, destination / supervision.name)
            with (source / 'baseline-background-worker.log').open('rb') as inp, gzip.open(destination / 'baseline-background-worker.log.gz', 'wb') as out:
                shutil.copyfileobj(inp, out)
            shutil.copyfile(args.cache / 'seven-go-worker47-supervised-controller.log', destination / 'controller.log')
            result['orphan_supervision'] = report
            result['orphan_supervisor_sha256'] = hashlib.sha256((ROOT / 'tools/parity/docker/orphan_nginx.py').read_bytes()).hexdigest()
    assert result['total_tests'] == 225 and len(result['suites']) == 6
    (args.output / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({'result': 'pass', 'total_tests': 225, 'suites': 6, 'owned_cleanup': 'empty'}))


if __name__ == '__main__':
    main()
