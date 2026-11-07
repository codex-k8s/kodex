"""Опциональная Git-owned проекция native CONNECT; без сети, секретов и kubectl.

Адреса берутся из явного operator document, а не из Connection, prompt либо DNS.
Последняя проверка допуска и ClientHello выполняется Go gateway до literal dial.
"""
import argparse
import copy
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re

import yaml
from deployment_profile import Rejected, require

SCHEMA = 'egress-native-integration/v1'
LABEL = 'kodex.dev/native-integration-egress'
GENERATION = 'kodex.dev/native-integration-generation'
PROXY = 'http://egress-gateway-native.kodex-system.svc.cluster.local:8085'
MOUNT = '/var/run/config/kodex/egress-gateway-native'
NAMESPACE = 'kodex-system'
PEERS = {'egress-gateway': 'platform-egress', 'integration-gateway': 'integration-worker'}
FIELDS = ('schema', 'generation', 'gateway_policy_digest', 'destinations')
DESTINATION_FIELDS = ('hostname', 'port', 'address_cidrs')
PRIVATE = [ipaddress.ip_network(v) for v in ('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', 'fc00::/7')]


def unique_object(pairs):
    result = {}
    for name, value in pairs:
        require(name not in result, 'NATIVE_DUPLICATE_FIELD_REJECTED')
        result[name] = value
    return result


def wire(document):
    return json.dumps(document, ensure_ascii=True, separators=(',', ':')).encode()


