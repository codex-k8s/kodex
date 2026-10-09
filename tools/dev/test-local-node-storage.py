"""Герметичная оснастка переноса ноды; живые Docker/systemd не вызываются."""

import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch


SPEC = importlib.util.spec_from_file_location(
    'local_node_storage', Path(__file__).with_name('local-node-storage.py'))
STORAGE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(STORAGE)
SHA = 'a' * 40
IMAGE = 'sha256:' + 'b' * 64
OTHER_IMAGE = 'sha256:' + 'c' * 64
ORIGINAL = {'device': os.stat('/').st_dev, 'inode': 101}
DESTINATION = {'device': ORIGINAL['device'] + 1, 'inode': 202}
SCRIPT = b'fixture script\n'


def value(phase='VERIFIED'):
    return {'phase': phase, 'fingerprint': 'fixture-fingerprint',
            'container': STORAGE.CONTAINER, 'volume': STORAGE.VOLUME,
            'sourceIdentity': dict(ORIGINAL), 'targetIdentity': dict(DESTINATION),
            'scriptSha256': hashlib.sha256(SCRIPT).hexdigest(),
            'sourceSha': SHA, 'pinnedImages': [IMAGE]}


class MockTests(unittest.TestCase):
    def setUp(self):
        self.stack = contextlib.ExitStack()
        self.addCleanup(self.stack.close)
        # Любой забытый внешний вызов закрыто останавливает тест до эффекта.
        self.run = self.stack.enter_context(patch.object(
            STORAGE, 'run', side_effect=AssertionError('UNMOCKED_COMMAND')))

    def mock(self, name, **kwargs):
        return self.stack.enter_context(patch.object(STORAGE, name, **kwargs))

    def failure(self, code, callback):
        with self.assertRaisesRegex(STORAGE.Failure, '^' + code + '$'):
            callback()

    def audit_fixture(self):
        self.mock('git_head', return_value=SHA)
        for name in ('JOURNAL', 'BACKUP', 'TARGET', 'INSTALLED'):
            self.mock(name, new=Mock(exists=Mock(return_value=False)))
        units = Mock()
        units.__truediv__ = Mock(return_value=Mock(exists=Mock(return_value=False)))
        self.mock('UNITS', new=units)
        self.mock('data_mount', return_value=DESTINATION)
        self.mock('directory', return_value=ORIGINAL)
        self.mock('mounts_under', return_value=[])
        self.mock('inspect', return_value={'running': True, 'pid': 123,
                                           'policy': 'unless-stopped'})
        self.mock('exclusive_volume')
        self.mock('quiescent')
        self.mock('pinned', return_value=[IMAGE])
        self.stack.enter_context(patch.object(
            STORAGE.shutil, 'disk_usage', return_value=SimpleNamespace(free=STORAGE.BUFFER + 100)))
        self.run.side_effect = lambda args, **kwargs: '100\n' if args[0] == '/usr/bin/du' else self.fail('Unexpected command')

    def guard_fixture(self):
        self.mock('data_mount', return_value=DESTINATION)
        self.mock('directory', return_value=DESTINATION)
        self.mock('file_content', return_value=SCRIPT)

    def readback_fixture(self):
        self.mock('mounted')
        self.mock('inspect', return_value={'running': True, 'pid': 123, 'policy': 'no'})
        self.mock('pinned', return_value=[IMAGE, OTHER_IMAGE])
        contents = {STORAGE.UNITS / name: text.encode() for name, text in STORAGE.units().items()}
        self.mock('file_content', side_effect=lambda path: contents[path])

        def command(args, **kwargs):
            if args[:2] == ['/usr/bin/docker', 'exec']:
                return f"{DESTINATION['device']}:{DESTINATION['inode']}\n"
            if args[0] == '/usr/bin/systemctl':
                self.assertIn(args[1], ('is-active', 'is-enabled'))
                return ''
            if args[0] == STORAGE.KUBECTL[0]:
                return json.dumps({'status': {'conditions': [{'type': 'Ready', 'status': 'True'}]}})
            self.fail('Unexpected command')
        self.run.side_effect = command

    def test_audit_binds_exact_source_and_no_effects(self):
        self.audit_fixture()
        result = STORAGE.audit(SHA)
        self.assertEqual(result['phase'], 'AUDITED')
        self.assertEqual(result['pinnedImages'], [IMAGE])
        binding = {key: result[key] for key in ('container', 'volume', 'sourceIdentity',
                                               'dataIdentity', 'sourceSha', 'scriptSha256')}
        self.assertEqual(result['fingerprint'], STORAGE.digest(binding))
        self.assertEqual(self.run.call_count, 1)

    def test_audit_rejects_capacity_mount_and_existing_migration(self):
        self.audit_fixture()
        with patch.object(STORAGE.shutil, 'disk_usage', return_value=SimpleNamespace(free=STORAGE.BUFFER + 99)):
            self.failure('TARGET_CAPACITY_INSUFFICIENT', lambda: STORAGE.audit(SHA))
        with patch.object(STORAGE, 'mounts_under', return_value=[str(STORAGE.SOURCE / 'nested')]):
            self.failure('SOURCE_ALREADY_MOUNTED', lambda: STORAGE.audit(SHA))
        with patch.object(STORAGE.JOURNAL, 'exists', return_value=True):
            self.failure('MIGRATION_ALREADY_PRESENT', lambda: STORAGE.audit(SHA))

    def test_audit_rejects_foreign_device_and_restart_policy(self):
        self.audit_fixture()
        with patch.object(STORAGE, 'directory', return_value=DESTINATION):
            self.failure('DEVICE_MISMATCH', lambda: STORAGE.audit(SHA))
        for node in ({'running': False, 'pid': 0, 'policy': 'unless-stopped'},
                     {'running': True, 'pid': 123, 'policy': 'always'}):
            with self.subTest(node=node), patch.object(STORAGE, 'inspect', return_value=node):
                self.failure('NODE_NOT_ORIGINAL', lambda: STORAGE.audit(SHA))

    def test_guard_phase_target_and_script_are_pinned(self):
        self.guard_fixture()
        for phase in ('COPY_VERIFIED', 'SWITCHING', 'SWITCHED', 'NODE_STARTING', 'VERIFIED', 'RETIRED'):
            STORAGE.guard(value(phase))
        for phase in ('AUDITED', 'PREPARED', 'NODE_STOPPED', 'ROLLED_BACK', 'unknown'):
            with self.subTest(phase=phase):
                self.failure('COPY_NOT_VERIFIED', lambda: STORAGE.guard(value(phase)))
        with patch.object(STORAGE, 'directory', return_value=ORIGINAL):
            self.failure('TARGET_CHANGED', lambda: STORAGE.guard(value()))
        with patch.object(STORAGE, 'file_content', return_value=b'changed'):
            self.failure('GUARD_CHANGED', lambda: STORAGE.guard(value()))
        self.run.assert_not_called()

    def test_mounted_requires_exact_root_mount_and_inode(self):
        self.mock('guard')
        self.mock('directory', return_value=DESTINATION)
        self.mock('mounts_under', return_value=[str(STORAGE.SOURCE)])
        STORAGE.mounted(value())
        for mounts in ([], [str(STORAGE.SOURCE / 'nested')]):
            with self.subTest(mounts=mounts), patch.object(STORAGE, 'mounts_under', return_value=mounts):
                self.failure('BIND_MOUNT_MISMATCH', lambda: STORAGE.mounted(value()))
        with patch.object(STORAGE, 'directory', return_value=ORIGINAL):
            self.failure('BIND_MOUNT_MISMATCH', lambda: STORAGE.mounted(value()))

    def test_readback_preserves_all_pinned_images(self):
        self.readback_fixture()
        self.assertEqual(STORAGE.readback(value())['pinnedImageCount'], 1)
        with patch.object(STORAGE, 'pinned', return_value=[OTHER_IMAGE]):
            self.failure('PINNED_IMAGE_LOST', lambda: STORAGE.readback(value()))

    def test_readback_rejects_wrong_node_storage_and_changed_units(self):
        self.readback_fixture()
        original_command = self.run.side_effect
        self.run.side_effect = lambda args, **kwargs: '0:0\n' if args[0] == '/usr/bin/docker' else original_command(args, **kwargs)
        self.failure('NODE_STORAGE_NOT_TARGET', lambda: STORAGE.readback(value()))
        self.run.side_effect = original_command
        with patch.object(STORAGE, 'file_content', return_value=b'changed'):
            self.failure('UNIT_CHANGED', lambda: STORAGE.readback(value()))

    def test_units_manage_only_node_not_global_docker(self):
        generated = STORAGE.units()
        self.assertEqual(set(generated), {STORAGE.GUARD, STORAGE.MOUNT, STORAGE.SERVICE})
        self.assertIn('BindsTo=' + STORAGE.MOUNT, generated[STORAGE.SERVICE])
        self.assertIn('Requires=docker.service ' + STORAGE.MOUNT, generated[STORAGE.SERVICE])
        self.assertIn('docker start --attach ' + STORAGE.CONTAINER, generated[STORAGE.SERVICE])
        for text in generated.values():
            self.assertNotIn('Before=docker.service', text)
            self.assertNotIn('PartOf=docker.service', text)
            self.assertNotIn('systemctl restart docker', text)
        self.assertIn('AssertPathIsMountPoint=/data', generated[STORAGE.GUARD])
        self.assertIn('Options=bind', generated[STORAGE.MOUNT])

    def test_activation_verifies_generated_fstab_units_before_effects(self):
        self.run.side_effect = None
        self.run.return_value = ''
        self.mock('mounted')
        self.mock('save')
        self.mock('readback', return_value={'status': 'PASS'})
        self.assertEqual(STORAGE.activate(value('SWITCHING'))['status'], 'PASS')
        arguments = self.run.call_args_list[0].args[0]
        self.assertEqual(arguments[:3], ['/usr/bin/systemd-analyze', 'verify', '--generators=yes'])

    def test_resume_rejects_started_phase_or_wrong_fingerprint_before_effects(self):
        self.mock('git_head', return_value=SHA)
        for phase in ('NODE_STARTING', 'VERIFIED', 'RETIRED', 'AUDITED'):
            self.failure('RESUME_PHASE_UNSUPPORTED',
                         lambda: STORAGE.resume(value(phase), SHA, 'fixture-fingerprint'))
        self.failure('RESUME_PHASE_UNSUPPORTED',
                     lambda: STORAGE.resume(value('SWITCHING'), SHA, 'wrong'))
        self.run.assert_not_called()

    def test_resume_verifies_copy_again_and_never_repeats_rename_or_copy(self):
        self.mock('git_head', return_value=SHA)
        self.mock('inspect', return_value={'running': False, 'pid': 0})
        self.mock('guard')
        self.mock('directory', return_value=ORIGINAL)
        self.mock('no_references')
        self.mock('save')
        contents = {STORAGE.UNITS / name: text.encode() for name, text in STORAGE.units().items()}
        self.mock('file_content', side_effect=lambda path: contents[path])
        activation = self.mock('activate', return_value={'status': 'PASS'})
        self.run.side_effect = None
        self.run.return_value = ''
        self.assertEqual(STORAGE.resume(value('SWITCHING'), SHA, 'fixture-fingerprint')['status'], 'PASS')
        args = self.run.call_args.args[0]
        self.assertIn('--checksum', args)
        self.assertIn('--dry-run', args)
        self.assertIn('--delete', args)
        self.assertTrue(self.run.call_args.kwargs['empty'])
        activation.assert_called_once()
        activation.reset_mock()
        self.run.side_effect = STORAGE.Failure('OUTPUT_MISMATCH')
        self.failure('OUTPUT_MISMATCH',
                     lambda: STORAGE.resume(value('SWITCHING'), SHA, 'fixture-fingerprint'))
        activation.assert_not_called()

    def test_copy_delete_is_dry_run_and_requires_empty_output(self):
        self.run.side_effect = None
        self.run.return_value = ''
        STORAGE.copy_verified(Path('/fixture/source'), Path('/fixture/target'))
        calls = self.run.call_args_list
        self.assertEqual(len(calls), 3)
        self.assertNotIn('--delete', calls[0].args[0])
        self.assertIn('--delete', calls[1].args[0])
        self.assertIn('--dry-run', calls[1].args[0])
        self.assertIn('--checksum', calls[1].args[0])
        self.assertIs(calls[1].kwargs['empty'], True)
        self.assertEqual(calls[2].args[0], ['/usr/bin/sync', '-f', '/fixture/target'])

    def test_copy_failed_verification_does_not_sync(self):
        self.run.side_effect = ['', STORAGE.Failure('OUTPUT_MISMATCH')]
        self.failure('OUTPUT_MISMATCH', lambda: STORAGE.copy_verified(Path('/fixture/source'), Path('/fixture/target')))
        self.assertEqual(self.run.call_count, 2)

    def test_retire_requires_verified_fingerprint_and_exact_backup(self):
        readback = self.mock('readback', return_value={'status': 'PASS'})
        directory = self.mock('directory', return_value=ORIGINAL)
        references = self.mock('no_references')
        save = self.mock('save')
        remove = self.stack.enter_context(patch.object(STORAGE.shutil, 'rmtree'))
        remove.avoids_symlink_attacks = True
        self.stack.enter_context(patch.object(STORAGE.shutil, 'disk_usage', return_value=SimpleNamespace(free=1)))
        for phase, fingerprint in (('SWITCHED', 'fixture-fingerprint'), ('VERIFIED', 'wrong'), ('RETIRED', 'wrong')):
            self.failure('RETIRE_NOT_VERIFIED', lambda: STORAGE.retire(value(phase), fingerprint))
        remove.assert_not_called()
        with patch.object(STORAGE, 'directory', return_value=DESTINATION):
            self.failure('BACKUP_CHANGED', lambda: STORAGE.retire(value(), 'fixture-fingerprint'))
        remove.assert_not_called()
        result = STORAGE.retire(value(), 'fixture-fingerprint')
        self.assertTrue(result['removedVerifiedOriginalCopy'])
        directory.assert_called_with(STORAGE.BACKUP)
        references.assert_called_once_with(STORAGE.BACKUP)
        remove.assert_called_once_with(STORAGE.BACKUP)
        save.assert_called_once()
        self.assertGreaterEqual(readback.call_count, 2)

    def test_retire_replay_without_backup_is_readback_only(self):
        self.mock('BACKUP', new=Mock(exists=Mock(return_value=False)))
        readback = self.mock('readback', return_value={'status': 'PASS'})
        directory = self.mock('directory')
        save = self.mock('save')
        remove = self.stack.enter_context(patch.object(STORAGE.shutil, 'rmtree'))
        result = STORAGE.retire(value('RETIRED'), 'fixture-fingerprint')
        self.assertFalse(result['effectPerformed'])
        self.assertEqual(readback.call_count, 2)
        directory.assert_not_called()
        save.assert_not_called()
        remove.assert_not_called()

    def test_retire_in_use_or_failed_readback_never_deletes(self):
        self.mock('readback')
        self.mock('directory', return_value=ORIGINAL)
        self.mock('no_references', side_effect=STORAGE.Failure('SOURCE_IN_USE'))
        remove = self.stack.enter_context(patch.object(STORAGE.shutil, 'rmtree'))
        self.failure('SOURCE_IN_USE', lambda: STORAGE.retire(value(), 'fixture-fingerprint'))
        remove.assert_not_called()
        with patch.object(STORAGE, 'readback', side_effect=STORAGE.Failure('PINNED_IMAGE_LOST')):
            self.failure('PINNED_IMAGE_LOST', lambda: STORAGE.retire(value(), 'fixture-fingerprint'))
        remove.assert_not_called()

    def test_rollback_invalid_phase_or_fingerprint_has_no_effects(self):
        for phase in ('AUDITED', 'ROLLED_BACK', 'unknown'):
            with self.subTest(phase=phase):
                self.failure('ROLLBACK_PHASE_UNSUPPORTED', lambda: STORAGE.rollback(value(phase), 'fixture-fingerprint'))
        self.failure('FINGERPRINT_CHANGED', lambda: STORAGE.rollback(value('COPY_VERIFIED'), 'wrong'))
        self.run.assert_not_called()

    def rollback_fixture(self):
        units = Mock()
        units.__truediv__ = Mock(return_value=Mock(exists=Mock(return_value=False)))
        self.mock('UNITS', new=units)
        self.mock('inspect', return_value={'running': False, 'pid': 0, 'policy': 'no'})
        self.mock('mounted')
        self.mock('no_references')
        self.mock('directory', return_value=ORIGINAL)
        source = self.mock('SOURCE', new=Mock(iterdir=Mock(return_value=[])))
        reverse = self.mock('REVERSE', new=Mock(exists=Mock(return_value=False)))
        backup = self.mock('BACKUP', new=Mock(exists=Mock(return_value=True)))
        self.mock('save')
        self.mock('copy_verified')
        self.run.side_effect = lambda args, **kwargs: '100\n' if args[0] == '/usr/bin/du' else ''
        self.stack.enter_context(patch.object(
            STORAGE.shutil, 'disk_usage', return_value=SimpleNamespace(free=STORAGE.BUFFER + 100)))
        return source, reverse, backup

    def test_rollback_after_node_start_copies_current_data_not_stale_original(self):
        source, reverse, backup = self.rollback_fixture()
        for phase in ('NODE_STARTING', 'VERIFIED', 'RETIRED'):
            with self.subTest(phase=phase):
                STORAGE.copy_verified.reset_mock()
                self.assertEqual(STORAGE.rollback(value(phase), 'fixture-fingerprint')['phase'], 'ROLLED_BACK')
                STORAGE.copy_verified.assert_called_once_with(STORAGE.TARGET, reverse)
                reverse.rename.assert_called_with(source)
                backup.rename.assert_not_called()

    def test_rollback_capacity_and_existing_reverse_preserve_copies(self):
        source, reverse, backup = self.rollback_fixture()
        with patch.object(STORAGE.shutil, 'disk_usage', return_value=SimpleNamespace(free=STORAGE.BUFFER + 99)):
            self.failure('REVERSE_CAPACITY_INSUFFICIENT', lambda: STORAGE.rollback(value(), 'fixture-fingerprint'))
        reverse.mkdir.assert_not_called()
        with patch.object(reverse, 'exists', return_value=True):
            self.failure('REVERSE_COPY_ALREADY_PRESENT', lambda: STORAGE.rollback(value(), 'fixture-fingerprint'))
        STORAGE.copy_verified.assert_not_called()
        source.rmdir.assert_not_called()
        backup.rename.assert_not_called()

    def test_rollback_before_node_start_restores_exact_original(self):
        source, reverse, backup = self.rollback_fixture()
        self.mock('mounts_under', return_value=[])
        self.assertEqual(STORAGE.rollback(value('COPY_VERIFIED'), 'fixture-fingerprint')['phase'], 'ROLLED_BACK')
        backup.rename.assert_called_once_with(source)
        reverse.rename.assert_not_called()
        STORAGE.copy_verified.assert_not_called()

    def test_data_mount_requires_exact_device_filesystem_and_target(self):
        self.mock('directory', return_value=DESTINATION)
        exact = {'filesystems': [{'source': '/dev/nvme1n1p3', 'fstype': 'ext4',
                                  'maj:min': '259:4', 'target': '/data'}]}
        self.run.side_effect = None
        self.run.return_value = json.dumps(exact)
        self.assertEqual(STORAGE.data_mount(), DESTINATION)
        for key, replacement in (('source', '/dev/foreign'), ('fstype', 'tmpfs'),
                                 ('maj:min', '9:3'), ('target', '/')):
            wrong = json.loads(json.dumps(exact))
            wrong['filesystems'][0][key] = replacement
            self.run.return_value = json.dumps(wrong)
            with self.subTest(key=key):
                self.failure('DATA_MOUNT_CHANGED', STORAGE.data_mount)

    def test_inspect_rejects_changed_node_volume_or_missing_fields(self):
        mount = {'Destination': '/var/lib/rancher/k3s', 'Type': 'volume', 'Driver': 'local',
                 'Name': STORAGE.VOLUME, 'Source': str(STORAGE.SOURCE), 'RW': True}
        fields = [STORAGE.CONTAINER, True, 123, 'unless-stopped', [mount], 'kodex', 'server']
        self.run.side_effect = None
        encode = lambda data: '\n'.join(json.dumps(item) for item in data)
        self.run.return_value = encode(fields)
        self.assertTrue(STORAGE.inspect()['running'])
        for index, replacement in ((0, 'f' * 64), (5, 'foreign'), (6, 'agent')):
            wrong = list(fields)
            wrong[index] = replacement
            self.run.return_value = encode(wrong)
            with self.subTest(index=index):
                self.failure('NODE_CHANGED', STORAGE.inspect)
        for key, replacement in (('Name', 'foreign'), ('Source', '/foreign'), ('Driver', 'foreign'),
                                 ('Type', 'bind'), ('RW', False)):
            wrong = list(fields)
            wrong[4] = [{**mount, key: replacement}]
            self.run.return_value = encode(wrong)
            with self.subTest(key=key):
                self.failure('NODE_VOLUME_CHANGED', STORAGE.inspect)
        self.run.return_value = encode(fields[:-1])
        self.failure('NODE_FIELDS_INVALID', STORAGE.inspect)

    def test_pinned_requires_nonempty_closed_image_digests(self):
        self.run.side_effect = None
        self.run.return_value = json.dumps({'images': [{'id': IMAGE, 'pinned': True}]})
        self.assertEqual(STORAGE.pinned(), [IMAGE])
        for images in ([], [{'id': 'latest', 'pinned': True}], [{'id': IMAGE, 'pinned': False}]):
            self.run.return_value = json.dumps({'images': images})
            self.failure('PINNED_IMAGES_INVALID', STORAGE.pinned)

    def test_quiescent_rejects_active_core_or_runtime(self):
        core = {'items': [{'metadata': {'name': name}, 'spec': {'replicas': 0}} for name in STORAGE.CORE]}
        runtime = {'items': [{'status': {'phase': 'Succeeded'}}]}
        self.run.side_effect = [json.dumps(core), json.dumps(runtime)]
        STORAGE.quiescent()
        core['items'][0]['spec']['replicas'] = 1
        self.run.side_effect = [json.dumps(core)]
        self.failure('CORE_NOT_QUIESCENT', STORAGE.quiescent)
        core['items'][0]['spec']['replicas'] = 0
        runtime['items'][0]['status']['phase'] = 'Running'
        self.run.side_effect = [json.dumps(core), json.dumps(runtime)]
        self.failure('RUNTIME_NOT_QUIESCENT', STORAGE.quiescent)

    def test_exclusive_volume_rejects_foreign_exact_parent_and_child_binds(self):
        foreign = 'f' * 64
        for source in (str(STORAGE.SOURCE), str(STORAGE.SOURCE.parent),
                       str(STORAGE.SOURCE / 'child'), str(STORAGE.TARGET)):
            with self.subTest(source=source):
                self.run.side_effect = [foreign + '\n', json.dumps([{'Source': source}])]
                self.failure('VOLUME_NOT_EXCLUSIVE', STORAGE.exclusive_volume)
        self.run.side_effect = [foreign + '\n', json.dumps([{'Name': STORAGE.VOLUME, 'Source': '/unrelated'}])]
        self.failure('VOLUME_NOT_EXCLUSIVE', STORAGE.exclusive_volume)
        self.run.side_effect = ['invalid\n']
        self.failure('CONTAINER_ID_INVALID', STORAGE.exclusive_volume)

    def test_main_redacts_foreign_error_and_guard_avoids_operation_lock(self):
        self.stack.enter_context(patch.object(STORAGE.os, 'geteuid', return_value=0))
        self.mock('journal', side_effect=OSError('PRIVACY_SENTINEL'))
        with patch('sys.argv', ['local-node-storage.py', 'guard']), contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertEqual(STORAGE.main(), 1)
        result = json.loads(output.getvalue())
        self.assertEqual(result['error'], 'LOCAL_OPERATION_FAILED')
        self.assertNotIn('PRIVACY_SENTINEL', output.getvalue())
        self.run.assert_not_called()


