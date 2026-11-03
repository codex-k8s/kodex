#!/usr/bin/env python3
"""Опциональный render без Prometheus Operator; допуск и прикладные probes сохраняются."""
import argparse
import copy
import os
from pathlib import Path
import tempfile
import yaml

MONITORING_API = 'monitoring.coreos.com/v1'
MONITORING_KINDS = frozenset(('ServiceMonitor', 'PodMonitor', 'PrometheusRule'))


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
        if kind in ('Deployment', 'StatefulSet', 'DaemonSet', 'Job'):
            pod = resource['spec']['template']['spec']
        elif kind == 'CronJob':
            pod = resource['spec']['jobTemplate']['spec']['template']['spec']
        else:
            pod = {}
        for container in pod.get('containers', []):
            container['env'] = [v for v in container.get('env', []) if v['name'] != 'OTEL_SDK_DISABLED'] + [dict(name='OTEL_SDK_DISABLED', value='true')]
        selected.append(resource)
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
