"""Герметичные проверки maintenance barrier без доступа к кластеру."""
import copy
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import unittest
import uuid

SCRIPT = Path(__file__).with_name("deploy-local.sh")
SOURCE = SCRIPT.read_text()
PREFIX = '\nfail() { printf "%s\\n" "$1" >&2; exit 1; }; namespace=kodex-system; render=synthetic\n'


def functions(*names):
    return "\n".join(re.search(r"(?ms)^" + name + r"\(\) \{.*?^\}", SOURCE).group(0) for name in names)


def run(command, **values):
    return subprocess.run(["bash", "-euo", "pipefail", "-c", command],
        env=dict(os.environ, **{k: json.dumps(v) if not isinstance(v, str) else v for k, v in values.items()}),
        capture_output=True, text=True, timeout=5)


class CutoverTest(unittest.TestCase):
    def test_historical_terminal_pods_require_complete_status_and_exact_lineage(self):
        deployment_uid = '12345678-1234-1234-1234-123456789abc'
        replica_uid = 'abcdefab-1234-1234-1234-123456789abc'
        pod = {'metadata': {'namespace': 'kodex-system', 'name': 'control-plane-historical',
            'uid': '87654321-1234-1234-1234-123456789abc', 'labels': {'app': 'control-plane'},
            'ownerReferences': [{'apiVersion': 'apps/v1', 'kind': 'ReplicaSet', 'name': 'control-plane-old',
                                 'uid': replica_uid, 'controller': True}]},
            'spec': {'containers': [{'name': 'control-plane'}]},
            'status': {'phase': 'Succeeded', 'containerStatuses': [{'name': 'control-plane', 'ready': False,
                'state': {'terminated': {'reason': 'Completed', 'exitCode': 0,
                                        'finishedAt': '2026-10-05T08:00:00Z'}}}]}}
        sets = {'items': [{'metadata': {'namespace': 'kodex-system', 'name': 'control-plane-old',
            'uid': replica_uid, 'ownerReferences': [{'apiVersion': 'apps/v1', 'kind': 'Deployment',
                'name': 'control-plane', 'uid': deployment_uid, 'controller': True}]}}]}
        command = functions('readback_local_quiesced_pods') + PREFIX + '''
kubectl() { printf '%s\\n' "$SETS"; }
readback_local_quiesced_pods "$DEPLOYMENT_UID" control-plane app=control-plane "$PODS"
'''
        cases = [('empty', {'items': []}, sets, True), ('completed', {'items': [pod]}, sets, True)]
        failed = copy.deepcopy(pod)
        failed['status']['phase'] = 'Failed'
        failed['status']['containerStatuses'][0]['started'] = False
        failed['status']['containerStatuses'][0]['state']['terminated'].update(reason='Error', exitCode=1)
        cases.append(('failed-terminated', {'items': [failed]}, sets, True))
        deleting = copy.deepcopy(pod)
        deleting['metadata']['deletionTimestamp'] = '2026-10-05T08:01:00Z'
        cases.append(('deleting-terminal', {'items': [deleting]}, sets, True))
        complete = copy.deepcopy(pod)
        for prefix in ('init', 'ephemeral'):
            complete['spec'][prefix + 'Containers'] = [{'name': prefix + '-helper'}]
            status = copy.deepcopy(pod['status']['containerStatuses'][0])
            status['name'] = prefix + '-helper'
            complete['status'][prefix + 'ContainerStatuses'] = [status]
        cases.append(('all-container-kinds-complete', {'items': [complete]}, sets, True))
        for phase in ('Pending', 'Running', 'Unknown', 'Other'):
            bad = copy.deepcopy(pod)
            bad['status']['phase'] = phase
            cases.append((phase, {'items': [bad]}, sets, False))
        for state in ({'running': {}}, {'waiting': {}},
                      {'terminated': pod['status']['containerStatuses'][0]['state']['terminated'], 'running': {}}):
            bad = copy.deepcopy(pod)
            bad['status']['containerStatuses'][0]['state'] = state
            cases.append(('fake-terminal-live-state', {'items': [bad]}, sets, False))
        for key, value in (('ready', True), ('started', True), ('started', 'false'), ('ready', None)):
            bad = copy.deepcopy(pod)
            bad['status']['containerStatuses'][0][key] = value
            cases.append(('invalid-status-' + key, {'items': [bad]}, sets, False))
        for key in ('containerStatuses', 'initContainerStatuses', 'ephemeralContainerStatuses'):
            bad = copy.deepcopy(complete)
            bad['status'].pop(key)
            cases.append(('missing-' + key, {'items': [bad]}, sets, False))
        duplicate = copy.deepcopy(pod)
        duplicate['status']['containerStatuses'].append(copy.deepcopy(duplicate['status']['containerStatuses'][0]))
        cases.append(('duplicate-status', {'items': [duplicate]}, sets, False))
        for reason in ('ContainerStatusUnknown', 'Unknown', 'NodeLost'):
            bad = copy.deepcopy(pod)
            bad['status']['containerStatuses'][0]['state']['terminated']['reason'] = reason
            cases.append((reason, {'items': [bad]}, sets, False))
        for mutate in ('pod-selector', 'pod-namespace', 'pod-owner-uid', 'replica-owner-uid', 'replica-namespace',
                       'replica-name', 'replica-controller', 'finished-at', 'pod-node-lost'):
            bad = copy.deepcopy(pod)
            bad_sets = copy.deepcopy(sets)
            if mutate == 'pod-selector': bad['metadata']['labels']['app'] = 'foreign'
            elif mutate == 'pod-namespace': bad['metadata']['namespace'] = 'foreign'
            elif mutate == 'pod-owner-uid': bad['metadata']['ownerReferences'][0]['uid'] = deployment_uid
            elif mutate == 'replica-owner-uid': bad_sets['items'][0]['metadata']['ownerReferences'][0]['uid'] = replica_uid
            elif mutate == 'replica-namespace': bad_sets['items'][0]['metadata']['namespace'] = 'foreign'
            elif mutate == 'replica-name': bad_sets['items'][0]['metadata']['name'] = 'foreign'
            elif mutate == 'replica-controller': bad_sets['items'][0]['metadata']['ownerReferences'][0]['controller'] = False
            elif mutate == 'finished-at': bad['status']['containerStatuses'][0]['state']['terminated'].pop('finishedAt')
            else: bad['status']['reason'] = 'NodeLost'
            cases.append((mutate, {'items': [bad]}, bad_sets, False))
        for name, pods, replica_sets, success in cases:
            with self.subTest(case=name):
                result = run(command, DEPLOYMENT_UID=deployment_uid, PODS=pods, SETS=replica_sets)
                self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_quiesce_public_stage_has_no_resume_or_apply(self):
        block = SOURCE[SOURCE.index('if [[ "$stage" == supply-chain-quiesce ]]; then'):]
        block = block[:block.index('if [[ "$mode" == apply && "$stage" == data ]]; then')]
        trusted = SOURCE.index('if [[ "$security_profile" == trusted-cluster ]]; then\n  python3')
        guard = SOURCE.index('local_hot_reload.verify(resources, source, annotations')
        quiesce = SOURCE.index('  if [[ "$stage" == supply-chain-quiesce ]]; then')
        self.assertLess(trusted, guard)
        self.assertLess(guard, quiesce)
        self.assertIn('image_admission_policy_owner_coherent=false', block)
        self.assertEqual(block.count('require_idle_local_supply_chain_owner'), 2)
        self.assertIn('require_empty_local_image_admission_runs', block)
        self.assertIn('mode=readback quiesce_local_supply_chain_workload', block)
        self.assertIn('exit 0', block)
        self.assertNotIn('apply_render', block)
        for mode in ('apply', 'readback'):
            result = subprocess.run(['bash', str(SCRIPT), '--context', 'synthetic-local', '--mode', mode,
                '--security-profile', 'trusted-cluster', '--stage', 'supply-chain-quiesce',
                '--render', '/nonexistent-render'], capture_output=True, text=True, timeout=5)
            self.assertIn('local render is invalid', result.stderr)

    def test_quiesce_identity_cas_and_readback(self):
        original = {'metadata': {'namespace': 'kodex-system', 'name': 'control-plane',
            'uid': '12345678-1234-1234-1234-123456789abc', 'resourceVersion': '42', 'labels': {
                'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload',
                'kodex.dev/security-profile': 'trusted-cluster'}},
            'spec': {'replicas': 1, 'selector': {'matchLabels': {'app': 'control-plane'}},
                'template': {'metadata': {'labels': {'app': 'control-plane'}},
                    'spec': {'containers': [{'name': 'control-plane', 'image': 'exact'}]}}},
            'status': {'replicas': 1, 'availableReplicas': 1}}
        drained = copy.deepcopy(original)
        drained['spec']['replicas'] = 0
        drained['status'] = {'replicas': 0, 'availableReplicas': 0}
        command = functions('readback_local_quiesced_pods', 'quiesce_local_supply_chain_workload') + PREFIX + '''
exec 3>&1
seq() { printf '180\\n'; }
kubectl() {
 case "$*" in
  *scale*) printf '%s\\n' "$*" >&3; [[ "$SCALE_EXIT" == 0 ]] || return 1; touch "$STATE_FILE" ;;
  *get\\ pods*) printf '%s\\n' "$PODS" ;;
  *get\\ deployment*) if [[ -f "$STATE_FILE" ]]; then printf '%s\\n' "$DRAINED";
                       else printf '%s\\n' "$ORIGINAL"; fi ;;
  *) return 99 ;;
 esac
}
mode=$TEST_MODE
quiesce_local_supply_chain_workload control-plane
'''
        cases = [('apply', original, drained, [], '0', True), ('readback', drained, drained, [], '0', True),
                 ('readback', original, drained, [], '0', False), ('apply', original, drained, [], '1', False),
                 ('apply', original, drained, [{'kind': 'Pod'}], '0', False)]
        for field, value in (('namespace', 'foreign'), ('name', 'foreign'), ('uid', 'bad')):
            bad = copy.deepcopy(original)
            bad['metadata'][field] = value
            cases.append(('apply', bad, drained, [], '0', False))
        for label in original['metadata']['labels']:
            bad = copy.deepcopy(original)
            bad['metadata']['labels'][label] = 'foreign'
            cases.append(('apply', bad, drained, [], '0', False))
        for field in ('uid', 'image'):
            bad = copy.deepcopy(drained)
            if field == 'uid': bad['metadata']['uid'] = 'abcdefab-1234-1234-1234-123456789abc'
            else: bad['spec']['template']['spec']['containers'][0]['image'] = 'foreign'
            cases.append(('apply', original, bad, [], '0', False))
        for mode, before, after, pods, scale_exit, success in cases:
            with self.subTest(mode=mode, success=success):
                with tempfile.TemporaryDirectory() as directory:
                    result = run(command, TEST_MODE=mode, ORIGINAL=before, DRAINED=after,
                        PODS={'items': pods}, SCALE_EXIT=scale_exit, STATE_FILE=str(Path(directory) / 'scaled'))
                self.assertEqual(result.returncode == 0, success, result.stderr)
                if mode == 'readback': self.assertNotIn('scale', result.stdout)
                if success and mode == 'apply': self.assertIn('--current-replicas=1 --resource-version=42', result.stdout)

    def test_crd_exact_spec_and_only_approved_defaults(self):
        expected = {'group': 'supplychain.kodex.dev', 'scope': 'Namespaced',
                    'versions': [{'schema': {'requiredTools': {'enum': ['validator,tr']}}}]}
        defaulted = dict(expected, conversion={'strategy': 'None'}, preserveUnknownFields=False)
        command = functions('canonical_image_admission_crd_spec', 'readback_local_image_admission_crd') + PREFIX + '''
yq() { printf '%s\\n' "$EXPECTED"; }
kubectl() { printf '%s\\n' "$ACTUAL"; }
readback_local_image_admission_crd
'''
        cases = [(defaulted, True, True), (expected, True, True), (defaulted, False, False)]
        for key, value in (('conversion', {'strategy': 'Webhook'}), ('preserveUnknownFields', True),
                           ('scope', 'Cluster'), ('unexpected', True), ('versions', [])):
            cases.append((dict(defaulted, **{key: value}), True, False))
        for spec, established, success in cases:
            result = run(command, EXPECTED=expected, ACTUAL={'spec': spec, 'status': {'conditions': [
                {'type': 'Established', 'status': 'True' if established else 'False'}]}})
            self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_exact_configuration_and_network_readback(self):
        for name, expected in (
            ('readback_local_claim_evidence_network', [
                {'name': 'kodex-image-admission-claim-evidence-exact-path', 'spec': {'policyTypes': ['Ingress', 'Egress'],
                    'ingress': [], 'egress': [{'to': [{'podSelector': {'matchLabels': {'scope': 'evidence'}}}],
                        'ports': [{'protocol': 'TCP', 'port': 5007}]}]}},
                {'name': 'kodex-image-registry-evidence', 'spec': {'policyTypes': ['Ingress'],
                    'ingress': [{'from': [{'podSelector': {'matchLabels': {'phase': 'claim'}}}]}]}}]),
            ('readback_local_supply_chain_configuration', [
                {'name': 'control-plane-runtime', 'data': {'policy': '89'}, 'binaryData': None, 'immutable': None},
                {'name': 'kodex-platform-endpoints', 'data': {'version': 'exact'}, 'binaryData': None, 'immutable': None}])):
            command = functions(name) + PREFIX + '''
yq() { jq -c '.[]' <<<"$EXPECTED"; }
kubectl() { printf '{"items":%s}\\n' "$ACTUAL"; }
''' + name
            actual = [{'metadata': {'name': item['name']}, **{k: v for k, v in item.items() if k != 'name'}}
                      for item in expected]
            bad = copy.deepcopy(actual)
            if name.endswith('network'): bad[0]['spec']['egress'][0]['ports'][0]['port'] = 443
            else: bad[0]['data']['policy'] = '88'
            shadow = copy.deepcopy(actual)
            shadow[0]['metadata']['name'] += '-shadow'
            for observed, success in ((actual, True), (actual[:1], False), (bad, False), (shadow, False)):
                result = run(command, EXPECTED=expected, ACTUAL=observed)
                self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_activation_order_and_closed_selectors(self):
        stage = SOURCE[SOURCE.index('  if [[ "$stage" == supply-chain ]]'):]
        steps = ['pause_local_image_admission_controller', 'require_empty_local_image_admission_runs',
            'apply_job control-plane-migrate', 'apply_image_admission_crd',
            'apply_render image-admission-claim-evidence-network', 'readback_local_claim_evidence_network',
            'apply_render image-admission-controller-policies', 'readback_local_image_admission_policies',
            'apply_render image-admission-owner-configuration', 'readback_local_supply_chain_configuration',
            'apply_render image-admission-control-plane-owner', 'readback_local_control_plane_image_policy',
            'apply_render image-admission-control-api-reader',
            'readback_local_supply_chain_deployment_inputs control-api-gateway',
            'apply_render image-supply-chain-controllers']
        positions = [stage.index(step) for step in steps]
        self.assertEqual(positions, sorted(positions))
        for phase, kind, names in (
            ('image-admission-claim-evidence-network', 'NetworkPolicy',
             ['kodex-image-registry-evidence', 'kodex-image-admission-claim-evidence-exact-path']),
            ('image-admission-owner-configuration', 'ConfigMap', ['control-plane-runtime', 'kodex-platform-endpoints']),
            ('image-admission-control-api-reader', 'Deployment', ['control-api-gateway'])):
            expression = re.search(r"apply_render " + phase + r"\s+'([^']*)'", stage).group(1)
            accepted = [{'kind': kind, 'metadata': {'namespace': 'kodex-system', 'name': name}} for name in names]
            rejected = [{'kind': other, 'metadata': {'namespace': namespace, 'name': name}}
                for other in (kind, 'Secret') for namespace in ('kodex-system', 'foreign')
                for name in names + [names[0] + '-shadow']
                if other != kind or namespace != 'kodex-system' or name not in names]
            result = subprocess.run(['jq', '-c', expression], capture_output=True, text=True, timeout=5,
                input='\n'.join(json.dumps(item) for item in accepted + rejected))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual([json.loads(line) for line in result.stdout.splitlines()], accepted)
        self.assertIn('for workload in control-plane control-api-gateway', stage)

    def test_cel_warning_is_a_closed_failure(self):
        command = functions('canonical_runtime_admission_specs', 'readback_local_image_admission_policies') + PREFIX + '''
yq() { printf '{"failurePolicy":"Fail"}\\n'; }
kubectl() { printf '%s\\n' "$ACTUAL"; }
sleep() { :; }
readback_local_image_admission_policies
'''
        actual = {'metadata': {'generation': 3}, 'status': {'observedGeneration': 3,
                  'typeChecking': {'expressionWarnings': []}}, 'spec': {'failurePolicy': 'Fail'}}
        cases = [(actual, True)]
        stale = copy.deepcopy(actual)
        stale['status']['observedGeneration'] = 2
        cases.append((stale, False))
        warning = copy.deepcopy(actual)
        warning['status']['typeChecking']['expressionWarnings'] = [{'fieldRef': 'spec.validations[0]', 'warning': 'synthetic'}]
        cases.append((warning, False))
        for observed, success in cases:
            result = run(command, ACTUAL=observed)
            self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_quiesce_partial_failure_and_exit_never_resume(self):
        block = SOURCE[SOURCE.index('if [[ "$stage" == supply-chain-quiesce ]]; then'):]
        block = block[:block.index('if [[ "$mode" == apply && "$stage" == data ]]; then')]
        command = functions('cleanup_on_exit') + PREFIX + '''
exec 3>&1
phase() { printf '%s\\n' "$1"; [[ "$FAIL_PHASE" != "$1" ]]; }
require_idle_local_supply_chain_owner() { idle=$((idle + 1)); phase "idle-$idle"; }
quiesce_local_supply_chain_workload() { phase "$mode-$1"; }
require_empty_local_image_admission_runs() { phase empty; }
kubectl() { printf 'unexpected-resume\\n' >&3; }
rm() { :; }
mode=apply
stage=supply-chain-quiesce
idle=0
temporary_directory=/synthetic
image_admission_controller_restore_replicas=1
image_admission_policy_owner_coherent=true
trap cleanup_on_exit EXIT
''' + block
        phases = ['idle-1', 'empty', 'idle-2'] + [mode + '-' + name for mode in ('apply', 'readback')
            for name in ('control-api-gateway', 'image-admission-controller', 'role-image-builder',
                         'runtime-controller', 'control-plane')]
        for failed in phases + ['']:
            result = run(command, FAIL_PHASE=failed)
            self.assertEqual(result.returncode == 0, failed == '', result.stderr)
            self.assertNotIn('unexpected-resume', result.stdout)
            if failed: self.assertEqual(result.stdout.strip().splitlines()[-1], failed)


