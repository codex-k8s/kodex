"""Exact readback и fenced mutations локального delivery; секреты не выводятся."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import urllib.parse

import yaml
from deployment_profile import require, Rejected


def key(resource):
    m = resource['metadata']
    return '|'.join((resource['apiVersion'], resource['kind'], m.get('namespace', ''), m['name']))


def descriptor(resource):
    return {k: copy.deepcopy(resource[k]) for k in ('apiVersion', 'kind', 'metadata')} | {
        'metadata': {k: v for k, v in resource['metadata'].items() if k in ('namespace', 'name')}}


def seal(resource):
    metadata = resource.get('metadata', {})
    value = {k: v for k, v in resource.items() if k not in ('metadata', 'status')}
    annotations = {k: v for k, v in metadata.get('annotations', {}).items()
                   if k != 'kubectl.kubernetes.io/last-applied-configuration'}
    value['labels'] = metadata.get('labels', {})
    value['annotations'] = annotations
    value['ownerReferences'] = metadata.get('ownerReferences', [])
    value['finalizers'] = metadata.get('finalizers', [])
    value['deletionTimestamp'] = metadata.get('deletionTimestamp')
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def owner(resource):
    return resource.get('metadata', {}).get('labels', {}).get('app.kubernetes.io/part-of') == 'kodex'


def subset(expected, actual):
    if isinstance(expected, dict):
        return isinstance(actual, dict) and all(k in actual and subset(v, actual[k]) for k, v in expected.items())
    if isinstance(expected, list):
        # Порядок и состав env, containers, routes и policy rules не расширяются.
        return isinstance(actual, list) and len(expected) == len(actual) and all(subset(x, y) for x, y in zip(expected, actual))
    return expected == actual


def verify_resource(expected, actual):
    require(key(expected) == key(actual) and owner(actual), 'LIVE_RESOURCE_IDENTITY_REJECTED')
    require(actual['metadata'].get('ownerReferences', []) == expected['metadata'].get('ownerReferences', []), 'LIVE_OWNER_REFERENCE_DRIFT')
    require(subset(expected['metadata'].get('labels', {}), actual['metadata'].get('labels', {})), 'LIVE_LABEL_DRIFT')
    require(subset(expected['metadata'].get('annotations', {}), actual['metadata'].get('annotations', {})), 'LIVE_ANNOTATION_DRIFT')
    for field in ('spec', 'data', 'binaryData', 'immutable', 'rules', 'roleRef', 'subjects'):
        if field in expected:
            require(subset(expected[field], actual.get(field)), 'LIVE_RESOURCE_DRIFT')
    # Дополнительное правило/annotation не должно расширить security boundary.
    if expected['kind'] in ('NetworkPolicy', 'Ingress', 'Middleware', 'ServersTransport'):
        def normalized_spec(resource):
            spec = copy.deepcopy(resource.get('spec'))
            if resource['kind'] == 'NetworkPolicy':
                for direction in ('ingress', 'egress'):
                    if spec.get(direction) == []:
                        spec.pop(direction)
                    for rule in spec.get(direction, []):
                        for port in rule.get('ports', []):
                            port.setdefault('protocol', 'TCP')
            return spec
        require(normalized_spec(actual) == normalized_spec(expected), 'LIVE_SECURITY_SPEC_DRIFT')
        annotations = {k: v for k, v in actual['metadata'].get('annotations', {}).items()
                       if k != 'kubectl.kubernetes.io/last-applied-configuration'}
        require(annotations == expected['metadata'].get('annotations', {}), 'LIVE_SECURITY_ANNOTATION_DRIFT')
    if expected['kind'] == 'ConfigMap':
        require(actual.get('data', {}) == expected.get('data', {}) and
                actual.get('binaryData', {}) == expected.get('binaryData', {}), 'LIVE_CONFIG_DRIFT')


class State:
    def __init__(self, context):
        self.context = context
        self.routes = {}

    def call(self, args, value=None):
        result = subprocess.run(['kubectl', '--context', self.context, '--request-timeout=15s', *args],
            input=None if value is None else json.dumps(value).encode(), capture_output=True, timeout=20)
        require(result.returncode == 0, 'KUBERNETES_OPERATION_FAILED_RECONCILE_REQUIRED')
        return result.stdout

    def discover(self, resources):
        groups = json.loads(self.call(['get', '--raw', '/apis']))
        served = {v['groupVersion'] for group in groups['groups'] for v in group['versions']} | {'v1'}
        declared = {(r['spec']['group'] + '/' + v['name'], r['spec']['names']['kind'])
                    for r in resources if r['kind'] == 'CustomResourceDefinition'
                    for v in r['spec']['versions'] if v.get('served')}
        for api in {r['apiVersion'] for r in resources if r['kind'] != 'Secret'}:
            if api not in served:
                require(all((api, r['kind']) in declared for r in resources if r['apiVersion'] == api), 'MISSING_INFRASTRUCTURE_API')
                continue
            prefix = '/api/v1' if api == 'v1' else '/apis/' + api
            document = json.loads(self.call(['get', '--raw', prefix]))
            for resource in document['resources']:
                if '/' not in resource['name']:
                    self.routes[(api, resource['kind'])] = (prefix, resource['name'], resource['namespaced'])
        for r in resources:
            if r['kind'] != 'Secret' and (r['apiVersion'], r['kind']) not in self.routes:
                require((r['apiVersion'], r['kind']) in declared, 'MISSING_INFRASTRUCTURE_KIND')

    def path(self, resource):
        prefix, plural, namespaced = self.routes[(resource['apiVersion'], resource['kind'])]
        metadata = resource['metadata']
        suffix = '/namespaces/' + metadata['namespace'] if namespaced else ''
        return prefix + suffix + '/' + plural + '/' + metadata['name']

    def get(self, resource):
        if (resource['apiVersion'], resource['kind']) not in self.routes:
            return None
        raw = self.call(['get', '-f', '-', '--ignore-not-found', '-o', 'json'], descriptor(resource))
        return json.loads(raw) if raw.strip() else None

    def inventory(self, resources):
        self.discover(resources)
        result = {}
        # Выполняется весь inventory до первой mutation. Secret seeds не обновляются.
        for resource in resources:
            if resource['kind'] == 'Secret':
                continue
            actual = self.get(resource)
            require(actual is None or owner(actual), 'FOREIGN_RESOURCE_COLLISION')
            if actual is not None:
                require(not actual['metadata'].get('deletionTimestamp') and
                        actual['metadata'].get('ownerReferences', []) == resource['metadata'].get('ownerReferences', []),
                        'FOREIGN_RESOURCE_OWNER_REFERENCE')
                for label in ('app.kubernetes.io/name', 'app.kubernetes.io/component'):
                    wanted = resource['metadata'].get('labels', {}).get(label)
                    if wanted is not None:
                        require(actual['metadata'].get('labels', {}).get(label) == wanted, 'FOREIGN_RESOURCE_COMPONENT')
            result[key(resource)] = None if actual is None else {
                'uid': actual['metadata']['uid'], 'seal': seal(actual)}
        return result

    def fenced_current(self, resource, inventory):
        require(key(resource) in inventory, 'RESOURCE_NOT_IN_APPROVED_INVENTORY')
        actual = self.get(resource)
        before = inventory[key(resource)]
        if before is None:
            require(actual is None, 'NEW_RESOURCE_COLLISION')
        else:
            require(actual is not None and owner(actual) and actual['metadata']['uid'] == before['uid']
                    and seal(actual) == before['seal'], 'RESOURCE_PREIMAGE_DRIFT')
        return actual

    def apply(self, resource, inventory):
        actual = self.fenced_current(resource, inventory)
        desired = copy.deepcopy(resource)
        if actual is None:
            # Create, а не SSA upsert: появившийся после inventory объект не перезапишется.
            self.call(['create', '--field-manager=kodex-install', '-f', '-'], desired)
        else:
            desired['metadata']['resourceVersion'] = actual['metadata']['resourceVersion']
            self.call(['apply', '--server-side', '--field-manager=kodex-install', '-f', '-'], desired)
        current = self.get(resource)
        require(current is not None and owner(current), 'WRITE_READBACK_REQUIRED')
        inventory[key(resource)] = {'uid': current['metadata']['uid'], 'seal': seal(current)}

    def delete(self, resource, inventory):
        actual = self.fenced_current(resource, inventory)
        if actual is None:
            return
        metadata = actual['metadata']
        self.call(['delete', '--raw', self.path(resource), '-f', '-'], {
            'apiVersion': 'v1', 'kind': 'DeleteOptions', 'propagationPolicy': 'Foreground',
            'preconditions': {'uid': metadata['uid'], 'resourceVersion': metadata['resourceVersion']}})
        # Завершение foreground deletion проверяется перед созданием successor.
        self.call(['wait', '--for=delete', '-f', '-', '--timeout=15s'], descriptor(resource))
        inventory[key(resource)] = None


def verify_schemas(state, resources):
    records = [r for r in resources if r['kind'] == 'ConfigMap' and r['metadata']['name'] == 'kodex-installation-profile']
    require(len(records) == 1, 'INSTALLATION_PROFILE_REQUIRED')
    expected = json.loads(records[0]['data']['schema-heads.json'])
    require(set(expected) == {'control-plane', 'internal-rpc-authority', 'email-bridge'}, 'SCHEMA_PIN_SET_REJECTED')
    targets = {'control-plane': ('kodex-postgresql-0', 'control_plane'),
               'internal-rpc-authority': ('kodex-postgresql-0', 'internal_rpc_authority'),
               'email-bridge': ('email-bridge-postgresql-0', 'email_bridge')}
    for component, (pod, database) in targets.items():
        require(type(expected[component]) is int and expected[component] > 0, 'SCHEMA_PIN_REJECTED')
        # Только чтение одного migration head через локальный PostgreSQL socket.
        # Пароли/DSN/дампы не читаются, никаких down или SQL repair.
        raw = state.call(['-n', 'kodex-system', 'exec', pod, '--', 'psql', '-XqAt', '-U', 'postgres',
                          '-d', database, '-v', 'ON_ERROR_STOP=1', '-c',
                          'SELECT max(version_id) FROM public.goose_db_version WHERE is_applied'])
        require(raw.strip().isdigit() and int(raw.strip()) == expected[component], 'LIVE_SCHEMA_DRIFT')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('inventory', 'apply', 'delete', 'readback'))
    parser.add_argument('--context', required=True)
    parser.add_argument('--render', required=True)
    parser.add_argument('--inventory')
    parser.add_argument('--manifest')
    parser.add_argument('--kind')
    parser.add_argument('--name')
    parser.add_argument('--namespace', default='kodex-system')
    args = parser.parse_args()
    try:
        raw = Path(args.render).read_bytes()
        resources = list(filter(None, yaml.safe_load_all(raw)))
        render_digest = hashlib.sha256(raw).hexdigest()
        state = State(args.context)
        state.discover(resources)
        if args.mode == 'inventory':
            value = state.inventory(resources)
            require(args.inventory and not Path(args.inventory).exists(), 'NEW_INVENTORY_PATH_REQUIRED')
            with open(args.inventory, 'x', opener=lambda p, f: os.open(p, f, 0o600)) as stream:
                json.dump({'version': 1, 'context': args.context, 'renderSHA256': render_digest, 'resources': value}, stream)
            return
        if args.mode == 'readback':
            for expected in resources:
                if expected['kind'] == 'Secret':
                    continue
                actual = state.get(expected)
                require(actual is not None, 'LIVE_RESOURCE_MISSING')
                verify_resource(expected, actual)
            verify_schemas(state, resources)
            return
        require(args.inventory, 'INVENTORY_REQUIRED')
        record = json.loads(Path(args.inventory).read_text())
        require(record.get('version') == 1 and record.get('context') == args.context and
                record.get('renderSHA256') == render_digest, 'INVENTORY_INPUT_DRIFT')
        inventory = record['resources']
        if args.mode == 'apply':
            require(args.manifest, 'MANIFEST_REQUIRED')
            selected = list(filter(None, yaml.safe_load_all(Path(args.manifest).read_bytes())))
            registered = {key(r): r for r in resources}
            for desired in selected:
                require(desired == registered.get(key(desired)), 'MANIFEST_OUTSIDE_PINNED_RENDER')
                state.apply(desired, inventory)
                Path(args.inventory).write_text(json.dumps(record))
        else:
            selected = [r for r in resources if r['kind'].lower() == args.kind.lower() and
                        r['metadata']['name'] == args.name and r['metadata'].get('namespace', '') == args.namespace]
            require(len(selected) == 1, 'EXACT_DELETE_RESOURCE_REQUIRED')
            state.delete(selected[0], inventory)
            Path(args.inventory).write_text(json.dumps(record))
    except (Rejected, OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError):
        raise SystemExit('Portable state operation rejected; readback required') from None


if __name__ == '__main__':
    main()
