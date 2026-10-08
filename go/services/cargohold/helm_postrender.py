#!/usr/bin/env python3
"""Select only the cargohold Deployment image in an existing Wire Helm render."""
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
        if '# Source: wire-server/templates/cargohold/deployment.yaml\n' not in document:
            continue
        if not re.search(r'^kind: Deployment$',document,re.M) or not re.search(r'^  name: cargohold$',document,re.M):
            raise ValueError('unexpected cargohold deployment shape')
        if len(re.findall(r'^\s+- name: cargohold$',document,re.M))!=1:
            raise ValueError('expected one cargohold container')
        # Match the image only in the cargohold container, preserving every other byte.
        pattern=r'(?m)(^\s+- name: cargohold\n\s+image: )"[^"\n]+"'
        document,count=re.subn(pattern,lambda m:m[1]+json.dumps(image),document)
        if count!=1:raise ValueError('expected one cargohold image')
        documents[index]=document;matches+=1
    if matches!=1:raise ValueError('expected one cargohold deployment')
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
        print('Cargohold selection failed: '+str(error),file=sys.stderr)
        return 2

if __name__=='__main__':raise SystemExit(main())
