"""Отбор node cache сохраняет текущие consumers и не удаляет неизвестные aliases."""

import importlib.util
from pathlib import Path
import unittest
import io
import sys
import tempfile
import contextlib
import os
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('node_cache', Path(__file__).with_name('node-runner-cache.py'))
C = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(C)


class CacheTests(unittest.TestCase):
    def test_delivered_factory_and_base_pins_are_projected_without_other_data(self):
        digest = 'sha256:' + 'a' * 64
        image = 'example.invalid/kodex/agent-runner@' + digest
        def command(args):
            if 'configmap' in args:
                self.assertIn('kodex-image-admission-policy-aaaaaaaa', args)
                self.assertEqual(args[-1], 'jsonpath={.data.nodeReadbackImage}{" "}{.data.trustedRoleBaseDigest}')
                return (image + ' ' + digest).encode()
            self.assertNotIn('.env[*].value}', args[-1])
            return ('synthetic|Running|' + digest + ' kodex-image-admission-policy-aaaaaaaa').encode()
        with patch.object(C, 'run', side_effect=command):
            self.assertEqual(C.delivered_pins(['kubectl']), {digest, image})
        item = {'name': C.PREFIX + digest, 'digest': digest}
        self.assertEqual(C.selected([item], {digest}), [])

    def test_same_policy_name_is_bound_to_actual_namespace(self):
        first, second = 'sha256:' + 'a' * 64, 'sha256:' + 'b' * 64
        seen = set()
        def command(args):
            if 'configmap' in args:
                namespace = args[args.index('-n') + 1]
                seen.add(namespace)
                return (first if namespace == 'project-one' else second).encode()
            return b'project-one||kodex-image-admission-policy-aaaaaaaa\nproject-two||kodex-image-admission-policy-aaaaaaaa\n'
        with patch.object(C, 'run', side_effect=command):
            self.assertEqual(C.delivered_pins(['kubectl']), {first, second})
        self.assertEqual(seen, {'project-one', 'project-two'})

    def test_oversized_helper_rejected_before_payload_read_or_go_probe(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'helper'
            with path.open('wb') as stream:
                stream.truncate(33 << 20)
            with patch.object(Path, 'read_bytes', side_effect=AssertionError('unbounded read')), patch.object(C.images, 'command') as command:
                with self.assertRaises(ValueError):
                    C.images.helper_file(path, 'synthetic', 'synthetic')
                command.assert_not_called()

    def test_main_export_streams_actual_producer_into_verified_owned_archive(self):
        from test_node_runner_archive import ArchiveTests
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original, digest = ArchiveTests().fixture(directory)
            archives = root / 'archives'; archives.mkdir(mode=0o700)
            node = {'name': 'synthetic', 'uid': 'synthetic-uid', 'container': 'synthetic-container'}
            plan = {'clusterUID': 'synthetic-cluster', 'helperSHA256': 'synthetic-helper',
                    'nodes': [dict(node, images=[{'digest': digest, 'ref': C.PREFIX + digest}])]}
            argv = ['node-runner-cache.py', 'export', '--kubectl', 'synthetic', '--kubeconfig', 'synthetic',
                    '--context', 'k3d-kodex', '--cluster-uid', 'synthetic-cluster', '--helper-sha256', 'synthetic-helper',
                    '--helper', str(root / 'helper'), '--plan', str(root / 'plan'), '--archives', str(archives),
                    '--journal', str(root / 'journal'), '--plan-sha256', 'synthetic-plan']
            actual_export = C.export_bytes
            def producer(command, output, **kwargs):
                self.assertEqual(command[-1], C.PREFIX + digest)
                return actual_export([sys.executable, '-c', 'import sys;sys.stdout.buffer.write(open(sys.argv[1],"rb").read())', str(original)], output, **kwargs)
            with patch.object(sys, 'argv', argv), patch.object(C, 'git_source', return_value='synthetic-source'), \
                 patch.object(C.images, 'helper_file', return_value=b'synthetic'), patch.object(C, 'target', return_value=([node], set())), \
                 patch.object(C, 'read_plan', return_value=plan), patch.object(C, 'export_bytes', side_effect=producer), \
                 contextlib.redirect_stdout(io.StringIO()):
                C.main()
            output = archives / (digest[7:] + '.oci.tar')
            self.assertEqual(output.read_bytes(), original.read_bytes())
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            self.assertFalse((root / 'journal').exists())

    def test_actual_export_process_has_byte_budget_and_positive_join(self):
        output = io.BytesIO()
        self.assertEqual(C.export_bytes([sys.executable, '-c', 'print("synthetic")'], output, limit=64, seconds=2), 10)
        self.assertEqual(output.getvalue(), b'synthetic\n')
        output = io.BytesIO()
        with self.assertRaises(ValueError):
            C.export_bytes([sys.executable, '-c', 'print("x"*1024)'], output, limit=64, seconds=2)
        self.assertLessEqual(len(output.getvalue()), 64)

    def test_actual_export_timeout_joins_silent_producer(self):
        with self.assertRaises(ValueError):
            C.export_bytes([sys.executable, '-c', 'import time; time.sleep(5)'], io.BytesIO(), seconds=0.05)

    def test_projection_retains_pending_and_init_images_without_private_payload(self):
        raw = b'uid-one|Pending|||example.invalid/init example.invalid/main docker-pullable://example.invalid/main@sha256:aaa containerd://sha256:bbb docker://sha256:ccc\nuid-two|Failed|||example.invalid/old\n'
        with patch.object(C, 'run', return_value=raw) as command:
            refs = C.workload_refs(['kubectl', '--context', 'synthetic'], 'pods')
        self.assertEqual(refs, {'example.invalid/init', 'example.invalid/main', 'example.invalid/main@sha256:aaa', 'sha256:bbb', 'sha256:ccc'})
        template = command.call_args.args[0][-1]
        self.assertNotIn('env', template)
        self.assertNotIn('annotations', template)
        for field in ('ephemeralContainers', 'initContainerStatuses', 'ephemeralContainerStatuses'):
            self.assertIn(field, template)

    def test_projection_malformed_or_missing_container_images_rejected(self):
        for raw in (b'uid|Pending|||\n', b'uid|Pending|||image|unexpected\n'):
            with patch.object(C, 'run', return_value=raw), self.assertRaises(ValueError):
                C.workload_refs(['kubectl'], 'pods')

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
