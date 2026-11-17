"""Полный synthetic OCI graph и закрытые отказы перед node-cache deletion."""

import hashlib
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import node_runner_archive as verifier


class ArchiveTests(unittest.TestCase):
    def test_large_pax_rejected_before_allocating_tar_decoder(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'large-pax.tar'
            header = tarfile.TarInfo('extension')
            header.type = tarfile.XHDTYPE
            header.size = 7 << 30
            path.write_bytes(header.tobuf() + b'\0' * 1024)
            path.chmod(0o600)
            with patch.object(verifier.tarfile, 'open', side_effect=AssertionError('PAX decoder reached')):
                with self.assertRaises(ValueError):
                    verifier.verify(path, 'sha256:' + 'a' * 64)

    def fixture(self, directory, change=None):
        """Собрать минимальный настоящий OCI layout; нарушения вносить до tar записи."""
        blobs = {}

        def blob(value, media):
            raw = value if isinstance(value, bytes) else json.dumps(value).encode()
            digest = hashlib.sha256(raw).hexdigest()
            blobs['blobs/sha256/' + digest] = raw
            return {'mediaType': media, 'digest': 'sha256:' + digest, 'size': len(raw)}

        config = blob({'os': 'linux', 'architecture': 'amd64'}, verifier.CONFIG)
        layer = blob(b'synthetic layer bytes', next(iter(verifier.LAYERS)))
        manifest = blob({'schemaVersion': 2, 'config': config, 'layers': [layer]}, verifier.MANIFEST)
        members = {'oci-layout': b'{"imageLayoutVersion":"1.0.0"}',
                   'index.json': json.dumps({'schemaVersion': 2, 'manifests': [manifest]}).encode(), **blobs}
        if change:
            change(members, config, layer, manifest)
        path = Path(directory) / 'image.oci.tar'
        with tarfile.open(path, 'w') as tar:
            for name, raw in members.items():
                member = tarfile.TarInfo(name)
                member.size = len(raw)
                tar.addfile(member, io.BytesIO(raw))
        path.chmod(0o600)
        return path, manifest['digest']

    def test_complete_graph_and_immutable_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            path, digest = self.fixture(directory)
            before = path.read_bytes()
            result = verifier.verify(path, digest)
            self.assertEqual(result['imageDigest'], digest)
            self.assertEqual(result['archiveSHA256'], hashlib.sha256(before).hexdigest())
            self.assertEqual(path.read_bytes(), before)

    def test_missing_corrupt_extra_blob_and_duplicate_json_rejected(self):
        changes = [
            lambda m, c, l, r: m.pop('blobs/sha256/' + l['digest'][7:]),
            lambda m, c, l, r: m.__setitem__('blobs/sha256/' + l['digest'][7:], b'corrupt'),
            lambda m, c, l, r: m.__setitem__('blobs/sha256/' + hashlib.sha256(b'extra').hexdigest(), b'extra'),
            lambda m, c, l, r: m.__setitem__('index.json', b'{"schemaVersion":2,"schemaVersion":2}'),
            lambda m, c, l, r: m.__setitem__('../escape', b'no'),
        ]
        for change in changes:
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                path, digest = self.fixture(directory, change)
                with self.assertRaises((ValueError, KeyError)):
                    verifier.verify(path, digest)

    def test_wrong_digest_hidden_tail_symlink_hardlink_and_mode_rejected(self):
        for kind in ['digest', 'tail', 'symlink', 'hardlink', 'mode']:
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                path, digest = self.fixture(directory)
                if kind == 'digest':
                    digest = 'sha256:' + '0' * 64
                elif kind == 'tail':
                    with path.open('ab') as out:
                        out.write(b'hidden archive')
                elif kind == 'symlink':
                    link = path.with_name('alias'); link.symlink_to(path); path = link
                elif kind == 'hardlink':
                    os.link(path, path.with_name('alias'))
                else:
                    path.chmod(0o644)
                with self.assertRaises(ValueError):
                    verifier.verify(path, digest)


if __name__ == '__main__':
    unittest.main()
