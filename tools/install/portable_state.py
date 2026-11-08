"""Exact readback и fenced mutations локального delivery; секреты не выводятся."""
import argparse
import copy
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import urllib.parse

import yaml
from deployment_profile import require, Rejected

CAS_TARGETS = {
    ('supplychain.kodex.dev/v1alpha1', 'ImageAdmissionPolicyParameters'): 'imageadmissionpolicyparameters.supplychain.kodex.dev',
    ('v1', 'ConfigMap'): 'configmaps',
    ('apps/v1', 'Deployment'): 'deployments.apps',
    ('batch/v1', 'CronJob'): 'cronjobs.batch',
    ('networking.k8s.io/v1', 'NetworkPolicy'): 'networkpolicies.networking.k8s.io',
    ('admissionregistration.k8s.io/v1', 'ValidatingAdmissionPolicy'): 'validatingadmissionpolicies.admissionregistration.k8s.io',
    ('admissionregistration.k8s.io/v1', 'ValidatingAdmissionPolicyBinding'): 'validatingadmissionpolicybindings.admissionregistration.k8s.io',
}


def key(resource):
    m = resource['metadata']
    return '|'.join((resource['apiVersion'], resource['kind'], m.get('namespace', ''), m['name']))


def descriptor(resource):
    return {k: copy.deepcopy(resource[k]) for k in ('apiVersion', 'kind', 'metadata')} | {
        'metadata': {k: v for k, v in resource['metadata'].items() if k in ('namespace', 'name')}}


def seal(resource):
    metadata = resource.get('metadata', {})
    value = {k: v for k, v in resource.items() if k not in ('metadata', 'status')}
    if resource.get('kind') == 'Lease' and metadata.get('name') == 'image-admission-controller-leader':
        value.pop('spec', None)
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


def strip_defaults(expected, actual, defaults):
    value = copy.deepcopy(actual)
    if not isinstance(expected, dict) or not isinstance(value, dict):
        return value
    for name, default in defaults.items():
        if name not in expected:
            if isinstance(default, dict) and isinstance(value.get(name), dict):
                value[name] = strip_defaults({}, value[name], default)
                if value[name] == {}:
                    value.pop(name)
            elif value.get(name) == default:
                value.pop(name, None)
        elif name not in value and expected[name] == default:
            value[name] = copy.deepcopy(default)
        elif isinstance(default, dict) and name in value:
            value[name] = strip_defaults(expected[name], value[name], default)
    return value