def validate(value):
    require(isinstance(value, dict) and set(value) == set(FIELDS), 'EXACT_NATIVE_FIELDS_REQUIRED')
    require(value['schema'] == SCHEMA and type(value['generation']) is int and 0 < value['generation'] < 2**63,
            'NATIVE_SCHEMA_GENERATION_REJECTED')
    require(isinstance(value['gateway_policy_digest'], str) and re.fullmatch('[a-f0-9]{64}', value['gateway_policy_digest']),
            'NATIVE_BASE_DIGEST_REQUIRED')
    destinations = value['destinations']
    require(isinstance(destinations, list) and 1 <= len(destinations) <= 64, 'BOUNDED_NATIVE_DESTINATIONS_REQUIRED')
    result = {key: copy.deepcopy(value[key]) for key in FIELDS}
    ordered = []
    previous = ''
    for destination in destinations:
        require(isinstance(destination, dict) and set(destination) == set(DESTINATION_FIELDS), 'EXACT_NATIVE_DESTINATION_FIELDS_REQUIRED')
        host = destination['hostname']
        require(isinstance(host, str) and len(host) <= 253 and host > previous and '.' in host and
                all(re.fullmatch('[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in host.split('.')),
                'CANONICAL_SORTED_NATIVE_HOST_REQUIRED')
        try:
            ipaddress.ip_address(host)
        except ValueError:
            pass
        else:
            raise Rejected('NATIVE_LITERAL_HOST_REJECTED')
        require(type(destination['port']) is int and destination['port'] == 443, 'NATIVE_TLS_PORT_REQUIRED')
        pins = destination['address_cidrs']
        require(isinstance(pins, list) and 1 <= len(pins) <= 32 and all(isinstance(p, str) for p in pins) and
                pins == sorted(set(pins)), 'CANONICAL_SORTED_NATIVE_PINS_REQUIRED')
        for pin in pins:
            try:
                network = ipaddress.ip_network(pin, strict=True)
            except ValueError:
                raise Rejected('EXACT_NATIVE_ADDRESS_REQUIRED') from None
            address = network.network_address
            private = any(address.version == prefix.version and address in prefix for prefix in PRIVATE)
            require(str(network) == pin and network.prefixlen == network.max_prefixlen and
                    not address.is_loopback and not address.is_link_local and not address.is_multicast and
                    not address.is_unspecified and not (address.version == 6 and address.ipv4_mapped) and
                    (private or address.is_global and (address.version == 4 or address in ipaddress.ip_network('2000::/3'))),
                    'NATIVE_SPECIAL_OR_BROAD_ADDRESS_REJECTED')
        ordered.append({key: copy.deepcopy(destination[key]) for key in DESTINATION_FIELDS})
        previous = host
    result['destinations'] = ordered
    require(len(wire(result)) <= 65536, 'NATIVE_DOCUMENT_SIZE_REJECTED')
    return result


def load(path, expected_sha256):
    require(isinstance(expected_sha256, str) and re.fullmatch('[a-f0-9]{64}', expected_sha256), 'NATIVE_FILE_DIGEST_REQUIRED')
    require(path.is_file() and not path.is_symlink() and path.stat().st_size <= 65536, 'BOUNDED_NATIVE_FILE_REQUIRED')
    raw = path.read_bytes()
    require(hashlib.sha256(raw).hexdigest() == expected_sha256, 'NATIVE_FILE_DIGEST_MISMATCH')
    return validate(json.loads(raw, object_pairs_hook=unique_object))


def labels(name):
    return {'app.kubernetes.io/name': name, 'app.kubernetes.io/component': PEERS[name]}


def peer(name):
    return {'namespaceSelector': {'matchLabels': {'kubernetes.io/metadata.name': NAMESPACE}},
            'podSelector': {'matchLabels': labels(name)}}


def selected(resources, kind, name):
    matches = [r for r in resources if r.get('kind') == kind and r.get('metadata', {}).get('name') == name and
               r['metadata'].get('namespace') == NAMESPACE]
    require(len(matches) == 1, 'EXACT_NATIVE_TARGET_REQUIRED')
    return matches[0]


def container(resources, name):
    deployment = selected(resources, 'Deployment', name)
    template = deployment['spec']['template']
    require(all(template['metadata']['labels'].get(k) == v for k, v in labels(name).items()), 'NATIVE_WORKLOAD_IDENTITY_MISMATCH')
    containers = [c for c in template['spec']['containers'] if c['name'] == name]
    require(len(containers) == 1, 'EXACT_NATIVE_CONTAINER_REQUIRED')
    return template, containers[0]


def append_named(items, values):
    require(not any(item.get('name') in {v['name'] for v in values} for item in items), 'NATIVE_RESOURCE_FIELD_CONFLICT')
    items.extend(copy.deepcopy(values))


def selector_matches(selector, pod_labels):
    require(isinstance(selector, dict) and set(selector) <= {'matchLabels', 'matchExpressions'}, 'NATIVE_POLICY_SELECTOR_REJECTED')
    if any(pod_labels.get(k) != v for k, v in selector.get('matchLabels', {}).items()):
        return False
    for expression in selector.get('matchExpressions', []):
        key, operation = expression.get('key'), expression.get('operator')
        values = expression.get('values', [])
        require(operation in ('In', 'NotIn', 'Exists', 'DoesNotExist'), 'NATIVE_POLICY_EXPRESSION_REJECTED')
        if operation == 'In' and (key not in pod_labels or pod_labels[key] not in values) or \
           operation == 'NotIn' and key in pod_labels and pod_labels[key] in values or \
           operation == 'Exists' and key not in pod_labels or operation == 'DoesNotExist' and key in pod_labels:
            return False
    return True


def allows_native_port(rule):
    if 'ports' not in rule:
        return True
    for port in rule['ports']:
        if port.get('protocol', 'TCP') != 'TCP':
            continue
        start = port.get('port')
        if start is None or start == 'native-connect' or type(start) is int and start <= 8085 <= port.get('endPort', start):
            return True
        # Неизвестный named port не считается доказанным ограничением.
        require(type(start) is int or start in ('connect', 'stt-connect', 'mail-connect', 'openapi-connect', 'runtime-connect', 'metrics', 'grpc', 'http', 'https'),
                'AMBIGUOUS_NATIVE_NETWORK_PORT_REJECTED')
    return False


def check_network_union(resources):
    """Полный desired union для нового8085: только точные AND peers в обе стороны."""
    for name, direction, field, other in (
            ('egress-gateway', 'ingress', 'from', 'integration-gateway'),
            ('integration-gateway', 'egress', 'to', 'egress-gateway')):
        template, _ = container(resources, name)
        for resource in resources:
            if resource.get('kind') != 'NetworkPolicy' or resource['metadata'].get('namespace') != NAMESPACE:
                continue
            spec = resource['spec']
            if not selector_matches(spec.get('podSelector', {}), template['metadata']['labels']):
                continue
            for rule in spec.get(direction, []):
                if allows_native_port(rule):
                    require(rule.get(field) == [peer(other)], 'NATIVE_NETWORK_UNION_NOT_EXACT')


def transform(resources, document):
    document = validate(document)
    require(not any(LABEL in r.get('metadata', {}).get('labels', {}) for r in resources), 'NATIVE_PROFILE_ALREADY_PRESENT')
    result = copy.deepcopy(resources)
    template, gateway = container(result, 'egress-gateway')
    _, integration = container(result, 'integration-gateway')
    expected = [v for v in gateway['env'] if v['name'] == 'EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST']
    require(expected == [{'name': 'EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST', 'value': document['gateway_policy_digest']}], 'NATIVE_BASE_POLICY_MISMATCH')
    digest = hashlib.sha256(wire(document)).hexdigest()
    generation = 'native-' + digest[:48]
    name = 'egress-gateway-native-' + digest[:20]
    require(GENERATION not in template['metadata']['labels'], 'NATIVE_GENERATION_CONFLICT')
    template['metadata']['labels'][GENERATION] = generation
    append_named(gateway['env'], [
        {'name': 'EGRESS_GATEWAY_NATIVE_CONNECT_LISTEN', 'value': ':8085'},
        {'name': 'EGRESS_GATEWAY_NATIVE_POLICY_FILE', 'value': MOUNT + '/policy.json'},
        {'name': 'EGRESS_GATEWAY_NATIVE_POLICY_DIGEST', 'value': digest}])
    append_named(gateway.setdefault('ports', []), [{'name': 'native-connect', 'containerPort': 8085, 'protocol': 'TCP'}])
    append_named(gateway.setdefault('volumeMounts', []), [{'name': 'native-policy', 'mountPath': MOUNT, 'readOnly': True}])
    append_named(template['spec'].setdefault('volumes', []), [{'name': 'native-policy', 'configMap': {'name': name, 'defaultMode': 0o444}}])
    append_named(integration['env'], [{'name': 'INTEGRATION_GATEWAY_NATIVE_PROXY_URL', 'value': PROXY}])
    metadata = lambda resource_name: {'name': resource_name, 'namespace': NAMESPACE,
        'labels': {'app.kubernetes.io/part-of': 'kodex', LABEL: 'v1'}}
    result.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': metadata(name), 'immutable': True,
                   'data': {'policy.json': wire(document).decode(), 'policy.sha256': digest}})
    result.append({'apiVersion': 'v1', 'kind': 'Service', 'metadata': metadata('egress-gateway-native'),
        'spec': {'type': 'ClusterIP', 'selector': {**labels('egress-gateway'), GENERATION: generation},
                 'ports': [{'name': 'native-connect', 'port': 8085, 'targetPort': 'native-connect', 'protocol': 'TCP'}]}})
    port = [{'protocol': 'TCP', 'port': 8085}]
    result.append({'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
        'metadata': metadata('integration-gateway-native-path'),
        'spec': {'podSelector': {'matchLabels': labels('integration-gateway')}, 'policyTypes': ['Egress'],
                 'egress': [{'to': [peer('egress-gateway')], 'ports': port}]}})
    pins = sorted({pin for d in document['destinations'] for pin in d['address_cidrs']})
    result.append({'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
        'metadata': metadata('egress-gateway-native-path'),
        'spec': {'podSelector': {'matchLabels': labels('egress-gateway')}, 'policyTypes': ['Ingress', 'Egress'],
                 'ingress': [{'from': [peer('integration-gateway')], 'ports': port}],
                 'egress': [{'to': [{'ipBlock': {'cidr': pin}} for pin in pins], 'ports': [{'protocol': 'TCP', 'port': 443}]}]}})
    identities = [(r['apiVersion'], r['kind'], r['metadata'].get('namespace'), r['metadata']['name']) for r in result]
    require(len(identities) == len(set(identities)), 'NATIVE_RESOURCE_IDENTITY_CONFLICT')
    check_network_union(result)
    return result


