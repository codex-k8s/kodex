"""Проверяет настоящие shell entrypoints: optional upgrade не повторяет исторические effects."""
import os
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
import yaml
from test_bootstrap_upgrade import fixture

ROOT = Path(__file__).resolve().parents[2]
TEXT = (ROOT/'tools/install/deploy-platform.sh').read_text()
FUNCTION = 'apply_job() {' + TEXT.split('apply_job() {',1)[1].split('\nwait_job_terminal()',1)[0]


class Tests(unittest.TestCase):
    def test_system_assistant_readback_uses_exact_leader_without_corrupting_json(self):
        function='wait_system_assistant() {'+TEXT.split('wait_system_assistant() {',1)[1].split('\n}',1)[0]+'\n}'
        for scenario in ('ready','empty-first','foreign-first','invalid'):
            with self.subTest(scenario=scenario),tempfile.TemporaryDirectory()as directory:
                script=r'''
set -euo pipefail
context=synthetic
namespace=kodex-system
runtime_namespace=kodex-runtime
fail(){ printf '%s\n' "$*" >&2; exit 42; }
sleep(){ printf 'wait\n' >>"$HOME/calls"; }
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
                    env={'PATH':os.environ['PATH'],'HOME':directory,'LANG':'C','scenario':scenario})
                path=Path(directory)/'calls';calls=path.read_text().splitlines()if path.exists()else[]
                if scenario=='invalid':
                    self.assertNotEqual(result.returncode,0);self.assertNotIn('leader-ready',calls)
                else:
                    self.assertEqual(result.returncode,0,result.stderr)
                    self.assertEqual(calls,['wait','leader-ready']if scenario.endswith('-first')else['leader-ready'])

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


if __name__=='__main__':unittest.main()
