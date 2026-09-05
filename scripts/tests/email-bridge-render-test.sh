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
  - name: kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/email-bridge
    newName: registry.example.test/kodex/email-bridge
    digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  - name: kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/email-bridge-migration
    newName: registry.example.test/kodex/email-bridge-migration
    digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  - name: ghcr.io/codex-k8s/kodex/internal-rpc-authority
    newName: registry.example.test/kodex/internal-rpc-authority
    digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
YAML
kubectl kustomize "$temporary" > "$temporary/render.yaml"
python3 - "$temporary/render.yaml" "$root/tools/install/secret-projections.json" "$root" <<'PY'
import json, pathlib, sys, yaml
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
issuer = next(x for x in pod['containers'] if x['name'] == 'internal-rpc-authority-issuer')
issuer_volumes = {x['name']: x for x in pod['volumes']}
assert 'configuration' not in issuer_volumes
assert issuer_volumes['mail']['secret'] == {'secretName': 'email-bridge-mailbox-projection', 'defaultMode': 288}
mail_mount = next(x for x in c['volumeMounts'] if x['name'] == 'mail')
assert mail_mount == {'name': 'mail', 'mountPath': '/var/run/email/mail', 'readOnly': True}
assert issuer_volumes['internal-rpc-authority-postgresql']['secret']['secretName'] == 'internal-rpc-authority-email-bridge-issuer-postgresql'
assert issuer_volumes['internal-rpc-authority-workload-tls']['secret']['secretName'] == 'internal-rpc-authority-email-bridge-workload-tls'
certificate = get('Certificate', 'internal-rpc-authority-email-bridge-workload')['spec']
assert certificate['secretName'] == issuer_volumes['internal-rpc-authority-workload-tls']['secret']['secretName']
assert certificate['uris'] == ['spiffe://kodex.local/ns/kodex-system/sa/email-bridge']
assert certificate['issuerRef'] == {'name': 'kodex-installation-ca', 'kind': 'Issuer'}
assert sorted(certificate['usages']) == ['client auth', 'server auth']
assert next(x for x in issuer['volumeMounts'] if x['name'] == 'internal-rpc-authority-postgresql')['mountPath'] == '/var/run/secrets/kodex/internal-rpc-authority/postgres'
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
assert destinations['egress-gateway'] == ('kodex-system', 8082)
assert destinations['email-bridge-postgresql'] == ('kodex-system', 5432)
assert destinations['opentelemetry-collector'] == ('observability', 4317)
root = pathlib.Path(sys.argv[3])
descriptor = yaml.safe_load((root / 'contracts/email-bridge/v1/deployable.yaml').read_text())
runtime = get('ConfigMap', 'email-bridge-runtime')['data']
assert runtime['EMAIL_BRIDGE_SECRETS_ROOT'] == mail_mount['mountPath']
assert 'EMAIL_BRIDGE_CONFIGURATION_FILE' not in runtime
authority = descriptor['authority']
assert authority['transport'] == 'grpc'
assert authority['endpoint'] == runtime['EMAIL_BRIDGE_AUTHORITY_TARGET']
assert authority['application_grant_env'] in runtime
assert authority['issuer_proof'] == 'local-internal-rpc-authority'
methods = ['ResolveEmailAuthorization', 'ReportEmailEffectReceipt', 'ResolveEmailReconciliation']
assert authority['methods'] == ['/controlplane.v1.RuntimeWorkService/' + name for name in methods]
generated = (root / 'libs/go/controlplaneapi/gen/controlplane/v1/control_plane_grpc.pb.go').read_text()
assert all('"' + method + '"' in generated for method in authority['methods'])
migrations = root / 'services/internal/email-bridge/cmd/cli/migrations'
assert int(descriptor['migration_version']) == max(int(path.name.split('_')[0]) for path in migrations.glob('*.sql'))
assert descriptor['egress']['endpoint'] == runtime['EMAIL_BRIDGE_EGRESS_ADDRESS']
assert runtime['EMAIL_BRIDGE_EGRESS_ADDRESS'] == 'egress-gateway.kodex-system.svc:8082'
assert 'owner-authorization' not in descriptor['readiness']['local_requires']
assert get('Job', 'email-bridge-migration')['spec']['activeDeadlineSeconds'] == 120
database = get('StatefulSet', 'email-bridge-postgresql')['spec']
assert database['volumeClaimTemplates']
for probe in ['startupProbe', 'readinessProbe', 'livenessProbe']:
    assert database['template']['spec']['containers'][0][probe]['exec']['command'] == ['pg_isready', '-h', '127.0.0.1', '-U', 'postgres']
