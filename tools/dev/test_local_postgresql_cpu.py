"""Герметичный CPU-only переход; kubectl никогда не запускается живым."""

import copy
import importlib.util
import json
from pathlib import Path
import types
import unittest
from unittest.mock import patch

SOURCE = Path(__file__).with_name('local-postgresql-cpu.py')
SPEC = importlib.util.spec_from_file_location('postgresql_cpu', SOURCE)
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)
UID = '11111111-1111-4111-8111-111111111111'
PUID = '22222222-2222-4222-8222-222222222222'
IMAGE = 'docker.io/library/postgres:18.3-alpine3.23@sha256:' + 'a' * 64


def fixture(cpu='2'):
    resources = copy.deepcopy(M.RESOURCES)
    resources['limits']['cpu'] = cpu
    container = {'name': 'postgresql', 'image': IMAGE, 'resources': resources,
                 'env': [{'name': 'PASSWORD_FILE', 'value': '/private/not-a-value'}],
                 'volumeMounts': [{'name': 'data', 'mountPath': '/var/lib/postgresql/data'}]}
    sts = {'kind': 'StatefulSet', 'metadata': {'name': M.NAME, 'namespace': M.NAMESPACE,
           'uid': UID, 'resourceVersion': '10', 'generation': 1,
           'labels': {M.PROFILE_LABEL: M.PROFILE}}, 'spec': {'replicas': 1,
           'serviceName': M.NAME, 'selector': {'matchLabels': {'app.kubernetes.io/name': M.NAME}},
           'template': {'metadata': {'labels': {M.PROFILE_LABEL: M.PROFILE}},
                        'spec': {'containers': [container], 'volumes': [{'name': 'tls', 'secret': {'secretName': 'keep'}}]}},
           'volumeClaimTemplates': [{'metadata': {'name': 'data'}, 'spec': {'accessModes': ['ReadWriteOnce']}}]},
           'status': {'readyReplicas': 1, 'observedGeneration': 1, 'currentRevision': 'rev1', 'updateRevision': 'rev1'}}
    pod = {'kind': 'Pod', 'metadata': {'name': M.POD, 'namespace': M.NAMESPACE, 'uid': PUID,
           'labels': {M.PROFILE_LABEL: M.PROFILE}, 'ownerReferences': [{'kind': 'StatefulSet',
           'name': M.NAME, 'uid': UID, 'controller': True}]},
           'spec': {'containers': [copy.deepcopy(container)],
                    'volumes': [{'name': 'data', 'persistentVolumeClaim': {'claimName': M.PVC}}]},
           'status': {'phase': 'Running', 'containerStatuses': [{'name': 'postgresql', 'ready': True,
                                                                 'state': {'running': {}}}]}}
    pvc = {'kind': 'PersistentVolumeClaim', 'metadata': {'name': M.PVC, 'namespace': M.NAMESPACE,
           'uid': '33333333-3333-4333-8333-333333333333'},
           'spec': {'volumeName': 'preserve-volume', 'resources': {'requests': {'storage': '32Gi'}}},
           'status': {'phase': 'Bound'}}
    return sts, pod, pvc


def args(value, mode='apply'):
    pins = M.validate(*value)
    return types.SimpleNamespace(mode=mode, confirm_postgresql_restart=True,
                                 **{'expected_' + k: v for k, v in pins.items() if k != 'cpu_limit'})


class Client:
    def __init__(self, value):
        self.value = copy.deepcopy(value)
        self.commands = []
        self.reads = 0
        self.mutate_on_read = None

    def context(self):
        M.validate_context('k3d-kodex|k3d-kodex|https://127.0.0.2:6443')

    def read(self):
        self.reads += 1
        if self.mutate_on_read:
            self.mutate_on_read(self)
        return copy.deepcopy(self.value)

    def command(self, command, input_bytes=None, timeout=20):
        self.commands.append((command, input_bytes, timeout))
        if command[0] == 'patch':
            candidate = json.loads(input_bytes)
            Test.test_patch_cas_and_sole_effect(None, candidate, self.value[0])
            for obj in self.value[:2]:
                target = obj['spec']['template']['spec'] if obj['kind'] == 'StatefulSet' else obj['spec']
                target['containers'][0]['resources']['limits']['cpu'] = '4'
            self.value[1]['metadata']['uid'] = '44444444-4444-4444-8444-444444444444'
        return b''


