"""Проверки закрытого cleanup на synthetic fixtures, без реального prune."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('material_cache', Path(__file__).with_name('local-material-install-cache.py'))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class MaterialCacheTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix='kodex-material-test-')
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name) / 'material'
        self.root.mkdir(mode=0o700)
        self.directory = self.root / '.install.Abc123'
        self.directory.mkdir(mode=0o700)
        (self.directory / 'nsc').mkdir(mode=0o775)
        self.old = self.directory / 'cosign'
        self.old.write_bytes(b'synthetic public binary')
        (self.directory / 'nsc.zip').write_bytes(b'synthetic installer zip')
        (self.directory / 'nsc/nsc').write_bytes(b'synthetic nsc binary')
        (self.root / 'cosign').write_bytes(b'active installed binary remains')
        self.now = time.time_ns() + M.AGE_NS + 3600 * 10**9
        clock = patch.object(M.time, 'time_ns', return_value=self.now)
        clock.start()
        self.addCleanup(clock.stop)
        self.check = lambda: None
        self.cache = M.Cache(self.root, process_check=self.check)

    def test_audit_readonly_then_explicit_unlink_only_cosign(self):
        before = M.C.snapshot(self.old.stat())
        plan = self.cache.audit()
        self.assertEqual(M.C.snapshot(self.old.stat()), before)
        output = self.root.parent / 'plan.json'
        M.C.write_plan(str(output), plan)
        self.assertEqual(output.stat().st_mode & 0o777, 0o600)
        loaded = M.C.load_plan(str(output))
        result = self.cache.prune(loaded, plan['fingerprint'])
        self.assertEqual(result['removedCount'], 1)
        self.assertFalse(self.old.exists())
        for item in ('nsc.zip', 'nsc/nsc'):
            self.assertTrue((self.directory / item).exists())
        self.assertTrue((self.root / 'cosign').exists())
        self.assertTrue(self.directory.is_dir())
        self.assertEqual(self.cache.audit()['records'], [])

    def test_invalid_layout_secret_name_special_link_and_fresh_file_deny(self):
        for case in ('secret', 'symlink', 'hardlink', 'fifo', 'fresh'):
            with self.subTest(case=case):
                extra = self.directory / '.env'
                saved = self.root / 'saved'
                if case == 'secret':
                    extra.write_bytes(b'synthetic private marker')
                elif case in ('symlink', 'hardlink', 'fifo'):
                    self.old.rename(saved)
                    if case == 'symlink':
                        self.old.symlink_to(saved)
                    elif case == 'hardlink':
                        os.link(saved, self.old)
                    else:
                        os.mkfifo(self.old)
                else:
                    os.utime(self.old, ns=(self.now, self.now))
                with self.assertRaises(M.C.Failure):
                    self.cache.audit()
                if extra.exists():
                    extra.unlink()
                if saved.exists():
                    self.old.unlink()
                    saved.rename(self.old)
                os.utime(self.old, ns=(1, 1))

    def test_world_writable_or_foreign_owner_rejected(self):
        self.directory.chmod(0o777)
        with self.assertRaises(M.C.Failure):
            self.cache.audit()
        self.directory.chmod(0o700)
        with patch.object(M.C.os, 'getuid', return_value=os.getuid() + 1):
            with self.assertRaises(M.C.Failure):
                self.cache.audit()

    def test_fingerprint_escape_duplicate_scope_expiry_and_budget_deny_before_unlink(self):
        for case in ('fingerprint', 'escape', 'duplicate', 'scope', 'expired', 'budget'):
            plan = self.cache.audit()
            expected = plan['fingerprint']
            if case == 'fingerprint':
                expected = '0' * 64
            elif case == 'escape':
                plan['records'][0]['path'] = '../cosign'
            elif case == 'duplicate':
                plan['records'] *= 2
            elif case == 'scope':
                plan['root'] = '/foreign'
            elif case == 'expired':
                plan['createdAtNs'] -= M.C.PLAN_AGE_NS + 1
            else:
                plan['records'][0]['stat'][6] = M.MAX_BYTES + 1
            if case != 'fingerprint':
                plan['fingerprint'] = expected = M.C.fingerprint({k: v for k, v in plan.items() if k != 'fingerprint'})
            with patch.object(M.os, 'unlink') as unlink:
                with self.assertRaises(M.C.Failure):
                    self.cache.prune(plan, expected)
                unlink.assert_not_called()

    def test_all_three_timestamps_and_directory_replacement_deny_before_effect(self):
        for changed in ('atime', 'mtime', 'ctime', 'directory'):
            plan = self.cache.audit()
            if changed == 'directory':
                self.directory.rename(self.root / '.saved')
                self.directory.mkdir(mode=0o700)
            elif changed == 'ctime':
                self.old.chmod(0o600)
            else:
                info = self.old.stat()
                os.utime(self.old, ns=(self.now if changed == 'atime' else info.st_atime_ns,
                                      self.now if changed == 'mtime' else info.st_mtime_ns))
            with patch.object(M.os, 'unlink') as unlink:
                with self.assertRaises(M.C.Failure):
                    self.cache.prune(plan, plan['fingerprint'])
                unlink.assert_not_called()
            if changed == 'directory':
                self.directory.rmdir()
                (self.root / '.saved').rename(self.directory)
            os.utime(self.old, ns=(1, 1))

    def test_in_use_or_inaccessible_process_proof_deny_before_effect(self):
        plan = self.cache.audit()
        def unavailable():
            raise M.C.Failure('PROCESS_SCAN_DENIED')
        self.cache.process_check = unavailable
        with patch.object(M.os, 'unlink') as unlink:
            with self.assertRaises(M.C.Failure):
                self.cache.prune(plan, plan['fingerprint'])
            unlink.assert_not_called()
        for data in ({'status': 'PASS', 'checkedProcesses': 3, 'matchedReferences': 1},
                     {'status': 'PASS', 'checkedProcesses': 0, 'matchedReferences': 0}):
            result = subprocess.CompletedProcess([], 0, json.dumps(data).encode())
            with patch.object(M.subprocess, 'run', return_value=result):
                with self.assertRaises(M.C.Failure):
                    M.privileged_process_check()

    def test_process_scan_fd_and_mapped_inode_match_without_command_or_env_reads(self):
        proc = self.root.parent / 'proc'
        pid = proc / '100'
        (pid / 'fd').mkdir(parents=True)
        (pid / 'fd/3').symlink_to(self.old)
        (pid / 'cwd').symlink_to(self.root.parent)
        (pid / 'exe').symlink_to(self.root / 'cosign')
        info = self.old.stat()
        (pid / 'maps').write_text(f'1-2 r--p 00000000 {os.major(info.st_dev):x}:{os.minor(info.st_dev):x} {info.st_ino} /other-namespace/cosign\n')
        result = M.process_scan(self.root, proc)
        self.assertGreaterEqual(result['matchedReferences'], 2)
        with patch.object(Path, 'open', side_effect=PermissionError):
            with self.assertRaisesRegex(M.C.Failure, 'PROCESS_SCAN_DENIED'):
                M.process_scan(self.root, proc)

    def test_process_scanner_cannot_get_write_path_and_prune_needs_apply(self):
        for args in (['prune'], ['prune', '--apply', '--output-plan', '/PRIVATE_CANARY'],
                     ['process-check', '--apply'], ['process-check', '--output-plan', '/PRIVATE_CANARY'],
                     ['audit', '--root', '/PRIVATE_CANARY']):
            with patch.object(M.Cache, 'prune') as prune, patch.object(M.C, 'load_plan') as load:
                output = io.StringIO()
                with contextlib.redirect_stdout(output):
                    self.assertEqual(M.main(args), 1)
                self.assertNotIn('PRIVATE_CANARY', output.getvalue())
                prune.assert_not_called()
                load.assert_not_called()

    def test_installer_single_exit_trap_cleans_download_and_readback_on_success_or_failure(self):
        source = Path(__file__).with_name('install-user-material-tools.sh').read_text()
        self.assertEqual(len(re.findall(r'^trap .* EXIT$', source, re.MULTILINE)), 1)
        function = source[source.index('cleanup_installation() {'):]
        function = function[:function.index('\n}\n') + 3]
        for code in (0, 7):
            install = self.root.parent / 'install-fixture'
            readback = self.root.parent / 'readback-fixture'
            install.mkdir()
            readback.mkdir()
            (install / 'cosign').write_bytes(b'synthetic temporary output')
            result = subprocess.run(['bash', '--noprofile', '--norc', '-c',
                function + '\ntemporary_directory=$1\nreadback_directory=$2\ntrap cleanup_installation EXIT\nexit "$3"',
                'fixture', str(install), str(readback), str(code)], check=False)
            self.assertEqual(result.returncode, code)
            self.assertFalse(install.exists())
            self.assertFalse(readback.exists())
            self.assertTrue((self.root / 'cosign').exists())


if __name__ == '__main__':
    unittest.main()
