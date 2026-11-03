#!/usr/bin/env python3
"""Явная ревизия deployment registry и readonly допуск продолжения первого bootstrap."""
import argparse
import copy
import hashlib
import re
import json
import os
from pathlib import Path
import yaml
from deployment_profile import require, Rejected
from portable_state import State, verify_resource, verify_schemas, key
import observability_profile

REGISTRY = 'internal-rpc-authority-publisher-target-registry'
ANNOTATION = 'kodex.dev/authority-registry-source-revision'
MIGRATION_JOBS = ('email-bridge-migration', 'internal-rpc-authority-migrate', 'control-plane-migrate',
                  'kodex-postgresql-runtime-credentials', 'control-plane-broker-bootstrap')
STATE_SQL = "SELECT json_build_object('rotation_count',(SELECT count(*) FROM internal_rpc_authority.authority_rotation_intents),'snapshot_count',(SELECT count(*) FROM internal_rpc_authority.authority_snapshot_history),'fence_allows',internal_rpc_authority.runtime_restore_fence_allows_work());"


def one(resources, kind, name):
    selected = [r for r in resources if r['kind'] == kind and r['metadata']['name'] == name and r['metadata'].get('namespace') == 'kodex-system']
    require(len(selected) == 1, 'AUTHORITY_BOOTSTRAP_RESOURCE_REQUIRED')
    return selected[0]


def configuration(value):
    require(isinstance(value, dict) and set(value) == {'schema', 'source_revision'} and value['schema'] == 'kodex.authority-registry/v1', 'AUTHORITY_REGISTRY_CONFIGURATION_REJECTED')
    revision = value['source_revision']
    require(type(revision) is int and 1 <= revision <= 9007199254740991, 'AUTHORITY_REGISTRY_REVISION_REJECTED')
    return revision


def transform(resources, value):
    revision = configuration(value)
    result = copy.deepcopy(resources)
    registry = one(result, 'ConfigMap', REGISTRY)
    document = yaml.safe_load(registry['data']['key-delivery-targets.yaml'])
    require(isinstance(document, dict) and set(document) == {'version', 'source_revision', 'targets'} and document['version'] == 1 and isinstance(document['targets'], list) and 0 < len(document['targets']) <= 384, 'AUTHORITY_REGISTRY_DOCUMENT_REJECTED')
    document['source_revision'] = revision
    registry['data']['key-delivery-targets.yaml'] = yaml.safe_dump(document, sort_keys=False)
    registry['metadata'].setdefault('annotations', {})[ANNOTATION] = str(revision)
    return result


def projection_plan(resources, registry):
    require(isinstance(registry, dict) and registry.get('version') == 1 and registry.get('namespace') == 'kodex-system' and isinstance(registry.get('secrets'), list), 'PROJECTION_REGISTRY_REJECTED')
    names = [row['name'] for row in registry['secrets']]
    require(len(names) == len(set(names)), 'DUPLICATE_PROJECTION_REJECTED')
    referenced = set()
    def reference(name):
        require(isinstance(name, str) and re.fullmatch(r'[a-z0-9][a-z0-9.-]{0,251}[a-z0-9]|[a-z0-9]', name), 'SECRET_REFERENCE_REJECTED')
        referenced.add(name)
    for resource in resources:
        if resource.get('metadata', {}).get('namespace') != registry['namespace']:
            continue
        kind = resource.get('kind')
        if kind == 'Pod':pod = resource['spec']
        elif kind in ('Deployment', 'StatefulSet', 'DaemonSet', 'Job'):pod = resource['spec']['template']['spec']
        elif kind == 'CronJob':pod = resource['spec']['jobTemplate']['spec']['template']['spec']
        else:continue
        for volume in pod.get('volumes', []):
            if 'secret' in volume:reference(volume['secret']['secretName'])
            for source in volume.get('projected', {}).get('sources', []):
                if 'secret' in source:reference(source['secret']['name'])
        for image_pull in pod.get('imagePullSecrets', []):reference(image_pull['name'])
        for group in ('containers', 'initContainers', 'ephemeralContainers'):
            for container in pod.get(group, []):
                for env in container.get('env', []):
                    if 'secretKeyRef' in env.get('valueFrom', {}):reference(env['valueFrom']['secretKeyRef']['name'])
                for source in container.get('envFrom', []):
                    if 'secretRef' in source:reference(source['secretRef']['name'])
    selected = [copy.deepcopy(row) for row in registry['secrets'] if row.get('dynamic') is True and row['name'] in referenced]
    for row in selected:
        keys = [item['key'] for item in row['items']]
        require(keys and len(keys) == len(set(keys)) and all(isinstance(key, str) and key for key in keys), 'PROJECTION_KEYS_REJECTED')
    return {'version': 1, 'namespace': registry['namespace'], 'secrets': selected}


