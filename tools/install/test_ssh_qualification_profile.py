"""Реальный Go decoder и offline Kubernetes consumer для SSH operator profile."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import time
import unittest

import yaml
import ssh_qualification_profile as profile
from deployment_profile import Rejected
from portable_state import owner, verify_resource
from test_ssh_identity_profile import fixture


def qualification():
    now = int(time.time())
    return json.dumps({'schema': 'kodex-ssh-qualifications/v2', 'profiles': [{
        'target_ref': 'ssh_target_example01', 'address': '192.0.2.10', 'port': 22, 'user': 'fixture', 'mode': 'EXECUTE',
        'host_key_sha256': '1'*64, 'host_key_algorithm': 'ssh-ed25519', 'authentication_key_sha256': '2'*64,
        'uid': 1001, 'gid': 1001, 'groups': [1001], 'qualified_at_unix': now-60, 'valid_until_unix': now+3600,
        'deployment_sha256': '3'*64, 'sshd_configuration_sha256': '4'*64, 'identity_inventory_sha256': '5'*64,
        'runtime_readback_sha256': '6'*64, 'identity_probe_sha256': '7'*64}]}, separators=(',', ':')).encode()


class Tests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.raw = qualification()
        resources, _ = fixture()
        pod, broker, _, _, _ = profile.broker_patch(resources)
        pod['initContainers'] = [{'name': 'unrelated-init', 'image': 'synthetic.example.invalid/init:fixed',
                                  'securityContext': {'runAsUser': 9007199254740993}}]
        pod['containers'].append({'name': 'unrelated-sidecar', 'image': 'synthetic.example.invalid/sidecar:fixed'})
        cls.rendered = profile.transform(resources, cls.raw)

    def test_actual_decoder_exact_projection_and_portable_ownership(self):
        profile.validate_render(self.rendered)
        projected = profile.qualification_objects(self.rendered)
        self.assertEqual(len(projected), 2)
        for resource in projected:
            self.assertTrue(owner(resource))
        config = next(item for item in projected if item['kind'] == 'ConfigMap')
        self.assertEqual(config['data']['profiles.json'].encode(), self.raw)
        _, broker, environment, mounts, _ = profile.broker_patch(self.rendered)
        self.assertEqual(broker['image'], 'registry.example.invalid/secret-broker@sha256:'+'b'*64)
        self.assertIn({'name': 'UNCHANGED', 'value': 'synthetic'}, environment)
        self.assertEqual(mounts[0]['readOnly'], True)
        pod, _, _, _, _ = profile.broker_patch(self.rendered)
        self.assertEqual(pod['initContainers'][0]['securityContext']['runAsUser'], 9007199254740993)
        self.assertEqual(pod['containers'][1]['image'], 'synthetic.example.invalid/sidecar:fixed')

    def test_both_real_profiles_use_same_canonical_go_projection(self):
        for name in ('web-only', 'web-with-mattermost'):
            with self.subTest(name=name):
                output = subprocess.run(['kubectl', '--kubeconfig', '/synthetic/unused', '--context', 'synthetic-offline',
                    'kustomize', str(profile.ROOT/'deploy/k8s/profiles'/name)], check=True, capture_output=True, timeout=30)
                resources = list(filter(None, yaml.safe_load_all(output.stdout)))
                before = copy.deepcopy(resources)
                rendered = profile.transform(resources, self.raw)
                self.assertEqual(resources, before)
                self.assertEqual(profile.broker_patch(resources)[1]['image'], profile.broker_patch(rendered)[1]['image'])
                self.assertEqual(profile.qualification_objects(rendered), profile.qualification_objects(self.rendered))
                key = lambda item: (item['apiVersion'], item['kind'], item['metadata'].get('namespace', ''), item['metadata']['name'])
                actual = {key(item): item for item in rendered}
                for item in before:
                    if item['kind'] != 'Deployment' or item['metadata']['name'] != 'secret-broker':
                        self.assertEqual(actual[key(item)], item)

    def test_partial_mount_environment_pin_and_network_widening_are_closed(self):
        for variant in ('digest', 'path', 'mount', 'duplicate-env', 'policy', 'record', 'label'):
            with self.subTest(variant=variant):
                resources = copy.deepcopy(self.rendered)
                pod, broker, environment, mounts, volumes = profile.broker_patch(resources)
                if variant == 'digest': next(item for item in environment if item['name'].endswith('_SHA256'))['value'] = '8'*64
                if variant == 'path': next(item for item in environment if item['name'].endswith('_FILE'))['value'] = '/foreign/profiles.json'
                if variant == 'mount': mounts[0]['readOnly'] = False
                if variant == 'duplicate-env': environment.append(copy.deepcopy(environment[0]))
                if variant == 'policy':
                    next(item for item in profile.qualification_objects(resources) if item['kind'] == 'NetworkPolicy')['spec']['egress'] = [{}]
                if variant == 'record':
                    next(item for item in profile.qualification_objects(resources) if item['kind'] == 'ConfigMap')['data']['profiles.json'] += '\n'
                if variant == 'label':
                    next(item for item in profile.qualification_objects(resources) if item['kind'] == 'ConfigMap')['metadata']['labels'].pop('app.kubernetes.io/part-of')
                with self.assertRaises(Rejected): profile.validate_render(resources)
        resources, _ = fixture()
        profile.validate_render(resources)
        profile.broker_patch(resources)[1]['env'].append({'name': 'SECRET_BROKER_SSH_QUALIFICATION_SHA256', 'value': '1'*64})
        with self.assertRaises(Rejected): profile.validate_render(resources)

    def test_expired_unknown_and_noncanonical_qualification_reject_before_projection(self):
        expired = json.loads(self.raw)
        expired['profiles'][0]['valid_until_unix'] = int(time.time())-1
        for raw in (self.raw+b'\n', json.dumps(expired, separators=(',', ':')).encode(), self.raw.replace(b'"mode":"EXECUTE"', b'"mode":"UNKNOWN"')):
            with self.subTest(rawSHA256=hashlib.sha256(raw).hexdigest()), self.assertRaises(Rejected):
                profile.canonical_projection(raw)

    def test_live_policy_drift_and_configuration_digest_rejected(self):
        expected = next(item for item in profile.qualification_objects(self.rendered) if item['kind'] == 'NetworkPolicy')
        actual = copy.deepcopy(expected)
        actual['metadata'].update(uid='synthetic-original', resourceVersion='1')
        verify_resource(expected, actual)
        actual['spec']['egress'] = [{}]
        with self.assertRaises(Rejected): verify_resource(expected, actual)
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory)/'render.yaml'
            config = Path(directory)/'profiles.json'
            resources, _ = fixture()
            original = yaml.safe_dump_all(resources).encode()
            target.write_bytes(original)
            config.write_bytes(self.raw)
            output = subprocess.run(['python3', '-B', str(profile.ROOT/'tools/install/ssh_qualification_profile.py'),
                '--render', str(target), '--configuration', str(config), '--configuration-sha256', '0'*64],
                capture_output=True, timeout=10)
            self.assertNotEqual(output.returncode, 0)
            self.assertEqual(target.read_bytes(), original)
            self.assertNotIn(b'192.0.2.10', output.stderr)


if __name__ == '__main__':
    unittest.main()
