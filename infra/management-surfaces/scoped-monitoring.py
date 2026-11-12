#!/usr/bin/env python3
"""Закрытый профиль Kodex для закреплённого Helm chart, без host collectors и чужих Secrets."""
import argparse
import copy
import ipaddress
import json
from pathlib import Path
import re
import sys

import yaml

ROOT = Path(__file__).resolve().parents[2]
NAMESPACE = 'observability'
SYSTEM = 'kodex-system'
PREFIX = 'kodex-monitoring-kube-prom-'
SOURCES = {
    'operator': 'quay.io/prometheus-operator/prometheus-operator:v0.93.1',
    'reloader': 'quay.io/prometheus-operator/prometheus-config-reloader:v0.93.1',
    'prometheus': 'quay.io/prometheus/prometheus:v3.14.0-distroless',
    'alertmanager': 'quay.io/prometheus/alertmanager:v0.34.0',
    'grafana': 'docker.io/grafana/grafana:13.2.0',
    'sidecar': 'quay.io/kiwigrid/k8s-sidecar:2.10.1',
}


def require(value):
    if not value:
        raise ValueError('SCOPED_MONITORING_CONTRACT_REJECTED')


def images():
    lock = json.loads(Path(__file__).with_name('scoped-monitoring-images.lock.json').read_text())
    require(set(lock) == {'schema', 'chartVersion', 'images'} and
            lock['schema'] == 'kodex.scoped-monitoring-images/v1' and lock['chartVersion'] == '88.5.4' and
            set(lock['images']) == set(SOURCES))
    for key, source in SOURCES.items():
        item = lock['images'][key]
        require(set(item) == {'source', 'digest'} and item['source'] == source and
                re.fullmatch(r'sha256:[a-f0-9]{64}', item['digest']))
    return lock['images']


def image_values(key):
    item = images()[key]
    repository, tag = item['source'].rsplit(':', 1)
    registry, repository = repository.split('/', 1)
    return {'registry': registry, 'repository': repository, 'tag': tag, 'sha': item['digest'].removeprefix('sha256:')}


def values(base):
    require(isinstance(base, dict) and base.get('grafana', {}).get('admin', {}).get('existingSecret') == 'grafana-admin')
    result = copy.deepcopy(base)
    result.update({'crds': {'enabled': False}, 'defaultRules': {'create': False},
                   'global': {'rbac': {'create': False, 'createAggregateClusterRoles': False}},
                   'kubernetesServiceMonitors': {'enabled': False}, 'kubeApiServer': {'enabled': False},
                   'kubelet': {'enabled': False}, 'kubeStateMetrics': {'enabled': False}, 'nodeExporter': {'enabled': False}})
    operator = result.setdefault('prometheusOperator', {})
    operator.update({'image': image_values('operator'), 'namespaces': {'releaseNamespace': True},
                     'kubeletService': {'enabled': False}, 'kubeletEndpointsEnabled': False,
                     'kubeletEndpointSliceEnabled': False,
                     'prometheusInstanceNamespaces': [NAMESPACE], 'alertmanagerInstanceNamespaces': [NAMESPACE],
                     'alertmanagerConfigNamespaces': [NAMESPACE], 'thanosRulerInstanceNamespaces': [NAMESPACE],
                     'serviceMonitor': {'selfMonitor': False},
                     'prometheusConfigReloader': {'image': image_values('reloader'), 'resources': {
                         'requests': {'cpu': '10m', 'memory': '32Mi'}, 'limits': {'memory': '128Mi'}}},
                     'admissionWebhooks': {'failurePolicy': 'Fail', 'namespaceSelector': {
                         'matchLabels': {'kubernetes.io/metadata.name': NAMESPACE}},
                         'patch': {'enabled': False}, 'certManager': {'enabled': True},
                         'deployment': {'image': image_values('operator')}}})
    grafana = result['grafana']
    grafana.update({'image': image_values('grafana'), 'rbac': {'create': False},
                   'initChownData': {'enabled': False}, 'testFramework': {'enabled': False},
                   'defaultDashboardsEnabled': False, 'serviceMonitor': {'enabled': False}})
    grafana['sidecar']['image'] = image_values('sidecar')
    grafana['sidecar']['dashboards'].update({'searchNamespace': [NAMESPACE, SYSTEM], 'resource': 'configmap'})
    grafana['sidecar']['datasources'].update({'searchNamespace': NAMESPACE, 'resource': 'configmap'})
    for key in ['prometheus', 'alertmanager']:
        result[key]['serviceMonitor'] = {'selfMonitor': False}
        spec = result[key][key + 'Spec']
        spec['image'] = image_values(key)
    prometheus = result['prometheus']['prometheusSpec']
    for kind in ['serviceMonitor', 'podMonitor', 'probe', 'scrapeConfig', 'rule']:
        prometheus[kind + 'SelectorNilUsesHelmValues'] = False
        prometheus[kind + 'NamespaceSelector'] = {'matchLabels': {'kubernetes.io/metadata.name': NAMESPACE}}
        prometheus[kind + 'Selector'] = {'matchLabels': {'kodex.dev/monitoring-scope': 'kodex'}}
    # Один собственный monitor с точным target namespace; произвольные внешние selectors не допускаются.
    prometheus['ignoreNamespaceSelectors'] = False
    # Chart имеет bool default, а CRD требует объект; post-render назначает точный объект.
    prometheus['arbitraryFSAccessThroughSMs'] = None
    return result


