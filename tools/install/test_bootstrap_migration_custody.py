"""Синтетическая проверка происхождения bootstrap без кластера, SQL и повторения effects."""
import copy
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import yaml
import bootstrap_completion as completion
import bootstrap_migration_adoption as adoption
import bootstrap_migration_custody as custody
import portable_state
from deployment_profile import fingerprint, Rejected
from test_bootstrap_completion import State, uuid, write
from test_bootstrap_upgrade import fixture, controller_defaults, pin_materializer
from test_deployment_profile import BUNDLED


class MigrationCustodyTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        old, _, _, _, _ = fixture()
        self.old = old
        old[0]['metadata']['labels']['app.kubernetes.io/managed-by'] = 'kodex-install'
        old[0]['data']['release-lock.sha256'] = 'c' * 64
        self.state = State(old)
        self.state.kubeconfig = Path('/synthetic/kubeconfig')
        self.state.current['kodex-installation-profile']['metadata']['resourceVersion'] = '19'
        controller_defaults(self.state)
        with patch.object(completion, 'verify_schemas'):
            self.receipt = completion.capture(self.state, old)
        self.receipt_path = self.root / 'original.json'
        self.receipt_sha = write(self.receipt_path, self.receipt)
        self.render_path = self.root / 'original.yaml'
        self.render_raw = yaml.safe_dump_all(old).encode()
        self.render_path.write_bytes(self.render_raw)
        self.render_path.chmod(0o600)
        self.render_sha = completion.digest(self.render_raw)
        self.dossier_path = self.root / 'dossier.json'
        self.dossier_sha = write(self.dossier_path, {'synthetic': 'dossier'})
        live = self.state.current['kodex-installation-profile']
        # Три поколения source/heads намеренно различаются: archive, declaration, live SQL.
        live['data']['source.sha'] = 'b' * 40
        heads = json.loads(live['data']['schema-heads.json'])
        heads['control-plane'] = 20261105000100
        live['data']['schema-heads.json'] = json.dumps(heads)
        self.witness = {
            'schema': custody.WITNESS_SCHEMA, 'observedAt': self.timestamp(),
            'context': self.state.context, 'clusterUID': self.state.cluster_uid,
            'namespaceUID': self.state.namespace_uid, 'installationUID': live['metadata']['uid'],
            'installationSHA256': custody.canonical_digest(live['data']),
            'declaredSourceSHA': 'b' * 40, 'declaredReleaseLockSHA256': 'c' * 64,
            'declaredSchemaHeads': heads, 'migrationSourceSHA': 'd' * 40,
            'schemaHeads': {**heads, 'control-plane': 20261121000200,
                            'internal-rpc-authority': 20261115000200},
            'archiveTerminalSHA256': '1' * 64, 'archiveExecutionSHA256': '2' * 64,
            'archiveJobUID': uuid(80), 'archivePodUID': uuid(81),
            'authorityBaselineSHA256': '3' * 64, 'storageSHA256': '4' * 64,
            'authorityIdentitySHA256': '5' * 64, 'schemaCatalogSHA256': '6' * 64,
            'dossierSHA256': self.dossier_sha}
        self.executions = []

    @staticmethod
    def timestamp(seconds=0):
        return (datetime.now(timezone.utc) + timedelta(seconds=seconds)).isoformat(timespec='milliseconds').replace('+00:00', 'Z')

    def execute(self, argv, **kwargs):
        self.executions.append((argv, kwargs))
        return subprocess.CompletedProcess(argv, 0, (json.dumps(self.witness) + '\n').encode(), b'')

    def producer(self, executor=None):
        return custody.MigrationWitnessProducer(self.dossier_path, self.dossier_sha, executor=executor or self.execute)

    def capture(self, **overrides):
        arguments = {'previous_receipt_path': self.receipt_path, 'previous_receipt_sha256': self.receipt_sha,
                     'previous_render_path': self.render_path, 'previous_render_sha256': self.render_sha,
                     'witness_producer': self.producer()}
        arguments.update(overrides)
        return custody.capture(self.state, **arguments)

    def test_independent_fresh_reader_preserves_original_effects_and_distinct_declarations(self):
        before = copy.deepcopy(self.state.current)
        with patch.object(completion, 'verify_schemas', side_effect=AssertionError('old schema must not be checked')):
            result = self.capture()
        self.assertEqual(result['jobs'], self.receipt['jobs'])
        self.assertEqual(result['archived_declaration']['source_sha'], 'a' * 40)
        self.assertEqual(result['live_declaration']['source_sha'], 'b' * 40)
        self.assertEqual(result['migration_source_sha'], 'd' * 40)
        self.assertNotEqual(result['archived_declaration']['schema_heads'], result['live_declaration']['schema_heads'])
        self.assertEqual(result['actual_schema_heads']['control-plane'], 20261121000200)
        self.assertEqual(result['migration_witness']['authorityIdentitySHA256'], self.witness['authorityIdentitySHA256'])
        self.assertEqual(result['migration_witness']['schemaCatalogSHA256'], self.witness['schemaCatalogSHA256'])
        self.assertFalse(result['effects_replayed'])
        self.assertFalse(result['apply_granted'])
        self.assertEqual(self.state.current, before)
        self.assertTrue(all(row[0] == 'get' for row in self.state.calls))
        argv, options = self.executions[0]
        program = Path(custody.__file__).resolve().parents[1] / 'release' / 'component-migration-adoption.mjs'
        self.assertEqual(argv, ['node', str(program), self.state.context, str(self.state.kubeconfig),
                                str(self.dossier_path), self.dossier_sha])
        self.assertEqual(options, {'capture_output': True, 'timeout': 110, 'check': False})
        path = self.root / 'custody.json'
        with self.assertRaises(Rejected):
            completion.load(path, write(path, result), allow_reconciled=True)

    def test_ttl_uses_original_rows_and_never_synthesizes_uid_or_status(self):
        for name in completion.JOBS:
            self.state.current.pop(name)
        before = copy.deepcopy(self.state.current)
        result = self.capture()
        self.assertEqual(result['jobs'], self.receipt['jobs'])
        self.assertEqual(self.state.current, before)
        result['jobs'][0]['job_uid'] = uuid(600)
        self.assertNotEqual(self.receipt['jobs'][0]['job_uid'], uuid(600))

    def test_original_receipt_render_cluster_and_job_drift_reject_before_producer(self):
        for change in ('receipt_pin', 'render_pin', 'render_bytes', 'cluster', 'namespace', 'job_uid', 'job_spec', 'job_failed'):
            with self.subTest(change=change):
                self.setUp()
                arguments = {}
                if change == 'receipt_pin': arguments['previous_receipt_sha256'] = '0' * 64
                elif change == 'render_pin': arguments['previous_render_sha256'] = '0' * 64
                elif change == 'render_bytes': self.render_path.write_bytes(self.render_raw + b'\n')
                elif change == 'cluster': self.state.cluster_uid = uuid(990)
                elif change == 'namespace': self.state.namespace_uid = uuid(991)
                else:
                    job = self.state.current[completion.JOBS[0]]
                    if change == 'job_uid': job['metadata']['uid'] = uuid(992)
                    elif change == 'job_spec': job['spec']['template']['spec']['containers'][0]['args'] = ['check']
                    else: job['status']['active'] = 1
                with self.assertRaises(Rejected): self.capture(**arguments)
                self.assertEqual(self.executions, [])

    def test_reconciled_receipt_cannot_replace_original_live_observation(self):
        self.receipt['origin'] = 'RECONCILED_VERIFIED_PREFLIGHT'
        for row in self.receipt['jobs']: row['completion_time'] = None
        self.receipt['evidence'] = {name: 'e' * (40 if name == 'verification_source_sha' else 64) for name in
            ('inventory_sha256', 'namespace_inventory_sha256', 'verification_sha256',
             'verification_source_sha', 'verification_archive_sha256')}
        self.receipt_sha = write(self.receipt_path, self.receipt)
        with self.assertRaisesRegex(Rejected, 'EXPLICIT_RECONCILED_COMPLETION_REQUIRED'): self.capture()
        self.assertEqual(self.executions, [])

    def test_owned_installation_is_mandatory_without_equating_old_and_current_data(self):
        for change in ('missing', 'foreign', 'managed_by', 'namespace', 'deleting', 'uid', 'resource_version'):
            with self.subTest(change=change):
                self.setUp()
                live = self.state.current['kodex-installation-profile']
                if change == 'missing': self.state.current.pop('kodex-installation-profile')
                elif change == 'foreign': live['metadata']['labels']['app.kubernetes.io/part-of'] = 'other'
                elif change == 'managed_by': live['metadata']['labels']['app.kubernetes.io/managed-by'] = 'other'
                elif change == 'namespace': live['metadata']['namespace'] = 'other'
                elif change == 'deleting': live['metadata']['deletionTimestamp'] = self.timestamp()
                elif change == 'uid': live['metadata']['uid'] = 'invalid'
                else: live['metadata']['resourceVersion'] = 19
                with self.assertRaises(Rejected): self.capture()
                self.assertEqual(self.executions, [])

    def test_closed_witness_shape_heads_freshness_and_live_binding(self):
        changes = [('extra', 'unknown'), ('schema', 'unknown'), ('context', 'other'),
                   ('clusterUID', uuid(950)), ('namespaceUID', uuid(951)), ('installationUID', uuid(952)),
                   ('installationSHA256', 'a' * 64), ('declaredSourceSHA', 'e' * 40),
                   ('declaredReleaseLockSHA256', 'e' * 64), ('dossierSHA256', 'e' * 64),
                   ('archivePodUID', 'invalid'), ('storageSHA256', 'invalid'),
                   ('authorityIdentitySHA256', 'invalid'), ('schemaCatalogSHA256', 'invalid'),
                   ('declaredSchemaHeads', {**self.witness['declaredSchemaHeads'], 'email-bridge': 1}),
                   ('schemaHeads', {'control-plane': 20261121000200}),
                   ('schemaHeads', {**self.witness['schemaHeads'], 'email-bridge': True}),
                   ('observedAt', self.timestamp(-60)), ('observedAt', self.timestamp(60)),
                   ('observedAt', 'invalid')]
        original = copy.deepcopy(self.witness)
        for name, value in changes:
            with self.subTest(field=name, value=value):
                self.witness = {**copy.deepcopy(original), name: value}
                with self.assertRaises((Rejected, ValueError)): self.capture()

    def test_raw_json_and_generic_callback_are_not_producer_ports(self):
        for value in (self.witness, lambda state: self.witness, self.root / 'witness.json'):
            with self.subTest(value=type(value).__name__), self.assertRaisesRegex(Rejected, 'PRODUCER_REQUIRED'):
                self.capture(witness_producer=value)

    def test_duplicate_json_failed_reader_timeout_and_oversized_output_fail_closed(self):
        for output, returncode in ((b'{"schema":1,"schema":2}', 0), (b'{}', 1), (b'x' * 65537, 0), (b'[]', 0)):
            def executor(argv, **kwargs): return subprocess.CompletedProcess(argv, returncode, output, b'private diagnostic')
            with self.subTest(returncode=returncode, size=len(output)), self.assertRaises(Rejected):
                self.capture(witness_producer=self.producer(executor))
        def timeout(argv, **kwargs): raise subprocess.TimeoutExpired(argv, kwargs['timeout'])
        with self.assertRaisesRegex(Rejected, '^MIGRATION_CUSTODY_PRODUCER_REJECTED$'):
            self.capture(witness_producer=self.producer(timeout))

    def test_late_job_installation_namespace_and_dossier_change_fail_closed(self):
        for change in ('job_uid', 'installation', 'namespace', 'dossier'):
            with self.subTest(change=change):
                self.setUp()
                def executor(argv, **kwargs):
                    result = self.execute(argv, **kwargs)
                    if change == 'job_uid': self.state.current[completion.JOBS[0]]['metadata']['uid'] = uuid(900)
                    elif change == 'installation': self.state.current['kodex-installation-profile']['metadata']['resourceVersion'] = '20'
                    elif change == 'namespace': self.state.namespace_uid = uuid(901)
                    else: write(self.dossier_path, {'synthetic': 'changed'})
                    return result
                with self.assertRaises(Rejected): self.capture(witness_producer=self.producer(executor))

    def test_dossier_pin_permissions_and_explicit_kubeconfig_fail_before_executor(self):
        for change in ('pin', 'permissions', 'implicit_kubeconfig', 'relative_kubeconfig'):
            with self.subTest(change=change):
                self.setUp()
                if change == 'pin': write(self.dossier_path, {'synthetic': 'changed'})
                elif change == 'permissions': self.dossier_path.chmod(0o644)
                elif change == 'implicit_kubeconfig': self.state.kubeconfig = None
                else: self.state.kubeconfig = Path('relative')
                with self.assertRaises(Rejected): self.capture()
                self.assertEqual(self.executions, [])

    def cli_arguments(self, output):
        return [custody.__file__, '--context', self.state.context, '--kubeconfig', str(self.state.kubeconfig),
                '--previous-render', str(self.render_path), '--previous-render-sha256', self.render_sha,
                '--previous-receipt', str(self.receipt_path), '--previous-receipt-sha256', self.receipt_sha,
                '--dossier', str(self.dossier_path), '--dossier-sha256', self.dossier_sha, '--output', str(output)]

    def test_cli_discovers_only_original_bootstrap_set_and_writes_new_private_custody(self):
        output = self.root / 'custody.json'
        with patch.object(sys, 'argv', self.cli_arguments(output)), \
                patch.object(portable_state, 'State', return_value=self.state), \
                patch.object(self.state, 'discover', create=True) as discover, \
                patch.object(custody.subprocess, 'run', side_effect=self.execute), \
                patch('builtins.print') as printed:
            custody.main()
        discovered = discover.call_args.args[0]
        self.assertEqual(len(discovered), 6)
        self.assertEqual({r['metadata']['name'] for r in discovered},
                         {'kodex-installation-profile', *completion.JOBS})
        self.assertEqual(output.stat().st_mode & 0o777, 0o600)
        value = json.loads(completion.private_bytes(output))
        self.assertEqual(value['schema'], custody.SCHEMA)
        self.assertEqual(value['jobs'], self.receipt['jobs'])
        summary = json.loads(printed.call_args.args[0])
        self.assertEqual(summary['sha256'], completion.digest(output.read_bytes()))
        self.assertFalse(summary['apply_granted'])
        with patch.object(sys, 'argv', self.cli_arguments(output)), \
                patch.object(portable_state, 'State', return_value=self.state), \
                patch.object(self.state, 'discover', create=True), \
                patch.object(custody.subprocess, 'run', side_effect=self.execute), \
                self.assertRaisesRegex(SystemExit, 'Bootstrap migration custody rejected'):
            custody.main()
        self.assertEqual(json.loads(completion.private_bytes(output)), value)

    def test_cli_pin_failure_precedes_discovery_and_has_sanitized_error(self):
        for name in ('receipt', 'render', 'dossier'):
            with self.subTest(input=name):
                self.setUp()
                target = {'receipt': self.receipt_path, 'render': self.render_path, 'dossier': self.dossier_path}[name]
                target.write_bytes(b'private sensitive bytes')
                with patch.object(sys, 'argv', self.cli_arguments(self.root / 'custody.json')), \
                        patch.object(portable_state, 'State') as state, \
                        self.assertRaisesRegex(SystemExit,
                            '^Bootstrap migration custody rejected; no bootstrap effects executed$'):
                    custody.main()
                state.assert_not_called()

    def test_repeat_comparison_checks_fresh_owner_and_stable_identifiers(self):
        prior = self.capture()
        expected = self.root / 'expected.json'
        for key in (None, 'authorityIdentitySHA256', 'schemaCatalogSHA256', 'storageSHA256', 'archiveTerminalSHA256'):
            with self.subTest(changed=key):
                pinned = copy.deepcopy(prior)
                pinned['migration_witness']['observedAt'] = self.timestamp(-3600)
                pinned['migration_witness']['authorityBaselineSHA256'] = '7' * 64
                pinned['migration_witness_sha256'] = '8' * 64
                if key is not None:
                    pinned['migration_witness'][key] = '9' * 64
                output = self.root / ((key or 'unchanged') + '.json')
                arguments = self.cli_arguments(output) + [
                    '--expected-custody', str(expected), '--expected-custody-sha256', write(expected, pinned)]
                executions = len(self.executions)
                with patch.object(sys, 'argv', arguments), \
                        patch.object(portable_state, 'State', return_value=self.state), \
                        patch.object(self.state, 'discover', create=True), \
                        patch.object(custody.subprocess, 'run', side_effect=self.execute), \
                        patch('builtins.print'):
                    if key is None:
                        custody.main()
                    else:
                        with self.assertRaisesRegex(SystemExit, 'Bootstrap migration custody rejected'):
                            custody.main()
                self.assertEqual(len(self.executions), executions + 1)
                self.assertEqual(output.exists(), key is None)

    def test_repeat_pin_mismatch_precedes_discovery(self):
        expected = self.root / 'expected.json'
        write(expected, self.capture())
        output = self.root / 'repeat.json'
        args = self.cli_arguments(output) + [
            '--expected-custody', str(expected), '--expected-custody-sha256', '0' * 64]
        with patch.object(sys, 'argv', args), patch.object(portable_state, 'State') as state, \
                self.assertRaisesRegex(SystemExit, 'Bootstrap migration custody rejected'):
            custody.main()
        state.assert_not_called()
        self.assertFalse(output.exists())

    def adoption_inputs(self):
        _, target, _, _, _ = fixture()
        target[0]['metadata']['labels']['app.kubernetes.io/managed-by'] = 'kodex-install'
        target[0]['data'].update({'source.sha': 'e' * 40, 'profile.json': json.dumps(BUNDLED),
            'profile.sha256': fingerprint(BUNDLED), 'schema-heads.json': json.dumps(self.witness['schemaHeads'])})
        pin_materializer(target, 'e' * 40)
        inventory = json.loads(adoption.LOCK_VALIDATOR.with_name('images.json').read_bytes())['images']
        registry = 'registry.example.invalid'
        image_digest = 'sha256:' + 'e' * 64
        lock = {'schema_version': 2, 'profile': 'web-only', 'source_sha': 'e' * 40, 'build_run_id': 'local',
            'role_base_runtime_engine': 'opencode', 'schema_heads': self.witness['schemaHeads'],
            'registry': {'push': registry, 'node_pull': registry, 'repository_prefix': 'kodex'},
            'images': [{'component': row['component'], 'repository': 'kodex/' + row['component'], 'digest': image_digest,
                        'pull_ref': registry + '/kodex/' + row['component'] + '@' + image_digest} for row in inventory],
            'external_images': [{'component': 'admission-tools', 'digest': image_digest,
                                 'pull_ref': registry + '/tools/admission@' + image_digest}],
            'role_image_input': {'repository': 'kodex/role-image-inputs', 'manifest_digest': image_digest,
                                 'payload_sha256': '1' * 64, 'source_sha256': '2' * 64,
                                 'pull_ref': registry + '/kodex/role-image-inputs@' + image_digest}}
        lock_path = self.root / 'release-lock.json'
        target[0]['data']['release-lock.sha256'] = write(lock_path, lock)
        raw = adoption.upgrade.render_bytes(target)
        target_path = self.root / 'target.yaml'
        target_path.write_bytes(raw)
        target_path.chmod(0o600)
        prior = self.root / 'expected.json'
        return target, SimpleNamespace(context=self.state.context, kubeconfig=self.state.kubeconfig,
            archive_receipt=self.receipt_path, archive_receipt_sha256=self.receipt_sha,
            archive_render=self.render_path, archive_render_sha256=self.render_sha,
            expected_custody=prior, expected_custody_sha256=write(prior, self.capture()),
            target_render=target_path, target_render_sha256=completion.digest(raw), release_lock=lock_path,
            dossier=self.dossier_path, dossier_sha256=self.dossier_sha)

    def test_migrated_adoption_keeps_five_effects_and_prepares_only_observed_schema(self):
        target, args = self.adoption_inputs()
        prior = completion.pinned_json(args.expected_custody, args.expected_custody_sha256)
        prepared = adoption.prepare_target(adoption.upgrade.decode_render(self.render_raw),
                                           adoption.upgrade.decode_render(args.target_render.read_bytes()), prior)
        self.assertEqual(completion.desired_jobs(prepared), completion.desired_jobs(self.old))
        self.assertEqual(prepared[0], target[0])
        job, script = adoption.materializer.resolve(prepared, versioned_required=True)
        self.assertEqual(job['metadata']['name'], 'release-materializer-' + 'e' * 40)
        self.assertTrue(script['immutable'])
        old = self.state.current[adoption.materializer.JOB]
        old['status']['conditions'] = [{'type': 'Complete', 'status': 'True'}]
        existing = self.state.call

        def read(args):
            if '-l' not in args:
                return existing(args)
            self.state.calls.append(args)
            return json.dumps({'items': [old] if args[3] == 'jobs' else []}).encode()

        before = copy.deepcopy(self.state.current)
        actual_run = subprocess.run

        def execute(argv, **options):
            runner = self.execute if argv[0] == 'node' else actual_run
            return runner(argv, **options)

        with patch.object(adoption, 'State', return_value=self.state), \
                patch.object(self.state, 'discover', create=True), \
                patch.object(self.state, 'call', side_effect=read), \
                patch.object(custody.subprocess, 'run', side_effect=execute):
            report = adoption.run(args)
        self.assertEqual(report['prepared_render_sha256'], completion.digest(adoption.upgrade.render_bytes(prepared)))
        self.assertEqual(report['target']['schema_heads'], self.witness['schemaHeads'])
        self.assertEqual(report['before_installation_resource_version'], '19')
        self.assertNotEqual(report['before_installation_sha256'], report['after_installation_sha256'])
        self.assertFalse(report['apply_granted'])
        self.assertEqual(self.state.current, before)
        self.assertTrue(all('get' in args for args in self.state.calls))

        verify = adoption.authority.verify_upgrade_preconditions
        passes = []

        def change_after_verify(*arguments, **options):
            verify(*arguments, **options)
            passes.append(True)
            if len(passes) == 2:
                self.state.current[completion.JOBS[0]]['metadata']['uid'] = uuid(975)

        with patch.object(adoption, 'State', return_value=self.state), \
                patch.object(self.state, 'discover', create=True), \
                patch.object(self.state, 'call', side_effect=read), \
                patch.object(adoption.authority, 'verify_upgrade_preconditions', side_effect=change_after_verify), \
                patch.object(custody.subprocess, 'run', side_effect=execute), self.assertRaises(Rejected):
            adoption.run(args)
        self.assertEqual(len(passes), 2)

    def test_migrated_adoption_refuses_invalid_target_before_discovery(self):
        target, args = self.adoption_inputs()
        for alteration in ('bytes', 'schema', 'effect', 'unlocked-materializer', 'lock-digest', 'lock-type'):
            with self.subTest(alteration=alteration):
                changed = copy.deepcopy(target)
                if alteration == 'schema':
                    changed[0]['data']['schema-heads.json'] = self.state.current['kodex-installation-profile']['data']['schema-heads.json']
                elif alteration == 'effect':
                    completion.desired_jobs(changed)[adoption.upgrade.MIGRATION]['spec']['template']['spec']['containers'][0]['args'] = ['check']
                elif alteration == 'unlocked-materializer':
                    job, _ = adoption.materializer.resolve(changed)
                    job['spec']['template']['spec']['containers'][0]['image'] = 'registry.example.invalid/new@sha256:' + 'd' * 64
                elif alteration == 'lock-digest':
                    with args.release_lock.open('ab') as lock:
                        lock.write(b'\n')
                elif alteration == 'lock-type':
                    changed[0]['data']['release-lock.sha256'] = write(args.release_lock, [])
                raw = adoption.upgrade.render_bytes(changed)
                args.target_render.write_bytes(raw)
                args.target_render_sha256 = '0' * 64 if alteration == 'bytes' else completion.digest(raw)
                with patch.object(adoption, 'State') as state, self.assertRaises(Rejected):
                    adoption.run(args)
                state.assert_not_called()


if __name__ == '__main__':
    unittest.main()
