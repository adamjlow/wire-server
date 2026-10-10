#!/usr/bin/env python3
"""Original compiled Wire tests, service substitution and honest preflight."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

import harness

BASE = 'quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27'
CATALOGUE = Path(__file__).with_name('e2e-tests.json')
SERVICES = ('brig','brig-index','brig-schema','cannon','cargohold','federator','galley','gundeck','proxy','spar','stern','background-worker','wire-server-enterprise')


def names(service):
    if service in ('background-worker-mls','background-worker-users'):
        selected = ('Migration.Conversation.testMigrationToPostgresMLS',) if service=='background-worker-mls' else ('Migration.User.testUserMigrationToPostgres','Migration.User.testReindexingUsersDuringMigration')
        available=set(names('background-worker'))
        if not set(selected).issubset(available):raise ValueError('targeted original cases absent from catalogue')
        return list(selected)
    catalogue = Path(__file__).with_name(service+'-tests.json') if service in ('gundeck','federator','background-worker','spar') else CATALOGUE
    groups = json.loads(catalogue.read_text())['services']
    if service not in groups:
        raise ValueError('unsupported service')
    return [group['module'].removeprefix('Test.')+'.'+name+suffix
            for group in groups[service] for name in group['tests']
            for suffix in (['['+v+']' for v in group.get('variants',{}).get(name,[])] if name in group.get('variants',{}) else [''])]


def binary(name):
    found = shutil.which(name)
    if found is None:
        raise ValueError('published image lacks required executable: '+name)
    path = Path(found).resolve(strict=True)
    return {'path':str(path), 'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}


def discover(service):
    tests = names(service)
    completed = subprocess.run([binary('integration')['path'],'--list',*[arg for name in tests for arg in ('--include',name)]],
                               check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30)
    # List output is inspected for actual fully qualified case names. Discovery
    # is not a passing test report or binary source attestation.
    missing = [name for name in tests if re.search(r'(?<![\w.])'+re.escape(name)+r'(?![\w.])', completed.stdout) is None]
    if missing:
        raise ValueError('compiled upstream test catalogue differs: '+','.join(missing))
    return tests


def preflight():
    return {'schema_version':1,'result':'preflight-pass','base_image':BASE,'uid':os.getuid(),
            'binaries':{name:binary(name) for name in ('integration',*SERVICES)},
            'tests':{service:discover(service) for service in ('proxy','cargohold','cannon','gundeck','federator','background-worker','spar')},
            'e2e_executed':False,'environment_ready':False,
            'requirements':['Provision disposable Cassandra/RabbitMQ/S3/search/Redis/federation dependencies and static Haskell peers.','Mount synthetic integration config and service configs; full tests are not run by preflight.']}


def suite(service, candidate, config, output, implementations=None):
    if not config.is_file() or config.is_symlink():
        raise ValueError('mount a regular integration config')
    if output.exists() or output.is_symlink() or any(parent.is_symlink() for parent in output.parents):
        raise ValueError('report destination must be new without symlink parents')
    if os.environ.get('WIRE_E2E_DISPOSABLE') != '1':
        raise ValueError('suite requires a provisioned disposable dependency environment; preflight is not readiness')
    sidecar = output.with_suffix('.run.json')
    if sidecar.exists() or sidecar.is_symlink():
        raise ValueError('run receipt destination must be new')
    tests = discover(service)
    env = dict(os.environ)
    selected = None
    local_selection = candidate is not None and config.parent.name == 'services'
    if candidate is not None:
        candidate = candidate.resolve(strict=True)
        if not candidate.is_file() or not os.access(candidate,os.X_OK):
            raise ValueError('candidate executable is unavailable')
        # Child service lookup uses PATH when config is outside services/. All
        # other service processes remain the pinned Haskell executables.
        if local_selection:
            if config.parent.resolve()!=Path('/tmp/wire-e2e/services') or (config.parent.parent/'dist'/service).resolve()!=candidate:
                raise ValueError('local candidate must match the owned staged service binary')
        else:
            shim = Path('/tmp/wire-e2e-selected')
            shim.mkdir(mode=0o700)
            (shim/service).symlink_to(candidate)
            env['PATH'] = str(shim)+':'+os.environ.get('PATH','/sbin:/bin')
        selected = {'path':str(candidate),'sha256':hashlib.sha256(candidate.read_bytes()).hexdigest()}
    report = {'schema_version':1,'service':service,'base_image':BASE,'required_tests':tests,'candidate':selected,
              'config_sha256':hashlib.sha256(config.read_bytes()).hexdigest(),'e2e_executed':False,'result':'running'}
    if implementations is not None:
        report['implementations'] = implementations
    try:
        result = subprocess.run([binary('integration')['path'],'--config',str(config),
                    *[arg for name in tests for arg in ('--include',name)],'--xml',str(output)],env=env,timeout=1200)
        report.update(process_exit_code=result.returncode,e2e_executed=True,result='executed-needs-report-validation')
        if result.returncode != 0 or not output.is_file():
            report['result']='failed'
            return report, 1
        # Full report acceptance stays in the shared harness; never treat an
        # executable's zero exit without required assertions as green.
        report['e2e_executed'] = True
        report['validated_report'] = harness.check_xml(output, tests)
        report['result']='pass'
        return report, 0
    except (OSError,ValueError,subprocess.SubprocessError) as error:
        report.update(result='failed',error_category=type(error).__name__)
        raise
    finally:
        if candidate is not None and not local_selection:
            (shim/service).unlink();shim.rmdir()
        with output.with_suffix('.run.json').open('x') as stream:
            json.dump(report,stream,indent=2);stream.write('\n')


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    sub=parser.add_subparsers(dest='command',required=True)
    sub.add_parser('preflight')
    environment=sub.add_parser('environment');environment.add_argument('--service',choices=('proxy','cargohold','cannon','gundeck','proxy-cargohold','proxy-cargohold-cannon','proxy-cargohold-cannon-gundeck','gundeck-reference','federator','proxy-cargohold-cannon-gundeck-federator','background-worker','background-worker-mls','background-worker-users','proxy-cargohold-cannon-gundeck-federator-background-worker','spar','proxy-cargohold-cannon-gundeck-federator-background-worker-spar'),required=True);environment.add_argument('--candidate',type=Path)
    environment.add_argument('--peer-candidate', nargs=2, action='append', default=[], metavar=('SERVICE','BINARY'))
    listing=sub.add_parser('list');listing.add_argument('--service',choices=('proxy','cargohold','cannon','gundeck','federator','background-worker','spar'),required=True)
    running=sub.add_parser('suite');running.add_argument('--service',choices=('proxy','cargohold','cannon','gundeck','federator','background-worker','spar'),required=True)
    running.add_argument('--candidate',type=Path);running.add_argument('--config',type=Path,required=True);running.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    try:
        if args.command=='preflight': report,code=preflight(),0
        elif args.command=='list': report,code={'tests':discover(args.service),'e2e_executed':False},0
        elif args.command=='environment':
            import runtime
            report,code=runtime.run(args.service,args.candidate,args.peer_candidate)
        else: report,code=suite(args.service,args.candidate,args.config,args.output)
        print(json.dumps(report,indent=2));return code
    except (OSError,ValueError,subprocess.SubprocessError) as error:
        print(json.dumps({'result':'blocked-or-failed','error_category':type(error).__name__,'detail':str(error)[:500],'e2e_executed':False}));return 2

if __name__=='__main__': raise SystemExit(main())
