#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Application render failed: %s\n' "$*" >&2; exit 1; }
usage() { printf 'Usage: %s --lock <path> --output <path> --scope release|bootstrap|interfaces\n' "$0" >&2; }

lock_file=""
output=""
scope=""
while (($# > 0)); do
  case "$1" in
    --lock) lock_file="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --scope) scope="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
[[ -n "$output" ]] || fail "output path is required"
case "$scope" in release|bootstrap|interfaces) ;; *) fail "scope must be release, bootstrap or interfaces" ;; esac
if [[ "$scope" == release ]]; then
  [[ -r "$lock_file" ]] || fail "release lock is not readable"
fi
for command_name in kubectl yq jq; do command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"; done

script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repository_root=$(cd -- "$script_directory/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
combined="$temporary_directory/combined.yaml"
: >"$combined"

components=(
  control-plane
  internal-rpc-authority
  runtime-controller
  interaction-gateway
  integration-gateway
  control-api-gateway
  staff-control-center
  automation-scheduler
  role-image-builder
  egress-gateway
)
for component in "${components[@]}"; do
  kubectl kustomize "$repository_root/deploy/k8s/overlays/production/$component" >>"$combined"
  printf '%s\n' '---' >>"$combined"
done

SCOPE="$scope" yq eval-all '
  select(.kind != null) |
  select(
    ((strenv(SCOPE) == "release" or strenv(SCOPE) == "interfaces") and
      (.kind == "ConfigMap" or .kind == "Service" or .kind == "Deployment" or
       .kind == "DaemonSet" or .kind == "Job" or .kind == "CronJob")) or
    (strenv(SCOPE) == "bootstrap" and
      (.kind == "ServiceAccount" or .kind == "Role" or .kind == "RoleBinding" or
       .kind == "ClusterRole" or .kind == "ClusterRoleBinding" or
       .kind == "ValidatingAdmissionPolicy" or .kind == "ValidatingAdmissionPolicyBinding" or
       .kind == "NetworkPolicy"))
  ) |
  select((.metadata.name | test("^(runtime-(archive|restore-verifier)|runtime-controller-(archive-workers-s3|s3-security-policy)|runtime-s3-(archive|restore)(-.*)?|runtime-s3-(exchanger|readback)-.*)$")) | not) |
  select((.metadata.name | test("^(mattercodex-(buildkit|image-|registry-)|.*role-image-builder.*|.*-dashboard$)")) | not)
' "$combined" >"$temporary_directory/filtered.yaml"

# Direct-production prototype заменяет Vault CSI mounts на материализованные
# владельцем Kubernetes Secrets, сохраняя имена и пути файлов интерфейса.
# Field path передаётся через окружение: literal single quotes внутри shell-
# quoted yq program иначе теряются при разборе Bash.
profile_field_path="metadata.labels['mattercodex.dev/profile']"
PROFILE_FIELD_PATH="$profile_field_path" yq eval-all '
  select(.kind != null) |
  with(select(.metadata.namespace != null); .metadata.namespace = "mattercodex-system") |
  .metadata.labels."mattercodex.dev/profile" = "direct-production-single-node-prototype" |
  .metadata.labels."mattercodex.dev/release-managed" = "true" |
  with(select(.kind == "Deployment" or .kind == "DaemonSet" or .kind == "Job");
    .spec.template.metadata.labels."mattercodex.dev/profile" = "direct-production-single-node-prototype" |
    .spec.template.metadata.labels."mattercodex.dev/release-managed" = "true" |
    .spec.template.spec.automountServiceAccountToken = false |
    .spec.template.spec.containers[] |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      with(select(.name == "publisher" or .name == "reconciler" or
          .name == "internal-rpc-authority-issuer" or
          .name == "internal-rpc-authority-verifier");
        .env = ((.env // []) | map(select((.name | test("^INTERNAL_RPC_AUTHORITY_(PUBLISHER_)?VAULT_")) | not))) + [
          {"name":"INTERNAL_RPC_AUTHORITY_SECRET_BACKEND","value":"direct-production-kubernetes-file"},
          {"name":"INTERNAL_RPC_AUTHORITY_DEPLOYMENT_PROFILE","valueFrom":{"fieldRef":{"fieldPath":strenv(PROFILE_FIELD_PATH)}}}
        ] |
        .volumeMounts = ((.volumeMounts // []) |
          map(select((.name | contains("vault")) | not)))
      ) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.template.spec.initContainers[]? |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      with(select(.name == "internal-rpc-authority-issuer" or
          .name == "internal-rpc-authority-verifier");
        .env = ((.env // []) | map(select((.name | test("^INTERNAL_RPC_AUTHORITY_(PUBLISHER_)?VAULT_")) | not))) + [
          {"name":"INTERNAL_RPC_AUTHORITY_SECRET_BACKEND","value":"direct-production-kubernetes-file"},
          {"name":"INTERNAL_RPC_AUTHORITY_DEPLOYMENT_PROFILE","valueFrom":{"fieldRef":{"fieldPath":strenv(PROFILE_FIELD_PATH)}}}
        ] |
        .volumeMounts = ((.volumeMounts // []) |
          map(select((.name | contains("vault")) | not)))
      ) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.template.spec.volumes = ((.spec.template.spec.volumes // []) |
      map(select((.name | test("otel|sentry|observability")) | not)) |
      map(select((.name | contains("vault")) | not))) |
    (.spec.template.spec.volumes[]? | select(.csi != null)) |= {
      "name": .name,
      "secret": {"secretName": .csi.volumeAttributes.secretProviderClass, "defaultMode": 288}
    } |
    .spec.template.spec.volumes[]? |= (
      with(select(.secret != null and
          (.secret.secretName | test("^internal-rpc-authority-.*-(issuer|resolver)-key$")));
        .secret.items = [{"key":"private.jwk","path":"private.jwk"}]
      ) |
      with(select(.secret != null and
          .secret.secretName != "internal-rpc-authority-publisher-manifest-trust" and
          (.secret.secretName | test("^internal-rpc-authority-.*-manifest-trust$")));
        .secret.items = [{"key":"bundle.jws","path":"bundle.jws"}]
      ) |
      with(select(.secret != null and
          (.secret.secretName | test("^internal-rpc-authority-.*-(proof-trust|resolver-trust)$")));
        .secret.items = [{"key":"jwks.json","path":"jwks.json"}]
      )
    )
  ) |
  with(select(.kind == "Deployment");
    (.spec.template.spec.containers[]?.env[]? | select(.name == "CONTROL_PLANE_NATS_REPLICAS").value) = "1"
  ) |
  with(select(.kind == "CronJob");
    .spec.jobTemplate.spec.template.metadata.labels."mattercodex.dev/profile" = "direct-production-single-node-prototype" |
    .spec.jobTemplate.spec.template.metadata.labels."mattercodex.dev/release-managed" = "true" |
    .spec.jobTemplate.spec.template.spec.automountServiceAccountToken = false |
    .spec.jobTemplate.spec.template.spec.containers[] |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.jobTemplate.spec.template.spec.initContainers[]? |= (
      .env = ((.env // []) |
        map(select((.name | test("^(OTEL_|SENTRY_)")) | not)) +
        [{"name": "OTEL_SDK_DISABLED", "value": "true"}]) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select((.name | test("otel|sentry|observability")) | not)))
    ) |
    .spec.jobTemplate.spec.template.spec.volumes = ((.spec.jobTemplate.spec.template.spec.volumes // []) |
      map(select((.name | test("otel|sentry|observability")) | not))) |
    (.spec.jobTemplate.spec.template.spec.volumes[]? | select(.csi != null)) |= {
      "name": .name,
      "secret": {"secretName": .csi.volumeAttributes.secretProviderClass, "defaultMode": 288}
    } |
    .spec.jobTemplate.spec.template.spec.volumes[]? |= (
      with(select(.secret != null and
          (.secret.secretName | test("^internal-rpc-authority-.*-(issuer|resolver)-key$")));
        .secret.items = [{"key":"private.jwk","path":"private.jwk"}]
      ) |
      with(select(.secret != null and
          .secret.secretName != "internal-rpc-authority-publisher-manifest-trust" and
          (.secret.secretName | test("^internal-rpc-authority-.*-manifest-trust$")));
        .secret.items = [{"key":"bundle.jws","path":"bundle.jws"}]
      ) |
      with(select(.secret != null and
          (.secret.secretName | test("^internal-rpc-authority-.*-(proof-trust|resolver-trust)$")));
        .secret.items = [{"key":"jwks.json","path":"jwks.json"}]
      )
    )
  ) |
  with(select(.kind == "ConfigMap" and .data != null);
    .data = (.data | with_entries(select((.key | test("^(OTEL_|SENTRY_)")) | not)))
  ) |
  with(select(.kind == "ConfigMap" and
      (.metadata.name == "internal-rpc-authority-publisher" or
       .metadata.name == "internal-rpc-authority-database-credential-reconciler"));
    .data = (.data | with_entries(
      select((.key | test("^INTERNAL_RPC_AUTHORITY_(PUBLISHER_)?VAULT_")) | not)
    ))
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^https://object-store\\.storage\\.svc\\.cluster\\.local$";
    "https://mattercodex-object-store.mattercodex-system.svc.cluster.local"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^object-store\\.storage\\.svc\\.cluster\\.local$";
    "mattercodex-object-store.mattercodex-system.svc.cluster.local"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^registry-pull\\.invalid/mattercodex/roles$";
    "localhost:5001/mattercodex/roles"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^https://mattermost\\.mattermost\\.svc\\.cluster\\.local$";
    "https://mattercodex-legacy-mattermost-bridge.mattercodex-system.svc.cluster.local:8443"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^mattermost\\.mattermost\\.svc\\.cluster\\.local$";
    "mattercodex-legacy-mattermost-bridge.mattercodex-system.svc.cluster.local"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^https://matter-codex-bot-service\\.mattercodex-system\\.svc\\.cluster\\.local:8443$";
    "https://mattercodex-legacy-bot-service-bridge.mattercodex-system.svc.cluster.local:8443"
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^matter-codex-bot-service\\.mattercodex-system\\.svc\\.cluster\\.local$";
    "mattercodex-legacy-bot-service-bridge.mattercodex-system.svc.cluster.local"
  ) |
  with(select(.kind == "NetworkPolicy");
    .spec.egress[]? |= (
      with(select(.to[0].podSelector.matchLabels."app.kubernetes.io/name" == "matter-codex-bot-service");
        .to = [{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"mattercodex-legacy-bot-service-bridge"}}}] |
        .ports = [{"protocol":"TCP","port":8443}]
      ) |
      with(select(.to[0].podSelector.matchLabels."app.kubernetes.io/name" == "mattermost");
        .to = [{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"mattercodex-legacy-mattermost-bridge"}}}] |
        .ports = [{"protocol":"TCP","port":8443}]
      ) |
      with(select(
        .to[0].podSelector.matchLabels."app.kubernetes.io/name" == "control-plane-postgresql" or
        .to[0].podSelector.matchLabels."app.kubernetes.io/name" == "internal-rpc-authority-postgresql" or
        .to[0].podSelector.matchLabels."app.kubernetes.io/name" == "runtime-controller-postgresql"
      );
        .to[0].podSelector.matchLabels."app.kubernetes.io/name" = "mattercodex-postgresql"
      ) |
      with(select(.to[0].podSelector.matchLabels."app.kubernetes.io/name" == "control-plane-redis");
        .to[0].podSelector.matchLabels."app.kubernetes.io/name" = "mattercodex-redis"
      ) |
      with(select(.to[0].podSelector.matchLabels."app.kubernetes.io/name" == "nats");
        .to[0].podSelector.matchLabels."app.kubernetes.io/name" = "mattercodex-nats"
      ) |
      with(select(.to[0].podSelector.matchLabels."app.kubernetes.io/name" == "object-store");
        .to[0].podSelector.matchLabels."app.kubernetes.io/name" = "mattercodex-object-store"
      )
    )
  ) |
  (.. | select(tag == "!!str")) |= sub(
    "^mattercodex-image-registry-push\\.mattercodex-system\\.svc\\.cluster\\.local:5001/staging/role-images$";
    "matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000/mattercodex/role-images-staging"
  )
' "$temporary_directory/filtered.yaml" >"$temporary_directory/normalized.yaml"

# Direct-production adapters A–D используют две exact namespaced Secret CAS-границы
# и immutable file material. Projected Kubernetes API token получают только main
# containers gateway; authority sidecars остаются file-only.
yq -i '
  with(select(.kind == "ConfigMap" and .metadata.name == "integration-gateway-runtime");
    .data = ((.data // {}) | with_entries(select((.key | test("^INTEGRATION_GATEWAY_VAULT_")) | not))) |
    .data.INTEGRATION_GATEWAY_DEPLOYMENT_PROFILE = "direct-production-single-node-prototype" |
    .data.INTEGRATION_GATEWAY_SECRET_BACKEND = "direct-production-kubernetes-file" |
    .data.INTEGRATION_GATEWAY_OIDC_VERIFIER_BACKEND = "direct-production-file" |
    .data.INTEGRATION_GATEWAY_KUBERNETES_PROVIDER_SECRET_NAME = "integration-gateway-provider-credentials" |
    .data.INTEGRATION_GATEWAY_KUBERNETES_PROVIDER_SECRET_DATA_KEY = "state.json" |
    .data.INTEGRATION_GATEWAY_GIT_CREDENTIAL_AGGREGATE_FILE = "/var/run/secrets/mattercodex/integration-gateway/git-credentials/state.json" |
    .data.INTEGRATION_GATEWAY_OIDC_PROVIDER_SNAPSHOT_FILE = "/var/run/config/mattercodex/integration-gateway/oidc/provider-snapshot.json"
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "interaction-gateway-runtime");
    .data = ((.data // {}) | with_entries(select((.key | test("^INTERACTION_GATEWAY_BOT_CREDENTIAL_VAULT_")) | not))) |
    .data.INTERACTION_GATEWAY_DEPLOYMENT_PROFILE = "direct-production-single-node-prototype" |
    .data.INTERACTION_GATEWAY_BOT_CREDENTIAL_BACKEND = "direct-production-kubernetes-file" |
    .data.INTERACTION_GATEWAY_BOT_CREDENTIAL_KUBERNETES_RESOURCE_NAME = "interaction-gateway-bot-credentials" |
    .data.INTERACTION_GATEWAY_BOT_CREDENTIAL_KUBERNETES_DATA_KEY = "state.json" |
    .data.INTERACTION_GATEWAY_BOT_CREDENTIAL_KUBERNETES_TIMEOUT = "5s"
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "control-plane-runtime");
    del(.data.CONTROL_PLANE_RUNTIME_ARCHIVE_SIGNING_KEY_FILE) |
    del(.data.CONTROL_PLANE_RUNTIME_RESTORE_SIGNING_KEY_FILE) |
    .data.CONTROL_PLANE_RUNTIME_ARCHIVE_RESTORE_CAPABILITY = "disabled"
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "runtime-controller-runtime");
    del(.data.RUNTIME_ARCHIVE_SERVICE_ACCOUNT) |
    del(.data.RUNTIME_RESTORE_SERVICE_ACCOUNT) |
    del(.data.RUNTIME_S3_ARCHIVE_BROKER_SERVICE_ACCOUNT) |
    del(.data.RUNTIME_S3_RESTORE_BROKER_SERVICE_ACCOUNT) |
    del(.data.RUNTIME_S3_ENDPOINT) |
    del(.data.RUNTIME_S3_TLS_SERVER_NAME) |
    del(.data.RUNTIME_S3_BUCKET) |
    del(.data.RUNTIME_S3_REGION) |
    .data.RUNTIME_ARCHIVE_RESTORE_CAPABILITY = "disabled" |
    .data.RUNTIME_ARCHIVE_RESTORE_FOLLOW_UP_ISSUE = "https://github.com/codex-k8s/matter-codex/issues/310"
  ) |
  with(select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry");
    .data."key-delivery-targets.yaml" = (
      .data."key-delivery-targets.yaml" | from_yaml |
      .source_revision = 7 |
      .targets = (.targets | map(select(.workload_id != "runtime-s3-restore-exchanger"))) |
      to_yaml
    )
  ) |
  with(select(.kind == "Role" and .metadata.name == "internal-rpc-authority-publisher");
    .rules[0].resourceNames |= map(select(
      test("^internal-rpc-authority-runtime-s3-restore-exchanger-") | not
    ))
  ) |
  with(select(.kind == "NetworkPolicy" and .metadata.name == "runtime-controller-workers-exact-paths");
    .spec.podSelector.matchExpressions[0].values |= map(select(. != "runtime-archive" and
      . != "runtime-restore-verifier" and . != "runtime-rehydrate"))
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "runtime-workload-admission");
    (.spec.template.spec.containers[] | select(.name == "admission")).env |=
      (map(select(
        .name != "RUNTIME_ADMISSION_S3_ARCHIVE_PUBLIC_KEY_FILE" and
        .name != "RUNTIME_ADMISSION_S3_RESTORE_PUBLIC_KEY_FILE" and
        .name != "RUNTIME_S3_READBACK_IMAGE" and
        .name != "RUNTIME_ARCHIVE_RESTORE_CAPABILITY"
      )) + [{"name":"RUNTIME_ARCHIVE_RESTORE_CAPABILITY","value":"disabled"}]) |
    (.spec.template.spec.volumes[] | select(.name == "ticket-trust" and .secret != null)).secret.items =
      [{"key":"public-key.hex","path":"public-key.hex"}]
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "control-plane");
    (.spec.template.spec.volumes[] | select(.name == "runtime-workload-signing" and .secret != null)).secret.items =
      [{"key":"admission-private-key.hex","path":"admission-private-key.hex"}]
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "integration-gateway");
    .spec.template.metadata.labels."mattercodex.dev/runtime-secret-api" = "integration-gateway" |
    .spec.template.spec.containers[] |= with(select(.name == "integration-gateway");
      .env = ((.env // []) | map(select(.name != "INTEGRATION_GATEWAY_OIDC_PROVIDER_SNAPSHOT_SHA256" and
        .name != "INTEGRATION_GATEWAY_OIDC_PROVIDER_SNAPSHOT_GENERATION")) + [
        {"name":"INTEGRATION_GATEWAY_OIDC_PROVIDER_SNAPSHOT_SHA256","valueFrom":{"configMapKeyRef":{"name":"integration-gateway-oidc-provider","key":"provider-snapshot.sha256"}}},
        {"name":"INTEGRATION_GATEWAY_OIDC_PROVIDER_SNAPSHOT_GENERATION","valueFrom":{"configMapKeyRef":{"name":"integration-gateway-oidc-provider","key":"provider-snapshot.generation"}}}
      ]) |
      .volumeMounts = ((.volumeMounts // []) |
        map(select(.name != "vault-ca" and .name != "vault-token" and .name != "oidc-ca")) + [
          {"name":"direct-kubernetes-api-token","mountPath":"/var/run/secrets/tokens/kubernetes-api","readOnly":true},
          {"name":"direct-kubernetes-api-ca","mountPath":"/var/run/config/kubernetes.io/serviceaccount","readOnly":true},
          {"name":"direct-git-credentials","mountPath":"/var/run/secrets/mattercodex/integration-gateway/git-credentials","readOnly":true},
          {"name":"direct-oidc-provider","mountPath":"/var/run/config/mattercodex/integration-gateway/oidc","readOnly":true}
        ]
      )
    ) |
    .spec.template.spec.volumes = ((.spec.template.spec.volumes // []) |
      map(select(.name != "vault-ca" and .name != "vault-token" and .name != "oidc-ca")) + [
        {"name":"direct-kubernetes-api-token","projected":{"defaultMode":256,"sources":[{"serviceAccountToken":{"path":"token","expirationSeconds":600}}]}},
        {"name":"direct-kubernetes-api-ca","configMap":{"name":"kube-root-ca.crt","defaultMode":288,"items":[{"key":"ca.crt","path":"ca.crt"}]}},
        {"name":"direct-git-credentials","secret":{"secretName":"integration-gateway-git-credentials","defaultMode":288,"items":[{"key":"state.json","path":"state.json"}]}},
        {"name":"direct-oidc-provider","configMap":{"name":"integration-gateway-oidc-provider","defaultMode":288,"items":[{"key":"provider-snapshot.json","path":"provider-snapshot.json"}]}}
      ]
    )
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "interaction-gateway");
    .spec.template.metadata.labels."mattercodex.dev/runtime-secret-api" = "interaction-gateway" |
    .spec.template.spec.containers[] |= with(select(.name == "interaction-gateway");
      .volumeMounts = ((.volumeMounts // []) |
        map(select(.name != "vault-ca" and .name != "vault-token")) + [
          {"name":"direct-kubernetes-api-token","mountPath":"/var/run/secrets/tokens/kubernetes-api","readOnly":true},
          {"name":"direct-kubernetes-api-ca","mountPath":"/var/run/config/kubernetes.io/serviceaccount","readOnly":true}
        ]
      )
    ) |
    .spec.template.spec.volumes = ((.spec.template.spec.volumes // []) |
      map(select(.name != "vault-ca" and .name != "vault-token")) + [
        {"name":"direct-kubernetes-api-token","projected":{"defaultMode":256,"sources":[{"serviceAccountToken":{"path":"token","expirationSeconds":600}}]}},
        {"name":"direct-kubernetes-api-ca","configMap":{"name":"kube-root-ca.crt","defaultMode":288,"items":[{"key":"ca.crt","path":"ca.crt"}]}}
      ]
    )
  ) |
  with(select(.kind == "Deployment" and .metadata.name == "internal-rpc-authority-publisher");
    .spec.template.spec.volumes[] |= with(select(.name == "publisher-config" and .projected != null);
      .projected.sources = ((.projected.sources // []) |
        map(select(.configMap.name != "internal-rpc-authority-vault-ca")))
    )
  ) |
  with(select(.kind == "NetworkPolicy" and .metadata.name == "integration-gateway-exact-runtime-paths");
    .spec.egress = ((.spec.egress // []) | map(select(
      .to[0].podSelector.matchLabels."app.kubernetes.io/name" != "vault" and
      .to[0].podSelector.matchLabels."app.kubernetes.io/name" != "sso")))
  ) |
  with(select(.kind == "NetworkPolicy" and
      (.metadata.name == "interaction-gateway-exact-runtime-paths" or
       .metadata.name == "interaction-gateway-migration-exact-paths"));
    .spec.egress = ((.spec.egress // []) | map(select(
      .to[0].podSelector.matchLabels."app.kubernetes.io/name" != "vault")))
  )
' "$temporary_directory/normalized.yaml"

# File readers получают только Secret своей exact target. Список закрыт и
# повторяет утверждённый target registry publisher.
add_prototype_delivery_mount() {
  local workload_id=$1 container_name=$2 secret_name=$3 directory=$4 volume_name=$5
  WORKLOAD_ID="$workload_id" CONTAINER_NAME="$container_name" SECRET_NAME="$secret_name" \
    DIRECTORY="$directory" VOLUME_NAME="$volume_name" yq -i '
      with(select(.kind == "Deployment" or .kind == "DaemonSet" or .kind == "Job");
        with(select(([
          (.spec.template.spec.containers[]?, .spec.template.spec.initContainers[]?) |
          select(.name == strenv(CONTAINER_NAME) and ([
            .env[]? |
            select(.name == "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID" and .value == strenv(WORKLOAD_ID))
          ] | length) > 0)
        ] | length) > 0);
          .spec.template.spec.containers[]? |=
            with(select(.name == strenv(CONTAINER_NAME) and ([
              .env[]? |
              select(.name == "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID" and .value == strenv(WORKLOAD_ID))
            ] | length) > 0);
              .volumeMounts = ((.volumeMounts // []) + [{
                "name":strenv(VOLUME_NAME),
                "mountPath":"/var/run/secrets/mattercodex/internal-rpc-authority/prototype-delivery/" + strenv(DIRECTORY),
                "readOnly":true
              }])
            ) |
          .spec.template.spec.initContainers[]? |=
            with(select(.name == strenv(CONTAINER_NAME) and ([
              .env[]? |
              select(.name == "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID" and .value == strenv(WORKLOAD_ID))
            ] | length) > 0);
              .volumeMounts = ((.volumeMounts // []) + [{
                "name":strenv(VOLUME_NAME),
                "mountPath":"/var/run/secrets/mattercodex/internal-rpc-authority/prototype-delivery/" + strenv(DIRECTORY),
                "readOnly":true
              }])
            ) |
          .spec.template.spec.volumes = ((.spec.template.spec.volumes // []) + [{
            "name":strenv(VOLUME_NAME),
            "secret":{"secretName":strenv(SECRET_NAME),"defaultMode":288}
          }])
        )
      )
    ' "$temporary_directory/normalized.yaml"
}

