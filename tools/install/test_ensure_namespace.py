"""Проверки создания namespace и адресного восстановления метки владельца."""
import copy
import json
import unittest
import ensure_namespace as subject


class NamespaceOwnership(unittest.TestCase):
    def test_create_has_ownership_and_replacement_is_rejected(self):
        for replacement in (False, True):
            calls = []
            def call(args, value=None):
                calls.append((args, value))
                if len(calls) == 1:return b''
                if args[0] == 'create':
                    self.assertEqual(value['metadata']['labels'], {subject.LABEL: 'kodex'})
                return json.dumps({'metadata': {'name': 'kodex-system', 'uid': 'replaced' if replacement and len(calls) == 3 else 'created', 'labels': {subject.LABEL: 'kodex'}}}).encode()
            if replacement:
                with self.assertRaises(ValueError):subject.ensure(call, 'kodex-system')
            else:subject.ensure(call, 'kodex-system')

    def test_adoption_requires_exact_uid_and_preserves_other_labels(self):
        original = {'metadata': {'name': 'kodex-trust', 'uid': 'created', 'resourceVersion': '12', 'labels': {'custom': 'kept'}}}
        for receipt, owner, accepted in ((None, None, False), ('wrong', None, False), ('created', 'foreign', False), ('created', None, True)):
            current = copy.deepcopy(original);calls = []
            if owner:current['metadata']['labels'][subject.LABEL] = owner
            def call(args, value=None):
                calls.append(args)
                if args[0] == 'patch':
                    self.assertEqual(value[:2], [{'op': 'test', 'path': '/metadata/uid', 'value': 'created'}, {'op': 'test', 'path': '/metadata/resourceVersion', 'value': '12'}])
                    current['metadata']['labels'] = value[2]['value']
                return json.dumps(current).encode()
            if accepted:
                subject.ensure(call, 'kodex-trust', receipt)
                self.assertEqual(current['metadata']['labels']['custom'], 'kept')
            else:
                with self.assertRaises(ValueError):subject.ensure(call, 'kodex-trust', receipt)
                self.assertEqual(len(calls), 1)

    def test_shared_namespace_rejected_without_any_api_call(self):
        def call(*args):self.fail('API must not be called')
        with self.assertRaises(ValueError):subject.ensure(call, 'kube-system')


if __name__ == '__main__':unittest.main()
