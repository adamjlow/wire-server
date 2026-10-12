#!/usr/bin/env python3
"""Export local, non-Git restart inputs; no backend is started or changed."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[4]
SDK = 'sha256:2f9e4393295d7270538dd1c666ab1345eaec06b2c2f8c217283ba6565caee399'
RUNNER = 'sha256:f487c90fd364aac9daee5a1ce6e60fdba0ab74ab0c671ab0de0198e8f05659a9'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output', type=Path)
    parser.add_argument('--cache', type=Path, default=Path.home()/'.cache/wire-migration')
    parser.add_argument('--modules', type=Path, default=Path.home()/'go/pkg/mod')
    args = parser.parse_args()
    output = args.output.resolve()
    if output.exists() or output.is_relative_to(ROOT):
        parser.error('output must be a new directory outside the repository')
    cache = args.cache.resolve()
    selected = [
        'toolchains/go-security-20261009',
        'brig-20261010/native-original310-fixture-corrected',
        'brig-20261010/provider-service-update-sdk-check',
        'brig-20261010/provider-service-create-pem-sdk-check',
        'brig-20261010/provider-service-create-pem-backend-controller.py',
        'brig-20261010/provider-service-update-sdk-controller.py',
        'go-patched-20261009/gundeck',
        'go-patched-20261009/gundeck-image.json',
    ]
    for name in selected:
        if not (cache/name).exists():
            parser.error('missing restart input: '+name)
    if not args.modules.is_dir():
        parser.error('missing module cache')
    images = [json.loads(subprocess.check_output(
        ['docker','image','inspect',image],text=True))[0] for image in [SDK,RUNNER]]
    output.mkdir(mode=0o700)
    fixture = json.loads((cache/selected[1]/'environment.json').read_text())
    # Registry dependencies can be pulled by their committed/recorded digest.
    dependencies = sorted(set(fixture['dependencies'].values()) | {
        'apache/activemq@sha256:1cacfbd9c08ad437c5e1177a9f1217e7c7ada62a64065128acd1a81866a03711',
        'technitium/dns-server@sha256:1045db38fd2f2e4d9578f9168b7eb4862575469ed0ffc79e03f36bdc772a93b5',
    })
    manifest = {'source_revision':subprocess.check_output(
        ['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
        'selected_cache_paths':selected,'private_images':[i['Id'] for i in images],
        'pull_dependencies':dependencies,'result':'incomplete'}
    (output/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    print('Archiving toolchain, modules, synthetic fixtures and identified binaries',flush=True)
    with tarfile.open(output/'restart-inputs.tar.gz','w:gz',compresslevel=1) as archive:
        for name in selected:
            archive.add(cache/name,arcname='cache/'+name)
        archive.add(args.modules,arcname='modules')
    print('Saving pinned SDK and private original Haskell runner images',flush=True)
    subprocess.run(['docker','image','save','-o',str(output/'private-images.tar'),SDK,RUNNER],check=True)
    checksums = {}
    for name in ['restart-inputs.tar.gz','private-images.tar']:
        with (output/name).open('rb') as stream:
            checksums[name] = hashlib.file_digest(stream,'sha256').hexdigest()
    manifest.update(result='complete',sha256=checksums)
    (output/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    (output/'SHA256SUMS').write_text(''.join(f'{digest}  {name}\n' for name,digest in checksums.items()))
    print(json.dumps({'result':'complete','output':str(output),'sha256':checksums}),flush=True)


if __name__ == '__main__':
    main()
