#!/usr/bin/env bash
set -euo pipefail

render=${1:?render path is required}
profile=${2:?deployment profile is required}
case "$profile" in web-only|web-with-mattermost) ;; *) exit 1 ;; esac
targets=$(yq -r '
  select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data."key-delivery-targets.yaml"
' "$render" | yq -o=json -I=0 '.targets' | jq '[.[] | select(.workload_id == "interaction-gateway")]')
yq -o=json -I=0 '.' "$render" | jq -s -e --arg profile "$profile" \
  --argjson targets "$targets" '
  any(.[]; .kind == "ConfigMap" and .metadata.name == "kodex-dev-source-provenance" and
    .data.deploymentProfile == $profile) and
  all(.[] | select(.kind == "Deployment");
    .spec.template.metadata.annotations["kodex.dev/deployment-profile"] == $profile) and
  ([.[] | select(.kind == "Deployment" and .metadata.name == "interaction-gateway")] as $workloads |
    if $profile == "web-only" then
      $workloads == [] and $targets == [] and
      all(.[]; .metadata.name != "interaction-gateway" and .metadata.name != "interaction-gateway-runtime")
    else
      ($workloads | length) == 1 and
      ($targets | length) == 1 and $targets[0].role == "AUTHORIZATION_ISSUER" and
      $targets[0].startup_readback_required == true and
      $targets[0].namespace == "kodex-system" and $targets[0].service_account == "interaction-gateway" and
      ($workloads[0].spec.template.spec |
        .serviceAccountName == "interaction-gateway" and
        ([.containers[].name] | sort) == ["interaction-gateway", "internal-rpc-authority-issuer", "platform-worker-grant-agent"] and
        all(.containers[];
          .command == ["/workspace/tools/dev/run-go-hot-reload.sh"] and
          all(["dev-source", "dev-go-mod", "dev-go-sumdb", "dev-go-tools"][] as $name |
            [.volumeMounts[] | select(.name == $name)]; length == 1 and .[0].readOnly == true)) and
        any(.initContainers[]; .name == "internal-rpc-authority-socket-init" and
          .command == ["/workspace/tools/dev/run-authority-socket-init.sh"])) and
      any(.[]; .kind == "Deployment" and .metadata.name == "control-plane" and
        any(.spec.template.spec.containers[] | select(.name == "control-plane") | .env[];
          .name == "CONTROL_PLANE_INTERACTION_GRANT_TRUST_FILE")) and
      any(.[]; .kind == "NetworkPolicy" and .metadata.name == "control-plane-exact-runtime-paths" and
        any(.spec.ingress[]?.from[]?;
          .podSelector.matchLabels["app.kubernetes.io/name"] == "interaction-gateway"))
    end)
' >/dev/null || {
  printf 'Local deployment profile render is incomplete or mismatched\n' >&2
  exit 1
}
