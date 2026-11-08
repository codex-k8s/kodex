"""Synthetic immutable-cache update, exact delta, sandbox compatibility и отрицательные границы."""
import copy
from pathlib import Path
import subprocess
import unittest
import yaml
from deployment_profile import Rejected
import image_policy_inputs as profile
import provider_sandbox_profile as sandbox
from test_provider_sandbox_profile import fixture as sandbox_fixture


def fixture():
    values = sandbox_fixture()
    runtime = sandbox.one(values, 'Deployment', 'runtime-controller')
    runtime['spec']['template']['spec']['containers'][0]['env'].append({
        'name': 'RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION',
        'valueFrom': {'configMapKeyRef': {'name': sandbox.SOURCE, 'key': 'roleRuntimeContractRevision'}}})
    for name in ('control-plane', 'role-image-builder', 'kodex-image-registry-pull'):
        item = copy.deepcopy(runtime); item['metadata']['name'] = name
        item['spec']['template']['spec']['containers'][0] = {
            'name': name, 'image': 'images.example.invalid/app@sha256:'+'e'*64,
            'env': [{'name': 'SYNTHETIC_POLICY_SHA256', 'valueFrom': {
                'configMapKeyRef': {'name': sandbox.SOURCE, 'key': 'policySHA256'}}},
                {'name': 'SYNTHETIC_SECRET_REFERENCE', 'valueFrom': {
                    'secretKeyRef': {'name': 'synthetic-owner-key', 'key': 'api-key'}}}]}
        values.append(item)
    return values