def normalized_pod(expected, actual):
    pod_defaults = {'dnsPolicy': 'ClusterFirst', 'restartPolicy': 'Always', 'schedulerName': 'default-scheduler',
        'terminationGracePeriodSeconds': 30, 'enableServiceLinks': True,
        'securityContext': {'fsGroupChangePolicy': 'Always', 'supplementalGroupsPolicy': 'Merge'},
        'serviceAccount': 'default', 'serviceAccountName': 'default', 'hostNetwork': False,
        'hostPID': False, 'hostIPC': False, 'shareProcessNamespace': False, 'preemptionPolicy': 'PreemptLowerPriority'}
    value = strip_defaults(expected, actual, pod_defaults)
    require(isinstance(value, dict), 'LIVE_POD_SPEC_REJECTED')
    if 'serviceAccount' not in expected and value.get('serviceAccount') == expected.get('serviceAccountName'):
        value.pop('serviceAccount', None)
    for field in ('containers', 'initContainers', 'ephemeralContainers'):
        require(len(value.get(field, [])) == len(expected.get(field, [])), 'LIVE_CONTAINER_SET_DRIFT')
        for index, container in enumerate(value.get(field, [])):
            wanted = expected[field][index]
            defaults = {'terminationMessagePath': '/dev/termination-log', 'terminationMessagePolicy': 'File',
                'imagePullPolicy': 'IfNotPresent', 'resources': {},
                'command': [], 'args': [],
                'securityContext': {'procMount': 'Default', 'privileged': False, 'capabilities': {}}, 'stdin': False, 'stdinOnce': False, 'tty': False}
            for probe in ('livenessProbe', 'readinessProbe', 'startupProbe'):
                defaults[probe] = {'timeoutSeconds': 1, 'periodSeconds': 10, 'successThreshold': 1,
                                   'failureThreshold': 3, 'httpGet': {'scheme': 'HTTP'}}
            result = strip_defaults(wanted, container, defaults)
            for port_index, port in enumerate(result.get('ports', [])):
                wanted_ports = wanted.get('ports', [])
                require(port_index < len(wanted_ports), 'LIVE_CONTAINER_PORT_DRIFT')
                result['ports'][port_index] = strip_defaults(wanted_ports[port_index], port, {'protocol': 'TCP'})
            require(isinstance(result.get('env', []), list) and len(result.get('env', [])) == len(wanted.get('env', [])), 'LIVE_ENV_SET_DRIFT')
            for env_index, env in enumerate(result.get('env', [])):
                wanted_env = wanted.get('env', [])
                require(env_index < len(wanted_env), 'LIVE_ENV_SET_DRIFT')
                result['env'][env_index] = strip_defaults(wanted_env[env_index], env, {
                    'valueFrom': {'fieldRef': {'apiVersion': 'v1'}, 'resourceFieldRef': {'divisor': '1'}}})
            value[field][index] = result
    for index, volume in enumerate(value.get('volumes', [])):
        wanted = expected.get('volumes', [])
        require(index < len(wanted), 'LIVE_POD_VOLUME_DRIFT')
        value['volumes'][index] = strip_defaults(wanted[index], volume, {
            'configMap': {'defaultMode': 420}, 'secret': {'defaultMode': 420},
            'projected': {'defaultMode': 420}, 'downwardAPI': {'defaultMode': 420}, 'hostPath': {'type': ''}})
        normalized = value['volumes'][index]
        if 'emptyDir' in wanted[index] and 'emptyDir' in normalized:
            normalized['emptyDir'] = strip_defaults(wanted[index]['emptyDir'], normalized['emptyDir'], {'medium': ''})
        if 'downwardAPI' in normalized:
            normalized['downwardAPI'] = normalized_downward_api(wanted[index].get('downwardAPI', {}), normalized['downwardAPI'])
        sources = normalized.get('projected', {}).get('sources', [])
        wanted_sources = wanted[index].get('projected', {}).get('sources', [])
        require(len(sources) == len(wanted_sources), 'LIVE_PROJECTED_SOURCE_SET_DRIFT')
        for source_index, source in enumerate(sources):
            if 'downwardAPI' in source:
                source['downwardAPI'] = normalized_downward_api(wanted_sources[source_index].get('downwardAPI', {}), source['downwardAPI'])
    return value


def normalized_downward_api(expected, actual):
    """Канонизирует только fieldRef.apiVersion=v1, сохраняя состав и все пути."""
    result = copy.deepcopy(actual)
    wanted_items = expected.get('items', [])
    require(len(result.get('items', [])) == len(wanted_items), 'LIVE_DOWNWARD_API_ITEM_SET_DRIFT')
    for index, item in enumerate(result.get('items', [])):
        result['items'][index] = strip_defaults(wanted_items[index], item, {'fieldRef': {'apiVersion': 'v1'}})
    return result


def normalized_claim_templates(expected, actual):
    require(isinstance(expected, list) and isinstance(actual, list) and len(expected) == len(actual), 'LIVE_CLAIM_TEMPLATE_DRIFT')
    result = []
    for wanted, template in zip(expected, actual):
        value = strip_defaults(wanted, template, {'apiVersion': 'v1', 'kind': 'PersistentVolumeClaim'})
        value['metadata'] = strip_defaults(wanted.get('metadata', {}), value.get('metadata', {}), {'creationTimestamp': None})
        value['spec'] = strip_defaults(wanted['spec'], value['spec'], {'volumeMode': 'Filesystem'})
        if 'status' not in wanted and value.get('status') in ({}, {'phase': 'Pending'}):
            value.pop('status', None)
        result.append(value)
    return result