add_prototype_delivery_mount role-image-builder internal-rpc-authority-issuer internal-rpc-authority-role-image-builder-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount image-admission internal-rpc-authority-issuer internal-rpc-authority-image-admission-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount image-promotion internal-rpc-authority-issuer internal-rpc-authority-image-promotion-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount automation-scheduler internal-rpc-authority-issuer internal-rpc-authority-automation-scheduler-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount control-api-gateway internal-rpc-authority-issuer internal-rpc-authority-control-api-gateway-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount integration-gateway internal-rpc-authority-issuer internal-rpc-authority-integration-gateway-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount integration-gateway internal-rpc-authority-verifier internal-rpc-authority-integration-gateway-verifier-delivery primary prototype-delivery-verifier
add_prototype_delivery_mount interaction-gateway internal-rpc-authority-issuer internal-rpc-authority-interaction-gateway-issuer-delivery primary prototype-delivery-issuer
add_prototype_delivery_mount interaction-gateway internal-rpc-authority-verifier internal-rpc-authority-interaction-gateway-verifier-delivery primary prototype-delivery-verifier
add_prototype_delivery_mount control-plane internal-rpc-authority-verifier internal-rpc-authority-control-plane-verifier-delivery primary prototype-delivery-verifier
add_prototype_delivery_mount control-plane internal-rpc-authority-verifier internal-rpc-authority-control-plane-resolver-delivery resolver prototype-delivery-resolver
add_prototype_delivery_mount runtime-controller internal-rpc-authority-issuer internal-rpc-authority-runtime-controller-issuer-delivery primary prototype-delivery-issuer

