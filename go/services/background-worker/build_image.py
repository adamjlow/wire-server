#!/usr/bin/env python3
"""Package identified worker/helper artifacts locally; never push or deploy."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import ssl
import struct
import subprocess
import tempfile
import uuid

from build_openssl import SOURCE_SHA256, VERSION

ROOT = Path(__file__).resolve().parent
REFERENCE = 'quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27'


def command(args):
    return subprocess.check_output(list(map(str, args)), cwd=ROOT, text=True, stderr=subprocess.PIPE, timeout=180).strip()


def static_amd64(path):
    data = path.read_bytes()
    if len(data) < 64 or data[:6] != b'\x7fELF\x02\x01':
        raise ValueError('artifact is not a little-endian ELF64 executable')
    kind, machine = struct.unpack_from('<HH', data, 16)
    offset = struct.unpack_from('<Q', data, 32)[0]
    size, count = struct.unpack_from('<HH', data, 54)
    if kind not in (2, 3) or machine != 62 or size < 56 or offset + size * count > len(data):
        raise ValueError('invalid linux/amd64 program headers')
    for i in range(count):
        header = struct.unpack_from('<IIQQQQQQ', data, offset + i * size)
        if header[0] == 3:
            raise ValueError('artifact requires an external ELF interpreter')
        if header[0] == 2:
            start, length = header[2], header[5]
            if start + length > len(data) or length % 16:
                raise ValueError('invalid ELF dynamic metadata')
            for position in range(start, start + length, 16):
                tag, _ = struct.unpack_from('<QQ', data, position)
                if tag == 1:
                    raise ValueError('artifact requires a shared library')
                if tag == 0:
                    break


def build(output, candidate, candidate_receipt, helper, helper_receipt):
    output = Path(output).absolute()
    if output.exists() or output.is_symlink() or any(p.is_symlink() for p in output.parents) or ROOT.parents[2] in output.parents:
        raise ValueError('receipt must be new and outside the checkout without symlink parents')
    if command(['git', 'status', '--porcelain']):
        raise ValueError('commit changes before packaging identified artifacts')
    packaging = command(['git', 'rev-parse', 'HEAD'])
    identified = json.loads(Path(candidate_receipt).read_text())
    openssl = json.loads(Path(helper_receipt).read_text())
    if not re.fullmatch('[a-f0-9]{40}', identified['source_revision']):
        raise ValueError('candidate revision must be a full commit SHA')
    if hashlib.sha256(Path(candidate).read_bytes()).hexdigest() != identified['binary_sha256']:
        raise ValueError('candidate hash does not match receipt')
    if hashlib.sha256(Path(helper).read_bytes()).hexdigest() != openssl['binary_sha256'] or openssl['source_version'] != VERSION or openssl['source_sha256'] != SOURCE_SHA256 or openssl['exit_code'] != 0:
        raise ValueError('helper does not match the pinned successful source build')
    static_amd64(Path(candidate)); static_amd64(Path(helper))
    info = json.loads(command(['go', 'version', '-m', '-json', candidate]))
    settings = {item['Key']: item['Value'] for item in info['Settings']}
    if info != identified['build_info'] or info['GoVersion'] != 'go1.26.9' or settings.get('vcs.modified') != 'false' or settings.get('vcs.revision') != identified['source_revision'] or settings.get('CGO_ENABLED') != '0' or settings.get('GOOS') != 'linux' or settings.get('GOARCH') != 'amd64' or info['Path'] != 'github.com/adamjlow/wire-server/go/services/background-worker/cmd/background-worker':
        raise ValueError('candidate compiler identity does not match the clean pinned worker')
    container = None
    try:
        with tempfile.TemporaryDirectory(prefix='wire-bg-image-') as temporary:
            files = Path(temporary)
            shutil.copyfile(candidate, files / 'background-worker'); shutil.copyfile(helper, files / 'openssl')
            licenses = files / 'licenses'; licenses.mkdir()
            for name in ('openssl-LICENSE.txt', 'musl-COPYRIGHT', 'gcc-COPYING.RUNTIME', 'gcc-COPYING3'):
                shutil.copyfile(Path(helper).parent / name, licenses / name)
            shutil.copyfile(ROOT / 'LICENSE', licenses / 'background-worker-LICENSE')
            container = command(['docker', 'create', '--name', 'wire-bg-ca-' + uuid.uuid4().hex,
                                 '--network=none', '--read-only', '--cap-drop=ALL', REFERENCE])
            if not re.fullmatch('[a-f0-9]{64}', container):
                container = None
                raise ValueError('missing owned CA extraction container identity')
            command(['docker', 'cp', '--follow-link', container + ':/etc/ssl/certs/ca-bundle.crt', files / 'ca-bundle.crt'])
            trust = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT); trust.load_verify_locations(cafile=str(files / 'ca-bundle.crt'))
            if not trust.cert_store_stats()['x509_ca']:
                raise ValueError('qualified reference CA bundle is empty')
            shutil.copyfile(ROOT / 'Dockerfile', files / 'Dockerfile')
            image = command(['docker', 'build', '--network=none', '--quiet', '--label',
                             'org.opencontainers.image.revision=' + identified['source_revision'], '--label',
                             'wire.migration.owner=background-worker-optional-image', files])
            if not re.fullmatch('sha256:[a-f0-9]{64}', image):
                raise ValueError('missing immutable image identity')
            record = {'source_revision': identified['source_revision'], 'packaging_revision': packaging,
                      'candidate_sha256': identified['binary_sha256'], 'build_info': info, 'image_id': image,
                      'openssl_build': openssl, 'ca': {'source_image': REFERENCE, 'sha256': hashlib.sha256((files / 'ca-bundle.crt').read_bytes()).hexdigest(), 'certificates': trust.cert_store_stats()['x509_ca']},
                      'files_sha256': {str(p.relative_to(files)): hashlib.sha256(p.read_bytes()).hexdigest() for p in files.rglob('*') if p.is_file()}, 'local_only': True}
        output.write_text(json.dumps(record, indent=2) + '\n')
        return record
    finally:
        if container:
            command(['docker', 'rm', container])


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('output', 'candidate', 'candidate-receipt', 'helper', 'helper-receipt'):
        parser.add_argument('--' + name, type=Path, required=True)
    arguments = parser.parse_args()
    build(arguments.output, arguments.candidate, arguments.candidate_receipt, arguments.helper, arguments.helper_receipt)
