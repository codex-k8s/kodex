"""Synthetic проверки operator artifact, чистого render и закрытого deployment consumer."""
import copy
from datetime import datetime, timedelta, timezone
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml

import observability_qualification as profile
from deployment_profile import Rejected


def fixture(now=None):
    now = now or datetime.now(timezone.utc).replace(microsecond=0)
    proof = lambda name: {'case': name, 'requestSha256': 'e'*64, 'responseSha256': 'f'*64, 'httpStatus': 200}
    item = {'kind': 'GRAFANA_LEGACY_SQL_ANNOTATIONS_V1', 'origin': 'https://grafana.example.invalid',
            'providerVersion': '13.2.0', 'organizationId': '1', 'dashboardUid': 'fixture-dashboard',
            'credentialSha256': '1'*64, 'imageSha256': 'a'*64, 'configurationSha256': 'b'*64,
            'writerInventorySha256': 'c'*64, 'runtimeReadbackSha256': 'd'*64,
            'qualifiedAt': (now-timedelta(minutes=1)).isoformat().replace('+00:00', 'Z'),
            'validUntil': (now+timedelta(hours=1)).isoformat().replace('+00:00', 'Z'),
            'writerMode': 'LEGACY_SQL_ANNOTATIONS', 'annotationStoreBackend': 'legacy-sql',
            'annotationApiMigrationPhase': 'off', 'proofs': [proof(name) for name in sorted(profile.BASE_PROOFS)]}
    return {'schema': profile.SCHEMA, 'profiles': [item]}


def resources():
    return [{'apiVersion': 'apps/v1', 'kind': 'Deployment',
             'metadata': {'name': 'integration-gateway', 'namespace': 'kodex-system'},
             'spec': {'template': {'metadata': {'labels': {'app.kubernetes.io/name': 'integration-gateway'}},
                      'spec': {'containers': [{'name': 'integration-gateway',
                        'image': 'registry.example.invalid/integration-gateway@sha256:'+'a'*64,
                        'env': [{'name': 'INTEGRATION_GATEWAY_NATIVE_PROXY_URL', 'value': 'http://egress-gateway-native.kodex-system.svc.cluster.local:8085'}],
                        'volumeMounts': []}], 'volumes': []}}}}]


