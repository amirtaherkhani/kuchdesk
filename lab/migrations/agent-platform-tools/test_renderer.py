"""Contract checks for the approved namespace rewrite on a rendered chart."""
import copy
import json
from pathlib import Path
import runpy
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[3]
RENDER = runpy.run_path(str(Path(__file__).with_name('post-renderer.py')))['render']

class RendererContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        manifest = subprocess.check_output([
            'helm', 'template', 'kuchdesk-agent', str(ROOT/'kuchdesk-agent/chart'),
            '-n', 'development-tools', '--set', 'cluster.id=example',
            '--set', 'cluster.name=Example', '--set', 'image.repository=example.invalid/agent',
            '--set', 'image.tag=immutable', '--set', 'rbac.clusterAdmin=true',
            '--set', 'agent.managementEnabled=true', '--set', 'dnsManagement.enabled=true',
        ])
        cls.original = json.loads(subprocess.check_output(
            ['yq', '-o=json', 'eval-all', '[.]', '-'], input=manifest))

    def test_only_approved_namespaces_change(self):
        result = RENDER(copy.deepcopy(self.original))
        count = 0
        for before, after in zip(self.original, result):
            if before['kind'] in ('Deployment', 'Service', 'ServiceAccount'):
                self.assertEqual(after['metadata'].pop('namespace'), 'platform-tools')
                if 'namespace' in before['metadata']:
                    after['metadata']['namespace'] = before['metadata']['namespace']
                count += 1
            elif before['kind'] in ('ClusterRoleBinding', 'RoleBinding'):
                self.assertEqual(after['subjects'][0]['namespace'], 'platform-tools')
                after['subjects'][0]['namespace'] = 'development-tools'
                count += 1
            self.assertEqual(before, after)
        self.assertEqual(count, 5)

    def test_unreviewed_scope_rejected(self):
        for scenario in ('missing', 'duplicate', 'extra', 'subject', 'roleRef', 'namespace'):
            with self.subTest(scenario=scenario):
                objects = copy.deepcopy(self.original)
                if scenario == 'missing': objects.pop()
                if scenario == 'duplicate': objects.append(copy.deepcopy(objects[0]))
                if scenario == 'extra': objects.append({'kind':'Secret','metadata':{'name':'unexpected'}})
                if scenario in ('subject','roleRef'):
                    binding = next(o for o in objects if o['kind']=='ClusterRoleBinding')
                    if scenario == 'subject': binding['subjects'][0]['namespace']='other'
                    else: binding['roleRef']['name']='different-role'
                if scenario == 'namespace':
                    next(o for o in objects if o['kind']=='Role')['metadata']['namespace']='other'
                with self.assertRaises(ValueError): RENDER(objects)

if __name__ == '__main__': unittest.main()