def verify_completed_bootstrap(state, resources):
    verify_schemas(state, resources)
    for name in MIGRATION_JOBS:
        job = one(resources, 'Job', name)
        observed = state.get(job)
        require(observed is not None and observed.get('status', {}).get('succeeded') == 1 and not observed.get('status', {}).get('active'), 'COMPLETED_BOOTSTRAP_JOB_REQUIRED')
        verify_resource(job, observed)


def verify_startup_only(state, observed):
    deployments = json.loads(state.call(['-n', 'kodex-system', 'get', 'deployments', '-o', 'json']))['items']
    require(len(deployments) == 1 and deployments[0]['metadata']['uid'] == observed['metadata']['uid'], 'AUTHORITY_ONLY_STARTUP_PHASE_REQUIRED')
    runtime = json.loads(state.call(['-n', 'kodex-runtime', 'get', 'pods', '-o', 'json']))['items']
    require(not runtime, 'EMPTY_RUNTIME_REQUIRED_FOR_INITIAL_RESUME')


def published_state_sql(registry_digest):
    require(re.fullmatch(r'[a-f0-9]{64}', registry_digest), 'REGISTRY_DIGEST_REQUIRED')
    return "SELECT json_build_object('rotation_count',(SELECT count(*) FROM internal_rpc_authority.authority_rotation_intents),'snapshot_count',(SELECT count(*) FROM internal_rpc_authority.authority_snapshot_history),'fence_allows',internal_rpc_authority.runtime_restore_fence_allows_work(),'matching_count',(SELECT count(*) FROM internal_rpc_authority.authority_rotation_intents r JOIN internal_rpc_authority.authority_snapshot_history h ON h.publication_intent_id=r.intent_id WHERE r.protocol_version=2 AND r.source_revision=1 AND r.registry_source_digest_sha256='" + registry_digest + "' AND r.status IN ('DELIVERED','PROMOTED') AND h.source_revision=r.source_revision AND h.source_digest_sha256=r.source_digest_sha256 AND h.predecessor_revision=0 AND h.predecessor_digest_sha256=repeat('0',64) AND h.snapshot_compact_jws IS NOT NULL AND h.published_at<=now()+interval '5 seconds' AND h.published_at>now()-interval '180 days'));"


def verify_published_resume(state, resources, startup_only=True):
    desired = one(resources, 'ConfigMap', REGISTRY)
    document = yaml.safe_load(desired['data']['key-delivery-targets.yaml'])
    require(desired['metadata'].get('annotations', {}).get(ANNOTATION) == '1' and document.get('source_revision') == 1, 'INITIAL_AUTHORITY_REVISION_ONE_REQUIRED')
    current = state.get(desired)
    require(current is not None, 'OWNED_AUTHORITY_REGISTRY_REQUIRED')
    verify_resource(desired, current)
    verify_completed_bootstrap(state, resources)
    publisher = one(resources, 'Deployment', 'internal-rpc-authority-publisher')
    observed = state.get(publisher)
    require(observed is not None and type(publisher['spec'].get('replicas')) is int and publisher['spec']['replicas'] > 0, 'STARTED_AUTHORITY_PUBLISHER_REQUIRED')
    verify_resource(publisher, observed)
    if startup_only:
        verify_startup_only(state, observed)
    digest = hashlib.sha256(desired['data']['key-delivery-targets.yaml'].encode()).hexdigest()
    raw = state.call(['-n', 'kodex-system', 'exec', 'kodex-postgresql-0', '--', 'psql', '-XqAt', '-U', 'postgres', '-d', 'internal_rpc_authority', '-v', 'ON_ERROR_STOP=1', '-c', published_state_sql(digest)])
    require(json.loads(raw) == {'rotation_count': 1, 'snapshot_count': 1, 'fence_allows': True, 'matching_count': 1}, 'EXACT_PUBLISHED_INITIAL_AUTHORITY_REQUIRED')
    return {'status': 'PUBLISHED_AUTHORITY_RESUME_PREFLIGHT_PASSED', 'sourceRevision': 1, 'migrations': 'VERIFIED_NOT_REPEATED', 'authorityHistory': 'PRESERVED_NOT_REPLAYED'}