class Tests(unittest.TestCase):
    def test_cross_language_golden_matches_native_bytes_and_profiles(self):
        root = Path(__file__).resolve().parents[2]
        raw = (root/'contracts/integrations/observability-qualifications/v1/synthetic-golden.json').read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), '3a0e8c4c753e05d5bb19319db428b5abbc1be64bef52e815d131204bdafd6e67')
        document = profile.validate(json.loads(raw, object_pairs_hook=profile.unique_object), datetime(2026, 11, 14, 12, tzinfo=timezone.utc))
        self.assertEqual(len(document['profiles']), 3)
        self.assertEqual(profile.wire(document), raw)

    def test_render_is_pure_immutable_and_exact(self):
        source = resources()
        before = copy.deepcopy(source)
        document = fixture()
        rendered = profile.transform(source, document)
        self.assertEqual(source, before)
        self.assertEqual(profile.check(rendered), document)
        self.assertEqual(len(rendered), 2)
        template, container = profile.workload(rendered)
        sha = hashlib.sha256(profile.wire(document)).hexdigest()
        self.assertIn({'name': profile.SHA_ENV, 'value': sha}, container['env'])
        self.assertEqual(template['spec']['volumes'][0]['configMap']['defaultMode'], 0o444)
        self.assertTrue(container['volumeMounts'][0]['readOnly'])
        self.assertTrue(rendered[-1]['immutable'])

    def test_complete_environment_render_and_native_composition(self):
        import native_integration_egress as native
        from test_native_integration_egress import fixture as native_fixture
        root = Path(__file__).resolve().parents[2]
        for name in ('web-only', 'web-with-mattermost'):
            result = subprocess.run(['kubectl', 'kustomize', str(root/'deploy/k8s/profiles'/name)], check=True, capture_output=True, timeout=30)
            source = list(filter(None, yaml.safe_load_all(result.stdout)))
            self.assertIsNone(profile.check(source))
            _, routes = native_fixture()
            _, gateway = native.container(source, 'egress-gateway')
            routes['gateway_policy_digest'] = next(v['value'] for v in gateway['env'] if v['name'] == 'EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST')
            source = native.transform(source, routes)
            rendered = profile.transform(source, fixture())
            self.assertEqual(native.check(rendered), routes)
            self.assertIsNotNone(profile.check(rendered))

    def test_disabled_is_unchanged_and_hidden_process_inputs_are_rejected(self):
        source = resources()
        self.assertIsNone(profile.check(source))
        _, container = profile.workload(source)
        container['env'].append({'name': profile.FILE_ENV, 'value': profile.MOUNT+'/profiles.json'})
        with self.assertRaises(Rejected):
            profile.check(source)

    def test_deployment_drift_never_becomes_authority(self):
        for variant in ('sha', 'namespace', 'writable', 'other-mount', 'mutable', 'sidecar', 'extra-cm', 'no-native'):
            rendered = profile.transform(resources(), fixture())
            template, container = profile.workload(rendered)
            if variant == 'sha': container['env'][-1]['value'] = '9'*64
            if variant == 'namespace': rendered[-1]['metadata']['namespace'] = 'other'
            if variant == 'writable': template['spec']['volumes'][0]['configMap']['defaultMode'] = 0o644
            if variant == 'other-mount': container['volumeMounts'][0]['mountPath'] = '/other'
            if variant == 'mutable': rendered[-1]['immutable'] = False
            if variant == 'sidecar': template['spec']['initContainers'] = [{'name': 'unexpected', 'env': [{'name': profile.SHA_ENV, 'value': 'a'*64}]}]
            if variant == 'extra-cm': rendered.append(copy.deepcopy(rendered[-1]))
            if variant == 'no-native': container['env'] = [v for v in container['env'] if v['name'] != 'INTEGRATION_GATEWAY_NATIVE_PROXY_URL']
            with self.assertRaises(Rejected, msg=variant): profile.check(rendered)

    def test_profile_scope_expiry_and_proof_fail_closed(self):
        for variant in ('expired', 'long-validity', 'unknown-mode', 'unknown-field', 'key', 'weak-proof', 'duplicate', 'backend', 'migration', 'path'):
            document = fixture()
            item = document['profiles'][0]
            if variant == 'expired': item['validUntil'] = item['qualifiedAt']
            if variant == 'long-validity': item['validUntil'] = (datetime.now(timezone.utc)+timedelta(days=2)).isoformat().replace('+00:00', 'Z')
            if variant == 'unknown-mode': item['kind'] = 'CALLER_JSON_IS_AUTHORITY'
            if variant == 'unknown-field': item['ownerApproved'] = True
            if variant == 'key': item['credentialSha256'] = '0'*64
            if variant == 'weak-proof': item['proofs'][0]['httpStatus'] = 401
            if variant == 'duplicate': document['profiles'].append(copy.deepcopy(item))
            if variant == 'backend': item['annotationStoreBackend'] = 'new-api'
            if variant == 'migration': item['annotationApiMigrationPhase'] = 'proxy-writes'
            if variant == 'path': item['origin'] += '/user/controlled'
            with self.assertRaises(Rejected, msg=variant): profile.validate(document)

    def test_cli_uses_pinned_bytes_and_never_invokes_apply(self):
        root = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory(prefix='kodex-obs-installer-') as directory:
            rendered, source = Path(directory)/'render.yaml', Path(directory)/'qualification.json'
            rendered.write_text(yaml.safe_dump_all(resources()))
            raw = profile.wire(fixture())
            source.write_bytes(raw)
            command = ['python3', '-B', str(root/'observability_qualification.py'), '--render', str(rendered)]
            subprocess.run(command+['--configuration', str(source), '--configuration-sha256', hashlib.sha256(raw).hexdigest()], check=True, timeout=10)
            subprocess.run(command+['--check'], check=True, timeout=10)
            source.write_bytes(b'{}')
            denied = subprocess.run(command+['--configuration', str(source), '--configuration-sha256', hashlib.sha256(raw).hexdigest()], capture_output=True, timeout=10)
            self.assertNotEqual(denied.returncode, 0)
            self.assertNotIn(b'example.invalid', denied.stderr)


if __name__ == '__main__':
    unittest.main()
