"""Проверяет будущий render мигрированной установки и сохраняет только read-only план."""
import argparse
import copy
import json
from pathlib import Path
import re
import subprocess
from tempfile import TemporaryDirectory

import yaml

import authority_registry as authority
import bootstrap_completion as completion
import bootstrap_migration_custody as custody
import bootstrap_upgrade as upgrade
from deployment_profile import read_render_profile, require, Rejected
from portable_state import State
import release_materializer as materializer


SCHEMA = 'kodex.bootstrap-migration-adoption-plan/v1'
LOCK_VALIDATOR = Path(__file__).resolve().parents[1] / 'release' / 'validate-release-lock.sh'


def prepare_target(archive, target, prior):
    """Сохраняет исходные Job specs и связывает новый materializer с target lock."""
    archived_installation = custody.installation_resource(archive)
    target_installation = custody.installation_resource(target)
    read_render_profile(target)
    archived = custody.declaration(archived_installation['data'])
    future = custody.declaration(target_installation['data'])
    require(archived == prior['archived_declaration'], 'MIGRATED_ADOPTION_ARCHIVE_CHANGED')
    require(future['schema_heads'] == prior['actual_schema_heads'] and
            future['schema_heads']['control-plane'] == upgrade.WORKFLOW_FILE_PROVENANCE_SCHEMA,
            'MIGRATED_ADOPTION_TARGET_SCHEMA_CHANGED')
    require(future['source_sha'] not in (archived['source_sha'], prior['live_declaration']['source_sha']) and
            future['release_lock_sha256'] not in (archived['release_lock_sha256'], prior['live_declaration']['release_lock_sha256']),
            'MIGRATED_ADOPTION_NEW_RELEASE_REQUIRED')
    old, new = completion.desired_jobs(archive), completion.desired_jobs(target)
    seals = {row['name']: row['desired_sha256'] for row in prior['jobs']}
    require(seals == {name: completion.fingerprint(job) for name, job in old.items()},
            'MIGRATED_ADOPTION_BOOTSTRAP_LINEAGE_CHANGED')
    for name in completion.JOBS:
        upgrade.immutable_job(old[name])
        upgrade.immutable_job(new[name])
        before, after = map(upgrade.without_images, (old[name], new[name]))
        if name == upgrade.MIGRATION:
            for job in (before, after):
                backoff = job['spec']['backoffLimit']
                require(type(backoff) is int and backoff in (0, 2), 'MIGRATED_ADOPTION_MIGRATION_CONTRACT_REJECTED')
                job['spec']['backoffLimit'] = 0
        require(before == after, 'MIGRATED_ADOPTION_BOOTSTRAP_EFFECT_CHANGED')
    prepared = [copy.deepcopy(old[row['metadata']['name']]) if row['kind'] == 'Job' and
                row['metadata']['name'] in old else copy.deepcopy(row) for row in target]
    materializer.version(prepared)
    return prepared


def verify_release_lock(path, prepared, job):
    """Использует штатный валидатор для защищённого снимка полного OCI lock."""
    raw = completion.private_bytes(path, 1 << 20)
    installation = custody.declaration(upgrade.installation(prepared))
    lock = json.loads(raw, object_pairs_hook=completion.unique_object)
    require(isinstance(lock, dict) and completion.digest(raw) == installation['release_lock_sha256'] and
            lock.get('schema_heads') == installation['schema_heads'] and
            lock.get('role_base_runtime_engine') == 'opencode', 'MIGRATED_ADOPTION_RELEASE_LOCK_DRIFT')
    with TemporaryDirectory(prefix='kodex-adoption-lock-') as directory:
        snapshot = Path(directory) / 'release-lock.json'
        upgrade.write_new(snapshot, raw)
        checked = subprocess.run([str(LOCK_VALIDATOR), '--lock', str(snapshot), '--sha256', installation['release_lock_sha256'],
            '--source-sha', installation['source_sha'], '--profile', lock['profile']], capture_output=True, timeout=10)
    require(checked.returncode == 0, 'MIGRATED_ADOPTION_RELEASE_LOCK_REJECTED')
    digest = next((entry['digest'] for entry in lock['images'] if entry['component'] == 'image-admission'), None)
    require(digest is not None and job['spec']['template']['spec']['containers'][0]['image'].endswith('@' + digest),
            'MIGRATED_ADOPTION_MATERIALIZER_IMAGE_DRIFT')