class RunnerTests(unittest.TestCase):
    def test_runner_nonempty_output_and_failure_never_leak_payload(self):
        for code, empty, expected in ((1, False, 'COMMAND_FAILED'), (0, True, 'OUTPUT_MISMATCH')):
            class Process:
                pid = 123
                returncode = code

                def __init__(self, args, **kwargs):
                    kwargs['stdout'].write(b'PRIVACY_SENTINEL')
                    kwargs['stderr'].write(b'PRIVACY_SENTINEL')

                def poll(self):
                    return self.returncode

            with self.subTest(code=code), patch.object(STORAGE.subprocess, 'Popen', Process):
                with self.assertRaisesRegex(STORAGE.Failure, '^' + expected + '$') as caught:
                    STORAGE.run(['/fixture/command'], empty=empty)
                self.assertNotIn('PRIVACY_SENTINEL', str(caught.exception))


class DisposableCopyTests(unittest.TestCase):
    @unittest.skipUnless(Path('/usr/bin/rsync').is_file(), 'rsync is required')
    def test_copy_verified_hardlinks_symlink_xattr_sparse_and_dry_run_change(self):
        with tempfile.TemporaryDirectory(prefix='kodex-node-storage-test-') as base:
            root = Path(base)
            source, target = root / 'source', root / 'target'
            source.mkdir(mode=0o700)
            target.mkdir(mode=0o700)
            regular = source / 'regular'
            regular.write_bytes(b'fixture payload\n')
            regular.chmod(0o640)
            os.link(regular, source / 'hardlink')
            (source / 'symlink').symlink_to('regular')
            os.setxattr(regular, 'user.kodex_fixture', b'exact')
            with (source / 'sparse').open('wb') as stream:
                stream.write(b'begin')
                stream.seek(4 * 1024**2)
                stream.write(b'end')
            real_run = STORAGE.run
            commands = []

            def fixture_run(args, **kwargs):
                commands.append(args)
                self.assertIn(args[0], ('/usr/bin/rsync', '/usr/bin/sync'))
                for argument in args[1:]:
                    if str(argument).startswith('/'):
                        self.assertTrue(Path(argument).is_relative_to(root))
                return real_run(args, **kwargs)

            with patch.object(STORAGE, 'run', side_effect=fixture_run), contextlib.redirect_stdout(io.StringIO()):
                STORAGE.copy_verified(source, target)
            self.assertEqual((target / 'regular').read_bytes(), regular.read_bytes())
            self.assertEqual((target / 'hardlink').stat().st_ino, (target / 'regular').stat().st_ino)
            self.assertEqual(os.readlink(target / 'symlink'), 'regular')
            self.assertEqual(os.getxattr(target / 'regular', 'user.kodex_fixture'), b'exact')
            self.assertEqual(stat.S_IMODE((target / 'regular').stat().st_mode), 0o640)
            self.assertEqual((target / 'sparse').read_bytes(), (source / 'sparse').read_bytes())
            self.assertLess((target / 'sparse').stat().st_blocks * 512, (target / 'sparse').stat().st_size)
            (target / 'regular').write_bytes(b'changed payload\n')
            before = (target / 'regular').read_bytes()
            verification = next(args for args in commands if '--dry-run' in args)
            with self.assertRaisesRegex(STORAGE.Failure, '^OUTPUT_MISMATCH$'):
                real_run(verification, timeout=30, empty=True)
            self.assertEqual((target / 'regular').read_bytes(), before)
            extra = target / 'extra'
            extra.write_bytes(b'keep during dry run')
            with self.assertRaisesRegex(STORAGE.Failure, '^OUTPUT_MISMATCH$'):
                real_run(verification, timeout=30, empty=True)
            self.assertTrue(extra.exists())


if __name__ == '__main__':
    unittest.main()
