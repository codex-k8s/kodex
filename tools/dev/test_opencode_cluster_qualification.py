"""Отрицательные проверки target/UID и запрета очистки соседнего объекта."""
import importlib.util
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('native_qualification', Path(__file__).with_name('opencode-cluster-qualification.py'))
native = importlib.util.module_from_spec(spec)
spec.loader.exec_module(native)


class ClientFixture:
    def __init__(self, identity='owned', scope='fixture', objects=None):
        self.identity, self.scope = identity, scope
        self.objects = objects or {}

    def get(self, *_):
        return {'metadata': {'uid': self.identity, 'labels': {native.LABEL: self.scope}}}

    def call(self, arguments):
        if arguments[0] == 'api-resources':
            return '\n'.join(self.objects).encode()
        if arguments[0] == 'get':
            return json.dumps({'items': [{'metadata': {'name': 'worker-a', 'uid': self.identity},
                'status': {'nodeInfo': {'architecture': 'amd64'},
                           'conditions': [{'type': 'Ready', 'status': 'True'}]}}]}).encode()
        return self.objects[arguments[3]].encode()


class NativeQualificationSafetyTests(unittest.TestCase):
    def test_replaced_uid_and_foreign_scope_never_authorize_cleanup(self):
        record = {'kind': 'Pod', 'name': 'native', 'namespace': 'fixture', 'uid': 'owned'}
        for client in (ClientFixture(identity='replacement'), ClientFixture(scope='foreign')):
            with self.assertRaises(ValueError):
                native.verify_owned(client, {'scope': 'fixture'}, record)
        native.verify_owned(ClientFixture(), {'scope': 'fixture'}, record)

    def test_target_cluster_and_node_incarnations_are_both_required(self):
        plan = {'clusterUID': 'owned', 'nodes': {'worker-a': 'owned'}}
        native.verify_target(ClientFixture(), plan)
        for changed in ({**plan, 'clusterUID': 'other'}, {**plan, 'nodes': {'worker-a': 'replacement'}}):
            with self.assertRaises(ValueError):
                native.verify_target(ClientFixture(), changed)

    def test_context_names_do_not_grant_authority(self):
        for name in ('k3d-kodex', 'test-cluster', 'team/cluster@region'):
            self.assertEqual(native.target_context(name), name)
        for name in ('', ' leading', 'trailing ', '--other-context', 'two\nlines', 'two\x00parts', 'x' * 1025):
            with self.subTest(name=repr(name)), self.assertRaises(ValueError):
                native.target_context(name)

    def test_pinned_runner_repository_is_explicit_and_portable(self):
        digest = 'a' * 64
        for repository in ('registry.dev.example.invalid/kodex/agent-runner',
                           'registry.example.invalid/team/agent-runner',
                           'localhost:5000/team/tools__native--runner'):
            value = repository + '@sha256:' + digest
            self.assertEqual(native.runner_reference(value), value)
        for value in ('registry.example.invalid/team/runner:latest',
                      'https://registry.example.invalid/team/runner@sha256:' + digest,
                      'user:password@registry.example.invalid/team/runner@sha256:' + digest,
                      'registry.example.invalid:0/team/runner@sha256:' + digest,
                      'registry.example.invalid:65536/team/runner@sha256:' + digest,
                      'registry.example.invalid/team/../runner@sha256:' + digest,
                      'registry.example.invalid/team/runner@sha256:' + 'A' * 64,
                      'registry.example.invalid/team/runner@sha256:' + 'a' * 63,
                      'registry.example.invalid/team/runner@sha256:' + digest + '\n'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                native.runner_reference(value)

    def test_node_names_are_portable_but_readiness_and_unique_uids_are_required(self):
        node = {'metadata': {'name': 'worker-a', 'uid': 'node-a'},
                'status': {'nodeInfo': {'architecture': 'amd64'},
                           'conditions': [{'type': 'Ready', 'status': 'True'}]}}
        self.assertEqual(native.node_identities([node]), {'worker-a': 'node-a'})
        for changed in ([], [node, node],
                        [{**node, 'metadata': {**node['metadata'], 'deletionTimestamp': 'now'}}],
                        [{**node, 'status': {**node['status'], 'conditions': []}}],
                        [{**node, 'status': {**node['status'], 'conditions': [{'type': 'Ready', 'status': 'Unknown'}]}}],
                        [{**node, 'status': {**node['status'], 'nodeInfo': {'architecture': 'arm64'}}}]):
            with self.subTest(nodes=changed), self.assertRaises(ValueError):
                native.node_identities(changed)
        replacement = {**node, 'metadata': {'name': 'worker-a', 'uid': 'replacement'}}
        client = ClientFixture()
        with patch.object(client, 'call', return_value=json.dumps({'items': [replacement]}).encode()):
            with self.assertRaises(ValueError):
                native.verify_target(client, {'clusterUID': 'owned', 'nodes': {'worker-a': 'node-a'}})

    def test_fixture_build_does_not_inherit_operator_credentials_or_go_overrides(self):
        environment = native.build_environment({'PATH': '/usr/bin', 'HOME': '/tmp/owner',
            'GOCACHE': '/tmp/cache', 'PROVIDER_API_KEY': 'synthetic-do-not-forward',
            'AWS_SECRET_ACCESS_KEY': 'synthetic-do-not-forward', 'GOFLAGS': '-skip=Native',
            'GOTOOLCHAIN': 'unexpected', 'GOSUMDB': 'off', 'GOROOT': '/unexpected'})
        self.assertEqual(environment, {'PATH': '/usr/bin', 'HOME': '/tmp/owner', 'GOCACHE': '/tmp/cache',
            'GOENV': 'off', 'GOWORK': 'off', 'GOTOOLCHAIN': 'go1.26.10', 'CGO_ENABLED': '0',
            'GOOS': 'linux', 'GOARCH': 'amd64'})

    def test_current_owner_instructions_regression_is_in_cluster_suite(self):
        import re
        self.assertRegex('TestNativeCurrentInstructionsSurviveCompaction', native.TESTS)
        self.assertIsNone(re.fullmatch(native.TESTS, 'TestUnselected'))

    def test_plan_accepts_explicit_target_and_builds_only_synthetic_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory) / 'qualification'
            image = 'registry.example.invalid/team/runner@sha256:' + 'a' * 64
            argv = ['qualification', 'plan', '--kubectl', '/usr/bin/kubectl',
                    '--kubeconfig', '/tmp/fixture-kubeconfig', '--context', 'test-cluster',
                    '--cluster-uid', 'owned', '--state', str(state), '--runner-image', image]

            def git(arguments, **kwargs):
                value = 'a' * 40 + '\n' if arguments[1] == 'rev-parse' else ''
                return value if kwargs.get('text') else value.encode()

            def build(arguments, **kwargs):
                environment = kwargs['env']
                self.assertNotIn('PROVIDER_API_KEY', environment)
                self.assertEqual(environment['GOTOOLCHAIN'], 'go1.26.10')
                if '-c' in arguments:
                    (state / 'native.test').write_bytes(b'\x7fELFsynthetic-test-binary')
                else:
                    (state / 'materializations.json').write_text(json.dumps([{}] * 4))
                return SimpleNamespace(returncode=0, stdout=b'', stderr=b'')

            with patch.object(native.sys, 'argv', argv), patch.object(native, 'Client', return_value=ClientFixture()), \
                    patch.object(native.subprocess, 'check_output', side_effect=git), \
                    patch.object(native.subprocess, 'run', side_effect=build), \
                    patch.dict(native.os.environ, {'PATH': '/usr/bin', 'HOME': '/tmp/fixture',
                        'PROVIDER_API_KEY': 'synthetic-do-not-forward'}, clear=True), patch('builtins.print'):
                native.main()
            plan = json.loads((state / 'plan.json').read_text())
            self.assertEqual(plan['context'], 'test-cluster')
            self.assertEqual(plan['nodes'], {'worker-a': 'owned'})
            self.assertEqual(plan['resources'][-1]['spec']['containers'][0]['image'], image)
            self.assertEqual([x['kind'] for x in plan['resources']],
                             ['Namespace', 'NetworkPolicy', 'ConfigMap', 'Pod'])
            self.assertFalse(plan['resources'][-1]['spec']['automountServiceAccountToken'])

    def test_unknown_namespaced_resource_blocks_namespace_deletion(self):
        objects = {'pods': 'owned native <none> <none>', 'serviceaccounts': 'auto default <none> <none>',
                   'configmaps': 'ca kube-root-ca.crt <none> <none>', 'events': 'event scheduled owned <none>'}
        plan, journal = {'namespace': 'fixture'}, {'objects': [{'uid': 'owned'}]}
        native.verify_cleanup_inventory(ClientFixture(objects=objects), plan, journal)
        for kind, row in [('pods', 'foreign neighbor <none> <none>'), ('secrets', 'foreign credential <none> <none>'),
                          ('widgets.example.invalid', 'foreign tenant <none> <none>'), ('events', 'foreign event neighbor <none>')]:
            with self.assertRaises(ValueError):
                native.verify_cleanup_inventory(ClientFixture(objects={**objects, kind: row}), plan, journal)

    def test_metrics_projection_requires_current_owned_pod(self):
        plan = {'namespace': 'fixture', 'scope': 'fixture'}
        journal = {'objects': [{'kind': 'Pod', 'name': 'native', 'namespace': 'fixture', 'uid': 'owned'}]}
        objects = {'pods.metrics.k8s.io': '<none> native <none> <none>'}
        native.verify_cleanup_inventory(ClientFixture(objects=objects), plan, journal)
        for client in (ClientFixture(identity='replacement', objects=objects),
                       ClientFixture(scope='foreign', objects=objects),
                       ClientFixture(objects={'pods.metrics.k8s.io': '<none> neighbor <none> <none>'})):
            with self.assertRaises(ValueError):
                native.verify_cleanup_inventory(client, plan, journal)


if __name__ == '__main__':
    unittest.main()
