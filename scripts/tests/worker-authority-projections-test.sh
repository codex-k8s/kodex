#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Worker authority projections test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

for profile in web-only web-with-mattermost; do
  render="$temporary_directory/$profile.yaml"
  kubectl kustomize "$repository_root/deploy/k8s/profiles/$profile" >"$render"
  yq -o=json -I=0 '.' "$render" | jq -s 'map(select(.kind != null))' \
    >"$temporary_directory/resources.json"
  yq -r -N 'select(.kind == "ConfigMap" and
    .metadata.name == "internal-rpc-authority-publisher-target-registry") |
    .data."key-delivery-targets.yaml"' "$render" |
    yq -o=json '.' >"$temporary_directory/targets.json"
  jq -e --arg profile "$profile" \
    --slurpfile resources "$temporary_directory/resources.json" \
    --slurpfile projections "$repository_root/tools/install/secret-projections.json" '
    . as $registry | $resources[0] as $resources | $projections[0].secrets as $secrets |
    (if $profile == "web-only" then ["email-bridge"]
     else ["email-bridge", "interaction-gateway"] end) as $workloads |
    def target($workload): [$registry.targets[] | select(.workload_id == $workload)];
    def delivery_names($target): [
      $target.auth_private_key.secret_name, $target.manifest_trust.secret_name,
      $target.authority_proof_trust.secret_name,
      $target.readback.credential_secret_name, $target.readback.possession_key_secret_name,
      $target.restore_coordination.role_credential_secret_name,
      $target.restore_coordination.ack_key_secret_name];
    def network_allows($name; $workload; $port):
      any($resources[]; .kind == "NetworkPolicy" and .metadata.name == $name and
        any(.spec.ingress[]?;
          any(.ports[]?; .protocol == "TCP" and .port == $port) and
          any(.from[]?;
            .podSelector.matchLabels["app.kubernetes.io/name"] == $workload or
            any(.podSelector.matchExpressions[]?;
              .key == "app.kubernetes.io/name" and .operator == "In" and
              (.values | index($workload)) != null))));
    $registry.version == 1 and $registry.source_revision == 7 and
    all($workloads[]; . as $workload |
      target($workload) as $targets |
      ($targets | length) == 1 and $targets[0].role == "AUTHORIZATION_ISSUER" and
      $targets[0].startup_readback_required == true and
      $targets[0].spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/" + $workload and
      $targets[0].database_identity.login_principal ==
        "ira_" + ($workload | gsub("-"; "_")) + "_issuer_g1" and
      all(delivery_names($targets[0])[]; . as $secret |
        any($secrets[]; .name == $secret and .dynamic == true) and
        any($resources[]; .kind == "Role" and .metadata.name == "internal-rpc-authority-publisher" and
          any(.rules[]; .resources == ["secrets"] and .verbs == ["get", "update"] and
            (.resourceNames | index($secret)) != null and
            (.resourceNames | index("*")) == null))) and
      network_allows("control-plane-exact-runtime-paths"; $workload; 8443) and
      network_allows("internal-rpc-authority-readback-attestor-exact-paths"; $workload; 8443) and
      network_allows("internal-rpc-authority-restore-controller-exact-paths"; $workload; 8443) and
      network_allows("platform-postgresql-exact-clients"; $workload; 5432)) and
    (if $profile == "web-only" then
      (target("interaction-gateway") | length) == 0 and
      (network_allows("platform-postgresql-exact-clients"; "interaction-gateway"; 5432) | not)
     else true end)
  ' "$temporary_directory/targets.json" >/dev/null ||
    fail "rendered issuer delivery, exact RBAC or inbound network path is incomplete: $profile"
done

printf 'Worker authority projection render tests passed\n'
