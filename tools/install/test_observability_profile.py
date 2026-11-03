"""Synthetic профиль: operator optional, security и исполняемые спецификации неизменны."""
import copy
import json
import shutil
from pathlib import Path
import subprocess
import tempfile
import unittest
import yaml
import observability_profile as profile


def fixture():
    deployment = dict(apiVersion='apps/v1', kind='Deployment', metadata=dict(name='synthetic'), spec=dict(template=dict(spec=dict(
        hostUsers=False, containers=[dict(name='worker', image='images.example.invalid/worker@sha256:'+'a'*64,
        env=[dict(name='APP_REQUIRED', value='true')], securityContext=dict(readOnlyRootFilesystem=True), readinessProbe=dict(tcpSocket=dict(port=8443)))]))))
    monitors = [dict(apiVersion=profile.MONITORING_API, kind=kind, metadata=dict(name='synthetic')) for kind in profile.MONITORING_KINDS]
    policy = dict(apiVersion='networking.k8s.io/v1', kind='NetworkPolicy', metadata=dict(name='synthetic'), spec=dict(podSelector={},policyTypes=['Ingress','Egress'], ingress=[], egress=[]))
    return [deployment, policy, *monitors]


def renderer_fixture():
    return dict(apiVersion='v1', kind='ConfigMap', immutable=True,
        metadata=dict(name='synthetic-intent', namespace='kodex-system', labels={'kodex.dev/owner-intent': 'true'},
            annotations={'kodex.dev/admission-tools-sha256': 'sha256:'+'e'*64}),
        data=dict(toolsImage='images.example.invalid/kodex/tools@sha256:'+'e'*64,
            admissionImage='images.example.invalid/kodex/admission@sha256:'+'f'*64,
            authorityImage='images.example.invalid/kodex/authority@sha256:'+'b'*64,
            authorityIssuerImage='images.example.invalid/kodex/issuer@sha256:'+'c'*64,
            promotionRepository='kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003/kodex/roles',
            promotionEvidenceRepository='kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003/kodex/evidence',
            evidenceRepository='kodex-image-registry-evidence.kodex-system.svc.cluster.local:5007/evidence/role-image-admission',
            promotedPullRepository='images.example.invalid/kodex/roles', policyRevision='7', policySHA256='c'*64,
            requiredTools='base64,cmp,cosign,grype,image-admission-bridge,image-tool-inventory-validator,image-vulnerability-report-validator,jq,regctl,sha256sum,syft,tr,wc',
            builderIdentity='spiffe://kodex.local/ns/kodex-system/sa/role-image-builder',
            buildType='https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md',
            trustedRoleBaseRepository='images.example.invalid/kodex/agent-runner', trustedRoleBaseDigest='sha256:'+'a'*64,
            roleRuntimeContractRevision='1', roleRuntimeContractSHA256='d'*64))


