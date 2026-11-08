"""Обновление images сохраняет пять настоящих effects и не повторяет DDL даже после TTL Jobs."""
import copy
import json
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import patch

import yaml
import bootstrap_completion as completion
import bootstrap_upgrade as upgrade
from deployment_profile import Rejected
from test_bootstrap_completion import State, uuid, write
from test_bootstrap_upgrade import fixture, controller_defaults


class ImageUpgradeTests(unittest.TestCase):
    def test_complete_web_profiles_preserve_every_bootstrap_job_and_unrelated_resource(self):
        root = Path(__file__).resolve().parents[2]
        heads = {'control-plane': 20261108000100, 'internal-rpc-authority': 20260912000200,
                 'email-bridge': 20260905000200}
        for name in ('web-only', 'web-with-mattermost'):
            with self.subTest(profile=name):
                raw = subprocess.run(['kubectl', 'kustomize', str(root / 'deploy/k8s/profiles' / name)],
                                     capture_output=True, timeout=20, check=True).stdout
                old = [r for r in yaml.safe_load_all(raw) if r is not None and r.get('kind') != 'Secret']
                old.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {
                    'name': 'kodex-installation-profile', 'namespace': 'kodex-system'},
                    'data': {'source.sha': 'a' * 40, 'schema-heads.json': json.dumps(heads)}})
                for job in completion.desired_jobs(old).values():
                    for field in ('containers', 'initContainers'):
                        for container in job['spec']['template']['spec'].get(field, []):
                            container['image'] = 'registry.example.invalid/bootstrap@sha256:' + 'a' * 64
                target = copy.deepcopy(old)
                upgrade.installation(target)['source.sha'] = 'b' * 40
                for job in completion.desired_jobs(target).values():
                    for field in ('containers', 'initContainers'):
                        for container in job['spec']['template']['spec'].get(field, []):
                            container['image'] = 'registry.example.invalid/bootstrap@sha256:' + 'b' * 64
                before, after = yaml.safe_dump_all(old).encode(), yaml.safe_dump_all(target).encode()
                profile = {'schema': upgrade.IMAGE_SCHEMA, 'upgrade_kind': upgrade.IMAGE_KIND, 'managed_by': 'git',
                    'previous_source_sha': 'a' * 40, 'target_source_sha': 'b' * 40, 'from_schema_heads': heads,
                    'to_schema_heads': heads, 'schema_profile': 'backup', 'previous_render_sha256': upgrade.sha(before),
                    'target_render_sha256': upgrade.sha(after)}
                prepared = upgrade.prepare(before, after, profile)
                self.assertEqual(completion.desired_jobs(prepared), completion.desired_jobs(old))
                expected = copy.deepcopy(target)
                for index, resource in enumerate(expected):
                    if resource.get('kind') == 'Job' and resource['metadata']['name'] in completion.JOBS:
                        expected[index] = copy.deepcopy(completion.desired_jobs(old)[resource['metadata']['name']])
                self.assertEqual(prepared[:-1], expected)

    def prepared(self):
        old, target, before, after, profile = fixture(20261108000100, 20261108000100)
        profile.update(schema=upgrade.IMAGE_SCHEMA, upgrade_kind=upgrade.IMAGE_KIND)
        state = State(old)
        controller_defaults(state)
        with patch.object(completion, 'verify_schemas'):
            receipt = completion.capture(state, old)
        return old, target, before, after, profile, state, receipt

    def capture(self, state, prepared, old, receipt):
        with patch.object(completion, 'verify_schemas'), patch.object(upgrade, 'verify_schemas'), \
                patch.object(upgrade, 'schema_proof', return_value='f' * 64) as proof:
            result = upgrade.capture(state, prepared, old, receipt, 'e' * 64)
        proof.assert_called_once_with(state, 'backup', 20261108000100)
        return result

    def test_preserves_every_effect_and_capture_never_executes_new_job(self):
        old, _, before, after, profile, state, receipt = self.prepared()
        prepared = upgrade.prepare(before, after, profile)
        self.assertEqual(completion.desired_jobs(prepared), completion.desired_jobs(old))
        preimage = copy.deepcopy(state.current)
        result = self.capture(state, prepared, old, receipt)
        self.assertEqual(result['origin'], upgrade.IMAGE_ORIGIN)
        self.assertEqual(result['jobs'], receipt['jobs'])
        self.assertNotIn('migration_job_uid', result['evidence'])
        self.assertEqual(state.current, preimage)
        self.assertTrue(all(call[0] == 'get' for call in state.calls))
        self.assertFalse(any('pods' in call for call in state.calls))
        with patch.object(completion, 'verify_schemas'), patch.object(upgrade, 'schema_proof', return_value='1' * 64):
            self.assertEqual(upgrade.verify_apply(state, prepared, result)['status'], 'SAME_SCHEMA_UPGRADE_APPLY_VERIFIED')

    def test_ttl_absence_uses_exact_previous_receipt_without_synthesizing_completion(self):
        old, _, before, after, profile, state, receipt = self.prepared()
        state.current = {}
        result = self.capture(state, upgrade.prepare(before, after, profile), old, receipt)
        self.assertEqual(result['jobs'], receipt['jobs'])
        self.assertEqual(state.current, {})
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'receipt'
            digest = write(path, result)
            with self.assertRaises(Rejected): completion.load(path, digest)
            self.assertEqual(completion.load(path, digest, True), result)

    def test_receipt_from_reconciled_history_keeps_unobserved_timestamps(self):
        old, _, before, after, profile, state, receipt = self.prepared()
        receipt['origin'] = completion.ORIGINS[1]
        for row in receipt['jobs']: row['completion_time'] = None
        receipt['evidence'] = {name: 'd' * (40 if name == 'verification_source_sha' else 64) for name in
            ('inventory_sha256', 'namespace_inventory_sha256', 'verification_sha256',
             'verification_source_sha', 'verification_archive_sha256')}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'previous'
            receipt = completion.load(path, write(path, receipt), True)
        state.current = {}
        result = self.capture(state, upgrade.prepare(before, after, profile), old, receipt)
        self.assertTrue(all(row['completion_time'] is None for row in result['jobs']))
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'receipt'
            self.assertEqual(completion.load(path, write(path, result), True), result)

    def test_profiles_are_distinct_and_reject_schema_source_and_unknown_field_drift(self):
        _, _, _, _, profile, _, _ = self.prepared()
        for field, value in (('schema', upgrade.SCHEMA), ('upgrade_kind', 'FORWARD_MIGRATION'),
                             ('target_source_sha', profile['previous_source_sha']), ('unknown', 'synthetic'),
                             ('from_schema_heads', {**profile['from_schema_heads'], 'control-plane': 20261107000100}),
                             ('to_schema_heads', {**profile['to_schema_heads'], 'email-bridge': 20260905000300})):
            with self.subTest(field=field), self.assertRaises(Rejected): upgrade.validate({**profile, field: value})
        forward = {k: v for k, v in profile.items() if k != 'upgrade_kind'}
        forward['schema'] = upgrade.SCHEMA
        with self.assertRaises(Rejected): upgrade.validate(forward)

    def test_changed_bootstrap_effect_is_rejected_for_all_five_jobs(self):
        _, target, before, _, profile, _, _ = self.prepared()
        for name in completion.JOBS:
            for field, value in (('args', ['status']), ('command', ['/bin/false']),
                                 ('env', [{'name': 'FOREIGN', 'value': 'synthetic'}]),
                                 ('image', 'registry.example.invalid/unpinned:latest')):
                changed = copy.deepcopy(target)
                completion.desired_jobs(changed)[name]['spec']['template']['spec']['containers'][0][field] = value
                after = yaml.safe_dump_all(changed).encode()
                with self.subTest(name=name, field=field), self.assertRaises(Rejected):
                    upgrade.prepare(before, after, {**profile, 'target_render_sha256': upgrade.sha(after)})
            for field, value in (('serviceAccountName', 'foreign'), ('volumes', [{'name': 'foreign', 'emptyDir': {}}])):
                changed = copy.deepcopy(target)
                completion.desired_jobs(changed)[name]['spec']['template']['spec'][field] = value
                after = yaml.safe_dump_all(changed).encode()
                with self.subTest(name=name, field=field), self.assertRaises(Rejected):
                    upgrade.prepare(before, after, {**profile, 'target_render_sha256': upgrade.sha(after)})

    def test_replacement_failed_job_receipt_or_cluster_drift_stops_before_native_proof(self):
        for change in ('uid', 'failed', 'receipt', 'cluster', 'namespace', 'target_spec'):
            old, _, before, after, profile, state, receipt = self.prepared()
            prepared = upgrade.prepare(before, after, profile)
            if change == 'uid': state.current[upgrade.MIGRATION]['metadata']['uid'] = uuid(900)
            elif change == 'failed': state.current[upgrade.MIGRATION]['status']['active'] = 1
            elif change == 'receipt': receipt['jobs'][0]['desired_sha256'] = '0' * 64
            elif change == 'cluster': state.cluster_uid = uuid(901)
            elif change == 'namespace': state.namespace_uid = uuid(902)
            else: completion.desired_jobs(prepared)[upgrade.MIGRATION]['spec']['backoffLimit'] = 1
            with self.subTest(change=change), patch.object(completion, 'verify_schemas'), \
                    patch.object(upgrade, 'schema_proof') as proof, self.assertRaises(Rejected):
                upgrade.capture(state, prepared, old, receipt, 'e' * 64)
            proof.assert_not_called()

    def test_native_schema_failure_and_late_job_replacement_fail_closed(self):
        old, _, before, after, profile, state, receipt = self.prepared()
        prepared = upgrade.prepare(before, after, profile)
        with patch.object(completion, 'verify_schemas'), patch.object(upgrade, 'verify_schemas'), \
                patch.object(upgrade, 'schema_proof', side_effect=Rejected('NATIVE_SCHEMA_FAILED')), self.assertRaises(Rejected):
            upgrade.capture(state, prepared, old, receipt, 'e' * 64)
        def replaced(*args):
            state.current[upgrade.MIGRATION]['metadata']['uid'] = uuid(903)
            return 'f' * 64
        with patch.object(completion, 'verify_schemas'), patch.object(upgrade, 'verify_schemas'), \
                patch.object(upgrade, 'schema_proof', side_effect=replaced) as proof, self.assertRaises(Rejected):
            upgrade.capture(state, prepared, old, receipt, 'e' * 64)
        proof.assert_called_once()

    def test_apply_rechecks_job_identity_after_native_proof_before_returning_success(self):
        old, _, before, after, profile, state, receipt = self.prepared()
        prepared = upgrade.prepare(before, after, profile)
        result = self.capture(state, prepared, old, receipt)
        def replaced(*args):
            state.current[upgrade.MIGRATION]['metadata']['uid'] = uuid(905)
            return 'f' * 64
        with patch.object(completion, 'verify_schemas'), \
                patch.object(upgrade, 'schema_proof', side_effect=replaced) as proof, self.assertRaises(Rejected):
            upgrade.verify_apply(state, prepared, result)
        proof.assert_called_once()

    def test_render_pins_and_profile_materialization_are_verified_before_cluster_access(self):
        old, _, before, after, profile, state, receipt = self.prepared()
        for changed_before, changed_after in ((before + b'\n', after), (before, after + b'\n')):
            with self.assertRaises(Rejected): upgrade.prepare(changed_before, changed_after, profile)
        prepared = upgrade.prepare(before, after, profile)
        profile_resource = next(r for r in prepared if r['metadata']['name'].startswith('kodex-bootstrap-upgrade-'))
        profile_resource['immutable'] = False
        state.calls.clear()
        with self.assertRaises(Rejected): upgrade.capture(state, prepared, old, receipt, 'e' * 64)
        self.assertEqual(state.calls, [])

    def test_new_origin_does_not_accept_forward_receipt_or_fabricated_migration_evidence(self):
        old, _, before, after, profile, state, receipt = self.prepared()
        prepared = upgrade.prepare(before, after, profile)
        result = self.capture(state, prepared, old, receipt)
        with patch.object(completion, 'verify_schemas'), self.assertRaises(Rejected):
            upgrade.verify_apply(state, prepared, {**result, 'origin': upgrade.ORIGIN})
        for changes in ({'migration_job_uid': uuid(904)}, {'schema_proof_sha256': 'invalid'}, {'unknown': 'synthetic'}):
            altered = copy.deepcopy(result)
            altered['evidence'].update(changes)
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'receipt'
                with self.assertRaises(Rejected): completion.load(path, write(path, altered), True)

    def test_second_image_upgrade_preserves_original_uids_and_can_be_verified_again(self):
        old, _, before, after, profile, state, receipt = self.prepared()
        first = upgrade.prepare(before, after, profile)
        first_receipt = self.capture(state, first, old, receipt)
        target = copy.deepcopy([r for r in first if not r['metadata']['name'].startswith('kodex-bootstrap-upgrade-')])
        upgrade.installation(target)['source.sha'] = 'c' * 40
        for job in completion.desired_jobs(target).values():
            job['spec']['template']['spec']['containers'][0]['image'] = 'registry.example.invalid/kodex/cli@sha256:' + 'c' * 64
        before2, after2 = yaml.safe_dump_all(first).encode(), yaml.safe_dump_all(target).encode()
        profile2 = {**profile, 'previous_source_sha': 'b' * 40, 'target_source_sha': 'c' * 40,
                    'previous_render_sha256': upgrade.sha(before2), 'target_render_sha256': upgrade.sha(after2)}
        second = upgrade.prepare(before2, after2, profile2)
        final = self.capture(state, second, first, first_receipt)
        self.assertEqual(final['jobs'], receipt['jobs'])
        for _ in range(2):
            with patch.object(completion, 'verify_schemas'), patch.object(upgrade, 'schema_proof', return_value='f' * 64):
                upgrade.verify_apply(state, second, final)


if __name__ == '__main__': unittest.main()