conflicting_resources=$(yq -o=json '.' "$temporary_directory/normalized.yaml" | jq -rs '
  map(select(.kind != null)) |
  group_by([.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name]) |
  map(select((map(tojson) | unique | length) > 1) | .[0] |
    [.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name] | @tsv) |
  .[]
')
[[ -z "$conflicting_resources" ]] || fail "canonical overlays contain conflicting resource identities"
yq -o=json '.' "$temporary_directory/normalized.yaml" | jq -sc '
  map(select(.kind != null)) |
  unique_by([.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name]) |
  .[]
' | yq -p=json -P >"$output"

if [[ "$scope" == release ]]; then
  while IFS=$'\t' read -r component pull_ref; do
    COMPONENT="$component" PULL_REF="$pull_ref" yq -i '
      (.. | select(tag == "!!str")) |= sub(
        "[A-Za-z0-9._:/-]+/" + strenv(COMPONENT) + "@sha256:[a-f0-9]{64}";
        strenv(PULL_REF)
      )
    ' "$output"
  done < <(jq -r '.images[] | [.component,.pull_ref] | @tsv' "$lock_file")

  agent_runner_ref=$(jq -er '.images[] | select(.component == "agent-runner") | .pull_ref' "$lock_file")
  agent_runner_digest=${agent_runner_ref##*@}
  lock_digest=$(sha256sum "$lock_file" | awk '{print $1}')
  AGENT_RUNNER_REF="$agent_runner_ref" AGENT_RUNNER_DIGEST="$agent_runner_digest" LOCK_DIGEST="$lock_digest" yq -i '
    with(select(.kind == "Deployment" and .metadata.name == "control-plane");
      .spec.template.metadata.annotations."mattercodex.dev/agent-runtime-image-digest" = strenv(AGENT_RUNNER_DIGEST)
    ) |
    (.. | select(tag == "!!str" and . == "sha256:0000000000000000000000000000000000000000000000000000000000000000")) = strenv(AGENT_RUNNER_DIGEST) |
    (.. | select(tag == "!!str" and . == "0000000000000000000000000000000000000000000000000000000000000000")) = strenv(LOCK_DIGEST)
  ' "$output"

  cat <<EOF >>"$output"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mattercodex-image-admission-policy
  namespace: mattercodex-system
  labels:
    mattercodex.dev/profile: direct-production-single-node-prototype
    mattercodex.dev/release-managed: "true"
data:
  policyRevision: "1"
  policySHA256: "$lock_digest"
  promotedPullRepository: "localhost:5001/mattercodex/roles"
  roleImageInputRepository: "matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000/mattercodex/role-image-inputs"
  trustedRoleBaseRepository: "localhost:5001/mattercodex/agent-runner"
  trustedRoleBaseDigest: "$agent_runner_digest"
  roleRuntimeContractRevision: "1"
  roleRuntimeContractSHA256: "$lock_digest"
EOF

  grep -Eq '^kind: Ingress$' "$output" && fail "application render contains Ingress"
  grep -Eq 'sha256:0{64}' "$output" && fail "application render contains a zero digest"
  if yq -r '.. | select(has("image")) | .image' "$output" | grep -E 'mattercodex/(control-plane|internal-rpc-authority|runtime-controller|interaction-gateway|integration-gateway|control-api-gateway|staff-control-center|egress-gateway|automation-scheduler|role-image-builder|agent-runner)(:|@)' |
    grep -Fvxf <(jq -r '.images[].pull_ref' "$lock_file") >/dev/null; then
    fail "application render contains an internal image outside the release lock"
  fi
fi

duplicate_resources=$(yq -o=json '.' "$output" | jq -rs '
  map(select(.kind != null)) |
  group_by([.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name]) |
  map(select(length != 1) | .[0] | [.apiVersion, .kind, (.metadata.namespace // ""), .metadata.name] | @tsv) |
  .[]
')
[[ -z "$duplicate_resources" ]] || fail "application render contains duplicate resource identities"

printf 'Application %s render created: %s\n' "$scope" "$output"
