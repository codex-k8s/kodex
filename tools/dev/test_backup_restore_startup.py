"""Проверяет настоящий read-only startup shell и все ветви disposable restore render."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import scoped_backup_drill as drill

IMAGE = "registry.local.kodex/kodex/backup-controller@sha256:" + "a" * 64
HOST = "qa-postgresql.kodex-qa-restore-fixture.svc.cluster.local"


class RestoreStartup(unittest.TestCase):
    """Не использует кластер, PostgreSQL или реальные credentials."""

    def run_barrier(self, failures):
        """Подменяет только psql/sleep; исполняет настоящий shell и настоящий timeout."""
        with tempfile.TemporaryDirectory(prefix="restore-startup-") as directory:
            root = Path(directory)
            trace = root / "trace"
            stub = root / "psql"
            stub.write_text("""#!/usr/bin/python3
import json, os, pathlib, sys
p=pathlib.Path(os.environ['TRACE'])
calls=p.read_text().splitlines() if p.exists() else []
with p.open('a') as f: f.write(json.dumps({'args':sys.argv[1:], 'host':os.environ['PGHOST'], 'tls':os.environ['PGSSLMODE']})+'\\n')
print('PRIVATE_SENTINEL',file=sys.stderr)
sys.exit(1 if len(calls)<int(os.environ['FAILURES']) else 0)
""")
            stub.chmod(0o700)
            sleeper = root / "sleep"
            sleeper.write_text("#!/bin/sh\n[ \"$1\" = 2 ]\n")
            sleeper.chmod(0o700)
            barrier = drill.target_connectivity_barrier(IMAGE, HOST)
            env = {e["name"]: e["value"] for e in barrier["env"] if "value" in e}
            env.update({"PATH": directory + ":/usr/bin:/bin", "TRACE": str(trace), "FAILURES": str(failures)})
            result = subprocess.run(barrier["command"] + barrier["args"], env=env, capture_output=True, timeout=10)
            calls = [json.loads(line) for line in trace.read_text().splitlines()]
            return result, calls

    def test_transient_failure_recovers_without_main_effect(self):
        """До успеха толькоSELECT1; вывод дочернего процесса не попадает в журнал."""
        result, calls = self.run_barrier(2)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(len(calls), 3)
        self.assertTrue(all(c == {"args": ["-X", "-w", "-tAc", "SELECT 1"], "host": HOST, "tls": "verify-full"} for c in calls))
        self.assertNotIn(b"PRIVATE_SENTINEL", result.stdout + result.stderr)

    def test_permanent_failure_is_bounded_and_closed(self):
        """После10неудач init завершается ошибкой и не вызывает restore/cleanup."""
        result, calls = self.run_barrier(100)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(len(calls), 10)
        self.assertIn(b"readiness exhausted", result.stderr)
        self.assertNotIn(b"PRIVATE_SENTINEL", result.stdout + result.stderr)

    def test_real_render_binds_only_disposable_target_and_preserves_fingerprint(self):
        """Все network consumers получают barrier; hash-only fingerprint не получает пароль или init."""
        result = drill.restore_resources("fixture", IMAGE, "qa-1830-fixture")
        self.assertNotIn("initContainers", result["fingerprintJob"]["spec"]["template"]["spec"])
        for key in ("preflightJob", "restoreJob", "targetInspectionJob", "cleanupJob", "cleanupReadbackJob"):
            pod = result[key]["spec"]["template"]["spec"]
            self.assertEqual(pod["initContainers"], [drill.target_connectivity_barrier(IMAGE, HOST)])
            self.assertFalse(pod["automountServiceAccountToken"])
            init = pod["initContainers"][0]
            self.assertEqual(init["volumeMounts"], [{"name": "tls", "mountPath": "/var/run/secrets/kodex/backup-controller/tls", "readOnly": True}])
            self.assertEqual(init["env"][-1]["valueFrom"]["secretKeyRef"], {"name": "backup-controller-restore-inputs", "key": "password"})
            self.assertNotIn("kodex-postgresql-runtime-credentials", json.dumps(init))
            self.assertIn("timeout -k 1 3", init["args"][0])


if __name__ == "__main__":
    unittest.main()
