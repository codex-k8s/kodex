"""Переносимый probe сохраняет exact-target проверки до первого Create."""

import contextlib
import importlib.util
import io
from pathlib import Path
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location('volume_probe', Path(__file__).with_name('workspace-volume-native-probe.py'))
PROBE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PROBE)
CLUSTER = '00000000-0000-4000-8000-000000000001'
NAMESPACE = '00000000-0000-4000-8000-000000000002'
NODE = '00000000-0000-4000-8000-000000000003'
IMAGE = 'registry.example.invalid:5000/runtime/agent-runner@sha256:' + 'a' * 64


class PortableVolumeProbeTest(unittest.TestCase):
    """Unknown identity, tag и отсутствие cached image не разрешают effects."""

    def arguments(self, **replacements):
        """Построить явно выбранную synthetic установку без локальных credentials."""
        values = {'context': 'k3d-example', 'expected-cluster-uid': CLUSTER,
                  'expected-staging-uid': NAMESPACE, 'node': 'example-node',
                  'expected-node-uid': NODE, 'image': IMAGE}
        values.update(replacements)
        return ['probe', *[item for key, value in values.items() for item in ('--' + key, value)]]

    def observed(self):
        """Вернуть только metadata preflight, без модели, сети и workloads."""
        return [
            {'metadata': {'uid': CLUSTER}},
            {'metadata': {'uid': NAMESPACE}},
            {'metadata': {'uid': NODE}, 'spec': {}, 'status': {'images': [{'names': [IMAGE]}]}},
            {'provisioner': 'rancher.io/local-path', 'volumeBindingMode': 'WaitForFirstConsumer'},
        ]

    def test_portable_target_reaches_create_only_after_all_readbacks(self):
        """Другие имена допустимы только при совпадении UID, node image и driver."""
        with patch('sys.argv', self.arguments()), patch.object(PROBE, 'read', side_effect=self.observed()) as read, \
                patch.object(PROBE, 'create', side_effect=RuntimeError('STOP_BEFORE_CREATE')) as create, \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(PROBE.main(), 1)
        self.assertEqual(read.call_count, 4)
        create.assert_called_once()
        namespace = create.call_args.args[1]
        self.assertEqual(namespace['kind'], 'Namespace')
        self.assertTrue(namespace['metadata']['name'].startswith('kodex-qa-1953-'))
        self.assertEqual(namespace['metadata']['labels']['pod-security.kubernetes.io/enforce'], 'restricted')

    def test_wrong_cluster_namespace_node_or_cache_never_creates(self):
        """Имя context и доступность API не заменяют положительный exact readback."""
        for index, field, value in [
            (0, 'metadata', {'uid': NODE}), (1, 'metadata', {'uid': CLUSTER}),
            (2, 'metadata', {'uid': CLUSTER}), (2, 'status', {'images': []}),
            (2, 'spec', {'unschedulable': True}), (3, 'provisioner', 'example.invalid/other'),
            (3, 'volumeBindingMode', 'Immediate'),
        ]:
            with self.subTest(index=index, field=field):
                observed = self.observed()
                observed[index][field] = value
                with patch('sys.argv', self.arguments()), patch.object(PROBE, 'read', side_effect=observed), \
                        patch.object(PROBE, 'create') as create, self.assertRaises(RuntimeError):
                    PROBE.main()
                create.assert_not_called()

    def test_invalid_input_never_contacts_cluster(self):
        """Tag, URL credentials, пустой UID и option injection отклоняются до I/O."""
        for replacements in [
            {'image': 'registry.example.invalid/runner:latest'},
            {'image': 'https://user:secret@registry.example.invalid/runner@sha256:' + 'a' * 64},
            {'image': 'registry.example.invalid/../runner@sha256:' + 'a' * 64},
            {'expected-cluster-uid': ''}, {'expected-node-uid': 'not-a-uid'},
            {'context': 'example\n--server=example.invalid'}, {'node': 'example node'},
        ]:
            with self.subTest(fields=list(replacements)):
                with patch('sys.argv', self.arguments(**replacements)), patch.object(PROBE, 'read') as read, \
                        patch.object(PROBE, 'create') as create, self.assertRaises(RuntimeError):
                    PROBE.main()
                read.assert_not_called()
                create.assert_not_called()


if __name__ == '__main__':
    unittest.main()
