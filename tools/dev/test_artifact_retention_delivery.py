"""Герметичные проверки первой доставки retention без доступа к кластеру."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('deploy-local.sh')
SOURCE = SCRIPT.read_text()
SPEC = importlib.util.spec_from_file_location('cutover_fixture', SCRIPT.with_name('test_deploy_local_cutover.py'))
FIXTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FIXTURE)
PREFIX = FIXTURE.PREFIX
run = FIXTURE.run
functions = FIXTURE.functions


class ArtifactRetentionDeliveryTest(unittest.TestCase):
    def test_public_selected_guard_is_narrow(self):
        header = SOURCE[:SOURCE.index('[[ -f "$render"')]
        for stage, workload, valid in [('core', 'artifact-retention', True),
                ('core', 'artifact-retention-shadow', False), ('migrate', 'artifact-retention', False),
                ('supply-chain', 'artifact-retention', False)]:
            result = subprocess.run(['bash', '-euo', 'pipefail', '-c', header, '--',
                '--context', 'k3d-kodex', '--mode', 'readback', '--security-profile', 'trusted-cluster',
                '--stage', stage, '--workload', workload], capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode == 0, valid, result.stderr)

    def test_bootstrap_absence_is_exact_and_api_failures_are_closed(self):
        empty = {'kind': 'List', 'items': []}
        command = functions('quiesce_local_supply_chain_workload') + PREFIX + '''
kubectl() {
 case "$*" in
  *get\\ deployments*) [[ "$DEPLOY_EXIT" == 0 ]] || return 1; printf '%s\\n' "$DEPLOYMENTS" ;;
  *get\\ replicasets,pods*) [[ "$CONSUMER_EXIT" == 0 ]] || return 1; printf '%s\\n' "$CONSUMERS" ;;
  *) return 99 ;;
 esac
}
mode=apply
quiesce_local_supply_chain_workload artifact-retention
mode=readback
quiesce_local_supply_chain_workload artifact-retention
[[ "$supply_chain_quiesce_retention_inventory" == ABSENT ]]
'''
        cases = [(empty, empty, '0', '0', True), (empty, empty, '1', '0', False),
                 (empty, empty, '0', '1', False), ({}, empty, '0', '0', False),
                 ({'kind': 'List', 'items': None}, empty, '0', '0', False),
                 ({'kind': 'List', 'items': [], 'metadata': {'continue': 'partial'}}, empty, '0', '0', False),
                 (empty, {'kind': 'List', 'items': [], 'metadata': {'remainingItemCount': 1}}, '0', '0', False),
                 (empty, {'kind': 'List', 'items': [{'kind': 'Pod'}]}, '0', '0', False),
                 ({'kind': 'List', 'items': [{'kind': 'Deployment', 'metadata': {
                     'name': 'artifact-retention', 'namespace': 'foreign'}}]}, empty, '0', '0', False)]
        for deployment, consumers, deploy_exit, consumer_exit, valid in cases:
            result = run(command, DEPLOYMENTS=deployment, CONSUMERS=consumers,
                         DEPLOY_EXIT=deploy_exit, CONSUMER_EXIT=consumer_exit)
            self.assertEqual(result.returncode == 0, valid, result.stderr)

    def test_absence_cannot_become_existing_between_barrier_snapshots(self):
        command = functions('quiesce_local_supply_chain_workload') + PREFIX + '''
kubectl() {
 if [[ "$*" == *get\\ deployments* ]]; then
  if [[ -f "$STATE_FILE" ]]; then printf '%s\\n' "$APPEARED";
  else touch "$STATE_FILE"; printf '{"kind":"List","items":[]}\\n'; fi
 elif [[ "$*" == *get\\ replicasets,pods* ]]; then printf '{"kind":"List","items":[]}\\n';
 else return 99; fi
}
mode=apply
quiesce_local_supply_chain_workload artifact-retention
'''
        appeared = {'kind': 'List', 'items': [{'kind': 'Deployment', 'metadata': {
            'name': 'artifact-retention', 'namespace': 'kodex-system'}}]}
        with tempfile.TemporaryDirectory() as directory:
            result = run(command, STATE_FILE=str(Path(directory) / 'first'), APPEARED=appeared)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('final Deployment inventory changed', result.stderr)

    def test_existing_retention_uses_original_exact_stop_join_proof(self):
        original = {'kind': 'Deployment', 'metadata': {'namespace': 'kodex-system', 'name': 'artifact-retention',
            'uid': '12345678-1234-1234-1234-123456789abc', 'resourceVersion': '42', 'labels': {
                'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload',
                'kodex.dev/security-profile': 'trusted-cluster'}},
            'spec': {'replicas': 1, 'selector': {'matchLabels': {'app': 'artifact-retention'}},
                'template': {'metadata': {'labels': {'app': 'artifact-retention'}},
                    'spec': {'containers': [{'name': 'artifact-retention', 'image': 'exact'}]}}},
            'status': {'replicas': 1, 'availableReplicas': 1}}
        drained = copy.deepcopy(original)
        drained['spec']['replicas'] = 0
        drained['status'] = {'replicas': 0, 'availableReplicas': 0}
        command = functions('readback_local_quiesced_pods', 'quiesce_local_supply_chain_workload') + PREFIX + '''
exec 3>&1
seq() { printf '180\\n'; }
kubectl() {
 case "$*" in
  *get\\ deployments*) printf '{"kind":"List","items":[%s]}\\n' "$ORIGINAL" ;;
  *scale*) printf '%s\\n' "$*" >&3; touch "$STATE_FILE" ;;
  *get\\ pods*) printf '%s\\n' "$PODS" ;;
  *get\\ deployment*) if [[ -f "$STATE_FILE" ]]; then printf '%s\\n' "$DRAINED";
                       else printf '%s\\n' "$ORIGINAL"; fi ;;
  *) return 99 ;;
 esac
}
mode=apply
quiesce_local_supply_chain_workload artifact-retention
'''
        for pods, valid in [([], True), ([{'kind': 'Pod'}], False)]:
            with tempfile.TemporaryDirectory() as directory:
                result = run(command, ORIGINAL=original, DRAINED=drained, PODS={'items': pods},
                             STATE_FILE=str(Path(directory) / 'scaled'))
            self.assertEqual(result.returncode == 0, valid, result.stderr)
            if valid: self.assertIn('--current-replicas=1 --resource-version=42', result.stdout)

    def test_selected_core_does_not_redeploy_other_consumers_and_requires_migration(self):
        block = SOURCE[SOURCE.index('  if [[ "$stage" == core ]]; then'):]
        block = block[:block.index("  printf 'Kodex trusted-cluster stage completed:")]
        command = PREFIX + '''
exec 3>&1
stage=core; selected_workload=artifact-retention; mode=$TEST_MODE
require_local_artifact_retention_migration() { printf 'migration\\n'; [[ "$MIGRATED" == yes ]]; }
apply_render() { printf '%s\\n' "$1"; }
kubectl() { printf '%s\\n' "$*" >&3; }
readback_local_artifact_retention() { printf 'retention-readback\\n'; }
''' + block
        for mode in ('apply', 'readback'):
            for migrated in ('yes', 'no'):
                result = run(command, TEST_MODE=mode, MIGRATED=migrated)
                self.assertEqual(result.returncode == 0, migrated == 'yes', result.stderr)
                calls = result.stdout.strip().splitlines()
                if migrated == 'no': self.assertEqual(calls, ['migration']); continue
                expected = ['migration']
                if mode == 'apply': expected += ['artifact-retention-foundation',
                    'artifact-retention-runtime-configuration', 'artifact-retention-deployment']
                expected += ['-n kodex-system rollout status deployment/artifact-retention --timeout=5m', 'retention-readback']
                self.assertEqual(calls, expected)

    def test_migration_gate_requires_completed_exact_render_job(self):
        seed = {'kind': 'Job', 'metadata': {'name': 'control-plane-migrate', 'namespace': 'kodex-system'}}
        expression = 'select(.kind == "Job" and .metadata.namespace == "kodex-system" and .metadata.name == "control-plane-migrate")'
        command = functions('filter_render', 'require_local_artifact_retention_migration') + PREFIX + '''
render=$RENDER_FILE; temporary_directory=$PRIVATE_DIR
kubectl() { [[ "$API_EXIT" == 0 ]] || return 1; printf '%s\\n' "$JOB"; }
require_local_artifact_retention_migration
'''
        with tempfile.TemporaryDirectory() as directory:
            render = Path(directory) / 'render.yaml'
            render.write_text(json.dumps(seed))
            raw = subprocess.check_output(['yq', expression, str(render)], timeout=5)
            digest = hashlib.sha256(raw).hexdigest()
            job = {'kind': 'Job', 'metadata': {'name': 'control-plane-migrate-' + digest[:12],
                'namespace': 'kodex-system', 'labels': {'app.kubernetes.io/part-of': 'kodex',
                    'kodex.dev/local-profile': 'hot-reload', 'kodex.dev/security-profile': 'trusted-cluster'},
                'annotations': {'kodex.dev/job-input-sha256': digest}},
                'status': {'succeeded': 1, 'conditions': [{'type': 'Complete', 'status': 'True'}]}}
            cases = [(job, '0', True), (job, '1', False), ({}, '0', False)]
            for field, value in [('namespace', 'foreign'), ('name', 'control-plane-migrate-old')]:
                bad = copy.deepcopy(job); bad['metadata'][field] = value; cases.append((bad, '0', False))
            bad = copy.deepcopy(job); bad['metadata']['annotations']['kodex.dev/job-input-sha256'] = 'e' * 64; cases.append((bad, '0', False))
            bad = copy.deepcopy(job); bad['status']['succeeded'] = 0; cases.append((bad, '0', False))
            bad = copy.deepcopy(job); bad['status']['active'] = 1; cases.append((bad, '0', False))
            bad = copy.deepcopy(job); bad['status']['conditions'].append({'type': 'Failed', 'status': 'True'}); cases.append((bad, '0', False))
            for observed, api_exit, valid in cases:
                result = run(command, RENDER_FILE=str(render), PRIVATE_DIR=directory, JOB=observed, API_EXIT=api_exit)
                self.assertEqual(result.returncode == 0, valid, result.stderr)
            render.write_text(json.dumps(seed) + '\n---\n' + json.dumps(seed))
            result = run(command, RENDER_FILE=str(render), PRIVATE_DIR=directory, JOB=job, API_EXIT='0')
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('render is ambiguous', result.stderr)

    def test_readback_rejects_zero_replica_stale_source_and_config_drift(self):
        template = {'metadata': {'annotations': {'kodex.dev/source-revision': 'a' * 40,
            'kodex.dev/source-content-sha256': 'b' * 64}}, 'spec': {
            'containers': [{'name': 'artifact-retention', 'image': 'exact'}]}}
        deployment = {'kind': 'Deployment', 'metadata': {'name': 'artifact-retention',
            'namespace': 'kodex-system', 'generation': 3, 'labels': {
            'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload',
            'kodex.dev/security-profile': 'trusted-cluster'}}, 'spec': {'replicas': 1, 'template': template},
            'status': {'observedGeneration': 3, 'replicas': 1, 'updatedReplicas': 1,
                'readyReplicas': 1, 'availableReplicas': 1}}
        cm = {'kind': 'ConfigMap', 'metadata': {'name': 'artifact-retention-runtime',
            'namespace': 'kodex-system', 'labels': {
                'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload',
                'kodex.dev/security-profile': 'trusted-cluster'}}, 'data': {'POLL_INTERVAL': '5s'}}
        command = functions('readback_local_supply_chain_deployment_inputs', 'readback_local_artifact_retention') + PREFIX + '''
render=$RENDER_FILE
kubectl() {
 case "$*" in
  *get\\ configmap*) printf '%s\\n' "$CM" ;;
  *get\\ deployment*) printf '%s\\n' "$DEPLOYMENT" ;;
  *) return 99 ;;
 esac
}
readback_local_artifact_retention
'''
        cases = [(deployment, cm, True)]
        for field, value in [('observedGeneration', 2), ('replicas', 0), ('readyReplicas', 0), ('updatedReplicas', 0)]:
            bad = copy.deepcopy(deployment); bad['status'][field] = value; cases.append((bad, cm, False))
        bad = copy.deepcopy(deployment); bad['spec']['replicas'] = 0; bad['status'].update({key: 0 for key in bad['status']}); cases.append((bad, cm, False))
        bad = copy.deepcopy(deployment); bad['spec']['template']['metadata']['annotations']['kodex.dev/source-content-sha256'] = 'c' * 64; cases.append((bad, cm, False))
        bad = copy.deepcopy(cm); bad['data']['POLL_INTERVAL'] = '1h'; cases.append((deployment, bad, False))
        bad = copy.deepcopy(cm); bad['metadata']['namespace'] = 'foreign'; cases.append((deployment, bad, False))
        bad = copy.deepcopy(cm); bad['metadata']['labels']['kodex.dev/security-profile'] = 'foreign'; cases.append((deployment, bad, False))
        with tempfile.TemporaryDirectory() as directory:
            render = Path(directory) / 'render.yaml'
            render.write_text(json.dumps(cm) + '\n---\n' + json.dumps(deployment))
            for observed, config, valid in cases:
                result = run(command, RENDER_FILE=str(render), DEPLOYMENT=observed, CM=config)
                self.assertEqual(result.returncode == 0, valid, result.stderr)

    def test_foundation_config_and_deployment_selectors_have_exact_boundaries(self):
        fixtures = []
        foundation = [('ServiceAccount', 'artifact-retention'), ('Service', 'artifact-retention'),
                      ('PodDisruptionBudget', 'artifact-retention'),
                      ('NetworkPolicy', 'artifact-retention-deny-all'),
                      ('NetworkPolicy', 'artifact-retention-exact-runtime-paths')]
        accepted = foundation + [('ConfigMap', 'artifact-retention-runtime'), ('Deployment', 'artifact-retention')]
        for kind, name in accepted:
            fixtures.append({'kind': kind, 'metadata': {'name': name, 'namespace': 'kodex-system'}})
            for namespace in ('foreign', '', None):
                fixtures.append({'kind': kind, 'metadata': {'name': name, 'namespace': namespace}})
            fixtures.append({'kind': kind, 'metadata': {'name': name + '-shadow', 'namespace': 'kodex-system'}})
        fixtures += [{'kind': 'Secret', 'metadata': {'name': 'artifact-retention', 'namespace': 'kodex-system'}},
                     {'kind': 'Deployment', 'metadata': {'name': 'control-plane', 'namespace': 'kodex-system'}}]
        for phase, expected in [('artifact-retention-foundation', foundation),
                ('artifact-retention-runtime-configuration', [('ConfigMap', 'artifact-retention-runtime')]),
                ('artifact-retention-deployment', [('Deployment', 'artifact-retention')])]:
            expression = re.search(r"apply_render " + phase + r"\s+'([^']*)'", SOURCE).group(1)
            result = subprocess.run(['jq', '-c', expression], input='\n'.join(map(json.dumps, fixtures)),
                                    capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            observed = [(item['kind'], item['metadata']['name']) for item in map(json.loads, result.stdout.splitlines())]
            self.assertEqual(observed, expected)


if __name__ == '__main__':
    unittest.main()
