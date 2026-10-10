import pathlib,subprocess,os,json,time,tarfile
base=pathlib.Path('/home/operator/.cache/wire-migration/background-worker-20261009/openssl-3.5.9')
with tarfile.open(base/'openssl-3.5.9.tar.gz') as archive:
 for row in archive.getmembers():
  parts=pathlib.PurePosixPath(row.name).parts
  assert parts and parts[0]=='openssl-3.5.9' and '..' not in parts
  if row.issym() or row.islnk():assert not row.linkname.startswith('/') and '..' not in pathlib.PurePosixPath(row.linkname).parts
work=base/'build';work.mkdir(exist_ok=False);(work/'source').mkdir()
paths=['788mx070y81zjlg5ipcl0cra3afviw9k-gcc-wrapper-15.2.0','mbyy19mdwnfvfwmdi0gqgggx0njvpl1w-binutils-wrapper-2.46','vlq7nnw39j7rwk0pp68w1fcwzpxahm9h-gnumake-4.4.1','6plwsm6pkq79yjv4xvy8csk2pd4hzr67-perl-5.42.0','9ypz3flqsrl5xl495mm8h645gadjsxi1-coreutils-9.11','gik3rh1vz2jlgnifb9dh6vc6sxwwz9jj-bash-5.3p9','k5akwnrn9x2afaj2va7g4a2zpdim8l43-gnutar-1.35','ndpbjk6jhw0da5h272dqqnyxa35a9gmx-gzip-1.14','kgxafhycw2kybbqih759ykc2043qyi5j-gnused-4.9','gn94gpcp5q08x4v6g8mvw8v4r65rcjzk-gnugrep-3.12','c1cjgg6p8m8fssivzrc2p13mwwml3p3v-findutils-4.10.0']
args=['docker','run','--rm','--network','none','--read-only','--user',f'{os.getuid()}:{os.getgid()}','--cap-drop','ALL','--security-opt','no-new-privileges','--memory','2g','--cpus','2','--pids-limit','256','--tmpfs','/tmp:rw,nosuid,nodev,size=128m,mode=1777','--label','wire.migration.owner=background-worker-openssl-patched-build','--mount',f'type=bind,src={base/"openssl-3.5.9.tar.gz"},dst=/input/source.tar.gz,readonly','--mount',f'type=bind,src={work},dst=/work','--env','PATH='+':'.join('/nix/store/'+p+'/bin' for p in paths),'--entrypoint','/nix/store/gik3rh1vz2jlgnifb9dh6vc6sxwwz9jj-bash-5.3p9/bin/bash','sha256:cd03212430eef6cfde0324b50885c12980ee211483ab27dde6ee730252966cd2','-eu','-c','tar -xf /input/source.tar.gz --strip-components=1 -C /work/source\ncd /work/source\nperl Configure linux-x86_64 no-shared no-tests --prefix=/work/install\nmake -j2 build_sw\nmake install_sw\n/work/install/bin/openssl version -a']
start=time.time()
with (base/'build.log').open('w') as log:
 result=subprocess.run(args,stdout=log,stderr=subprocess.STDOUT,timeout=1200)
(base/'build-receipt.json').write_text(json.dumps({'command':args,'exit_code':result.returncode,'elapsed_seconds':time.time()-start},indent=2)+'\n')
print('Patched OpenSSL fixture build exit:',result.returncode,flush=True)
raise SystemExit(result.returncode)
