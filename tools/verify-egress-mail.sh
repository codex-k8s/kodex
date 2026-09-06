#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf -- "$temporary"' EXIT
(cd "$root/libs/go/mailpolicy" && go test -race ./...)
jq -r '.data["mailboxes.yaml"]' "$root/deploy/k8s/base/control-plane/email-configuration.yaml" >"$temporary/mailboxes.yaml"
for environment in staging production; do
  "$root/tools/render-egress-mail.sh" "$environment" \
    sha256:1111111111111111111111111111111111111111111111111111111111111111 \
    registry.example.test "$temporary/mailboxes.yaml" /etc/resolv.conf >"$temporary/render.yaml"
  yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-mail-destinations") |
    (.spec.policyTypes | length == 1) and .spec.policyTypes[0] == "Egress" and ((.spec.egress // []) | length == 0) and
    .spec.podSelector.matchLabels."app.kubernetes.io/name" == "egress-gateway" and
    .spec.podSelector.matchLabels."app.kubernetes.io/component" == "platform-egress"' "$temporary/render.yaml" >/dev/null
  yq -r 'select(.kind == "ConfigMap" and .data."mail-policy.json" != null) | .data."mail-policy.json"' \
    "$temporary/render.yaml" >"$temporary/policy.json"
  jq -e '.schema == "egress-mail/v1" and .destinations == [] and .configurationRevision == 1' "$temporary/policy.json" >/dev/null
  configmap=$(yq -r 'select(.kind == "ConfigMap" and .data."mail-policy.json" != null and .immutable == true) | .metadata.name' "$temporary/render.yaml")
  reference=$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
    .spec.template.spec.volumes[] | select(.name == "mail-policy") | .configMap.name' "$temporary/render.yaml")
  [[ "$configmap" =~ ^egress-gateway-mail-[a-f0-9]{24}$ && "$reference" == "$configmap" ]] || {
    echo "mail policy content-addressed mount mismatch" >&2; exit 1;
  }
  yq -o=json 'select(.kind == "ValidatingAdmissionPolicy" and .metadata.name == "egress-mail-configmap-publication")' "$temporary/render.yaml" |
    jq -e '.metadata.namespace == null and .spec.failurePolicy == "Fail" and
      .spec.matchConstraints.resourceRules[0].resources == ["configmaps"] and
      .spec.matchConstraints.resourceRules[0].operations == ["CREATE"] and
      (.spec.validations | length == 4)' >/dev/null
  yq -o=json 'select(.kind == "ValidatingAdmissionPolicyBinding" and .metadata.name == "egress-mail-configmap-publication")' "$temporary/render.yaml" |
    jq -e '.metadata.namespace == null and .spec.policyName == "egress-mail-configmap-publication" and
      .spec.validationActions == ["Deny"] and .spec.matchResources.namespaceSelector == {}' >/dev/null
  yq -e 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") |
    ([.spec.template.spec.containers[0].env[] | select(.name == "EGRESS_GATEWAY_MAIL_CONNECT_LISTEN" and .value == ":8082")] | length == 1) and
    ([.spec.template.spec.containers[0].env[] | select(.name == "EGRESS_GATEWAY_MAIL_POLICY_DIGEST" and (.value | length == 64))] | length == 1) and
    ([.spec.template.spec.containers[0].volumeMounts[] | select(.name == "mail-policy" and .readOnly == true)] | length == 1) and
    .spec.template.spec.containers[0].securityContext.runAsNonRoot == true and
    .spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true and
    .spec.template.spec.automountServiceAccountToken == false' "$temporary/render.yaml" >/dev/null
done
yq '.password = "invalid-fixture-field"' "$temporary/mailboxes.yaml" >"$temporary/invalid.yaml"
if "$root/tools/render-egress-mail.sh" staging \
  sha256:1111111111111111111111111111111111111111111111111111111111111111 \
  registry.example.test "$temporary/invalid.yaml" /etc/resolv.conf >"$temporary/rejected.yaml" 2>/dev/null; then
  echo "mail renderer accepted a raw credential field" >&2
  exit 1
fi
[[ ! -s "$temporary/rejected.yaml" ]] || { echo "failed mail producer emitted a partial render" >&2; exit 1; }
echo "Mail source projection, both environment renders and closed bootstrap verification passed."
