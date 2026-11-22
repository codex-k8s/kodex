"""Допуск и фиксация принятия мигрированной установки без повторения bootstrap."""
import json
import re
import subprocess

import authority_registry as authority
import bootstrap_completion as completion
import bootstrap_migration_custody as custody
from bootstrap_upgrade import installation, render_bytes, write_new
from deployment_profile import require, Rejected
from portable_state import key, owner, verify_resource, verify_resource_set, verify_workload_ready
import release_materializer as materializer

ORIGIN = completion.ADOPTION_ORIGIN
CONTINUITY_SCHEMA = 'kodex.component-migration-continuity-readback/v1'
CONTINUITY_FIELDS = {
    'installationResourceVersion', 'serviceUID', 'serviceResourceVersion', 'serviceSpecSHA256',
    'serviceAnnotationsSHA256', 'deploymentUID', 'deploymentResourceVersion', 'deploymentSpecSHA256',
    'beforeApiDeploymentSpecSHA256', 'beforeApiServiceSpecSHA256', 'beforeApiServiceAnnotationsSHA256',
    'durableAuthorityIdentitySHA256',
}
STABLE_CUSTODY_FIELDS = {
    'context', 'clusterUID', 'namespaceUID', 'installationUID', 'migrationSourceSHA', 'schemaHeads',
    'archiveTerminalSHA256', 'archiveExecutionSHA256', 'archiveJobUID', 'archivePodUID',
    'authorityIdentitySHA256', 'schemaCatalogSHA256', 'storageSHA256', 'dossierSHA256',
}
EVIDENCE_PINS = {
    'plan_sha256', 'prepared_render_sha256', 'dossier_sha256', 'continuity_sha256',
    'before_installation_sha256', 'after_installation_sha256', 'archive_receipt_sha256',
}


def validate_evidence(receipt):
    """Проверяет закрытый набор pins, сохраняющий подлинные времена пяти bootstrap effects."""
    evidence = receipt['evidence']
    require(isinstance(evidence, dict) and set(evidence) == EVIDENCE_PINS | {
        'installation_uid', 'before_installation_resource_version',
    } and all(completion.hex_value(evidence[name]) for name in EVIDENCE_PINS)
        and completion.uuid_value(evidence['installation_uid'])
        and isinstance(evidence['before_installation_resource_version'], str)
        and re.fullmatch(r'[0-9]+', evidence['before_installation_resource_version']),
        'MIGRATED_ROLLOUT_EVIDENCE_REJECTED')
    return evidence


def read_continuity(state, dossier, digest):
    """Читает только зарегистрированный producer на точном target с частным dossier."""
    return custody.MigrationWitnessProducer(dossier, digest).read_continuity(
        state, CONTINUITY_SCHEMA, custody.WITNESS_FIELDS | CONTINUITY_FIELDS).value()


def observed(state, desired, uid, version):
    """Связывает native bracket с тем же owner resource для проверки точного spec."""
    value = state.get(desired)
    require(value is not None and key(value) == key(desired) and owner(value) and
            value['metadata'].get('uid') == uid and value['metadata'].get('resourceVersion') == version and
            not value['metadata'].get('deletionTimestamp'), 'MIGRATED_ROLLOUT_READBACK_DRIFT')
    return value


def matches(desired, actual):
    """Использует штатную нормализацию API defaults при проверке принятого render."""
    try:
        verify_resource(desired, actual)
    except Rejected:
        return False
    return True


