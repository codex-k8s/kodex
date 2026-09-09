#!/usr/bin/env python3
"""Герметичные OCI/CLI fixtures без Docker, registry, Pod и provider."""
import gzip
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from concurrent.futures import ThreadPoolExecutor

CLI = Path(__file__).with_name('runner-binary-provenance.py')
spec = importlib.util.spec_from_file_location('runner_provenance', CLI)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
TARGET = m.TARGET
BINARY = b'\x7fELF synthetic runner\x00\x01'


def tar_bytes(entries):
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode='w', format=tarfile.PAX_FORMAT) as tar:
        for name, body, kind, mode in entries:
            entry = tarfile.TarInfo(name)
            entry.mode = mode
            entry.type = kind
            if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
                entry.linkname = body.decode()
                body = b''
            entry.size = len(body)
            tar.addfile(entry, io.BytesIO(body))
    return out.getvalue()


def file(name=TARGET, body=BINARY, mode=0o555):
    return name, body, tarfile.REGTYPE, mode


def directory(name):
    return name, b'', tarfile.DIRTYPE, 0o755


class Verifier(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='runner-provenance-')
        self.root = Path(self.tmp.name)
        self.source = self.root / 'source'
        self.source.mkdir()
        for folder in m.INPUTS:
            p = self.source / folder
            p.mkdir(parents=True)
            (p / 'fixture.txt').write_text('source')
        self.git('init', '-q')
        self.git('add', '.')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture')
        self.revision = self.git('rev-parse', 'HEAD').strip()
        self.input_digest = m.source_input(str(self.source), self.revision)
        self.archive = self.root / f'agent-runner-{self.input_digest}.oci.tar'
        self.output = self.root / 'provenance.json'

    def tearDown(self):
        self.tmp.cleanup()

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.source, stderr=subprocess.DEVNULL, text=True)

    def fixture(self, layers=None, change=None, outer=None, gzip_layers=True):
        layers = layers if layers is not None else [[file()]]
        blobs = {}
        def blob(body, media):
            key = m.sha(body)
            blobs['blobs/sha256/' + key] = body
            return {'mediaType': media, 'digest': 'sha256:' + key, 'size': len(body)}
        layer_descriptors, diffids = [], []
        for entries in layers:
            plain = tar_bytes(entries)
            compressed = gzip.compress(plain, mtime=0) if gzip_layers else plain
            layer_descriptors.append(blob(compressed, m.OCI + 'layer.v1.tar' + ('+gzip' if gzip_layers else '')))
            diffids.append('sha256:' + m.sha(plain))
        config = {'architecture': 'amd64', 'os': 'linux', 'rootfs': {'type': 'layers', 'diff_ids': diffids}}
        manifest = {'schemaVersion': 2, 'mediaType': m.OCI + 'manifest.v1+json', 'config': None, 'layers': layer_descriptors}
        if change:
            change('config', config)
        manifest['config'] = blob(json.dumps(config).encode(), m.OCI + 'config.v1+json')
        if change:
            change('manifest', manifest)
        desc = blob(json.dumps(manifest).encode(), m.OCI + 'manifest.v1+json')
        self.expected = desc['digest']
        desc['annotations'] = {'io.containerd.image.name': 'registry.fixture.invalid/kodex/agent-runner:local-' + self.input_digest, 'org.opencontainers.image.ref.name': 'local-' + self.input_digest}
        index = {'schemaVersion': 2, 'mediaType': m.OCI + 'index.v1+json', 'manifests': [desc]}
        if change:
            change('index', index)
        entries = [file('oci-layout', b'{"imageLayoutVersion":"1.0.0"}'), file('index.json', json.dumps(index).encode())]
        entries.extend(file(k, v) for k, v in blobs.items())
        if outer:
            entries = outer(entries)
        self.archive.write_bytes(tar_bytes(entries))
        return self.archive

    def cli(self, phase='verify', extra=None, source=None):
        args = [sys.executable, '-B', str(CLI), phase, '--source-root', str(source or self.source), '--revision', self.revision]
        if phase != 'input':
            args += ['--archive', str(self.archive), '--expected-manifest', self.expected,
                     '--expected-input-digest', self.input_digest, '--repository', 'registry.fixture.invalid/kodex/agent-runner', '--output', str(self.output)]
        return subprocess.run(args + (extra or []), capture_output=True, text=True, timeout=20)

    def reject(self, code=None):
        result = self.cli()
        self.assertNotEqual(result.returncode, 0, result.stdout)
        if code:
            self.assertIn(code, result.stderr)
        self.assertFalse(self.output.exists())
        self.assertEqual(list(self.root.glob('.runner-provenance-*')), [])

    def test_public_cli_and_existing_exact_readback(self):
        self.fixture()
        result = self.cli()
        self.assertEqual(result.returncode, 0, result.stderr)
        value = json.loads(self.output.read_bytes())
        self.assertEqual(value['binarySHA256'], m.sha(BINARY))
        self.assertEqual(value['sourceRevision'], self.revision)
        self.assertEqual(value['sourceInputSHA256'], self.input_digest)
        self.assertEqual(value['archiveSHA256'], m.sha(self.archive.read_bytes()))
        self.assertEqual(self.output.stat().st_mode & 0o777, 0o600)
        previous = self.output.read_bytes()
        self.assertEqual(self.cli('check').returncode, 0)
        self.assertNotEqual(self.cli().returncode, 0)
        self.assertEqual(self.output.read_bytes(), previous)
        self.output.write_bytes(previous + b' ')
        self.assertNotEqual(self.cli('check').returncode, 0)

    def test_existing_symlink_rejected(self):
        self.fixture()
        other = self.root / 'other'
        other.write_text('keep')
        self.output.symlink_to(other)
        self.assertNotEqual(self.cli('check').returncode, 0)
        self.assertNotEqual(self.cli().returncode, 0)
        self.assertEqual(other.read_text(), 'keep')

    def test_existing_public_mode_rejected(self):
        self.fixture()
        self.assertEqual(self.cli().returncode, 0)
        self.output.chmod(0o644)
        self.assertNotEqual(self.cli('check').returncode, 0)

    def test_input_mode_matches_original_gnu_tar(self):
        result = self.cli('input')
        self.assertEqual(result.returncode, 0, result.stderr)
        original = subprocess.check_output(['tar', '--sort=name', "--mtime=UTC 1970-01-01", '--owner=0', '--group=0', '--numeric-owner', '-C', str(self.source), '-cf', '-', *m.INPUTS])
        self.assertEqual(result.stdout.strip(), m.sha(original))

    def test_uncompressed_layer(self):
        self.fixture(gzip_layers=False)
        self.assertEqual(self.cli().returncode, 0)

    def test_changed_blob(self):
        def corrupt(entries):
            name, body, kind, mode = entries[-1]
            entries[-1] = (name, body + b' ', kind, mode)
            return entries
        self.fixture(outer=corrupt)
        self.reject('OCI_BLOB_DIGEST_MISMATCH')

    def test_descriptor_sizes(self):
        for stage in ['index', 'manifest']:
            with self.subTest(stage=stage):
                def change(at, value):
                    if at == stage:
                        (value['manifests'][0] if at == 'index' else value['layers'][0])['size'] += 1
                self.fixture(change=change)
                self.reject('DESCRIPTOR_SIZE_MISMATCH')

    def test_wrong_manifest(self):
        self.fixture()
        self.expected = 'sha256:' + 'a' * 64
        self.reject('MANIFEST_MISMATCH')

    def test_wrong_source(self):
        self.fixture()
        self.revision = 'a' * 40
        self.reject('SOURCE_REVISION_MISMATCH')

    def test_wrong_input_digest(self):
        self.fixture()
        self.input_digest = 'a' * 64
        self.reject('SOURCE_INPUT_MISMATCH')

    def test_wrong_or_missing_source_tags(self):
        for mutate in [lambda v: v['manifests'][0]['annotations'].pop('io.containerd.image.name'),
                       lambda v: v['manifests'][0]['annotations'].update({'org.opencontainers.image.ref.name': 'local-' + 'a' * 64}),
                       lambda v: v.update(annotations={'io.containerd.image.name': 'foreign'})]:
            self.fixture(change=lambda at, value: mutate(value) if at == 'index' else None)
            self.reject('SOURCE_TAG_BINDING_MISMATCH')

    def test_wrong_archive_name(self):
        self.fixture()
        next_path = self.root / 'renamed.oci.tar'
        self.archive.rename(next_path)
        self.archive = next_path
        self.reject('ARCHIVE_INPUT_BINDING_MISMATCH')

    def test_dirty_and_ignored_source(self):
        self.fixture()
        (self.source / 'new.txt').write_text('untracked')
        self.reject('SOURCE_DIRTY')
        (self.source / 'new.txt').unlink()
        (self.source / '.git/info/exclude').write_text('ignored.txt\n')
        (self.source / m.INPUTS[0] / 'ignored.txt').write_text('hidden')
        self.reject('SOURCE_IGNORED_INPUT')

    def test_invalid_config_diffids_and_platform(self):
        for mutate in [lambda v: v['rootfs']['diff_ids'].__setitem__(0, 'sha256:' + 'a' * 64), lambda v: v.update(architecture='arm64')]:
            self.fixture(change=lambda at, value: mutate(value) if at == 'config' else None)
            self.reject()

    def test_unsupported_media_and_extra_index_manifest(self):
        self.fixture(change=lambda at, value: value['layers'][0].update(mediaType=m.OCI + 'layer.v1.tar+zstd') if at == 'manifest' else None)
        self.reject('DESCRIPTOR_INVALID')
        self.fixture(change=lambda at, value: value['manifests'].append(value['manifests'][0]) if at == 'index' else None)
        self.reject('OCI_INDEX_INVALID')

    def test_ordered_file_replacement(self):
        self.fixture([[file(body=b'old')], [file()]])
        self.assertEqual(self.cli().returncode, 0)
        self.assertEqual(json.loads(self.output.read_bytes())['binarySHA256'], m.sha(BINARY))

    def test_whiteout_removes_lower_target(self):
        self.fixture([[file()], [file('usr/local/bin/.wh.kodex-agent-runner', b'')]])
        self.reject('RUNNER_EXECUTABLE_REQUIRED')

    def test_whiteout_never_removes_same_layer(self):
        for entries in [[file(), file('usr/local/bin/.wh.kodex-agent-runner', b'')], [file('usr/local/bin/.wh.kodex-agent-runner', b''), file()]]:
            self.fixture([[file(body=b'old')], entries])
            self.assertEqual(self.cli().returncode, 0)
            self.assertEqual(json.loads(self.output.read_bytes())['binarySHA256'], m.sha(BINARY))
            self.output.unlink()

    def test_opaque_preserves_same_layer_child(self):
        self.fixture([[file(body=b'old'), file('usr/local/bin/other')], [file(), file('usr/local/bin/.wh..wh..opq', b'')]])
        self.assertEqual(self.cli().returncode, 0)
        self.output.unlink()
        self.fixture([[file()], [file('usr/local/.wh..wh..opq', b'')]])
        self.reject('RUNNER_EXECUTABLE_REQUIRED')

    def test_directory_replacement_removes_child(self):
        self.fixture([[file()], [file('usr/local/bin', b'not-directory')]])
        self.reject('RUNNER_PARENT_UNSAFE')

    def test_whiteout_invalid(self):
        for entry in [file('usr/.wh.', b''), file('usr/.wh.thing', b'not-empty'), directory('usr/.wh.thing')]:
            self.fixture([[file(), entry]])
            self.reject('WHITEOUT_INVALID')

    def test_target_and_parent_links_fail_closed(self):
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
            for path in (TARGET, 'usr/local/bin'):
                self.fixture([[(path, b'/other', kind, 0o777)]])
                self.reject()

    def test_unrelated_distro_links_are_never_followed(self):
        self.fixture([[("bin", b"usr/bin", tarfile.SYMTYPE, 0o777), ('usr/bin/other', b'/outside-host', tarfile.LNKTYPE, 0o777), file()]])
        self.assertEqual(self.cli().returncode, 0)

    def test_link_parent_with_child_is_ambiguous(self):
        self.fixture([[('usr/local', b'/outside-host', tarfile.SYMTYPE, 0o777), file()]])
        self.reject('LAYER_PARENT_AMBIGUOUS')

    def test_path_traversal_and_duplicates(self):
        for name in ('../target', '/target', 'usr/../target', 'usr//local/bin/other'):
            self.fixture([[file(), file(name)]])
            self.reject('TAR_PATH_INVALID')
        self.fixture([[file(), file('./' + TARGET)]])
        self.reject('TAR_DUPLICATE_PATH')

    def test_outer_duplicates_and_link(self):
        self.fixture(outer=lambda entries: entries + [entries[0]])
        self.reject('TAR_DUPLICATE_PATH')
        self.fixture(outer=lambda entries: entries + [('extra', b'/tmp', tarfile.SYMTYPE, 0o777)])
        self.reject('OCI_ENTRY_INVALID')

    def test_non_executable_and_special_target(self):
        for entry in (file(mode=0o444), file(mode=0o4555), (TARGET, b'', tarfile.FIFOTYPE, 0o555)):
            self.fixture([[entry]])
            self.reject()

    def test_archive_symlink(self):
        self.fixture()
        other = self.root / 'archive.tar'
        self.archive.rename(other)
        self.archive.symlink_to(other)
        self.reject('PATH_INVALID')

    def test_output_directory_mode(self):
        self.fixture()
        self.root.chmod(0o755)
        self.reject('PRIVATE_OUTPUT_DIRECTORY_REQUIRED')
        self.root.chmod(0o700)

    def test_output_race_exactly_one_winner(self):
        self.fixture()
        with ThreadPoolExecutor(max_workers=2) as executor:
            results = list(executor.map(lambda _: self.cli(), range(2)))
        self.assertEqual(sorted(x.returncode for x in results), [0, 1])
        self.assertEqual(json.loads(self.output.read_bytes())['binarySHA256'], m.sha(BINARY))
        self.assertEqual(self.output.stat().st_nlink, 1)
        self.assertEqual(list(self.root.glob('.runner-provenance-*')), [])

    def test_trailing_hidden_archive_rejected(self):
        self.fixture()
        with self.archive.open('ab') as out:
            out.write(tar_bytes([file('hidden')]))
        self.reject('TAR_TRAILING_DATA')

    def test_truncated_tar_terminator(self):
        self.fixture()
        data = self.archive.read_bytes()
        with tarfile.open(fileobj=io.BytesIO(data), mode='r:') as tar:
            list(tar)
            end = tar.offset
        self.archive.write_bytes(data[:end])
        self.reject('TAR_TERMINATOR_INVALID')

    def test_duplicate_index_json_key(self):
        def change(entries):
            name, body, kind, mode = entries[1]
            entries[1] = (name, body.replace(b'{', b'{"schemaVersion":2,', 1), kind, mode)
            return entries
        self.fixture(outer=change)
        self.reject('JSON_DUPLICATE_KEY')

    def test_unchanged_inputs_new_revision_reuses_identical_archive(self):
        self.fixture()
        before = self.archive.read_bytes()
        self.assertEqual(self.cli().returncode, 0)
        original_provenance = self.output.read_bytes()
        (self.source / 'doc.txt').write_text('outside build inputs')
        self.git('add', '.')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'documentation')
        self.revision = self.git('rev-parse', 'HEAD').strip()
        self.output = self.root / 'next-provenance.json'
        self.assertEqual(self.cli().returncode, 0)
        self.assertEqual(self.archive.read_bytes(), before)
        self.assertEqual(json.loads(self.output.read_bytes())['binarySHA256'], json.loads(original_provenance)['binarySHA256'])
        self.assertNotEqual(self.output.read_bytes(), original_provenance)
        self.assertEqual((self.root / 'provenance.json').read_bytes(), original_provenance)

    def test_empty_target_rejected(self):
        self.fixture([[file(body=b'')]])
        self.reject('RUNNER_CONTENT_INVALID')

    def test_duplicate_cli_flag(self):
        self.fixture()
        self.assertNotEqual(self.cli(extra=['--revision', self.revision]).returncode, 0)
        self.assertNotEqual(self.cli(extra=['--revision=' + self.revision]).returncode, 0)
        self.assertNotEqual(self.cli(extra=['--rev', self.revision]).returncode, 0)


if __name__ == '__main__':
    unittest.main()
