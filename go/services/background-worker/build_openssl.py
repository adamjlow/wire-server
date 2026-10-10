#!/usr/bin/env python3
"""Build the optional static bot TLS helper in disposable Docker, never on the host."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import urllib.request
import uuid

VERSION = '3.5.9'
SOURCE_SHA256 = '603f5602e2eef00d77fbd429d34dcd5822bb301757a1bc9cdb24c670f1eb859a'
SOURCE_URL = 'https://github.com/openssl/openssl/releases/download/openssl-3.5.9/openssl-3.5.9.tar.gz'
BUILDER = 'docker.io/library/alpine@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6'
REPO = Path(__file__).resolve().parents[3]
SCRIPT = '''printf '%s\\n' 'https://dl-cdn.alpinelinux.org/alpine/v3.24/main' > /etc/apk/repositories
apk add --no-cache build-base perl musl-dev linux-headers
apk info -v > /output/apk-packages.txt
mkdir -p /work/source
tar -xf /input/openssl.tar.gz --strip-components=1 -C /work/source
cd /work/source
perl Configure linux-x86_64 no-shared no-tests no-dso no-module -static --prefix=/work/install --openssldir=/etc/ssl
make -j2 build_sw
cp apps/openssl /output/openssl
cp LICENSE.txt /output/openssl-LICENSE.txt
chmod 755 /output/openssl
/output/openssl version -a
readelf -l /output/openssl > /output/elf-program-headers.txt
readelf -d /output/openssl > /output/elf-dynamic.txt
'''
LICENSES = {
    'musl-COPYRIGHT': 'https://git.musl-libc.org/cgit/musl/plain/COPYRIGHT?h=v1.2.6',
    'gcc-COPYING.RUNTIME': 'https://raw.githubusercontent.com/gcc-mirror/gcc/releases/gcc-15.2.0/COPYING.RUNTIME',
    'gcc-COPYING3': 'https://raw.githubusercontent.com/gcc-mirror/gcc/releases/gcc-15.2.0/COPYING3',
    'alpine-main-secdb.json': 'https://secdb.alpinelinux.org/v3.24/main.json',
}


def download(url, target, maximum):
    with urllib.request.urlopen(url, timeout=30) as response:
        data = response.read(maximum + 1)
    if len(data) > maximum:
        raise ValueError('download exceeds the declared limit')
    target.write_bytes(data)
    return hashlib.sha256(data).hexdigest()


def build(output, archive=None):
    output = Path(output).absolute()
    if output.exists() or output.is_symlink() or any(p.is_symlink() for p in output.parents) or REPO in output.parents:
        raise ValueError('output must be a fresh directory outside the checkout without symlink parents')
    output.mkdir(mode=0o755)
    source = Path(archive).resolve(strict=True) if archive else output / 'source.tar.gz'
    if archive is None:
        download(SOURCE_URL, source, 80 * 1024**2)
    if not source.is_file() or source.stat().st_size > 80 * 1024**2 or hashlib.sha256(source.read_bytes()).hexdigest() != SOURCE_SHA256:
        raise ValueError('OpenSSL source does not match the pinned publisher checksum')
    downloads = {name: {'url': url, 'sha256': download(url, output / name, 4 * 1024**2)} for name, url in LICENSES.items()}
    name = 'wire-bg-openssl-build-' + uuid.uuid4().hex
    args = ['docker', 'run', '--rm', '--name', name, '--platform=linux/amd64', '--network=bridge',
            '--cap-drop=ALL', '--security-opt=no-new-privileges', '--memory=2g', '--cpus=2', '--pids-limit=256',
            '--tmpfs', '/work:rw,nosuid,nodev,size=768m,mode=1777',
            '--label', 'wire.migration.owner=background-worker-openssl-musl-build',
            '--mount', 'type=bind,source=' + str(source) + ',target=/input/openssl.tar.gz,readonly',
            '--mount', 'type=bind,source=' + str(output) + ',target=/output', BUILDER, '/bin/sh', '-eu', '-c', SCRIPT]
    # The root builder writes only its synthetic public output directory; compiler
    # packages and build objects disappear with its owned container/tmpfs.
    output.chmod(0o777)
    try:
        with (output / 'build.log').open('x') as log:
            result = subprocess.run(args, stdout=log, stderr=subprocess.STDOUT, timeout=900)
    finally:
        subprocess.run(['docker', 'rm', '--force', name], capture_output=True, timeout=30)
        output.chmod(0o755)
    record = {'source_version': VERSION, 'source_sha256': SOURCE_SHA256, 'source_url': SOURCE_URL,
              'image': BUILDER, 'args': args, 'script': SCRIPT, 'exit_code': result.returncode,
              'platform': 'linux/amd64', 'host_dependencies_installed': False, 'downloads': downloads}
    if result.returncode == 0:
        if 'INTERP' in (output / 'elf-program-headers.txt').read_text() or '(NEEDED)' in (output / 'elf-dynamic.txt').read_text():
            raise ValueError('helper has an external interpreter or shared-library dependency')
        record.update(binary_sha256=hashlib.sha256((output / 'openssl').read_bytes()).hexdigest(), static_linkage_verified=True)
    (output / 'build.json').write_text(json.dumps(record, indent=2) + '\n')
    (output / 'source-downloads.json').write_text(json.dumps(downloads, indent=2) + '\n')
    if result.returncode:
        raise ValueError('helper build failed; inspect retained build.log')
    return record


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--archive', type=Path)
    arguments = parser.parse_args()
    build(arguments.output, arguments.archive)
