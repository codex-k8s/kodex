"""Synthetic проверки opt-in render, поколения, полного desired union и отсутствия API effects."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import subprocess
import unittest

import yaml

import native_integration_egress as profile
from deployment_profile import Rejected


def fixture():
    resources = []
    for name in profile.PEERS:
        env = [{'name': 'UNCHANGED', 'value': 'synthetic'}]
        if name == 'egress-gateway':
            env.append({'name': 'EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST', 'value': 'a'*64})
        resources.append({'apiVersion': 'apps/v1', 'kind': 'Deployment',
            'metadata': {'name': name, 'namespace': 'kodex-system'},
            'spec': {'template': {'metadata': {'labels': profile.labels(name)},
                'spec': {'containers': [{'name': name, 'image': 'registry.example.invalid/'+name+'@sha256:'+'b'*64,
                    'env': env, 'ports': [], 'volumeMounts': []}], 'volumes': []}}}})
    resources.append({'apiVersion': 'v1', 'kind': 'ConfigMap',
                      'metadata': {'name': 'unchanged', 'namespace': 'kodex-system'}, 'data': {'fixture': 'synthetic'}})
    document = {'schema': profile.SCHEMA, 'generation': 1, 'gateway_policy_digest': 'a'*64,
        'destinations': [{'hostname': 'gitlab.example.invalid', 'port': 443, 'address_cidrs': ['10.77.0.4/32', 'fd77::4/128']}]}
    return resources, document


class Tests(unittest.TestCase):
    def test_complete_desired_render_of_both_supported_profiles(self):
        root = Path(__file__).resolve().parents[2]
        for name in ('web-only', 'web-with-mattermost'):
            completed = subprocess.run(['kubectl', 'kustomize', str(root/'deploy/k8s/profiles'/name)], check=True, capture_output=True, timeout=30)
            resources = list(filter(None, yaml.safe_load_all(completed.stdout)))
            self.assertIsNone(profile.check(resources))
            _, document = fixture()
            _, gateway = profile.container(resources, 'egress-gateway')
            document['gateway_policy_digest'] = next(v['value'] for v in gateway['env'] if v['name'] == 'EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST')
            wanted = profile.transform(resources, document)
            self.assertEqual(profile.check(wanted), document)
            self.assertEqual(len(wanted), len(resources)+4)

    def test_disabled_is_unchanged_and_no_native_assets_exist(self):
        resources, _ = fixture()
        before = copy.deepcopy(resources)
        self.assertIsNone(profile.check(resources))
        self.assertEqual(resources, before)

    def test_wire_matches_gateway_cross_language_golden(self):
        _, document = fixture()
        self.assertEqual(hashlib.sha256(profile.wire(profile.validate(document))).hexdigest(),
                         'a28747c10413d65f6618f38a15c06f2960f16d8e37f12ed1c5661a81df822c1f')

    def test_opt_in_is_complete_immutable_bounded_and_generation_fenced(self):
        resources, document = fixture()
        before = copy.deepcopy(resources)
        result = profile.transform(resources, document)
        self.assertEqual(resources, before)
        self.assertEqual(profile.check(result), document)
        self.assertEqual(len(result), len(resources)+4)
        self.assertEqual(result[2], before[2])
        service = profile.selected(result, 'Service', 'egress-gateway-native')
        template, gateway = profile.container(result, 'egress-gateway')
        self.assertEqual(service['spec']['selector'][profile.GENERATION], template['metadata']['labels'][profile.GENERATION])
        self.assertLessEqual(len(service['spec']['selector'][profile.GENERATION]), 63)
        policy = profile.selected(result, 'NetworkPolicy', 'egress-gateway-native-path')
        self.assertEqual(policy['spec']['ingress'], [{'from': [profile.peer('integration-gateway')], 'ports': [{'protocol': 'TCP', 'port': 8085}]}])
        self.assertEqual([p['ipBlock']['cidr'] for p in policy['spec']['egress'][0]['to']], ['10.77.0.4/32', 'fd77::4/128'])
        changed = copy.deepcopy(document); changed['generation'] += 1
        next_result = profile.transform(resources, changed)
        self.assertNotEqual(service['spec']['selector'], profile.selected(next_result, 'Service', 'egress-gateway-native')['spec']['selector'])

    def test_resource_drift_cannot_hide_behind_a_valid_document(self):
        resources, document = fixture()
        for variant in ('service', 'env', 'pin', 'namespace', 'mount', 'unknown-label', 'base'):
            result = profile.transform(resources, document)
            if variant == 'service': profile.selected(result, 'Service', 'egress-gateway-native')['spec']['selector'].pop(profile.GENERATION)
            if variant == 'env': profile.container(result, 'integration-gateway')[1]['env'][-1]['value'] = 'http://other.example.invalid:8085'
            if variant == 'pin': profile.selected(result, 'NetworkPolicy', 'egress-gateway-native-path')['spec']['egress'][0]['to'][0]['ipBlock']['cidr'] = '10.77.0.0/24'
            if variant == 'namespace': profile.selected(result, 'NetworkPolicy', 'integration-gateway-native-path')['metadata']['namespace'] = 'other'
            if variant == 'mount': profile.container(result, 'egress-gateway')[1]['volumeMounts'][-1]['readOnly'] = False
            if variant == 'unknown-label': result[-1]['metadata']['labels'][profile.LABEL] = 'foreign'
            if variant == 'base': profile.container(result, 'egress-gateway')[1]['env'][1]['value'] = 'c'*64
            with self.subTest(variant=variant), self.assertRaises(Rejected): profile.check(result)

    def test_unsafe_or_ambiguous_operator_inputs_are_rejected(self):
        _, document = fixture()
        for pin in ('10.77.0.0/24', '127.0.0.1/32', '169.254.169.254/32', '100.64.0.1/32', '::1/128', 'fe80::1/128', '::ffff:10.77.0.4/128', '224.0.0.1/32'):
            changed = copy.deepcopy(document); changed['destinations'][0]['address_cidrs'] = [pin]
            with self.subTest(pin=pin), self.assertRaises(Rejected): profile.validate(changed)
        for host in ('*.example.invalid', 'https://gitlab.example.invalid', '127.0.0.1', 'API.example.invalid', 'api.example.invalid.'):
            changed = copy.deepcopy(document); changed['destinations'][0]['hostname'] = host
            with self.subTest(host=host), self.assertRaises(Rejected): profile.validate(changed)
        resources, _ = fixture()
        changed = copy.deepcopy(document); changed['gateway_policy_digest'] = 'b'*64
        with self.assertRaises(Rejected): profile.transform(resources, changed)

    def test_file_digest_and_duplicate_fields_are_checked_before_render(self):
        _, document = fixture()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'operator.json'; raw = json.dumps(document).encode(); path.write_bytes(raw)
            self.assertEqual(profile.load(path, hashlib.sha256(raw).hexdigest()), document)
            with self.assertRaises(Rejected): profile.load(path, '0'*64)
            raw = b'{"schema":"one","schema":"two"}'; path.write_bytes(raw)
            with self.assertRaises(Rejected): profile.load(path, hashlib.sha256(raw).hexdigest())

    def test_full_policy_union_rejects_additive_broad_native_access(self):
        resources, document = fixture()
        for direction, field, name in (('ingress', 'from', 'egress-gateway'), ('egress', 'to', 'integration-gateway')):
            for rule in ({}, {field: []}, {field: [{'namespaceSelector': {}}], 'ports': [{'port': 8085}]},
                         {field: [profile.peer('integration-gateway' if name == 'egress-gateway' else 'egress-gateway')], 'ports': [{'port': 8080, 'endPort': 8090}]}):
                bad = copy.deepcopy(resources)
                bad.append({'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
                    'metadata': {'name': 'broad', 'namespace': 'kodex-system'},
                    'spec': {'podSelector': {'matchLabels': profile.labels(name)}, direction: [rule]}})
                if rule.get(field) == [profile.peer('integration-gateway' if name == 'egress-gateway' else 'egress-gateway')]:
                    profile.transform(bad, document)  # Точный peer безопасен и для более широкого port set.
                else:
                    with self.subTest(direction=direction, rule=rule), self.assertRaises(Rejected): profile.transform(bad, document)


if __name__ == '__main__':
    unittest.main()
