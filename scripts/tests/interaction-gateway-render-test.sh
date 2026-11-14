#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Interaction gateway render test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

for profile in web-only web-with-mattermost; do
  kubectl kustomize "$repository_root/deploy/k8s/profiles/$profile" \
    >"$temporary_directory/$profile.yaml"
  yq -o=json -I=0 '.' "$temporary_directory/$profile.yaml" | jq -s -e \
    --arg profile "$profile" \
    --slurpfile registry "$repository_root/tools/install/secret-projections.json" '
    map(select(.kind != null)) as $resources |
    [$resources[] | select(.kind == "Deployment" and .metadata.name == "interaction-gateway")] as $deployments |
      [$resources[] | select(.kind == "ConfigMap" and .metadata.name == "interaction-gateway-runtime")] as $runtime |
      $deployments[0].spec.template.spec as $pod |
      def container($name): $pod.containers[] | select(.name == $name);
      def env($name): [.env[]? | select(.name == $name)];
      ($deployments | length) == 1 and ($runtime | length) == 1 and
      $runtime[0].data.INTERACTION_GATEWAY_TELEGRAM_PROXY_URL == "http://egress-gateway.kodex-system.svc.cluster.local:8086" and
      $runtime[0].data.INTERACTION_GATEWAY_ALLOWED_HOSTS == "" and
      $runtime[0].data.INTERACTION_GATEWAY_OPERATION_TIMEOUT == "30s" and
      $runtime[0].data.INTERACTION_GATEWAY_CONTROL_PLANE_TLS_SERVER_NAME == "control-plane.kodex-system.svc.cluster.local" and
      all($resources[]; .kind != "Deployment" or (.metadata.name != "mattermost" and .metadata.name != "mattermost-postgresql")) and
      $pod.serviceAccountName == "interaction-gateway" and
      $pod.automountServiceAccountToken == false and $pod.securityContext.fsGroup == 29000 and
      (container("interaction-gateway") |
        .securityContext.runAsUser == 10001 and .securityContext.readOnlyRootFilesystem == true and
        env("INTERACTION_GATEWAY_CONTROL_PLANE_PRIVATE_KEY_FILE")[0].value == "/var/run/secrets/kodex/interaction-gateway/workload-tls/tls.key" and
        env("INTERACTION_PUBLIC_ORIGIN")[0].valueFrom.configMapKeyRef.name == "kodex-platform-endpoints" and
        env("INTERACTION_PUBLIC_ORIGIN")[0].valueFrom.configMapKeyRef.key == "publicOrigin" and
        all(.env[]; (.name | startswith("MATTERMOST_")) == false) and
        (env("INTERACTION_GATEWAY_APPLICATION_GRANT_FILE")[0].value ==
          "/var/run/secrets/kodex/interaction-gateway/application-grant/application-grant.jws") and
        any(.volumeMounts[]; .name == "application-grant" and .readOnly == true) and
        any(.volumeMounts[]; .name == "internal-rpc-authority-sockets" and .readOnly == true) and
        all(.volumeMounts[]; .name != "platform-worker-grant-signer" and .name != "internal-rpc-authority-issuer-key")) and
      (container("platform-worker-grant-agent") |
        .securityContext.runAsUser == 29004 and .readinessProbe.httpGet.path == "/readyz" and
        env("PLATFORM_WORKER_GRANT_WORKLOAD_ID")[0].value == "interaction-gateway" and
        env("PLATFORM_WORKER_GRANT_OUTPUT_FILE")[0].value ==
          "/var/run/secrets/kodex/interaction-gateway/application-grant/application-grant.jws" and
        any(.volumeMounts[]; .name == "application-grant" and (.readOnly // false) == false) and
        any(.volumeMounts[]; .name == "platform-worker-grant-signer" and .readOnly == true)) and
      (container("internal-rpc-authority-issuer") |
        .securityContext.runAsUser == 29001 and .readinessProbe.httpGet.path == "/readyz" and
        env("INTERNAL_RPC_AUTHORITY_WORKLOAD_ID")[0].value == "interaction-gateway" and
        env("INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID")[0].value == "10001" and
        env("INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID")[0].value == "10001" and
        env("INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER")[0].valueFrom.secretKeyRef.name ==
          "internal-rpc-authority-interaction-gateway-issuer-postgresql") and
      any($pod.volumes[]; .name == "integration-credentials" and .secret.secretName == "kodex-integration-credentials" and .secret.optional == true) and
      any($pod.volumes[]; .name == "application-grant" and .emptyDir.sizeLimit == "1Mi") and
      any($pod.volumes[]; .name == "platform-worker-grant-signer" and
        .secret.secretName == "interaction-gateway-platform-worker-grant-signer") and
      any($registry[0].secrets[]; .name == "interaction-gateway-platform-worker-grant-signer" and
        .items == [{key:"private.jwk", source:{type:"material",ref:"kodex/platform-worker-grants/interaction-gateway",field:"private.jwk"}}]) and
      any($resources[]; .kind == "Deployment" and .metadata.name == "control-plane" and
        any(.spec.template.spec.containers[]; .name == "control-plane" and
          env("CONTROL_PLANE_INTERACTION_GRANT_TRUST_FILE")[0].value ==
            "/var/run/config/kodex/control-plane/interaction-grant-trust/interaction-gateway.platform-worker.public.jwk") and
        any(.spec.template.spec.volumes[]; .name == "interaction-grant-trust" and
          .secret.secretName == "control-plane-interaction-grant-trust")) and
      any($registry[0].secrets[]; .name == "control-plane-interaction-grant-trust" and
        .items == [{key:"interaction-gateway.platform-worker.public.jwk",source:{type:"material",ref:"kodex/platform-worker-grants/interaction-gateway",field:"public-jwk"}}])
  ' >/dev/null || fail "web interaction deployment or signer-to-consumer trust path is incomplete: $profile"
done

printf 'Interaction gateway render tests passed\n'
