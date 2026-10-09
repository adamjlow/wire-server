"""Automated optional image and real Helm selection proof; no deployment/push."""
import argparse, hashlib, json, re, subprocess, tempfile, time, uuid
from pathlib import Path
import helm_postrender
ROOT=Path(__file__).resolve().parents[3]
REFERENCE='quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27'
p=argparse.ArgumentParser();p.add_argument('--image-receipt',type=Path,required=True);p.add_argument('--fixtures',type=Path,required=True);p.add_argument('--output',type=Path,required=True);p.add_argument('--helm',type=Path,required=True);p.add_argument('--chart',type=Path,required=True);p.add_argument('--values',type=Path,required=True);a=p.parse_args()
if a.output.exists():raise ValueError('new evidence output required')
def command(args):return subprocess.check_output(list(map(str,args)),stderr=subprocess.PIPE,text=True,timeout=90).strip()
receipt=json.loads(a.image_receipt.read_text());image=receipt['image_id'];network='wire-federator-image-'+uuid.uuid4().hex;container=None;created=False
try:
    command(['docker','network','create','--internal',network]);created=True
    with tempfile.TemporaryDirectory(prefix='federator-image-conf-') as tmp:
        directory=Path(tmp);directory.chmod(0o755)
        cfg=json.loads(a.fixtures.read_text())['federator']
        config=directory/'federator.yaml';config.write_text(json.dumps(cfg));config.chmod(0o444)
        container=command(['docker','create','--name',network+'-service','--network',network,'--network-alias','candidate','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--pids-limit','128','--memory','192m','--cpus','1','--mount','type=bind,source='+str(ROOT)+',target=/source,readonly','--mount','type=bind,source='+str(directory)+',target=/etc/wire/federator/conf,readonly',image])
        inspected=json.loads(command(['docker','inspect',container]))[0]
        if inspected['Config']['User']!='65534:65534' or not inspected['HostConfig']['ReadonlyRootfs'] or inspected['HostConfig']['PortBindings']:raise ValueError('image isolation mismatch')
        command(['docker','start',container])
        health=command(['docker','run','--rm','--user','65534:65534','--network',network,'--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--pids-limit','64','--memory','128m','--entrypoint','curl',REFERENCE,'--fail','--retry','10','--retry-delay','1','--retry-all-errors','--silent','--show-error','--write-out','%{http_code}','http://candidate:8097/i/status'])
        if health!='200':raise ValueError('image default-config health failed')
        started=time.monotonic();command(['docker','stop','--time','4',container]);elapsed=time.monotonic()-started
        code=json.loads(command(['docker','inspect',container]))[0]['State']['ExitCode']
        if code!=0 or elapsed>4:raise ValueError('image shutdown failed')
        # Render existing chart without changes: selected result can differ ONLY
        # at its one federator image. The source chart currently has no exec hook.
        for source in (ROOT/'charts/wire-server').rglob('*'):
            if source.is_file() and source.read_bytes()!=(a.chart/source.relative_to(ROOT/'charts/wire-server')).read_bytes():raise ValueError('cached source chart changed')
        rendered=command([a.helm,'template','wire',a.chart,'--values',a.values,'--set','tags.federation=true','--set','federator.tls.useSharedFederatorSecret=true'])+'\n'
        if helm_postrender.select(rendered,None)!=rendered:raise ValueError('default render changed')
        selected=helm_postrender.select(rendered,image)
        pattern=r'(?m)(^\s+image: )"[^"\n]+"'
        original_images=re.findall(pattern,rendered);new_images=re.findall(pattern,selected)
        changes=[(i,x,y) for i,(x,y) in enumerate(zip(rendered.splitlines(),selected.splitlines(),strict=True)) if x!=y]
        if len(changes)!=1 or 'image: '+json.dumps(image) not in changes[0][2]:raise ValueError('selection changed more than one image')
        for invalid in ('bad image\n',):
            try:helm_postrender.select(rendered,invalid)
            except ValueError:pass
            else:raise ValueError('invalid image selected')
        try:helm_postrender.select('kind: Deployment\nmetadata:\n  name: unknown\n',image)
        except ValueError:pass
        else:raise ValueError('missing deployment accepted')
        source=(ROOT/'charts/federator/templates/deployment.yaml').read_text()
        if 'preStop' in source or 'command:' in source or 'args:' in source:raise ValueError('source lifecycle changed; verify real commands')
        report={'result':'pass','image_receipt':receipt,'user':'65534:65534','health':200,'shutdown':{'exit_code':code,'seconds':round(elapsed,3)},'source_chart_exec_hook':None,'helm':{'default_unchanged':True,'changes':changes,'sha256':hashlib.sha256(rendered.encode()).hexdigest(),'fail_closed':True}}
        a.output.write_text(json.dumps(report,indent=2)+'\n')
finally:
    if container is not None:subprocess.run(['docker','rm','--force',container],check=True,capture_output=True,timeout=30)
    if created:subprocess.run(['docker','network','rm',network],check=True,capture_output=True,timeout=30)
print(json.dumps(report,indent=2))