@unittest.skipUnless(os.environ.get('KODEX_TEST_SUPPLY_CHAIN_POSTGRES') == '1',
                     'Отдельный разрешённый disposable PostgreSQL: KODEX_TEST_SUPPLY_CHAIN_POSTGRES=1')
class OwnerIdlePostgresTest(unittest.TestCase):
    def test_exact_query_with_actual_postgresql_and_preserved_pins(self):
        # Только созданный контейнер и private fixture, без внешнего DSN/живых томов.
        image = 'docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'
        container = None
        with tempfile.TemporaryDirectory(prefix='supply-chain-idle-pg.') as directory:
            credential = Path(directory) / 'password'
            credential.write_text(uuid.uuid4().hex)
            credential.chmod(0o600)
            environment = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR', 'DOCKER_HOST') if key in os.environ}
            environment['DOCKER_CONFIG'] = directory

            def docker(*args, input=None, required=True):
                result = subprocess.run(['docker', *args], input=input, env=environment,
                    capture_output=True, text=True, timeout=45)
                if required: self.assertEqual(result.returncode, 0, 'Disposable PostgreSQL command failed')
                return result

            try:
                result = docker('run', '--rm', '-d', '--network', 'none', '--memory', '1g', '--cpus', '2',
                    '--name', 'kodex-supply-chain-idle-' + uuid.uuid4().hex,
                    '--label', 'kodex.dev/disposable-test=supply-chain-cutover',
                    '--mount', 'type=bind,src=' + directory + ',dst=/run/test-fixture,readonly',
                    '--tmpfs', '/var/lib/postgresql:rw,size=1073741824',
                    '-e', 'POSTGRES_PASSWORD_FILE=/run/test-fixture/password', image)
                container = result.stdout.strip()
                self.assertRegex(container, r'^[a-f0-9]{64}$')
                for attempt in range(30):
                    if docker('exec', container, 'pg_isready', '-U', 'postgres', required=False).returncode == 0: break
                    self.assertLess(attempt, 29, 'Disposable PostgreSQL startup exhausted')
                    time.sleep(1)

                def sql(source):
                    return docker('exec', '-i', container, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                                  '-U', 'postgres', '-d', 'postgres', input=source).stdout

                parts = re.split(r'^-- scenario: ([a-z-]+)\n',
                    SCRIPT.with_name('fixtures').joinpath('supply-chain-owner-idle.sql').read_text(), flags=re.MULTILINE)
                fixtures = dict(zip(parts[1::2], parts[2::2]))
                sql(fixtures['setup'])
                query = SCRIPT.with_name('supply-chain-owner-readback.sql').read_text()
                baseline = json.loads(sql(query))
                self.assertEqual(baseline['promotedArtifactCount'], 19)
                model = SCRIPT.parent.parent / 'release' / 'runner-policy-model.mjs'
                for name, fixture in fixtures.items():
                    if name in ('setup', 'reset'): continue
                    with self.subTest(scenario=name):
                        sql(fixtures['reset'] + fixture)
                        state = json.loads(sql(query))
                        self.assertEqual(state['promotedArtifactCount'], baseline['promotedArtifactCount'])
                        self.assertEqual(state['promotedPinsSHA256'], baseline['promotedPinsSHA256'])
                        check = subprocess.run(['node', '--input-type=module', '-e',
                            'import {requireIdle} from ' + json.dumps(model.as_uri()) + ';\n'
                            'try { requireIdle(JSON.parse(await new Promise(resolve => {let s=""; '
                            'process.stdin.on("data",c=>s+=c);process.stdin.on("end",()=>resolve(s));}))); } '
                            'catch { process.exitCode=1; }'], input=json.dumps(state),
                            env=environment, capture_output=True, text=True, timeout=5)
                        self.assertEqual(check.returncode == 0, name == 'unrequested')
                        if name == 'unrequested':
                            self.assertEqual(state['pendingPromotions'], 0)
                            self.assertEqual(state['unrequestedAcceptedArtifacts'], 1)
                        if name == 'foreign-project': self.assertEqual(state['pendingPromotions'], 2)
            finally:
                if container and re.fullmatch(r'[a-f0-9]{64}', container):
                    docker('stop', '--time', '5', container, required=False)


if __name__ == '__main__':
    unittest.main()
