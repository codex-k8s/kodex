"""Сценарии допуска, частичного rollout и потерянного ACK без повторения bootstrap."""
import copy
import json
import subprocess
import unittest
from unittest.mock import patch

import bootstrap_completion as completion
import bootstrap_migration_adoption as adoption
import bootstrap_migration_custody as custody
import bootstrap_migration_rollout as rollout
import bootstrap_upgrade as upgrade
from deployment_profile import Rejected
from portable_state import verify_workload_ready
import release_materializer as materializer
import test_bootstrap_migration_custody as fixtures
from test_bootstrap_completion import uuid
from test_bootstrap_upgrade import controller_defaults


class MigrationRolloutTests(unittest.TestCase):
    def setUp(self):
        fixture = fixtures.MigrationCustodyTests()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.fixture = fixture
        self.state = fixture.state
        target, args = fixture.adoption_inputs()
        name = 'control-api-gateway'
        metadata = {'name': name, 'namespace': 'kodex-system', 'labels': {'app.kubernetes.io/part-of': 'kodex'}}
        self.service = {'apiVersion': 'v1', 'kind': 'Service', 'metadata': copy.deepcopy(metadata),
                        'spec': {'selector': {'app': 'new'}, 'ports': [{'port': 443}]}}
        self.deployment = {'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': copy.deepcopy(metadata),
                           'spec': {'replicas': 2, 'template': {'spec': {'containers': [
                               {'name': 'api', 'image': 'registry.example.invalid/gateway@sha256:' + 'b' * 64}]}}}}
        self.other_deployment = copy.deepcopy(self.deployment)
        self.other_deployment['metadata']['name'] = 'synthetic-staff'
        target.extend((self.service, self.deployment, self.other_deployment))
        existing_staff = self.state.current['synthetic-staff'] = copy.deepcopy(self.other_deployment)
        existing_staff['metadata']['uid'] = uuid(199)
        raw = upgrade.render_bytes(target)
        args.target_render.write_bytes(raw)
        args.target_render_sha256 = completion.digest(raw)
        self.args = args
        self.old_service, self.old_deployment = map(copy.deepcopy, (self.service, self.deployment))
        self.old_service['spec']['selector']['app'] = 'old'
        self.old_deployment['spec']['replicas'] = 1
        self.current_serving = {'Service': self.old_service, 'Deployment': self.old_deployment}
        for index, value in enumerate(self.current_serving.values(), 1):
            value['metadata'].update(uid=uuid(100 + index), resourceVersion='61')
        legacy = self.state.current[materializer.JOB]
        legacy['status'].update(conditions=[{'type': 'Complete', 'status': 'True'}])
        original_get, original_call = self.state.get, self.state.call

        def get(desired):
            if desired['metadata']['name'] == name:
                return copy.deepcopy(self.current_serving[desired['kind']])
            return original_get(desired)

        def call(arguments):
            if '-l' in arguments:
                self.state.calls.append(arguments)
                if arguments[3] == 'pods':
                    return b'{"items":[]}'
                jobs = [v for v in self.state.current.values() if
                        v['metadata'].get('labels', {}).get(materializer.NAME_LABEL) == materializer.JOB]
                return json.dumps({'items': jobs}).encode()
            return original_call(arguments)

        self.state.get, self.state.call = get, call
        old = upgrade.decode_render(fixture.render_raw)
        self.resources = adoption.prepare_target(old, upgrade.decode_render(upgrade.render_bytes(target)),
                                                 fixture.capture())
        self.installation = custody.installation_resource(self.resources)
        self.plan = {
            'prepared_render_sha256': completion.digest(upgrade.render_bytes(self.resources)),
            'dossier_sha256': args.dossier_sha256, 'installation_uid': fixture.witness['installationUID'],
            'before_installation_sha256': fixture.witness['installationSHA256'],
            'before_installation_resource_version': '19',
            'after_installation_sha256': custody.canonical_digest(upgrade.installation(self.resources)),
            'archive_receipt_sha256': fixture.receipt_sha,
        }
        self.before_serving = self.serving()
        self.dossier = args.dossier
        self.native = patch.object(rollout, 'read_continuity', side_effect=self.continuity)
        self.native.start()
        self.addCleanup(self.native.stop)
        self.schema = patch.object(completion, 'verify_schemas')
        self.schema.start()
        self.addCleanup(self.schema.stop)
        self.receipt = rollout.capture(self.state, self.resources, fixture.receipt,
                                       self.plan, 'a' * 64, fixture.witness, self.dossier)

    def serving(self):
        service, deployment = (self.current_serving[kind] for kind in ('Service', 'Deployment'))
        return {'serviceUID': service['metadata']['uid'], 'serviceResourceVersion': service['metadata']['resourceVersion'],
                'serviceSpecSHA256': custody.canonical_digest(service['spec']),
                'serviceAnnotationsSHA256': custody.canonical_digest(service['metadata'].get('annotations', {})),
                'deploymentUID': deployment['metadata']['uid'],
                'deploymentResourceVersion': deployment['metadata']['resourceVersion'],
                'deploymentSpecSHA256': custody.canonical_digest(deployment['spec'])}

    def continuity(self, state, dossier, digest):
        self.assertIs(state, self.state)
        self.assertEqual(dossier, self.dossier)
        self.assertEqual(digest, self.fixture.dossier_sha)
        config = self.state.current['kodex-installation-profile']
        return {**self.fixture.witness, **self.serving(),
                'schema': rollout.CONTINUITY_SCHEMA,
                'installationSHA256': custody.canonical_digest(config['data']),
                'installationResourceVersion': config['metadata']['resourceVersion'],
                'durableAuthorityIdentitySHA256': '8' * 64,
                'beforeApiServiceSpecSHA256': self.before_serving['serviceSpecSHA256'],
                'beforeApiServiceAnnotationsSHA256': self.before_serving['serviceAnnotationsSHA256'],
                'beforeApiDeploymentSpecSHA256': self.before_serving['deploymentSpecSHA256']}

    def ready(self):
        for desired in (self.service, self.deployment):
            live = self.current_serving[desired['kind']]
            live['spec'] = copy.deepcopy(desired['spec'])
            live['metadata']['resourceVersion'] = '62'
        for serving in (self.current_serving['Deployment'], *(
                row for row in self.state.current.values() if row['kind'] == 'Deployment')):
            serving['metadata']['generation'] = 2
            replicas = serving['spec'].get('replicas', 1)
            serving['status'] = {'observedGeneration': 2, **{name: replicas for name in
                ('replicas', 'updatedReplicas', 'readyReplicas', 'availableReplicas')}}
        job, script = materializer.resolve(self.resources, versioned_required=True)
        for index, item in enumerate((job, script), 10):
            live = self.state.current[item['metadata']['name']] = copy.deepcopy(item)
            live['metadata']['uid'] = uuid(index)
        self.state.current[job['metadata']['name']]['status'] = {
            'succeeded': 1, 'conditions': [{'type': 'Complete', 'status': 'True'}]}
        controller_defaults(self.state)

    def cas(self, desired, actual):
        live = self.state.current['kodex-installation-profile']
        self.assertEqual(actual, live)
        self.assertEqual(custody.canonical_digest(live['data']), self.plan['before_installation_sha256'])
        live['data'] = copy.deepcopy(desired['data'])
        live['metadata']['resourceVersion'] = '25'

    def verify(self, phase='begin'):
        return upgrade.verify_apply(self.state, self.resources, self.receipt,
                                    adoption_dossier=self.dossier, adoption_phase=phase)

    def test_exact_capsule_pins_all_original_jobs_and_complete_render(self):
        directory = self.fixture.root / 'capsule'
        digest = rollout.write_capsule(directory, self.resources, self.receipt)
        observed = completion.load(directory / 'receipt.json', digest, True)
        self.assertEqual(observed['jobs'], self.fixture.receipt['jobs'])
        raw = completion.private_bytes(directory / 'render.yaml', 8 << 20)
        completion.verify_render_pin(observed, completion.digest(raw))
        with self.assertRaisesRegex(Rejected, 'UPGRADE_PREPARED_RENDER_DRIFT'):
            completion.verify_render_pin(observed, completion.digest(raw + b'\n'))
        with self.assertRaises(FileExistsError):
            rollout.write_capsule(directory, self.resources, self.receipt)
        with self.assertRaises(Rejected):
            completion.load(directory / 'receipt.json', digest)

    def test_capsule_entry_repeats_preflight_and_rejects_changed_saved_plan(self):
        system_run = subprocess.run

        def execute(argv, **options):
            if argv[0] == 'node':
                return self.fixture.execute(argv, **options)
            return system_run(argv, **options)

        with patch.object(adoption, 'State', return_value=self.state), \
                patch.object(self.state, 'discover', create=True), patch.object(custody.subprocess, 'run', side_effect=execute):
            plan = adoption.run(self.args)
            self.args.verified_plan_sha256 = completion.digest(json.dumps(plan).encode())
            resources, receipt = adoption.run(self.args, verified_plan=plan)
            self.assertEqual(resources, self.resources)
            self.assertEqual(receipt['jobs'], self.fixture.receipt['jobs'])
            self.assertEqual(receipt['evidence']['plan_sha256'], self.args.verified_plan_sha256)
            with self.assertRaisesRegex(Rejected, 'MIGRATED_ROLLOUT_PLAN_CHANGED'):
                adoption.run(self.args, verified_plan={**plan, 'before_installation_resource_version': '20'})

    def test_resumes_before_and_exact_partial_and_commits_only_after_all_checks(self):
        self.assertEqual(self.verify()['status'], 'MIGRATED_ROLLOUT_VERIFIED')
        self.current_serving['Service']['spec'] = copy.deepcopy(self.service['spec'])
        self.assertEqual(self.verify()['status'], 'MIGRATED_ROLLOUT_VERIFIED')
        self.state.cas_update = lambda *_: self.fail('CAS happened before final readiness')
        with self.assertRaises(Rejected):
            self.verify('commit')
        with self.assertRaises(Rejected):
            self.verify('readback')
        self.ready()
        self.current_serving['Deployment']['status']['readyReplicas'] = 1
        with self.assertRaisesRegex(Rejected, 'LIVE_WORKLOAD_NOT_READY'):
            self.verify('commit')
        self.current_serving['Deployment']['status']['readyReplicas'] = 2
        self.state.current['synthetic-staff']['status']['availableReplicas'] = 1
        with self.assertRaisesRegex(Rejected, 'LIVE_WORKLOAD_NOT_READY'):
            self.verify('commit')
        self.state.current['synthetic-staff']['status']['availableReplicas'] = 2
        with patch.object(self.state, 'cas_update', side_effect=self.cas) as cas:
            self.assertEqual(self.verify('commit')['status'], 'MIGRATED_ROLLOUT_COMMITTED')
            self.assertEqual(self.verify()['status'], 'MIGRATED_ROLLOUT_COMMITTED')
            self.assertEqual(self.verify('commit')['status'], 'MIGRATED_ROLLOUT_COMMITTED')
            self.assertEqual(self.verify('readback')['status'], 'MIGRATED_ROLLOUT_COMMITTED')
        self.assertEqual(cas.call_count, 1)

    def test_readiness_matches_generation_replicas_and_stateful_revision(self):
        deployment = {'kind': 'Deployment', 'metadata': {'generation': 2}, 'spec': {'replicas': 0},
                      'status': {'observedGeneration': 2}}
        daemon = {'kind': 'DaemonSet', 'metadata': {'generation': 2}, 'spec': {},
                  'status': {'desiredNumberScheduled': 1, 'observedGeneration': 2, 'currentNumberScheduled': 1,
                             'updatedNumberScheduled': 1, 'numberReady': 1, 'numberAvailable': 1}}
        stateful = {'kind': 'StatefulSet', 'metadata': {'generation': 2}, 'spec': {},
                    'status': {'observedGeneration': 2, 'replicas': 1, 'updatedReplicas': 1, 'readyReplicas': 1,
                               'availableReplicas': 1, 'currentRevision': 'new', 'updateRevision': 'new'}}
        for controller, field, value in ((deployment, 'observedGeneration', 1),
                                         (daemon, 'numberMisscheduled', 1),
                                         (stateful, 'currentRevision', 'old')):
            with self.subTest(kind=controller['kind']):
                verify_workload_ready(controller)
                controller['status'][field] = value
                with self.assertRaisesRegex(Rejected, 'LIVE_WORKLOAD_NOT_READY'):
                    verify_workload_ready(controller)

    def test_late_workload_failure_during_native_proof_refuses_cas(self):
        self.ready()
        self.state.cas_update = lambda *_: self.fail('CAS passed failing workload')

        def readiness_lost(*arguments):
            proof = self.continuity(*arguments)
            self.state.current['synthetic-staff']['status']['availableReplicas'] = 0
            return proof

        with patch.object(rollout, 'read_continuity', side_effect=readiness_lost), \
                self.assertRaisesRegex(Rejected, 'LIVE_WORKLOAD_NOT_READY'):
            self.verify('commit')

    def test_lost_ack_is_read_back_and_never_replayed(self):
        self.ready()

        def apply_with_lost_ack(desired, before):
            self.cas(desired, before)
            raise subprocess.TimeoutExpired('synthetic-kubectl', 1)

        self.state.cas_update = apply_with_lost_ack
        with patch.object(self.state, 'cas_update', wraps=apply_with_lost_ack) as cas:
            self.assertEqual(self.verify('commit')['status'], 'MIGRATED_ROLLOUT_COMMITTED')
        self.assertEqual(cas.call_count, 1)

    def test_unknown_cas_outcome_and_serving_drift_refuse_mutation(self):
        self.ready()
        self.state.cas_update = self.cas
        with patch.object(self.state, 'cas_update', side_effect=Rejected('SYNTHETIC_COMMIT_REJECTED')) as cas:
            with self.assertRaisesRegex(Rejected, 'MIGRATED_ROLLOUT_COMMIT_UNKNOWN'):
                self.verify('commit')
        self.assertEqual(cas.call_count, 1)
        self.current_serving['Deployment']['spec']['replicas'] = 7
        with self.assertRaisesRegex(Rejected, 'MIGRATED_ROLLOUT_SERVING_UNKNOWN'):
            self.verify('begin')

    def test_terminal_pins_reject_new_cluster_replaced_job_and_storage_drift(self):
        self.ready()
        self.state.cas_update = lambda *_: self.fail('unexpected write')
        self.fixture.witness['storageSHA256'] = '9' * 64
        with self.assertRaisesRegex(Rejected, 'MIGRATED_ROLLOUT_CUSTODY_CHANGED'):
            self.verify()
        self.fixture.witness['storageSHA256'] = '4' * 64
        self.state.current[completion.JOBS[0]]['metadata']['uid'] = uuid(999)
        controller_defaults(self.state)
        with self.assertRaisesRegex(Rejected, 'COMPLETION_JOB_IDENTITY_DRIFT'):
            self.verify()
        self.state.namespace_uid = uuid(998)
        with self.assertRaisesRegex(Rejected, 'COMPLETION_CLUSTER_IDENTITY_MISMATCH'):
            self.verify()


if __name__ == '__main__':
    unittest.main()
