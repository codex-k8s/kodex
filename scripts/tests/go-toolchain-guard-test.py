#!/usr/bin/env python3
"""Вспомогательные compiler stages проверяются до сборки, без Docker и сетевых вызовов."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
BUILDER = 'docker.io/library/golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696'
STALE = 'docker.io/library/golang:1.26.6-alpine@sha256:' + 'a' * 64


def dockerfile(builder=BUILDER, checked=True):
    """Минимальный synthetic stage не собирает исходники и не получает credentials."""
    return (f'FROM {builder} AS build\nENV GOTOOLCHAIN=local\n' +
            ('RUN test "$(go env GOVERSION)" = "go1.26.10"\n' if checked else ''))


class Tests(unittest.TestCase):
    def execute(self, path, content):
        """Исполняет настоящий guard в отдельном repository fixture с закрытой fake Go CLI."""
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name in ('scripts', 'libs/go', 'services/synthetic', 'infra/admission-tools', 'bin'):
                (root / name).mkdir(parents=True, exist_ok=True)
            (root / 'go.mod').write_text('module example.invalid/synthetic\n\ngo 1.26.10\n')
            target = root / 'scripts/check-go-toolchain.sh'
            target.write_bytes((ROOT / 'scripts/check-go-toolchain.sh').read_bytes())
            (root / 'services/synthetic/Dockerfile').write_text(dockerfile())
            selected = root / path
            selected.parent.mkdir(parents=True, exist_ok=True)
            selected.write_text(content)
            go = root / 'bin/go'
            go.write_text('#!/bin/sh\n[ "$#" -eq 2 ] && [ "$1" = env ] && [ "$2" = GOVERSION ] || exit 91\nprintf "go1.26.10\\n"\n')
            go.chmod(0o700)
            rg = shutil.which('rg')
            self.assertIsNotNone(rg)
            environment = {'PATH': str(root / 'bin') + ':' + str(Path(rg).parent) + ':' + os.defpath,
                           'HOME': str(root), 'LC_ALL': 'C'}
            return subprocess.run(['bash', str(target)], env=environment, capture_output=True, text=True, timeout=10)

    def test_auxiliary_infra_cannot_use_a_stale_compiler(self):
        path = 'infra/admission-tools/Dockerfile'
        failed = self.execute(path, dockerfile(STALE))
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn(path, failed.stderr)
        self.assertEqual(self.execute(path, dockerfile()).returncode, 0)

    def test_named_dockerfiles_in_both_release_roots_are_checked(self):
        for path in ('infra/synthetic/compiler.Dockerfile', 'services/synthetic/compiler.Dockerfile'):
            with self.subTest(path=path):
                failed = self.execute(path, dockerfile(STALE))
                self.assertNotEqual(failed.returncode, 0)
                self.assertIn(path, failed.stderr)

    def test_auxiliary_stage_must_verify_its_actual_compiler(self):
        failed = self.execute('infra/admission-tools/Dockerfile', dockerfile(checked=False))
        self.assertNotEqual(failed.returncode, 0)

    def test_non_go_frontend_remains_supported(self):
        content = 'FROM registry.example.invalid/frontend@sha256:' + 'b' * 64 + '\n'
        self.assertEqual(self.execute('infra/frontend/Dockerfile', content).returncode, 0)


if __name__ == '__main__':
    unittest.main()
