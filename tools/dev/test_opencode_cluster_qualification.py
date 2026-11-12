"""Отрицательные проверки target/UID и запрета очистки соседнего объекта."""
import importlib.util
import json
from pathlib import Path
import unittest

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
            return json.dumps({'items': [{'metadata': {'name': 'k3d-kodex-server-0', 'uid': self.identity}}]}).encode()
        return self.objects[arguments[3]].encode()


class NativeQualificationSafetyTests(unittest.TestCase):
    def test_replaced_uid_and_foreign_scope_never_authorize_cleanup(self):
        record = {'kind': 'Pod', 'name': 'native', 'namespace': 'fixture', 'uid': 'owned'}
        for client in (ClientFixture(identity='replacement'), ClientFixture(scope='foreign')):
            with self.assertRaises(ValueError):
                native.verify_owned(client, {'scope': 'fixture'}, record)
        native.verify_owned(ClientFixture(), {'scope': 'fixture'}, record)

    def test_target_cluster_and_node_incarnations_are_both_required(self):
        plan = {'clusterUID': 'owned', 'nodes': {'k3d-kodex-server-0': 'owned'}}
        native.verify_target(ClientFixture(), plan)
        for changed in ({**plan, 'clusterUID': 'other'}, {**plan, 'nodes': {'k3d-kodex-server-0': 'replacement'}}):
            with self.assertRaises(ValueError):
                native.verify_target(ClientFixture(), changed)

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
