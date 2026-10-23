"""Offline safety checks for the explicitly invoked Kubernetes smoke fixture."""
import json
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]


class LocalK6Smoke(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.objects = json.loads(subprocess.check_output([
            'yq', 'eval-all', '-o=json', '[.]',
            str(ROOT / 'lab/apps/observability/k6/tests/local-dashboard-smoke.yaml'),
        ], text=True))
        cls.job = next(x for x in cls.objects if x['kind'] == 'Job')
        cls.pod = cls.job['spec']['template']['spec']

    def test_bounded_credential_free_runner(self):
        self.assertEqual(self.job['spec']['backoffLimit'], 0)
        self.assertLessEqual(self.job['spec']['activeDeadlineSeconds'], 120)
        self.assertFalse(self.pod['automountServiceAccountToken'])
        self.assertFalse(any('secret' in x or 'hostPath' in x for x in self.pod['volumes']))
        for container in self.pod['containers'] + self.pod['initContainers']:
            self.assertIn('@sha256:', container['image'])
            self.assertFalse(container['securityContext']['allowPrivilegeEscalation'])
            self.assertNotIn('envFrom', container)

    def test_fixed_owned_target_and_budget(self):
        script = next(x for x in self.objects if x['kind'] == 'ConfigMap')['data']['test.js']
        self.assertIn('vus: 1', script)
        self.assertIn('iterations: 20', script)
        self.assertIn("http.get('http://127.0.0.1:8080/')", script)
        self.assertNotIn('__ENV.TARGET', script)
        self.assertIn('maxRedirects: 0', script)
        self.assertIn('testrun_name: __ENV.RUN_ID', script)
        self.assertIn('owned target body matches', script)

    def test_local_ui_and_backend_separation(self):
        env = {x['name']: x for x in self.pod['containers'][0]['env']}
        self.assertTrue(env['K6_PROMETHEUS_RW_SERVER_URL']['value'].endswith(':9090/api/v1/write'))
        self.assertEqual(env['K6_WEB_DASHBOARD_PORT']['value'], '5665')
        self.assertEqual(env['K6_WEB_DASHBOARD_EXPORT']['value'], '/results/dashboard.html')
        self.assertIn('job-name', env['RUN_ID']['valueFrom']['fieldRef']['fieldPath'])
        policy = next(x for x in self.objects if x['kind'] == 'NetworkPolicy')['spec']
        self.assertEqual(policy['ingress'], [])
        self.assertEqual({p['port'] for r in policy['egress'] for p in r['ports']}, {53, 9090})
        self.assertEqual({x['kind'] for x in self.objects}, {'ConfigMap', 'Job', 'NetworkPolicy'})


if __name__ == '__main__':
    unittest.main()