def verify_workload_resume(state, resources, previous):
    """Продолжение initial graph допускает только точный observability profile delta."""
    require(resources == observability_profile.transform(previous, disabled=True), 'EXACT_BOOTSTRAP_PROFILE_DELTA_REQUIRED')
    verify_published_resume(state, resources, startup_only=False)
    before = {key(r): r for r in previous if r.get('kind') == 'Deployment'}
    after = {key(r): r for r in resources if r.get('kind') == 'Deployment'}
    require(len(before) == sum(r.get('kind') == 'Deployment' for r in previous) and set(before) == set(after), 'EXACT_BOOTSTRAP_DEPLOYMENT_SET_REQUIRED')
    live = json.loads(state.call(['-n', 'kodex-system', 'get', 'deployments', '-o', 'json']))['items']
    require(bool(live), 'STARTED_BOOTSTRAP_DEPLOYMENTS_REQUIRED')
    for deployment in live:
        identity = key(deployment)
        require(identity in before, 'UNKNOWN_BOOTSTRAP_DEPLOYMENT_REJECTED')
        try:
            verify_resource(before[identity], deployment)
        except Rejected:
            # Interrupted repair допускает только уже применённый exact successor.
            verify_resource(after[identity], deployment)
    runtime = json.loads(state.call(['-n', 'kodex-runtime', 'get', 'pods', '-o', 'json']))['items']
    require(not runtime, 'EMPTY_RUNTIME_REQUIRED_FOR_INITIAL_RESUME')
    return dict(status='WORKLOAD_BOOTSTRAP_RESUME_PREFLIGHT_PASSED', deployments=len(live),
                migrations='VERIFIED_NOT_REPEATED', authorityHistory='PRESERVED_NOT_REPLAYED')


def existing_job(state, resources, name):
    """Проверяет существующий Job, не повторяя его известный либо неизвестный эффект."""
    desired = one(resources, 'Job', name)
    actual = state.get(desired)
    if actual is None:
        return False
    verify_resource(desired, actual)
    require(not any(c.get('type') == 'Failed' and c.get('status') == 'True' for c in actual.get('status', {}).get('conditions', [])), 'FAILED_JOB_REQUIRES_RECONCILIATION')
    return True