def workload_spec(expected, actual, kind, uid=None, name=None):
    defaults = {'replicas': 1, 'revisionHistoryLimit': 10, 'minReadySeconds': 0}
    if kind == 'Deployment':
        defaults.update(progressDeadlineSeconds=600, strategy={
            'type': 'RollingUpdate', 'rollingUpdate': {'maxSurge': '25%', 'maxUnavailable': '25%'}})
    elif kind == 'StatefulSet':
        defaults.update(podManagementPolicy='OrderedReady', updateStrategy={'type': 'RollingUpdate', 'rollingUpdate': {'partition': 0}},
            persistentVolumeClaimRetentionPolicy={'whenDeleted': 'Retain', 'whenScaled': 'Retain'}, ordinals={'start': 0})
    elif kind == 'DaemonSet':
        defaults.update(updateStrategy={'type': 'RollingUpdate', 'rollingUpdate': {'maxUnavailable': 1, 'maxSurge': 0}})
    if kind in ('Job', 'CronTemplate'):
        defaults = {'parallelism': 1, 'completions': 1, 'backoffLimit': 6, 'completionMode': 'NonIndexed',
                    'suspend': False, 'manualSelector': False, 'podReplacementPolicy': 'TerminatingOrFailed'}
    value = strip_defaults(expected, actual, defaults)
    if kind == 'Job':
        generated_labels = {'controller-uid': uid, 'batch.kubernetes.io/controller-uid': uid,
                            'job-name': name, 'batch.kubernetes.io/job-name': name}
        labels = value.get('template', {}).get('metadata', {}).get('labels', {})
        wanted_labels = expected.get('template', {}).get('metadata', {}).get('labels', {})
        for field, content in generated_labels.items():
            if field not in wanted_labels and field in labels:
                require(labels[field] == content, 'LIVE_JOB_CONTROLLER_LABEL_DRIFT')
                labels.pop(field)
        if 'selector' not in expected:
            actual_selector = value.get('selector', {}).get('matchLabels', {})
            require(set(value.get('selector', {})) == {'matchLabels'} and actual_selector and all(k in ('controller-uid', 'batch.kubernetes.io/controller-uid') and v == uid
                                            for k, v in actual_selector.items()), 'LIVE_JOB_SELECTOR_DRIFT')
            value.pop('selector', None)
    require(isinstance(value, dict) and 'template' in value, 'LIVE_WORKLOAD_TEMPLATE_REQUIRED')
    value['template']['spec'] = normalized_pod(expected['template']['spec'], value['template']['spec'])
    # server timestamps не являются конфигурацией запуска.
    template_defaults = {'creationTimestamp': None, 'labels': {}, 'annotations': {}}
    if 'metadata' in value['template']:
        metadata = strip_defaults(expected['template'].get('metadata', {}), value['template']['metadata'], template_defaults)
        if 'metadata' not in expected['template'] and metadata == {}:
            value['template'].pop('metadata')
        else:
            value['template']['metadata'] = metadata
    if kind == 'StatefulSet' and 'volumeClaimTemplates' in value:
        value['volumeClaimTemplates'] = normalized_claim_templates(expected.get('volumeClaimTemplates', []), value['volumeClaimTemplates'])
    return value


def rbac_rules(resource):
    value = resource.get('rules')
    require(value is None or isinstance(value, list), 'RBAC_RULES_SHAPE_REJECTED')
    return [] if value is None else value


def api_write_resource(resource, actual=None):
    result = copy.deepcopy(resource)
    # API канонизирует deny-all [] в null. Другие правила не меняются.
    if result.get('kind') in ('Role', 'ClusterRole') and rbac_rules(result) == []:
        result['rules'] = None
    if (result.get('apiVersion'), result.get('kind')) == ('apps/v1', 'StatefulSet') and actual is not None:
        require(owner(result) and owner(actual) and key(result) == key(actual) and actual['metadata'].get('ownerReferences', []) == result['metadata'].get('ownerReferences', []), 'FOREIGN_RESOURCE_COLLISION')
        wanted = result['spec'].get('volumeClaimTemplates', [])
        observed = actual['spec'].get('volumeClaimTemplates', [])
        require(normalized_claim_templates(wanted, observed) == wanted, 'IMMUTABLE_CLAIM_TEMPLATE_DRIFT')
        # Список immutable/atomic: после exact проверки сохраняем только его API defaults.
        if 'volumeClaimTemplates' in actual['spec']:
            result['spec']['volumeClaimTemplates'] = copy.deepcopy(observed)
    if result.get('apiVersion') == 'admissionregistration.k8s.io/v1' and result.get('kind') in ('ValidatingAdmissionPolicy', 'ValidatingAdmissionPolicyBinding'):
        field = 'matchConstraints' if result['kind'] == 'ValidatingAdmissionPolicy' else 'matchResources'
        match = result.get('spec', {}).get(field)
        if match is not None:
            require(isinstance(match, dict), 'ADMISSION_MATCH_CONFIGURATION_REJECTED')
            for name, value in (('matchPolicy', 'Equivalent'), ('namespaceSelector', {}), ('objectSelector', {})):
                match.setdefault(name, value)
            for name in ('resourceRules', 'excludeResourceRules'):
                require(isinstance(match.get(name, []), list), 'ADMISSION_MATCH_RULES_REJECTED')
                for rule in match.get(name, []):
                    require(isinstance(rule, dict), 'ADMISSION_MATCH_RULES_REJECTED')
                    rule.setdefault('scope', '*')
    return result


