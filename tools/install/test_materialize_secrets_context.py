"""Проверка передачи явного context до первой namespace mutation без сети и credentials."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class MaterializeContextTests(unittest.TestCase):
    def test_explicit_context_reaches_namespace_helper_without_ambient_context(self):
        for ambient in (None, 'unrelated-context'):
            with self.subTest(ambient=ambient), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root/'material').mkdir()
                (root/'ca.pem').write_text('synthetic certificate boundary')
                binary = root/'bin'; binary.mkdir()
                scripts = {
                    'kubectl': '#!/bin/sh\ntest "$1 $2" = "config current-context" || exit 91\nprintf fixture-context\n',
                    'openssl': '#!/bin/sh\nexit 0\n',
                    'python3': '#!/usr/bin/python3\nimport sys\nassert sys.argv[1].endswith("/ensure_namespace.py")\nassert sys.argv[2:] == ["--context","fixture-context","--namespace","kodex-system"]\nprint("EXPLICIT_NAMESPACE_CONTEXT_VERIFIED")\nraise SystemExit(78)\n',
                }
                for name, content in scripts.items():
                    path = binary/name; path.write_text(content); path.chmod(0o700)
                env = dict(os.environ, PATH=str(binary)+':'+os.defpath)
                env.pop('context', None)
                if ambient is not None: env['context'] = ambient
                result = subprocess.run(['bash',str(ROOT/'tools/install/materialize-secrets.sh'),
                    '--context','fixture-context','--material-directory',str(root/'material'),
                    '--oidc-ca-file',str(root/'ca.pem'),'--provider-mode','deferred'],
                    env=env,capture_output=True,text=True,timeout=15)
                self.assertEqual(result.returncode,78,result.stderr)
                self.assertIn('EXPLICIT_NAMESPACE_CONTEXT_VERIFIED',result.stdout)


if __name__ == '__main__':
    unittest.main()
