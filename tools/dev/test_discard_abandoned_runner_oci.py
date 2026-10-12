"""Малые fixtures; Docker, sudo и живой кластер не вызываются."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('abandoned_discard', Path(__file__).with_name('discard-abandoned-runner-oci.py'))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class DiscardTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / 'fixture.oci.tar'
        self.path.write_bytes(b'fixture')
        self.plan = {'proof': {'metadata': M.T.identity(self.path.stat())}}
        for name, value in [('TARGET', str(self.path)), ('SIZE', 7)]:
            p = patch.object(M, name, value)
            p.start()
            self.addCleanup(p.stop)

    def test_exact_unlink_and_readback(self):
        with patch.object(M, 'audit', return_value=self.plan):
            result = M.execute(M.fingerprint(self.plan))
        self.assertEqual(result['status'], 'PASS')
        self.assertEqual(result['removedBytes'], 7)
        self.assertFalse(self.path.exists())

    def test_fingerprint_drift_refuses_unlink(self):
        with patch.object(M, 'audit', return_value=self.plan):
            with self.assertRaises(RuntimeError):
                M.execute('foreign')
        self.assertTrue(self.path.exists())

    def test_stat_drift_refuses_unlink(self):
        self.path.write_bytes(b'changed size')
        with patch.object(M, 'audit', return_value=self.plan):
            with self.assertRaises(RuntimeError):
                M.execute(M.fingerprint(self.plan))
        self.assertTrue(self.path.exists())

    def test_audit_refusal_preserves_file(self):
        with patch.object(M, 'audit', side_effect=RuntimeError('ARCHIVE_IN_USE')):
            with self.assertRaises(RuntimeError):
                M.execute('any')
        self.assertTrue(self.path.exists())

    def test_mounted_file_or_ancestor_is_refused(self):
        for source in [str(self.path), str(self.path.parent), '/']:
            with patch.object(M.subprocess, 'check_output', side_effect=[b'fixture\n', json.dumps([{'Source': source}]).encode()]):
                with self.assertRaises(RuntimeError):
                    M.external_references()

    def test_unrelated_mount_is_allowed(self):
        with patch.object(M.subprocess, 'check_output', side_effect=[b'fixture\n', b'[{"Source":"/unrelated"}]']):
            M.external_references()


if __name__ == '__main__':
    unittest.main()
