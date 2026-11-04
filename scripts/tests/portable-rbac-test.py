"""Проверка настоящей shell-функции с kubectl stub под set -euo pipefail, без кластера."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
TEXT = (ROOT / 'tools/install/deploy-platform.sh').read_text()
FUNCTION = TEXT.split('verify_runtime_namespace_boundary() {', 1)[1].split('\napply_workloads_after_authority()', 1)[0]
FUNCTION = 'verify_runtime_namespace_boundary() {' + FUNCTION

STUB = r'''
umask 077
context=synthetic
namespace=synthetic-system
runtime_namespace=synthetic-runtime
fail() { printf '%s\n' "$*" >&2; exit 42; }
kubectl() {
  printf '%s\n' "$*" >>"$temporary_directory/calls"
  case "$scenario" in
    transport) printf 'PRIVATE_DIAGNOSTIC_SENTINEL\n' >&2; return 1 ;;
    warning) printf 'PRIVATE_DIAGNOSTIC_SENTINEL\n' >&2; printf 'yes\n'; return 0 ;;
    malformed) printf 'yes extra\n'; return 0 ;;
    denied-create) printf 'no\n'; return 1 ;;
    yes-one) printf 'yes\n'; return 1 ;;
    no-zero) printf 'no\n'; return 0 ;;
    no-two) printf 'no\n'; return 2 ;;
  esac
  case " $* " in
    *" get secrets "*|*" create pods --namespace synthetic-runtime --as system:serviceaccount:kodex-runtime:agent-runner "*)
      if [[ "$scenario" == unexpected-allow ]]; then printf 'yes\n'; return 0; fi
      printf 'no\n'; return 1 ;;
    *) printf 'yes\n'; return 0 ;;
  esac
}
'''


class Tests(unittest.TestCase):
    def run_check(self, scenario):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(['bash', '-euo', 'pipefail', '-c',
                'temporary_directory=$1\nscenario=$2\n' + STUB + FUNCTION +
                '\nverify_runtime_namespace_boundary\nprintf "RBAC_COMPLETE\\n"\n',
                'synthetic-rbac', directory, scenario], capture_output=True, text=True, timeout=10)
            calls = (Path(directory) / 'calls').read_text().splitlines()
            diagnostics = Path(directory) / 'runtime-rbac.stderr'
            if diagnostics.exists():
                self.assertEqual(diagnostics.stat().st_mode & 0o077, 0)
        self.assertNotIn('PRIVATE_DIAGNOSTIC_SENTINEL', result.stdout + result.stderr)
        self.assertTrue(all(' auth can-i ' in call for call in calls))
        return result, calls

    def test_expected_allow_and_deny_complete_under_errexit(self):
        result, calls = self.run_check('expected')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, 'RBAC_COMPLETE\n')
        self.assertEqual(len(calls), 10)
        self.assertTrue(any(' get secrets ' in call for call in calls))

    def test_mismatches_unavailable_and_inconsistent_responses_fail_closed(self):
        for scenario in ('unexpected-allow', 'denied-create', 'transport', 'warning',
                         'malformed', 'yes-one', 'no-zero', 'no-two'):
            with self.subTest(scenario=scenario):
                result, _ = self.run_check(scenario)
                self.assertEqual(result.returncode, 42)
                self.assertNotIn('RBAC_COMPLETE', result.stdout)


if __name__ == '__main__':
    unittest.main()
