"""Опциональная operator-owned квалификация OBS: чистый render/check без сети и apply.

Artifact не выдаёт owner grant. Точные target/key/version/expiry повторно проверяет
native consumer; смена writer/backend конфигурации требует отзыва и drain до rollout.
"""
import argparse
import copy
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re

import yaml
from deployment_profile import Rejected, require, https_endpoint

SCHEMA = 'kodex-observability-qualifications/v1'
LABEL = 'kodex.dev/observability-qualification'
MOUNT = '/var/run/kodex/observability-qualifications'
VOLUME = 'observability-qualifications'
FILE_ENV = 'INTEGRATION_GATEWAY_OBSERVABILITY_QUALIFICATION_FILE'
SHA_ENV = 'INTEGRATION_GATEWAY_OBSERVABILITY_QUALIFICATION_SHA256'
NAMESPACE = 'kodex-system'
COMMON = {'kind', 'origin', 'providerVersion', 'credentialSha256', 'imageSha256',
          'configurationSha256', 'writerInventorySha256', 'runtimeReadbackSha256',
          'qualifiedAt', 'validUntil', 'writerMode', 'proofs'}
OPTIONAL = {'organizationId', 'dashboardUid', 'tenantId', 'namespace',
            'annotationStoreBackend', 'annotationApiMigrationPhase', 'rulerStorageBackend'}
DIGESTS = ('credentialSha256', 'imageSha256', 'configurationSha256',
           'writerInventorySha256', 'runtimeReadbackSha256')
PROOF = {'case', 'requestSha256', 'responseSha256', 'httpStatus'}
PROOF_OPTIONAL = {'credentialSha256', 'tenantId', 'namespace'}
BASE_PROOFS = {'deployment-readback', 'target-config', 'writer-inventory'}


def digest(value):
    return isinstance(value, str) and re.fullmatch('[a-f0-9]{64}', value) and value != '0'*64


def slug(value):
    return isinstance(value, str) and re.fullmatch('[A-Za-z0-9_-]{1,100}', value) and value not in ('.', '..')


def unique_object(pairs):
    result = {}
    folded = set()
    for key, value in pairs:
        require(key.casefold() not in folded, 'OBSERVABILITY_DUPLICATE_FIELD_REJECTED')
        folded.add(key.casefold())
        result[key] = value
    return result