def check(resources):
    markers = [r for r in resources if LABEL in r.get('metadata', {}).get('labels', {})]
    if not markers:
        require(not any(r.get('metadata', {}).get('name', '').startswith('egress-gateway-native') or
                        r.get('metadata', {}).get('name') == 'integration-gateway-native-path' for r in resources),
                'UNDECLARED_NATIVE_RESOURCES_REJECTED')
        for resource in resources:
            if resource.get('kind') != 'Deployment' or resource['metadata'].get('namespace') != NAMESPACE or resource['metadata'].get('name') not in PEERS:
                continue
            template = resource['spec']['template']
            require(GENERATION not in template.get('metadata', {}).get('labels', {}), 'UNDECLARED_NATIVE_PROFILE_REJECTED')
            for target in template['spec'].get('containers', []):
                require(not any(value.get('name', '').startswith('EGRESS_GATEWAY_NATIVE_') or value.get('name') == 'INTEGRATION_GATEWAY_NATIVE_PROXY_URL' for value in target.get('env', [])) and
                        not any(v.get('containerPort') == 8085 or v.get('name') == 'native-connect' for v in target.get('ports', [])) and
                        not any(v.get('name') == 'native-policy' for v in target.get('volumeMounts', [])), 'UNDECLARED_NATIVE_PROFILE_REJECTED')
        return None
    documents = [r for r in markers if r.get('kind') == 'ConfigMap']
    require(len(documents) == 1, 'EXACT_NATIVE_CONFIGMAP_REQUIRED')
    config = documents[0]
    require(config.get('immutable') is True and set(config.get('data', {})) == {'policy.json', 'policy.sha256'}, 'IMMUTABLE_NATIVE_CONFIG_REQUIRED')
    document = validate(json.loads(config['data']['policy.json'], object_pairs_hook=unique_object))
    require(config['data']['policy.json'].encode() == wire(document) and config['data']['policy.sha256'] == hashlib.sha256(wire(document)).hexdigest(),
            'NATIVE_DOCUMENT_DIGEST_DRIFT')
    baseline = copy.deepcopy([r for r in resources if LABEL not in r.get('metadata', {}).get('labels', {})])
    template, gateway = container(baseline, 'egress-gateway')
    _, integration = container(baseline, 'integration-gateway')
    template['metadata']['labels'].pop(GENERATION, None)
    gateway['env'] = [v for v in gateway['env'] if not v['name'].startswith('EGRESS_GATEWAY_NATIVE_')]
    integration['env'] = [v for v in integration['env'] if v['name'] != 'INTEGRATION_GATEWAY_NATIVE_PROXY_URL']
    gateway['ports'] = [v for v in gateway.get('ports', []) if v['name'] != 'native-connect']
    gateway['volumeMounts'] = [v for v in gateway.get('volumeMounts', []) if v['name'] != 'native-policy']
    template['spec']['volumes'] = [v for v in template['spec'].get('volumes', []) if v['name'] != 'native-policy']
    wanted = transform(baseline, document)
    key = lambda r: (r['apiVersion'], r['kind'], r['metadata'].get('namespace', ''), r['metadata']['name'])
    require(sorted(wanted, key=key) == sorted(resources, key=key), 'NATIVE_RENDER_DRIFT')
    return document


def main():
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('--render', required=True, type=Path)
    parser.add_argument('--configuration', type=Path)
    parser.add_argument('--configuration-sha256')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    try:
        require(args.render.is_file() and not args.render.is_symlink() and args.render.stat().st_size <= 16 << 20, 'BOUNDED_NATIVE_RENDER_REQUIRED')
        resources = list(filter(None, yaml.safe_load_all(args.render.read_bytes())))
        if args.check:
            require(args.configuration is None and args.configuration_sha256 is None, 'NATIVE_CHECK_USES_RENDERED_AUTHORITY')
            check(resources)
        else:
            require(args.configuration is not None, 'NATIVE_CONFIGURATION_REQUIRED')
            result = transform(resources, load(args.configuration, args.configuration_sha256))
            check(result)
            descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
            with os.fdopen(descriptor, 'w') as stream:
                yaml.safe_dump_all(result, stream, sort_keys=False)
    except (Rejected, ValueError, OSError, KeyError, TypeError, yaml.YAMLError):
        raise SystemExit('Native integration profile rejected') from None


if __name__ == '__main__':
    main()
