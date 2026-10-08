#!/usr/bin/env python3
"""Bounded migration checks and unchanged upstream e2e binary selection."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
ORACLE = ROOT / 'docs/go-migration/services/proxy/oracle'
CATALOGUE = Path(__file__).with_name('e2e-tests.json')


def proxy_runtime_changed(paths):
    for path in paths:
        if path == 'tools/parity/harness.py':
            return True
        if path.startswith('go/services/proxy/'):
            name = Path(path).name
            if (name.endswith('.go') and not name.endswith('_test.go')) or name in {'go.mod', 'go.sum', 'Dockerfile', 'build_image.py'}:
                return True
        if path.startswith('docs/go-migration/services/proxy/oracle/'):
            name = Path(path).name
            if name in {'capture.py', 'compare.py', 'check_operations.py'} or (name.endswith('.go') and not name.endswith('_test.go')):
                return True
    return False


def run(args, *, cwd=ROOT, env=None, timeout=180):
    subprocess.run([str(arg) for arg in args], cwd=cwd, env=env, check=True, timeout=timeout)


def new_directory(path):
    path = Path(path).absolute()
    if path.exists() or path.is_symlink() or any(parent.is_symlink() for parent in path.parents):
        raise ValueError('artifact path must be new and have no symlink parents')
    if path == ROOT or ROOT in path.parents:
        raise ValueError('artifacts must be outside the checkout')
    path.mkdir(parents=False)
    return path


def executable(path):
    path = Path(path).resolve(strict=True)
    if not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError('required executable is unavailable')
    return path


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def required_tests(service, root=ROOT):
    catalogue = json.loads(CATALOGUE.read_text())
    if service not in catalogue['services']:
        raise ValueError('unsupported service')
    result = []
    for group in catalogue['services'][service]:
        source = root / group['source']
        text = source.read_text()
        for name in group['tests']:
            if not re.search(r'^'+re.escape(name)+r'\s*::', text, flags=re.MULTILINE):
                raise ValueError('upstream test catalogue needs refresh')
            prefix=group['module'].removeprefix('Test.')+'.'+name
            variants=group.get('variants',{}).get(name)
            if variants is None:
                result.append(prefix)
            else:
                if not isinstance(variants,list) or not variants or len(set(variants))!=len(variants) or any(not isinstance(v,str) or not re.fullmatch(r'[A-Za-z0-9_=,.-]+',v) for v in variants):
                    raise ValueError('invalid original test variants')
                result.extend(prefix+'['+variant+']' for variant in variants)
    if not result or len(set(result)) != len(result):
        raise ValueError('empty/duplicate test catalogue')
    return result


def check_xml(path, required):
    path = Path(path)
    if not path.is_file() or path.is_symlink() or path.stat().st_size > 8*1024**2:
        raise ValueError('missing or oversized e2e report')
    raw = path.read_bytes()
    if b'<!DOCTYPE' in raw.upper() or b'<!ENTITY' in raw.upper():
        raise ValueError('XML declarations are not permitted')
    root = ET.fromstring(raw)
    cases = root.findall('.//testcase')
    names = [case.get('name') for case in cases]
    if not cases or len(set(names)) != len(names) or set(names) != set(required):
        raise ValueError('required e2e test coverage differs')
    if any(case.find(kind) is not None for case in cases for kind in ('failure', 'error', 'skipped')):
        raise ValueError('e2e report contains failure/error/skip')
    for suite in root.iter('testsuite'):
        if any(int(suite.get(kind, '0')) != 0 for kind in ('failures', 'errors', 'skipped')):
            raise ValueError('e2e suite failed')
        if int(suite.get('tests', '-1')) != len(suite.findall('testcase')):
            raise ValueError('e2e suite count inconsistent')
    return {'tests': len(cases), 'sha256': sha(path)}


def prepare_e2e(service, haskell_dist, candidate, output, root=ROOT):
    names = required_tests(service, root)
    original = Path(haskell_dist).resolve(strict=True)
    binaries = {path.name: executable(path) for path in original.iterdir() if path.is_file() and os.access(path, os.X_OK)}
    for name in ('integration', 'run-services', service):
        if name not in binaries:
            raise ValueError('Haskell bundle lacks required executable: '+name)
    candidate = executable(candidate)
    # No source/test/default binary is modified. Real service directories keep
    # ../../dist resolution inside the overlay instead of following a cwd link.
    output = new_directory(output)
    for role in ('reference', 'candidate'):
        overlay = output / role
        overlay.mkdir()
        for item in root.iterdir():
            if item.name not in {'services', 'dist'}:
                (overlay/item.name).symlink_to(item, target_is_directory=item.is_dir())
        (overlay/'dist').mkdir()
        for name, binary in binaries.items():
            target = candidate if role == 'candidate' and name == service else binary
            (overlay/'dist'/name).symlink_to(target)
        (overlay/'services').mkdir()
        for item in (root/'services').iterdir():
            target = overlay/'services'/item.name
            if item.is_dir():
                target.mkdir()
                for child in item.iterdir():
                    (target/child.name).symlink_to(child, target_is_directory=child.is_dir())
            elif item.name == 'integration.yaml':
                shutil.copyfile(item, target)
            else:
                target.symlink_to(item)
    report = {'schema_version': 1, 'result': 'prepared', 'e2e_executed': False,
              'service': service, 'required_tests': names,
              'binaries': {name: {'path': str(binary), 'sha256': sha(binary)} for name,binary in binaries.items()},
              'candidate': {'path': str(candidate), 'sha256': sha(candidate)},
              'commands': {role: [str(output/role/'dist/run-services'), '--', str(output/role/'dist/integration'),
                          '--config', str(output/role/'services/integration.yaml'),
                          *[arg for name in names for arg in ('--include', name)],
                          '--xml', str(output/(role+'.xml'))] for role in ('reference','candidate')},
              'working_directories': {role: str(output/role) for role in ('reference','candidate')},
              'environment_requirement': 'Execute only in the provisioned disposable upstream dependency environment. Preparation is not a test pass.'}
    (output/'selection.json').write_text(json.dumps(report, indent=2)+'\n')
    return report


def proxy(output, profile):
    if subprocess.check_output(['git','status','--porcelain'],cwd=ROOT,text=True).strip():
        raise ValueError('commit checkout changes before creating binary evidence')
    go = executable(shutil.which('go') or '/nonexistent-go')
    if subprocess.check_output([go,'version'],text=True).split()[2] != 'go1.26.8':
        raise ValueError('pinned Go 1.26.8 is required')
    run(['docker','info','--format','{{.ServerVersion}}'], timeout=20)
    output = new_directory(output)
    env = dict(os.environ, CGO_ENABLED='0', GOWORK='off', GOTOOLCHAIN='local')
    run([sys.executable, ROOT/'go/services/proxy/build_image.py','--receipt',output/'build.json'],env=env)
    run([go,'build','-mod=readonly','-o',output/'provider','.'],cwd=ORACLE/'provider',env=env)
    args = [sys.executable,ORACLE/'capture.py','--provider-binary',output/'provider',
            '--candidate-receipt',output/'build.json','--suite','extended','--output',output/'capture']
    if profile == 'full': args += ['--operations','full']
    run(args, timeout=360)
    run([sys.executable,ORACLE/'compare.py',output/'capture/reference.json',output/'comparison.json'])
    if profile == 'full':
        run([sys.executable,ORACLE/'check_operations.py',output/'capture/reference.json','--output',output/'operations.json'])
    report = {'schema_version':1,'result':'pass','service':'proxy','profile':profile,
              'coverage':'103 live API cases'+(' and declared operational checks' if profile=='full' else '; operations were not rerun'),
              'parity_verified':False,'capture_sha256':sha(output/'capture/reference.json')}
    (output/'result.json').write_text(json.dumps(report,indent=2)+'\n')
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest='command', required=True)
    live = subs.add_parser('proxy')
    live.add_argument('--profile', choices=('api','full'), default='api')
    live.add_argument('--output', type=Path, required=True)
    prepare = subs.add_parser('prepare-e2e')
    prepare.add_argument('--service', choices=('proxy','cargohold'), required=True)
    prepare.add_argument('--haskell-dist', type=Path, required=True)
    prepare.add_argument('--candidate', type=Path, required=True)
    prepare.add_argument('--output', type=Path, required=True)
    check = subs.add_parser('check-e2e')
    check.add_argument('--service', choices=('proxy','cargohold'), required=True)
    check.add_argument('--reference', type=Path, required=True)
    check.add_argument('--candidate', type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == 'proxy': result = proxy(args.output,args.profile)
        elif args.command == 'prepare-e2e': result = prepare_e2e(args.service,args.haskell_dist,args.candidate,args.output)
        else:
            names = required_tests(args.service)
            result = {'result':'pass','coverage':'upstream e2e reports only; not authenticated execution provenance',
                      'reference':check_xml(args.reference,names),'candidate':check_xml(args.candidate,names)}
        print(json.dumps(result,indent=2))
        return 0
    except (OSError, ValueError, subprocess.SubprocessError, ET.ParseError) as error:
        print(json.dumps({'result':'blocked-or-failed','error_category':type(error).__name__,'detail':str(error)[:300],'parity_verified':False}))
        return 2

if __name__ == '__main__':
    raise SystemExit(main())
