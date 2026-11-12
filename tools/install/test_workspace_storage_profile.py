"""Проверка полной доставки V2 и отказа при несовместимой геометрии/authority inputs."""
import copy
import unittest
from deployment_profile import Rejected
import workspace_storage_profile as subject


def fixture():
    result = []
    for name in subject.COMPONENTS:
        data = {}
        if name != 'control-plane':
            prefix = name.upper().replace('-', '_')
            data = {prefix + '_STORAGE_CLASS': '', prefix + '_SESSION_PVC_SIZE': '20Gi'}
        result.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': name + '-runtime', 'namespace': 'kodex-system'}, 'data': data})
        result.append({'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': {'name': name, 'namespace': 'kodex-system'}, 'spec': {'template': {'spec': {'containers': [{'name': name, 'image': 'registry.example.invalid/kodex/'+name+'@sha256:'+'a'*64, 'envFrom': [{'configMapRef': {'name': name+'-runtime'}}], 'env': ([{'name': 'CONTROL_PLANE_ROLE_RUNTIME_CONTRACT_REVISION', 'value': '5'}] if name == 'control-plane' else [])}]}}}})
    return result


class WorkspaceStorageProfileTests(unittest.TestCase):
    def test_delivery_and_repeat(self):
        before = fixture(); saved = copy.deepcopy(before)
        after = subject.transform(before, subject.PROFILE)
        self.assertEqual(before, saved)
        self.assertEqual(subject.transform(after, subject.PROFILE), after)
        for item in after:
            if item['kind'] == 'Deployment':
                self.assertEqual(item['spec']['template']['metadata']['annotations'][subject.MARKER], subject.PROFILE)
            elif item['metadata']['name'] != 'runtime-controller-runtime':
                self.assertEqual(item['data'][subject.KEY], subject.PROFILE)
        self.assertEqual(after[2]['data']['RUNTIME_CONTROLLER_STORAGE_CLASS'], 'local-path')
        self.assertEqual(after[4]['data']['SESSION_ARCHIVE_STORAGE_CLASS'], 'local-path')

    def test_disabled_does_not_remove_existing_binding(self):
        before = subject.transform(fixture(), subject.PROFILE)
        self.assertEqual(subject.transform(before, 'disabled'), before)

    def test_closed_rejections(self):
        mutations = [
            lambda r: r.pop(),
            lambda r: r.append(copy.deepcopy(r[0])),
            lambda r: r[1]['spec']['template']['spec']['containers'][0]['env'][0].update(value='4'),
            lambda r: r[2]['data'].update(RUNTIME_CONTROLLER_STORAGE_CLASS='foreign'),
            lambda r: r[4]['data'].update(SESSION_ARCHIVE_SESSION_PVC_SIZE='10Gi'),
            lambda r: r[5]['spec']['template']['spec']['containers'][0]['env'].append({'name': subject.KEY, 'value': ''}),
            lambda r: r[1]['spec']['template']['spec']['containers'][0].update(image='mutable:latest'),
            lambda r: r[0]['data'].update(SESSION_WORKSPACE_STORAGE_PROFILE='unknown'),
            lambda r: r[3]['spec']['template']['spec']['containers'][0]['envFrom'].append({'secretRef': {'name': 'shadow'}}),
        ]
        for mutation in mutations:
            with self.subTest(mutation=mutation):
                resources = fixture(); mutation(resources)
                with self.assertRaises(Rejected): subject.transform(resources, subject.PROFILE)
        with self.assertRaises(Rejected): subject.transform(fixture(), 'unknown')

    def test_configmap_abi(self):
        values = fixture(); values[1]['spec']['template']['spec']['containers'][0]['env'][0] = {'name':'CONTROL_PLANE_ROLE_RUNTIME_CONTRACT_REVISION', 'valueFrom': {'configMapKeyRef': {'name':'policy','key':'roleRuntimeContractRevision'}}}
        values.append({'kind':'ConfigMap','metadata':{'namespace':'kodex-system','name':'policy'},'data':{'roleRuntimeContractRevision':'5'}})
        subject.transform(values, subject.PROFILE)
        values[-1]['data']['roleRuntimeContractRevision']='4'
        with self.assertRaises(Rejected): subject.transform(values, subject.PROFILE)


if __name__ == '__main__': unittest.main()
