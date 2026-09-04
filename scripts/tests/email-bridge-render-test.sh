#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d "$root/deploy/k8s/overlays/staging/.email-render-XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT
cat > "$temporary/kustomization.yaml" <<'YAML'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: [../email-bridge]
images:
  - name: email-bridge
    newName: registry.example.test/kodex/email-bridge
    digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  - name: email-bridge-migration
    newName: registry.example.test/kodex/email-bridge-migration
    digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
YAML
kubectl kustomize "$temporary" > "$temporary/render.yaml"
python3 - "$temporary/render.yaml" <<'PY'
import sys, yaml
objects = list(yaml.safe_load_all(open(sys.argv[1])))
def get(kind, name):
    return next(o for o in objects if o['kind'] == kind and o['metadata']['name'] == name)
deployment = get('Deployment', 'email-bridge')['spec']
assert deployment['replicas'] == 2
pod = deployment['template']['spec']
assert pod['automountServiceAccountToken'] is False
c = pod['containers'][0]
assert '@sha256:' in c['image'] and c['securityContext']['readOnlyRootFilesystem']
assert all(p in c for p in ['livenessProbe', 'readinessProbe', 'startupProbe'])
assert get('PodDisruptionBudget', 'email-bridge')['spec']['minAvailable'] == 1
assert get('ServiceMonitor', 'email-bridge')['spec']['endpoints'][0]['port'] == 'metrics'
assert get('Job', 'email-bridge-migration')['spec']['activeDeadlineSeconds'] == 120
assert get('StatefulSet', 'email-bridge-postgresql')['spec']['volumeClaimTemplates']
for o in objects:
    if o['kind'] != 'NetworkPolicy': continue
    for rule in o['spec'].get('egress', []):
        assert rule.get('to')
        for destination in rule['to']:
            assert destination.get('namespaceSelector', {}).get('matchLabels')
            assert destination.get('podSelector', {}).get('matchLabels')
            assert 'ipBlock' not in destination
rules = get('PrometheusRule', 'email-bridge')['spec']['groups'][0]['rules']
assert all(r['annotations']['runbook_url'].startswith('https://') for r in rules)
assert any('unknown' in r['expr'] for r in rules)
assert not any(o['kind'] in ['Role', 'ClusterRole', 'RoleBinding', 'ClusterRoleBinding'] for o in objects)
print('Email bridge staging render passed')
PY
