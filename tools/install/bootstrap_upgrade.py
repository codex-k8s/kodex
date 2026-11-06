"""Явный forward032→033 profile: прежние bootstrap effects сохраняются, новая миграция подтверждается отдельно.

prepare не обращается к кластеру. capture выполняет только чтение реального Job/Pod
и schema catalog; этот модуль не запускает миграцию, не удаляет Jobs и не создаёт
поддельный Kubernetes status. Применение остаётся обязанностью operator lifecycle.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

import yaml
import bootstrap_completion as completion
from deployment_profile import require, Rejected
from portable_state import State, seal, verify_resource, verify_schemas, normalized_pod

SCHEMA = 'kodex.bootstrap-upgrade/v1'
ORIGIN = 'FORWARD_MIGRATION_UPGRADE'
MIGRATION = 'control-plane-migrate'
PRESERVED = tuple(name for name in completion.JOBS if name != MIGRATION)


def sha(raw):
    """Закрепляет точные bytes input/output без чтения environment или credential backend."""
    return hashlib.sha256(raw).hexdigest()


def validate(profile):
    """Закрывает profile на один известный forward contract и два явно закреплённых render/source."""
    fields = {'schema', 'managed_by', 'previous_render_sha256', 'target_render_sha256',
              'previous_source_sha', 'target_source_sha', 'from_schema_heads', 'to_schema_heads', 'schema_profile'}
    require(isinstance(profile, dict) and set(profile) == fields and profile['schema'] == SCHEMA and
            profile['managed_by'] == 'git' and profile['schema_profile'] in ('plain', 'backup'), 'UPGRADE_PROFILE_REJECTED')
    for field in ('previous_render_sha256', 'target_render_sha256', 'previous_source_sha', 'target_source_sha'):
        length = 40 if field.endswith('_sha') else 64
        require(completion.hex_value(profile[field], length) and profile[field] != '0' * length, 'UPGRADE_PIN_REJECTED')
    require(profile['previous_source_sha'] != profile['target_source_sha'], 'UPGRADE_SOURCE_UNCHANGED')
    before, after = profile['from_schema_heads'], profile['to_schema_heads']
    keys = {'control-plane', 'internal-rpc-authority', 'email-bridge'}
    require(isinstance(before, dict) and isinstance(after, dict) and set(before) == set(after) == keys and
            all(type(v) is int and v > 0 for heads in (before, after) for v in heads.values()), 'UPGRADE_SCHEMA_PINS_REJECTED')
    require(before['control-plane'] == 20261030000100 and after['control-plane'] == 20261105000100 and
            all(before[key] == after[key] for key in keys - {'control-plane'}), 'UPGRADE_SCHEMA_PATH_REJECTED')
    return copy.deepcopy(profile)


def installation(resources):
    """Читает единственный namespaced deployment profile; caller payload не выбирает cluster или tenant."""
    rows = [r for r in resources if r.get('kind') == 'ConfigMap' and
            r.get('metadata', {}).get('namespace') == 'kodex-system' and r['metadata'].get('name') == 'kodex-installation-profile']
    require(len(rows) == 1, 'UPGRADE_INSTALLATION_PROFILE_REQUIRED')
    return rows[0]['data']


def without_images(job):
    """Разрешает для уже исполненного bootstrap только различие в immutable executable image."""
    value = copy.deepcopy(job)
    for field in ('containers', 'initContainers'):
        for container in value['spec']['template']['spec'].get(field, []):
            container.pop('image', None)
    return value


def migration_contract(job):
    """Capture не принимает check/произвольную команду вместо единственного штатного forward-up."""
    spec = job['spec']; pod = spec['template']['spec']; containers = pod.get('containers', [])
    require(spec.get('backoffLimit') == 0 and spec.get('activeDeadlineSeconds') == 300 and
            spec.get('parallelism', 1) == 1 and spec.get('completions', 1) == 1 and
            not any(k in spec for k in ('successPolicy', 'podFailurePolicy', 'manualSelector')) and
            pod.get('serviceAccountName') == 'control-plane-migrator' and pod.get('automountServiceAccountToken') is False and
            pod.get('restartPolicy') == 'Never' and len(containers) == 1, 'UPGRADE_MIGRATION_CONTRACT_REJECTED')
    container = containers[0]
    environment = [{'name': 'CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE',
                    'value': '/var/run/secrets/kodex/control-plane/postgres-migration/dsn'}]
    environments = (environment, environment + [{'name': 'OTEL_SDK_DISABLED', 'value': 'true'}])
    require(container.get('name') == 'migrate' and container.get('command') == ['/usr/local/bin/control-plane-cli'] and
            container.get('args') == ['up'] and container.get('env') in environments and not container.get('envFrom') and
            re.fullmatch(r'[^\s@]+@sha256:[a-f0-9]{64}', container.get('image', '')),
            'UPGRADE_MIGRATION_COMMAND_REJECTED')


def immutable_job(job):
    """Сохранение исторического effect не разрешает mutable либо незаполненные executable refs."""
    pod = job['spec']['template']['spec']
    for container in pod.get('containers', []) + pod.get('initContainers', []):
        image = container.get('image', '')
        require(re.fullmatch(r'[^\s@]+@sha256:[a-f0-9]{64}', image) and not image.endswith('0' * 64),
                'UPGRADE_IMMUTABLE_BOOTSTRAP_IMAGE_REQUIRED')


def decode_render(raw):
    """Читает bounded render; Secret seeds исключены, как в штатном apply, и никогда не переносятся в upgrade."""
    require(0 < len(raw) <= 8 << 20, 'UPGRADE_RENDER_SIZE_REJECTED')
    resources = [r for r in yaml.safe_load_all(raw) if r is not None]
    require(0 < len(resources) <= 2000 and all(isinstance(r, dict) for r in resources),
            'UPGRADE_RENDER_REJECTED')
    return [r for r in resources if r.get('kind') != 'Secret']


def prepare(previous_raw, target_raw, profile):
    """Сохраняет четыре неизменных bootstrap Job и оставляет только новую CP migration с backoffLimit=0."""
    profile = validate(profile)
    require(sha(previous_raw) == profile['previous_render_sha256'] and sha(target_raw) == profile['target_render_sha256'],
            'UPGRADE_RENDER_PIN_MISMATCH')
    previous, target = decode_render(previous_raw), decode_render(target_raw)
    for resources, source, heads in ((previous, profile['previous_source_sha'], profile['from_schema_heads']),
                                     (target, profile['target_source_sha'], profile['to_schema_heads'])):
        data = installation(resources)
        require(data.get('source.sha') == source and json.loads(data['schema-heads.json']) == heads,
                'UPGRADE_INSTALLATION_PIN_MISMATCH')
    old, new = completion.desired_jobs(previous), completion.desired_jobs(target)
    for job in [*old.values(), *new.values()]: immutable_job(job)
    require(completion.fingerprint(old[MIGRATION]) != completion.fingerprint(new[MIGRATION]), 'UPGRADE_NEW_MIGRATION_REQUIRED')
    require(without_images(old[MIGRATION]) == without_images(new[MIGRATION]), 'UPGRADE_MIGRATION_EFFECT_CHANGED')
    for name in PRESERVED:
        require(without_images(old[name]) == without_images(new[name]), 'UPGRADE_BOOTSTRAP_EFFECT_CHANGED')
    result = copy.deepcopy(target)
    for index, resource in enumerate(result):
        if resource.get('kind') == 'Job' and resource['metadata']['name'] in PRESERVED:
            result[index] = copy.deepcopy(old[resource['metadata']['name']])
        elif resource.get('kind') == 'Job' and resource['metadata']['name'] == MIGRATION:
            resource['spec']['backoffLimit'] = 0
            migration_contract(resource)
    require(not any(r.get('kind') == 'ConfigMap' and r['metadata']['name'].startswith('kodex-bootstrap-upgrade-') for r in result),
            'UPGRADE_PROFILE_COLLISION')
    raw = json.dumps(profile, sort_keys=True, separators=(',', ':')).encode()
    result.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {
        'name': 'kodex-bootstrap-upgrade-' + sha(raw)[:24], 'namespace': 'kodex-system',
        'labels': {'app.kubernetes.io/part-of': 'kodex'}, 'annotations': {'kodex.dev/bootstrap-upgrade-sha256': sha(raw)}},
        'immutable': True, 'data': {'profile.json': raw.decode()}})
    return result


def render_profile(resources):
    """Проверяет immutable profile materialization до доверия operator receipt."""
    rows = [r for r in resources if r.get('kind') == 'ConfigMap' and
            r.get('metadata', {}).get('namespace') == 'kodex-system' and r['metadata']['name'].startswith('kodex-bootstrap-upgrade-')]
    require(len(rows) == 1, 'UPGRADE_PROFILE_REQUIRED')
    resource = rows[0]
    raw = resource['data']['profile.json'].encode()
    profile = validate(json.loads(raw, object_pairs_hook=completion.unique_object))
    require(resource.get('immutable') is True and resource['metadata']['name'] == 'kodex-bootstrap-upgrade-' + sha(raw)[:24] and
            resource['metadata'].get('annotations', {}).get('kodex.dev/bootstrap-upgrade-sha256') == sha(raw), 'UPGRADE_PROFILE_DRIFT')
    data = installation(resources)
    require(data['source.sha'] == profile['target_source_sha'] and json.loads(data['schema-heads.json']) == profile['to_schema_heads'],
            'UPGRADE_INSTALLATION_PIN_MISMATCH')
    return profile, sha(raw)


def schema_proof(state, mode):
    """Исполняет только fixed READ ONLY033 catalog proof и штатный exact validator; пароль/DSN не читаются."""
    directory = Path(__file__).resolve().parent.parent / 'release'
    require(mode in ('plain', 'backup'), 'UPGRADE_SCHEMA_PROFILE_REJECTED')
    query = (directory / ('platform-oidc-identity-' + ('backup-' if mode == 'backup' else '') + 'migration-postcondition.sql')).read_bytes()
    result = subprocess.run(['kubectl', '--context', state.context, '--request-timeout=15s', '-n', 'kodex-system',
                             'exec', '-i', 'kodex-postgresql-0', '--', 'psql', '-XqAt', '-U', 'postgres', '-d', 'control_plane',
                             '-v', 'ON_ERROR_STOP=1'], input=query, capture_output=True, timeout=25)
    require(result.returncode == 0 and len(result.stdout) <= 1 << 20, 'UPGRADE_SCHEMA_CATALOG_REJECTED')
    function = 'validPlatformOIDCIdentity' + ('Backup' if mode == 'backup' else '') + 'MigrationPostcondition'
    verifier = "import {" + function + " as valid} from './control-plane-immutable-migration.mjs';let raw='';for await(const chunk of process.stdin)raw+=chunk;if(!valid(JSON.parse(raw)))process.exit(1);"
    checked = subprocess.run(['node', '--input-type=module', '-e', verifier], cwd=directory,
                             input=result.stdout, capture_output=True, timeout=10)
    require(checked.returncode == 0, 'UPGRADE_SCHEMA_CATALOG_REJECTED')
    return sha(result.stdout)


def verify_apply(state, resources, receipt):
    """До первой rollout mutation проверяет genuine composite receipt, cluster/Jobs и полный033 catalog."""
    require(isinstance(receipt, dict) and receipt.get('origin') == ORIGIN, 'UPGRADE_COMPLETION_ORIGIN_REQUIRED')
    profile, _ = render_profile(resources)
    completion.verify(state, resources, receipt)
    proof = schema_proof(state, profile['schema_profile'])
    return {'status': 'FORWARD_UPGRADE_APPLY_VERIFIED', 'schema_proof_sha256': proof, 'effects_replayed': False}


def migration_execution(state, expected, actual):
    """Требует один настоящий terminal Job-owned Pod, неизменённый spec и фактический image digest."""
    pods = json.loads(state.call(['-n', 'kodex-system', 'get', 'pods', '-l', 'job-name=' + MIGRATION, '-o', 'json']))['items']
    require(len(pods) == 1, 'UPGRADE_MIGRATION_POD_REQUIRED')
    pod = pods[0]
    require(pod.get('metadata', {}).get('namespace') == 'kodex-system' and completion.uuid_value(pod['metadata'].get('uid')) and
            any(o.get('kind') == 'Job' and o.get('uid') == actual['metadata']['uid'] and o.get('controller') is True
                for o in pod['metadata'].get('ownerReferences', [])) and pod.get('status', {}).get('phase') == 'Succeeded',
            'UPGRADE_MIGRATION_EXECUTION_REJECTED')
    wanted = expected['spec']['template']['spec']
    observed = copy.deepcopy(pod['spec'])
    if 'nodeName' not in wanted:
        require(isinstance(observed.pop('nodeName', None), str), 'UPGRADE_MIGRATION_NODE_REQUIRED')
    if 'priority' not in wanted and observed.get('priority') == 0: observed.pop('priority')
    if 'tolerations' not in wanted:
        tolerations = observed.pop('tolerations', [])
        allowed = [{'key': 'node.kubernetes.io/' + key, 'operator': 'Exists', 'effect': 'NoExecute', 'tolerationSeconds': 300}
                   for key in ('not-ready', 'unreachable')]
        require(all(t in allowed for t in tolerations) and len(tolerations) <= 2, 'UPGRADE_MIGRATION_TOLERATION_DRIFT')
    require(normalized_pod(wanted, observed) == wanted, 'UPGRADE_MIGRATION_POD_SPEC_DRIFT')
    initial = pod['status'].get('initContainerStatuses', [])
    require({c['name'] for c in wanted.get('initContainers', [])} == {s.get('name') for s in initial} and
            len(initial) == len(wanted.get('initContainers', [])) and
            all(s.get('state', {}).get('terminated', {}).get('exitCode') == 0 for s in initial),
            'UPGRADE_MIGRATION_INIT_NOT_COMPLETED')
    containers = wanted['containers']
    statuses = pod['status'].get('containerStatuses', [])
    require(len(containers) == len(statuses) == 1 and containers[0]['name'] == statuses[0].get('name') == 'migrate' and
            re.fullmatch(r'[^\s@]+@sha256:[a-f0-9]{64}', containers[0]['image']) and
            statuses[0].get('imageID', '').split('@')[-1] == containers[0]['image'].split('@')[-1] and
            statuses[0].get('state', {}).get('terminated', {}).get('exitCode') == 0, 'UPGRADE_MIGRATION_IMAGE_NOT_EXECUTED')
    return pod['metadata']['uid'], containers[0]['image'].split('@')[-1]


def capture(state, resources, previous_resources, previous_receipt, previous_receipt_sha256):
    """Объединяет сохранённые четыре effects с независимо наблюдённой новой миграцией, не повторяя effects."""
    profile, profile_digest = render_profile(resources)
    require(completion.hex_value(previous_receipt_sha256) and
            installation(previous_resources).get('source.sha') == profile['previous_source_sha'] and
            json.loads(installation(previous_resources)['schema-heads.json']) == profile['from_schema_heads'],
            'UPGRADE_PREVIOUS_INSTALLATION_DRIFT')
    require(completion.identity(state) == {k: previous_receipt[k] for k in ('cluster_uid', 'namespace_uid')},
            'UPGRADE_CLUSTER_IDENTITY_MISMATCH')
    old, new = completion.desired_jobs(previous_resources), completion.desired_jobs(resources)
    rows = {r['name']: r for r in previous_receipt['jobs']}
    require(set(rows) == set(completion.JOBS), 'UPGRADE_PREVIOUS_COMPLETION_REQUIRED')
    result = []
    for name in PRESERVED:
        require(completion.fingerprint(old[name]) == completion.fingerprint(new[name]) == rows[name]['desired_sha256'],
                'UPGRADE_PRESERVED_COMPLETION_DRIFT')
        actual = state.get(new[name])
        if actual is not None:
            require(completion.successful(actual) and actual['metadata']['uid'] == rows[name]['job_uid'] and
                    seal(actual) == rows[name]['observed_seal'], 'UPGRADE_PRESERVED_JOB_DRIFT')
            verify_resource(new[name], actual)
        result.append(copy.deepcopy(rows[name]))
    expected = new[MIGRATION]
    migration_contract(expected)
    actual = state.get(expected)
    require(actual is not None and completion.successful(actual) and completion.uuid_value(actual['metadata'].get('uid')) and
            actual['metadata']['uid'] != rows[MIGRATION]['job_uid'] and expected['spec']['backoffLimit'] == 0,
            'UPGRADE_NEW_MIGRATION_NOT_OBSERVED')
    require(not actual['metadata'].get('deletionTimestamp') and not actual['status'].get('terminating') and
            not actual['status'].get('failed') and
            sum(c.get('type') == 'Complete' and c.get('status') == 'True' for c in actual['status'].get('conditions', [])) == 1,
            'UPGRADE_MIGRATION_NOT_TERMINAL')
    verify_resource(expected, actual)
    pod_uid, image_digest = migration_execution(state, expected, actual)
    verify_schemas(state, resources)
    proof_digest = schema_proof(state, profile['schema_profile'])
    again = state.get(expected)
    require(again is not None and again['metadata']['uid'] == actual['metadata']['uid'] and seal(again) == seal(actual) and
            completion.successful(again), 'UPGRADE_MIGRATION_READBACK_CHANGED')
    completed = actual.get('status', {}).get('completionTime')
    require(isinstance(completed, str), 'UPGRADE_MIGRATION_COMPLETION_TIME_REQUIRED')
    result.append({'name': MIGRATION, 'job_uid': actual['metadata']['uid'], 'desired_sha256': completion.fingerprint(expected),
                   'observed_seal': seal(actual), 'completion_time': completed})
    return completion.envelope(state, result, ORIGIN, {'previous_receipt_sha256': previous_receipt_sha256,
        'previous_render_sha256': profile['previous_render_sha256'], 'upgrade_profile_sha256': profile_digest,
        'migration_job_uid': actual['metadata']['uid'], 'migration_pod_uid': pod_uid,
        'migration_image_digest': image_digest.removeprefix('sha256:'), 'schema_proof_sha256': proof_digest})


def write_new(path, raw):
    """Не перезаписывает неизвестный результат; файлы operator evidence остаются private0600."""
    require(path.is_absolute() and path.parent.resolve() == path.parent and path.parent.stat().st_mode & 0o077 == 0,
            'UPGRADE_PRIVATE_DIRECTORY_REQUIRED')
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as stream: stream.write(raw)


def main():
    """prepare — render; capture и verify-apply проверяют реальный migration effect только чтением."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('prepare', 'capture', 'verify-apply'))
    for flag in ('profile', 'previous-render', 'target-render', 'render', 'previous-receipt', 'output'):
        parser.add_argument('--' + flag, type=Path)
    parser.add_argument('--previous-receipt-sha256')
    parser.add_argument('--context')
    completion.add_arguments(parser)
    args = parser.parse_args()
    try:
        if args.mode == 'verify-apply':
            require(args.render is not None and args.context and
                    all(v is None for v in (args.profile, args.previous_render, args.target_render, args.previous_receipt,
                                           args.previous_receipt_sha256, args.output)), 'UPGRADE_APPLY_ARGUMENTS_REQUIRED')
            receipt = completion.from_arguments(args)
            require(isinstance(receipt, dict) and receipt.get('origin') == ORIGIN, 'UPGRADE_COMPLETION_ORIGIN_REQUIRED')
            resources = decode_render(completion.private_bytes(args.render, 8 << 20))
            state = State(args.context);state.discover(resources)
            print(json.dumps(verify_apply(state, resources, receipt)))
            return
        require(args.previous_render is not None and args.output is not None and
                args.bootstrap_completion_receipt is None and args.bootstrap_completion_sha256 is None and
                not args.allow_reconciled_bootstrap_completion, 'UPGRADE_PREPARATION_ARGUMENTS_REQUIRED')
        previous_raw = completion.private_bytes(args.previous_render, 8 << 20)
        if args.mode == 'prepare':
            require(args.profile is not None and args.target_render is not None and args.render is None and
                    args.previous_receipt is None and args.previous_receipt_sha256 is None and args.context is None,
                    'UPGRADE_PREPARE_ARGUMENTS_REQUIRED')
            profile = json.loads(completion.private_bytes(args.profile, 16384), object_pairs_hook=completion.unique_object)
            resources = prepare(previous_raw, completion.private_bytes(args.target_render, 8 << 20), profile)
            write_new(args.output, yaml.safe_dump_all(resources, sort_keys=False).encode())
        else:
            require(args.render is not None and args.previous_receipt is not None and args.previous_receipt_sha256 and
                    args.context and args.profile is None and args.target_render is None, 'UPGRADE_CAPTURE_ARGUMENTS_REQUIRED')
            resources = decode_render(completion.private_bytes(args.render, 8 << 20))
            profile, _ = render_profile(resources)
            require(sha(previous_raw) == profile['previous_render_sha256'], 'UPGRADE_PREVIOUS_RENDER_DRIFT')
            previous = completion.load(args.previous_receipt, args.previous_receipt_sha256, True)
            state = State(args.context); state.discover(resources)
            receipt = capture(state, resources, decode_render(previous_raw), previous, args.previous_receipt_sha256)
            write_new(args.output, (json.dumps(receipt, sort_keys=True) + '\n').encode())
    except (Rejected, ValueError, TypeError, KeyError, OSError, subprocess.SubprocessError, yaml.YAMLError):
        raise SystemExit('Bootstrap upgrade rejected') from None


if __name__ == '__main__':
    main()
