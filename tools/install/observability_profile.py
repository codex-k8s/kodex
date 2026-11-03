#!/usr/bin/env python3
"""Опциональный render без Prometheus Operator; допуск и прикладные probes сохраняются."""
import argparse
import copy
import hashlib
import os
from pathlib import Path
import tempfile
import yaml

MONITORING_API = 'monitoring.coreos.com/v1'
MONITORING_KINDS = frozenset(('ServiceMonitor', 'PodMonitor', 'PrometheusRule'))
PROFILE_LABEL = 'kodex.dev/observability-profile'
CONTROLLER = 'image-admission-controller'
POLICY = 'kodex-image-admission-controller-jobs'
PARAMETERS = 'kodex-image-admission-policy'
RENDERER_DIRECTORY = '/opt/kodex/disabled-observability'
ORIGINAL_RENDERER = '/opt/kodex/render-image-admission-job.sh'
DISABLED_EXPRESSION = "has(params.metadata.labels) && 'kodex.dev/observability-profile' in params.metadata.labels && params.metadata.labels['kodex.dev/observability-profile'] == 'disabled'"
SDK_BLOCK = '''    if [[ $local_profile == hot-reload ]]; then
      cat <<EOF
            - {name: OTEL_SDK_DISABLED, value: "true"}
EOF
    fi
'''


def managed_renderer_script(source=None):
    """Производный immutable renderer проверяет digest baked source и меняет ровно SDK block."""
    raw = (Path(__file__).resolve().parents[1]/'render-image-admission-job.sh').read_bytes() if source is None else source
    text = raw.decode('utf-8')
    if text.count(SDK_BLOCK) != 1:
        raise ValueError('Canonical admission renderer SDK contract drift')
    header = Path(__file__).with_name('render-image-admission-no-observability.sh').read_text()
    if header.count('__RENDERER_SOURCE_SHA256__') != 1:
        raise ValueError('Admission renderer source guard drift')
    header = header.replace('__RENDERER_SOURCE_SHA256__', hashlib.sha256(raw).hexdigest())
    replacement = '    cat <<EOF\n            - {name: OTEL_SDK_DISABLED, value: "true"}\nEOF\n'
    return header + '\n' + text.replace(SDK_BLOCK, replacement)


