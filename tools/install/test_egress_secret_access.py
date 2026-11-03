"""Синтетический partial render: только группа egress, без ослабления Secret mode."""
import copy
from pathlib import Path
import unittest
import yaml
import egress_secret_access as profile
from deployment_profile import Rejected


def fixture():
    root = Path(__file__).resolve().parents[2]
    deployment = yaml.safe_load((root/'deploy/k8s/base/egress-gateway/deployment.yaml').read_text())
    deployment['metadata']['namespace'] = 'kodex-system'
    deployment['metadata']['labels']['app.kubernetes.io/part-of'] = 'kodex'
    security = deployment['spec']['template']['spec']['securityContext']
    security.pop('fsGroup'); security.pop('fsGroupChangePolicy')
    return [deployment, {'kind': 'ConfigMap', 'metadata': {'name': 'synthetic-other'}, 'data': {'key': 'unchanged'}}]


class Tests(unittest.TestCase):
    def test_only_group_changes_and_repeat_preserves_inputs(self):
        before = fixture(); original = copy.deepcopy(before)
        after = profile.transform(before)
        self.assertEqual(before, original)
        self.assertEqual(after[1:], before[1:])
        security = after[0]['spec']['template']['spec']['securityContext']
        self.assertEqual(security['fsGroup'], 10001)
        self.assertEqual(security['fsGroupChangePolicy'], 'OnRootMismatch')
        self.assertEqual(profile.transform(after), after)
        security.pop('fsGroup'); security.pop('fsGroupChangePolicy')
        self.assertEqual(after, before)

    def test_unknown_workload_group_credentials_or_privileges_fail_closed(self):
        for change in ('missing', 'duplicate', 'namespace', 'group', 'user', 'mode', 'secret', 'writable', 'caps'):
            value = fixture(); pod = value[0]['spec']['template']['spec']
            if change == 'missing': value.pop(0)
            elif change == 'duplicate': value.append(copy.deepcopy(value[0]))
            elif change == 'namespace': value[0]['metadata']['namespace'] = 'other'
            elif change == 'group': pod['securityContext']['fsGroup'] = 12345
            elif change == 'user': pod['containers'][0]['securityContext']['runAsUser'] = 0
            elif change in ('mode', 'secret'):
                secret = next(v for v in pod['volumes'] if v['name'] == 'runtime-signing')['secret']
                secret.update({'defaultMode': 0o444} if change == 'mode' else {'secretName': 'other'})
            elif change == 'writable':
                next(v for v in pod['containers'][0]['volumeMounts'] if v['name'] == 'runtime-signing')['readOnly'] = False
            else: pod['containers'][0]['securityContext']['capabilities'] = {'add': ['ALL']}
            with self.subTest(change=change), self.assertRaises(Rejected): profile.transform(value)


if __name__ == '__main__': unittest.main()
