#!/usr/bin/env python3
"""Package an identified static Spar executable locally; never push or deploy."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import ssl
import struct
import subprocess
import tempfile
import uuid

ROOT=Path(__file__).resolve().parent
REFERENCE='quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27'


def command(args):return subprocess.check_output(list(map(str,args)),cwd=ROOT,text=True,stderr=subprocess.PIPE,timeout=180).strip()
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('receipt','candidate','candidate-receipt'):parser.add_argument('--'+name,type=Path,required=True)
    args=parser.parse_args();output=args.receipt.absolute()
    if output.exists() or output.is_symlink() or any(p.is_symlink() for p in output.parents) or not output.parent.is_dir():raise ValueError('new receipt with regular existing parent required')
    if command(['git','status','--porcelain']):raise ValueError('clean committed checkout required')
    identified=json.loads(args.candidate_receipt.read_text());revision=identified['source_revision']
    if not re.fullmatch('[a-f0-9]{40}',revision) or sha(args.candidate)!=identified['binary_sha256']:raise ValueError('candidate receipt mismatch')
    info=json.loads(command(['go','version','-m','-json',args.candidate]));settings={item['Key']:item['Value'] for item in info['Settings']}
    if info['GoVersion']!='go1.26.9' or info['Path']!='github.com/adamjlow/wire-server/go/services/spar/cmd/spar' or any(settings.get(k)!=v for k,v in {'vcs.revision':revision,'vcs.modified':'false','CGO_ENABLED':'0','GOOS':'linux','GOARCH':'amd64'}.items()):raise ValueError('clean pinned static compiler identity required')
    binary=args.candidate.read_bytes()
    if binary[:6]!=b'\x7fELF\x02\x01' or struct.unpack_from('<H',binary,18)[0]!=62:raise ValueError('linux amd64 ELF required')
    offset=struct.unpack_from('<Q',binary,32)[0];size,count=struct.unpack_from('<HH',binary,54)
    for i in range(count):
        if struct.unpack_from('<I',binary,offset+i*size)[0] in (2,3):raise ValueError('external loader/dynamic dependencies not allowed')
    quality=identified['quality_log'];log=Path(quality['path'])
    if sha(log)!=quality['sha256'] or 'Go module checks passed: spar; Go 1.26.9' not in log.read_text():raise ValueError('successful pinned quality receipt required')
    module_cache=Path(command(['go','env','GOMODCACHE']))
    container=None
    try:
        with tempfile.TemporaryDirectory(prefix='wire-spar-image-',dir='/dev/shm') as temporary:
            files=Path(temporary);shutil.copyfile(args.candidate,files/'spar');shutil.copyfile(ROOT/'Dockerfile',files/'Dockerfile')
            licenses=files/'licenses';licenses.mkdir();shutil.copyfile(ROOT/'LICENSE',licenses/'spar-LICENSE')
            for dependency in info['Deps']:
                if 'Replace' in dependency:raise ValueError('unreviewed dependency replacement')
                module=module_cache/(dependency['Path']+'@'+dependency['Version']);found=[]
                for name in ('LICENSE','LICENSE.txt','LICENSE.md','COPYING','NOTICE'):
                    path=module/name
                    if path.is_file():
                        target=licenses/dependency['Path']/dependency['Version']/name;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(path,target);found.append(name)
                if not found:raise ValueError('missing reviewed dependency license '+dependency['Path'])
            source=files/'source';source.mkdir()
            tracked=command(['git','ls-tree','-r','--name-only',revision,'--','go/services/spar']).splitlines()
            for name in tracked:
                relative=Path(name).relative_to('go/services/spar')
                if (relative.suffix=='.go' and not relative.name.endswith('_test.go')) or relative.parts[0]=='scim-schemas' or str(relative) in ('go.mod','go.sum','LICENSE'):
                    target=source/relative;target.parent.mkdir(parents=True,exist_ok=True)
                    target.write_bytes(subprocess.check_output(['git','show',revision+':'+name],cwd=ROOT,timeout=30))
            (source/'SOURCE-REVISION').write_text(revision+'\nhttps://github.com/adamjlow/wire-server\n')
            container=command(['docker','create','--name','wire-spar-ca-'+uuid.uuid4().hex,'--network=none','--read-only','--cap-drop=ALL',REFERENCE])
            if not re.fullmatch('[a-f0-9]{64}',container):raise ValueError('owned reference extraction identity missing')
            command(['docker','cp','--follow-link',container+':/etc/ssl/certs/ca-bundle.crt',files/'ca-bundle.crt'])
            trust=ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT);trust.load_verify_locations(cafile=str(files/'ca-bundle.crt'))
            if not trust.cert_store_stats()['x509_ca']:raise ValueError('empty trusted CA bundle')
            image=command(['docker','build','--network=none','--quiet','--label','org.opencontainers.image.revision='+revision,'--label','wire.migration.owner=spar-optional-image',files])
            if not re.fullmatch('sha256:[a-f0-9]{64}',image):raise ValueError('immutable image identity required')
            receipt={'source_revision':revision,'packaging_revision':command(['git','rev-parse','HEAD']),'binary_sha256':identified['binary_sha256'],'image_id':image,'build_info':info,'ca':{'source_image':REFERENCE,'sha256':sha(files/'ca-bundle.crt'),'certificates':trust.cert_store_stats()['x509_ca']},'files_sha256':{str(p.relative_to(files)):sha(p) for p in files.rglob('*') if p.is_file()},'local_only':True}
            output.write_text(json.dumps(receipt,indent=2)+'\n')
    finally:
        if container:command(['docker','rm',container])
    print(json.dumps({'image_id':image,'receipt':str(output)}))


if __name__=='__main__':main()
