"""Cold cache cleanup только на собственных временных synthetic fixtures."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('cold_cache', Path(__file__).with_name('local-go-build-cache.py'))
C = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(C)


class ColdCacheTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix='kodex-cold-cache-test-')
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name) / 'cache'
        self.root.mkdir(mode=0o775)
        self.shard = self.root / 'aa'
        self.shard.mkdir(mode=0o775)
        self.old = self.shard / ('a' * 64 + '-d')
        self.old.write_bytes(b'synthetic build output')
        self.old.chmod(0o664)
        self.now = time.time_ns() + C.AGE_NS + 3600 * 10**9
        clock = patch.object(C.time, 'time_ns', return_value=self.now)
        clock.start()
        self.addCleanup(clock.stop)
        self.cache = C.Cache(self.root)

    def test_audit_no_mutation_and_explicit_prune(self):
        before = C.snapshot(self.old.stat())
        plan = self.cache.audit()
        self.assertEqual(C.snapshot(self.old.stat()), before)
        self.assertEqual(len(plan['records']), 1)
        result = self.cache.prune(plan, plan['fingerprint'])
        self.assertEqual(result['removedCount'], 1)
        self.assertTrue(self.shard.is_dir())
        self.assertFalse(self.old.exists())

    def test_fresh_all_three_times_and_boundary_preserved(self):
        info = self.old.stat()
        for field in ('st_mtime_ns', 'st_atime_ns', 'st_ctime_ns'):
            values = {name: getattr(info, name) for name in dir(info) if name.startswith('st_')}
            values[field] = self.now - C.AGE_NS
            fake = type('Stat', (), values)()
            self.assertFalse(C.eligible(fake, self.now))
        os.utime(self.old, ns=(self.now, self.now))
        self.assertEqual(self.cache.audit()['records'], [])

    def test_changed_atime_mtime_or_content_skipped(self):
        for kind in ('atime', 'mtime', 'replace'):
            with self.subTest(kind=kind):
                os.utime(self.old, ns=(1, 1))
                plan = self.cache.audit()
                if kind == 'replace':
                    self.old.unlink()
                    self.old.write_bytes(b'new output')
                else:
                    info = self.old.stat()
                    os.utime(self.old, ns=(self.now if kind == 'atime' else info.st_atime_ns,
                                          self.now if kind == 'mtime' else info.st_mtime_ns))
                result = self.cache.prune(plan, plan['fingerprint'])
                self.assertEqual(result['removedCount'], 0)
                self.assertEqual(result['skippedChangedOrAbsent'], 1)
                self.assertTrue(self.old.exists())

    def test_ctime_change_permission_preserved(self):
        plan = self.cache.audit()
        self.old.chmod(0o600)
        result = self.cache.prune(plan, plan['fingerprint'])
        self.assertEqual(result['removedCount'], 0)

    def test_second_stat_detects_access_immediately_before_unlink(self):
        plan = self.cache.audit()
        original = C.os.stat
        calls = 0

        def raced(path, *args, **kwargs):
            nonlocal calls
            if path == self.old.name:
                calls += 1
                if calls == 2:
                    os.utime(self.old, ns=(self.now, self.now))
            return original(path, *args, **kwargs)

        with patch.object(C.os, 'stat', side_effect=raced):
            result = self.cache.prune(plan, plan['fingerprint'])
        self.assertEqual(result['removedCount'], 0)
        self.assertTrue(self.old.exists())

    def test_symlink_hardlink_noncache_service_files_preserved(self):
        other = self.root / 'README'
        other.write_bytes(b'keep')
        symlink = self.shard / ('a' * 63 + 'b-d')
        symlink.symlink_to(other)
        os.link(self.old, self.shard / ('a' * 63 + 'c-d'))
        (self.shard / 'not-a-hash').write_bytes(b'keep')
        self.assertEqual(self.cache.audit()['records'], [])
        self.assertEqual(other.read_bytes(), b'keep')

    def test_root_and_shard_symlink_rejected(self):
        link = self.root.parent / 'link'
        link.symlink_to(self.root)
        with self.assertRaises(C.Failure):
            C.Cache(link).audit()
        (self.root / 'bb').symlink_to(self.shard)
        with self.assertRaises(C.Failure):
            self.cache.audit()

    def test_foreign_owner_world_writable_rejected(self):
        self.old.chmod(0o666)
        self.assertEqual(self.cache.audit()['records'], [])
        info = self.old.stat()
        with patch.object(C.os, 'getuid', return_value=info.st_uid + 1):
            self.assertFalse(C.eligible(info, self.now))

    def test_fingerprint_scope_escape_duplicate_and_age_rejected_before_unlink(self):
        for change in ('fingerprint', 'escape', 'duplicate', 'scope', 'age'):
            plan = self.cache.audit()
            expected = plan['fingerprint']
            if change == 'fingerprint':
                expected = '0' * 64
            elif change == 'escape':
                plan['records'][0]['path'] = '../escape'
            elif change == 'duplicate':
                plan['records'] *= 2
            elif change == 'scope':
                plan['root'] = '/not-selected'
            else:
                plan['createdAtNs'] -= C.PLAN_AGE_NS + 1
            if change != 'fingerprint':
                plan['fingerprint'] = expected = C.fingerprint({k: v for k, v in plan.items() if k != 'fingerprint'})
            with patch.object(C.os, 'unlink') as unlink:
                with self.assertRaises(C.Failure):
                    self.cache.prune(plan, expected)
                unlink.assert_not_called()

    def test_count_and_bytes_bounded(self):
        (self.shard / ('a' * 63 + 'b-d')).write_bytes(b'synthetic build output')
        for field, limit in (('MAX_FILES', 0), ('MAX_BYTES', 30)):
            with patch.object(C, field, limit):
                with self.assertRaises(C.Failure):
                    self.cache.audit()

    def test_directory_replacement_fails_closed(self):
        plan = self.cache.audit()
        self.shard.rename(self.root / 'saved')
        self.shard.mkdir()
        with self.assertRaises(C.Failure):
            self.cache.prune(plan, plan['fingerprint'])
        self.assertTrue((self.root / 'saved' / self.old.name).exists())

    def test_plan_file_private_bounded_and_duplicate_keys(self):
        plan = self.cache.audit()
        path = self.root.parent / 'plan.json'
        path.write_text(json.dumps(plan))
        path.chmod(0o600)
        self.assertEqual(C.load_plan(path), plan)
        path.chmod(0o644)
        with self.assertRaises(C.Failure):
            C.load_plan(path)
        path.chmod(0o600)
        path.write_text('{"x":1,"x":2}')
        with self.assertRaises(C.Failure):
            C.load_plan(path)

    def test_cli_no_alternate_root_no_raw_errors(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(C.main(['audit', '--root', '/PRIVATE_CANARY']), 1)
        self.assertNotIn('PRIVATE_CANARY', output.getvalue())
        self.assertEqual(json.loads(output.getvalue())['removedCount'], 0)


if __name__ == '__main__':
    unittest.main()