def metadata(name, namespace=NAMESPACE):
    return {'name': name, **({'namespace': namespace} if namespace else {}),
            'labels': {'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/monitoring-scope': 'kodex'}}


def role(name, namespace, rules, account):
    return [
        {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'Role', 'metadata': metadata(name, namespace), 'rules': rules},
        {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'RoleBinding', 'metadata': metadata(name, namespace),
         'roleRef': {'apiGroup': 'rbac.authorization.k8s.io', 'kind': 'Role', 'name': name},
         'subjects': [{'kind': 'ServiceAccount', 'name': account, 'namespace': NAMESPACE}]},
    ]


def rule(group, resources, verbs):
    return {'apiGroups': [group], 'resources': resources, 'verbs': verbs}


def access():
    read = ['get', 'list', 'watch']
    operator = PREFIX + 'operator'
    result = role('kodex-monitoring-operator', NAMESPACE, [
        rule('monitoring.coreos.com', ['alertmanagers', 'alertmanagers/finalizers', 'alertmanagers/status',
             'alertmanagerconfigs', 'prometheuses', 'prometheuses/finalizers', 'prometheuses/status',
             'prometheusagents', 'prometheusagents/finalizers', 'prometheusagents/status', 'thanosrulers',
             'thanosrulers/finalizers', 'thanosrulers/status', 'scrapeconfigs', 'scrapeconfigs/status',
             'servicemonitors', 'servicemonitors/status', 'podmonitors', 'podmonitors/status',
             'probes', 'probes/status', 'prometheusrules', 'prometheusrules/status'], ['*']),
        rule('apps', ['statefulsets'], ['*']), rule('', ['configmaps', 'secrets'], ['*']),
        rule('', ['pods'], ['list', 'delete']),
        rule('', ['services', 'services/finalizers', 'endpoints'], ['get', 'create', 'update', 'delete']),
        rule('', ['events'], ['patch', 'create']), rule('events.k8s.io', ['events'], ['patch', 'create']),
        rule('networking.k8s.io', ['ingresses'], read),
        rule('discovery.k8s.io', ['endpointslices'], read + ['create', 'update', 'delete']),
    ], operator)
    result.extend([
        {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'ClusterRole',
         'metadata': metadata('kodex-monitoring-namespace-reader', None),
         'rules': [rule('', ['namespaces'], read), rule('storage.k8s.io', ['storageclasses'], ['get'])]},
        {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'ClusterRoleBinding',
         'metadata': metadata('kodex-monitoring-namespace-reader', None),
         'roleRef': {'apiGroup': 'rbac.authorization.k8s.io', 'kind': 'ClusterRole', 'name': 'kodex-monitoring-namespace-reader'},
         'subjects': [{'kind': 'ServiceAccount', 'name': operator, 'namespace': NAMESPACE}]},
    ])
    for namespace in [NAMESPACE, SYSTEM]:
        result += role('kodex-monitoring-discovery', namespace, [rule('', ['services', 'endpoints', 'pods'], read),
                       rule('discovery.k8s.io', ['endpointslices'], read)], PREFIX + 'prometheus')
        result += role('kodex-monitoring-dashboards', namespace, [rule('', ['configmaps'], read)], 'kodex-monitoring-grafana')
    return result