class Test(unittest.TestCase):
    def test_patch_cas_and_sole_effect(self, candidate=None, sts=None):
        sts = sts or fixture()[0]
        candidate = candidate or M.patch(sts)
        assert candidate[:3] == [{'op': 'test', 'path': '/metadata/uid', 'value': UID},
                                {'op': 'test', 'path': '/metadata/resourceVersion', 'value': '10'},
                                {'op': 'test', 'path': '/spec', 'value': sts['spec']}]
        assert candidate[3:] == [{'op': 'replace', 'path': '/spec/template/spec/containers/0/resources/limits/cpu', 'value': '4'}]

    def test_apply_rollout_cpu_only_preserves_full_spec_storage_and_image(self):
        value = fixture()
        client = Client(value)
        result = M.execute(client, args(value))
        expected = copy.deepcopy(value[0]['spec'])
        expected['template']['spec']['containers'][0]['resources']['limits']['cpu'] = '4'
        self.assertEqual(client.value[0]['spec'], expected)
        self.assertEqual(client.value[2], value[2])
        self.assertEqual(result['storage_sha256'], M.validate(*value)['storage_sha256'])
        self.assertEqual(result['image'], IMAGE)
        self.assertTrue(result['changed'])
        self.assertNotEqual(result['pod_uid'], PUID)
        self.assertEqual([c[0][0] for c in client.commands], ['patch', 'rollout'])

    def test_preflight_no_effect(self):
        client = Client(fixture())
        self.assertEqual(M.execute(client, args(client.value, 'preflight'))['status'], 'PREFLIGHT')
        self.assertEqual(client.commands, [])

    def test_idempotent_four_no_patch_or_restart(self):
        client = Client(fixture('4'))
        self.assertFalse(M.execute(client, args(client.value))['changed'])
        self.assertEqual(client.commands, [])

    def test_readback_four_without_effect(self):
        client = Client(fixture('4'))
        self.assertEqual(M.execute(client, args(client.value, 'readback'))['status'], 'READY')
        self.assertEqual(client.commands, [])

    def test_readback_old_cpu_denied(self):
        client = Client(fixture())
        with self.assertRaisesRegex(M.Failure, 'CPU_NOT_APPLIED'):
            M.execute(client, args(client.value, 'readback'))
        self.assertEqual(client.commands, [])

    def test_confirmation_required_before_effect(self):
        client = Client(fixture()); a = args(client.value); a.confirm_postgresql_restart = False
        with self.assertRaisesRegex(M.Failure, 'RESTART_CONFIRMATION_REQUIRED'):
            M.execute(client, a)
        self.assertEqual(client.commands, [])

    def test_all_expected_pins_fail_closed(self):
        for key in ['statefulset_uid', 'pod_uid', 'image', 'spec_sha256', 'storage_sha256']:
            with self.subTest(pin=key):
                client = Client(fixture()); a = args(client.value); setattr(a, 'expected_' + key, 'wrong')
                with self.assertRaisesRegex(M.Failure, 'EXPECTED_PIN_MISMATCH'):
                    M.execute(client, a)
                self.assertEqual(client.commands, [])

    def test_uid_and_storage_races_fail_before_effect(self):
        for change in ['pod', 'pvc', 'version']:
            client = Client(fixture()); a = args(client.value)
            def mutate(c):
                if c.reads != 2: return
                if change == 'version': c.value[0]['metadata']['resourceVersion'] = '11'
                else: c.value[1 if change == 'pod' else 2]['metadata']['uid'] = '44444444-4444-4444-8444-444444444444'
            client.mutate_on_read = mutate
            with self.assertRaisesRegex(M.Failure, 'PRECONDITION_CHANGED'):
                M.execute(client, a)
            self.assertEqual(client.commands, [])

    def test_rollout_failure_never_auto_rollback_or_success(self):
        client = Client(fixture()); original = client.command
        def command(command, input_bytes=None, timeout=20):
            if command[0] == 'rollout': raise M.Failure('COMMAND_FAILED')
            return original(command, input_bytes, timeout)
        client.command = command
        with self.assertRaisesRegex(M.Failure, 'COMMAND_FAILED'):
            M.execute(client, args(client.value))
        self.assertEqual(len(client.commands), 1)

    def test_after_spec_or_pvc_change_denied(self):
        for change in ['memory', 'pvc']:
            client = Client(fixture()); a = args(client.value)
            def mutate(c):
                if c.reads != 3: return
                if change == 'memory':
                    for obj in c.value[:2]:
                        spec = obj['spec']['template']['spec'] if obj['kind'] == 'StatefulSet' else obj['spec']
                        spec['containers'][0]['args'] = ['unexpected']
                else:c.value[2]['spec']['resources']['requests']['storage'] = '64Gi'
            client.mutate_on_read = mutate
            with self.assertRaisesRegex(M.Failure, 'AFTER_BINDING_INVALID'):
                M.execute(client, a)

    def test_foreign_destination_profile_image_resources_or_storage_rejected(self):
        mutations = [lambda v:v[0]['metadata'].update(namespace='foreign'),
                     lambda v:v[0]['metadata']['labels'].update({M.PROFILE_LABEL: 'protected'}),
                     lambda v:v[0]['spec'].update(replicas=2),
                     lambda v:v[1]['metadata']['ownerReferences'][0].update(uid=PUID),
                     lambda v:v[1]['metadata'].update(deletionTimestamp='now'),
                     lambda v:v[1]['spec']['containers'][0].update(image='foreign'),
                     lambda v:v[1]['spec']['volumes'][0]['persistentVolumeClaim'].update(claimName='foreign'),
                     lambda v:v[0]['spec']['template']['spec']['containers'][0]['resources']['limits'].update(memory='8Gi'),
                     lambda v:v[0]['spec']['template']['spec']['containers'][0]['resources']['limits'].update(cpu='3'),
                     lambda v:v[2]['status'].update(phase='Pending'),
                     lambda v:v[1]['status']['containerStatuses'][0].update(ready=False)]
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                value = fixture(); mutate(value)
                with self.assertRaises(M.Failure): M.validate(*value)

    def test_context_endpoint_exact_and_no_credentials(self):
        M.validate_context('k3d-kodex|k3d-kodex|https://127.0.0.2:6443')
        for value in ['foreign|k3d-kodex|https://127.0.0.2:6443',
                      'k3d-kodex|foreign|https://127.0.0.2:6443',
                      'k3d-kodex|k3d-kodex|http://127.0.0.2:6443',
                      'k3d-kodex|k3d-kodex|https://example.com:6443',
                      'k3d-kodex|k3d-kodex|https://127.0.0.1:6443',
                      'k3d-kodex|k3d-kodex|https://127.0.0.2:6444',
                      'k3d-kodex|k3d-kodex|https://127.0.0.2:443',
                      'k3d-kodex|k3d-kodex|https://[::1]:6443',
                      'k3d-kodex|k3d-kodex|https://user:privacy-sentinel@127.0.0.2:6443',
                      'k3d-kodex|k3d-kodex|https://127.0.0.2:6443/?secret=privacy-sentinel',
                      'k3d-kodex|k3d-kodex|https://127.0.0.2:6443/',
                      'k3d-kodex|k3d-kodex|https://127.0.0.2:6443#privacy-sentinel']:
            with self.assertRaises(M.Failure) as caught: M.validate_context(value)
            self.assertNotIn('privacy-sentinel', str(caught.exception))

    def test_unknown_generation_revision_or_mixed_container_state_denied(self):
        mutations = [lambda v:v[0]['metadata'].pop('generation'),
                     lambda v:v[0]['status'].pop('currentRevision'),
                     lambda v:v[0]['status'].update(observedGeneration=0),
                     lambda v:v[0]['spec'].update(persistentVolumeClaimRetentionPolicy={'whenScaled': 'Delete'}),
                     lambda v:v[1]['status']['containerStatuses'][0]['state'].update(terminated={})]
        for mutate in mutations:
            value = fixture(); mutate(value)
            with self.assertRaises(M.Failure): M.validate(*value)

    def test_post_patch_precondition_rejection_no_rollout_or_second_effect(self):
        client = Client(fixture())
        def deny(command, input_bytes=None, timeout=20):
            client.commands.append((command, input_bytes, timeout))
            raise M.Failure('COMMAND_FAILED')
        client.command = deny
        with self.assertRaisesRegex(M.Failure, 'COMMAND_FAILED'):
            M.execute(client, args(client.value))
        self.assertEqual([c[0][0] for c in client.commands], ['patch'])

    def test_closed_cli_invalid_and_no_arbitrary_target(self):
        for argv in [['--mode', 'foreign'], ['--mode', 'preflight', '--namespace', 'privacy-sentinel'],
                     ['--mode', 'apply'], ['--mode', 'preflight', '--expected-image', 'privacy-sentinel']]:
            with self.assertRaises(M.Failure) as caught: M.arguments(argv)
            self.assertEqual(str(caught.exception), 'ARGUMENT_INVALID')

    def test_subprocess_safe_destination_stdin_and_redaction(self):
        complete = types.SimpleNamespace(returncode=0, stdout=b'ok', stderr=b'privacy-sentinel')
        with patch.object(M.subprocess, 'run', return_value=complete) as run:
            client = M.Kubectl(Path('/owned/cache'))
            client.command(['patch', 'statefulset', M.NAME, '--type=json', '--patch-file=/dev/stdin'], b'private-spec')
            positional, keyword = run.call_args
            self.assertIn('--context=k3d-kodex', positional[0])
            self.assertIn('--cache-dir=/owned/cache', positional[0])
            self.assertNotIn('private-spec', ' '.join(positional[0]))
            self.assertEqual(keyword['input'], b'private-spec')
            self.assertEqual(keyword['env']['HOME'], '/home/s')
        with patch.object(M.subprocess, 'run', return_value=types.SimpleNamespace(returncode=1, stdout=b'', stderr=b'privacy-sentinel')):
            with self.assertRaisesRegex(M.Failure, '^COMMAND_FAILED$'): client.command(['get'])

    def test_canonical_manifest_only_cpu_limit_four(self):
        source = SOURCE.parents[2] / 'deploy/k8s/base/platform-state/postgresql.yaml'
        text = source.read_text()
        self.assertIn('requests:\n              cpu: 250m\n              memory: 512Mi\n            limits:\n              cpu: "4"\n              memory: 4Gi', text)


if __name__ == '__main__':
    unittest.main()
