import hashlib,json,os,subprocess,time
from pathlib import Path
cache=Path('/home/operator/.cache/wire-migration/background-worker-20261009')
out=cache/'openssl-musl-3.5.9'
archive=cache/'openssl-3.5.9/openssl-3.5.9.tar.gz'
assert hashlib.sha256(archive.read_bytes()).hexdigest()=='603f5602e2eef00d77fbd429d34dcd5822bb301757a1bc9cdb24c670f1eb859a'
image='docker.io/library/alpine@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6'
os.chmod(out,0o777)
script='''printf '%s\\n' 'https://dl-cdn.alpinelinux.org/alpine/v3.24/main' > /etc/apk/repositories
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
args=['docker','run','--rm','--network=bridge','--cap-drop=ALL','--security-opt=no-new-privileges','--memory=2g','--cpus=2','--pids-limit=256','--tmpfs','/work:rw,nosuid,nodev,size=768m,mode=1777','--label','wire.migration.owner=background-worker-openssl-musl-build','--mount','type=bind,source='+str(archive)+',target=/input/openssl.tar.gz,readonly','--mount','type=bind,source='+str(out)+',target=/output',image,'/bin/sh','-eu','-c',script]
started=time.time()
with (out/'build.log').open('x') as log:r=subprocess.run(args,stdout=log,stderr=subprocess.STDOUT,timeout=900)
record={'image':image,'args':args,'script':script,'exit_code':r.returncode,'seconds':round(time.time()-started,3),'source_sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'source_version':'3.5.9','platform':'linux/amd64','build_network':'outbound signed official Alpine packages only; no source downloads or service fixtures','host_dependencies_installed':False}
if r.returncode==0:
 record['binary_sha256']=hashlib.sha256((out/'openssl').read_bytes()).hexdigest()
 assert 'INTERP' not in (out/'elf-program-headers.txt').read_text()
 assert '(NEEDED)' not in (out/'elf-dynamic.txt').read_text()
 record['static_linkage_verified']=True
(out/'build.json').write_text(json.dumps(record,indent=2)+'\n')
print(json.dumps({k:v for k,v in record.items() if k not in ('args','script')},indent=2))
raise SystemExit(r.returncode)
