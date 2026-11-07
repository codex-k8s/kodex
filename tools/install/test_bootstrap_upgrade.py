"""Forward upgrade не повторяет historical effects и не подменяет наблюдение Job/Pod/schema флагом из payload."""
import copy
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import yaml
import bootstrap_completion as completion
import bootstrap_upgrade as upgrade
import observability_profile
from deployment_profile import Rejected
from test_bootstrap_completion import State, uuid, write


def fixture(previous_version=20261030000100, target_version=20261105000100):
    heads = {'control-plane': previous_version, 'internal-rpc-authority': 20260912000200, 'email-bridge': 20260905000200}
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
    target[0]['data'].update({'source.sha': 'b' * 40, 'schema-heads.json': json.dumps({**heads, 'control-plane': target_version})})
    for resource in target[1:]: resource['spec']['template']['spec']['containers'][0]['image'] = 'registry.example.invalid/kodex/cli@sha256:' + 'b' * 64
    before, after = yaml.safe_dump_all(resources).encode(), yaml.safe_dump_all(target).encode()
    profile = {'schema': upgrade.SCHEMA, 'managed_by': 'git', 'previous_render_sha256': upgrade.sha(before), 'target_render_sha256': upgrade.sha(after),
               'previous_source_sha': 'a' * 40, 'target_source_sha': 'b' * 40, 'from_schema_heads': heads,
               'to_schema_heads': {**heads, 'control-plane': target_version}, 'schema_profile': 'backup'}
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
                               'imageID': job['spec']['template']['spec']['containers'][0]['image'], 'state': {'terminated': {'exitCode': 0}}}]}}

    def call(self, args):
        if 'pods' in args:
            self.calls.append(args)
            return json.dumps({'items': [self.pod]}).encode()
        return super().call(args)