def classify(state, resources, receipt, evidence, proof, *, require_ready=False):
    """Допускает только прежнее либо точное целевое состояние по каждой serving identity."""
    require(proof['context'] == state.context and proof['installationUID'] == evidence['installation_uid'] and
            proof['clusterUID'] == receipt['cluster_uid'] and proof['namespaceUID'] == receipt['namespace_uid'] and
            proof['schemaHeads'] == custody.declaration(installation(resources))['schema_heads'],
            'MIGRATED_ROLLOUT_CONTINUITY_TARGET_DRIFT')
    config = custody.installation_resource(resources)
    live = observed(state, config, proof['installationUID'], proof['installationResourceVersion'])
    actual_hash = custody.canonical_digest(live['data'])
    require(actual_hash == proof['installationSHA256'], 'MIGRATED_ROLLOUT_INSTALLATION_DRIFT')
    before = actual_hash == evidence['before_installation_sha256'] and (
        live['metadata']['resourceVersion'] == evidence['before_installation_resource_version'])
    committed = actual_hash == evidence['after_installation_sha256'] and matches(config, live)
    require(before != committed, 'MIGRATED_ROLLOUT_INSTALLATION_UNKNOWN')

    service = authority.one(resources, 'Service', 'control-api-gateway')
    deployment = authority.one(resources, 'Deployment', 'control-api-gateway')
    live_service = observed(state, service, proof['serviceUID'], proof['serviceResourceVersion'])
    live_deployment = observed(state, deployment, proof['deploymentUID'], proof['deploymentResourceVersion'])
    require(custody.canonical_digest(live_service['spec']) == proof['serviceSpecSHA256'] and
            custody.canonical_digest(live_service['metadata'].get('annotations', {})) == proof['serviceAnnotationsSHA256'] and
            custody.canonical_digest(live_deployment['spec']) == proof['deploymentSpecSHA256'],
            'MIGRATED_ROLLOUT_SERVING_READBACK_DRIFT')
    service_before = (proof['serviceSpecSHA256'] == proof['beforeApiServiceSpecSHA256'] and
                      proof['serviceAnnotationsSHA256'] == proof['beforeApiServiceAnnotationsSHA256'])
    deployment_before = proof['deploymentSpecSHA256'] == proof['beforeApiDeploymentSpecSHA256']
    service_after, deployment_after = matches(service, live_service), matches(deployment, live_deployment)
    require((service_before or service_after) and (deployment_before or deployment_after),
            'MIGRATED_ROLLOUT_SERVING_UNKNOWN')
    ready = service_after and deployment_after
    require(not committed or ready, 'MIGRATED_ROLLOUT_COMMITTED_SERVING_DRIFT')
    if require_ready:
        require(ready and deployment['spec'].get('replicas', 1) > 0, 'MIGRATED_ROLLOUT_SERVING_NOT_READY')
        verify_workload_ready(live_deployment)
    return live, committed


def capture(state, resources, original, plan, plan_sha, custody_witness, dossier):
    """Материализует допуск только после свежего независимого чтения исходного состояния."""
    proof = read_continuity(state, dossier, plan['dossier_sha256'])
    require(all(proof[name] == custody_witness[name] for name in STABLE_CUSTODY_FIELDS) and
            proof['deploymentSpecSHA256'] == proof['beforeApiDeploymentSpecSHA256'] and
            proof['serviceSpecSHA256'] == proof['beforeApiServiceSpecSHA256'] and
            proof['serviceAnnotationsSHA256'] == proof['beforeApiServiceAnnotationsSHA256'],
            'MIGRATED_ROLLOUT_CAPTURE_CHANGED')
    evidence = {name: plan[name] for name in ('prepared_render_sha256', 'dossier_sha256', 'installation_uid',
        'before_installation_sha256', 'before_installation_resource_version', 'after_installation_sha256',
        'archive_receipt_sha256')}
    evidence['plan_sha256'] = plan_sha
    evidence['continuity_sha256'] = continuity_seal(proof)
    receipt = completion.envelope(state, original['jobs'], ORIGIN, evidence)
    _, committed = classify(state, resources, receipt, validate_evidence(receipt), proof)
    require(not committed, 'MIGRATED_ROLLOUT_CAPTURE_ALREADY_COMMITTED')
    completion.verify_jobs(state, resources, receipt)
    return receipt


