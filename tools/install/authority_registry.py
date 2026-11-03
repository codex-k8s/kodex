#!/usr/bin/env python3
"""Явная ревизия deployment registry и readonly допуск продолжения первого bootstrap."""
import argparse
import copy
import json
import os
from pathlib import Path
import yaml
from deployment_profile import require, Rejected
from portable_state import State, verify_resource, verify_schemas

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


def verify_resume(state, resources):
    desired = one(resources, 'ConfigMap', REGISTRY)
    document = yaml.safe_load(desired['data']['key-delivery-targets.yaml'])
    require(desired['metadata'].get('annotations', {}).get(ANNOTATION) == '1' and document.get('source_revision') == 1, 'INITIAL_AUTHORITY_REVISION_ONE_REQUIRED')
    current = state.get(desired)
    require(current is not None and current['metadata'].get('labels', {}).get('app.kubernetes.io/part-of') == 'kodex', 'OWNED_AUTHORITY_REGISTRY_REQUIRED')
    actual = yaml.safe_load(current['data']['key-delivery-targets.yaml'])
    predecessor = copy.deepcopy(document);predecessor['source_revision'] = actual.get('source_revision')
    require(type(actual.get('source_revision')) is int and actual['source_revision'] >= 1 and predecessor == actual, 'AUTHORITY_REGISTRY_TARGET_DRIFT')
    verify_schemas(state, resources)
    for name in MIGRATION_JOBS:
        job = one(resources, 'Job', name)
        observed = state.get(job)
        require(observed is not None and observed.get('status', {}).get('succeeded') == 1 and not observed.get('status', {}).get('active'), 'COMPLETED_BOOTSTRAP_JOB_REQUIRED')
        verify_resource(job, observed)
    publisher = one(resources, 'Deployment', 'internal-rpc-authority-publisher')
    observed = state.get(publisher)
    require(observed is not None and observed['spec'].get('replicas') == 0, 'QUIESCED_AUTHORITY_PUBLISHER_REQUIRED')
    expected = copy.deepcopy(publisher);expected['spec']['replicas'] = 0
    verify_resource(expected, observed)
    deployments = json.loads(state.call(['-n', 'kodex-system', 'get', 'deployments', '-o', 'json']))['items']
    require(len(deployments) == 1 and deployments[0]['metadata']['uid'] == observed['metadata']['uid'], 'AUTHORITY_ONLY_STARTUP_PHASE_REQUIRED')
    publishers = json.loads(state.call(['-n', 'kodex-system', 'get', 'pods', '-l', 'app.kubernetes.io/name=internal-rpc-authority-publisher', '-o', 'json']))['items']
    require(all(p.get('status', {}).get('phase') in ('Succeeded', 'Failed') for p in publishers), 'TERMINATED_PUBLISHER_PODS_REQUIRED')
    runtime = json.loads(state.call(['-n', 'kodex-runtime', 'get', 'pods', '-o', 'json']))['items']
    require(not runtime, 'EMPTY_RUNTIME_REQUIRED_FOR_INITIAL_RESUME')
    raw = state.call(['-n', 'kodex-system', 'exec', 'kodex-postgresql-0', '--', 'psql', '-XqAt', '-U', 'postgres', '-d', 'internal_rpc_authority', '-v', 'ON_ERROR_STOP=1', '-c', STATE_SQL])
    require(json.loads(raw) == {'rotation_count': 0, 'snapshot_count': 0, 'fence_allows': True}, 'EMPTY_OPEN_AUTHORITY_HISTORY_REQUIRED')
    return {'status': 'INITIAL_AUTHORITY_RESUME_PREFLIGHT_PASSED', 'sourceRevision': 1, 'migrations': 'VERIFIED_NOT_REPEATED', 'authorityHistory': 'EMPTY'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('render', 'preflight-resume'))
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--configuration', type=Path)
    parser.add_argument('--context')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes();require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw)));require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    if args.mode == 'render':
        require(args.configuration is not None and args.configuration.is_file() and not args.configuration.is_symlink(), 'AUTHORITY_CONFIGURATION_REQUIRED')
        value = json.loads(args.configuration.read_bytes())
        result = transform(resources, value)
        descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
        with os.fdopen(descriptor, 'w') as stream:yaml.safe_dump_all(result, stream, sort_keys=False)
    else:
        require(args.context, 'EXACT_CONTEXT_REQUIRED')
        state = State(args.context);state.discover(resources)
        print(json.dumps(verify_resume(state, resources)))


if __name__ == '__main__':
    try:main()
    except (Rejected, OSError, ValueError, TypeError, KeyError, yaml.YAMLError):
        raise SystemExit('Authority registry profile rejected') from None
