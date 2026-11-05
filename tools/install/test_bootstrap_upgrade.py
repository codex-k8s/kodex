"""Forward upgrade не повторяет historical effects и не подменяет наблюдение Job/Pod/schema флагом из payload."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import yaml
import bootstrap_completion as completion
import bootstrap_upgrade as upgrade
from deployment_profile import Rejected
from test_bootstrap_completion import State, uuid, write


def fixture():
    heads = {'control-plane': 20261030000100, 'internal-rpc-authority': 20260912000200, 'email-bridge': 20260905000200}
    resources = [{'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': 'kodex-installation-profile', 'namespace': 'kodex-system',
                 'labels': {'app.kubernetes.io/part-of': 'kodex'}}, 'data': {'source.sha': 'a' * 40, 'schema-heads.json': json.dumps(heads)}}]
    for name in completion.JOBS:
        resources.append({'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {'name': name, 'namespace': 'kodex-system',
                          'labels': {'app.kubernetes.io/part-of': 'kodex'}}, 'spec': {'backoffLimit': 2, 'activeDeadlineSeconds': 300,
                          'template': {'spec': {'serviceAccountName': 'control-plane-migrator', 'automountServiceAccountToken': False,
                          'restartPolicy': 'Never', 'containers': [{'name': 'migrate', 'image': 'registry.example.invalid/kodex/cli@sha256:' + 'a' * 64,
                          'command': ['/usr/local/bin/control-plane-cli'], 'args': ['up'], 'env': [{'name': 'CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE',
                          'value': '/var/run/secrets/kodex/control-plane/postgres-migration/dsn'}]}]}}}})
    target = copy.deepcopy(resources)
    target[0]['data'].update({'source.sha': 'b' * 40, 'schema-heads.json': json.dumps({**heads, 'control-plane': 20261105000100})})
    for resource in target[1:]: resource['spec']['template']['spec']['containers'][0]['image'] = 'registry.example.invalid/kodex/cli@sha256:' + 'b' * 64
    before, after = yaml.safe_dump_all(resources).encode(), yaml.safe_dump_all(target).encode()
    profile = {'schema': upgrade.SCHEMA, 'managed_by': 'git', 'previous_render_sha256': upgrade.sha(before), 'target_render_sha256': upgrade.sha(after),
               'previous_source_sha': 'a' * 40, 'target_source_sha': 'b' * 40, 'from_schema_heads': heads,
               'to_schema_heads': {**heads, 'control-plane': 20261105000100}, 'schema_profile': 'backup'}
    return resources, target, before, after, profile


def controller_defaults(state):
    """Моделирует настоящие server-generated Job selectors без ослабления production verifier."""
    for value in state.current.values():
        if value['kind'] != 'Job': continue
        uid = value['metadata']['uid']
        value['spec']['selector'] = {'matchLabels': {'batch.kubernetes.io/controller-uid': uid}}
        value['spec']['template']['metadata'] = {'labels': {'batch.kubernetes.io/controller-uid': uid,
            'batch.kubernetes.io/job-name': value['metadata']['name']}}


class UpgradeState(State):
    def __init__(self, resources, previous):
        super().__init__(resources)
        for name in upgrade.PRESERVED: self.current[name] = copy.deepcopy(previous.current[name])
        job = self.current[upgrade.MIGRATION]; job['metadata']['uid'] = uuid(99)
        job['status']['conditions'] = [{'type': 'Complete', 'status': 'True'}]
        controller_defaults(self)
        self.pod = {'metadata': {'name': 'synthetic-migrate', 'namespace': 'kodex-system', 'uid': uuid(100),
                    'ownerReferences': [{'kind': 'Job', 'uid': uuid(99), 'controller': True}]},
                    'spec': {**copy.deepcopy(job['spec']['template']['spec']), 'nodeName': 'synthetic-node'},
                    'status': {'phase': 'Succeeded', 'containerStatuses': [{'name': 'migrate',
                               'imageID': 'registry.example.invalid/kodex/cli@sha256:' + 'b' * 64, 'state': {'terminated': {'exitCode': 0}}}]}}

    def call(self, args):
        if 'pods' in args:
            self.calls.append(args)
            return json.dumps({'items': [self.pod]}).encode()
        return super().call(args)


class UpgradeTests(unittest.TestCase):
    def prepared(self):
        old, _, before, after, profile = fixture()
        target = upgrade.prepare(before, after, profile)
        previous = State(old)
        controller_defaults(previous)
        with patch.object(completion, 'verify_schemas'):
            receipt = completion.capture(previous, old)
        return old, target, profile, receipt, previous

    def capture(self, state, target, old, receipt):
        with patch.object(upgrade, 'verify_schemas') as heads, patch.object(upgrade, 'schema_proof', return_value='f' * 64) as proof:
            result = upgrade.capture(state, target, old, receipt, 'e' * 64)
        heads.assert_called_once_with(state, target)
        proof.assert_called_once_with(state, 'backup')
        return result

    def test_prepare_keeps_four_effects_and_changes_no_other_resources(self):
        old, target, before, after, profile = fixture()
        original = copy.deepcopy((old, target, profile))
        prepared = upgrade.prepare(before, after, profile)
        self.assertEqual((old, target, profile), original)
        old_jobs, new_jobs = completion.desired_jobs(old), completion.desired_jobs(prepared)
        for name in upgrade.PRESERVED: self.assertEqual(new_jobs[name], old_jobs[name])
        self.assertEqual(new_jobs[upgrade.MIGRATION]['spec']['backoffLimit'], 0)
        self.assertEqual(prepared[0], target[0])
        self.assertEqual(upgrade.render_profile(prepared)[0], profile)
        self.assertEqual(upgrade.prepare(before, after, profile), prepared)

    def test_drift_changed_effect_and_nonforward_schema_are_rejected(self):
        _, target, before, after, profile = fixture()
        for patch_value in ({'target_render_sha256': '0' * 64}, {'unknown': True}, {'schema_profile': 'automatic'},
                            {'to_schema_heads': profile['from_schema_heads']}):
            with self.assertRaises(Rejected): upgrade.prepare(before, after, {**profile, **patch_value})
        for name in (upgrade.PRESERVED[0], upgrade.MIGRATION):
            changed = copy.deepcopy(target)
            completion.desired_jobs(changed)[name]['spec']['template']['spec']['containers'][0]['args'] = ['check']
            raw = yaml.safe_dump_all(changed).encode()
            with self.assertRaises(Rejected): upgrade.prepare(before, raw, {**profile, 'target_render_sha256': upgrade.sha(raw)})

    def test_capture_joins_real_new_execution_and_preserves_ttl_history(self):
        old, target, profile, receipt, previous = self.prepared()
        state = UpgradeState(target, previous)
        for name in upgrade.PRESERVED: state.current.pop(name)
        result = self.capture(state, target, old, receipt)
        self.assertEqual(result['origin'], upgrade.ORIGIN)
        rows = {r['name']: r for r in result['jobs']}
        for row in receipt['jobs']:
            if row['name'] in upgrade.PRESERVED: self.assertEqual(rows[row['name']], row)
        self.assertEqual(rows[upgrade.MIGRATION]['job_uid'], uuid(99))
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'receipt.json'; digest = write(path, result)
            with self.assertRaises(Rejected): completion.load(path, digest)
            loaded = completion.load(path, digest, True)
        completion.verify_jobs(state, target, loaded)
        self.assertTrue(all(c[0] == 'get' or 'get' in c for c in state.calls))
        changed = copy.deepcopy(target)
        changed[-1]['data']['profile.json'] = '{}'
        with self.assertRaises(Rejected): completion.verify_jobs(state, changed, loaded)

    def test_successful_job_alone_never_substitutes_execution_or_schema(self):
        old, target, _, receipt, previous = self.prepared()
        for scenario in ('foreign-pod', 'failed-exit', 'wrong-image', 'injected-env', 'old-job-uid', 'not-terminal', 'previous-failure', 'schema'):
            state = UpgradeState(target, previous)
            if scenario == 'foreign-pod': state.pod['metadata']['ownerReferences'][0]['uid'] = uuid(101)
            elif scenario == 'failed-exit': state.pod['status']['containerStatuses'][0]['state']['terminated']['exitCode'] = 7
            elif scenario == 'wrong-image': state.pod['status']['containerStatuses'][0]['imageID'] = 'sha256:' + 'c' * 64
            elif scenario == 'injected-env': state.pod['spec']['containers'][0]['env'].append({'name': 'UNTRUSTED', 'value': 'synthetic'})
            elif scenario == 'old-job-uid': state.current[upgrade.MIGRATION]['metadata']['uid'] = previous.current[upgrade.MIGRATION]['metadata']['uid']
            elif scenario == 'not-terminal': state.current[upgrade.MIGRATION]['status']['conditions'] = []
            elif scenario == 'previous-failure': state.current[upgrade.MIGRATION]['status']['failed'] = 1
            with patch.object(upgrade, 'verify_schemas'), patch.object(upgrade, 'schema_proof', side_effect=Rejected('SYNTHETIC_SCHEMA_FAILURE') if scenario == 'schema' else None, return_value='f' * 64):
                with self.assertRaises(Rejected): upgrade.capture(state, target, old, receipt, 'e' * 64)


if __name__ == '__main__':
    unittest.main()