def verify_resume(state, resources):
    desired = one(resources, 'ConfigMap', REGISTRY)
    document = yaml.safe_load(desired['data']['key-delivery-targets.yaml'])
    require(desired['metadata'].get('annotations', {}).get(ANNOTATION) == '1' and document.get('source_revision') == 1, 'INITIAL_AUTHORITY_REVISION_ONE_REQUIRED')
    current = state.get(desired)
    require(current is not None and current['metadata'].get('labels', {}).get('app.kubernetes.io/part-of') == 'kodex', 'OWNED_AUTHORITY_REGISTRY_REQUIRED')
    actual = yaml.safe_load(current['data']['key-delivery-targets.yaml'])
    predecessor = copy.deepcopy(document);predecessor['source_revision'] = actual.get('source_revision')
    require(type(actual.get('source_revision')) is int and actual['source_revision'] >= 1 and predecessor == actual, 'AUTHORITY_REGISTRY_TARGET_DRIFT')
    verify_completed_bootstrap(state, resources)
    publisher = one(resources, 'Deployment', 'internal-rpc-authority-publisher')
    observed = state.get(publisher)
    require(observed is not None and observed['spec'].get('replicas') == 0, 'QUIESCED_AUTHORITY_PUBLISHER_REQUIRED')
    expected = copy.deepcopy(publisher);expected['spec']['replicas'] = 0
    verify_resource(expected, observed)
    verify_startup_only(state, observed)
    publishers = json.loads(state.call(['-n', 'kodex-system', 'get', 'pods', '-l', 'app.kubernetes.io/name=internal-rpc-authority-publisher', '-o', 'json']))['items']
    require(all(p.get('status', {}).get('phase') in ('Succeeded', 'Failed') for p in publishers), 'TERMINATED_PUBLISHER_PODS_REQUIRED')
    raw = state.call(['-n', 'kodex-system', 'exec', 'kodex-postgresql-0', '--', 'psql', '-XqAt', '-U', 'postgres', '-d', 'internal_rpc_authority', '-v', 'ON_ERROR_STOP=1', '-c', STATE_SQL])
    require(json.loads(raw) == {'rotation_count': 0, 'snapshot_count': 0, 'fence_allows': True}, 'EMPTY_OPEN_AUTHORITY_HISTORY_REQUIRED')
    return {'status': 'INITIAL_AUTHORITY_RESUME_PREFLIGHT_PASSED', 'sourceRevision': 1, 'migrations': 'VERIFIED_NOT_REPEATED', 'authorityHistory': 'EMPTY'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('render', 'preflight-resume', 'preflight-published', 'preflight-workloads', 'existing-job', 'projection-plan'))
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--configuration', type=Path)
    parser.add_argument('--context')
    parser.add_argument('--projection-registry', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--previous-render', type=Path)
    parser.add_argument('--previous-render-sha256')
    parser.add_argument('--job-name')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes();require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw)));require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    if args.mode == 'projection-plan':
        require(args.projection_registry is not None and args.projection_registry.is_file() and not args.projection_registry.is_symlink() and args.output is not None, 'PROJECTION_PLAN_PATHS_REQUIRED')
        result = projection_plan(resources, json.loads(args.projection_registry.read_bytes()))
        descriptor = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, 'w') as stream:json.dump(result, stream)
    elif args.mode == 'render':
        require(args.configuration is not None and args.configuration.is_file() and not args.configuration.is_symlink(), 'AUTHORITY_CONFIGURATION_REQUIRED')
        value = json.loads(args.configuration.read_bytes())
        result = transform(resources, value)
        descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
        with os.fdopen(descriptor, 'w') as stream:yaml.safe_dump_all(result, stream, sort_keys=False)
    else:
        require(args.context, 'EXACT_CONTEXT_REQUIRED')
        state = State(args.context);state.discover(resources)
        if args.mode == 'preflight-workloads':
            require(args.previous_render is not None and args.previous_render.is_file() and not args.previous_render.is_symlink(), 'PREVIOUS_RENDER_REQUIRED')
            raw = args.previous_render.read_bytes()
            require(len(raw) <= 16 << 20 and hashlib.sha256(raw).hexdigest() == args.previous_render_sha256, 'PREVIOUS_RENDER_DIGEST_REQUIRED')
            previous = list(filter(None, yaml.safe_load_all(raw)))
            print(json.dumps(verify_workload_resume(state, resources, previous)))
        elif args.mode == 'existing-job':
            require(args.job_name, 'EXACT_JOB_NAME_REQUIRED')
            print('PRESENT' if existing_job(state, resources, args.job_name) else 'ABSENT')
        else:
            print(json.dumps(verify_published_resume(state, resources) if args.mode == 'preflight-published' else verify_resume(state, resources)))


if __name__ == '__main__':
    try:main()
    except (Rejected, OSError, ValueError, TypeError, KeyError, yaml.YAMLError):
        raise SystemExit('Authority registry profile rejected') from None