def validate(document, now=None):
    now = now or datetime.now(timezone.utc)
    require(isinstance(document, dict) and set(document) == {'schema', 'profiles'} and document['schema'] == SCHEMA,
            'OBSERVABILITY_EXACT_SCHEMA_REQUIRED')
    profiles = document['profiles']
    require(isinstance(profiles, list) and 1 <= len(profiles) <= 32, 'OBSERVABILITY_BOUNDED_PROFILES_REQUIRED')
    seen = set()
    for item in profiles:
        require(isinstance(item, dict) and COMMON <= set(item) <= COMMON | OPTIONAL,
                'OBSERVABILITY_EXACT_PROFILE_REQUIRED')
        endpoint = https_endpoint(item['origin'])
        require(endpoint.path == '' and all(digest(item[key]) for key in DIGESTS), 'OBSERVABILITY_EXACT_PINS_REQUIRED')
        require(all(isinstance(item.get(key, ''), str) for key in OPTIONAL), 'OBSERVABILITY_LITERAL_SCOPE_REQUIRED')
        try:
            qualified = datetime.fromisoformat(item['qualifiedAt'].replace('Z', '+00:00'))
            expiry = datetime.fromisoformat(item['validUntil'].replace('Z', '+00:00'))
            valid = item['qualifiedAt'].endswith('Z') and item['validUntil'].endswith('Z') and qualified <= now+timedelta(minutes=1) and now < expiry and timedelta(0) < expiry-qualified <= timedelta(hours=24)
        except (TypeError, ValueError, AttributeError):
            valid = False
        require(valid, 'OBSERVABILITY_EXPIRED_OR_UNBOUNDED_PROFILE')
        identity = tuple(item.get(k, '') for k in ('kind', 'origin', 'organizationId', 'dashboardUid', 'tenantId', 'namespace', 'credentialSha256'))
        require(identity not in seen, 'OBSERVABILITY_DUPLICATE_PROFILE_REJECTED')
        seen.add(identity)
        proofs = item['proofs']
        require(isinstance(proofs, list) and 3 <= len(proofs) <= 16, 'OBSERVABILITY_PROOFS_REQUIRED')
        indexed = {}
        for proof in proofs:
            require(isinstance(proof, dict) and PROOF <= set(proof) <= PROOF | PROOF_OPTIONAL and
                    isinstance(proof['case'], str) and 0 < len(proof['case']) <= 80 and proof['case'] not in indexed and
                    digest(proof['requestSha256']) and digest(proof['responseSha256']) and type(proof['httpStatus']) is int and
                    ('credentialSha256' not in proof or digest(proof['credentialSha256'])) and
                    all(slug(proof[k]) for k in ('tenantId', 'namespace') if k in proof), 'OBSERVABILITY_EXACT_PROOF_REQUIRED')
            indexed[proof['case']] = proof
        require(all(indexed.get(k, {}).get('httpStatus') == 200 for k in BASE_PROOFS), 'OBSERVABILITY_DEPLOYMENT_PROOFS_REQUIRED')
        kind = item['kind']
        org = item.get('organizationId', '')
        org_valid = isinstance(org, str) and re.fullmatch('[1-9][0-9]{0,18}', org) and int(org) <= 2**63-1
        if kind == 'GRAFANA_LEGACY_SQL_ANNOTATIONS_V1':
            require(item['providerVersion'] == '13.2.0' and org_valid and slug(item.get('dashboardUid')) and
                    item['writerMode'] == 'LEGACY_SQL_ANNOTATIONS' and item.get('annotationStoreBackend') == 'legacy-sql' and
                    item.get('annotationApiMigrationPhase') == 'off' and not any(item.get(k) for k in ('tenantId', 'namespace', 'rulerStorageBackend')) and
                    set(indexed) == BASE_PROOFS, 'OBSERVABILITY_LEGACY_SQL_PROFILE_REQUIRED')
        elif kind == 'GRAFANA_API_ONLY_RULE_DELETE_V1':
            require(item['providerVersion'] == '13.2.0' and org_valid and item['writerMode'] == 'API_ONLY_ORGANIZATION_ALERT_RULES' and
                    not any(item.get(k) for k in OPTIONAL - {'organizationId'}) and set(indexed) == BASE_PROOFS,
                    'OBSERVABILITY_API_ONLY_ORGANIZATION_REQUIRED')
        elif kind == 'LOKI_API_ONLY_RULER_NAMESPACE_V1':
            require(item['providerVersion'] == '3.6.21' and slug(item.get('tenantId')) and slug(item.get('namespace')) and
                    item['writerMode'] == 'API_ONLY_RULER_NAMESPACE' and item.get('rulerStorageBackend') == 'S3' and
                    not any(item.get(k) for k in ('organizationId', 'dashboardUid', 'annotationStoreBackend', 'annotationApiMigrationPhase')),
                    'OBSERVABILITY_REMOTE_RULER_REQUIRED')
            names = BASE_PROOFS | {'tenant-positive-a', 'tenant-positive-b', 'tenant-cross-a-b', 'tenant-cross-b-a', 'ruler-create', 'ruler-update', 'ruler-delete'}
            require(set(indexed) == names, 'OBSERVABILITY_TWO_TENANT_PROOFS_REQUIRED')
            a, b = indexed['tenant-positive-a'], indexed['tenant-positive-b']
            require(a['httpStatus'] == b['httpStatus'] == 200 and a.get('credentialSha256') == item['credentialSha256'] and
                    a.get('tenantId') == item['tenantId'] and digest(b.get('credentialSha256')) and
                    b['credentialSha256'] != a['credentialSha256'] and slug(b.get('tenantId')) and b['tenantId'] != a['tenantId'],
                    'OBSERVABILITY_DISTINCT_REAL_KEYS_REQUIRED')
            for name, key, tenant in [('tenant-cross-a-b', a['credentialSha256'], b['tenantId']), ('tenant-cross-b-a', b['credentialSha256'], a['tenantId'])]:
                proof = indexed[name]
                require(proof['httpStatus'] in (403, 404) and proof.get('credentialSha256') == key and proof.get('tenantId') == tenant,
                        'OBSERVABILITY_CROSS_TENANT_REJECTION_REQUIRED')
            for name in ('ruler-create', 'ruler-update', 'ruler-delete'):
                proof = indexed[name]
                require(proof['httpStatus'] == 202 and proof.get('credentialSha256') == a['credentialSha256'] and proof.get('tenantId') == a['tenantId'] and proof.get('namespace') == item['namespace'],
                        'OBSERVABILITY_EXACT_RULER_EFFECT_PROOF_REQUIRED')
        else:
            raise Rejected('OBSERVABILITY_UNKNOWN_PROFILE_REJECTED')
    return document


