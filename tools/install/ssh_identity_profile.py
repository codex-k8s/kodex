"""Опциональный direct-TLS upload SSH identity; только render, без сети и ключей.

Сертификат и DNS подготавливает оператор. TCP ingress сохраняет TLS до broker;
преобразование не читает Secret и не выдаёт setup permit либо execution capacity.
"""
import argparse
import copy
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
from urllib.parse import urlsplit

import yaml
from deployment_profile import Rejected, require
from native_integration_egress import selector_matches

SCHEMA = 'kodex.ssh-identity-upload/v1'
LABEL = 'kodex.dev/ssh-identity-upload'
CSP_LABEL = 'kodex.dev/ssh-identity-upload-csp'
NAME = 'secret-broker-ssh-upload'
NAMESPACE = 'kodex-system'
MOUNT = '/var/run/secrets/kodex/secret-broker/ssh-upload'
SELECTOR = {'app.kubernetes.io/name': 'secret-broker', 'app.kubernetes.io/component': 'secret-broker'}


def wire(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'SSH_UPLOAD_DUPLICATE_FIELD')
        result[key] = value
    return result


def dns(value):
    if not (isinstance(value, str) and len(value) <= 253 and '.' in value and all(
            re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', part) for part in value.split('.'))):
        return False
    try:
        ipaddress.ip_address(value)
    except ValueError:
        return True
    return False


def validate(document):
    require(isinstance(document, dict) and set(document) == {
        'schema', 'origin', 'browser_origin', 'tls_secret_name', 'ingress_namespace', 'ingress_pod_name'},
        'SSH_UPLOAD_EXACT_PROFILE_REQUIRED')
    require(document['schema'] == SCHEMA, 'SSH_UPLOAD_SCHEMA_REJECTED')
    hosts = []
    for key in ('origin', 'browser_origin'):
        value = document[key]
        require(isinstance(value, str) and len(value) <= 512, 'SSH_UPLOAD_ORIGIN_REJECTED')
        parsed = urlsplit(value)
        require(dns(parsed.hostname) and value == 'https://' + parsed.hostname, 'SSH_UPLOAD_ORIGIN_REJECTED')
        hosts.append(parsed.hostname)
    require(hosts[0] != hosts[1], 'SSH_UPLOAD_SEPARATE_HOST_REQUIRED')
    for key in ('tls_secret_name', 'ingress_namespace', 'ingress_pod_name'):
        require(isinstance(document[key], str) and len(document[key]) <= 63 and re.fullmatch(
            r'[a-z0-9](?:[a-z0-9-]*[a-z0-9])?', document[key]), 'SSH_UPLOAD_NAME_REJECTED')
    return copy.deepcopy(document)


def one(resources, kind, name):
    values = [r for r in resources if r.get('kind') == kind and
              r.get('metadata', {}).get('name') == name and r['metadata'].get('namespace') == NAMESPACE]
    require(len(values) == 1, 'SSH_UPLOAD_EXACT_RESOURCE_REQUIRED')
    return values[0]


def container(resources, name):
    pod = one(resources, 'Deployment', name)['spec']['template']['spec']
    values = [c for c in pod['containers'] if c.get('name') == name]
    require(len(values) == 1, 'SSH_UPLOAD_EXACT_CONTAINER_REQUIRED')
    return pod, values[0]


def peer(document):
    return {'namespaceSelector': {'matchLabels': {'kubernetes.io/metadata.name': document['ingress_namespace']}},
            'podSelector': {'matchLabels': {'app.kubernetes.io/name': document['ingress_pod_name']}}}


def endpoint_contract(resources, document):
    ingress = one(resources, 'Ingress', 'staff-control-center')
    host = urlsplit(document['browser_origin']).hostname
    require(ingress['spec']['rules'][0]['host'] == host and len(ingress['spec']['rules']) == 1,
            'SSH_UPLOAD_BROWSER_RENDER_MISMATCH')
    policy = one(resources, 'NetworkPolicy', 'staff-control-center-ingress')
    require(any(rule.get('from') == [peer(document)] for rule in policy['spec'].get('ingress', [])),
            'SSH_UPLOAD_INGRESS_RENDER_MISMATCH')
    upload_host = urlsplit(document['origin']).hostname
    for resource in resources:
        if resource.get('kind') == 'Ingress':
            require(all(rule.get('host') != upload_host for rule in resource.get('spec', {}).get('rules', [])),
                    'SSH_UPLOAD_HOST_COLLISION')
        if resource.get('kind') == 'IngressRouteTCP' and resource.get('metadata', {}).get('name') != NAME:
            require(all(upload_host not in route.get('match', '') for route in resource.get('spec', {}).get('routes', [])),
                    'SSH_UPLOAD_HOST_COLLISION')


def objects(document, source_sha):
    labels = {'app.kubernetes.io/part-of': 'kodex', LABEL: 'v1'}
    def resource(api, kind, spec):
        return {'apiVersion': api, 'kind': kind, 'metadata': {'name': NAME, 'namespace': NAMESPACE,
                'labels': copy.deepcopy(labels)}, 'spec': spec}
    service = resource('v1', 'Service', {'type': 'ClusterIP', 'selector': SELECTOR,
        'ports': [{'name': 'ssh-upload', 'port': 8446, 'targetPort': 'ssh-upload', 'protocol': 'TCP'}]})
    route = resource('traefik.io/v1alpha1', 'IngressRouteTCP', {'entryPoints': ['websecure'], 'routes': [
        {'match': 'HostSNI(`' + urlsplit(document['origin']).hostname + '`)',
         'services': [{'name': NAME, 'port': 8446}]}], 'tls': {'passthrough': True}})
    policy = resource('networking.k8s.io/v1', 'NetworkPolicy', {'podSelector': {'matchLabels': SELECTOR},
        'policyTypes': ['Ingress'], 'ingress': [{'from': [peer(document)], 'ports': [{'protocol': 'TCP', 'port': 8446}]}]})
    record = {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': NAME, 'namespace': NAMESPACE,
        'labels': copy.deepcopy(labels)}, 'immutable': True, 'data': {'profile.json': wire(document).decode(),
        'profileSHA256': hashlib.sha256(wire(document)).hexdigest(), 'sourceSHA': source_sha}}
    return [service, route, policy, record]


def settings(document):
    return {'secret-broker': {
        'SECRET_BROKER_SSH_IDENTITY_NAMESPACE': 'kodex-ssh-identities',
        'SECRET_BROKER_SSH_UPLOAD_LISTEN': ':8446', 'SECRET_BROKER_SSH_UPLOAD_ORIGIN': document['origin'],
        'SECRET_BROKER_SSH_UPLOAD_BROWSER_ORIGIN': document['browser_origin'],
        'SECRET_BROKER_SSH_UPLOAD_CERTIFICATE_FILE': MOUNT + '/tls.crt',
        'SECRET_BROKER_SSH_UPLOAD_PRIVATE_KEY_FILE': MOUNT + '/tls.key'},
        'control-plane': {'CONTROL_PLANE_SSH_IDENTITY_UPLOAD_ORIGIN': document['origin'],
                          'CONTROL_PLANE_SSH_IDENTITY_UPLOAD_TLS_SERVER_NAME': urlsplit(document['origin']).hostname}}


def frontend_runtime(resources):
    values = [r for r in resources if r.get('kind') == 'ConfigMap' and
              r.get('metadata', {}).get('namespace') == NAMESPACE and
              {'runtime-config.json', 'security-headers.conf'} <= set(r.get('data', {}))]
    require(len(values) == 1 and values[0].get('immutable') is True, 'SSH_UPLOAD_FRONTEND_RUNTIME_REQUIRED')
    return values[0]


def frontend_headers(runtime, document, enabled):
    config = json.loads(runtime['data']['runtime-config.json'], object_pairs_hook=unique_object)
    authority = urlsplit(config['oidc']['authority'])
    require(authority.scheme == 'https' and dns(authority.hostname) and authority.netloc == authority.hostname,
            'SSH_UPLOAD_OIDC_ORIGIN_REJECTED')
    template = (Path(__file__).resolve().parents[2]/'deploy/k8s/base/staff-control-center/security-headers.conf').read_text()
    require(template.count('__KODEX_OIDC_ORIGIN__') == 1 and template.count("connect-src 'self'") == 1,
            'SSH_UPLOAD_CANONICAL_CSP_REQUIRED')
    origins = 'https://' + authority.hostname
    if enabled:
        require(document['origin'] != origins, 'SSH_UPLOAD_SEPARATE_OIDC_HOST_REQUIRED')
        origins += ' ' + document['origin']
    return template.replace('__KODEX_OIDC_ORIGIN__', origins)


def frontend_config_name(runtime):
    return 'staff-control-center-runtime-ssh-' + hashlib.sha256(wire(runtime['data'])).hexdigest()[:16]


def project_frontend(resources, document):
    runtime = frontend_runtime(resources)
    require(runtime['data']['security-headers.conf'] == frontend_headers(runtime, document, False),
            'SSH_UPLOAD_FRONTEND_CSP_PREIMAGE_REJECTED')
    old_name = runtime['metadata']['name']
    runtime['data']['security-headers.conf'] = frontend_headers(runtime, document, True)
    runtime['metadata']['name'] = frontend_config_name(runtime)
    runtime['metadata'].setdefault('labels', {})[CSP_LABEL] = 'v1'
    pod, _ = container(resources, 'staff-control-center')
    references = [v['configMap'] for v in pod.get('volumes', []) if v.get('configMap', {}).get('name') == old_name]
    require(len(references) == 1, 'SSH_UPLOAD_FRONTEND_REFERENCE_REQUIRED')
    references[0]['name'] = runtime['metadata']['name']


def transform(resources, document, source_sha):
    document = validate(document)
    require(isinstance(source_sha, str) and re.fullmatch('[a-f0-9]{40}', source_sha), 'SSH_UPLOAD_SOURCE_REQUIRED')
    require(not any(LABEL in r.get('metadata', {}).get('labels', {}) for r in resources), 'SSH_UPLOAD_PROFILE_ALREADY_PRESENT')
    result = copy.deepcopy(resources)
    endpoint_contract(result, document)
    additions = objects(document, source_sha)
    identities = {(r['apiVersion'], r['kind'], r['metadata'].get('namespace'), r['metadata']['name']) for r in result}
    require(all((r['apiVersion'], r['kind'], NAMESPACE, NAME) not in identities for r in additions), 'SSH_UPLOAD_RESOURCE_COLLISION')
    for name, env in settings(document).items():
        pod, client = container(result, name)
        require(not any(e.get('name', '').startswith(('SECRET_BROKER_SSH_UPLOAD_', 'CONTROL_PLANE_SSH_IDENTITY_UPLOAD_'))
                        or e.get('name') == 'SECRET_BROKER_SSH_IDENTITY_NAMESPACE' for e in client.get('env', [])),
                'SSH_UPLOAD_CONFIG_COLLISION')
        client.setdefault('env', []).extend({'name': key, 'value': value} for key, value in env.items())
        if name == 'secret-broker':
            require(not any(v.get('name') == 'ssh-upload-tls' for v in pod.get('volumes', [])) and
                    not any(v.get('name') == 'ssh-upload-tls' or v.get('mountPath') == MOUNT for c in pod['containers']
                            for v in c.get('volumeMounts', [])) and
                    not any(p.get('name') == 'ssh-upload' or p.get('containerPort') == 8446 for c in pod['containers']
                            for p in c.get('ports', [])), 'SSH_UPLOAD_MOUNT_PORT_COLLISION')
            pod.setdefault('volumes', []).append({'name': 'ssh-upload-tls', 'secret': {'secretName': document['tls_secret_name'],
                'defaultMode': 288, 'items': [{'key': 'tls.crt', 'path': 'tls.crt'}, {'key': 'tls.key', 'path': 'tls.key'}]}})
            client.setdefault('volumeMounts', []).append({'name': 'ssh-upload-tls', 'mountPath': MOUNT, 'readOnly': True})
            client.setdefault('ports', []).append({'name': 'ssh-upload', 'containerPort': 8446, 'protocol': 'TCP'})
    result.extend(additions)
    project_frontend(result, document)
    validate_render(result)
    return result


def validate_render(resources):
    # EnvFrom не должен скрывать незаявленный listener либо подменять источник typed profile.
    for resource in resources:
        if resource.get('kind') == 'ConfigMap':
            require(not any(key.startswith(('SECRET_BROKER_SSH_UPLOAD_', 'CONTROL_PLANE_SSH_IDENTITY_UPLOAD_')) or
                key == 'SECRET_BROKER_SSH_IDENTITY_NAMESPACE' for key in resource.get('data', {})), 'SSH_UPLOAD_CONFIGMAP_ENV_REJECTED')
    records = [r for r in resources if r.get('kind') == 'ConfigMap' and r.get('metadata', {}).get('name') == NAME]
    enabled = any(LABEL in r.get('metadata', {}).get('labels', {}) or CSP_LABEL in r.get('metadata', {}).get('labels', {}) for r in resources)
    if not records and not enabled:
        require(not any(r.get('metadata', {}).get('name') == NAME for r in resources), 'SSH_UPLOAD_PARTIAL_ASSETS')
        for resource in resources:
            if resource.get('kind') != 'Deployment' or resource.get('metadata', {}).get('name') not in ('secret-broker', 'control-plane'):
                continue
            for client in resource.get('spec', {}).get('template', {}).get('spec', {}).get('containers', []):
                require(not any(e.get('name', '').startswith(('SECRET_BROKER_SSH_UPLOAD_', 'CONTROL_PLANE_SSH_IDENTITY_UPLOAD_'))
                    or e.get('name') == 'SECRET_BROKER_SSH_IDENTITY_NAMESPACE' for e in client.get('env', [])), 'SSH_UPLOAD_PROFILE_REQUIRED')
        return
    require(len(records) == 1, 'SSH_UPLOAD_EXACT_RECORD_REQUIRED')
    record = records[0]
    data = record.get('data', {})
    require(set(data) == {'profile.json', 'profileSHA256', 'sourceSHA'}, 'SSH_UPLOAD_RECORD_FIELDS_REJECTED')
    document = validate(json.loads(data['profile.json'], object_pairs_hook=unique_object))
    source = data['sourceSHA']
    require(isinstance(source, str) and re.fullmatch('[a-f0-9]{40}', source), 'SSH_UPLOAD_SOURCE_REQUIRED')
    expected = objects(document, source)
    marked = [r for r in resources if LABEL in r.get('metadata', {}).get('labels', {})]
    require(len(marked) == len(expected) and all(marked.count(r) == 1 for r in expected), 'SSH_UPLOAD_OBJECT_DRIFT')
    endpoint_contract(resources, document)
    runtime = frontend_runtime(resources)
    require(runtime['metadata'].get('labels', {}).get(CSP_LABEL) == 'v1' and
            runtime['metadata']['name'] == frontend_config_name(runtime) and
            runtime['data']['security-headers.conf'] == frontend_headers(runtime, document, True),
            'SSH_UPLOAD_FRONTEND_CSP_DRIFT')
    frontend, _ = container(resources, 'staff-control-center')
    require(sum(v.get('configMap', {}).get('name') == runtime['metadata']['name'] for v in frontend.get('volumes', [])) == 1,
            'SSH_UPLOAD_FRONTEND_REFERENCE_DRIFT')
    for name, env in settings(document).items():
        pod, client = container(resources, name)
        actual = [e for e in client.get('env', []) if e.get('name') in env or e.get('name', '').startswith(
            ('SECRET_BROKER_SSH_UPLOAD_', 'CONTROL_PLANE_SSH_IDENTITY_UPLOAD_'))]
        require(actual == [{'name': key, 'value': value} for key, value in env.items()], 'SSH_UPLOAD_ENV_DRIFT')
        if name == 'secret-broker':
            require([v for v in pod.get('volumes', []) if v.get('name') == 'ssh-upload-tls'] == [
                {'name': 'ssh-upload-tls', 'secret': {'secretName': document['tls_secret_name'], 'defaultMode': 288,
                 'items': [{'key': 'tls.crt', 'path': 'tls.crt'}, {'key': 'tls.key', 'path': 'tls.key'}]}}], 'SSH_UPLOAD_TLS_VOLUME_DRIFT')
            require([v for c in pod['containers'] for v in c.get('volumeMounts', []) if v.get('name') == 'ssh-upload-tls'] == [
                {'name': 'ssh-upload-tls', 'mountPath': MOUNT, 'readOnly': True}] and
                any(v.get('name') == 'ssh-upload-tls' for v in client.get('volumeMounts', [])), 'SSH_UPLOAD_TLS_CONSUMER_DRIFT')
            require([p for p in client.get('ports', []) if p.get('name') == 'ssh-upload' or p.get('containerPort') == 8446] == [
                {'name': 'ssh-upload', 'containerPort': 8446, 'protocol': 'TCP'}], 'SSH_UPLOAD_PORT_DRIFT')
    # NetworkPolicy правила складываются; посторонний allow на этот порт отменил бы narrow peer.
    pod, _ = container(resources, 'secret-broker')
    named = {p.get('name') for c in pod['containers'] for p in c.get('ports', [])
             if p.get('containerPort') == 8446 and p.get('protocol', 'TCP') == 'TCP'}
    for r in resources:
        if r.get('kind') != 'NetworkPolicy' or r['metadata'].get('namespace') != NAMESPACE:
            continue
        selector = r['spec'].get('podSelector', {})
        labels = one(resources, 'Deployment', 'secret-broker')['spec']['template']['metadata']['labels']
        if not selector_matches(selector, labels):
            continue
        for rule in r['spec'].get('ingress', []):
            ports = rule.get('ports', [])
            allowed = not ports or any(p.get('protocol', 'TCP') == 'TCP' and (p.get('port') is None or p.get('port') in named or
                type(p.get('port')) is int and p['port'] <= 8446 <= p.get('endPort', p['port'])) for p in ports)
            if allowed:
                require(rule.get('from') == [peer(document)], 'SSH_UPLOAD_NETWORK_UNION_WIDENED')


def main():
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--configuration', type=Path)
    parser.add_argument('--configuration-sha256')
    parser.add_argument('--source-sha')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink() and args.render.stat().st_size <= 32 << 20, 'SSH_UPLOAD_RENDER_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(args.render.read_bytes())))
    if args.check:
        require(args.configuration is None and args.configuration_sha256 is None and args.source_sha is None, 'SSH_UPLOAD_CHECK_USES_RENDER')
        validate_render(resources)
        return
    require(args.configuration is not None and args.configuration.is_file() and not args.configuration.is_symlink() and
            args.configuration.stat().st_size <= 4096, 'SSH_UPLOAD_CONFIGURATION_REQUIRED')
    raw = args.configuration.read_bytes()
    require(len(raw) <= 4096 and hashlib.sha256(raw).hexdigest() == args.configuration_sha256, 'SSH_UPLOAD_CONFIGURATION_DIGEST_MISMATCH')
    result = transform(resources, json.loads(raw, object_pairs_hook=unique_object), args.source_sha)
    fd = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(fd, 'w') as stream:
        yaml.safe_dump_all(result, stream, sort_keys=False)


if __name__ == '__main__':
    try:
        main()
    except (Rejected, ValueError, OSError, KeyError, TypeError, yaml.YAMLError):
        raise SystemExit('SSH identity upload profile rejected') from None
