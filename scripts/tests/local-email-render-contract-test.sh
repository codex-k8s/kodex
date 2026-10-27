#!/usr/bin/env bash
set -euo pipefail

render=${1:?render path is required}
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
verifier="$root/tools/dev/verify-local-email-render.sh"
"$verifier" "$render"
count=0
for mutation in \
  'select(.kind != "Deployment" or .metadata.name != "email-bridge")' \
  'select(.kind != "Job" or .metadata.name != "email-bridge-migration")' \
  'select(.kind != "StatefulSet" or .metadata.name != "email-bridge-postgresql")' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.containers) |= map(select(.name != "internal-rpc-authority-issuer"))' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.containers) |= map(select(.name != "platform-worker-grant-agent"))' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.initContainers[].securityContext.runAsUser) = 0' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.containers[].volumeMounts[] | select(.name == "dev-go-mod") | .readOnly) = false' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.containers[].securityContext.readOnlyRootFilesystem) = false' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.containers[] | select(.name == "email-bridge") | .readinessProbe.exec) = {"command":["true"]}' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.containers[].image) = "golang:latest"' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.metadata.annotations."kodex.dev/source-revision") = "foreign"' \
  '(select(.kind == "Job" and .metadata.name == "email-bridge-migration") | .spec.template.spec.containers[].args) = ["services/internal/email-bridge", "./cmd/cli"]' \
  '(select(.kind == "Job" and .metadata.name == "email-bridge-migration") | .spec.template.spec.volumes[] | select(.name == "database") | .secret.secretName) = "email-bridge-runtime-database"' \
  '(select(.kind == "ConfigMap" and .metadata.name == "email-bridge-runtime") | .data.EMAIL_BRIDGE_AUTHORITY_TARGET) = "control-plane:80"' \
  '(select(.kind == "ConfigMap" and .metadata.name == "email-bridge-runtime") | .data.EMAIL_BRIDGE_EGRESS_ADDRESS) = "egress-gateway.kodex-system.svc:8080"' \
  '(select(.kind == "ConfigMap" and .metadata.name == "email-bridge-runtime") | .data.EMAIL_BRIDGE_EGRESS_ADDRESS) = "egress-gateway.kodex-system.svc:8081"' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.volumes[] | select(.name == "mail") | .secret.optional) = true' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.volumes[] | select(.name == "mail") | .secret.items) = [{"key":"mailboxes.json","path":"mailboxes.json"}]' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.spec.containers[] | select(.name == "email-bridge") | .volumeMounts[] | select(.name == "mail") | .subPath) = "mailboxes.json"' \
  'select(.kind != "Secret" or .metadata.name != "email-bridge-mailbox-projection")' \
  '(select(.kind == "Role" and .metadata.name == "control-plane-email-projection-writer") | .rules[0].resourceNames) = ["foreign-projection"]' \
  '(select(.kind == "Role" and .metadata.name == "control-plane-email-projection-writer") | .rules[1].resourceNames) = ["foreign-deployment"]' \
  '(select(.kind == "Role" and .metadata.name == "control-plane-email-projection-writer") | .rules[2].resourceNames) = ["foreign-network-policy"]' \
  '(select(.kind == "Role" and .metadata.name == "control-plane-email-projection-writer") | .rules[3].verbs) += ["update"]' \
  '(select(.kind == "Role" and .metadata.name == "control-plane-email-projection-writer") | .rules) += [{"apiGroups":["*"],"resources":["*"],"verbs":["*"]}]' \
  '(select(.kind == "RoleBinding" and .metadata.name == "control-plane-email-projection-writer") | .subjects[0].name) = "email-bridge"' \
  '(select(.kind == "ClusterRole" and .metadata.name == "control-plane-mail-publication-admission-reader") | .rules[0].verbs) += ["update"]' \
  '(select(.kind == "ClusterRoleBinding" and .metadata.name == "control-plane-mail-publication-admission-reader") | .subjects[0].name) = "email-bridge"' \
  'select(.kind != "ValidatingAdmissionPolicy" or .metadata.name != "egress-mail-configmap-publication")' \
  '(select(.kind == "ValidatingAdmissionPolicy" and .metadata.name == "egress-mail-configmap-publication") | .spec.failurePolicy) = "Ignore"' \
  '(select(.kind == "ValidatingAdmissionPolicyBinding" and .metadata.name == "egress-mail-configmap-publication") | .spec.validationActions) = ["Audit"]' \
  '(select(.kind == "ConfigMap" and .metadata.name == "email-bridge-runtime") | .data.EMAIL_BRIDGE_EGRESS_POLICY_DIGEST) = "stale"' \
  '(select(.kind == "Deployment" and .metadata.name == "email-bridge") | .spec.template.metadata.annotations."kodex.dev/mail-policy-digest") = "stale"' \
  '(select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-mail-destinations") | .spec.egress) = [{}]' \
  '(select(.kind == "Deployment" and .metadata.name == "egress-gateway") | .spec.template.spec.volumes[] | select(.name == "mail-policy") | .configMap.name) = "foreign"' \
  '(select(.kind == "ConfigMap" and .data."mail-policy.json" != null) | .data."mail-policy.json") = "{}"' \
  '(select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") | .data."key-delivery-targets.yaml") |= (from_yaml | .targets |= map(select(.workload_id != "email-bridge")) | to_yaml)' \
  '(select(.kind == "NetworkPolicy" and .metadata.name == "email-bridge") | .spec.egress) = [{}]' \
  'select(.kind != "NetworkPolicy" or .metadata.name != "email-bridge-internal-rpc-authority-exact-paths")' \
  '(select(.kind == "NetworkPolicy" and .metadata.name == "email-bridge-internal-rpc-authority-exact-paths") | .spec.egress) = []' \
  '(select(.kind == "NetworkPolicy" and .metadata.name == "email-bridge-internal-rpc-authority-exact-paths") | .spec.egress) += [{}]' \
  '(select(.kind == "NetworkPolicy" and .metadata.name == "email-bridge-internal-rpc-authority-exact-paths") | .spec.egress[1].ports[0].port) = 5433' \
  '(select(.kind == "NetworkPolicy" and .metadata.name == "email-bridge-internal-rpc-authority-exact-paths") | .metadata.namespace) = "foreign"' \
  '(select(.kind == "NetworkPolicy" and .metadata.name == "email-bridge-internal-rpc-authority-exact-paths") | .spec.egress[1].to[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name") = "foreign"' \
  '(select(.kind == "Ingress") | .spec.rules[].http.paths[].backend.service.name) = "email-bridge"'; do
  yq "$mutation" "$render" >"$temporary_directory/negative.yaml"
  if "$verifier" "$temporary_directory/negative.yaml" >/dev/null 2>&1; then
    printf 'Local EMAIL verifier accepted broken render case %s\n' "$count" >&2
    exit 1
  fi
  count=$((count + 1))
done
# Два отдельных YAML documents: yq объединяет повторные ссылки на один source node.
{
  cat -- "$render"
  printf '\n---\n'
  yq 'select(.kind == "NetworkPolicy" and .metadata.name == "email-bridge-internal-rpc-authority-exact-paths")' "$render"
} >"$temporary_directory/negative.yaml"
if "$verifier" "$temporary_directory/negative.yaml" >/dev/null 2>&1; then
  printf 'Local EMAIL verifier accepted duplicate policy identity\n' >&2
  exit 1
fi
count=$((count + 1))
# Даже частично полученный expected stream не разрешает пропустить ошибку следующей базы.
real_kubectl=$(command -v kubectl)
install -d "$temporary_directory/bin"
cat >"$temporary_directory/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 2 && "$1" == kustomize ]] || exit 64
if [[ "${2##*/}" == "$KODEX_TEST_FAILED_BASE" ]]; then
  [[ "$KODEX_TEST_SOURCE_OUTCOME" != empty ]] || exit 0
  exit 42
fi
exec "$KODEX_TEST_REAL_KUBECTL" "$@"
EOF
chmod 0555 "$temporary_directory/bin/kubectl"
for source_outcome in fail empty; do
  for failed_base in email-bridge email-bridge-data email-bridge-migration; do
    if PATH="$temporary_directory/bin:$PATH" KODEX_TEST_FAILED_BASE="$failed_base" \
      KODEX_TEST_SOURCE_OUTCOME="$source_outcome" \
      KODEX_TEST_REAL_KUBECTL="$real_kubectl" "$verifier" "$render" >/dev/null 2>&1; then
      printf 'Local EMAIL verifier ignored an invalid expected-policy source\n' >&2
      exit 1
    fi
    count=$((count + 1))
  done
done
printf 'Local EMAIL render contract passed: positive and %s negative cases\n' "$count"
