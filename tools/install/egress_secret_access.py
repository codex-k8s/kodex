"""Точечное исправление group-read секретов non-root egress без изменения файлов."""
import argparse
import copy
import os
from pathlib import Path
import yaml
from deployment_profile import require, Rejected


def transform(resources):
    result = copy.deepcopy(resources)
    selected = [r for r in result if r.get('kind') == 'Deployment' and
                r['metadata']['name'] == 'egress-gateway' and r['metadata'].get('namespace') == 'kodex-system']
    require(len(selected) == 1, 'EXACT_EGRESS_DEPLOYMENT_REQUIRED')
    pod = selected[0]['spec']['template']['spec']
    containers = pod.get('containers', [])
    require(len(containers) == 1 and containers[0]['name'] == 'egress-gateway' and
            containers[0]['securityContext'].get('runAsUser') == 10001 and
            containers[0]['securityContext'].get('runAsGroup') == 10001 and
            containers[0]['securityContext'].get('allowPrivilegeEscalation') is False and
            containers[0]['securityContext'].get('capabilities') == {'drop': ['ALL']}, 'EXACT_NONROOT_EGRESS_REQUIRED')
    for name, secret in [('runtime-signing', 'runtime-egress-signing'), ('runtime-proxy-ca', 'runtime-egress-proxy-ca')]:
        volumes = [v for v in pod.get('volumes', []) if v['name'] == name]
        mounts = [v for v in containers[0].get('volumeMounts', []) if v['name'] == name]
        require(len(volumes) == len(mounts) == 1 and
                volumes[0].get('secret') == {'secretName': secret, 'defaultMode': 0o440} and
                mounts[0].get('readOnly') is True, 'EXACT_READONLY_EGRESS_SECRET_REQUIRED')
    security = pod['securityContext']
    require(security.get('runAsNonRoot') is True and
            security.get('fsGroup') in (None, 10001) and
            security.get('fsGroupChangePolicy') in (None, 'OnRootMismatch'), 'EGRESS_GROUP_DRIFT_REJECTED')
    security.update(fsGroup=10001, fsGroupChangePolicy='OnRootMismatch')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes(); require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw))); require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    desired = transform(resources)
    descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'w') as stream: yaml.safe_dump_all(desired, stream, sort_keys=False)


if __name__ == '__main__':
    try: main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, yaml.YAMLError):
        raise SystemExit('Egress secret access repair rejected') from None
