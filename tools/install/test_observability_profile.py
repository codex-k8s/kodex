"""Synthetic профиль: operator optional, security и исполняемые спецификации неизменны."""
import copy
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

    def test_actual_wrapper_preserves_job_security_and_pvc_and_places_sdk_in_contract_order(self):
        issuer = dict(name='internal-rpc-authority-issuer', restartPolicy='Always', env=[dict(name='DEPLOYMENT_ENVIRONMENT', value='synthetic'), dict(name='EXACT_AUTHORITY', value='required')], securityContext=dict(readOnlyRootFilesystem=True))
        job = dict(apiVersion='batch/v1', kind='Job', metadata=dict(name='synthetic'), spec=dict(template=dict(spec=dict(hostUsers=False, containers=[dict(name='admission')], initContainers=[issuer]))))
        pvc = dict(apiVersion='v1', kind='PersistentVolumeClaim', metadata=dict(name='synthetic'), spec=dict(accessModes=['ReadWriteOnce']))
        text = Path(__file__).with_name('render-image-admission-no-observability.sh').read_text()
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            original = directory/'original.sh'
            fixture_file = directory/'fixture.yaml'
            original.write_text('#!/usr/bin/env bash\ncat "'+str(fixture_file)+'"\n'); original.chmod(0o700)
            wrapper = directory/'wrapper.sh';wrapper.write_text(text.replace(profile.ORIGINAL_RENDERER, str(original)))
            for documents in [[job, pvc], [pvc]]:
                fixture_file.write_text(yaml.safe_dump_all(documents))
                response = subprocess.run(['bash', str(wrapper)], capture_output=True, timeout=10, check=True)
                actual = list(yaml.safe_load_all(response.stdout))
                expected = copy.deepcopy(documents)
                if len(expected) == 2:
                    expected[0]['spec']['template']['spec']['initContainers'][0]['env'].insert(1, dict(name='OTEL_SDK_DISABLED', value='true'))
                self.assertEqual(actual, expected)

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