class OptionalObservability(unittest.TestCase):
    def test_native_sidecars_receive_profile_but_completed_init_template_is_preserved(self):
        for kind in ['Deployment', 'StatefulSet', 'DaemonSet', 'Job', 'CronJob', 'Pod']:
            pod = dict(containers=[dict(name='app')], initContainers=[dict(name='socket-init'), dict(name='issuer', restartPolicy='Always')])
            spec = pod if kind == 'Pod' else dict(jobTemplate=dict(spec=dict(template=dict(spec=pod)))) if kind == 'CronJob' else dict(template=dict(spec=pod))
            value = dict(kind=kind, metadata=dict(name='synthetic'), spec=spec)
            actual = profile.transform([value], True)[0]
            result = actual['spec'] if kind == 'Pod' else actual['spec']['jobTemplate']['spec']['template']['spec'] if kind == 'CronJob' else actual['spec']['template']['spec']
            self.assertEqual(result['initContainers'][0], pod['initContainers'][0])
            self.assertEqual(result['initContainers'][1]['env'], [dict(name='OTEL_SDK_DISABLED', value='true')])
            self.assertEqual(result['initContainers'][1]['restartPolicy'], 'Always')

    def test_managed_renderer_and_parameter_owned_policy_change_together(self):
        root = Path(__file__).resolve().parents[2]
        policy = list(yaml.safe_load_all((root/'deploy/k8s/base/image-supply-chain/image-admission-controller-policy.yaml').read_text()))[0]
        controller = dict(apiVersion='apps/v1', kind='Deployment', metadata=dict(name=profile.CONTROLLER, namespace='kodex-system'), spec=dict(template=dict(spec=dict(containers=[dict(name=profile.CONTROLLER)]))))
        parameter = dict(apiVersion='supplychain.kodex.dev/v1alpha1', kind='ImageAdmissionPolicyParameters', metadata=dict(name=profile.PARAMETERS, namespace='kodex-system'), spec=dict(synthetic=True))
        resources = [controller, policy, parameter]
        self.assertEqual(profile.transform(resources), resources)
        actual = profile.transform(resources, True)
        self.assertEqual(profile.transform(actual, True), actual)
        self.assertTrue(actual[-1]['immutable'])
        self.assertEqual(actual[-1]['metadata']['name'], actual[0]['spec']['template']['spec']['volumes'][0]['configMap']['name'])
        self.assertEqual(actual[2]['spec'], parameter['spec'])
        self.assertEqual(actual[2]['metadata']['labels'][profile.PROFILE_LABEL], 'disabled')
        self.assertEqual(actual[1]['spec']['failurePolicy'], 'Fail')
        self.assertEqual(actual[1]['spec']['matchConstraints'], policy['spec']['matchConstraints'])
        self.assertEqual(actual[1]['spec']['matchConditions'], policy['spec']['matchConditions'])
        changed = [(before, after) for before, after in zip(policy['spec']['validations'], actual[1]['spec']['validations']) if before != after]
        self.assertEqual(len(changed), 1)
        self.assertEqual(actual[1]['spec']['variables'][:-1], policy['spec']['variables'])
        self.assertIn('params.metadata.labels', actual[1]['spec']['variables'][-1]['expression'])
        self.assertNotIn('object.metadata.labels', actual[1]['spec']['variables'][-1]['expression'])
        for mutate in ['policy', 'parameter', 'custom-renderer', 'volume']:
            bad = copy.deepcopy(resources)
            if mutate == 'policy': bad.pop(1)
            elif mutate == 'parameter': bad.pop(2)
            elif mutate == 'volume': bad[0]['spec']['template']['spec']['volumes'] = [dict(name='disabled-observability-renderer', emptyDir={})]
            else: bad[0]['spec']['template']['spec']['containers'][0]['env'] = [dict(name='IMAGE_ADMISSION_CONTROLLER_RENDERER_PATH', value='/custom/renderer.sh')]
            with self.subTest(mutate=mutate), self.assertRaises(ValueError): profile.transform(bad, True)

    def test_real_canonical_renderer_with_baked_dependency_set_preserves_everything_except_sdk(self):
        original_source = Path(__file__).resolve().parents[1]/'render-image-admission-job.sh'
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name); original = directory/'original.sh';original.write_bytes(original_source.read_bytes())
            binary = directory/'bin';binary.mkdir()
            # Точный runtime contract: yq/python/kubectl недоступны, HTTP вызов невозможен.
            for command in ['bash', 'cat', 'jq', 'awk', 'sha256sum']:
                (binary/command).symlink_to(shutil.which(command))
            wrapper = directory/'wrapper.sh';wrapper.write_text(profile.managed_renderer_script().replace(profile.ORIGINAL_RENDERER, str(original)))
            for security in ['protected', 'trusted-cluster']:
                intent = renderer_fixture();intent['metadata']['labels']['kodex.dev/security-profile'] = security
                environment = dict(PATH=str(binary), IMAGE_ADMISSION_POLICY_JSON=json.dumps(intent))
                for phase in ['claim', 'scan', 'sign', 'admit', 'promote']:
                    arguments = ['staging', 'v20260101000000-'+'a'*40, phase]
                    before = subprocess.run(['/bin/bash', str(original), *arguments], env=environment, capture_output=True, timeout=10, check=True)
                    response = subprocess.run(['/bin/bash', str(wrapper), *arguments], env=environment, capture_output=True, timeout=10, check=True)
                    expected = list(yaml.safe_load_all(before.stdout))
                    for resource in expected:
                        if resource and resource.get('kind') == 'Job':
                            for container in resource['spec']['template']['spec'].get('initContainers', []):
                                if container['name'] == 'internal-rpc-authority-issuer':
                                    container['env'].insert(1, dict(name='OTEL_SDK_DISABLED', value='true'))
                    self.assertEqual(list(yaml.safe_load_all(response.stdout)), expected)
            original.write_text(original.read_text()+'\n# synthetic changed source\n')
            rejected = subprocess.run(['/bin/bash', str(wrapper), 'staging', 'v20260101000000-'+'a'*40, 'claim'], env=environment, capture_output=True, timeout=10)
            self.assertEqual(rejected.returncode, 78);self.assertEqual(rejected.stdout, b'')

    def test_renderer_source_drift_is_rejected_instead_of_guessing_a_patch(self):
        source = (Path(__file__).resolve().parents[1]/'render-image-admission-job.sh').read_bytes()
        for raw in [source.replace(profile.SDK_BLOCK.encode(), b''), source+profile.SDK_BLOCK.encode()]:
            with self.assertRaises(ValueError):profile.managed_renderer_script(raw)

    def test_default_preserves_full_profile_without_mutating_input(self):
        resources=fixture();before=copy.deepcopy(resources)
        self.assertEqual(profile.transform(resources),before)
        self.assertEqual(resources,before)

    def test_disabled_removes_only_operator_contract_and_sets_otel_idempotently(self):
        resources=fixture();before=copy.deepcopy(resources);result=profile.transform(resources,True)
        self.assertEqual(len(result),2);self.assertEqual(result[1],before[1])
        expected=before[0];expected['spec']['template']['spec']['containers'][0]['env'].append(dict(name='OTEL_SDK_DISABLED',value='true'))
        self.assertEqual(result[0],expected);self.assertEqual(profile.transform(result,True),result)
        self.assertEqual(resources,fixture())

    def test_unknown_operator_workload_is_not_silently_removed(self):
        with self.assertRaises(ValueError):
            profile.transform([dict(apiVersion=profile.MONITORING_API,kind='Prometheus',metadata=dict(name='synthetic'))],True)


if __name__=='__main__':unittest.main()
