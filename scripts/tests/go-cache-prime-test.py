#!/usr/bin/env python3
"""Герметичный CLI lifecycle: настоящий Git/Go parser, управляемый download без сети."""

import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
CLI = ROOT / "tools/dev/prime-go-cache.py"
RENDER = ROOT / "tools/dev/prime-render-go-cache.py"
MODULE = "services/external/control-api-gateway"
REAL_GO = shutil.which("go")
STUB = r'''#!/usr/bin/env python3
import json,os,pathlib,sys,time
base=pathlib.Path(__file__).parent
config=json.loads((base/'config.json').read_text())
mode=(base/'mode').read_text()
args=sys.argv[1:]
assert os.environ['GOWORK']=='off' and os.environ['GOTOOLCHAIN']=='local'
assert os.environ['GOENV']=='off' and os.environ['GOPROXY']=='https://proxy.golang.org'
assert os.environ['GOSUMDB']=='sum.golang.org' and 'KODEX_GITHUB_PAT' not in os.environ
assert os.environ['HOME'] != config['callerHome'] and 'GOFLAGS' not in os.environ
if 'download' not in args and 'verify' not in args and 'install' not in args:
 os.execv(config['go'],[config['go'],*args])
with (base/'calls.jsonl').open('a') as f:f.write(json.dumps(args)+'\n')
if 'verify' in args:
 print('all modules verified');sys.exit(0)
if 'install' in args:
 output=pathlib.Path(os.environ['GOBIN'])/'air';output.write_text('synthetic air');output.chmod(0o755);sys.exit(0)
cache=pathlib.Path(os.environ['GOMODCACHE']);directory=cache/'example.test/fixture@v1.0.0'
directory.mkdir(parents=True,exist_ok=True)
files={key:cache/('cache/download/example.test/fixture/@v/v1.0.0'+extension) for key,extension in [('Info','.info'),('GoMod','.mod'),('Zip','.zip')]}
for file in files.values():
 file.parent.mkdir(parents=True,exist_ok=True)
 if not file.exists():file.write_text('fixture')
if mode=='failure':print('PRIVATE_DIAGNOSTIC_SENTINEL',file=sys.stderr);sys.exit(1)
if mode=='timeout':time.sleep(5)
if mode=='drift':(pathlib.Path(config['source'])/'README.md').write_text('changed')
if mode=='rewrite':
 (pathlib.Path(args[args.index('-C')+1])/'go.sum').write_text('changed')
if mode=='missing':files['Zip'].unlink()
print(json.dumps({'Path':'example.test/fixture','Version':'v1.0.0','Sum':'h1:'+'A'*43+'=','GoModSum':'h1:'+'B'*43+'=','Dir':str(directory),**{k:str(v) for k,v in files.items()}}))
'''


