"""Synthetic проверка direct TLS delivery, целого render и закрытых отказов SSH upload."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml
import ssh_identity_profile as profile
from deployment_profile import Rejected
from portable_state import verify_resource


def fixture():
    document = {'schema': profile.SCHEMA, 'origin': 'https://ssh-upload.example.invalid',
        'browser_origin': 'https://control.example.invalid', 'tls_secret_name': 'secret-broker-ssh-upload-tls',
        'ingress_namespace': 'traefik-system', 'ingress_pod_name': 'traefik'}
    resources = []
    for name in ('control-plane', 'secret-broker', 'staff-control-center'):
        labels = profile.SELECTOR if name == 'secret-broker' else {'app.kubernetes.io/name': name}
        resources.append({'apiVersion': 'apps/v1', 'kind': 'Deployment',
            'metadata': {'name': name, 'namespace': profile.NAMESPACE}, 'spec': {'template': {
                'metadata': {'labels': labels}, 'spec': {'containers': [{'name': name,
                    'image': 'registry.example.invalid/'+name+'@sha256:'+'b'*64,
                    'env': [{'name': 'UNCHANGED', 'value': 'synthetic'}]}]}}}})
    pod, _ = profile.container(resources, 'staff-control-center')
    pod['volumes'] = [{'name': 'runtime', 'configMap': {'name': 'staff-control-center-runtime-fixture'}}]
    template = (Path(__file__).resolve().parents[2]/'deploy/k8s/base/staff-control-center/security-headers.conf').read_text()
    resources.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'immutable': True,
        'metadata': {'name': 'staff-control-center-runtime-fixture', 'namespace': profile.NAMESPACE},
        'data': {'runtime-config.json': json.dumps({'oidc': {'authority': 'https://identity.example.invalid/realm/fixture'}}),
                 'security-headers.conf': template.replace('__KODEX_OIDC_ORIGIN__', 'https://identity.example.invalid')}})
    resources.extend([
        {'apiVersion': 'networking.k8s.io/v1', 'kind': 'Ingress',
         'metadata': {'name': 'staff-control-center', 'namespace': profile.NAMESPACE},
         'spec': {'rules': [{'host': 'control.example.invalid'}]}},
        {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
         'metadata': {'name': 'staff-control-center-ingress', 'namespace': profile.NAMESPACE},
         'spec': {'podSelector': {'matchLabels': {'app.kubernetes.io/name': 'staff-control-center'}},
                  'ingress': [{'from': [profile.peer(document)], 'ports': [{'port': 8443}]}]}}])
    return resources, document


def broker_fixture():
    """Возвращает принадлежащий Kodex Secret Broker для проверки полного render."""
    resources, _ = fixture()
    broker = profile.one(resources, 'Deployment', 'secret-broker')
    broker['metadata']['labels'] = {'app.kubernetes.io/part-of': 'kodex'}
    return broker


class Tests(unittest.TestCase):
    def test_actual_live_readback_rejects_added_route_authority(self):
        resources, document = fixture()
        expected = profile.one(profile.transform(resources, document, 'a'*40), 'IngressRouteTCP', profile.NAME)
        actual = copy.deepcopy(expected)
        actual['metadata'].update(uid='synthetic-original', resourceVersion='1')
        verify_resource(expected, actual)
        for variant in ('namespace', 'tls', 'middleware', 'annotation'):
            changed = copy.deepcopy(actual)
            if variant == 'namespace': changed['spec']['routes'][0]['services'][0]['namespace'] = 'foreign-namespace'
            if variant == 'tls': changed['spec']['tls']['secretName'] = 'foreign-secret'
            if variant == 'middleware': changed['spec']['routes'][0]['middlewares'] = [{'name': 'foreign'}]
            if variant == 'annotation': changed['metadata']['annotations'] = {'traefik.ingress.kubernetes.io/router.tls': 'false'}
            with self.subTest(variant=variant), self.assertRaises(Rejected): verify_resource(expected, changed)

    def test_both_real_profiles_preserve_images_and_use_direct_tls(self):
        root = Path(__file__).resolve().parents[2]
        for name in ('web-only', 'web-with-mattermost'):
            result = subprocess.run(['kubectl', '--kubeconfig', '/synthetic/unused', '--context', 'synthetic-offline',
                'kustomize', str(root/'deploy/k8s/profiles'/name)], check=True, capture_output=True, timeout=30)
            resources = list(filter(None, yaml.safe_load_all(result.stdout)))
            _, document = fixture()
            route = profile.one(resources, 'Ingress', 'staff-control-center')
            route['spec']['rules'][0]['host'] = 'control.example.invalid'
            host = route['spec']['rules'][0]['host']
            ingress = profile.one(resources, 'NetworkPolicy', 'staff-control-center-ingress')['spec']['ingress'][0]['from'][0]
            document.update(browser_origin='https://'+host,
                ingress_namespace=ingress['namespaceSelector']['matchLabels']['kubernetes.io/metadata.name'],
                ingress_pod_name=ingress['podSelector']['matchLabels']['app.kubernetes.io/name'])
            # Подставляем только document coordinates вместо шаблонов render fixture.
            document['ingress_namespace'] = 'traefik-system'
            document['ingress_pod_name'] = 'traefik'
            ingress.clear(); ingress.update(profile.peer(document))
            runtime = profile.frontend_runtime(resources)
            config = json.loads(runtime['data']['runtime-config.json'])
            config['oidc']['authority'] = 'https://identity.example.invalid/realm/fixture'
            runtime['data']['runtime-config.json'] = json.dumps(config)
            runtime['data']['security-headers.conf'] = runtime['data']['security-headers.conf'].replace(
                '__KODEX_OIDC_ORIGIN__', 'https://identity.example.invalid')
            before = copy.deepcopy(resources)
            wanted = profile.transform(resources, document, 'a'*40)
            self.assertEqual(resources, before)
            self.assertIsNone(profile.validate_render(list(reversed(wanted))))
            for workload in ('control-plane', 'secret-broker'):
                self.assertEqual(profile.container(resources, workload)[1]['image'], profile.container(wanted, workload)[1]['image'])
            self.assertEqual(profile.one(wanted, 'IngressRouteTCP', profile.NAME)['spec']['tls'], {'passthrough': True})
            self.assertFalse(any(r.get('kind') == 'Secret' for r in wanted if profile.LABEL in r.get('metadata', {}).get('labels', {})))

    def test_disabled_render_is_unchanged_and_partial_environment_is_rejected(self):
        resources, _ = fixture()
        before = copy.deepcopy(resources)
        profile.validate_render(resources)
        self.assertEqual(resources, before)
        profile.container(resources, 'secret-broker')[1]['env'].append({'name': 'SECRET_BROKER_SSH_UPLOAD_LISTEN', 'value': ':8446'})
        with self.assertRaises(Rejected): profile.validate_render(resources)

    def test_closed_origin_fields_source_and_existing_render_agreement(self):
        resources, document = fixture()
        for key, value in [('origin', 'http://ssh-upload.example.invalid'), ('origin', 'https://*.example.invalid'),
            ('origin', 'https://192.0.2.10'),
            ('origin', 'https://ssh-upload.example.invalid:443'), ('origin', 'https://ssh-upload.example.invalid/path'),
            ('origin', document['browser_origin']), ('browser_origin', 'https://foreign.example.invalid'),
            ('tls_secret_name', '../foreign'), ('ingress_namespace', 'foreign'), ('ingress_pod_name', '*'), ('unexpected', True)]:
            changed = copy.deepcopy(document); changed[key] = value
            with self.subTest(key=key, value=value), self.assertRaises((Rejected, ValueError)):
                profile.transform(resources, changed, 'a'*40)
        with self.assertRaises(Rejected): profile.transform(resources, document, '')

    def test_whole_render_drift_and_additive_network_policy_cannot_widen_upload(self):
        resources, document = fixture()
        for variant in ('env', 'secret', 'sidecar', 'service', 'tls', 'union', 'expression', 'source', 'record', 'partial', 'duplicate', 'csp', 'csp-ref'):
            wanted = profile.transform(resources, document, 'a'*40)
            pod, broker = profile.container(wanted, 'secret-broker')
            if variant == 'env': broker['env'][-1]['value'] = '/foreign/key'
            if variant == 'secret': pod['volumes'][-1]['secret']['items'].append({'key': 'foreign', 'path': 'extra'})
            if variant == 'sidecar': pod['containers'].append({'name': 'foreign', 'volumeMounts': copy.deepcopy(broker['volumeMounts'])})
            if variant == 'service': profile.one(wanted, 'Service', profile.NAME)['spec']['selector'] = {}
            if variant == 'tls': profile.one(wanted, 'IngressRouteTCP', profile.NAME)['spec']['tls']['passthrough'] = False
            if variant == 'union': wanted.append({'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
                'metadata': {'name': 'foreign', 'namespace': profile.NAMESPACE},
                'spec': {'podSelector': {}, 'ingress': [{'ports': [{'port': 8446}]}]}})
            if variant == 'expression': wanted.append({'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
                'metadata': {'name': 'foreign', 'namespace': profile.NAMESPACE}, 'spec': {
                    'podSelector': {'matchExpressions': [{'key': 'app.kubernetes.io/name', 'operator': 'In', 'values': ['secret-broker']}]},
                    'ingress': [{'ports': [{'port': 8446}]}]}})
            if variant == 'source': profile.one(wanted, 'ConfigMap', profile.NAME)['data']['sourceSHA'] = ''
            if variant == 'record': profile.one(wanted, 'ConfigMap', profile.NAME)['data']['profileSHA256'] = 'f'*64
            if variant == 'partial': wanted.pop()
            if variant == 'duplicate': wanted.append(copy.deepcopy(wanted[-1]))
            if variant == 'csp': profile.frontend_runtime(wanted)['data']['security-headers.conf'] += '\nadd_header Content-Security-Policy "connect-src *";\n'
            if variant == 'csp-ref': profile.container(wanted, 'staff-control-center')[0]['volumes'][0]['configMap']['name'] = 'foreign-runtime'
            with self.subTest(variant=variant), self.assertRaises(Rejected): profile.validate_render(wanted)

    def test_bounded_cli_binds_captured_profile_digest_and_rejects_changed_bytes(self):
        resources, document = fixture()
        root = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            render = directory/'render.yaml'; render.write_text(yaml.safe_dump_all(resources))
            configuration = directory/'profile.json'; raw = json.dumps(document).encode(); configuration.write_bytes(raw)
            command = ['python3', '-B', str(root/'ssh_identity_profile.py'), '--render', str(render),
                '--configuration', str(configuration), '--configuration-sha256', hashlib.sha256(raw).hexdigest(), '--source-sha', 'a'*40]
            subprocess.run(command, check=True, capture_output=True, timeout=10)
            subprocess.run(['python3', '-B', str(root/'ssh_identity_profile.py'), '--render', str(render), '--check'],
                check=True, capture_output=True, timeout=10)
            original = render.read_bytes(); configuration.write_bytes(raw+b' ')
            result = subprocess.run(command, check=False, capture_output=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(render.read_bytes(), original)
            self.assertEqual(result.stderr, b'SSH identity upload profile rejected\n')


if __name__ == '__main__':
    unittest.main()