class UpgradeTests(unittest.TestCase):
    def test_upgrade_apply_requires_real_composite_effects_and_fresh_schema_proof(self):
        old, target, _, receipt, previous = self.prepared()
        state = UpgradeState(target, previous)
        composite = self.capture(state, target, old, receipt)
        with patch.object(completion, 'verify_schemas'), patch.object(upgrade, 'schema_proof', return_value='f'*64) as proof:
            result = upgrade.verify_apply(state, target, composite)
            self.assertEqual(result['status'], 'FORWARD_UPGRADE_APPLY_VERIFIED')
            proof.assert_called_once_with(state, 'backup', 20261105000100)
        for origin in ('LIVE_CAPTURE', 'RECONCILED_VERIFIED_PREFLIGHT'):
            with self.assertRaises(Rejected):upgrade.verify_apply(state, target, {**composite, 'origin':origin})
        state.current[upgrade.MIGRATION]['status']['conditions'] = [{'type':'Failed','status':'True'}]
        with patch.object(completion, 'verify_schemas'), patch.object(upgrade, 'schema_proof') as proof, self.assertRaises(Rejected):
            upgrade.verify_apply(state, target, composite)
        proof.assert_not_called()

    def test_disabled_observability_profile_keeps_canonical_migration_contract(self):
        old, target, _, _, profile = fixture()
        old = observability_profile.transform(old, disabled=True)
        target = observability_profile.transform(target, disabled=True)
        before, after = yaml.safe_dump_all(old).encode(), yaml.safe_dump_all(target).encode()
        profile.update(previous_render_sha256=upgrade.sha(before), target_render_sha256=upgrade.sha(after))
        prepared = upgrade.prepare(before, after, profile)
        job = completion.desired_jobs(prepared)[upgrade.MIGRATION]
        upgrade.migration_contract(job)
        for value in ('false', '1'):
            changed = copy.deepcopy(job)
            changed['spec']['template']['spec']['containers'][0]['env'][-1]['value'] = value
            with self.assertRaises(Rejected): upgrade.migration_contract(changed)
        for entry in ({'name': 'FOREIGN', 'value': 'synthetic'}, {'name': 'OTEL_SDK_DISABLED', 'value': 'true'}):
            changed = copy.deepcopy(job)
            changed['spec']['template']['spec']['containers'][0]['env'].append(entry)
            with self.assertRaises(Rejected): upgrade.migration_contract(changed)

    def test_prepare_excludes_secret_seeds_without_replaying_or_copying_their_values(self):
        old, target, _, _, profile = fixture()
        seed = {'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': 'synthetic-projection',
                'namespace': 'kodex-system'}, 'type': 'Opaque', 'stringData': {'synthetic': 'synthetic-value'}}
        before = yaml.safe_dump_all(old + [seed]).encode()
        after = yaml.safe_dump_all(target + [{**seed, 'stringData': {'synthetic': 'synthetic-replacement'}}]).encode()
        profile.update(previous_render_sha256=upgrade.sha(before), target_render_sha256=upgrade.sha(after))
        prepared = upgrade.prepare(before, after, profile)
        self.assertFalse(any(r.get('kind') == 'Secret' for r in prepared))
        self.assertNotIn('synthetic-value', yaml.safe_dump_all(prepared))
        self.assertNotIn('synthetic-replacement', yaml.safe_dump_all(prepared))
        for name in upgrade.PRESERVED:
            self.assertEqual(completion.desired_jobs(prepared)[name], completion.desired_jobs(old)[name])

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
        proof.assert_called_once_with(state, 'backup', upgrade.render_profile(target)[0]['to_schema_heads']['control-plane'])
        return result

    def test_second_upgrade_preserves_completed033_jobs_and_requires034_catalog(self):
        _, old, _, _, _ = self.prepared()
        previous = State(old)
        controller_defaults(previous)
        with patch.object(completion, 'verify_schemas'):
            receipt = completion.capture(previous, old)
        target = copy.deepcopy([r for r in old if not r['metadata']['name'].startswith('kodex-bootstrap-upgrade-')])
        before_heads = json.loads(upgrade.installation(old)['schema-heads.json'])
        after_heads = {**before_heads, 'control-plane': 20261106000100}
        upgrade.installation(target).update({'source.sha': 'c' * 40, 'schema-heads.json': json.dumps(after_heads)})
        for job in completion.desired_jobs(target).values():
            job['spec']['template']['spec']['containers'][0]['image'] = 'registry.example.invalid/kodex/cli@sha256:' + 'c' * 64
            job['spec']['backoffLimit'] = 2
        before, after = yaml.safe_dump_all(old).encode(), yaml.safe_dump_all(target).encode()
        profile = {'schema': upgrade.SCHEMA, 'managed_by': 'git', 'previous_render_sha256': upgrade.sha(before),
                   'target_render_sha256': upgrade.sha(after), 'previous_source_sha': 'b' * 40, 'target_source_sha': 'c' * 40,
                   'from_schema_heads': before_heads, 'to_schema_heads': after_heads, 'schema_profile': 'backup'}
        prepared = upgrade.prepare(before, after, profile)
        state = UpgradeState(prepared, previous)
        composite = self.capture(state, prepared, old, receipt)
        self.assertEqual(composite['evidence']['migration_image_digest'], 'c' * 64)
        for name in upgrade.PRESERVED:
            self.assertEqual(completion.desired_jobs(prepared)[name], completion.desired_jobs(old)[name])
        with patch.object(completion, 'verify_schemas'), patch.object(upgrade, 'schema_proof', return_value='f' * 64) as proof:
            upgrade.verify_apply(state, prepared, composite)
            proof.assert_called_once_with(state, 'backup', 20261106000100)
        for start, end in ((20261030000100, 20261106000100), (20261106000100, 20261105000100),
                           (20261105000100, 20261107000100)):
            with self.subTest(start=start, end=end), self.assertRaises(Rejected):
                upgrade.validate({**profile, 'from_schema_heads': {**before_heads, 'control-plane': start},
                                  'to_schema_heads': {**after_heads, 'control-plane': end}})

    def test_schema_proof_selects_exact_version_and_profile_before_cluster_access(self):
        directory = Path(upgrade.__file__).resolve().parent.parent / 'release'
        state = SimpleNamespace(context='synthetic-context')
        for version, prefix, validator in ((20261105000100, 'platform-oidc-identity-', 'validPlatformOIDCIdentity'),
                                           (20261106000100, 'personal-credentials-', 'validPersonalCredentials')):
            for mode in ('plain', 'backup'):
                with self.subTest(version=version, mode=mode):
                    responses = [SimpleNamespace(returncode=0, stdout=b'{"synthetic":true}'), SimpleNamespace(returncode=0)]
                    with patch.object(upgrade.subprocess, 'run', side_effect=responses) as run:
                        self.assertEqual(upgrade.schema_proof(state, mode, version), upgrade.sha(responses[0].stdout))
                    query = (directory / (prefix + ('backup-' if mode == 'backup' else '') + 'migration-postcondition.sql')).read_bytes()
                    self.assertEqual(run.call_args_list[0].kwargs['input'], query)
                    expected = validator + ('Backup' if mode == 'backup' else '') + 'MigrationPostcondition'
                    self.assertIn(expected + ' as valid', run.call_args_list[1].args[0][-1])
                    for responses in ([SimpleNamespace(returncode=1, stdout=b'')],
                                      [SimpleNamespace(returncode=0, stdout=b'{}'), SimpleNamespace(returncode=1)]):
                        with patch.object(upgrade.subprocess, 'run', side_effect=responses), self.assertRaises(Rejected):
                            upgrade.schema_proof(state, mode, version)
        for mode, version in (('automatic', 20261106000100), ('plain', 20261107000100), ('backup', '20261106000100')):
            with patch.object(upgrade.subprocess, 'run') as run, self.assertRaises(Rejected):
                upgrade.schema_proof(state, mode, version)
            run.assert_not_called()

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
