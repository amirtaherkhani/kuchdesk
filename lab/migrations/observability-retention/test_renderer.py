"""Check the candidate against a private, pinned baseline Helm render.

Usage: python3 test_renderer.py /private/monitoring-base.yaml
The baseline is not copied into Git or printed; see README for its inputs.
"""
import copy
import json
from pathlib import Path
import subprocess
import sys
import unittest

BASELINE = Path(sys.argv.pop(1)).read_bytes()
RENDERER = Path(__file__).with_name('post-renderer.rb')
RULE = ('PrometheusRule', 'monitoring-kube-prometheus-kube-apiserver-availability.rules')
DASHBOARD = ('ConfigMap', 'monitoring-kube-prometheus-apiserver')

def parse(raw):
    return [o for o in json.loads(subprocess.check_output(
        ['yq', '-o=json', 'eval-all', '[.]', '-'], input=raw)) if isinstance(o, dict)]

def key(obj):
    return obj['kind'], obj['metadata']['name']

def stream(objects):
    return '\n---\n'.join(json.dumps(o) for o in objects).encode()

class RetentionRendererContract(unittest.TestCase):
    def test_preserves_other_objects_and_every_alert(self):
        before = parse(BASELINE)
        after = parse(subprocess.check_output([str(RENDERER)], input=BASELINE))
        self.assertEqual(len(before), len(after))
        changed = {key(a) for a, b in zip(before, after) if a != b}
        self.assertEqual(changed, {RULE, DASHBOARD})
        for a, b in zip(before, after):
            self.assertEqual(key(a), key(b))
            if key(a) == RULE:
                original, result = copy.deepcopy(a), copy.deepcopy(b)
                old_rules = original['spec']['groups'][0].pop('rules')
                new_rules = result['spec']['groups'][0].pop('rules')
                self.assertEqual(original, result)
                self.assertEqual(len(old_rules), len(new_rules))
                old_names = {r['record'] for r in old_rules if '30d' in r['record']}
                self.assertEqual(sum('30d' in r['record'] for r in old_rules), 8)
                self.assertEqual(sum('3d' in r['record'] for r in new_rules), 8)
                for old_rule, new_rule in zip(old_rules, new_rules):
                    changed_fields = {k for k in old_rule if old_rule[k] != new_rule.get(k)}
                    self.assertEqual(set(old_rule), set(new_rule))
                    self.assertEqual(changed_fields, {'record', 'expr'} if '30d' in old_rule['record'] else set())
                self.assertTrue(all(name not in json.dumps(after) for name in old_names))
            elif key(a) == DASHBOARD:
                original, result = copy.deepcopy(a), copy.deepcopy(b)
                original['data'].pop('apiserver.json')
                result['data'].pop('apiserver.json')
                self.assertEqual(original, result)
            else:
                self.assertEqual(a, b)
        alerts = lambda docs: [r for o in docs if o['kind'] == 'PrometheusRule'
                              for g in o['spec']['groups'] for r in g['rules'] if 'alert' in r]
        self.assertEqual(alerts(before), alerts(after))

    def test_rejects_missing_duplicate_drift_and_reapplication(self):
        objects = parse(BASELINE)
        for scenario in ('missing', 'duplicate', 'rule-drift', 'dashboard-drift', 'reapply'):
            with self.subTest(scenario=scenario):
                docs = copy.deepcopy(objects)
                rule = next(o for o in docs if key(o) == RULE)
                dashboard = next(o for o in docs if key(o) == DASHBOARD)
                if scenario == 'missing': docs.remove(rule)
                if scenario == 'duplicate': docs.append(copy.deepcopy(rule))
                if scenario == 'rule-drift': rule['spec']['groups'][0]['rules'][0]['expr'] += ' + 0'
                if scenario == 'dashboard-drift': dashboard['data']['apiserver.json'] = '{}'
                raw = subprocess.check_output([str(RENDERER)], input=BASELINE) if scenario == 'reapply' else stream(docs)
                result = subprocess.run([str(RENDERER)], input=raw, capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, b'')

if __name__ == '__main__':
    unittest.main()