def continuity_seal(proof):
    """Закрепляет архив, owner identities, schema и durable storage независимо от стадии rollout."""
    fields = STABLE_CUSTODY_FIELDS - {'authorityIdentitySHA256'} | {
        'durableAuthorityIdentitySHA256', 'serviceUID', 'deploymentUID', 'beforeApiDeploymentSpecSHA256',
        'beforeApiServiceSpecSHA256', 'beforeApiServiceAnnotationsSHA256',
    }
    return custody.canonical_digest({name: proof[name] for name in fields})


def verify_apply(state, resources, receipt, dossier, phase):
    """Проверяет begin/readback и один раз фиксирует готовую установку JSON CAS-переходом."""
    evidence = validate_evidence(receipt)
    require(phase in ('begin', 'commit', 'readback') and dossier is not None and
            state.kubeconfig is not None, 'MIGRATED_ROLLOUT_EXPLICIT_SCOPE_REQUIRED')
    completion.verify_render_pin(receipt, completion.digest(render_bytes(resources)))
    require(evidence['after_installation_sha256'] == custody.canonical_digest(installation(resources)) and
            evidence['after_installation_sha256'] != evidence['before_installation_sha256'],
            'MIGRATED_ROLLOUT_TARGET_DRIFT')
    authority.verify_upgrade_preconditions(state, resources, versioned_materializer=True)
    proof = read_continuity(state, dossier, evidence['dossier_sha256'])
    require(continuity_seal(proof) == evidence['continuity_sha256'], 'MIGRATED_ROLLOUT_CUSTODY_CHANGED')
    if phase != 'begin':
        # Долгая сверка БД и архива завершена. Теперь проверяем актуальный render перед CAS.
        verify_resource_set(state, resources, receipt, deferred_installation=phase == 'commit',
                            require_workload_readiness=True)
        job, _ = materializer.resolve(resources, versioned_required=True)
        actual = state.get(job)
        status = actual.get('status', {}) if actual is not None else {}
        require(completion.successful({'status': status}) and not status.get('terminating') and
                [entry['type'] for entry in status.get('conditions', []) if entry.get('type') in ('Complete', 'Failed')
                 and entry.get('status') == 'True'] == ['Complete'], 'MIGRATED_ROLLOUT_MATERIALIZER_NOT_COMPLETE')
        authority.verify_upgrade_preconditions(state, resources, versioned_materializer=True)
    live, committed = classify(state, resources, receipt, evidence, proof, require_ready=phase != 'begin')
    completion.verify_jobs(state, resources, receipt)
    if phase == 'readback':
        require(committed, 'MIGRATED_ROLLOUT_COMMIT_REQUIRED')
    if phase == 'commit' and not committed:
        config = custody.installation_resource(resources)
        # CAS отвечает и за случай потерянного ACK. Не повторяем эффект без readback.
        try:
            state.cas_update(config, live)
        except (Rejected, subprocess.SubprocessError):
            try:
                return verify_apply(state, resources, receipt, dossier, 'readback')
            except (Rejected, subprocess.SubprocessError):
                raise Rejected('MIGRATED_ROLLOUT_COMMIT_UNKNOWN') from None
        return verify_apply(state, resources, receipt, dossier, 'readback')
    return {'status': 'MIGRATED_ROLLOUT_COMMITTED' if committed else 'MIGRATED_ROLLOUT_VERIFIED',
            'phase': phase, 'effects_replayed': False}


def write_capsule(directory, resources, receipt):
    """Создаёт private capsule; потребитель проверяет полный digest render и receipt."""
    require(directory.is_absolute() and directory.parent.resolve() == directory.parent and
            directory.parent.stat().st_mode & 0o077 == 0, 'MIGRATED_ROLLOUT_PRIVATE_DIRECTORY_REQUIRED')
    directory.mkdir(mode=0o700)
    raw = (json.dumps(receipt, sort_keys=True) + '\n').encode()
    write_new(directory / 'render.yaml', render_bytes(resources))
    write_new(directory / 'receipt.json', raw)
    digest = completion.digest(raw)
    completion.load(directory / 'receipt.json', digest, True)
    return digest
