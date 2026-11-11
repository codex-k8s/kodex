"""Synthetic typed registry overlay contracts; без live API/credentials."""
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import tomllib
import unittest
from unittest.mock import patch
import registry_source as registry

def fixture():
    config = {'schema': 'kodex.registry-source/v1', 'namespace': 'synthetic-infra', 'service': 'synthetic-registry', 'service_uid': '11111111-1111-1111-1111-111111111111', 'pod_labels': {'app.kubernetes.io/name': 'synthetic-registry'}, 'target_port': 8443, 'ca_configmap': 'synthetic-ca'}
    host = 'synthetic-registry.synthetic-infra.svc.cluster.local'
    lock = {'registry': {'push': host, 'node_pull': host, 'repository_prefix': 'synthetic'}}

    def resource(kind, name, spec):
        return {'apiVersion': 'v1', 'kind': kind, 'metadata': {'name': name, 'namespace': 'kodex-system'}, 'spec': spec}
    job = resource('Job', 'release-artifact-materializer', {'template': {'spec': {'containers': [{'name': 'materializer', 'env': [{'name': 'RELEASE_SOURCE_REGISTRY', 'value': host}, {'name': 'NO_PROXY', 'value': 'destination.synthetic.svc.cluster.local'}]}]}}})
    policy = resource('NetworkPolicy', 'release-artifact-materializer-exact-paths', {'egress': [{'ports': [{'protocol': 'TCP', 'port': 8080}]}]})
    admission = resource('ConfigMap', 'kodex-image-admission-policy', {})
    admission.pop('spec')
    admission['data'] = {'pullRegistryHost': 'kodex-image-registry-pull.kodex-system.svc.cluster.local', 'promotedPullRepository': host + '/kodex/roles', 'policySHA256': '0' * 64, 'orchestrationRevision': '2' * 40, 'policyRevision': '1'}
    parameters = resource('ImageAdmissionPolicyParameters', 'kodex-image-admission-policy', {})
    parameters['apiVersion'] = 'supplychain.kodex.dev/v1alpha1'
    service = resource('Service', 'kodex-image-registry-pull', {'ports': [{'name': 'https', 'port': 5000, 'targetPort': 'registry'}]})
    route = resource('IngressRouteTCP', 'kodex-image-registry-pull', {'tls': {'passthrough': True}})
    lock['images'] = [{'component': 'control-plane', 'digest': 'sha256:' + 'c' * 64}, {'component': 'agent-runner', 'digest': 'sha256:' + 'b' * 64}]
    admission['data'].update(promotionRepository='kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003/kodex/roles', trustedRoleBaseRepository='kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner', trustedRoleBaseDigest='sha256:' + 'b' * 64)
    job['spec']['template']['spec']['containers'][0]['env'].append({'name': 'AGENT_RUNNER_SOURCE_REF', 'value': host + '/synthetic/agent-runner@sha256:' + 'b' * 64})
    script = resource('ConfigMap', 'kodex-release-artifact-materializer', {})
    script.pop('spec')
    script['data'] = {'release-artifact-materializer.sh': 'synthetic-old-script'}
    deployment = resource('Deployment', 'kodex-image-registry-pull', {'template': {'spec': {'containers': [{'name': 'certificate-guard', 'env': [{'name': 'READBACK_IMAGE', 'value': 'synthetic-old-path'}]}, {'name': 'pull-authorizer', 'env': []}]}}})
    consumers = [resource('Deployment', name, {'template': {'metadata': {'annotations': {
        'kodex.dev/trusted-role-base-repository': admission['data']['trustedRoleBaseRepository'],
        'kodex.dev/trusted-role-base-digest': admission['data']['trustedRoleBaseDigest']}}, 'spec': {'containers': []}}})
        for name in ('control-plane', 'role-image-builder', 'kodex-buildkit')]
    buildkit_config = {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': 'kodex-buildkit-config', 'namespace': 'kodex-system'},
        'data': {'buildkitd.toml': '[worker.oci]\n  enabled = true\n'}}
    return (config, lock, [job, policy, admission, parameters, service, route, script, deployment, buildkit_config, *consumers])

