#!/usr/bin/env python3
"""Проверка реального builder entrypoint без сборки образов и импорта на узлы."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

BUILDER = Path(__file__).resolve().with_name('build-local-runner.sh')


class RunnerBuildContextTests(unittest.TestCase):
    def test_private_context_preserves_modes_and_excludes_ignored_files(self):
        self.check_context(dirty=False)

    def test_dirty_source_never_reaches_builder(self):
        self.check_context(dirty=True)

    def check_context(self, dirty):
        with tempfile.TemporaryDirectory(prefix='kodex-runner-context-') as temporary:
            area = Path(temporary)
            source, state, binaries = [area / name for name in ('source', 'state', 'bin')]
            for directory in (source, state, binaries):
                directory.mkdir(mode=0o700)
            files = {
                '.gitignore': '.env\nprivate.txt\n',
                'services/jobs/agent-runner/Dockerfile': 'FROM scratch\n',
                'services/jobs/agent-runner/package.json': '{}\n',
                'libs/go/probe.txt': 'public fixture\n',
                'tools/dev/ensure-local-buildx-builder.sh': '#!/bin/sh\nexit 0\n',
            }
            for name, content in files.items():
                path = source / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content)
                path.chmod(0o755 if name.endswith('.sh') else 0o664)
            for arguments in (
                ['init', '-q'], ['config', 'user.name', 'Context Fixture'],
                ['config', 'user.email', 'context@example.invalid'],
                ['add', '.'], ['commit', '-qm', 'fixture'],
            ):
                subprocess.run(['git', '-C', str(source), *arguments], check=True,
                               stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            (source / '.env').write_text('PRIVATE_CANARY=never-send\n')
            (source / 'private.txt').write_text('never-send\n')
            for path in source.rglob('*'):
                if path.is_dir() and '.git' not in path.parts:
                    path.chmod(0o775)
            if dirty:
                (source / 'libs/go/probe.txt').write_text('changed\n')
            proof = area / 'proof.json'
            docker = binaries / 'docker'
            docker.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
if sys.argv[1:] == ['buildx', 'version']:
    raise SystemExit(0)
assert sys.argv[1:3] == ['buildx', 'build']
context = pathlib.Path(sys.argv[-1])
assert pathlib.Path(sys.argv[sys.argv.index('--file') + 1]).is_relative_to(context)
files = {str(p.relative_to(context)): oct(p.stat().st_mode & 0o777)
         for p in context.rglob('*') if p.is_file()}
assert all((p.stat().st_mode & 0o777) == 0o775
           for p in context.rglob('*') if p.is_dir())
assert (context.parent.stat().st_mode & 0o777) == 0o700
pathlib.Path(os.environ['KODEX_CONTEXT_PROOF']).write_text(json.dumps(files))
raise SystemExit(77)
''')
            docker.chmod(0o755)
            environment = {key: value for key, value in os.environ.items()
                           if key in ('HOME', 'PATH', 'LANG')}
            environment['PATH'] = str(binaries) + ':' + environment.get('PATH', '/usr/bin:/bin')
            environment['KODEX_CONTEXT_PROOF'] = str(proof)
            result = subprocess.run(
                ['bash', str(BUILDER), '--source-root', str(source),
                 '--state-directory', str(state), '--context', 'fixture', '--image-profile', 'full'],
                env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30,
                umask=0o077,
            )
            self.assertNotEqual(result.returncode, 0)
            if dirty:
                self.assertFalse(proof.exists())
                self.assertIn(b'SOURCE_DIRTY', result.stderr)
                return
            self.assertEqual(result.returncode, 77, result.stderr.decode())
            self.assertEqual(json.loads(proof.read_text()), {
                'services/jobs/agent-runner/Dockerfile': '0o664',
                'services/jobs/agent-runner/package.json': '0o664',
                'libs/go/probe.txt': '0o664',
            })
            self.assertEqual(list((state / 'cache').glob('runner-source.*')), [])
            self.assertFalse((state / 'agent-runner-image').exists())


if __name__ == '__main__':
    unittest.main()
