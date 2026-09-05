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
  - name: ghcr.io/codex-k8s/kodex/internal-rpc-authority
    newName: registry.example.test/kodex/internal-rpc-authority
    digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
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
names = {c['name'] for c in pod['containers']}
assert names == {'email-bridge', 'internal-rpc-authority-issuer', 'platform-worker-grant-agent'}
volumes = {v['name'] for v in pod['volumes']}
for container in pod['containers'] + pod['initContainers']:
    assert '@sha256:' in container['image'] and 'sha256:' + '0' * 64 not in container['image']
    assert all(m['name'] in volumes for m in container.get('volumeMounts', []))
assert pod['securityContext']['fsGroup'] == 29000
assert get('PodDisruptionBudget', 'email-bridge')['spec']['minAvailable'] == 1
assert get('ServiceMonitor', 'email-bridge')['spec']['endpoints'][0]['port'] == 'metrics'
assert get('Service', 'email-bridge')['spec']['ports'][0]['port'] == 443
assert get('ConfigMap', 'email-bridge-dashboard')['metadata']['labels']['grafana_dashboard'] == '1'
assert get('NetworkPolicy', 'integration-gateway-email-bridge')['spec']['egress'][0]['ports'][0]['port'] == 8443
destinations = {}
for rule in get('NetworkPolicy', 'email-bridge')['spec']['egress']:
    for destination in rule['to']:
        name = destination['podSelector']['matchLabels'].get('app.kubernetes.io/name')
        if name:
            destinations[name] = (destination['namespaceSelector']['matchLabels']['kubernetes.io/metadata.name'], rule['ports'][0]['port'])
assert destinations['control-plane'] == ('kodex-system', 8443)
assert destinations['email-bridge-postgresql'] == ('kodex-system', 5432)
assert destinations['opentelemetry-collector'] == ('observability', 4317)
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
assert any(r['expr'] == 'kodex_email_bridge_readiness{ready="false"} == 1' for r in rules)
assert not any(o['kind'] in ['Role', 'ClusterRole', 'RoleBinding', 'ClusterRoleBinding'] for o in objects)
print('Email bridge staging render passed')
PY