class RegistrySource(unittest.TestCase):

    def test_ingress_source_preserves_tls_identity_and_rejects_alias_collision(self):
        config, lock, resources = fixture()
        host = lock['registry']['push']
        config.update(schema='kodex.registry-source/v2', tls_hostname=host,
                      service_ip='192.0.2.20', service_port_name='websecure')
        lock['registry']['node_pull'] = 'node-cache.example.invalid'
        result = registry.transform(resources, config, lock)
        pod = registry.one(result, 'Job', 'release-artifact-materializer')['spec']['template']['spec']
        self.assertEqual(pod['hostAliases'], [{'ip': '192.0.2.20', 'hostnames': [host]}])
        self.assertEqual(registry.transform(result, config, lock), result)
        pod['hostAliases'][0]['ip'] = '192.0.2.21'
        with self.assertRaises(ValueError):
            registry.transform(result, config, lock)
        for field, value in [('service_ip', '127.0.0.1'), ('service_ip', '169.254.169.254'),
                             ('service_ip', '0.0.0.0'), ('tls_hostname', 'foreign.example.invalid'),
                             ('tls_hostname', 'bad..example.invalid'), ('service_port_name', '443')]:
            invalid = dict(config, **{field: value})
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                registry.transform(resources, invalid, lock)

    def test_ingress_preflight_pins_service_ip_and_named_target(self):
        config, lock, _ = fixture()
        config.update(schema='kodex.registry-source/v2', tls_hostname=lock['registry']['push'],
                      service_ip='192.0.2.20', service_port_name='websecure')
        service = {'metadata': {'uid': config['service_uid']}, 'spec': {'type': 'LoadBalancer',
            'clusterIP': config['service_ip'], 'selector': config['pod_labels'],
            'ports': [{'port': 443, 'targetPort': 'websecure', 'protocol': 'TCP'}]}}
        slices = {'items': [{'metadata': {'ownerReferences': [{'kind': 'Service', 'uid': config['service_uid'], 'controller': True}]},
            'ports': [{'port': 8443, 'protocol': 'TCP'}], 'endpoints': [{'conditions': {'ready': True},
            'addresses': ['192.0.2.9'], 'targetRef': {'kind': 'Pod', 'namespace': config['namespace'], 'name': 'synthetic', 'uid': 'pod-uid'}}]}]}
        pod = {'metadata': {'uid': 'pod-uid', 'labels': config['pod_labels']},
            'spec': {'containers': [{'ports': [{'name': 'websecure', 'containerPort': 8443}]}]},
            'status': {'podIPs': [{'ip': '192.0.2.9'}]}}
        def run_case(service_value, pod_value, accepted):
            def get(command, **kwargs):
                kind = command[command.index('get') + 1]
                value = {'service': service_value, 'configmap': {'data': {'ca.crt': '-----BEGIN CERTIFICATE-----synthetic'}},
                         'endpointslices': slices, 'pod': pod_value}[kind]
                return subprocess.CompletedProcess(command, 0, json.dumps(value).encode())
            with patch.object(registry.subprocess, 'run', side_effect=get), patch('ssl.create_default_context'):
                if accepted:
                    self.assertEqual(registry.preflight(config, lock, 'synthetic-context')['workload_tls_and_auth'], 'NOT_RUN')
                else:
                    with self.assertRaises(ValueError):registry.preflight(config, lock, 'synthetic-context')
        run_case(service, pod, True)
        bad = copy.deepcopy(service);bad['spec']['clusterIP'] = '192.0.2.21'
        run_case(bad, pod, False)
        bad = copy.deepcopy(service);bad['spec']['ports'][0]['targetPort'] = 'web'
        run_case(bad, pod, False)
        bad = copy.deepcopy(pod);bad['spec']['containers'][0]['ports'][0]['containerPort'] = 8080
        run_case(service, bad, False)

    def test_promoted_buildkit_tls_is_exact_and_config_digest_rolls_only_daemon(self):
        config, lock, resources = fixture()
        result = registry.transform(resources, config, lock)
        text = registry.one(result, 'ConfigMap', 'kodex-buildkit-config')['data']['buildkitd.toml']
        table = tomllib.loads(text)['registry']['kodex-image-registry-pull.kodex-system.svc.cluster.local']
        self.assertEqual(table, {'ca': ['/var/run/secrets/kodex/buildkit/tls/pull-ca.pem'], 'keypair': [{
            'cert': '/var/run/secrets/kodex/buildkit/tls/base-registry-client.crt',
            'key': '/var/run/secrets/kodex/buildkit/tls/base-registry-client.key'}]})
        sha = hashlib.sha256(text.encode()).hexdigest()
        self.assertEqual(registry.one(result, 'Deployment', 'kodex-buildkit')['spec']['template']['metadata']['annotations']['kodex.dev/buildkit-config-sha256'], sha)
        self.assertEqual(registry.transform(result, config, lock), result)
        for suffix in ('\n[registry."kodex-image-registry-pull.kodex-system.svc.cluster.local"]\n insecure=true\n',
                       "\n[registry.'kodex-image-registry-pull.kodex-system.svc.cluster.local']\n http=true\n"):
            bad = copy.deepcopy(resources)
            registry.one(bad, 'ConfigMap', 'kodex-buildkit-config')['data']['buildkitd.toml'] += suffix
            with self.assertRaises(ValueError): registry.transform(bad, config, lock)

    def test_runtime_pull_policy_is_opt_in_and_rejects_lock_or_environment_conflict(self):
        config,lock,resources=fixture()
        result=registry.transform(resources,config,lock)
        authorizer=registry.one(result,'Deployment','kodex-image-registry-pull')['spec']['template']['spec']['containers'][1]
        self.assertEqual(authorizer['env'],[])
        for engine in ('opencode','unknown'):
            with self.subTest(engine=engine),self.assertRaises(ValueError):registry.transform(resources,config,{**lock,'role_base_runtime_engine':engine})
        for env in ([{'name':'REGISTRY_PULL_RUNTIME_ENGINE','value':'opencode'}],
                    [{'name':'REGISTRY_PULL_RUNTIME_ENGINE','value':'codex'}]*2,
                    [{'name':'REGISTRY_PULL_RUNTIME_ENGINE','valueFrom':{'configMapKeyRef':{'name':'synthetic','key':'engine'}}}]):
            altered=copy.deepcopy(resources)
            registry.one(altered,'Deployment','kodex-image-registry-pull')['spec']['template']['spec']['containers'][1]['env']=env
            with self.subTest(env=env),self.assertRaises(ValueError):registry.transform(altered,config,lock)

    def test_opencode_materializer_and_promoted_readback_use_destination_namespace(self):
        config, lock, resources = fixture()
        data = next((v['data'] for v in resources if v['kind'] == 'ConfigMap' and v['metadata']['name'] == 'kodex-image-admission-policy'))
        data['trustedRoleBaseRepository'] = data['trustedRoleBaseRepository'].replace('/agent-runner', '/agent-runner-opencode')
        for resource in resources:
            if resource['kind'] == 'Deployment' and resource['metadata']['name'] in ('control-plane', 'role-image-builder', 'kodex-buildkit'):
                resource['spec']['template']['metadata']['annotations']['kodex.dev/trusted-role-base-repository'] = data['trustedRoleBaseRepository']
        lock['images'][1]['component'] = 'agent-runner-opencode'
        job = next((v for v in resources if v['kind'] == 'Job'))
        for item in job['spec']['template']['spec']['containers'][0]['env']:
            if item['name'] == 'AGENT_RUNNER_SOURCE_REF':
                item['value'] = item['value'].replace('/agent-runner@', '/agent-runner-opencode@')
        with self.assertRaises(ValueError):
            registry.transform(resources, config, lock)
        lock['role_base_runtime_engine'] = 'opencode'
        result = registry.transform(resources, config, lock)
        data = registry.one(result, 'ConfigMap', 'kodex-image-admission-policy')['data']
        self.assertEqual(data['nodeReadbackImage'], 'kodex-image-registry-pull.kodex-system.svc.cluster.local/kodex/agent-runner-opencode@sha256:' + 'b' * 64)
        env = registry.one(result, 'Job', 'release-artifact-materializer')['spec']['template']['spec']['containers'][0]['env']
        self.assertIn({'name': 'AGENT_RUNNER_DESTINATION_REPOSITORY', 'value': 'kodex/agent-runner-opencode'}, env)
        authorizer = registry.one(result, 'Deployment', 'kodex-image-registry-pull')['spec']['template']['spec']['containers'][1]
        self.assertEqual(authorizer['env'], [{'name': 'REGISTRY_PULL_RUNTIME_ENGINE', 'value': 'opencode'}])
        guard = registry.one(result, 'Deployment', 'kodex-image-registry-pull')['spec']['template']['spec']['containers'][0]
        self.assertEqual(guard['env'][0]['value'], 'kodex-image-registry-pull.kodex-system.svc.cluster.local/kodex/control-plane@sha256:' + 'c' * 64)
        script = registry.one(result, 'ConfigMap', 'kodex-release-artifact-materializer')
        self.assertIn('AGENT_RUNNER_DESTINATION_REPOSITORY', script['data']['release-artifact-materializer.sh'])
        self.assertEqual(registry.transform(result, config, lock), result)

    def test_preflight_rejects_forged_endpoint_owner_pod_or_address(self):
        config, lock, _ = fixture()
        service = {'metadata': {'uid': config['service_uid']}, 'spec': {'type': 'ClusterIP', 'selector': config['pod_labels'], 'ports': [{'port': 443, 'targetPort': 8443, 'protocol': 'TCP'}]}}
        slices = {'items': [{'metadata': {'ownerReferences': [{'kind': 'Service', 'uid': config['service_uid'], 'controller': True}]}, 'ports': [{'port': 8443, 'protocol': 'TCP'}], 'endpoints': [{'conditions': {'ready': True}, 'addresses': ['192.0.2.9'], 'targetRef': {'kind': 'Pod', 'namespace': config['namespace'], 'name': 'synthetic-registry-0', 'uid': 'synthetic-pod-uid'}}]}]}
        pod = {'metadata': {'uid': 'synthetic-pod-uid', 'labels': config['pod_labels']}, 'status': {'podIPs': [{'ip': '192.0.2.9'}]}}

        def run_case(endpoints, workload, accepted):

            def get(command, **kwargs):
                kind = command[command.index('get') + 1]
                value = {'service': service, 'configmap': {'data': {'ca.crt': '-----BEGIN CERTIFICATE-----synthetic'}}, 'endpointslices': endpoints, 'pod': workload}[kind]
                return subprocess.CompletedProcess(command, 0, json.dumps(value).encode())
            with patch.object(registry.subprocess, 'run', side_effect=get), patch('ssl.create_default_context'):
                if accepted:
                    self.assertEqual(registry.preflight(config, lock, 'synthetic-context')['workload_tls_and_auth'], 'NOT_RUN')
                else:
                    with self.assertRaises(ValueError):
                        registry.preflight(config, lock, 'synthetic-context')
        run_case(slices, pod, True)
        bad = copy.deepcopy(slices)
        bad['items'][0]['metadata']['ownerReferences'][0]['uid'] = 'different'
        run_case(bad, pod, False)
        bad = copy.deepcopy(pod)
        bad['metadata']['uid'] = 'replaced'
        run_case(slices, bad, False)
        bad = copy.deepcopy(pod)
        bad['metadata']['labels'] = {}
        run_case(slices, bad, False)
        bad = copy.deepcopy(slices)
        bad['items'][0]['endpoints'][0]['addresses'] = ['192.0.2.10']
        run_case(bad, pod, False)

    def test_overlay_is_idempotent_preserves_input_and_checks_tls_ca(self):
        config, lock, resources = fixture()
        before = copy.deepcopy(resources)
        result = registry.transform(resources, config, lock)
        self.assertEqual(resources, before)
        self.assertEqual(registry.transform(result, config, lock), result)
        job = next((r for r in result if r['kind'] == 'Job'))
        container = job['spec']['template']['spec']['containers'][0]
        self.assertIn({'name': 'SSL_CERT_FILE', 'value': '/var/run/config/kodex/release-source/ca.crt'}, container['env'])
        self.assertIn(lock['registry']['push'], next((v['value'] for v in container['env'] if v['name'] == 'NO_PROXY')).split(','))
        self.assertTrue(container['volumeMounts'][0]['readOnly'])
        policy = next((r for r in result if r['kind'] == 'NetworkPolicy'))
        self.assertEqual(policy['spec']['egress'][-1], {'to': [{'namespaceSelector': {'matchLabels': {'kubernetes.io/metadata.name': 'synthetic-infra'}}, 'podSelector': {'matchLabels': config['pod_labels']}}], 'ports': [{'protocol': 'TCP', 'port': 8443}]})

    def test_promoted_role_path_is_independent_of_source_and_policy_digest_matches(self):
        import json
        config, lock, resources = fixture()
        result = registry.transform(resources, config, lock)
        data = next((r['data'] for r in result if r['kind'] == 'ConfigMap' and r['metadata']['name'] == 'kodex-image-admission-policy'))
        self.assertEqual(data['promotedPullRepository'], 'kodex-image-registry-pull.kodex-system.svc.cluster.local/kodex/roles')
        expected_base = 'kodex-image-registry-pull.kodex-system.svc.cluster.local/kodex/agent-runner'
        self.assertEqual(data['trustedRoleBaseRepository'], expected_base)
        for name in ('control-plane', 'role-image-builder', 'kodex-buildkit'):
            annotations = registry.one(result, 'Deployment', name)['spec']['template']['metadata']['annotations']
            self.assertEqual(annotations['kodex.dev/trusted-role-base-repository'], expected_base)
            self.assertEqual(annotations['kodex.dev/trusted-role-base-digest'], 'sha256:' + 'b' * 64)
        payload = {k: v for k, v in data.items() if k not in ('orchestrationRevision', 'policySHA256')}
        self.assertEqual(data['policySHA256'], hashlib.sha256((json.dumps(payload, sort_keys=True, separators=(',', ':'), ensure_ascii=False) + '\n').encode()).hexdigest())
        self.assertEqual(next((r['spec'] for r in result if r['kind'] == 'ImageAdmissionPolicyParameters')), data)
        self.assertFalse(any((r['kind'] == 'IngressRouteTCP' for r in result)))
        service = next((r for r in result if r['kind'] == 'Service'))
        self.assertIn({'name': 'internal-https', 'port': 443, 'targetPort': 'registry', 'protocol': 'TCP'}, service['spec']['ports'])

    def test_foreign_trusted_base_or_consumer_digest_is_rejected(self):
        for target in ('policy', 'control-plane', 'role-image-builder', 'kodex-buildkit'):
            config, lock, resources = fixture()
            if target == 'policy':
                registry.one(resources, 'ConfigMap', 'kodex-image-admission-policy')['data']['trustedRoleBaseRepository'] = 'other.example.invalid/kodex/agent-runner'
            else:
                registry.one(resources, 'Deployment', target)['spec']['template']['metadata']['annotations']['kodex.dev/trusted-role-base-digest'] = 'sha256:' + 'f' * 64
            with self.subTest(target=target), self.assertRaises(ValueError):
                registry.transform(resources, config, lock)

    def test_unknown_fields_broad_selectors_and_source_mismatch_fail_closed(self):
        config, lock, resources = fixture()
        cases = []
        value = copy.deepcopy(config)
        value['password'] = 'synthetic-forbidden'
        cases.append(value)
        value = copy.deepcopy(config)
        value['namespace'] = '*'
        cases.append(value)
        value = copy.deepcopy(config)
        value['pod_labels'] = {}
        cases.append(value)
        value = copy.deepcopy(config)
        value['pod_labels'] = {'app': '*'}
        cases.append(value)
        value = copy.deepcopy(config)
        value['service'] = 'different'
        cases.append(value)
        for value in cases:
            with self.subTest(value=value), self.assertRaises(ValueError):
                registry.transform(resources, value, lock)

    def test_duplicate_owner_resource_and_conflicting_ca_binding_fail_closed(self):
        config, lock, resources = fixture()
        resources.append(copy.deepcopy(resources[0]))
        with self.assertRaises(ValueError):
            registry.transform(resources, config, lock)
        config, lock, resources = fixture()
        resources[0]['spec']['template']['spec']['containers'][0]['env'].append({'name': 'SSL_CERT_FILE', 'value': '/different/ca.crt'})
        with self.assertRaises(ValueError):
            registry.transform(resources, config, lock)

    def test_complete_portable_render_and_optional_source_projection(self):
        root = Path(__file__).resolve().parents[2]
        config, lock, _ = fixture()
        host = lock['registry']['push']
        inventory = json.loads((root / 'tools/release/images.json').read_text())['images']
        lock.update(schema_version=2, profile='web-only', source_sha='b' * 40, build_run_id='local', images=[dict(component=item['component'], repository='synthetic/' + item['component'], digest='sha256:' + 'c' * 64, pull_ref=host + '/synthetic/' + item['component'] + '@sha256:' + 'c' * 64) for item in inventory], external_images=[dict(component='admission-tools', digest='sha256:' + 'd' * 64, pull_ref='tools.example.invalid/admission@sha256:' + 'd' * 64)], role_image_input=dict(repository='synthetic/role-image-inputs', manifest_digest='sha256:' + 'e' * 64, payload_sha256='f' * 64, source_sha256='a' * 64, pull_ref=host + '/synthetic/role-image-inputs@sha256:' + 'e' * 64))
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            source = directory / 'lock.json'
            render = directory / 'render.yaml'
            source.write_text(json.dumps(lock))
            command = [str(root / 'tools/release/render-web-only.sh'), '--lock', str(source), '--lock-sha256', hashlib.sha256(source.read_bytes()).hexdigest(), '--output', str(render), '--public-host', 'console.example.invalid', '--public-origin', 'https://console.example.invalid', '--oidc-issuer', 'https://sso.example.invalid/realms/test', '--oidc-jwks-url', 'https://sso.example.invalid/realms/test/protocol/openid-connect/certs', '--oidc-connect-address', 'keycloak.kodex-identity.svc:8443', '--oidc-tls-server-name', 'sso.example.invalid', '--promoted-pull-host', 'kodex-image-registry-pull.kodex-system.svc.cluster.local', '--kubernetes-api-service-cidr', '192.0.2.10/32', '--kubernetes-api-endpoint-cidrs', '192.0.2.11/32', '--kubernetes-api-endpoint-ports', '6443', '--ingress-class', 'nginx', '--cluster-issuer', 'test-issuer', '--ingress-namespace', 'ingress-nginx', '--ingress-pod-name', 'ingress-nginx', '--oidc-namespace', 'kodex-identity', '--oidc-pod-name', 'keycloak', '--oidc-pod-component', 'identity', '--oidc-target-port', '8443', '--disable-observability']
            result = subprocess.run(command, cwd=root, capture_output=True, timeout=150, env={**os.environ, 'GOMAXPROCS': '2', 'GOFLAGS': '-p=1', 'GOWORK': 'off'})
            self.assertEqual(result.returncode, 0, (result.stdout + result.stderr).decode()[-3000:])
            resources = [item for item in registry.yaml.safe_load_all(render.read_text()) if item]
            self.assertTrue(any(item['apiVersion'] == 'monitoring.coreos.com/v1' for item in resources))
            for name in ['control-plane-exact-runtime-paths','secret-broker-exact-runtime-paths']:
                policy=registry.one(resources,'NetworkPolicy',name)
                self.assertIn({'to':[{'ipBlock':{'cidr':'192.0.2.11/32'}}],'ports':[{'protocol':'TCP','port':6443}]},policy['spec']['egress'])
            control=registry.one(resources,'Deployment','control-plane')['spec']['template']['spec']['containers'][0]
            self.assertIn({'name':'OTEL_SDK_DISABLED','value':'true'},control['env'])
            self.assertEqual(registry.yaml.safe_load(registry.one(resources,'ConfigMap','internal-rpc-authority-publisher-target-registry')['data']['key-delivery-targets.yaml'])['source_revision'],7)
            publisher=registry.one(resources,'NetworkPolicy','internal-rpc-authority-publisher-exact-paths')
            self.assertIn({'to':[{'ipBlock':{'cidr':'192.0.2.10/32'}}],'ports':[{'protocol':'TCP','port':443}]},publisher['spec']['egress'])
            self.assertTrue(any(c['name']=='skill-scanner'for c in registry.one(resources,'Deployment','control-plane')['spec']['template']['spec']['containers']))
            self.assertFalse(registry.one(resources,'CronJob','clamav-db-updater')['spec'].get('suspend',False))
            disabled=directory/'disabled.yaml'
            disabled_command=[str(disabled)if arg==str(render)else arg for arg in command]+['--clamav-mode','disabled','--artifact-antivirus-mode','disabled']
            disabled_result=subprocess.run(disabled_command,cwd=root,capture_output=True,timeout=150,env={**os.environ,'GOMAXPROCS':'2','GOFLAGS':'-p=1','GOWORK':'off'})
            self.assertEqual(disabled_result.returncode,0,(disabled_result.stdout+disabled_result.stderr).decode()[-1000:])
            disabled_resources=[item for item in registry.yaml.safe_load_all(disabled.read_text())if item]
            disabled_cp=registry.one(disabled_resources,'Deployment','control-plane')
            self.assertFalse(any(c['name']=='skill-scanner'for c in disabled_cp['spec']['template']['spec']['containers']))
            self.assertEqual(disabled_cp['metadata']['annotations']['kodex.dev/clamav-mode'],'disabled')
            self.assertIn({'name':'CONTROL_PLANE_ARTIFACT_ANTIVIRUS_MODE','value':'disabled'}, next(c for c in disabled_cp['spec']['template']['spec']['containers'] if c['name']=='control-plane')['env'])
            self.assertTrue(registry.one(disabled_resources,'CronJob','clamav-db-updater')['spec']['suspend'])
            self.assertEqual(registry.one(disabled_resources,'Deployment','clamav-egress-gateway')['spec']['replicas'],0)
            self.assertFalse(any(item['kind']=='PersistentVolumeClaim'and item['metadata']['name']=='clamav-database'for item in disabled_resources))
            projected = registry.transform(resources, config, lock)
            self.assertEqual(registry.transform(projected, config, lock), projected)
            self.assertTrue(any((item['kind'] == 'Deployment' and item['metadata']['name'] == 'control-plane' for item in projected)))
            data = registry.one(projected, 'ConfigMap', 'kodex-image-admission-policy')['data']
            self.assertEqual(data['promotedPullRepository'], 'kodex-image-registry-pull.kodex-system.svc.cluster.local/kodex/roles')
            self.assertFalse(any((item['kind'] == 'IngressRouteTCP' and item['metadata']['name'] == 'kodex-image-registry-pull' for item in projected)))
if __name__ == '__main__':
    unittest.main()
