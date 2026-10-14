"""Герметичные проверки: реальные маленькие OCI, без sudo, Docker или кластера."""
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('transfer', Path(__file__).with_name('transfer-own-runner-oci.py'))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)
HEADROOM = M.headroom


class TransferTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / 'source'
        self.dest = self.root / 'oci-transfer-1797-fixture'
        self.source.mkdir(mode=0o700)
        self.dest.mkdir(mode=0o700)
        self.archive = self.source / ('agent-runner-' + 'a' * 64 + '.oci.tar')
        self.manifest = 'sha256:' + 'b' * 64
        index = json.dumps({'schemaVersion': 2, 'manifests': [{'digest': self.manifest}]}).encode()
        with tarfile.open(self.archive, 'w') as tar:
            member = tarfile.TarInfo('index.json')
            member.size = len(index)
            tar.addfile(member, io.BytesIO(index))
        self.archive.chmod(0o600)
        self.proof = self.source / (self.archive.name[:-8] + '-' + 'c' * 40 + '.provenance.json')
        M.write_new(self.proof, {'kind': 'RUNNER_BINARY_PROVENANCE', 'imageProfile': 'full', 'sourceRevision': 'c' * 40, 'baseImage': 'registry.local.kodex/kodex/agent-runner@' + self.manifest, 'archiveSHA256': hashlib.sha256(self.archive.read_bytes()).hexdigest()})
        for name, value in [('SOURCES', ((str(self.archive), self.manifest, self.archive.stat().st_size),)), ('DEST_PARENT', self.root)]:
            helper = patch.object(M, name, value)
            helper.start(); self.addCleanup(helper.stop)
        for name, value in [('revision', 'c' * 40), ('process_check', None), ('headroom', None), ('root_device', self.dest.stat().st_dev)]:
            helper = patch.object(M, name, return_value=value)
            helper.start(); self.addCleanup(helper.stop)
        data = M.inspect(self.archive)
        self.plan = {'version': 1, 'sourceSHA': M.revision(), 'scriptSHA256': M.sha(Path(M.__file__).read_bytes()), 'destination': str(self.dest), 'directory': M.destination(self.dest), 'records': [{'source': str(self.archive), 'destination': str(self.dest / self.archive.name), 'parent': M.directory(self.source, True), **data, 'provenance': str(self.proof), 'provenanceSHA256': M.load(self.proof)[1], 'sourceRevision': 'c' * 40}]}
        self.plan['fingerprint'] = M.sha(json.dumps(self.plan, sort_keys=True).encode())

    def run_transfer(self):
        return M.execute(self.plan, self.plan['fingerprint'], True)

    def test_move_and_readback_preserve_exact_bytes_and_provenance(self):
        before = self.archive.read_bytes()
        self.assertEqual(self.run_transfer()['status'], 'PASS')
        target = self.dest / self.archive.name
        self.assertFalse(self.archive.exists())
        self.assertEqual(target.read_bytes(), before)
        self.assertEqual(target.stat().st_mode & 0o777, 0o600)
        self.assertTrue(self.proof.exists())
        self.assertEqual(M.execute(self.plan, self.plan['fingerprint'])['status'], 'PASS')
        self.assertEqual(self.run_transfer()['status'], 'PASS')

    def test_verified_copy_resume_after_unlink_failure(self):
        with patch.object(M.os, 'unlink', side_effect=OSError('synthetic')):
            with self.assertRaises(OSError): self.run_transfer()
        self.assertTrue(self.archive.exists())
        self.assertTrue((self.dest / self.archive.name).exists())
        self.assertEqual(self.run_transfer()['status'], 'PASS')

    def test_published_copy_resume_before_copied_receipt(self):
        original = M.publish
        def interrupted(partial, target):
            original(partial, target)
            raise OSError('synthetic crash')
        with patch.object(M, 'publish', side_effect=interrupted):
            with self.assertRaises(OSError): self.run_transfer()
        self.assertTrue(self.archive.exists())
        self.assertEqual(self.run_transfer()['status'], 'PASS')

    def test_verified_partial_resume_without_overwrite(self):
        with patch.object(M, 'publish', side_effect=OSError('synthetic crash')):
            with self.assertRaises(OSError): self.run_transfer()
        self.assertTrue((self.dest / (self.archive.name + '.partial')).exists())
        self.assertEqual(self.run_transfer()['status'], 'PASS')

    def test_fsync_failure_never_unlinks_source(self):
        with patch.object(M, 'sync_file', side_effect=OSError('synthetic fsync failure')):
            with self.assertRaises(OSError): self.run_transfer()
        self.assertTrue(self.archive.exists())
        self.assertEqual(self.run_transfer()['status'], 'PASS')

    def test_audit_binds_provenance_original_metadata_and_subset(self):
        original = M.inspect
        def other_device(path, private=False):
            result = original(path, private)
            result['metadata'][0] += 1
            return result
        with patch.object(M, 'inspect', side_effect=other_device):
            plan = M.audit(self.dest, [self.archive.name])
        self.assertEqual(len(plan['records']), 1)
        self.assertEqual(plan['records'][0]['sha256'], self.plan['records'][0]['sha256'])
        self.assertEqual(plan['records'][0]['sourceRevision'], 'c' * 40)
        self.assertEqual(plan['records'][0]['provenanceSHA256'], M.load(self.proof)[1])

    def test_existing_copy_without_same_journal_is_refused(self):
        M.write_new(self.dest / self.archive.name, {'foreign': True})
        with self.assertRaises(RuntimeError): self.run_transfer()
        self.assertTrue(self.archive.exists())

    def test_existing_partial_without_same_journal_is_refused(self):
        target = self.dest / (self.archive.name + '.partial')
        target.write_bytes(self.archive.read_bytes()); target.chmod(0o600)
        with self.assertRaises(RuntimeError): self.run_transfer()
        self.assertTrue(self.archive.exists())

    def test_corrupt_partial_is_not_overwritten_on_resume(self):
        with patch.object(M, 'publish', side_effect=OSError('synthetic crash')):
            with self.assertRaises(OSError): self.run_transfer()
        target = self.dest / (self.archive.name + '.partial')
        target.write_bytes(b'corrupted')
        with self.assertRaises((RuntimeError, M.OCI.Failure)): self.run_transfer()
        self.assertEqual(target.read_bytes(), b'corrupted')
        self.assertTrue(self.archive.exists())

    def test_changed_source_and_provenance_refused(self):
        self.archive.write_bytes(b'changed')
        with self.assertRaises((RuntimeError, M.OCI.Failure)): self.run_transfer()
        self.assertTrue(self.archive.exists())

    def test_in_use_refused_before_copy(self):
        with patch.object(M, 'process_check', side_effect=RuntimeError('ARCHIVE_IN_USE')):
            with self.assertRaises(RuntimeError): self.run_transfer()
        self.assertTrue(self.archive.exists())
        self.assertFalse((self.dest / self.archive.name).exists())

    def test_no_overwrite_atomic_publish(self):
        partial, target = self.dest / 'partial', self.dest / 'existing'
        partial.write_bytes(b'new'); target.write_bytes(b'old')
        with self.assertRaises(RuntimeError): M.publish(partial, target)
        self.assertEqual(target.read_bytes(), b'old')

    def test_symlink_hardlink_permissions_and_plan_drift(self):
        with self.assertRaises(RuntimeError): M.validate(self.plan, '0' * 64)
        link = self.source / 'link'
        link.symlink_to(self.archive)
        with self.assertRaises(RuntimeError): M.checked(link)
        link.unlink(); os.link(self.archive, link)
        with self.assertRaises(RuntimeError): M.checked(self.archive)
        link.unlink(); self.archive.chmod(0o666)
        with self.assertRaises(RuntimeError): M.checked(self.archive)

    def test_destination_drift_is_refused(self):
        self.dest.chmod(0o755)
        with self.assertRaises(RuntimeError): self.run_transfer()

    def test_wrong_device_is_refused(self):
        with patch.object(M, 'root_device', return_value=0):
            with self.assertRaises(RuntimeError): self.run_transfer()

    def test_subset_is_exact_and_duplicates_foreign_names_refused(self):
        self.assertEqual(M.selected_sources([self.archive.name]), list(M.SOURCES))
        with self.assertRaises(RuntimeError): M.selected_sources(['foreign.oci.tar'])
        with self.assertRaises(RuntimeError): M.selected_sources([self.archive.name, self.archive.name])

    def test_foreign_source_cannot_enter_rehashed_plan(self):
        self.plan['records'][0]['source'] = str(self.source / 'foreign')
        value = {k: v for k, v in self.plan.items() if k != 'fingerprint'}
        self.plan['fingerprint'] = M.sha(json.dumps(value, sort_keys=True).encode())
        with self.assertRaises(RuntimeError): self.run_transfer()

    def test_headroom_uses_all_remaining_copies(self):
        with patch.object(M.os, 'statvfs') as statvfs:
            statvfs.return_value.f_frsize = 1
            statvfs.return_value.f_blocks = 100 << 30
            statvfs.return_value.f_bavail = 15 << 30
            with self.assertRaises(RuntimeError): HEADROOM(self.dest, 3 << 30)
            HEADROOM(self.dest, 1 << 30)


if __name__ == '__main__':
    unittest.main()