def run(args, *, verified_plan=None):
    """Сверяет все закреплённые bytes до discovery и повторяет наблюдение перед результатом."""
    require(re.fullmatch(r'[A-Za-z0-9_.:@/-]{1,160}', args.context),
            'MIGRATED_ADOPTION_EXPLICIT_TARGET_REQUIRED')
    original, archive, archived_installation = custody.load_original(
        args.archive_receipt, args.archive_receipt_sha256, args.archive_render, args.archive_render_sha256)
    prior = completion.pinned_json(args.expected_custody, args.expected_custody_sha256)
    raw = completion.private_bytes(args.target_render, 8 << 20)
    require(completion.hex_value(args.target_render_sha256) and
            completion.digest(raw) == args.target_render_sha256, 'MIGRATED_ADOPTION_TARGET_RENDER_DRIFT')
    require(prior['schema'] == custody.SCHEMA and prior['context'] == args.context and
            prior['previous_receipt_sha256'] == args.archive_receipt_sha256 and
            prior['previous_render_sha256'] == args.archive_render_sha256 and
            prior['jobs'] == original['jobs'] and prior['apply_granted'] is False and
            prior['migration_witness']['dossierSHA256'] == args.dossier_sha256 and
            completion.hex_value(args.dossier_sha256) and args.dossier.is_absolute() and
            completion.digest(completion.private_bytes(args.dossier)) == args.dossier_sha256,
            'MIGRATED_ADOPTION_CUSTODY_PIN_MISMATCH')
    prepared = prepare_target(archive, upgrade.decode_render(raw), prior)
    job, script = materializer.resolve(prepared, versioned_required=True)
    verify_release_lock(args.release_lock, prepared, job)
    state = State(args.context, args.kubeconfig)
    scoped = [archived_installation, *completion.desired_jobs(archive).values(), job, script,
                    authority.one(prepared, 'ConfigMap', authority.REGISTRY),
                    authority.one(prepared, 'Deployment', authority.PUBLISHER)]
    if verified_plan is not None:
        scoped.extend(authority.one(prepared, kind, 'control-api-gateway') for kind in ('Service', 'Deployment'))
    state.discover(scoped)
    installation = custody.live_installation(state, archived_installation)
    authority.verify_upgrade_preconditions(state, prepared, versioned_materializer=True)
    witness = custody.capture(state, previous_receipt_path=args.archive_receipt,
                              previous_receipt_sha256=args.archive_receipt_sha256,
                              previous_render_path=args.archive_render,
                              previous_render_sha256=args.archive_render_sha256,
                              witness_producer=custody.MigrationWitnessProducer(args.dossier, args.dossier_sha256))
    custody.verify_observation(prior, witness)
    authority.verify_upgrade_preconditions(state, prepared, versioned_materializer=True)
    completion.verify_jobs(state, archive, original)
    require(witness['installation_uid'] == installation['uid'] and
            witness['installation_sha256'] == installation['data_sha256'] and
            custody.live_installation(state, archived_installation) == installation,
            'MIGRATED_ADOPTION_INSTALLATION_CHANGED')
    result = {'schema': SCHEMA, 'status': 'MIGRATED_INSTALLATION_PREFLIGHT_VERIFIED',
            'context': state.context,
            'cluster_uid': witness['cluster_uid'], 'namespace_uid': witness['namespace_uid'],
            'installation_uid': witness['installation_uid'], 'before_installation_sha256': witness['installation_sha256'],
            'before_installation_resource_version': installation['resource_version'],
            'after_installation_sha256': custody.canonical_digest(upgrade.installation(prepared)),
            'target': custody.declaration(upgrade.installation(prepared)),
            'custody_sha256': args.expected_custody_sha256, 'target_render_sha256': args.target_render_sha256,
            'archive_render_sha256': args.archive_render_sha256, 'archive_receipt_sha256': args.archive_receipt_sha256,
            'dossier_sha256': args.dossier_sha256,
            'prepared_render_sha256': completion.digest(upgrade.render_bytes(prepared)),
            'materializer': job['metadata']['name'],
            'effects_replayed': False, 'apply_granted': False}
    if verified_plan is None:
        return result
    require(verified_plan == result, 'MIGRATED_ROLLOUT_PLAN_CHANGED')
    import bootstrap_migration_rollout as rollout
    receipt = rollout.capture(state, prepared, original, result, args.verified_plan_sha256,
                              witness['migration_witness'], args.dossier)
    return prepared, receipt


def main():
    """Пишет приватный план либо закреплённую квитанцию и render без изменения кластера."""
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('--context', required=True)
    parser.add_argument('--kubeconfig', type=Path, required=True)
    for flag in ('archive-render', 'archive-receipt', 'expected-custody', 'target-render', 'dossier'):
        parser.add_argument('--' + flag, type=Path, required=True)
        parser.add_argument('--' + flag + '-sha256', required=True)
    parser.add_argument('--release-lock', type=Path, required=True)
    outputs = parser.add_mutually_exclusive_group(required=True)
    outputs.add_argument('--output', type=Path)
    outputs.add_argument('--capsule-directory', type=Path)
    parser.add_argument('--verified-plan', type=Path)
    parser.add_argument('--verified-plan-sha256')
    args = parser.parse_args()
    try:
        require((args.capsule_directory is not None) == (args.verified_plan is not None and args.verified_plan_sha256 is not None)
                and ((args.verified_plan is not None) == (args.verified_plan_sha256 is not None)),
                'MIGRATED_ROLLOUT_PLAN_REQUIRED')
        if args.capsule_directory is not None:
            import bootstrap_migration_rollout as rollout
            verified_plan = completion.pinned_json(args.verified_plan, args.verified_plan_sha256, 16384)
            require(isinstance(verified_plan, dict) and verified_plan.get('schema') == SCHEMA,
                    'MIGRATED_ROLLOUT_PLAN_REQUIRED')
            resources, receipt = run(args, verified_plan=verified_plan)
            digest = rollout.write_capsule(args.capsule_directory, resources, receipt)
            print(json.dumps({'status': 'MIGRATED_ROLLOUT_CAPSULE_CAPTURED',
                'receipt_sha256': digest, 'render_sha256': receipt['evidence']['prepared_render_sha256']}))
            return
        result = run(args)
        raw = (json.dumps(result, sort_keys=True, ensure_ascii=False) + '\n').encode()
        upgrade.write_new(args.output, raw)
        require(completion.private_bytes(args.output) == raw, 'MIGRATED_ADOPTION_PLAN_READBACK_REJECTED')
        print(json.dumps({'status': result['status'], 'sha256': completion.digest(raw), 'apply_granted': False}))
    except (Rejected, ValueError, TypeError, KeyError, OSError, subprocess.SubprocessError, yaml.YAMLError):
        raise SystemExit('Migrated installation preflight rejected; no writes to Kubernetes') from None


if __name__ == '__main__':
    main()