class Tests(unittest.TestCase):
    def test_both_complete_web_profile_manifests(self):
        root = Path(__file__).resolve().parents[2]
        for name in ('web-only', 'web-with-mattermost'):
            raw = subprocess.check_output(['kubectl','kustomize',str(root/'deploy/k8s/profiles'/name)], timeout=30)
            values = list(filter(None, yaml.safe_load_all(raw)))
            data = sandbox.one(values,'ConfigMap',sandbox.SOURCE)['data']
            data['nodeReadbackImage'] = 'images.example.invalid/runner@sha256:'+'a'*64
            data['policySHA256'] = sandbox.digest({k:v for k,v in data.items() if k not in ('policySHA256','orchestrationRevision')})
            with self.subTest(profile=name):
                actual = profile.transform(values,'versioned')
                self.assertTrue(profile.validate_render(actual))
                self.assertEqual(profile.transform(actual,'versioned'),actual)

    def test_default_preserves_original_and_versioned_is_exact_idempotent_delta(self):
        before = fixture(); original = copy.deepcopy(before)
        self.assertEqual(profile.transform(before), before)
        actual = profile.transform(before, 'versioned')
        self.assertEqual(before, original); self.assertEqual(profile.transform(actual, 'versioned'), actual)
        alias = next(r for r in actual if r.get('metadata', {}).get('name', '').startswith(profile.PREFIX))
        self.assertEqual(alias['data'], before[0]['data']); self.assertTrue(alias['immutable'])
        self.assertEqual(sandbox.one(actual, 'ConfigMap', sandbox.SOURCE), before[0])
        self.assertEqual(sandbox.one(actual, 'ValidatingAdmissionPolicyBinding', sandbox.BINDING, ''), before[2])
        self.assertEqual(next(r for r in actual if r['kind'] == 'Job'), before[3])
        restored = copy.deepcopy(actual); restored.remove(alias)
        for resource in restored:
            if resource.get('kind') != 'Deployment': continue
            template = resource['spec']['template']; template['metadata']['annotations'].pop(profile.DIGEST)
            for container in template['spec']['containers']:
                for env in container.get('env', []):
                    ref = env.get('valueFrom', {}).get('configMapKeyRef', {})
                    if ref.get('name') == alias['metadata']['name']: ref['name'] = sandbox.SOURCE
        self.assertEqual(restored, before)

    def test_changed_policy_creates_new_name_and_keeps_source_digest_valid(self):
        old = fixture(); new = copy.deepcopy(old); new[0]['data']['requiredTools'] = 'synthetic-updated'
        data = new[0]['data']; data['policySHA256'] = sandbox.digest({k:v for k,v in data.items() if k not in ('policySHA256','orchestrationRevision')})
        names = [next(r['metadata']['name'] for r in profile.transform(v,'versioned') if r['metadata']['name'].startswith(profile.PREFIX)) for v in (old,new)]
        self.assertNotEqual(*names)

    def test_independent_sandbox_projection_remains_exact(self):
        for selected in ('', sandbox.PROFILE):
            before = sandbox.transform(fixture(), selected); actual = profile.transform(before,'versioned')
            self.assertEqual(sandbox.active(actual), selected)
            self.assertEqual(sandbox.one(actual,'ValidatingAdmissionPolicyBinding',sandbox.BINDING,''), sandbox.one(before,'ValidatingAdmissionPolicyBinding',sandbox.BINDING,''))

    def test_authority_rebase_replaces_alias_without_changing_canonical_owner(self):
        original = profile.transform(fixture(),'versioned'); changed = copy.deepcopy(original)
        data = sandbox.one(changed,'ConfigMap',sandbox.SOURCE)['data']; data['requiredTools'] = 'synthetic-next'
        data['policySHA256'] = sandbox.digest({k:v for k,v in data.items() if k not in ('policySHA256','orchestrationRevision')})
        actual = profile.rebase(changed,original)
        self.assertTrue(profile.validate_render(actual))
        self.assertNotEqual(next(r['metadata']['name'] for r in original if r['metadata']['name'].startswith(profile.PREFIX)),
                            next(r['metadata']['name'] for r in actual if r['metadata']['name'].startswith(profile.PREFIX)))
        self.assertEqual(sandbox.one(actual,'ConfigMap',sandbox.SOURCE),sandbox.one(changed,'ConfigMap',sandbox.SOURCE))

    def test_missing_foreign_tampered_and_mixed_inputs_fail_closed(self):
        for change in ('missing','owner','namespace','source-digest','alias-data','marker','missing-markers','mixed','foreign-alias','unknown-key','unknown-mode'):
            values = fixture(); mode = 'versioned'
            if change == 'missing': values = [r for r in values if r['metadata']['name'] != 'control-plane']
            elif change == 'owner': values[0]['metadata']['labels'] = {}
            elif change == 'namespace': values[0]['metadata']['namespace'] = 'foreign'
            elif change == 'source-digest': values[0]['data']['policySHA256'] = 'f'*64
            elif change == 'unknown-mode': mode = 'other'
            elif change == 'unknown-key': sandbox.one(values,'Deployment','control-plane')['spec']['template']['spec']['containers'][0]['env'][0]['valueFrom']['configMapKeyRef']['key'] = 'unregistered'
            else:
                values = profile.transform(values,'versioned'); alias = next(r for r in values if r['metadata']['name'].startswith(profile.PREFIX))
                if change == 'alias-data': alias['data']['requiredTools'] = 'foreign'
                elif change == 'foreign-alias': alias['metadata']['namespace'] = 'foreign'
                elif change == 'missing-markers':
                    for r in values:
                        if r.get('kind') == 'Deployment': r['spec']['template']['metadata']['annotations'].pop(profile.DIGEST)
                elif change == 'marker': sandbox.one(values,'Deployment','control-plane')['spec']['template']['metadata']['annotations'][profile.DIGEST] = 'f'*64
                else: sandbox.one(values,'Deployment','control-plane')['spec']['template']['spec']['containers'][0]['env'][0]['valueFrom']['configMapKeyRef']['name'] = sandbox.SOURCE
            with self.subTest(change=change), self.assertRaises(Rejected): profile.transform(values,mode)
        with self.assertRaises(Rejected): profile.transform(profile.transform(fixture(),'versioned'),'direct')


if __name__ == '__main__': unittest.main()