registry = {s['name']: s for s in json.load(open(sys.argv[2]))['secrets']}
for name, fields, ref in [
    ('email-bridge-postgresql-bootstrap', ['admin-password', 'runtime-password', 'migration-password'], 'postgres-bootstrap'),
    ('email-bridge-runtime-database', ['dsn'], 'postgres-runtime'),
    ('email-bridge-migration-database', ['dsn'], 'postgres-migration'),
]:
    secret = registry[name]
    assert secret['dynamic'] is False
    assert sorted(i['key'] for i in secret['items']) == sorted(fields)
    assert all(i['source'] == {'type': 'material', 'ref': 'kodex/email-bridge/' + ref, 'field': i['key']} for i in secret['items'])
migration_volumes = get('Job', 'email-bridge-migration')['spec']['template']['spec']['volumes']
assert next(v['secret']['items'] for v in migration_volumes if v['name'] == 'tls') == [{'key': 'ca.crt', 'path': 'ca.crt'}]
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

for profile in web-only web-with-mattermost; do
  kubectl kustomize "$root/deploy/k8s/profiles/$profile" > "$temporary/$profile.yaml"
done
python3 - "$temporary" "$root/tools/release/images.json" <<'PY'
import json, pathlib, re, sys, yaml
base = pathlib.Path(sys.argv[1])
images = json.loads(pathlib.Path(sys.argv[2]).read_text())['images']
for component, target in [('email-bridge', 'runtime'), ('email-bridge-migration', 'migration')]:
    entries = [x for x in images if x['component'] == component]
    assert len(entries) == 1 and entries[0]['target'] == target
    assert entries[0]['dockerfile'] == 'services/internal/email-bridge/Dockerfile'
for profile in ['web-only', 'web-with-mattermost']:
    objects = list(yaml.safe_load_all((base / (profile + '.yaml')).read_text()))
    def get(kind, name):
        result = [o for o in objects if o and o['kind'] == kind and o['metadata']['name'] == name]
        assert len(result) == 1
        return result[0]
    runtime = get('ConfigMap', 'email-bridge-runtime')['data']
    assert runtime['DEPLOYMENT_ENVIRONMENT'] == 'production'
    assert runtime['EMAIL_BRIDGE_RECONCILIATION_INTERVAL_SECONDS'] == '15'
    assert runtime['EMAIL_BRIDGE_RECONCILIATION_BATCH'] == '16'
    assert get('StatefulSet', 'email-bridge-postgresql')
    for kind, name, container in [('Deployment', 'email-bridge', 'email-bridge'), ('Job', 'email-bridge-migration', 'migration')]:
        pod = get(kind, name)['spec']['template']['spec']
        c = next(c for c in pod['containers'] if c['name'] == container)
        assert re.fullmatch(r'kodex-image-registry\.kodex-system\.svc\.cluster\.local:5000/kodex/' + name + r'@sha256:[0-9a-f]{64}', c['image'])
        assert pod['automountServiceAccountToken'] is False
        if kind == 'Deployment':
            mounts = {m['name']: m for m in c['volumeMounts']}
            assert mounts['application-grant']['readOnly']
            assert mounts['internal-rpc-authority-observability']['mountPath'] == '/var/run/email/observability'
    print('Email bridge full profile render passed: ' + profile)
    digest = 'sha256:' + 'a' * 64
    prefix = 'registry.example.test/kodex/'
    entries = [dict(component=x['component'], repository='kodex/' + x['component'], digest=digest, pull_ref=prefix + x['component'] + '@' + digest) for x in images]
    lock = dict(schema_version=2, profile=profile, source_sha='a' * 40, build_run_id='local', registry=dict(push='registry.example.test', node_pull='registry.example.test', repository_prefix='kodex'), images=entries,
                external_images=[dict(component='admission-tools', digest=digest, pull_ref='registry.example.test/admission-tools@' + digest)],
                role_image_input=dict(repository='kodex/role-image-inputs', manifest_digest=digest, payload_sha256='b' * 64, source_sha256='c' * 64, pull_ref=prefix + 'role-image-inputs@' + digest))
    (base / (profile + '.lock.json')).write_text(json.dumps(lock))
PY
for profile in web-only web-with-mattermost; do
  lock="$temporary/$profile.lock.json"
  "$root/tools/release/validate-release-lock.sh" --lock "$lock" --source-sha aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
    --sha256 "$(sha256sum "$lock" | awk '{print $1}')" --profile "$profile"
done
