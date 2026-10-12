"""Маленькие OCI fixtures; sudo, процессный scanner и кластер не запускаются."""
import importlib.util
import json
import os
from pathlib import Path
import unittest
from types import SimpleNamespace
from unittest.mock import patch

def module(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value

FIXTURE = module('discard_fixture', 'test_transfer_own_runner_oci.py')
D = module('discard', 'discard-moved-runner-oci.py')
PROCESS_CHECK = D.process_check

class DiscardTests(unittest.TestCase):
    def setUp(self):
        self.fixture = FIXTURE.TransferTests('test_move_and_readback_preserve_exact_bytes_and_provenance')
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.t = FIXTURE.M
        self.t.write_new(self.fixture.dest / 'plan.json', self.fixture.plan)
        self.fixture.run_transfer()
        for name, value in [('T', self.t), ('DEST', self.fixture.dest), ('OLD_FP', self.fixture.plan['fingerprint']), ('OLD_SHA', self.fixture.plan['sourceSHA'])]:
            p = patch.object(D, name, value); p.start(); self.addCleanup(p.stop)
        p = patch.object(D, 'process_check', return_value=None); p.start(); self.addCleanup(p.stop)
        self.plan = D.audit()
        self.target = self.fixture.dest / self.fixture.archive.name
        self.old_receipt = (self.fixture.dest / 'receipt.jsonl').read_bytes()

    def apply(self):
        return D.execute(self.plan, self.plan['fingerprint'], True)

    def test_discard_only_archive_and_preserve_all_metadata(self):
        self.assertEqual(self.apply()['status'], 'PASS')
        self.assertFalse(self.target.exists())
        self.assertTrue(self.fixture.proof.exists())
        self.assertTrue((self.fixture.dest / 'plan.json').exists())
        self.assertEqual((self.fixture.dest / 'receipt.jsonl').read_bytes(), self.old_receipt)
        self.assertEqual(D.execute(self.plan, self.plan['fingerprint'])['status'], 'PASS')

    def test_in_use_is_refused_without_unlink(self):
        with patch.object(D, 'process_check', side_effect=RuntimeError('ARCHIVE_IN_USE')):
            with self.assertRaises(RuntimeError): self.apply()
        self.assertTrue(self.target.exists())

    def test_changed_archive_sha_stat_or_symlink_refused(self):
        self.target.write_bytes(b'changed')
        with self.assertRaises((RuntimeError, self.t.OCI.Failure)): self.apply()
        self.assertTrue(self.target.exists())

    def test_unknown_absence_and_old_receipt_mutation_refused(self):
        self.target.unlink()
        with self.assertRaises(RuntimeError): self.apply()
        self.assertTrue(self.fixture.proof.exists())

    def test_resume_after_unlink_before_ack_uses_same_journal(self):
        real = D.os.fsync
        calls = 0
        def crash(fd):
            nonlocal calls
            calls += 1
            if calls == 4:
                raise OSError('synthetic crash')
            return real(fd)
        with patch.object(D.os, 'fsync', side_effect=crash):
            with self.assertRaises(OSError): self.apply()
        self.assertEqual(self.apply()['status'], 'PASS')

    def test_transfer_receipt_drift_refused(self):
        with (self.fixture.dest / 'receipt.jsonl').open('a') as f:
            f.write(json.dumps({'kind': 'foreign'}) + '\n')
        with self.assertRaises(RuntimeError): self.apply()
        self.assertTrue(self.target.exists())

    def test_rehashed_foreign_destination_refused(self):
        self.plan['records'][0]['path'] = str(self.fixture.dest / 'plan.json')
        value = {k: v for k, v in self.plan.items() if k != 'fingerprint'}
        self.plan['fingerprint'] = self.t.sha(json.dumps(value, sort_keys=True).encode())
        with self.assertRaises(RuntimeError): self.apply()
        self.assertTrue(self.target.exists())

    def test_process_errors_and_nonzero_references_are_closed(self):
        for code, refs, stderr in [(1, 0, b''), (0, 1, b''), (0, 0, b'warning'), (0, False, b'')]:
            result = SimpleNamespace(returncode=code, stdout=json.dumps({'processes': 10, 'references': refs}).encode(), stderr=stderr)
            with patch.object(D.subprocess, 'run', return_value=result):
                with self.assertRaises(RuntimeError): PROCESS_CHECK()
        result = SimpleNamespace(returncode=0, stdout=b'{"processes":10,"references":0}', stderr=b'')
        with patch.object(D.subprocess, 'run', return_value=result): PROCESS_CHECK()

    def test_symlink_and_hardlink_are_refused(self):
        link = self.fixture.dest / 'extra-link'
        os.link(self.target, link)
        with self.assertRaises(RuntimeError): self.apply()
        link.unlink()
        self.target.unlink(); self.target.symlink_to(self.fixture.proof)
        with self.assertRaises(RuntimeError): self.apply()

if __name__ == '__main__':
    unittest.main()