def policy_spec(resource):
    if resource.get('apiVersion') == 'admissionregistration.k8s.io/v1' and resource['kind'] in ('ValidatingAdmissionPolicy', 'ValidatingAdmissionPolicyBinding'):
        return api_write_resource(resource)['spec']
    spec = copy.deepcopy(resource.get('spec'))
    if resource['kind'] == 'NetworkPolicy':
        for direction in ('ingress', 'egress'):
            if spec.get(direction) == []:
                spec.pop(direction)
            for rule in spec.get(direction, []):
                for port in rule.get('ports', []):
                    port.setdefault('protocol', 'TCP')
    return spec


def verify_resource(expected, actual):
    require(key(expected) == key(actual) and owner(actual), 'LIVE_RESOURCE_IDENTITY_REJECTED')
    require(actual['metadata'].get('ownerReferences', []) == expected['metadata'].get('ownerReferences', []), 'LIVE_OWNER_REFERENCE_DRIFT')
    require(subset(expected['metadata'].get('labels', {}), actual['metadata'].get('labels', {})), 'LIVE_LABEL_DRIFT')
    require(subset(expected['metadata'].get('annotations', {}), actual['metadata'].get('annotations', {})), 'LIVE_ANNOTATION_DRIFT')
    for field in ('data', 'binaryData', 'immutable', 'roleRef', 'subjects'):
        if field in expected:
            require(subset(expected[field], actual.get(field)), 'LIVE_RESOURCE_DRIFT')
    kind = expected['kind']
    if kind in ('NetworkPolicy', 'Ingress', 'Middleware', 'ServersTransport', 'ValidatingAdmissionPolicy', 'ValidatingAdmissionPolicyBinding', 'ImageAdmissionPolicyParameters'):
        require(policy_spec(actual) == policy_spec(expected), 'LIVE_SECURITY_SPEC_DRIFT')
        annotations = {k: v for k, v in actual['metadata'].get('annotations', {}).items()
                       if k != 'kubectl.kubernetes.io/last-applied-configuration'}
        require(annotations == expected['metadata'].get('annotations', {}), 'LIVE_SECURITY_ANNOTATION_DRIFT')
    elif kind in ('Deployment', 'StatefulSet', 'DaemonSet', 'Job'):
        require(workload_spec(expected['spec'], actual.get('spec'), kind, actual['metadata'].get('uid'), actual['metadata']['name']) == expected['spec'], 'LIVE_EXECUTABLE_SPEC_DRIFT')
    elif kind == 'CronJob':
        value = strip_defaults(expected['spec'], actual.get('spec'), {'concurrencyPolicy': 'Allow', 'successfulJobsHistoryLimit': 3,
                              'failedJobsHistoryLimit': 1, 'suspend': False})
        value['jobTemplate']['spec'] = workload_spec(expected['spec']['jobTemplate']['spec'], value['jobTemplate']['spec'], 'CronTemplate')
        actual_meta = value['jobTemplate'].get('metadata', {})
        wanted_meta = expected['spec']['jobTemplate'].get('metadata', {})
        meta = strip_defaults(wanted_meta, actual_meta, {'creationTimestamp': None, 'labels': {}, 'annotations': {}})
        if 'metadata' not in expected['spec']['jobTemplate'] and not meta:
            value['jobTemplate'].pop('metadata', None)
        else:
            value['jobTemplate']['metadata'] = meta
        require(value == expected['spec'], 'LIVE_CRONJOB_SPEC_DRIFT')
    elif kind == 'Service':
        value = strip_defaults(expected['spec'], actual.get('spec'), {'type': 'ClusterIP', 'sessionAffinity': 'None',
            'internalTrafficPolicy': 'Cluster', 'publishNotReadyAddresses': False})
        for field in ('clusterIP', 'clusterIPs', 'ipFamilies', 'ipFamilyPolicy'):
            if field not in expected['spec']:
                value.pop(field, None)
        for index, port in enumerate(value.get('ports', [])):
            wanted = expected['spec'].get('ports', [])
            require(index < len(wanted), 'LIVE_SERVICE_PORT_DRIFT')
            value['ports'][index] = strip_defaults(wanted[index], port, {'protocol': 'TCP', 'targetPort': port['port']})
        require(value == expected['spec'], 'LIVE_SERVICE_SPEC_DRIFT')
    elif 'spec' in expected:
        require(subset(expected['spec'], actual.get('spec')), 'LIVE_RESOURCE_DRIFT')
    if kind in ('Role', 'ClusterRole'):
        require(rbac_rules(actual) == rbac_rules(expected), 'LIVE_RBAC_RULE_DRIFT')
    elif 'rules' in expected:
        require(actual.get('rules') == expected['rules'], 'LIVE_RBAC_RULE_DRIFT')
    if kind == 'ConfigMap':
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
        if not raw.strip():
            return None
        actual = json.loads(raw)
        require(isinstance(actual, dict), 'KUBERNETES_READBACK_SHAPE_REJECTED')
        # kubectl get -f - может оборачивать единственный объект в List.
        # Пустой список означает absence; иной набор не выбирается по порядку.
        if actual.get('kind') == 'List':
            items = actual.get('items')
            require(isinstance(items, list) and len(items) <= 1, 'KUBERNETES_READBACK_CARDINALITY_REJECTED')
            if not items:
                return None
            actual = items[0]
        require(isinstance(actual, dict) and key(actual) == key(resource), 'KUBERNETES_READBACK_IDENTITY_REJECTED')
        return actual

    def inventory(self, resources):
        require(all(owner(r) for r in resources if r['kind'] != 'Secret'), 'UNOWNED_RENDER_RESOURCE_REJECTED')
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

    def cas_update(self, desired, actual, dry_run=False):
        kind = (desired['apiVersion'], desired['kind'])
        require(kind in CAS_TARGETS, 'EXACT_CAS_KIND_REQUIRED')
        require(owner(desired) and owner(actual) and key(desired) == key(actual) and actual['metadata'].get('ownerReferences', []) == desired['metadata'].get('ownerReferences', []), 'FOREIGN_RESOURCE_COLLISION')
        metadata = actual['metadata']
        patch = [{'op': 'test', 'path': '/metadata/uid', 'value': metadata['uid']},
                 {'op': 'test', 'path': '/metadata/resourceVersion', 'value': metadata['resourceVersion']}]
        for field in ('labels', 'annotations'):
            wanted = copy.deepcopy(desired['metadata'].get(field, {}))
            if field == 'annotations':
                for server_field in ('kubectl.kubernetes.io/last-applied-configuration', 'deployment.kubernetes.io/revision'):
                    if server_field in metadata.get(field, {}):
                        wanted[server_field] = metadata[field][server_field]
            if wanted != metadata.get(field, {}):
                patch.append({'op': 'add', 'path': '/metadata/' + field, 'value': wanted})
        fields = ('data', 'binaryData', 'immutable') if kind[1] == 'ConfigMap' else ('spec',)
        for field in fields:
            if field in desired:
                if field not in actual or desired[field] != actual[field]:
                    patch.append({'op': 'add', 'path': '/' + field, 'value': copy.deepcopy(desired[field])})
            elif field in actual:
                patch.append({'op': 'remove', 'path': '/' + field})
        target = CAS_TARGETS[kind]
        arguments = (['-n', desired['metadata']['namespace']] if desired['metadata'].get('namespace') else []) + ['patch', target, desired['metadata']['name'],
                     '--type=json', '--field-manager=kodex-install', '--patch-file=/dev/stdin']
        if dry_run:arguments.append('--dry-run=server')
        self.call(arguments, patch)

    def preflight_apply(self, resource):
        require(owner(resource) and resource['kind'] != 'Secret', 'UNOWNED_RENDER_RESOURCE_REJECTED')
        actual = self.get(resource)
        if actual is not None:
            require(owner(actual) and not actual['metadata'].get('deletionTimestamp') and actual['metadata'].get('ownerReferences', []) == resource['metadata'].get('ownerReferences', []), 'FOREIGN_RESOURCE_COLLISION')
        if resource['kind'] == 'ImageAdmissionPolicyParameters' and actual is not None and actual.get('spec') != resource.get('spec'):
            self.admission_parameters_quiescent(resource)
            probe = copy.deepcopy(resource)
            probe['metadata'].pop('name')
            probe['metadata']['generateName'] = 'kodex-preflight-'
            self.call(['create', '--dry-run=server', '--field-manager=kodex-install', '-f', '-'], probe)
            return
        if actual is not None and (resource['apiVersion'], resource['kind']) in CAS_TARGETS:
            self.cas_update(api_write_resource(resource, actual), actual, dry_run=True)
        else:
            self.call(['apply', '--server-side', '--dry-run=server', '--field-manager=kodex-install', '-f', '-'], api_write_resource(resource, actual))

    def apply(self, resource, inventory):
        require(owner(resource), 'UNOWNED_RENDER_RESOURCE_REJECTED')
        actual = self.fenced_current(resource, inventory)
        if resource['kind'] == 'ImageAdmissionPolicyParameters' and actual is not None:
            require(actual.get('spec') == resource.get('spec'), 'IMMUTABLE_ADMISSION_PARAMETERS_RECONCILIATION_REQUIRED')
        if resource['kind'] == 'Lease':
            require(resource['metadata']['name'] == 'image-admission-controller-leader' and resource.get('spec') == {},
                    'UNREGISTERED_CONTROLLER_OWNED_LEASE')
            if actual is not None:
                return
        desired = api_write_resource(resource, actual)
        if actual is None:
            # Create, а не SSA upsert: появившийся после inventory объект не перезапишется.
            self.call(['create', '--field-manager=kodex-install', '-f', '-'], desired)
        elif (desired['apiVersion'], desired['kind']) in CAS_TARGETS:
            self.cas_update(desired, actual)
        else:
            desired['metadata']['resourceVersion'] = actual['metadata']['resourceVersion']
            self.call(['apply', '--server-side', '--field-manager=kodex-install', '-f', '-'], desired)
        current = self.get(resource)
        require(current is not None and owner(current), 'WRITE_READBACK_REQUIRED')
        if resource['kind'] == 'ImageAdmissionPolicyParameters' or (actual is not None and ((resource['apiVersion'], resource['kind']) in CAS_TARGETS or (resource['apiVersion'], resource['kind']) == ('apps/v1', 'StatefulSet'))):
            verify_resource(resource, current)
        inventory[key(resource)] = {'uid': current['metadata']['uid'], 'seal': seal(current)}

    def delete(self, resource, inventory):
        actual = self.fenced_current(resource, inventory)
        if actual is None:
            return
        if resource['kind'] == 'ImageAdmissionPolicyParameters':
            self.admission_parameters_quiescent(resource)
        metadata = actual['metadata']
        self.call(['delete', '--raw', self.path(resource), '-f', '-'], {
            'apiVersion': 'v1', 'kind': 'DeleteOptions', 'propagationPolicy': 'Foreground',
            'preconditions': {'uid': metadata['uid'], 'resourceVersion': metadata['resourceVersion']}})
        # Завершение foreground deletion проверяется перед созданием successor.
        self.call(['wait', '--for=delete', '-f', '-', '--timeout=15s'], descriptor(resource))
        inventory[key(resource)] = None

    def admission_parameters_quiescent(self, resource):
        """Immutable successor требует остановленного producer и неизменённого fail-closed binding."""
        require((resource['apiVersion'],resource['kind'],resource['metadata'].get('namespace'),resource['metadata']['name']) ==
                ('supplychain.kodex.dev/v1alpha1','ImageAdmissionPolicyParameters','kodex-system','kodex-image-admission-policy'), 'EXACT_ADMISSION_PARAMETERS_REQUIRED')
        controller=self.get({'apiVersion':'apps/v1','kind':'Deployment','metadata':{'namespace':'kodex-system','name':'image-admission-controller'}})
        require(controller is not None and owner(controller) and controller.get('spec',{}).get('replicas',1)==0 and
                controller.get('status',{}).get('replicas',0)==0, 'ADMISSION_CONTROLLER_MUST_BE_STOPPED')
        binding=self.get({'apiVersion':'admissionregistration.k8s.io/v1','kind':'ValidatingAdmissionPolicyBinding',
                          'metadata':{'name':'kodex-image-admission-controller-jobs'}})
        require(binding is not None and owner(binding) and binding.get('spec',{}).get('policyName')=='kodex-image-admission-controller-jobs'and
                binding['spec'].get('validationActions')==['Deny']and binding['spec'].get('paramRef')==
                {'name':'kodex-image-admission-policy','namespace':'kodex-system','parameterNotFoundAction':'Deny'}, 'FAIL_CLOSED_ADMISSION_BINDING_REQUIRED')
        jobs=json.loads(self.call(['-n','kodex-system','get','jobs','-l','kodex.dev/image-admission-orchestrated=true','-o','json']))['items']
        claims=json.loads(self.call(['-n','kodex-system','get','pvc','-l','kodex.dev/image-admission-orchestrated=true','-o','json']))['items']
        require(not claims and all(any(c.get('type')in('Complete','Failed')and c.get('status')=='True'for c in j.get('status',{}).get('conditions',[]))for j in jobs), 'ADMISSION_WORK_MUST_BE_DRAINED')


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