def wire(document):
    return json.dumps(document, ensure_ascii=True, separators=(',', ':')).encode()


def workload(resources):
    matches = [r for r in resources if r.get('kind') == 'Deployment' and r.get('metadata', {}).get('namespace') == NAMESPACE and r['metadata'].get('name') == 'integration-gateway']
    require(len(matches) == 1, 'OBSERVABILITY_EXACT_WORKLOAD_REQUIRED')
    template = matches[0]['spec']['template']
    require(template['metadata']['labels'].get('app.kubernetes.io/name') == 'integration-gateway', 'OBSERVABILITY_WORKLOAD_IDENTITY_REQUIRED')
    matches = [c for c in template['spec']['containers'] if c.get('name') == 'integration-gateway']
    require(len(matches) == 1, 'OBSERVABILITY_EXACT_CONTAINER_REQUIRED')
    return template, matches[0]


def transform(resources, document):
    document = validate(copy.deepcopy(document))
    result = copy.deepcopy(resources)
    require(not any(LABEL in r.get('metadata', {}).get('labels', {}) for r in result), 'OBSERVABILITY_EXISTING_ARTIFACT_REJECTED')
    template, container = workload(result)
    require(LABEL not in template['metadata']['labels'], 'OBSERVABILITY_EXISTING_WORKLOAD_PROFILE_REJECTED')
    require(any(v.get('name') == 'INTEGRATION_GATEWAY_NATIVE_PROXY_URL' and v.get('value') == 'http://egress-gateway-native.kodex-system.svc.cluster.local:8085' for v in container.get('env', [])), 'OBSERVABILITY_NATIVE_TRANSPORT_REQUIRED')
    for candidate in template['spec'].get('containers', []) + template['spec'].get('initContainers', []) + template['spec'].get('ephemeralContainers', []):
        require(not any(v.get('name') in (FILE_ENV, SHA_ENV) for v in candidate.get('env', [])) and
                not any(v.get('name') == VOLUME or v.get('mountPath', '').startswith(MOUNT) for v in candidate.get('volumeMounts', [])), 'OBSERVABILITY_CONTAINER_FIELD_CONFLICT')
    require(not any(v.get('name') == VOLUME for v in template['spec'].get('volumes', [])), 'OBSERVABILITY_VOLUME_CONFLICT')
    raw = wire(document)
    require(len(raw) <= 128 << 10, 'OBSERVABILITY_BOUNDED_ARTIFACT_REQUIRED')
    sha = hashlib.sha256(raw).hexdigest()
    name = 'kodex-observability-qualification-' + sha[:24]
    environment = container.setdefault('env', [])
    index = next(i for i, value in enumerate(environment) if value.get('name') == 'INTEGRATION_GATEWAY_NATIVE_PROXY_URL')
    environment[index:index] = [{'name': FILE_ENV, 'value': MOUNT+'/profiles.json'}, {'name': SHA_ENV, 'value': sha}]
    container.setdefault('volumeMounts', []).append({'name': VOLUME, 'mountPath': MOUNT, 'readOnly': True})
    template['spec'].setdefault('volumes', []).append({'name': VOLUME, 'configMap': {'name': name, 'defaultMode': 292, 'items': [{'key': 'profiles.json', 'path': 'profiles.json'}]}})
    template['metadata']['labels'][LABEL] = sha[:32]
    result.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': name, 'namespace': NAMESPACE, 'labels': {LABEL: sha[:32]}}, 'immutable': True, 'data': {'profiles.json': raw.decode()}})
    return result


