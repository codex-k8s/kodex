#!/usr/bin/env python3
"""Проверяет настоящий shell reconciler без PostgreSQL, Kubernetes и настоящих ключей."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh"
STUB = r'''#!/usr/bin/python3
import json, os, sys
from pathlib import Path
name = Path(sys.argv[0]).name
args = sys.argv[1:]
sql = (args[args.index('--command') + 1] if '--command' in args else sys.stdin.read()) if name == 'psql' else ''
with open(os.environ['TRACE'], 'a') as output:
    output.write(json.dumps({'program': name, 'args': args, 'sql': sql,
        'user': os.environ.get('PGUSER'), 'passwordPresent': bool(os.environ.get('PGPASSWORD'))}) + '\n')
if name == 'cat':
    if args == ['/fixture/bootstrap']:
        print('b' * 64)
    elif len(args) == 1 and args[0].startswith('/var/run/runtime-credentials/'):
        print('invalid!' if os.environ.get('INVALID_PASSWORD') else 'a' * 64)
    else:
        sys.exit(2)
elif name == 'psql':
    if '--dbname' in args and args[args.index('--dbname') + 1] == os.environ.get('MISSING_DATABASE'):
        sys.exit(2)
    if 'authority_runtime_database_identities' in sql:
        print(2)
    elif 'AND NOT rolreplication AND NOT rolinherit' in sql:
        print(0 if os.environ.get('INVALID_ROLE') else 1)
    elif "rolname IN (" in sql:
        print(os.environ['EXPECTED_ROLES'])
    elif "rolname = 'kodex_backup_reader'" in sql and 'SELECT count(*)' in sql:
        print(1)
'''


class BackupReaderReconcile(unittest.TestCase):
    """Проверяет target-only запись, ранний отказ и сохранение обоих полных профилей."""

    def run_script(self, scope="backup-reader", profile="trusted-cluster", **flags):
        """Подставляет только дочерние CLI, сохраняя настоящий порядок shell-операций."""
        with tempfile.TemporaryDirectory(prefix="kodex-reader-test-") as directory:
            root = Path(directory)
            for name in ("cat", "psql", "pg_isready"):
                path = root / name
                path.write_text(STUB)
                path.chmod(0o700)
            trace = root / "trace.jsonl"
            env = {"PATH": str(root) + ":/usr/bin:/bin", "TRACE": str(trace),
                   "PGPASSWORD_FILE": "/fixture/bootstrap", "PGUSER": "postgres",
                   "KODEX_RPC_PROFILE": profile, "KODEX_RUNTIME_CREDENTIAL_SCOPE": scope,
                   "EXPECTED_ROLES": "1" if scope == "backup-reader" else "2" if profile == "trusted-cluster" else "22",
                   **flags}
            result = subprocess.run(["/bin/sh", str(SCRIPT)], env=env, capture_output=True, timeout=10)
            calls = [json.loads(line) for line in trace.read_text().splitlines()] if trace.exists() else []
            return result, calls

    def test_reader_only_changes_one_role_and_authenticates_both_databases(self):
        """Ремонт читает один runtime key, не создаёт роли и проверяет вход именно reader."""
        result, calls = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        reads = [c["args"] for c in calls if c["program"] == "cat"]
        self.assertEqual(reads, [["/fixture/bootstrap"], ["/var/run/runtime-credentials/kodex_backup_reader"],
                                 ["/var/run/runtime-credentials/kodex_backup_reader"]])
        sql = [c["sql"] for c in calls if c["program"] == "psql"]
        alterations = [s for s in sql if "ALTER ROLE" in s]
        self.assertEqual(len(alterations), 1)
        self.assertTrue(alterations[0].startswith("ALTER ROLE kodex_backup_reader PASSWORD "))
        self.assertFalse(any("CREATE ROLE" in s or "GRANT artifact_retention_runtime" in s for s in sql))
        reader = [c for c in calls if c["program"] == "psql" and c["user"] == "kodex_backup_reader"]
        self.assertEqual([c["args"][1] for c in reader], ["control_plane", "internal_rpc_authority"])
        self.assertTrue(all(c["passwordPresent"] for c in reader))
        self.assertFalse(b"a" * 64 in result.stdout + result.stderr)
        self.assertTrue(any("GRANT SELECT ON ALL TABLES IN SCHEMA public, internal_rpc_authority" in s for s in sql))

    def test_invalid_scope_fails_before_any_child_or_secret_read(self):
        """Неизвестный scope не превращается в широкий reconciliation."""
        result, calls = self.run_script(scope="backup-reader;all")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

    def test_invalid_role_or_missing_database_fails_before_alter(self):
        """Неожиданные role flags и отсутствующая БД не допускают частичный repair."""
        for flag in ({"INVALID_ROLE": "1"}, {"MISSING_DATABASE": "internal_rpc_authority"}):
            with self.subTest(flag=flag):
                result, calls = self.run_script(**flag)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any("ALTER ROLE" in c["sql"] or "GRANT SELECT" in c["sql"] for c in calls))
                self.assertFalse(any(c["program"] == "cat" and c["args"] != ["/fixture/bootstrap"] for c in calls))

    def test_invalid_password_is_never_sent_to_postgresql(self):
        """Неверный формат не попадает в SQL, stderr или успешный результат."""
        result, calls = self.run_script(INVALID_PASSWORD="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any("ALTER ROLE" in c["sql"] for c in calls))
        self.assertNotIn(b"invalid!", result.stdout + result.stderr)

    def test_full_profiles_keep_existing_role_registry(self):
        """Обычный deployment сохраняет точные наборы2/22 ролей и прежнюю authority-проверку."""
        for profile, count in (("trusted-cluster", 2), ("protected", 22)):
            with self.subTest(profile=profile):
                result, calls = self.run_script(scope="all", profile=profile)
                self.assertEqual(result.returncode, 0, result.stderr.decode())
                self.assertEqual(sum("ALTER ROLE" in c["sql"] for c in calls), count)
                self.assertEqual(any("authority_runtime_database_identities" in c["sql"] for c in calls), profile == "protected")


if __name__ == "__main__":
    unittest.main()
