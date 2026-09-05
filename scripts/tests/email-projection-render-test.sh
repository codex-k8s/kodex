#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
for profile in web-only web-with-mattermost; do
  kubectl kustomize "$root/deploy/k8s/profiles/$profile" > "$temporary/$profile.yaml"
  python3 - "$temporary/$profile.yaml" "$profile" <<'PY'
import json, sys, yaml
objects = list(yaml.safe_load_all(open(sys.argv[1])))
def get(kind, name):
    matches = [o for o in objects if o and o['kind'] == kind and o['metadata']['name'] == name]
    assert len(matches) == 1, (kind, name)
    return matches[0]
secret = get('Secret', 'email-bridge-mailbox-projection')
assert secret['type'] == 'Opaque'
assert secret['metadata']['labels']['app.kubernetes.io/managed-by'] == 'control-plane'
assert set(secret['stringData']) == {'mailboxes.json'}
assert not secret.get('data')
seed = json.loads(secret['stringData']['mailboxes.json'])
assert seed == dict(mailboxes=[], managed_by='git', revision=1, source='release-bootstrap', version='email-bridge/v1')
role = get('Role', 'control-plane-email-projection-writer')
assert role['rules'] == [
    dict(apiGroups=[''], resources=['secrets'], resourceNames=['email-bridge-mailbox-projection'], verbs=['get','update']),
    dict(apiGroups=['apps'], resources=['deployments'], resourceNames=['email-bridge','egress-gateway'], verbs=['get','update']),
    dict(apiGroups=['networking.k8s.io'], resources=['networkpolicies'], resourceNames=['egress-gateway-mail-destinations'], verbs=['get','update']),
    dict(apiGroups=[''], resources=['configmaps'], verbs=['get','create']),
]
admission_role = get('ClusterRole', 'control-plane-mail-publication-admission-reader')
assert not admission_role['metadata'].get('namespace')
assert admission_role['rules'] == [dict(apiGroups=['admissionregistration.k8s.io'], resources=['validatingadmissionpolicies','validatingadmissionpolicybindings'], resourceNames=['egress-mail-configmap-publication'], verbs=['get'])]
admission_binding = get('ClusterRoleBinding', admission_role['metadata']['name'])
assert not admission_binding['metadata'].get('namespace')
assert admission_binding['roleRef'] == dict(apiGroup='rbac.authorization.k8s.io', kind='ClusterRole', name=admission_role['metadata']['name'])
assert admission_binding['subjects'] == [dict(kind='ServiceAccount', name='control-plane', namespace=secret['metadata']['namespace'])]
binding = get('RoleBinding', 'control-plane-email-projection-writer')
assert binding['roleRef'] == dict(apiGroup='rbac.authorization.k8s.io', kind='Role', name=role['metadata']['name'])
assert binding['subjects'] == [dict(kind='ServiceAccount', name='control-plane', namespace=secret['metadata']['namespace'])]
pod = get('Deployment', 'control-plane')['spec']['template']['spec']
container = next(c for c in pod['containers'] if c['name'] == 'control-plane')
env = {e['name']: e.get('value') for e in container['env']}
assert env['CONTROL_PLANE_EMAIL_CONFIGURATION_FILE'] == '/var/run/config/kodex/control-plane/email/mailboxes.yaml'
gateway = next(c for c in get('Deployment','egress-gateway')['spec']['template']['spec']['containers'] if c['name']=='egress-gateway')
gateway_env = {e['name']:e.get('value') for e in gateway['env']}
assert env['CONTROL_PLANE_EMAIL_GATEWAY_POLICY_DIGEST'] == gateway_env['EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST']
assert env['CONTROL_PLANE_EMAIL_GRANT_TRUST_FILE'] == '/var/run/config/kodex/control-plane/application-grants/email-bridge.platform-worker.public.jwk'
mount = next(m for m in container['volumeMounts'] if m['name'] == 'email-configuration')
assert mount['readOnly'] and mount['mountPath'] == '/var/run/config/kodex/control-plane/email'
volume = next(v for v in pod['volumes'] if v['name'] == 'email-configuration')['configMap']
assert volume['name'] == 'email-bridge-configuration'
assert volume['items'] == [dict(key='mailboxes.yaml', path='mailboxes.yaml')]
assert yaml.safe_load(get('ConfigMap', volume['name'])['data']['mailboxes.yaml']) == seed
print('Control-plane email projection render passed: ' + sys.argv[2])
PY
done