def verify_readback(state, resources, completion=None):
    import provider_sandbox_profile
    import image_policy_inputs
    provider_sandbox_profile.validate_render(resources)
    image_policy_inputs.validate_render(resources)
    verify_resource_set(state, resources, completion)


def verify_resource_set(state, resources, completion=None):
    """Читает заданный набор объектов; coherence полного render проверяет вызывающий."""
    import bootstrap_completion
    if completion is not None:bootstrap_completion.verify(state, resources, completion)
    for expected in resources:
        if expected['kind'] == 'Secret':continue
        actual = state.get(expected)
        if actual is None and completion is not None and expected['kind'] == 'Job' and expected['metadata']['name'] in bootstrap_completion.JOBS:
            continue
        require(actual is not None, 'LIVE_RESOURCE_MISSING')
        verify_resource(expected, actual)
    if completion is None:verify_schemas(state, resources)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('inventory', 'apply', 'delete', 'readback', 'prepare-api-write', 'preflight-apply'))
    parser.add_argument('--context', required=True)
    parser.add_argument('--render', required=True)
    parser.add_argument('--inventory')
    parser.add_argument('--manifest')
    parser.add_argument('--output')
    parser.add_argument('--kind')
    parser.add_argument('--name')
    parser.add_argument('--namespace', default='kodex-system')
    import bootstrap_completion
    bootstrap_completion.add_arguments(parser)
    args = parser.parse_args()
    try:
        completion = bootstrap_completion.from_arguments(args)
        require(completion is None or args.mode == 'readback', 'BOOTSTRAP_COMPLETION_READBACK_ONLY')
        raw = Path(args.render).read_bytes()
        resources = list(filter(None, yaml.safe_load_all(raw)))
        render_digest = hashlib.sha256(raw).hexdigest()
        if args.mode == 'prepare-api-write':
            require(args.manifest and args.output, 'MANIFEST_AND_OUTPUT_REQUIRED')
            selected = list(filter(None, yaml.safe_load_all(Path(args.manifest).read_bytes())))
            registered = {key(r): r for r in resources}
            require(all(r == registered.get(key(r)) and r['kind'] != 'Secret' and owner(r) for r in selected), 'MANIFEST_OUTSIDE_PINNED_RENDER')
            with open(args.output, 'x', opener=lambda p, f: os.open(p, f, 0o600)) as stream:
                yaml.safe_dump_all([api_write_resource(r) for r in selected], stream, sort_keys=False)
            return
        state = State(args.context)
        state.discover(resources)
        if args.mode == 'preflight-apply':
            require(args.manifest, 'MANIFEST_REQUIRED')
            selected = list(filter(None, yaml.safe_load_all(Path(args.manifest).read_bytes())))
            registered = {key(r): r for r in resources}
            require(all(r == registered.get(key(r)) for r in selected), 'MANIFEST_OUTSIDE_PINNED_RENDER')
            for resource in selected:state.preflight_apply(resource)
            return
        if args.mode == 'inventory':
            value = state.inventory(resources)
            require(args.inventory and not Path(args.inventory).exists(), 'NEW_INVENTORY_PATH_REQUIRED')
            with open(args.inventory, 'x', opener=lambda p, f: os.open(p, f, 0o600)) as stream:
                json.dump({'version': 1, 'context': args.context, 'renderSHA256': render_digest, 'resources': value}, stream)
            return
        if args.mode == 'readback':
            verify_readback(state, resources, completion)
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
    except Rejected as error:
        code = str(error) if re.fullmatch(r'[A-Z][A-Z0-9_]{0,95}', str(error)) else 'REJECTED'
        raise SystemExit('Portable state operation rejected: ' + code + '; readback required') from None
    except (OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError):
        raise SystemExit('Portable state operation rejected; readback required') from None


if __name__ == '__main__':
    main()
