#!/usr/bin/env python3
"""Synthetic validation, secret-file and complete optional release render regressions."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('model_provider', ROOT / 'tools/install/model-provider.py')
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def fixture():
    return dict(schema='kodex.model-provider/v1', managed_by='git', source_revision='a' * 40, engine='opencode',
                engine_version='1.19.5', tool_mode='managed_mcp', provider_id='test-gateway',
                base_url='https://gateway.example.invalid/v1', address_cidrs=['192.0.2.4/32'],
                models=[dict(id='vendor/model-v1', context_window_tokens=32768, maximum_output_tokens=1024)])


class ModelProviderTests(unittest.TestCase):
    def test_secret_is_file_only_and_configuration_is_closed(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            config, key, output = directory / 'provider.json', directory / 'key', directory / 'auth.json'
            config.write_text(json.dumps(fixture()))
            key.write_text('synthetic-gateway-key')
            key.chmod(0o600)
            value = MODULE.configuration(config)
            MODULE.prepare_auth(value, key, output)
            secret = json.loads(output.read_text())
            self.assertEqual(secret['auth_mode'], 'gateway-api-key')
            self.assertEqual(secret['provider_definition_key'], 'opencode-test-gateway')
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                MODULE.prepare_auth(value, key, output)
            invalid = fixture()
            invalid['API_KEY'] = 'synthetic-gateway-key'
            config.write_text(json.dumps(invalid))
            with self.assertRaises(Exception):
                MODULE.configuration(config)

    def test_missing_opencode_role_base_profile_is_rejected_before_render(self):
        with tempfile.TemporaryDirectory()as temporary:
            directory=Path(temporary);lock=directory/'lock.json';lock.write_text('{}')
            config=directory/'provider.json';config.write_text(json.dumps(fixture()))
            value=subprocess.run([str(ROOT/'tools/release/render-web-only.sh'),'--lock',str(lock),'--lock-sha256',hashlib.sha256(lock.read_bytes()).hexdigest(),
                '--output',str(directory/'render.yaml'),'--model-provider-configuration',str(config),'--model-provider-default-model','vendor/model-v1'],capture_output=True,timeout=15)
            self.assertNotEqual(value.returncode,0)
            self.assertIn(b'OpenCode requires a matching role-base runtime profile',value.stderr)

    def test_complete_optional_render_binds_images_provider_and_egress(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            inventory = json.loads((ROOT / 'tools/release/images.json').read_text())['images']
            lock = {'role_base_runtime_engine': 'opencode', 'schema_version': 2, 'profile': 'web-only', 'source_sha': 'b' * 40, 'build_run_id': 'local',
                    'registry': {'push': 'push.example.invalid', 'node_pull': 'pull.example.invalid', 'repository_prefix': 'kodex'},
                    'images': [{'component': item['component'], 'repository': 'kodex/' + item['component'],
                                'digest': 'sha256:' + 'c' * 64,
                                'pull_ref': 'pull.example.invalid/kodex/' + item['component'] + '@sha256:' + 'c' * 64} for item in inventory],
                    'external_images': [{'component': 'admission-tools', 'digest': 'sha256:' + 'd' * 64,
                                         'pull_ref': 'tools.example.invalid/admission@sha256:' + 'd' * 64}],
                    'role_image_input': {'repository': 'kodex/role-image-inputs', 'manifest_digest': 'sha256:' + 'e' * 64,
                                         'payload_sha256': 'f' * 64, 'source_sha256': 'a' * 64,
                                         'pull_ref': 'pull.example.invalid/kodex/role-image-inputs@sha256:' + 'e' * 64}}
            lock_path, output, config = directory / 'lock.json', directory / 'render.yaml', directory / 'provider.json'
            lock_path.write_text(json.dumps(lock))
            config.write_text(json.dumps(fixture()))
            authority=directory/'authority.json';authority.write_text(json.dumps({'schema':'kodex.authority-registry/v1','source_revision':1}))
            args = [str(ROOT / 'tools/release/render-web-only.sh'), '--lock', str(lock_path), '--lock-sha256', hashlib.sha256(lock_path.read_bytes()).hexdigest(),
                    '--output', str(output), '--public-host', 'console.example.invalid', '--public-origin', 'https://console.example.invalid',
                    '--oidc-issuer', 'https://sso.example.invalid/realms/test', '--oidc-jwks-url', 'https://sso.example.invalid/realms/test/protocol/openid-connect/certs',
                    '--oidc-connect-address', 'keycloak.kodex-identity.svc:8443', '--oidc-tls-server-name', 'sso.example.invalid',
                    '--promoted-pull-host', 'pull.example.invalid', '--kubernetes-api-service-cidr', '192.0.2.10/32',
                    '--kubernetes-api-endpoint-cidrs', '192.0.2.11/32', '--kubernetes-api-endpoint-ports', '6443', '--ingress-class', 'nginx',
                    '--cluster-issuer', 'test-issuer', '--ingress-namespace', 'ingress-nginx', '--ingress-pod-name', 'ingress-nginx',
                    '--oidc-namespace', 'kodex-identity', '--oidc-pod-name', 'keycloak', '--oidc-pod-component', 'identity', '--oidc-target-port', '8443',
                    '--model-provider-configuration', str(config), '--model-provider-default-model', 'vendor/model-v1', '--disable-observability', '--authority-registry-configuration', str(authority)]
            result = subprocess.run(args, cwd=ROOT, capture_output=True, timeout=150,
                                    env={**os.environ, 'GOMAXPROCS': '2', 'GOFLAGS': '-p=1', 'GOWORK': 'off'})
            self.assertEqual(result.returncode, 0, (result.stdout + result.stderr).decode()[-3000:])
            resources = [item for item in MODULE.yaml.safe_load_all(output.read_text()) if item]
            self.assertFalse(any(item['apiVersion'] == 'monitoring.coreos.com/v1' for item in resources))
            authority_cm=next(item for item in resources if item['kind']=='ConfigMap'and item['metadata']['name']=='internal-rpc-authority-publisher-target-registry')
            self.assertEqual(MODULE.yaml.safe_load(authority_cm['data']['key-delivery-targets.yaml'])['source_revision'],1)
            for item in resources:
                if item['kind'] in ('Deployment', 'StatefulSet', 'DaemonSet', 'Job'):
                    for container in item['spec']['template']['spec']['containers']:
                        self.assertIn(dict(name='OTEL_SDK_DISABLED', value='true'), container['env'])
            projections = [item for item in resources if item['kind'] == 'ConfigMap' and item['metadata']['name'].startswith('kodex-model-provider-')]
            self.assertEqual(len(projections), 1)
            self.assertEqual(json.loads(projections[0]['data']['provider.json']), fixture())
            self.assertTrue(projections[0]['immutable'])
            self.assertTrue(all(item['metadata'].get('labels',{}).get('app.kubernetes.io/part-of')=='kodex' for item in resources if item['kind']!='Secret'))
            for identity in ('control-plane', 'secret-broker'):
                deployment = next(item for item in resources if item['kind'] == 'Deployment' and item['metadata']['name'] == identity)
                pod = deployment['spec']['template']['spec']
                container = next(item for item in pod['containers'] if item['name'] == identity)
                env = {item['name']: item.get('value') for item in container['env']}
                self.assertIn(identity.upper().replace('-', '_') + '_MODEL_PROVIDER_CONFIGURATION_FILE', env)
                self.assertTrue(any(item.get('configMap', {}).get('name') == projections[0]['metadata']['name'] for item in pod['volumes']))
            policy = next(json.loads(item['data']['policy.json']) for item in resources if item['kind'] == 'ConfigMap' and item['metadata']['name'].startswith('egress-gateway-policy-'))
            self.assertEqual(policy['spec']['modelProvider'], fixture())
            catalog = next(json.loads(item['data']['catalog.json']) for item in resources if item['kind'] == 'ConfigMap' and item['metadata']['name'].startswith('kodex-role-environments-'))
            standard = next(item for item in catalog['environments'] if item['key'] == 'standard')
            self.assertTrue(standard['baseImageReference'].endswith('/agent-runner-opencode'))


if __name__ == '__main__':
    unittest.main()
