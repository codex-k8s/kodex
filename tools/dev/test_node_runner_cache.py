"""Отбор node cache сохраняет текущие consumers и не удаляет неизвестные aliases."""

import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location('node_cache', Path(__file__).with_name('node-runner-cache.py'))
C = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(C)


class CacheTests(unittest.TestCase):
    def test_only_old_exact_owned_runner_selected(self):
        digest = 'sha256:' + 'a' * 64
        ref = C.PREFIX + digest
        image = {'name': ref, 'digest': digest}
        other = {'name': 'example.invalid/foreign@' + 'sha256:' + 'b' * 64, 'digest': 'sha256:' + 'b' * 64}
        self.assertEqual(C.selected([image, other], set()), [{'ref': ref, 'digest': digest, 'aliases': [image]}])
        self.assertEqual(C.selected([image], {ref}), [])
        self.assertEqual(C.selected([image], {'pull.example.invalid/kodex/agent-runner@' + digest}), [])

    def test_unknown_alias_same_target_preserves_entire_group(self):
        digest = 'sha256:' + 'a' * 64
        image = {'name': C.PREFIX + digest, 'digest': digest}
        foreign = {'name': 'example.invalid/foreign:fixture', 'digest': digest}
        self.assertEqual(C.selected([image, foreign], set()), [])

    def test_bare_alias_requires_owned_witness_and_remains_protected(self):
        digest = 'sha256:' + 'a' * 64
        image = {'name': C.PREFIX + digest, 'digest': digest}
        bare = {'name': 'sha256:' + 'b' * 64, 'digest': digest}
        self.assertEqual(C.selected([bare], set()), [])
        self.assertEqual(len(C.selected([image, bare], set())[0]['aliases']), 2)
        self.assertEqual(C.selected([image, bare], {bare['name']}), [])

    def test_digest_mismatch_rejected(self):
        with self.assertRaises(ValueError):
            C.selected([{'name': C.PREFIX + 'sha256:' + 'a' * 64, 'digest': 'sha256:' + 'b' * 64}], set())


if __name__ == '__main__':
    unittest.main()
