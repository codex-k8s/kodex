#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fail() { printf 'Local mail projection contract failed: %s\n' "$*" >&2; exit 1; }

# Реальный typed producer; пустые snapshots не обращаются к DNS или mail hosts.
kubectl kustomize "$root/deploy/k8s/profiles/web-only" | yq '
  select((.kind == "Deployment" and (.metadata.name == "egress-gateway" or .metadata.name == "email-bridge")) or
    (.kind == "ConfigMap" and .metadata.name == "email-bridge-runtime"))
' >"$temporary_directory/input.yaml"
printf '%s\n' '---' >>"$temporary_directory/input.yaml"
jq -n '{apiVersion:"v1",kind:"ConfigMap",metadata:{name:"kodex-dev-source-provenance",namespace:"kodex-system"},data:{}}' \
  | yq -p=json -P >>"$temporary_directory/input.yaml"
yq -r 'select(.kind == "Secret") | .stringData."mailboxes.json"' \
  "$root/deploy/k8s/base/control-plane/email-projection.yaml" >"$temporary_directory/source.json"
for revision in 1 2; do
  jq --argjson revision "$revision" '.revision = $revision' "$temporary_directory/source.json" >"$temporary_directory/source-$revision.json"
  bash "$root/tools/dev/render-local-mail.sh" "$temporary_directory/input.yaml" \
    "$temporary_directory/source-$revision.json" /etc/resolv.conf "$temporary_directory/render-$revision.yaml"
  yq -o=json -I=0 '.' "$temporary_directory/render-$revision.yaml" | jq -s -e --argjson revision "$revision" '
    def r($kind; $name): .[] | select(.kind == $kind and .metadata.name == $name);
    . as $all |
    ([.[] | select(.kind == "ConfigMap" and .data["mail-policy.json"] != null)][0]) as $cm |
    ($cm.data["mail-policy.json"] | fromjson) as $policy |
    ($all | r("ConfigMap"; "email-bridge-runtime") | .data.EMAIL_BRIDGE_EGRESS_POLICY_DIGEST) as $digest |
    ([.[] | select(.kind == "ConfigMap" and .data["mail-policy.json"] != null)] | length == 1) and
    ($digest | test("^[a-f0-9]{64}$")) and $cm.immutable == true and
    $cm.metadata.name == ("egress-gateway-mail-" + $digest[:24]) and
    $policy.configurationRevision == $revision and $policy.destinations == [] and
    ($all | r("NetworkPolicy"; "egress-gateway-mail-destinations") | .spec.egress == [] and
      .spec.podSelector.matchLabels["app.kubernetes.io/name"] == "egress-gateway") and
    ($all | r("Deployment"; "egress-gateway") | .spec.template.spec |
      any(.containers[].env[]; .name == "EGRESS_GATEWAY_MAIL_POLICY_DIGEST" and .value == $digest) and
      any(.volumes[]; .name == "mail-policy" and .configMap.name == $cm.metadata.name)) and
    all($all | r("Deployment"; "email-bridge"), r("Deployment"; "egress-gateway");
      .spec.template.metadata.annotations["kodex.dev/mail-policy-digest"] == $digest and
      .spec.template.metadata.annotations["kodex.dev/mail-configuration-digest"] == $policy.configurationDigest)
  ' >/dev/null || fail 'consumer digest or CNI projection mismatch'
done
first=$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "email-bridge-runtime") | .data.EMAIL_BRIDGE_EGRESS_POLICY_DIGEST' "$temporary_directory/render-1.yaml")
second=$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "email-bridge-runtime") | .data.EMAIL_BRIDGE_EGRESS_POLICY_DIGEST' "$temporary_directory/render-2.yaml")
[[ "$first" != "$second" ]] || fail 'new snapshot retained old digest'
jq '.password = "fixture-rejected-value"' "$temporary_directory/source.json" >"$temporary_directory/invalid.json"
if bash "$root/tools/dev/render-local-mail.sh" "$temporary_directory/input.yaml" \
  "$temporary_directory/invalid.json" /etc/resolv.conf "$temporary_directory/rejected.yaml" >/dev/null 2>&1; then
  fail 'untyped credential input accepted'
fi
[[ ! -e "$temporary_directory/rejected.yaml" ]] || fail 'failure published a partial render'

# Только fake kubectl: actual CP Secret document читается без credential values в выводе.
export fixture_secret="$temporary_directory/secret.json"
kubectl() {
  [[ "$*" == '-n kodex-system get secret/email-bridge-mailbox-projection --ignore-not-found --request-timeout=20s -o json' ]] || return 1
  [[ "${fixture_failure:-false}" != true ]] || return 1
  if [[ -f "$fixture_secret" ]]; then cat "$fixture_secret"; fi
}
export -f kubectl
bash "$root/tools/dev/read-local-mail-configuration.sh" "$temporary_directory/current.json"
cmp "$temporary_directory/source.json" "$temporary_directory/current.json" || fail 'bootstrap source mismatch'
jq -n --rawfile document "$temporary_directory/source-2.json" '{kind:"Secret",type:"Opaque",
  metadata:{name:"email-bridge-mailbox-projection",namespace:"kodex-system",uid:"fixture-uid",
    labels:{"app.kubernetes.io/managed-by":"control-plane"}},data:{"mailboxes.json":($document | @base64)}}' >"$fixture_secret"
bash "$root/tools/dev/read-local-mail-configuration.sh" "$temporary_directory/current.json"
jq -e '.revision == 2' "$temporary_directory/current.json" >/dev/null || fail 'published generation was ignored'

# Точная функция installer отклоняет stale render до foundation apply.
# Переменные читает извлечённая из installer функция.
# shellcheck disable=SC2034
script_directory="$root/tools/dev"
render="$temporary_directory/render-2.yaml"
# shellcheck disable=SC1090
source <(awk '/^verify_email_projection_generation\(\) \{/ {capture=1} capture {print} capture && /^}$/ {exit}' "$root/tools/dev/deploy-local.sh")
verify_email_projection_generation
# shellcheck disable=SC2034
render="$temporary_directory/render-1.yaml"
if (verify_email_projection_generation) >/dev/null 2>&1; then fail 'stale render accepted'; fi
jq '.metadata.labels["app.kubernetes.io/managed-by"] = "foreign"' "$fixture_secret" >"$temporary_directory/foreign.json"
mv "$temporary_directory/foreign.json" "$fixture_secret"
if bash "$root/tools/dev/read-local-mail-configuration.sh" "$temporary_directory/rejected-source.json" >/dev/null 2>&1; then fail 'foreign owner accepted'; fi
export fixture_failure=true
if bash "$root/tools/dev/read-local-mail-configuration.sh" "$temporary_directory/rejected-source.json" >/dev/null 2>&1; then fail 'API failure became bootstrap fallback'; fi
printf 'Local mail projection contract passed: typed generations, coordinated digests/CNI/rollout, CP readback and stale rejection\n'