def policy(name, selector, ingress=None, egress=None):
    spec = {'podSelector': {'matchLabels': selector}, 'policyTypes': ['Ingress', 'Egress'],
            'ingress': ingress or [], 'egress': egress or []}
    return {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy', 'metadata': metadata(name), 'spec': spec}


def peer(namespace, labels):
    return {'namespaceSelector': {'matchLabels': {'kubernetes.io/metadata.name': namespace}},
            'podSelector': {'matchLabels': labels}}


def path(direction, targets, ports):
    return {direction: targets, 'ports': [{'protocol': protocol, 'port': port} for protocol, port in ports]}


def policies(service, endpoints, ports):
    require(len(endpoints) in range(1, 17) and len(ports) in range(1, 9))
    for value in [service] + endpoints:
        address = ipaddress.ip_network(value, strict=True)
        require(address.prefixlen == address.max_prefixlen)
    require(all(1 <= port <= 65535 for port in ports))
    api = [path('to', [{'ipBlock': {'cidr': service}}], [('TCP', 443)]),
           path('to', [{'ipBlock': {'cidr': value}} for value in endpoints], [('TCP', port) for port in ports])]
    dns = path('to', [peer('kube-system', {'k8s-app': 'kube-dns'})], [('TCP', 53), ('UDP', 53)])
    grafana = {'app.kubernetes.io/name': 'grafana', 'app.kubernetes.io/instance': 'kodex-monitoring'}
    prometheus = {'app.kubernetes.io/name': 'prometheus', 'prometheus': PREFIX + 'prometheus'}
    alertmanager = {'app.kubernetes.io/name': 'alertmanager', 'alertmanager': PREFIX + 'alertmanager'}
    operator = {'app.kubernetes.io/name': 'kube-prometheus-stack-prometheus-operator', 'release': 'kodex-monitoring'}
    return [
        policy('kodex-monitoring-deny-all', {}),
        policy('kodex-monitoring-operator-paths', operator,
               [path('from', [{'ipBlock': {'cidr': value}} for value in endpoints], [('TCP', 10250)])], api + [dns]),
        policy('kodex-monitoring-prometheus-paths', prometheus,
               [path('from', [peer(NAMESPACE, grafana)], [('TCP', 9090)])], api + [dns,
                path('to', [peer(SYSTEM, {'app.kubernetes.io/name': 'control-plane', 'app.kubernetes.io/component': 'internal-service'})], [('TCP', 9090)]),
                path('to', [peer(NAMESPACE, alertmanager)], [('TCP', 9093)])]),
        policy('kodex-monitoring-grafana-paths', grafana, egress=api + [dns,
               path('to', [peer(NAMESPACE, prometheus)], [('TCP', 9090)])]),
        policy('kodex-monitoring-alertmanager-paths', alertmanager,
               [path('from', [peer(NAMESPACE, prometheus)], [('TCP', 9093)]),
                path('from', [peer(NAMESPACE, alertmanager)], [('TCP', 9094), ('UDP', 9094)])],
               [dns, path('to', [peer(NAMESPACE, alertmanager)], [('TCP', 9094), ('UDP', 9094)])]),
    ]


def postrender(resources, service, endpoints, ports):
    require(resources and len(resources) < 128)
    pins = {value['source'] + '@' + value['digest'] for value in images().values()}
    seen = set()
    for resource in resources:
        kind, meta = resource['kind'], resource['metadata']
        identity = (kind, meta.get('namespace', NAMESPACE), meta['name'])
        require(identity not in seen)
        seen.add(identity)
        require(kind not in ['CustomResourceDefinition', 'DaemonSet', 'ClusterRole', 'ClusterRoleBinding', 'Role', 'RoleBinding'])
        if kind in ['MutatingWebhookConfiguration', 'ValidatingWebhookConfiguration']:
            require(meta['name'] == PREFIX + 'admission')
            # Chart 88.5.4 выводит пустой matchExpressions рядом с exact matchLabels.
            for webhook in resource['webhooks']:
                selector = webhook.get('namespaceSelector', {})
                if 'matchExpressions' in selector and selector['matchExpressions'] is None:
                    del selector['matchExpressions']
            require(all(w.get('namespaceSelector') == {'matchLabels': {'kubernetes.io/metadata.name': NAMESPACE}}
                        and w.get('failurePolicy') == 'Fail' for w in resource['webhooks']))
        else:
            require(meta.get('namespace', NAMESPACE) == NAMESPACE)
        pod = resource.get('spec', {}).get('template', {}).get('spec', {})
        require(not any(pod.get(key) for key in ['hostNetwork', 'hostPID', 'hostIPC']) and
                all('hostPath' not in volume for volume in pod.get('volumes', [])))
        for container in pod.get('containers', []) + pod.get('initContainers', []):
            require(container['image'] in pins and not container.get('securityContext', {}).get('privileged'))
        if kind == 'Deployment':
            require(meta['name'] == PREFIX + 'operator')
            args = pod['containers'][0]['args']
            require('--namespaces=observability' in args and
                    not any(arg.startswith('--kubelet-') and arg not in
                            ['--kubelet-endpoints=false', '--kubelet-endpointslice=false'] for arg in args) and
                    '--prometheus-config-reloader=' + images()['reloader']['source'] + '@' +
                    images()['reloader']['digest'] in args)
        if kind in ['Prometheus', 'Alertmanager']:
            require(resource['spec']['image'] in pins)
        if kind == 'Prometheus':
            require('arbitraryFSAccessThroughSMs' not in resource['spec'])
            resource['spec']['arbitraryFSAccessThroughSMs'] = {'deny': True}
            for key in ['serviceMonitor', 'podMonitor', 'probe', 'scrapeConfig', 'rule']:
                require(resource['spec'][key + 'NamespaceSelector'] == {'matchLabels': {'kubernetes.io/metadata.name': NAMESPACE}} and
                        resource['spec'][key + 'Selector'] == {'matchLabels': {'kodex.dev/monitoring-scope': 'kodex'}})
        require(kind not in ['ServiceMonitor', 'PodMonitor', 'Probe', 'ScrapeConfig', 'PrometheusRule'])
    require(any(r['kind'] == 'Prometheus' for r in resources) and any(r['kind'] == 'StatefulSet' for r in resources))
    monitor = yaml.safe_load((ROOT / 'deploy/k8s/base/control-plane/servicemonitor.yaml').read_text())
    require(monitor['spec']['namespaceSelector'] == {'matchNames': [SYSTEM]})
    monitor['metadata'] = metadata('kodex-control-plane')
    return resources + access() + policies(service, endpoints, ports) + [monitor]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['values', 'postrender'])
    parser.add_argument('--api-service-cidr')
    parser.add_argument('--api-endpoint-cidrs')
    parser.add_argument('--api-endpoint-ports')
    args = parser.parse_args()
    raw = sys.stdin.read(8 << 20)
    require(len(raw) < 8 << 20)
    if args.mode == 'values':
        yaml.safe_dump(values(yaml.safe_load(raw)), sys.stdout, sort_keys=False)
    else:
        result = postrender(list(filter(None, yaml.safe_load_all(raw))), args.api_service_cidr,
                            args.api_endpoint_cidrs.split(','), [int(p) for p in args.api_endpoint_ports.split(',')])
        yaml.safe_dump_all(result, sys.stdout, sort_keys=False)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, TypeError, KeyError, AttributeError, OSError, yaml.YAMLError):
        print('Scoped monitoring profile rejected', file=sys.stderr)
        sys.exit(1)