def check(resources):
    marked = [r for r in resources if LABEL in r.get('metadata', {}).get('labels', {})]
    if not marked:
        for resource in resources:
            if resource.get('kind') == 'Deployment' and resource.get('metadata', {}).get('name') == 'integration-gateway' and resource['metadata'].get('namespace') == NAMESPACE:
                template, container = workload(resources)
                require(LABEL not in template['metadata']['labels'] and not any(v.get('name') in (FILE_ENV, SHA_ENV) for c in template['spec'].get('containers', [])+template['spec'].get('initContainers', [])+template['spec'].get('ephemeralContainers', []) for v in c.get('env', [])) and
                        not any(v.get('name') == VOLUME for v in template['spec'].get('volumes', [])), 'OBSERVABILITY_UNDECLARED_PROFILE_REJECTED')
        return None
    require(len(marked) == 1 and marked[0].get('kind') == 'ConfigMap' and marked[0].get('immutable') is True and set(marked[0].get('data', {})) == {'profiles.json'}, 'OBSERVABILITY_EXACT_CONFIGMAP_REQUIRED')
    raw = marked[0]['data']['profiles.json'].encode()
    require(len(raw) <= 128 << 10, 'OBSERVABILITY_BOUNDED_ARTIFACT_REQUIRED')
    document = validate(json.loads(raw, object_pairs_hook=unique_object))
    require(raw == wire(document), 'OBSERVABILITY_CANONICAL_ARTIFACT_REQUIRED')
    baseline = copy.deepcopy([r for r in resources if r is not marked[0]])
    template, container = workload(baseline)
    template['metadata']['labels'].pop(LABEL, None)
    container['env'] = [v for v in container.get('env', []) if v.get('name') not in (FILE_ENV, SHA_ENV)]
    container['volumeMounts'] = [v for v in container.get('volumeMounts', []) if v.get('name') != VOLUME]
    template['spec']['volumes'] = [v for v in template['spec'].get('volumes', []) if v.get('name') != VOLUME]
    wanted = transform(baseline, document)
    key = lambda r: (r['apiVersion'], r['kind'], r['metadata'].get('namespace', ''), r['metadata']['name'])
    require(sorted(wanted, key=key) == sorted(resources, key=key), 'OBSERVABILITY_RENDER_DRIFT')
    return document


def main():
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('--render', required=True, type=Path)
    parser.add_argument('--configuration', type=Path)
    parser.add_argument('--configuration-sha256')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink() and args.render.stat().st_size <= 16 << 20, 'OBSERVABILITY_RENDER_FILE_REQUIRED')
    resources = list(filter(None, yaml.safe_load_all(args.render.read_text())))
    if args.check:
        require(not args.configuration and not args.configuration_sha256, 'OBSERVABILITY_CHECK_INPUT_CONFLICT')
        check(resources)
    else:
        require(args.configuration and digest(args.configuration_sha256) and args.configuration.is_file() and not args.configuration.is_symlink() and args.configuration.stat().st_size <= 128 << 10, 'OBSERVABILITY_PINNED_SOURCE_REQUIRED')
        raw = args.configuration.read_bytes()
        require(hashlib.sha256(raw).hexdigest() == args.configuration_sha256, 'OBSERVABILITY_SOURCE_DIGEST_MISMATCH')
        result = transform(resources, json.loads(raw, object_pairs_hook=unique_object))
        check(result)
        descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
        with os.fdopen(descriptor, 'w') as stream:
            yaml.safe_dump_all(result, stream, sort_keys=False)


if __name__ == '__main__':
    try:
        main()
    except (Rejected, ValueError, KeyError, TypeError, OSError, yaml.YAMLError):
        raise SystemExit('Observability qualification rejected') from None
