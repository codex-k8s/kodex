#!/usr/bin/env python3
"""Проверяет настоящий startup helper без Kubernetes, сети и credentials."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
INSTALLER = ROOT / 'tools/install/deploy-platform.sh'

# Закрытая оснастка выполняет только реальные selector expressions на synthetic YAML.
# PATH пуст: новый внешний вызов в installer не сможет случайно обратиться к кластеру.
HARNESS = r'''#!/usr/bin/env bash
set -euo pipefail
repository_root=$FIXTURE_REPOSITORY
context=synthetic-context
namespace=kodex-system
clamav_storage_profile=$FIXTURE_CLAMAV_STORAGE
render_file=$FIXTURE_MANIFEST
yq() { "$FIXTURE_YQ" "$@"; }
stage() {
  printf '%s\n' "$1" >>"$FIXTURE_TRACE"
  [[ "$FIXTURE_SCENARIO" != "fail-$1" ]]
}
fail() { printf 'Startup fixture rejected: %s\n' "$*" >&2; exit 1; }
ensure_email_projection_secret() { stage email-projection; }
apply_render() {
  [[ $# == 2 && $1 == workloads ]] || fail 'unexpected apply phase'
  "$FIXTURE_YQ" -r -N "$2 | .kind + \"/\" + .metadata.name" "$FIXTURE_MANIFEST" >"$FIXTURE_SELECTED"
  local selected expected
  selected=$(<"$FIXTURE_SELECTED")
  expected=$'Deployment/control-plane\nDeployment/secret-broker\nDeployment/internal-rpc-authority-readback-attestor\nDeployment/role-image-builder\nDaemonSet/synthetic-pull-guard\nCronJob/synthetic-housekeeping'
  [[ "$selected" == "$expected" ]] || fail 'startup consumer or non-workload selection differs'
  stage startup-consumers
}
kubectl() {
  [[ $# == 8 && $1 == --context && $2 == "$context" && $3 == -n && $4 == "$namespace" &&
     $5 == rollout && $6 == status && ( $8 == --timeout=15m || ( $7 == deployment/clamav-egress-gateway && $8 == --timeout=5m ) ) ]] || fail 'unexpected Kubernetes command'
  local deployment=${7#deployment/}
  case "$7" in
    deployment/clamav-egress-gateway|deployment/control-plane|deployment/egress-gateway|deployment/kodex-image-registry-promotion|deployment/kodex-image-registry-pull|deployment/kodex-buildkit) ;;
    *) fail 'unexpected readiness target' ;;
  esac
  stage "ready:$deployment"
}
python3() {
  [[ $# == 4 && $1 == "$repository_root/tools/install/provider-bootstrap.py" && $2 == recover &&
     $3 == --context && $4 == "$context" ]] || fail 'unexpected provider command'
  stage provider-bootstrap
}
apply_job() {
  [[ $# == 1 ]] || fail 'unexpected Job arguments'
  case "$1" in
    release-artifact-materializer) stage materializer-terminal ;;
    clamav-db-bootstrap-aaaaaaaaaaaaaaaa) stage clamav-bootstrap-terminal ;;
    *) fail 'unexpected Job' ;;
  esac
}
wait_workloads() { stage all-workloads-ready; }
prune_role_environment_configmaps() { stage prune-owned-role-catalog; }
# shellcheck source=/dev/null
source "$FIXTURE_FUNCTION"
apply_workloads_after_authority
'''

WORKLOADS = [
    ('Deployment', 'control-plane'), ('Deployment', 'secret-broker'),
    ('Deployment', 'internal-rpc-authority-readback-attestor'), ('Deployment', 'role-image-builder'),
    ('DaemonSet', 'synthetic-pull-guard'), ('CronJob', 'synthetic-housekeeping'),
    ('ConfigMap', 'not-a-workload'), ('Job', 'not-a-startup-job'), ('StatefulSet', 'not-a-startup-statefulset'),
]
STAGES = [
    'email-projection', 'startup-consumers', 'ready:control-plane', 'provider-bootstrap',
    'ready:egress-gateway', 'ready:kodex-image-registry-promotion', 'materializer-terminal',
    'ready:kodex-image-registry-pull', 'ready:kodex-buildkit', 'all-workloads-ready', 'prune-owned-role-catalog',
]


def installer_helper():
    """Извлекает единственную canonical top-level функцию; исходная реализация не копируется."""
    source = INSTALLER.read_text()
    signature = 'apply_workloads_after_authority() {\n'
    if source.count(signature) != 1:
        raise AssertionError('exact installer startup helper is required')
    start = source.index(signature)
    end = source.index('\n}\n', start) + 3
    return source[start:end]


class InstallWorkloadStartupTest(unittest.TestCase):
    def test_real_helper_keeps_consumers_and_fail_closed_gates(self):
        bash, yq = shutil.which('bash'), shutil.which('yq')
        self.assertTrue(bash and yq, 'Bash and repository yq are required')
        with tempfile.TemporaryDirectory(prefix='kodex-startup-contract-') as directory:
            temporary = Path(directory)
            (temporary / 'empty-path').mkdir()
            helper = temporary / 'helper.sh'; helper.write_text(installer_helper())
            harness = temporary / 'harness.sh'; harness.write_text(HARNESS)
            fixture = temporary / 'workloads.yaml'
            fixture.write_text('---\n'.join('apiVersion: v1\nkind: '+kind+'\nmetadata:\n  name: '+name+'\n' for kind, name in WORKLOADS))
            for script in (helper, harness):
                syntax = subprocess.run([bash, '-n', str(script)], capture_output=True, timeout=5)
                self.assertEqual(syntax.returncode, 0, 'generated Bash fixture syntax differs')
            with fixture.open('a') as stream:stream.write('---\napiVersion: batch/v1\nkind: Job\nmetadata:\n  name: clamav-db-bootstrap-aaaaaaaaaaaaaaaa\n  annotations:\n    kodex.dev/clamav-bootstrap-sha256: '+ 'a'*64+'\n')
            scenarios=[]
            for profile in ['shared-rwx','node-local-rwo']:
                stages=STAGES if profile=='shared-rwx' else STAGES[:2]+['ready:clamav-egress-gateway','clamav-bootstrap-terminal']+STAGES[2:]
                scenarios += [(profile,'success',stages)]+[(profile,'fail-'+stage,stages[:index+1]) for index,stage in enumerate(stages)]
            for index, (profile, scenario, expected) in enumerate(scenarios):
                with self.subTest(scenario=scenario):
                    trace = temporary / ('trace-'+str(index)); selected = temporary / ('selected-'+str(index))
                    environment = {'PATH': str(temporary/'empty-path'), 'HOME': directory,
                                   'FIXTURE_REPOSITORY': str(ROOT), 'FIXTURE_YQ': yq, 'FIXTURE_CLAMAV_STORAGE': profile,
                                   'FIXTURE_FUNCTION': str(helper), 'FIXTURE_MANIFEST': str(fixture),
                                   'FIXTURE_TRACE': str(trace), 'FIXTURE_SELECTED': str(selected), 'FIXTURE_SCENARIO': scenario}
                    result = subprocess.run([bash, '--noprofile', '--norc', str(harness)], env=environment,
                                            cwd=directory, capture_output=True, timeout=5)
                    actual = trace.read_text().splitlines() if trace.exists() else []
                    self.assertEqual(result.returncode == 0, scenario == 'success', 'wrong startup outcome: '+scenario+' ' + result.stderr.decode(errors='replace')[:512])
                    self.assertEqual(actual, expected, 'mandatory gate was skipped or later effect ran: '+scenario)


if __name__ == '__main__':
    unittest.main()