class CachePrime(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="kodex-go-prime-test-")
        self.directory = Path(self.temp.name)
        self.source = self.directory / "source"
        self.cache = self.directory / "cache"
        self.bin = self.directory / "bin"
        self.evidence = self.directory / "evidence"
        for path in (self.source, self.cache, self.bin, self.evidence):
            path.mkdir(mode=0o700)
        self.git("init", "-q")
        self.git("config", "user.name", "fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.git("remote", "add", "origin", "https://github.com/codex-k8s/kodex.git")
        # Та же production allowlist импортируется без выполнения prime.
        import importlib.util
        spec = importlib.util.spec_from_file_location("go_cache_fixture", ROOT / "tools/dev/go_cache.py")
        library = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(library)
        for module in library.MODULES:
            target = self.source / module
            target.mkdir(parents=True)
            (target / "go.mod").write_text(f"module github.com/codex-k8s/kodex/{module}\n\ngo 1.26.6\n")
            (target / "go.sum").write_text("")
        (self.source / "README.md").write_text("fixture")
        (self.source / "tools/dev").mkdir(parents=True)
        (self.source / "tools/dev/components.lock.json").write_text(json.dumps({"tools": {"air": {"module": "github.com/air-verse/air", "version": "v1.63.4"}}}))
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")
        self.revision = self.git("rev-parse", "HEAD").strip()
        (self.bin / "go").write_text(STUB)
        (self.bin / "go").chmod(0o755)
        (self.bin / "mode").write_text("success")
        (self.bin / "config.json").write_text(json.dumps({"go": REAL_GO, "source": str(self.source), "callerHome": str(Path.home())}))
        self.environment = {**os.environ, "PATH": f"{self.bin}:{os.environ['PATH']}", "KODEX_GITHUB_PAT": "DO_NOT_INHERIT", "GOFLAGS": "FORBIDDEN", "PYTHONDONTWRITEBYTECODE": "1"}
        self.counter = 0

    def tearDown(self):
        for directory, dirs, files in os.walk(self.directory):
            Path(directory).chmod(0o700)
            for name in files:
                path = Path(directory) / name
                if not path.is_symlink():
                    path.chmod(0o600)
        self.temp.cleanup()

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.source), *args], text=True)

    def run_cli(self, *, phase="prime", module=MODULE, extra=(), source=None):
        self.counter += 1
        path = self.evidence / f"run{self.counter}.jsonl"
        args = ["python3", "-B", str(CLI), phase, "--profile", "staging-hot-reload", "--source-root", str(source or self.source), "--revision", self.revision, "--cache-root", str(self.cache), "--module", module, "--evidence", str(path), "--timeout-seconds", "20"]
        if phase == "prime":
            args += ["--confirm", "PRIME-STAGING-GO-CACHE"]
        result = subprocess.run([*args, *extra], env=self.environment, text=True, capture_output=True, timeout=25)
        self.assertNotIn("PRIVATE_DIAGNOSTIC_SENTINEL", result.stdout + result.stderr + (path.read_text() if path.exists() else ""))
        return result, path

    def sealed(self):
        for root in (self.cache / "go-mod-v2", self.cache / "go-sumdb"):
            for path in [root, *root.rglob("*")]:
                self.assertEqual(path.stat().st_mode & 0o222, 0, path)
                self.assertEqual(path.stat().st_mode & (0o555 if path.is_dir() else 0o444), 0o555 if path.is_dir() else 0o444)

    def test_success_and_cache_hit_preserve_source(self):
        first, path = self.run_cli()
        self.assertEqual(first.returncode, 0, first.stderr)
        events = [json.loads(line) for line in path.read_text().splitlines()]
        self.assertEqual([e['type'] for e in events], ['HEADER', 'PLAN', 'INTENT', 'RESULT'])
        dependency = events[-1]['dependencies'][MODULE][0]
        self.assertEqual(set(dependency), {'Path', 'Version', 'Sum', 'GoModSum'})
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.sealed()
        cache_file = self.cache / 'go-mod-v2/cache/download/example.test/fixture/@v/v1.0.0.zip'
        before = cache_file.stat().st_mtime_ns
        second, _ = self.run_cli()
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(cache_file.stat().st_mtime_ns, before)
        self.assertEqual(self.git('status', '--porcelain'), '')
        self.sealed()

    def test_plan_does_not_touch_cache(self):
        before = list(self.cache.iterdir())
        result, path = self.run_cli(phase='plan')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(list(self.cache.iterdir()), before)
        self.assertFalse((self.bin / 'calls.jsonl').exists())
        events = [json.loads(line) for line in path.read_text().splitlines()]
        self.assertEqual([event['type'] for event in events], ['HEADER', 'PLAN', 'RESULT'])
        self.assertEqual(events[-1]['status'], 'PLANNED')

    def test_failure_and_timeout_seal_cache(self):
        for mode in ('failure', 'timeout', 'missing', 'rewrite'):
            with self.subTest(mode=mode):
                (self.bin / 'mode').write_text(mode)
                result, _ = self.run_cli(extra=('--timeout-seconds', '1') if mode == 'timeout' else ())
                self.assertNotEqual(result.returncode, 0)
                self.sealed()
                self.assertEqual(self.git('status', '--porcelain'), '')

    def test_source_drift_fails_and_seals(self):
        (self.bin / 'mode').write_text('drift')
        result, _ = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.sealed()

    def test_dirty_standalone_rejected_but_render_allowed(self):
        (self.source / 'README.md').write_text('dirty local source')
        result, _ = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('SOURCE_DIRTY', result.stderr)
        render = subprocess.run(['python3', '-B', str(RENDER), str(self.source), str(self.cache), 'web-only'], env=self.environment, capture_output=True, text=True, timeout=25)
        self.assertEqual(render.returncode, 0, render.stderr)
        self.assertRegex(json.loads(render.stdout)['airSHA256'], r'^[a-f0-9]{64}$')
        self.assertEqual((self.source / 'README.md').read_text(), 'dirty local source')
        self.sealed()
        self.assertEqual((self.cache / 'go-tools/air').stat().st_mode & 0o777, 0o555)

    def test_foreign_repository_rejected(self):
        self.git('remote', 'set-url', 'origin', 'https://example.invalid/foreign.git')
        result, _ = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('FOREIGN_REPOSITORY', result.stderr)

    def test_paths_and_profile_rejected(self):
        alias = self.directory / 'alias'
        alias.symlink_to(self.source)
        for options in ({'source': alias}, {'source': '/'}, {'module': '../control-api-gateway'}, {'module': MODULE + '/..'}, {'extra': ('--profile', 'production')}, {'extra': ('--confirm', 'WRONG')}):
            with self.subTest(options=options):
                result, _ = self.run_cli(**options)
                self.assertNotEqual(result.returncode, 0)
        self.assertEqual(list(self.cache.iterdir()), [])

    def test_symlink_cache_rejected_without_target_chmod(self):
        target = self.directory / 'unrelated'
        target.mkdir(mode=0o700)
        (self.cache / 'go-mod-v2').symlink_to(target)
        result, _ = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(target.stat().st_mode & 0o777, 0o700)

    def test_lock_timeout_without_unseal(self):
        lock = self.cache / '.go-prime.lock'
        lock.touch(mode=0o600)
        with lock.open('r+') as stream:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result, _ = self.run_cli(extra=('--lock-timeout-seconds', '1'))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('CACHE_LOCK_TIMEOUT', result.stderr)
        self.assertFalse((self.cache / 'go-mod-v2').exists())

    def test_unsafe_build_root_rejected_before_cache_write(self):
        target = self.directory / 'unrelated-build-cache'
        target.mkdir(mode=0o700)
        (self.cache / 'go-build-v2').symlink_to(target)
        result, _ = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(list(target.iterdir()), [])
        self.assertEqual(target.stat().st_mode & 0o777, 0o700)
        self.assertFalse((self.cache / 'go-mod-v2').exists())

    def test_nested_host_cache_links_rejected_without_target_change(self):
        host = self.cache / 'go-build-v2/host-prime'
        host.mkdir(parents=True, mode=0o700)
        nested = host / 'ab'
        nested.mkdir(mode=0o700)
        target = self.directory / 'unrelated-host-cache'
        target.write_bytes(b'unchanged')
        target.chmod(0o600)
        before = (target.read_bytes(), target.stat().st_mode, target.stat().st_mtime_ns)
        for kind in ('symlink', 'hardlink'):
            with self.subTest(kind=kind):
                link = nested / 'entry'
                if kind == 'symlink':
                    link.symlink_to(target)
                else:
                    os.link(target, link)
                for phase in ('plan', 'prime'):
                    result, _ = self.run_cli(phase=phase)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('CACHE_ENTRY_UNSAFE', result.stderr)
                    self.assertEqual((target.read_bytes(), target.stat().st_mode, target.stat().st_mtime_ns), before)
                    self.assertFalse((self.cache / 'go-mod-v2').exists())
                    self.assertFalse((self.bin / 'calls.jsonl').exists())
                link.unlink()

    def test_replacement_traversal_rejected_before_cache_write(self):
        mod = self.source / MODULE / 'go.mod'
        mod.write_text(mod.read_text() + '\nreplace example.test/private => ../../../../outside\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'invalid replacement fixture')
        self.revision = self.git('rev-parse', 'HEAD').strip()
        result, _ = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('REPLACEMENT_OUTSIDE_SOURCE', result.stderr)
        self.assertEqual(list(self.cache.iterdir()), [])

    def test_real_go_empty_dependency_graph_no_network(self):
        self.environment['PATH'] = os.environ['PATH']
        result, _ = self.run_cli()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.git('status', '--porcelain'), '')
        self.sealed()


if __name__ == '__main__':
    unittest.main()
