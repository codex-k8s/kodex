"""Доказательства отказа при drift, чужом объекте и гонке, без Kubernetes/БД."""
import copy
import json
import unittest

from deployment_profile import Rejected
from portable_state import State, key, seal, verify_resource, verify_schemas


def resource(kind='Job', name='control-plane-migrate'):
    return {'apiVersion': 'batch/v1', 'kind': kind, 'metadata': {'name': name, 'namespace': 'kodex-system',
        'labels': {'app.kubernetes.io/part-of': 'kodex'}}, 'spec': {'template': {'spec': {'containers': [
            {'name': 'migrate', 'image': 'registry.example.invalid/app@sha256:' + '1' * 64,
             'env': [{'name': 'MODE', 'value': 'test'}]}]}}}}


def live(desired):
    result = copy.deepcopy(desired)
    result['metadata'].update(uid='test-uid', resourceVersion='1')
    result['status'] = {'succeeded': 1}
    return result


class FixtureState(State):
    def __init__(self, objects):
        super().__init__('test-context')
        self.objects = {key(r): copy.deepcopy(r) for r in objects}
        self.writes = []

    def discover(self, resources):
        self.routes = {(r['apiVersion'], r['kind']): ('/apis/batch/v1', 'jobs', True) for r in resources}

    def get(self, resource):
        return copy.deepcopy(self.objects.get(key(resource)))

    def call(self, args, value=None):
        if args[0] == 'create':
            if key(value) in self.objects: raise Rejected('COLLISION')
            self.objects[key(value)] = live(value)
        elif args[0] == 'apply':
            current = self.objects[key(value)]
            if value['metadata']['resourceVersion'] != current['metadata']['resourceVersion']: raise Rejected('CONFLICT')
            self.objects[key(value)] = live(value)
        elif args[0] == 'delete':
            current = next(iter(self.objects.values()))
            assert value['preconditions'] == {'uid': current['metadata']['uid'], 'resourceVersion': current['metadata']['resourceVersion']}
            self.objects.clear()
        elif args[0] == 'wait':
            return b''
        else:
            raise AssertionError('Unexpected external operation')
        self.writes.append((args, value))
        return b'{}'


class StateTest(unittest.TestCase):
    def test_foreign_job_collision_is_rejected_before_any_write(self):
        desired = resource(); current = live(desired)
        current['metadata']['labels'] = {'app.kubernetes.io/part-of': 'other'}
        state = FixtureState([current])
        with self.assertRaises(Rejected): state.inventory([desired])
        self.assertEqual(state.writes, [])

    def test_create_collision_and_preimage_drift_cannot_be_overwritten(self):
        desired = resource(); state = FixtureState([]); inventory = state.inventory([desired])
        state.objects[key(desired)] = live(desired)
        with self.assertRaises(Rejected): state.apply(desired, inventory)
        self.assertEqual(state.writes, [])
        inventory = state.inventory([desired])
        state.objects[key(desired)]['spec']['foreign'] = True
        with self.assertRaises(Rejected): state.delete(desired, inventory)
        self.assertEqual(state.writes, [])

    def test_successor_deletion_uses_uid_and_fresh_resource_version(self):
        desired = resource(); state = FixtureState([live(desired)]); inventory = state.inventory([desired])
        state.objects[key(desired)]['metadata']['resourceVersion'] = '2'
        state.objects[key(desired)]['status'] = {'succeeded': 1, 'observed': True}
        state.delete(desired, inventory)
        self.assertIsNone(inventory[key(desired)])
        state.apply(desired, inventory)
        self.assertEqual(len(state.writes), 2)
        self.assertTrue(inventory[key(desired)]['uid'])

    def test_readback_rejects_healthy_other_release_config_and_added_rules(self):
        desired = resource(kind='Deployment', name='control-plane')
        verify_resource(desired, live(desired))
        for field in ('image', 'env'):
            current = live(desired)
            current['spec']['template']['spec']['containers'][0][field] = 'foreign'
            with self.assertRaises(Rejected): verify_resource(desired, current)
        cm = {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': desired['metadata'], 'data': {'profile': 'A'}}
        current = live(cm); current['data']['profile'] = 'B'
        with self.assertRaises(Rejected): verify_resource(cm, current)
        policy = {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy', 'metadata': desired['metadata'],
                  'spec': {'podSelector': {}, 'policyTypes': ['Ingress'], 'ingress': []}}
        current = live(policy); current['spec']['ingress'] = [{}]
        with self.assertRaises(Rejected): verify_resource(policy, current)

    def test_schema_readback_is_read_only_and_exact(self):
        resources = [{'kind': 'ConfigMap', 'metadata': {'name': 'kodex-installation-profile'}, 'data': {
            'schema-heads.json': json.dumps({'control-plane': 100, 'internal-rpc-authority': 200, 'email-bridge': 300})}}]
        class Reader:
            def call(self, args):
                if args[2] != 'exec': raise AssertionError('Write attempted')
                database = args[args.index('-d') + 1]
                return {'control_plane': b'100\n', 'internal_rpc_authority': b'200\n', 'email_bridge': b'300\n'}[database]
        verify_schemas(Reader(), resources)
        resources[0]['data']['schema-heads.json'] = json.dumps({'control-plane': 101, 'internal-rpc-authority': 200, 'email-bridge': 300})
        with self.assertRaises(Rejected): verify_schemas(Reader(), resources)


if __name__ == '__main__': unittest.main()