def admission_profile(resources):
    """Настраивает управляемый renderer и его parameter-owned CEL opt-in вместе."""
    controllers = [r for r in resources if r.get('kind') == 'Deployment' and r['metadata']['name'] == CONTROLLER]
    if not controllers:
        return
    if len(controllers) != 1:
        raise ValueError('Unique admission controller required')
    controller = controllers[0]
    namespace = controller['metadata']['namespace']
    policies = [r for r in resources if r.get('kind') == 'ValidatingAdmissionPolicy' and r['metadata']['name'] == POLICY]
    parameters = [r for r in resources if r.get('kind') == 'ImageAdmissionPolicyParameters' and r['metadata']['name'] == PARAMETERS and r['metadata'].get('namespace') == namespace]
    if len(policies) != 1 or len(parameters) != 1:
        raise ValueError('Admission observability parameter contract required')
    policy = policies[0]
    variables = policy['spec']['variables']
    existing = [v for v in variables if v['name'] == 'observabilityDisabled']
    if existing and existing != [dict(name='observabilityDisabled', expression=DISABLED_EXPRESSION)]:
        raise ValueError('Admission observability variable drift')
    if not existing:
        variables.append(dict(name='observabilityDisabled', expression=DISABLED_EXPRESSION))
    replacements = {
        "(variables.localProfile ? 'OTEL_SDK_DISABLED,' : '')": "((variables.localProfile || variables.observabilityDisabled) ? 'OTEL_SDK_DISABLED,' : '')",
        "variables.localProfile && item.value == 'true'": "(variables.localProfile || variables.observabilityDisabled) && item.value == 'true'",
    }
    for before, after in replacements.items():
        found = 0
        for validation in policy['spec']['validations']:
            expression = validation['expression']
            found += expression.count(before) + expression.count(after)
            validation['expression'] = expression.replace(before, after)
        if found != 1:
            raise ValueError('Admission observability validation drift')
    parameters[0]['metadata'].setdefault('labels', {})[PROFILE_LABEL] = 'disabled'
    policy['metadata'].setdefault('labels', {})[PROFILE_LABEL] = 'disabled'
    for resource in resources:
        if resource.get('kind') == 'ConfigMap' and resource['metadata']['name'] == PARAMETERS and resource['metadata'].get('namespace') == namespace:
            resource['metadata'].setdefault('labels', {})[PROFILE_LABEL] = 'disabled'
    script = managed_renderer_script()
    digest = hashlib.sha256(script.encode()).hexdigest()
    name = 'image-admission-renderer-no-otel-' + digest[:16]
    asset = dict(apiVersion='v1', kind='ConfigMap', metadata=dict(name=name, namespace=namespace,
        labels={'app.kubernetes.io/part-of': 'kodex', PROFILE_LABEL: 'disabled'}), immutable=True, data={'renderer.sh': script})
    present = [r for r in resources if r.get('kind') == 'ConfigMap' and r['metadata']['name'] == name and r['metadata'].get('namespace') == namespace]
    if present and present != [asset]:
        raise ValueError('Admission renderer asset collision')
    if not present:
        resources.append(asset)
    pod = controller['spec']['template']['spec']
    containers = [c for c in pod['containers'] if c['name'] == CONTROLLER]
    if len(containers) != 1:
        raise ValueError('Admission controller container required')
    container = containers[0]
    renderer_path = RENDERER_DIRECTORY + '/renderer.sh'
    setting = 'IMAGE_ADMISSION_CONTROLLER_RENDERER_PATH'
    overrides = [e for e in container.get('env', []) if e['name'] == setting]
    if len(overrides) > 1 or (overrides and overrides[0] not in [dict(name=setting, value=ORIGINAL_RENDERER), dict(name=setting, value=renderer_path)]):
        raise ValueError('Custom admission renderer requires explicit profile support')
    container['env'] = [e for e in container.get('env', []) if e['name'] != setting] + [dict(name=setting, value=renderer_path)]
    volume_name = 'disabled-observability-renderer'
    volume = dict(name=volume_name, configMap=dict(name=name, defaultMode=0o555))
    mount = dict(name=volume_name, mountPath=RENDERER_DIRECTORY, readOnly=True)
    for field, value, owner in [('volumes', volume, pod), ('volumeMounts', mount, container)]:
        same = [v for v in owner.get(field, []) if v['name'] == volume_name]
        if same and same != [value]:
            raise ValueError('Admission renderer volume collision')
        if not same:
            owner.setdefault(field, []).append(value)


def transform(resources, disabled=False):
    result = copy.deepcopy(resources)
    if not disabled:
        return result
    selected = []
    for resource in result:
        if resource.get('apiVersion') == MONITORING_API:
            if resource.get('kind') not in MONITORING_KINDS:
                raise ValueError('Unsupported monitoring resource in disabled profile')
            continue
        kind = resource.get('kind')
        if kind == 'Pod':
            pod = resource['spec']
        elif kind in ('Deployment', 'StatefulSet', 'DaemonSet', 'Job'):
            pod = resource['spec']['template']['spec']
        elif kind == 'CronJob':
            pod = resource['spec']['jobTemplate']['spec']['template']['spec']
        else:
            pod = {}
        # Одноразовые init сохраняют уже применённые immutable Job templates.
        containers = pod.get('containers', []) + [c for c in pod.get('initContainers', []) if c.get('restartPolicy') == 'Always']
        for container in containers:
            container['env'] = [v for v in container.get('env', []) if v['name'] != 'OTEL_SDK_DISABLED'] + [dict(name='OTEL_SDK_DISABLED', value='true')]
        selected.append(resource)
    admission_profile(selected)
    return selected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--disabled', action='store_true')
    args = parser.parse_args()
    path = args.render
    if not path.is_file() or path.is_symlink():
        raise ValueError('Regular render file required')
    raw = path.read_bytes()
    if len(raw) > 16 << 20:
        raise ValueError('Render size limit exceeded')
    resources = list(filter(None, yaml.safe_load_all(raw)))
    if len(resources) > 10000:
        raise ValueError('Render resource limit exceeded')
    result = transform(resources, args.disabled)
    descriptor, temporary = tempfile.mkstemp(prefix='.observability-', dir=path.parent)
    try:
        with os.fdopen(descriptor, 'w') as stream:
            yaml.safe_dump_all(result, stream, sort_keys=False)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, yaml.YAMLError):
        raise SystemExit('Observability profile rejected') from None
