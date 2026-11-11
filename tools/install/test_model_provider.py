#!/usr/bin/env python3
"""Synthetic validation, secret-file and complete optional release render regressions."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import tomllib
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
    def assert_role_catalog_authority(self, resources, expected_component):
        deployment = next(item for item in resources if item['kind'] == 'Deployment' and item['metadata']['name'] == 'control-plane')
        pod = deployment['spec']['template']
        container = next(item for item in pod['spec']['containers'] if item['name'] == 'control-plane')
        field = next(item for item in container['env'] if item['name'] == 'CONTROL_PLANE_DEFAULT_ROLE_IMAGE_REFERENCE')
        selector = field['valueFrom']['configMapKeyRef']
        parameters = next(item for item in resources if item['kind'] == 'ConfigMap' and item['metadata']['name'] == selector['name'])
        default_image = parameters['data'][selector['key']]
        catalog_map = next(item for item in resources if item['kind'] == 'ConfigMap' and item['metadata']['name'].startswith('kodex-role-environments-'))
        catalog_raw = catalog_map['data']['catalog.json']
        catalog = json.loads(catalog_raw)
        standard = next(item for item in catalog['environments'] if item['key'] == 'standard')
        documents = next(item for item in catalog['environments'] if item['key'] == 'documents')
        self.assertEqual(standard['baseImageReference'] + '@' + standard['baseImageDigest'], default_image)
        self.assertEqual(parameters['data']['trustedRoleBaseRepository'], standard['baseImageReference'])
        self.assertEqual(parameters['data']['trustedRoleBaseDigest'], standard['baseImageDigest'])
        for name in ('control-plane', 'role-image-builder', 'kodex-buildkit'):
            consumer = next(item for item in resources if item['kind'] == 'Deployment' and item['metadata']['name'] == name)
            annotations = consumer['spec']['template']['metadata']['annotations']
            self.assertEqual(annotations['kodex.dev/trusted-role-base-repository'], standard['baseImageReference'])
            if name != 'control-plane':
                self.assertEqual(annotations['kodex.dev/trusted-role-base-digest'], standard['baseImageDigest'])
            if name == 'role-image-builder':
                container = next(item for item in consumer['spec']['template']['spec']['containers'] if item['name'] == name)
                reference = next(item for item in container['env'] if item['name'] == 'ROLE_IMAGE_BUILDER_TRUSTED_ROLE_BASE_REPOSITORY')
                self.assertEqual(reference['valueFrom']['fieldRef']['fieldPath'], "metadata.annotations['kodex.dev/trusted-role-base-repository']")
        self.assertEqual(standard['baseImageReference'], 'pull.example.invalid/kodex/' + expected_component)
        self.assertEqual(documents['baseImageReference'], 'pull.example.invalid/kodex/role-base-documents')
        self.assertEqual(documents['available'], expected_component == 'agent-runner')
        buildkit = next(r for r in resources if r['kind'] == 'Deployment' and r['metadata']['name'] == 'kodex-buildkit')
        config = next(r for r in resources if r['kind'] == 'ConfigMap' and r['metadata']['name'] == 'kodex-buildkit-config')['data']['buildkitd.toml']
        self.assertEqual(buildkit['spec']['template']['metadata']['annotations']['kodex.dev/buildkit-config-sha256'], hashlib.sha256(config.encode()).hexdigest())
        transport = tomllib.loads(config)['registry']['pull.example.invalid']
        self.assertNotIn('http', transport); self.assertNotIn('insecure', transport)
        self.assertEqual(transport['ca'], ['/var/run/secrets/kodex/buildkit/tls/pull-ca.pem'])
        self.assertEqual(len(transport['keypair']), 1)
        digest = hashlib.sha256(catalog_raw.encode()).hexdigest()
        self.assertTrue(catalog_map['immutable'])
        self.assertEqual(catalog_map['metadata']['name'], 'kodex-role-environments-' + digest[:12])
        self.assertEqual(pod['metadata']['annotations']['kodex.dev/role-environments-sha256'], digest)

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
            first = output.read_bytes()
            MODULE.prepare_auth(value, key, output)
            self.assertEqual(first, output.read_bytes())
            self.assertRegex(secret['cache_namespace_key'], r'^[a-f0-9]{64}$')
            key.write_text('synthetic-other-key')
            with self.assertRaises(ValueError):
                MODULE.prepare_auth(value, key, output)
            self.assertEqual(first, output.read_bytes())
            invalid = fixture()
            invalid['API_KEY'] = 'synthetic-gateway-key'
            config.write_text(json.dumps(invalid))
            with self.assertRaises(Exception):
                MODULE.configuration(config)

    def test_unsupported_role_base_engine_is_rejected_before_render(self):
        with tempfile.TemporaryDirectory()as temporary:
            directory=Path(temporary);lock=directory/'lock.json';lock.write_text('{"role_base_runtime_engine":"codex"}')
            config=directory/'provider.json';config.write_text(json.dumps(fixture()))
            value=subprocess.run([str(ROOT/'tools/release/render-web-only.sh'),'--lock',str(lock),'--lock-sha256',hashlib.sha256(lock.read_bytes()).hexdigest(),
                '--output',str(directory/'render.yaml'),'--public-host','console.example.invalid','--public-origin','https://console.example.invalid','--model-provider-configuration',str(config),'--model-provider-default-model','vendor/model-v1'],capture_output=True,timeout=15)
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
                    '--model-provider-configuration', str(config), '--model-provider-default-model', 'vendor/model-v1', '--disable-observability', '--disable-monitoring-resources', '--authority-registry-configuration', str(authority)]
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
            first_render = output.read_bytes()
            MODULE.render(fixture(), output, 'vendor/model-v1')
            self.assertEqual(first_render, output.read_bytes())
            catalog = next(json.loads(item['data']['catalog.json']) for item in resources if item['kind'] == 'ConfigMap' and item['metadata']['name'].startswith('kodex-role-environments-'))
            standard = next(item for item in catalog['environments'] if item['key'] == 'standard')
            self.assertTrue(standard['baseImageReference'].endswith('/agent-runner'))
            self.assert_role_catalog_authority(resources, 'agent-runner')
            import provider_sandbox_profile
            for selected in (provider_sandbox_profile.PROFILE, ''):
                policy_arguments = ['--image-policy-inputs-mode', 'versioned'] if selected == provider_sandbox_profile.PROFILE else []
                rendered = subprocess.run(args + ['--provider-apparmor-profile', selected] + policy_arguments, cwd=ROOT, capture_output=True, timeout=150,
                                          env={**os.environ, 'GOMAXPROCS': '2', 'GOFLAGS': '-p=1', 'GOWORK': 'off'})
                self.assertEqual(rendered.returncode, 0, 'optional provider sandbox render failed')
                actual = list(filter(None, MODULE.yaml.safe_load_all(output.read_text())))
                expected = provider_sandbox_profile.transform(resources, selected)
                if policy_arguments:
                    import copy, image_policy_inputs, promoted_registry
                    expected = image_policy_inputs.transform(expected, 'versioned')
                    self.assertTrue(image_policy_inputs.validate_render(actual))
                    self.assertEqual(promoted_registry.bind(copy.deepcopy(actual), lock), actual)
                self.assertEqual(actual, expected)
                self.assertEqual(provider_sandbox_profile.active(actual), selected)
            # Без optional provider остаются штатные Codex defaults с тем же authority.
            baseline_args = list(args)
            for option in ('--model-provider-configuration', '--model-provider-default-model'):
                index = baseline_args.index(option)
                del baseline_args[index:index + 2]
            lock.pop('role_base_runtime_engine')
            lock['registry']['repository_prefix'] = 'synthetic-source'
            for image in lock['images']:
                image['repository'] = 'synthetic-source/' + image['component']
                image['pull_ref'] = lock['registry']['node_pull'] + '/' + image['repository'] + '@' + image['digest']
            lock['role_image_input']['repository'] = 'synthetic-source/role-image-inputs'
            lock['role_image_input']['pull_ref'] = lock['registry']['node_pull'] + '/' + lock['role_image_input']['repository'] + '@' + lock['role_image_input']['manifest_digest']
            lock_path.write_text(json.dumps(lock))
            baseline_args[baseline_args.index('--lock-sha256') + 1] = hashlib.sha256(lock_path.read_bytes()).hexdigest()
            baseline = subprocess.run(baseline_args, cwd=ROOT, capture_output=True, timeout=150,
                                      env={**os.environ, 'GOMAXPROCS': '2', 'GOFLAGS': '-p=1', 'GOWORK': 'off'})
            self.assertEqual(baseline.returncode, 0, (baseline.stdout + baseline.stderr).decode()[-3000:])
            baseline_resources = list(filter(None, MODULE.yaml.safe_load_all(output.read_text())))
            self.assert_role_catalog_authority(baseline_resources, 'agent-runner')
            self.assertFalse(any(item['kind'] == 'ConfigMap' and item['metadata']['name'].startswith('kodex-model-provider-') for item in baseline_resources))


if __name__ == '__main__':
    unittest.main()
