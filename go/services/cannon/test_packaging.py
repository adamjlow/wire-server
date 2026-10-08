import importlib.util
from pathlib import Path
import unittest

path=Path(__file__).with_name('helm_postrender.py')
spec=importlib.util.spec_from_file_location('cannon_selection',path);selection=importlib.util.module_from_spec(spec);spec.loader.exec_module(selection)

RENDER='''# Source: wire-server/templates/cannon/statefulset.yaml
kind: StatefulSet
metadata:
  name: cannon
spec:
  template:
    spec:
      containers:
      - name: nginz
        image: "quay.io/wire/nginz:reference"
      - name: cannon
        env:
          - name: RABBITMQ_USERNAME
            valueFrom:
              secretKeyRef:
                name: cannon
                key: rabbitmqUsername
        image: "quay.io/wire/cannon:reference"
        args:
          - -c
          - /etc/wire/cannon/conf/cannon.yaml
      initContainers:
      - name: cannon-configurator
        image: "quay.io/wire/cannon-configurator:reference"
'''
class PackagingTests(unittest.TestCase):
    def test_selection_preserves_sidecar_init_config_and_omitted_default(self):
        self.assertEqual(RENDER,selection.select(RENDER,None))
        expected=RENDER.replace('"quay.io/wire/cannon:reference"','"wire-cannon-go:local"')
        self.assertEqual(expected,selection.select(RENDER,'wire-cannon-go:local'))
    def test_ambiguous_or_missing_shape_and_invalid_image_fail_closed(self):
        for value in (RENDER+'\n---\n'+RENDER,RENDER.replace('kind: StatefulSet','kind: Deployment'),RENDER.replace('      - name: cannon\n','      - name: wrong\n'),RENDER.replace('        image: "quay.io/wire/cannon:reference"\n','')):
            with self.assertRaises(ValueError):selection.select(value,'wire-cannon-go:local')
        with self.assertRaises(ValueError):selection.select(RENDER,'unsafe image\nname')
