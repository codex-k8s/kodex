#!/usr/bin/env python3
"""Герметичная проверка dispatch SSH-профилей; настоящие Go/Docker/SQL не вызываются."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().with_name("control-plane-postgres-test.sh")
CURRENT = "--ssh-identity-owner-current"
LEGACY = "--ssh-identity-owner-development"
DRIVER = r'''#!/usr/bin/python3
import hashlib, json, os, pathlib, stat, sys
name, a = pathlib.Path(sys.argv[0]).name, sys.argv[1:]
row = {'tool': name}
if name == 'go':
    row.update(args=a, cwd=pathlib.Path.cwd().name)
    if '-c' in a:
        row['databaseContext'] = any(k in os.environ for k in ['KODEX_CONTROL_PLANE_TEST_DSN', 'KODEX_CONTROL_PLANE_TEST_ADMIN_DSN', 'KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN'])
    elif a[0] == 'test' and './internal/repository/postgres/platform' in a:
        p = pathlib.Path(os.environ['KODEX_SSH_GATEWAY_NATIVE_CONSUMER_BINARY'])
        row['nativeConsumerValid'] = (hashlib.sha256(p.read_bytes()).hexdigest() == os.environ['KODEX_SSH_GATEWAY_NATIVE_CONSUMER_SHA256'] and stat.S_IMODE(p.stat().st_mode) == 0o500 and stat.S_IMODE(p.parent.stat().st_mode) == 0o700)
with open(os.environ['SSH_ENTRYPOINT_CALLS'], 'a') as f:
    f.write(json.dumps(row) + '\n')
if name == 'go':
    if '-c' in a:
        if os.environ.get('SSH_CONSUMER_FAIL') == '1': sys.exit(19)
        pathlib.Path(a[a.index('-o') + 1]).write_bytes(b'synthetic native consumer')
    elif a[0] == 'test': sys.exit(23)
elif name == 'docker':
    image = 'sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'
    if a[0] == 'context': print('unix:///synthetic.sock')
    elif a[0] == 'info': print('linux')
    elif a[0] == 'image': print(image)
    elif a[0] == 'run': print('c' * 64)
    elif a[0] == 'exec':
        if a[-1].endswith('memory.max'): print('1073741824')
        elif a[-1].endswith('cpu.max'): print('100000 100000')
        elif '-f' in a: print('tmpfs')
        else: print('70:70:700')
    elif a[0] == 'inspect':
        if 'NetworkSettings' in a[2]: print('15432')
        else:
            print(json.dumps({'id': 'c' * 64, 'image': image, 'readonly': True,
              'memory': 1073741824, 'swap': 1073741824, 'cpu': 1000000000, 'pids': 128,
              'tmpfs': {'/var/lib/postgresql': 'rw,noexec,nosuid,nodev,size=536870912,mode=0700,uid=70,gid=70', '/var/run/postgresql': 'rw,noexec,nosuid,nodev,size=16777216,mode=0775', '/tmp': 'rw,noexec,nosuid,nodev,size=16777216,mode=1777'},
              'mounts': [{'Type': 'tmpfs'}]}))
    elif a[0] not in ['stop', 'ps']: sys.exit(98)
elif name == 'psql':
    if '-c' in a and 'max(version_id)' in a[a.index('-c') + 1]: print(os.environ.get('SSH_SCHEMA_HEAD', '20261121000200'))
    elif '-c' in a and 'skill_artifact_reference_count' in a[a.index('-c') + 1]: print('0')
    elif '--file' in a and 'migration-postcondition' in a[a.index('--file') + 1]: print('{}')
elif name == 'node': sys.stdin.read()
elif name != 'pg_isready': sys.exit(99)
'''


class SSHCurrentEntrypointTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="ssh-current-entrypoint-")
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.bin = self.root / "bin"
        self.bin.mkdir(mode=0o700)
        self.temp = self.root / "private-temp"
        self.temp.mkdir(mode=0o700)
        self.log = self.root / "calls.jsonl"
        for name in ["go", "docker", "psql", "pg_isready", "node"]:
            p = self.bin / name
            p.write_text(DRIVER, encoding="utf-8")
            p.chmod(0o700)

    def run_profile(self, *args, **extra):
        env = {"PATH": f"{self.bin}:/usr/bin:/bin", "TMPDIR": str(self.temp),
               "SSH_ENTRYPOINT_CALLS": str(self.log), "KODEX_TEST_POSTGRES_STORAGE": "ram"}
        env.update(extra)
        result = subprocess.run(["bash", str(SCRIPT), *args], env=env,
                                capture_output=True, timeout=15, check=False)
        calls = [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []
        return result, calls

    def test_current_uses_exact_consumer_normal_schema_and_only_execution_owner(self):
        result, calls = self.run_profile(CURRENT)
        self.assertEqual(result.returncode, 23, result.stderr.decode())
        go = [c for c in calls if c["tool"] == "go"]
        self.assertFalse(go[0]["databaseContext"])
        self.assertEqual(go[0]["cwd"], "integration-gateway")
        self.assertIn("./internal/integration", go[0]["args"])
        self.assertEqual([c["args"][-1] for c in go if c["args"][0] == "run"], ["up", "status", "up"])
        self.assertEqual(go[-1]["args"][-1], "^TestSSHExecutionOwnerComponent$")
        self.assertTrue(go[-1]["nativeConsumerValid"])
        self.assertEqual(list(self.temp.iterdir()), [])

    def test_legacy_keeps_exact_049_seed(self):
        result, calls = self.run_profile(LEGACY)
        self.assertEqual(result.returncode, 23, result.stderr.decode())
        go = [c for c in calls if c["tool"] == "go"]
        self.assertEqual(go[-1]["args"][-1], "^TestPersonalEmailOwnerDevelopmentSeed$")
        self.assertFalse(any(c["args"][0] == "run" for c in go))

    def test_wrong_schema_fails_before_owner(self):
        result, calls = self.run_profile(CURRENT, SSH_SCHEMA_HEAD="20261114170000")
        self.assertEqual(result.returncode, 1)
        self.assertIn(b"requires schema 051", result.stderr)
        self.assertFalse(any(c.get("nativeConsumerValid") for c in calls))

    def test_consumer_build_failure_never_starts_docker(self):
        result, calls = self.run_profile(CURRENT, SSH_CONSUMER_FAIL="1")
        self.assertEqual(result.returncode, 19)
        self.assertFalse(any(c["tool"] == "docker" for c in calls))
        self.assertEqual(list(self.temp.iterdir()), [])

    def test_external_consumer_unknown_suffix_extra_argument_and_disk_are_rejected(self):
        for args, extra in [((CURRENT,), {"KODEX_SSH_GATEWAY_NATIVE_CONSUMER_BINARY": "/synthetic/foreign"}),
                            ((CURRENT,), {"KODEX_SSH_GATEWAY_NATIVE_CONSUMER_SHA256": "a" * 64}),
                            ((CURRENT + "-foreign",), {}), ((CURRENT, "extra"), {}),
                            ((CURRENT,), {"KODEX_TEST_POSTGRES_STORAGE": "disk"}),
                            ((CURRENT,), {"KODEX_CONTROL_PLANE_TEST_FILTER": "^TestForeign$"}),
                            ((CURRENT,), {"KODEX_CONTROL_PLANE_TEST_FILTER": ""})]:
            with self.subTest(args=args, extra=list(extra)):
                self.log.unlink(missing_ok=True)
                result, calls = self.run_profile(*args, **extra)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])


if __name__ == "__main__":
    unittest.main()
