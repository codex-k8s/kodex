"""Проверяет настоящие shell entrypoints: optional upgrade не повторяет исторические effects."""
import os
import json
import hashlib
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import yaml
from test_bootstrap_upgrade import fixture

ROOT = Path(__file__).resolve().parents[2]
TEXT = (ROOT/'tools/install/deploy-platform.sh').read_text()
FUNCTION = 'apply_job() {' + TEXT.split('apply_job() {',1)[1].split('\nwait_job_terminal()',1)[0]


class Tests(unittest.TestCase):
    def test_receipt_selects_one_materializer_from_verified_snapshot_before_mutations(self):
        block = 'materializer_job_name=release-artifact-materializer' + TEXT.split(
            'materializer_job_name=release-artifact-materializer', 1)[1].split('\nrender_filter() {', 1)[0]
        setup = r'''
set -euo pipefail
repository_root=/synthetic
render_file=/synthetic/verified-render.yaml
fail(){ printf '%s\n' "$*" >&2; exit 42; }
python3(){
  [[ "$*" == '-B /synthetic/tools/install/release_materializer.py --render /synthetic/verified-render.yaml' ]] || exit 90
  printf 'select\n' >&2
  [[ "$selected_name" != REJECT ]] || return 1
  printf '%s\n' "$selected_name"
}
'''
        for receipt in (False, True):
            for name in ('release-artifact-materializer', 'release-materializer-' + 'a' * 40,
                         'release-materializer-' + 'a' * 39, 'foreign-job', 'REJECT',
                         'release-artifact-materializer\nrelease-materializer-' + 'b' * 40):
                with self.subTest(receipt=receipt, name=name):
                    arguments = '(--bootstrap-completion-receipt synthetic)' if receipt else '()'
                    script = setup + '\ncompletion_arguments=' + arguments + '\n' + block + '\nprintf "%s\\n" "$materializer_job_name"\n'
                    result = subprocess.run(['bash'], input=script, text=True, capture_output=True, timeout=5,
                                            env={**os.environ, 'selected_name': name})
                    valid = name in ('release-artifact-materializer', 'release-materializer-' + 'a' * 40)
                    self.assertEqual(result.returncode, 0 if valid or not receipt else 42, result.stderr)
                    self.assertEqual(result.stderr.splitlines().count('select'), int(receipt))
                    if result.returncode == 0:
                        self.assertEqual(result.stdout.strip(), name if receipt else 'release-artifact-materializer')
        selection = TEXT.index('materializer_job_name=release-artifact-materializer')
        self.assertLess(selection, TEXT.index('if [[ "$mode" == defer-public-tls ]]; then'))
        self.assertLess(selection, TEXT.index('if [[ "$mode" == prepare-preflight ]]; then', selection))

    def test_upgrade_versioned_materializer_reuses_or_creates_selected_job_and_requires_same_readback(self):
        selection = 'materializer_job_name=release-artifact-materializer' + TEXT.split(
            'materializer_job_name=release-artifact-materializer', 1)[1].split('\nrender_filter() {', 1)[0]
        workloads = 'apply_workloads_after_authority() {' + TEXT.split(
            'apply_workloads_after_authority() {', 1)[1].split('\nif [[ "$mode" == prepare-preflight', 1)[0]
        readback = 'for job_name in ' + TEXT.split('for job_name in ', 1)[1].split('\nfailing_pods=', 1)[0]
        setup = r'''
set -euo pipefail
repository_root=/synthetic
render_file=/synthetic/verified-render.yaml
context=synthetic
namespace=kodex-system
mode=upgrade
profile_mode=portable
clamav_storage_profile=shared-rwx
completion_arguments=(--bootstrap-completion-receipt synthetic)
fail(){ printf '%s\n' "$*" >&2; exit 42; }
python3(){
  case "$1" in
    -B)
      [[ "$*" == '-B /synthetic/tools/install/release_materializer.py --render /synthetic/verified-render.yaml' ]] || exit 90
      printf 'select\n' >>"$temporary_directory/calls"
      printf '%s\n' "$selected_name" ;;
    */authority_registry.py)
      [[ "${*: -1}" == "$selected_name" ]] || exit 91
      printf 'existing:%s\n' "${*: -1}" >>"$temporary_directory/calls"
      [[ "$synthetic_presence" != REJECT ]] || return 1
      printf '%s\n' "$synthetic_presence" ;;
    */provider-bootstrap.py) [[ "$2" == recover ]] || exit 92 ;;
    *) exit 93 ;;
  esac
}
yq(){ [[ "$JOB_NAME" == "$selected_name" ]] || exit 94; printf 'selected-manifest\n'; }
portable_apply_file(){ printf 'apply:%s\n' "$(cat "$1")" >>"$temporary_directory/calls"; }
wait_job_terminal(){ [[ "$1" == "$selected_name" ]] || exit 95; printf 'wait:%s\n' "$1" >>"$temporary_directory/calls"; }
kubectl(){
  if [[ "$*" == *'rollout status '* ]]; then return; fi
  [[ "$*" == *"get job/$selected_name -o jsonpath="* ]] || exit 96
  printf 'readback:%s\n' "$selected_name" >>"$temporary_directory/calls"
  printf '1'
}
ensure_email_projection_secret(){ :; }
apply_render(){ :; }
wait_workloads(){ :; }
prune_role_environment_configmaps(){ :; }
'''
        selected = 'release-materializer-' + 'b' * 40
        for presence in ('PRESENT', 'ABSENT', 'UNKNOWN', 'REJECT'):
            with self.subTest(presence=presence), tempfile.TemporaryDirectory() as directory:
                script = setup + selection + '\n' + FUNCTION + '\n' + workloads + '\napply_workloads_after_authority\n' + readback
                result = subprocess.run(['bash'], input=script, text=True, capture_output=True, timeout=5,
                    env={**os.environ, 'temporary_directory': directory, 'selected_name': selected,
                         'synthetic_presence': presence})
                calls = (Path(directory) / 'calls').read_text().splitlines()
                expected = ['select', 'existing:' + selected]
                if presence in ('PRESENT', 'ABSENT'):
                    if presence == 'ABSENT': expected.append('apply:selected-manifest')
                    expected += ['wait:' + selected, 'readback:' + selected]
                self.assertEqual(result.returncode, 0 if presence in ('PRESENT', 'ABSENT') else 42, result.stderr)
                self.assertEqual(calls, expected)

    def test_upgrade_never_reconciles_versioned_materializer_script_by_deleting_it(self):
        function = 'reconcile_immutable_configmaps() {' + TEXT.split(
            'reconcile_immutable_configmaps() {', 1)[1].split('\nprune_role_environment_configmaps()', 1)[0]
        setup = r'''
set -euo pipefail
namespace=kodex-system
context=synthetic
render_file=/synthetic/not-opened
yq(){ printf '%s\n' "$script_name"; }
kubectl(){ printf 'query\n' >>"$test_calls"; return 1; }
'''
        for mode in ('upgrade', 'apply'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                calls = Path(directory) / 'calls'
                result = subprocess.run(['bash'], input=setup + function + '\nreconcile_immutable_configmaps\n',
                    env={**os.environ, 'mode': mode, 'script_name': 'kodex-materializer-script-' + 'a' * 24,
                         'test_calls': str(calls)}, capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(calls.exists(), mode == 'apply')

    def test_stock_upgrade_excludes_authority_registry_and_publisher_from_the_write_manifest(self):
        function = 'apply_render() {' + TEXT.split('apply_render() {',1)[1].split('\nwait_statefulset()',1)[0]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'source.yaml'
            registry = 'internal-rpc-authority-publisher-target-registry'
            publisher = 'internal-rpc-authority-publisher'
            desired = [('ConfigMap',registry,'kodex-system'), ('Deployment',publisher,'kodex-system'),
                       ('Deployment','synthetic-control-plane','kodex-system'), ('Deployment',publisher,'synthetic')]
            path.write_text(yaml.safe_dump_all([
                {'kind':kind,'metadata':{'name':name,'namespace':namespace}} for kind,name,namespace in desired]))
            setup = r'''
set -euo pipefail
profile_mode=portable
render_filter() {
  yq "$2" "$test_render" >"$test_output"
  printf '%s\n' "$test_output"
}
portable_apply_file() { cat "$1"; }
'''
            for mode, expected in (('upgrade',desired[2:]), ('apply',desired)):
                with self.subTest(mode=mode):
                    result = subprocess.run(['bash'],input=setup+function+'\napply_render synthetic ". | select(.kind != null)"\n',
                        env={**os.environ,'mode':mode,'test_render':str(path),'test_output':str(Path(directory)/'out.yaml')},
                        capture_output=True,text=True,timeout=5)
                    self.assertEqual(result.returncode,0,result.stderr)
                    actual = [(r['kind'],r['metadata']['name'],r['metadata']['namespace'])
                              for r in yaml.safe_load_all(result.stdout)]
                    self.assertEqual(actual,expected)

    def test_stock_upgrade_never_queries_authority_registry_for_immutable_replacement(self):
        function = 'reconcile_immutable_configmaps() {' + TEXT.split('reconcile_immutable_configmaps() {',1)[1].split('\nprune_role_environment_configmaps()',1)[0]
        setup = r'''
set -euo pipefail
namespace=kodex-system
context=synthetic
render_file=/synthetic/not-opened
yq(){ printf '%s\n' internal-rpc-authority-publisher-target-registry; }
kubectl(){ printf '%s\n' "${3:-}" >>"$test_calls"; return 1; }
'''
        with tempfile.TemporaryDirectory() as directory:
            calls = Path(directory)/'calls'
            for mode, wanted in (('upgrade',False), ('apply',True)):
                if calls.exists(): calls.unlink()
                result = subprocess.run(['bash'],input=setup+function+'\nreconcile_immutable_configmaps\n',
                    env={**os.environ,'mode':mode,'test_calls':str(calls)}, capture_output=True,text=True,timeout=5)
                self.assertEqual(result.returncode,0,result.stderr)
                self.assertEqual(calls.exists(),wanted)

    def test_final_readback_retains_the_verified_upgrade_completion_receipt(self):
        block=TEXT.rsplit('if [[ -n "${profile_mode:-}" ]]; then',1)[1].split('\nfi',1)[0]
        expected=['--bootstrap-completion-receipt','/synthetic/composite.json',
                  '--bootstrap-completion-sha256','a'*64,'--allow-reconciled-bootstrap-completion']
        for arguments in ([],expected):
            setup='set -euo pipefail\nrepository_root=/synthetic\ncontext=synthetic\nrender_file=/synthetic/render.yaml\ncompletion_arguments=('+ ' '.join(arguments)+')\ntimeout(){ shift; "$@"; }\npython3(){ printf "%s\\n" "$@"; }\n'
            result=subprocess.run(['bash'],input=setup+block,text=True,capture_output=True,timeout=5)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertEqual(result.stdout.splitlines(),['/synthetic/tools/install/portable_state.py','readback',
                '--context','synthetic','--render','/synthetic/render.yaml']+arguments)

    def test_system_assistant_readback_uses_exact_leader_without_corrupting_json(self):
        function='wait_system_assistant() {'+TEXT.split('wait_system_assistant() {',1)[1].split('\n}',1)[0]+'\n}'
        for scenario in ('ready','empty-first','foreign-first','invalid','forward-ready','forward-retry'):
            with self.subTest(scenario=scenario),tempfile.TemporaryDirectory()as directory:
                script=r'''
set -euo pipefail
context=synthetic
namespace=kodex-system
runtime_namespace=kodex-runtime
repository_root=/synthetic
fail(){ printf '%s\n' "$*" >&2; exit 42; }
sleep(){ printf 'wait\n' >>"$HOME/calls"; }
python3(){
  [[ "$*" == '/synthetic/tools/install/assistant_readback.py --context synthetic --pod synthetic-controller --pod-uid synthetic-leader' ]] || return 23
  printf 'forward\n' >>"$HOME/calls"
  if [[ "$scenario" == forward-retry && ! -e "$HOME/first-forward" ]]; then
    : >"$HOME/first-forward"; return 1
  fi
  printf 'leader-ready\n' >>"$HOME/calls"
}
kubectl(){
  case "$*" in
    *'get pod/system-assistant-warm -o json')
      printf '%s\n' '{"status":{"conditions":[{"type":"Ready","status":"True"}]},"spec":{"volumes":[{"name":"session","persistentVolumeClaim":{"claimName":"synthetic-session"}}]}}' ;;
    *'get pvc/synthetic-session -o json')
      printf '%s\n' '{"status":{"phase":"Bound"},"spec":{"storageClassName":"synthetic"}}' ;;
    *'get lease/runtime-controller-leader -o jsonpath='*) printf '%s' synthetic-leader ;;
    *'get pods -l app.kubernetes.io/name=runtime-controller -o json')
      if [[ ! -e "$HOME/first-list" ]]; then
        : >"$HOME/first-list"
        case "$scenario" in
          empty-first) return 1 ;;
          foreign-first) printf '%s\n' '{"items":[{"metadata":{"uid":"foreign","name":"foreign"}}]}'; return ;;
          invalid) printf '%s\n' '{invalid'; return ;;
        esac
      fi
      printf '%s\n' '{"items":[{"metadata":{"uid":"foreign","name":"foreign"}},{"metadata":{"uid":"synthetic-leader","name":"synthetic-controller"}}]}' ;;
    *'get --raw /api/v1/namespaces/kodex-system/pods/synthetic-controller:9090/proxy/assistant/readyz')
      printf 'leader-ready\n' >>"$HOME/calls" ;;
    *) printf 'unexpected-command\n' >>"$HOME/calls"; return 23 ;;
  esac
}
'''
                result=subprocess.run(['bash'],input=script+'\n'+function+'\nwait_system_assistant\n',
                    text=True,capture_output=True,timeout=10,
                    env={'PATH':os.environ['PATH'],'HOME':directory,'LANG':'C','scenario':scenario,
                         'assistant_readback_transport':'port-forward'if scenario.startswith('forward-')else'api-proxy'})
                path=Path(directory)/'calls';calls=path.read_text().splitlines()if path.exists()else[]
                if scenario=='invalid':
                    self.assertNotEqual(result.returncode,0);self.assertNotIn('leader-ready',calls)
                else:
                    self.assertEqual(result.returncode,0,result.stderr)
                    expected={'forward-ready':['forward','leader-ready'],
                              'forward-retry':['forward','wait','forward','leader-ready']}
                    self.assertEqual(calls,expected.get(scenario,['wait','leader-ready']if scenario.endswith('-first')else['leader-ready']))

    def test_unknown_assistant_transport_is_rejected_before_cluster_access(self):
        for name in ('portable-release.sh','deploy-platform.sh'):
            result=subprocess.run(['bash',str(ROOT/'tools/install'/name),'--mode','readback',
                '--context','synthetic','--assistant-readback-transport','unsafe'],
                capture_output=True,text=True,timeout=5)
            self.assertNotEqual(result.returncode,0)
            self.assertIn('assistant readback transport is invalid',result.stderr)

    def test_email_projection_upgrade_and_preflight_require_existing_without_seed(self):
        function='ensure_email_projection_secret() {'+TEXT.split('ensure_email_projection_secret() {',1)[1].split('\n}',1)[0]+'\n}'
        for mode,arguments in [('upgrade','()'),('preflight','(--existing-only)'),('apply','()')]:
            script='set -euo pipefail\nrepository_root=/synthetic\nrender_file=/synthetic/render.yaml\ncontext=synthetic\nmode='+mode+'\nemail_projection_arguments='+arguments+'\npython3(){ printf "%s\\n" "$@"; }\nfail(){ exit 42; }\n'+function+'\nensure_email_projection_secret --dry-run\n'
            value=subprocess.run(['bash'],input=script,text=True,capture_output=True,timeout=10)
            self.assertEqual(value.returncode,0,value.stderr)
            self.assertEqual('--existing-only'in value.stdout.splitlines(),mode!='apply')
            self.assertIn('--dry-run',value.stdout.splitlines())

    def test_actual_prepare_cli_and_receipt_gate_never_access_cluster(self):
        with tempfile.TemporaryDirectory()as directory:
            root=Path(directory);_,_,previous,target,profile=fixture()
            for name,raw in [('previous.yaml',previous),('target.yaml',target),('profile.json',json.dumps(profile).encode())]:
                p=root/name;p.write_bytes(raw);p.chmod(0o600)
            binary=root/'bin';binary.mkdir();kubectl=binary/'kubectl'
            kubectl.write_text('#!/bin/sh\nprintf called > "$HOME/cluster-access"\nexit 23\n');kubectl.chmod(0o700)
            environment={'PATH':str(binary)+':'+os.environ['PATH'],'HOME':directory,'LANG':'C','PYTHONDONTWRITEBYTECODE':'1'}
            command=['python3',str(ROOT/'tools/install/bootstrap_upgrade.py')]
            value=subprocess.run(command+['prepare','--profile',str(root/'profile.json'),'--previous-render',str(root/'previous.yaml'),
                '--target-render',str(root/'target.yaml'),'--output',str(root/'upgrade.yaml')],env=environment,capture_output=True,text=True,timeout=10)
            self.assertEqual(value.returncode,0,value.stderr)
            self.assertTrue(any(r.get('kind')=='ConfigMap'and r['metadata']['name'].startswith('kodex-bootstrap-upgrade-')
                                for r in yaml.safe_load_all((root/'upgrade.yaml').read_text())))
            value=subprocess.run(command+['verify-apply','--render',str(root/'upgrade.yaml'),'--context','synthetic'],
                env=environment,capture_output=True,text=True,timeout=10)
            self.assertNotEqual(value.returncode,0);self.assertFalse((root/'cluster-access').exists())

    def test_upgrade_rejects_missing_explicit_receipt_before_cluster_access(self):
        for name in ('portable-release.sh','deploy-platform.sh'):
            value = subprocess.run(['bash',str(ROOT/'tools/install'/name),'--mode','upgrade','--context','synthetic',
                '--render','/synthetic/not-opened.yaml'],capture_output=True,text=True,
                env={'PATH':os.environ['PATH'],'HOME':'/synthetic','LANG':'C'},timeout=10)
            self.assertNotEqual(value.returncode,0)
            self.assertIn('upgrade requires',value.stderr)

    def test_upgrade_keeps_the_same_render_when_the_input_path_is_replaced(self):
        for mode in ('upgrade', 'prepare-preflight', 'preflight', 'resume-authority-bootstrap',
                     'resume-authority-published', 'resume-workloads', 'readback'):
            with self.subTest(mode=mode):
                self.check_verified_render_snapshot(mode)

    def check_verified_render_snapshot(self, mode):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            render = root / 'original.yaml'
            render.write_text('approved render\n')
            render.chmod(0o600)
            kubectl = root / 'kubectl'
            kubectl.write_text('#!/bin/sh\n[ "$*" = "config current-context" ] || exit 91\nprintf "synthetic\\n"\n')
            kubectl.chmod(0o700)
            python = root / 'python3'
            python.write_text(r'''#!/bin/sh
case "$1" in
  -B) exec "$test_python" "$@" ;;
  */authority_registry.py)
    [ "$2" = verify-bootstrap ] && [ "$3" = --render ] && [ "$4" != "$test_render" ] || exit 92
    [ "$(cat "$4")" = "approved render" ] || exit 93
    printf '%s\n' "$4" >"$test_directory/snapshot-path"
    printf 'replaced input\n' >"$test_render"
    ;;
  */bootstrap_upgrade.py)
    [ "$2" = verify-apply ] && [ "$3" = --render ] || exit 94
    [ "$4" = "$(cat "$test_directory/snapshot-path")" ] || exit 95
    [ "$(cat "$4")" = "approved render" ] || exit 96
    exit 76
    ;;
  */provider_sandbox_profile.py)
    [ "$test_mode" != upgrade ] && [ "$2" = --check ] && [ "$3" = --render ] || exit 98
    [ "$4" = "$(cat "$test_directory/snapshot-path")" ] || exit 95
    [ "$(cat "$4")" = "approved render" ] || exit 96
    exit 76
    ;;
  *) exit 97 ;;
esac
''')
            python.chmod(0o700)
            environment = {'PATH':str(root)+':'+os.environ['PATH'], 'HOME':directory, 'LANG':'C',
                           'test_python':sys.executable, 'test_render':str(render), 'test_directory':directory,
                           'test_mode':mode}
            value = subprocess.run(['bash', str(ROOT/'tools/install/deploy-platform.sh'), '--mode', mode,
                '--context', 'synthetic', '--render', str(render), '--public-tls-mode', 'external',
                '--bootstrap-completion-receipt', 'synthetic.json', '--bootstrap-completion-sha256', 'a'*64],
                env=environment, capture_output=True, text=True, timeout=10)
            self.assertEqual(value.returncode, 76, value.stderr)
            self.assertEqual(render.read_text(), 'replaced input\n')
            snapshot = Path((root/'snapshot-path').read_text().strip())
            self.assertFalse(snapshot.exists())

    def run_job(self,mode,name,presence='PRESENT'):
        with tempfile.TemporaryDirectory()as directory:
            setup = r'''
set -euo pipefail
context=synthetic
render_file=/synthetic/render.yaml
repository_root=/synthetic
profile_mode=portable
fail() { printf '%s\n' "$*" >&2; exit 42; }
yq() { printf 'synthetic-manifest\n'; }
python3() { printf 'existing-job\n' >>"$temporary_directory/calls"; [[ "$synthetic_presence" != REJECT ]] || return 1; printf '%s\n' "$synthetic_presence"; }
portable_apply_file() { printf 'apply\n' >>"$temporary_directory/calls"; }
portable_delete() { printf 'delete\n' >>"$temporary_directory/calls"; }
wait_job_terminal() { printf 'wait\n' >>"$temporary_directory/calls"; }
kubectl() { printf 'unexpected-kubectl\n' >>"$temporary_directory/calls"; return 1; }
'''
            environment={'PATH':os.environ['PATH'],'HOME':directory,'LANG':'C','mode':mode,'name':name,
                         'synthetic_presence':presence,'temporary_directory':directory}
            value=subprocess.run(['bash'],input=setup+'\n'+FUNCTION+'\napply_job "$name"\n',
                text=True,capture_output=True,env=environment,timeout=10)
            path=Path(directory)/'calls';calls=path.read_text().splitlines()if path.exists()else[]
            return value,calls

    def test_all_five_preserved_effects_are_skipped_without_delete_or_create(self):
        for name in ('email-bridge-migration','internal-rpc-authority-migrate','control-plane-migrate',
                     'kodex-postgresql-runtime-credentials','control-plane-broker-bootstrap'):
            result,calls=self.run_job('upgrade',name)
            self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(calls,[])

    def test_materializer_rejoins_or_creates_once_and_stops_on_unknown_state(self):
        for presence,wanted in [('PRESENT',['existing-job','wait']),('ABSENT',['existing-job','apply','wait'])]:
            result,calls=self.run_job('upgrade','release-artifact-materializer',presence)
            self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(calls,wanted)
        result,calls=self.run_job('upgrade','release-artifact-materializer','REJECT')
        self.assertEqual(result.returncode,42);self.assertEqual(calls,['existing-job'])

    def test_fresh_apply_retains_previous_job_lifecycle(self):
        result,calls=self.run_job('apply','control-plane-migrate')
        self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(calls,['delete','apply','wait'])


class PreparedRenderFreshGuardTests(unittest.TestCase):
    def run_entrypoint(self, entry, resource, *, replace_input=False):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            render = base / 'render.yaml'
            render.write_text(yaml.safe_dump(resource))
            render.chmod(0o600)
            binary = base / 'bin'
            binary.mkdir()
            kubectl = binary / 'kubectl'
            # Полный entrypoint не может вызвать настоящий kubectl, даже до исправления.
            kubectl.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >>"$TEST_KUBECTL_CALLS"\n'
                               '[ "$*" = "config current-context" ] || exit 91\nprintf synthetic\n')
            kubectl.chmod(0o700)
            python = binary / 'python3'
            python.write_text('#!' + sys.executable + '\n' + r'''
import hashlib, os, pathlib, stat, sys
args = sys.argv[1:]
path = pathlib.Path(args[args.index('--render') + 1])
original = pathlib.Path(os.environ['TEST_ORIGINAL'])
with open(os.environ['TEST_HELPER_CALLS'], 'a') as stream:
    stream.write(path.name + '\n')
if os.environ['TEST_REPLACE'] == 'true':
    original.write_text('replaced input\n')
    assert path != original
    assert hashlib.sha256(path.read_bytes()).hexdigest() == os.environ['TEST_SHA']
    assert stat.S_IMODE(path.stat().st_mode) == 0o600
# Первый pure helper завершает оснастку до inventory, admission или effects.
sys.exit(77)
''')
            python.chmod(0o700)
            digest = hashlib.sha256(render.read_bytes()).hexdigest()
            arguments = ['bash', str(ROOT / 'tools/install' / entry), '--mode', 'apply',
                         '--context', 'synthetic', '--render', str(render)]
            if entry == 'portable-release.sh':
                arguments += ['--render-sha256', digest]
            kubectl_calls = base / 'kubectl-calls'
            helper_calls = base / 'helper-calls'
            result = subprocess.run(arguments, capture_output=True, text=True, timeout=5,
                env={**os.environ, 'PATH': str(binary) + ':' + os.environ['PATH'],
                     'TEST_KUBECTL_CALLS': str(kubectl_calls), 'TEST_HELPER_CALLS': str(helper_calls),
                     'TEST_ORIGINAL': str(render), 'TEST_REPLACE': str(replace_input).lower(), 'TEST_SHA': digest})
            calls = kubectl_calls.read_text().splitlines() if kubectl_calls.exists() else []
            helpers = helper_calls.read_text().splitlines() if helper_calls.exists() else []
            return result, calls, helpers

    def test_prepared_render_is_rejected_by_both_entrypoints_before_inventory_or_effects(self):
        for entry in ('deploy-platform.sh', 'portable-release.sh'):
            with self.subTest(entry=entry):
                resource = {'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {
                    'namespace': 'kodex-system', 'name': 'release-materializer-' + 'b' * 40}}
                result, calls, helpers = self.run_entrypoint(entry, resource)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn('prepared upgrade render requires verified completion receipt and upgrade mode', result.stderr)
                self.assertTrue(all(call == 'config current-context' for call in calls))
                self.assertEqual(helpers, [])

    def test_legitimate_fresh_apply_consumes_same_private_snapshot_after_input_replacement(self):
        resource = {'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {
            'namespace': 'kodex-system', 'name': 'release-artifact-materializer'}}
        for entry in ('deploy-platform.sh', 'portable-release.sh'):
            with self.subTest(entry=entry):
                result, calls, helpers = self.run_entrypoint(entry, resource, replace_input=True)
                self.assertEqual(result.returncode, 77, result.stderr)
                self.assertTrue(all(call == 'config current-context' for call in calls))
                self.assertEqual(len(helpers), 1)

    def test_guard_is_exact_and_readonly_plan_and_upgrade_are_unchanged(self):
        for entry in ('deploy-platform.sh', 'portable-release.sh'):
            source = (ROOT / 'tools/install' / entry).read_text()
            command = source.index('  yq -N -o=json -I=0')
            start = source.rfind('if [[ "$mode" == apply ]]; then', 0, command)
            guard = source[start:source.index('\nfi', command) + len('\nfi')] + '\n'
            for mode in ('apply', 'plan', 'upgrade', 'readback'):
                for api, kind, name, namespace, blocked in (
                    ('batch/v1', 'Job', 'release-materializer-' + 'a' * 40, 'kodex-system', True),
                    ('batch/v1', 'Job', 'release-artifact-materializer', 'kodex-system', False),
                    ('batch/v1', 'Job', 'release-materializer-' + 'a' * 39, 'kodex-system', False),
                    ('batch/v1', 'Job', 'release-materializer-' + 'a' * 41, 'kodex-system', False),
                    ('batch/v1', 'Job', 'release-materializer-' + 'a' * 40, 'other', False),
                    ('v1', 'ConfigMap', 'release-materializer-' + 'a' * 40, 'kodex-system', False),
                    ('other/v1', 'Job', 'release-materializer-' + 'a' * 40, 'kodex-system', False)):
                    with self.subTest(entry=entry, mode=mode, api=api, kind=kind, name=name, namespace=namespace), tempfile.TemporaryDirectory() as directory:
                        path = Path(directory) / 'render.yaml'
                        path.write_text(yaml.safe_dump({'apiVersion': api, 'kind': kind,
                            'metadata': {'name': name, 'namespace': namespace}}))
                        (Path(directory) / 'verified-render.yaml').write_bytes(path.read_bytes())
                        result = subprocess.run(['bash'], input='set -euo pipefail\nfail(){ exit 42; }\n' + guard,
                            capture_output=True, text=True, timeout=5,
                            env={**os.environ, 'render_file': str(path), 'temporary': directory,
                                 'temporary_directory': directory, 'mode': mode})
                        self.assertEqual(result.returncode, 42 if blocked and mode == 'apply' else 0, result.stderr)


if __name__=='__main__':unittest.main()
