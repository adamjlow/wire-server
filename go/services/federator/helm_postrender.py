#!/usr/bin/env python3
"""Select only the federator Deployment image in an existing Wire Helm render."""
import argparse
import json
import re
import sys


def select(rendered, image):
    if image is None:
        return rendered
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._:/@-]{1,254}',image):
        raise ValueError('invalid container image reference')
    documents=rendered.split('\n---\n')
    matches=0
    for index,document in enumerate(documents):
        if '# Source: wire-server/charts/federator/templates/deployment.yaml\n' not in document:
            continue
        if not re.search(r'^kind: Deployment$',document,re.M) or not re.search(r'^  name: federator$',document,re.M):
            raise ValueError('unexpected federator deployment shape')
        if len(re.findall(r'^\s+- name: federator$',document,re.M))!=1:
            raise ValueError('expected one federator container')
        # Preserve the rendered configuration, environment and any peers. Only
        # select the existing federator container block, including its env stanza.
        lines=document.splitlines(keepends=True)
        indices=[i for i,line in enumerate(lines) if re.fullmatch(r'\s+- name: federator\n?',line)]
        if len(indices)!=1:raise ValueError('expected one federator container')
        start=indices[0];indent=len(lines[start])-len(lines[start].lstrip());end=len(lines)
        for i in range(start+1,len(lines)):
            if lines[i].strip() and not lines[i].lstrip().startswith('#') and len(lines[i])-len(lines[i].lstrip())<=indent:
                end=i;break
        block=''.join(lines[start:end])
        block,count=re.subn(r'(?m)(^\s+image: )"[^"\n]+"',lambda m:m[1]+json.dumps(image),block)
        if count!=1:raise ValueError('expected one federator image')
        document=''.join(lines[:start])+block+''.join(lines[end:])
        documents[index]=document;matches+=1
    if matches!=1:raise ValueError('expected one federator deployment')
    return '\n---\n'.join(documents)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('image',nargs='?')
    args=parser.parse_args()
    try:
        rendered=sys.stdin.read(16*1024*1024+1)
        if len(rendered)>16*1024*1024:raise ValueError('render exceeds bounded input')
        sys.stdout.write(select(rendered,args.image))
        return 0
    except ValueError as error:
        print('Federator selection failed: '+str(error),file=sys.stderr)
        return 2

if __name__=='__main__':raise SystemExit(main())
