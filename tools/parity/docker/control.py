#!/usr/bin/env python3
"""Build and inspect the pinned e2e runner without host Haskell packages."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import uuid

ROOT=Path(__file__).resolve().parents[3]
FILES=Path(__file__).resolve().parent


def command(args,timeout=180):
    result=subprocess.run([str(arg) for arg in args],check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
    return result.stdout.strip()


def destination(path):
    path=Path(path).absolute()
    if path.exists() or path.is_symlink() or any(parent.is_symlink() for parent in path.parents):
        raise ValueError('receipt destination must be new without symlink parents')
    if ROOT in path.parents:
        raise ValueError('receipts must be outside the checkout')
    return path


def build(output):
    output=destination(output)
    revision=command(['git','rev-parse','HEAD'])
    if command(['git','status','--porcelain']):
        raise ValueError('commit changes before building identified runner')
    with tempfile.TemporaryDirectory(prefix='wire-e2e-build-') as tmp:
        for name in ('Dockerfile','entrypoint.sh','runner.py','runtime.py','gundeck-tests.json','federator-tests.json','background-worker-tests.json','spar-tests.json'):
            shutil.copyfile(FILES/name,Path(tmp)/name)
        for name in ('harness.py','e2e-tests.json'):
            shutil.copyfile(ROOT/'tools/parity'/name,Path(tmp)/name)
        checksums={path.name:hashlib.sha256(path.read_bytes()).hexdigest() for path in Path(tmp).iterdir()}
        image=command(['docker','build','--network=none','--quiet','--label','org.opencontainers.image.revision='+revision,tmp],timeout=300)
    if not re.fullmatch('sha256:[0-9a-f]{64}',image):
        raise ValueError('missing immutable image identity')
    config=json.loads(command(['docker','image','inspect',image]))[0]
    if config['Config']['User']!='65534:65534':
        raise ValueError('runner must be non-root')
    receipt={'schema_version':1,'result':'built','image_id':image,'source_revision':revision,
             'base_image':'quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27',
             'files_sha256':checksums,'e2e_executed':False}
    with output.open('x') as stream:json.dump(receipt,stream,indent=2);stream.write('\n')
    return receipt


def preflight(receipt_path,output):
    output=destination(output)
    path=Path(receipt_path)
    if path.is_symlink() or path.stat().st_size>1024**2:raise ValueError('invalid build receipt')
    receipt=json.loads(path.read_text());image=receipt['image_id']
    if not re.fullmatch('sha256:[0-9a-f]{64}',image):raise ValueError('invalid immutable image identity')
    actual=json.loads(command(['docker','image','inspect',image]))[0]
    if actual['Id']!=image or actual['Config']['User']!='65534:65534' or actual['Config'].get('Labels',{}).get('org.opencontainers.image.revision')!=receipt['source_revision']:
        raise ValueError('image receipt mismatch')
    name='wire-e2e-preflight-'+uuid.uuid4().hex
    container=None
    try:
        container=command(['docker','create','--name',name,'--network=none','--read-only','--cap-drop=ALL',
                     '--security-opt=no-new-privileges','--pids-limit=256','--memory=1g','--cpus=2',
                     '--tmpfs','/tmp:rw,nosuid,nodev,size=128m,mode=1777',image,'preflight'],timeout=30)
        if not re.fullmatch('[0-9a-f]{64}',container):
            container=None
            raise ValueError('invalid created container identity')
        raw=command(['docker','start','--attach',container],timeout=90)
        state=json.loads(command(['docker','inspect',container]))[0]['State']
        if state['ExitCode'] != 0:
            raise ValueError('runner exited unsuccessfully')
        report=json.loads(raw)
        if report.get('result')!='preflight-pass' or report.get('uid')!=65534 or report.get('e2e_executed') is not False or report.get('base_image')!=receipt['base_image']:
            raise ValueError('runner preflight failed')
        report['runner_image_id']=image;report['runner_revision']=receipt['source_revision']
        with output.open('x') as stream:json.dump(report,stream,indent=2);stream.write('\n')
        return report
    finally:
        if container is not None:
            subprocess.run(['docker','rm','--force',container],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=15)


def main():
    parser=argparse.ArgumentParser(description=__doc__);sub=parser.add_subparsers(dest='command',required=True)
    builder=sub.add_parser('build');builder.add_argument('--output',type=Path,required=True)
    probe=sub.add_parser('preflight');probe.add_argument('--receipt',type=Path,required=True);probe.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    try:
        result=build(args.output) if args.command=='build' else preflight(args.receipt,args.output)
        print(json.dumps(result,indent=2));return 0
    except (OSError,ValueError,KeyError,subprocess.SubprocessError) as error:
        print(json.dumps({'result':'blocked-or-failed','error_category':type(error).__name__,'detail':str(error)[:300],'e2e_executed':False}));return 2

if __name__=='__main__':raise SystemExit(main())
