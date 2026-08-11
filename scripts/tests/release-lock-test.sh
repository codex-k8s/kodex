#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
validator="$repository_root/tools/release/validate-release-lock.sh"
renderer="$repository_root/tools/release/render-direct-production.sh"
secret_bootstrap="$repository_root/tools/deploy/bootstrap-direct-production-secrets.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
source_sha=1111111111111111111111111111111111111111
digest=sha256:2222222222222222222222222222222222222222222222222222222222222222
grep -Fq "openssl rand -hex 32 | tr -d '\\n'" "$secret_bootstrap" &&
  grep -Fq 'legacy newline-terminated material' "$secret_bootstrap" &&
  grep -Fq 'tail -c 1 "$file" | base64' "$secret_bootstrap" || {
  printf 'Direct-production secret bootstrap does not canonicalize legacy newline-terminated material\n' >&2
  exit 1
}
jq --arg source_sha "$source_sha" --arg digest "$digest" '
  {schema_version:1,profile:"direct-production single-node prototype",source_sha:$source_sha,build_run_id:"123",registry_push:"matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000",node_pull:"localhost:5001",images:[.images[]|{component,repository:("mattercodex/"+.component),digest:$digest,pull_ref:("localhost:5001/mattercodex/"+.component+"@"+$digest)}]}
' "$repository_root/tools/release/images.json" | jq -S . >"$temporary_directory/valid.json"
jq -e '
  ([.images[] | select(.component == "role-image-builder" and .target == "runtime")] | length) == 1 and
  all(.images[] | select(.component != "role-image-builder"); has("target") | not)
' "$repository_root/tools/release/images.json" >/dev/null || {
  printf 'Release image catalog does not select the exact role-image-builder runtime target\n' >&2
  exit 1
}
grep -Fq 'target_options=(--opt "target=$target")' \
  "$repository_root/tools/release/build-release.sh" || {
    printf 'Release builder does not pass the exact Dockerfile target to BuildKit\n' >&2
    exit 1
  }
admission_arg_line=$(rg -n '^ARG ADMISSION_TOOLS_IMAGE=' \
  "$repository_root/services/jobs/role-image-builder/Dockerfile" | cut -d: -f1)
first_role_builder_from_line=$(rg -n '^FROM ' \
  "$repository_root/services/jobs/role-image-builder/Dockerfile" | head -n 1 | cut -d: -f1)
[[ "$admission_arg_line" =~ ^[0-9]+$ && "$first_role_builder_from_line" =~ ^[0-9]+$ &&
   "$admission_arg_line" -lt "$first_role_builder_from_line" ]] || {
  printf 'Deferred admission image ARG is not globally parseable before named target selection\n' >&2
  exit 1
}
lock_sha=$(sha256sum "$temporary_directory/valid.json" | awk '{print $1}')
"$validator" --lock "$temporary_directory/valid.json" --source-sha "$source_sha" --sha256 "$lock_sha" >/dev/null
"$renderer" --lock "$temporary_directory/valid.json" --source-sha "$source_sha" \
  --sha256 "$lock_sha" --output "$temporary_directory/direct-production.yaml" >/dev/null
for expected_resource in \
  Deployment/control-plane Deployment/runtime-controller Deployment/interaction-gateway \
  Deployment/integration-gateway Deployment/control-api-gateway Deployment/automation-scheduler \
  Job/control-plane-migrate Job/internal-rpc-authority-migrate StatefulSet/mattercodex-postgresql; do
  expected_kind=${expected_resource%%/*}
  expected_name=${expected_resource#*/}
  EXPECTED_KIND="$expected_kind" EXPECTED_NAME="$expected_name" yq eval-all -e '
    select(.kind == strenv(EXPECTED_KIND) and .metadata.name == strenv(EXPECTED_NAME)) |
    .metadata.namespace == "mattercodex-system" and
    .metadata.labels."mattercodex.dev/release-managed" == "true"
  ' "$temporary_directory/direct-production.yaml" >/dev/null || {
    printf 'Expected direct-production resource is absent: %s\n' "$expected_resource" >&2
    exit 1
  }
done
if grep -Eq '^kind: Ingress$|namespace: matter-kodex-prod$|sha256:0{64}' "$temporary_directory/direct-production.yaml"; then
  printf 'Direct-production render contains a forbidden marker\n' >&2
  exit 1
fi
yq -o=json eval-all '.' "$temporary_directory/direct-production.yaml" | jq -sc -e '
  [ .[] | select(.kind == "Deployment" and .metadata.name != "mattercodex-object-store-bootstrap") ] as $deployments |
  ($deployments | length) > 0 and
  all($deployments[];
    ((.spec.replicas | type) == "number" and .spec.replicas >= 2) and
    ((.spec.strategy.type // "RollingUpdate") == "RollingUpdate"))
' >/dev/null || {
  printf 'Direct-production render lost replicated rolling application workloads\n' >&2
  exit 1
}
yq -o=json eval-all '.' "$temporary_directory/direct-production.yaml" | jq -sc -e '
  all(.[];
    if (.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job" or .kind == "CronJob") then
      (if .kind == "CronJob" then .spec.jobTemplate.spec.template.spec else .spec.template.spec end) as $pod |
      all((($pod.containers // []) + ($pod.initContainers // []))[];
        all((.ports // [])[]; (.name | length) <= 15))
    else true end)
' >/dev/null || {
  printf 'Direct-production render contains a container port name longer than 15 characters\n' >&2
  exit 1
}
while IFS= read -r image; do
  [[ "$image" != localhost:5001/mattercodex/* ]] ||
    grep -Fqx "$image" <(jq -r '.images[].pull_ref' "$temporary_directory/valid.json") || {
      printf 'Direct-production render contains an image outside the release lock\n' >&2
      exit 1
    }
done < <(yq eval-all -r '.. | .image?' "$temporary_directory/direct-production.yaml" | sed '/^---$/d;/^null$/d')

while IFS=$'\t' read -r component dockerfile; do
  module_directory=$(dirname -- "$repository_root/$dockerfile")
  module_file="$module_directory/go.mod"
  [[ -f "$module_file" ]] || continue
  if grep -Eq '^COPY[[:space:]]+libs/go/?[[:space:]]+' "$repository_root/$dockerfile"; then
    continue
  fi
  while IFS= read -r replacement; do
    [[ "$replacement" == ./* || "$replacement" == ../* ]] || continue
    replacement_path=$(realpath -m -- "$module_directory/$replacement")
    replacement_path=${replacement_path#"$repository_root/"}
    grep -Fq "COPY $replacement_path/ $replacement_path/" \
      "$repository_root/$dockerfile" || {
      printf 'Dockerfile %s omits local replacement %s before build\n' \
        "$component" "$replacement_path" >&2
      exit 1
    }
  done < <(go mod edit -json "$module_file" | jq -r '.Replace[]?.New.Path')
done < <(jq -r '.images[] | [.component,.dockerfile] | @tsv' \
  "$repository_root/tools/release/images.json")

for debian_runtime in \
  services/external/integration-gateway/Dockerfile \
  services/jobs/agent-runner/Dockerfile; do
  grep -Fq "sed -i 's#^URIs: http://deb.debian.org/#URIs: https://deb.debian.org/#'" \
    "$repository_root/$debian_runtime" || {
      printf 'Dockerfile %s does not upgrade Debian package sources to TLS\n' \
        "$debian_runtime" >&2
      exit 1
    }
  grep -Fq "if grep -Eq '^URIs: http://'" "$repository_root/$debian_runtime" || {
    printf 'Dockerfile %s permits plaintext Debian package fallback\n' \
      "$debian_runtime" >&2
    exit 1
  }
done

grep -Fq \
  'COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt' \
  "$repository_root/services/external/integration-gateway/Dockerfile" || {
    printf 'Slim integration-gateway runtime has no pinned pre-APT CA bootstrap\n' >&2
    exit 1
  }

if yq eval-all -e 'select(.kind == "Deployment" and .metadata.name == "role-image-builder")' \
  "$temporary_directory/direct-production.yaml" >/dev/null 2>&1; then
  printf 'Deferred hardened supply-chain workload leaked into dark render\n' >&2
  exit 1
fi

"$repository_root/tools/release/render-production-workload-contracts.sh" \
  --manifest "$temporary_directory/direct-production.yaml" \
  --output "$temporary_directory/workload-contracts.yaml" >/dev/null
yq -o=json '.data' "$temporary_directory/workload-contracts.yaml" |
  jq -e 'length > 0 and all(to_entries[];
    (.key | endswith(".automountServiceAccountToken") | not) or .value == "false")' >/dev/null || {
  printf 'Production workload contract is empty or allows ServiceAccount token automount\n' >&2
  exit 1
}
yq -o=json '.data."deployment.internal-rpc-authority-restore-controller.volumes"' \
  "$temporary_directory/workload-contracts.yaml" |
  jq -e 'contains("kubernetes-ca\u001econfigMap\u001ekube-root-ca.crt\u001dfalse\u001d420\u001d")' \
  >/dev/null || {
  printf 'Production workload contract does not model the Kubernetes ConfigMap defaultMode\n' >&2
  exit 1
}
cp "$temporary_directory/direct-production.yaml" "$temporary_directory/forged-secret.yaml"
yq -i '
  with(select(.kind == "Deployment" and .metadata.name == "control-plane");
    (.spec.template.spec.volumes[] | select(.secret != null) | .secret.secretName) =
      "forbidden-production-secret")
' "$temporary_directory/forged-secret.yaml"
"$repository_root/tools/release/render-production-workload-contracts.sh" \
  --manifest "$temporary_directory/forged-secret.yaml" \
  --output "$temporary_directory/forged-contracts.yaml" >/dev/null
if diff -q <(yq -o=json '.data' "$temporary_directory/workload-contracts.yaml" | jq -S .) \
  <(yq -o=json '.data' "$temporary_directory/forged-contracts.yaml" | jq -S .) >/dev/null; then
  printf 'Production workload contract did not detect a forged Secret mount\n' >&2
  exit 1
fi

yq -o=json eval-all '.' "$repository_root/infra/direct-production/bootstrap.yaml" | jq -sc -e '
  (map(select(.kind == "Role" and .metadata.name == "mattercodex-production-deployer"))[0]) as $role |
  ([$role.rules[] | select(.resources | index("pods/log"))] | length) == 0 and
  ([$role.rules[] | select(.resources == ["secrets"])] | length) == 1 and
  ([$role.rules[] | select(.resources == ["secrets"])][0] |
    .verbs == ["get"] and .resourceNames == ["internal-rpc-authority-snapshot"]) and
  ([$role.rules[] | select((.verbs | index("delete")) and (.resources == ["jobs"])) |
    .resourceNames] | add | sort) ==
  ["control-plane-migrate","integration-gateway-migrate","interaction-gateway-migrate",
   "internal-rpc-authority-migrate","mattercodex-postgresql-principal-bootstrap",
   "runtime-controller-migration"]
' >/dev/null || {
  printf 'Routine production deployer RBAC is broader than the exact workload contract\n' >&2
  exit 1
}

yq -o=json eval-all '.' "$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/rbac.yaml" |
  jq -sc -e '
    (map(select(.kind == "Role" and .metadata.name == "internal-rpc-authority-publisher"))[0]) as $role |
    ($role.rules | length) == 1 and
    $role.rules[0].apiGroups == [""] and
    $role.rules[0].resources == ["secrets"] and
    $role.rules[0].verbs == ["get","update"] and
    ($role.rules[0].resourceNames | length) > 0 and
    ($role.rules[0].resourceNames | length) == ($role.rules[0].resourceNames | unique | length)
  ' >/dev/null || {
  printf 'Publisher RBAC is broader than exact Secret GET/PUT authority\n' >&2
  exit 1
}

kubectl kustomize "$repository_root/deploy/k8s/base/direct-production-foundation" |
  yq eval-all 'select(.kind == "NetworkPolicy")' >"$temporary_directory/network-policies.yaml"
"$repository_root/tools/release/render-direct-production-applications.sh" \
  --scope bootstrap --output "$temporary_directory/application-bootstrap.yaml" >/dev/null
yq eval-all 'select(.kind == "NetworkPolicy")' "$temporary_directory/application-bootstrap.yaml" \
  >>"$temporary_directory/network-policies.yaml"
yq -o=json eval-all '.' "$temporary_directory/network-policies.yaml" | jq -sc -e '
  def ingress_peers: [(.spec.ingress // [])[]? | (.from // [])[]?];
  def egress_peers: [(.spec.egress // [])[]? | (.to // [])[]?];
  def egress_ports: [(.spec.egress // [])[]? | (.ports // [])[]?];
  length > 0 and
  all(.[];
    all(ingress_peers[];
      (.namespaceSelector == null) or (.podSelector != null) or (.ipBlock != null)) and
    all(egress_peers[];
      (.namespaceSelector == null) or (.podSelector != null) or (.ipBlock != null)) and
    all(egress_ports[]; .port != 8222))
' >/dev/null || {
  printf 'Direct-production NetworkPolicy contains a namespace-wide data destination\n' >&2
  exit 1
}
yq -o=json eval-all '.' "$repository_root/infra/arc/network-policy.yaml" | jq -sc -e '
  map(select(.kind == "NetworkPolicy" and .metadata.name == "build-registry")) |
  length == 1 and
  .[0].spec.egress[0].to[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name" ==
    "matter-kodex-prod" and
  .[0].spec.egress[0].to[0].podSelector.matchLabels."app.kubernetes.io/name" ==
    "matter-codex-registry" and
  ([.[0].spec.egress[0].to[] | select(.ipBlock != null) | .ipBlock.cidr] | sort) ==
    ["__REGISTRY_ENDPOINT_CIDR__","__REGISTRY_SERVICE_CIDR__"] and
  ([.[0].spec.egress[0].ports[] | select(.protocol == "TCP") | .port] | sort) ==
    [5000,5001]
' >/dev/null || {
  printf 'Build runner registry NetworkPolicy destination is not exact\n' >&2
  exit 1
}
yq eval-all -e '
  select(.kind == "ConfigMap" and .metadata.name == "mattercodex-postgresql-init") |
  (.data."pg_hba.conf" | contains("local all all peer")) and
  (.data."pg_hba.conf" | contains("hostnossl all all 0.0.0.0/0 reject")) and
  (.data."pg_hba.conf" | contains("hostssl all all 0.0.0.0/0 scram-sha-256")) and
  (.data."10-mattercodex-databases.sh" |
    contains("\\connect control_plane\nCREATE EXTENSION IF NOT EXISTS vector;"))
' "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" >/dev/null || {
  printf 'PostgreSQL TLS-only pg_hba contract is absent\n' >&2
  exit 1
}
yq eval-all -e '
  select(.kind == "StatefulSet" and .metadata.name == "mattercodex-postgresql") |
  (.spec.template.spec.securityContext.runAsUser == 999) and
  (.spec.template.spec.securityContext.runAsGroup == 999) and
  (.spec.template.spec.securityContext.fsGroup == 999) and
  (.spec.template.spec.containers | any_c(
    .name == "postgresql" and
    .image == "pgvector/pgvector:0.8.5-pg16@sha256:1d533553fefe4f12e5d80c7b80622ba0c382abb5758856f52983d8789179f0fb"
  ))
' "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" >/dev/null || {
  printf 'Direct-production PostgreSQL does not provide the pinned pgvector runtime\n' >&2
  exit 1
}

yq eval-all -e '
  select(.kind == "StatefulSet" and .metadata.name == "mattercodex-nats") |
  .spec.template.spec as $pod |
  (($pod.initContainers[0].name == "render-config") and
   ($pod.initContainers[0].args[0] | contains("system-account.jwt")) and
   ($pod.containers[0].args | length == 2) and
   ($pod.containers[0].args[0] == "-c") and
   ($pod.containers[0].args[1] == "/var/run/runtime/nats.conf") and
   (($pod.containers[0] | has("env")) | not) and
   ($pod.volumes | any_c(.name == "credentials" and .secret.secretName == "mattercodex-nats-credentials")))
' "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" >/dev/null || {
  printf 'NATS secret-backed runtime render contract is absent\n' >&2
  exit 1
}

yq -o=json eval-all '.' "$repository_root/deploy/k8s/base/runtime-controller/serviceaccounts-rbac.yaml" |
  jq -sc -e '
    map(select(.kind == "ValidatingAdmissionPolicy" and .metadata.name == "runtime-controller-exact-pod-authority"))[0]
      .spec.validations | map(.expression) |
    map(select(contains("object.metadata.labels['"'"'app.kubernetes.io/component'"'"'] != '"'"'role-runtime'"'"'"))) as $role_guards |
    ($role_guards | length) == 2 and
    all($role_guards[]; contains("!('"'"'app.kubernetes.io/component'"'"' in object.metadata.labels)"))
  ' >/dev/null || {
  printf 'Runtime role Pod admission does not safely ignore unrelated labeled Pods\n' >&2
  exit 1
}
yq -o=json eval-all '.' "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" |
  jq -sc -e '
    (map(select(.kind == "StatefulSet" and .metadata.name == "mattercodex-postgresql"))[0]
      .spec.template.spec.containers[0]) as $postgres |
    (map(select(.kind == "StatefulSet" and .metadata.name == "mattercodex-redis"))[0]
      .spec.template.spec.containers[0]) as $redis |
    all([$postgres.startupProbe,$postgres.readinessProbe,$postgres.livenessProbe][];
      (.exec.command | join(" ") | contains("host=mattercodex-postgresql hostaddr=127.0.0.1")) and
      (.exec.command | join(" ") | contains("sslmode=verify-full"))) and
    all([$redis.readinessProbe,$redis.livenessProbe][];
      (.exec.command | index("--tls")) != null and
      (.exec.command | index("--sni")) != null and
      (.exec.command | index("--cacert")) != null and
      (.exec.command | index("mattercodex-redis")) != null and
      (.exec.command | index("127.0.0.1")) != null)
  ' >/dev/null || {
  printf 'Foundation TLS probes do not verify the intended hostname and CA\n' >&2
  exit 1
}
grep -Fq 'sql_file=$(mktemp /tmp/principals.sql.XXXXXX)' \
  "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
  printf 'PostgreSQL principal bootstrap uses a non-portable mktemp template\n' >&2
  exit 1
}
grep -Fq "dollar_quote='\$bootstrap\$'" \
  "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" &&
  ! grep -Fq '\\$bootstrap\\$' \
    "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
  printf 'PostgreSQL principal bootstrap uses expandable SQL dollar quoting\n' >&2
  exit 1
}
grep -Fq 'attempt=$((attempt + 1))' \
  "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" &&
  grep -Fq '[ "$attempt" -lt 30 ] || exit 27' \
    "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
  printf 'PostgreSQL principal bootstrap has no bounded network-readiness retry\n' >&2
  exit 1
}
grep -Fq "tr '\\t' '|' </var/run/bootstrap/principals.tsv" \
  "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" &&
  grep -Fq "while IFS='|' read -r principal database memberships create_role" \
    "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
  printf 'PostgreSQL principal bootstrap does not preserve empty TSV fields\n' >&2
  exit 1
}
grep -Fq 'if [ "$create_role" = true ]; then admin_flag=TRUE; else admin_flag=FALSE; fi' \
  "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" &&
  grep -Fq 'WITH INHERIT FALSE, SET TRUE, ADMIN %s' \
    "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
  printf 'PostgreSQL migrator role graph does not bind ADMIN to the bounded create-role flag\n' >&2
  exit 1
}
grep -Fq 'WITH INHERIT FALSE, SET FALSE, ADMIN TRUE' \
  "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" &&
  grep -Fq 'member.admin_option AND NOT member.inherit_option AND NOT member.set_option' \
    "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
  printf 'PostgreSQL managed login principals lack bounded ADMIN-only ownership\n' >&2
  exit 1
}
grep -Fq "format('REVOKE %%I FROM %%I GRANTED BY %%I'" \
  "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
  printf 'PostgreSQL principal retirement ignores the membership grantor\n' >&2
  exit 1
}
grep -Fq 'SELECT DISTINCT member.roleid, member.member' \
  "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
  printf 'PostgreSQL principal readback counts grantor rows instead of canonical memberships\n' >&2
  exit 1
}
for migration_registry_entry in \
  'control_plane_migrator:control_plane_owner,control_plane_runtime,control_plane_relay,control_plane_role_controller,pg_signal_backend' \
  'integration_gateway_migrator_g1:integration_gateway_owner,integration_gateway_runtime,integration_gateway_migrator,integration_gateway_role_controller,pg_signal_backend' \
  'interaction_gateway_migrator:interaction_gateway_owner,interaction_gateway_runtime,interaction_gateway_role_controller,pg_signal_backend' \
  'internal_rpc_authority_migrator:internal_rpc_authority_owner,internal_rpc_authority_readback_owner,internal_rpc_authority_credential_lifecycle_definer,internal_rpc_authority_issuer,internal_rpc_authority_verifier,internal_rpc_authority_publisher,internal_rpc_authority_readback_attestor,internal_rpc_authority_database_credential_reconciler,internal_rpc_authority_recovery,internal_rpc_authority_restore_controller,pg_signal_backend'; do
  migration_principal=${migration_registry_entry%%:*}
  migration_memberships=${migration_registry_entry#*:}
  awk -F '\t' -v principal="$migration_principal" -v memberships="$migration_memberships" '
    {gsub(/^[[:space:]]+/, "", $1)}
    $1 == principal && $3 == memberships && $4 == "true" {found=1}
    END {exit(found ? 0 : 1)}
  ' "$repository_root/deploy/k8s/base/direct-production-foundation/foundation.yaml" || {
    printf 'PostgreSQL migrator registry is incomplete: %s\n' "$migration_principal" >&2
    exit 1
  }
done
grep -Fq 'GRANT internal_rpc_authority_readback_owner TO internal_rpc_authority_owner' \
  "$repository_root/services/internal/internal-rpc-authority/cmd/cli/migrations/20260730000100_internal_rpc_authority_runtime.sql" &&
  grep -Fq 'WITH INHERIT TRUE, SET TRUE, ADMIN FALSE' \
    "$repository_root/services/internal/internal-rpc-authority/cmd/cli/migrations/20260730000100_internal_rpc_authority_runtime.sql" || {
  printf 'Internal RPC authority object-owner transfer graph is incomplete\n' >&2
  exit 1
}
for migration_scope in \
  'services/internal/control-plane/cmd/cli/migrations:control_plane_owner' \
  'services/external/integration-gateway/cmd/cli/migrations:integration_gateway_owner' \
  'services/external/interaction-gateway/cmd/cli/migrations:interaction_gateway_owner' \
  'services/internal/internal-rpc-authority/cmd/cli/migrations:internal_rpc_authority_owner' \
  'services/internal/runtime-controller/cmd/cli/migrations:runtime_controller_owner'; do
  migration_directory=${migration_scope%%:*}
  migration_owner=${migration_scope#*:}
  duplicate_versions=$(find "$repository_root/$migration_directory" -maxdepth 1 -type f -name '*.sql' -printf '%f\n' |
    cut -d_ -f1 | LC_ALL=C sort | uniq -d)
  [[ -z "$duplicate_versions" ]] || {
    printf 'Goose migration versions are duplicated in %s: %s\n' "$migration_directory" "$duplicate_versions" >&2
    exit 1
  }
  for migration_file in "$repository_root/$migration_directory"/*.sql; do
    [[ "$(sed -n '1p' "$migration_file")" == '-- +goose Up' ]] &&
      [[ "$(sed -n '2p' "$migration_file")" == 'RESET ROLE;' ]] &&
      [[ "$(sed -n '3p' "$migration_file")" == "SET ROLE $migration_owner;" ]] || {
      printf 'Goose migration does not establish its exact owner role: %s\n' "$migration_file" >&2
      exit 1
    }
  done
  awk '
    FNR == 1 {previous=""}
    /^DO \$[A-Za-z0-9_]*\$$/ && previous != "-- +goose StatementBegin" {exit 1}
    NF {previous=$0}
  ' "$repository_root/$migration_directory"/*.sql || {
    printf 'Goose migration has an unbounded PostgreSQL DO block: %s\n' "$migration_directory" >&2
    exit 1
  }
  if rg --pcre2 -U -q \
    '(?s)ALTER ROLE\b[^;]*\b(?:NOSUPERUSER|NOCREATEDB|NOREPLICATION|NOBYPASSRLS)\b' \
    "$repository_root/$migration_directory"; then
    printf 'Bounded PostgreSQL migrator changes a superuser-only role attribute: %s\n' \
      "$migration_directory" >&2
    exit 1
  fi
done
if rg -n '(control-plane|internal-rpc-authority|runtime-controller)-postgresql\.mattercodex-system' \
  "$repository_root/deploy" "$repository_root/services" "$repository_root/contracts" >/dev/null; then
  printf 'PostgreSQL runtime contract still references a non-canonical service alias\n' >&2
  exit 1
fi
for migration_contract in \
  'deploy/k8s/base/control-plane/migration-job.yaml:control-plane-postgresql-rw.mattercodex-system.svc.cluster.local' \
  'deploy/k8s/base/integration-gateway/migration-job.yaml:integration-gateway-postgresql-rw.mattercodex-system.svc.cluster.local' \
  'deploy/k8s/base/interaction-gateway/migration-job.yaml:interaction-gateway-postgresql-rw.mattercodex-system.svc.cluster.local' \
  'deploy/k8s/base/internal-rpc-authority-data/migration-job.yaml:internal-rpc-authority-postgresql-rw.mattercodex-system.svc.cluster.local' \
  'deploy/k8s/base/runtime-controller/migration-job.yaml:runtime-controller-postgresql-rw.mattercodex-system.svc.cluster.local'; do
  migration_file=${migration_contract%%:*}
  migration_host=${migration_contract#*:}
  yq -o=json '.' "$repository_root/$migration_file" | jq -e --arg postgres_host "$migration_host" '
    .spec.activeDeadlineSeconds > 0 and .spec.activeDeadlineSeconds <= 300 and
    .spec.template.spec.restartPolicy == "Never" and
    .spec.template.spec.securityContext.runAsNonRoot == true and
    (.spec.template.spec.securityContext.fsGroup | type == "number") and
    .spec.template.spec.securityContext.fsGroupChangePolicy == "OnRootMismatch" and
    (.spec.template.spec.initContainers[] | select(.name == "wait-for-postgresql") |
      (.image | test("^docker.io/library/postgres:[^@]+@sha256:[a-f0-9]{64}$")) and
      .env == [{"name":"POSTGRES_HOST","value":$postgres_host}] and
      (.args | join(" ") | contains("until pg_isready")) and
      .securityContext.runAsNonRoot == true and
      .securityContext.readOnlyRootFilesystem == true)
  ' >/dev/null || {
    printf 'Migration Job has no bounded PostgreSQL network-readiness barrier: %s\n' "$migration_file" >&2
    exit 1
  }
done
for migration_binding in \
  'control-plane-postgres-migration:control_plane_owner' \
  'integration-gateway-postgres-migrator:integration_gateway_owner' \
  'interaction-gateway-postgres-migrator:interaction_gateway_owner' \
  'internal-rpc-authority-migrator-postgresql:internal_rpc_authority_owner' \
  'runtime-controller-postgres-migration:runtime_controller_owner'; do
  migration_secret=${migration_binding%%:*}
  migration_owner=${migration_binding#*:}
  rg -q "^put_pg $migration_secret dsn [a-z0-9_]+ [a-z0-9_]+ \\\"\\\$[a-z_]+\\\" $migration_owner$" \
    "$repository_root/tools/deploy/materialize-direct-production-application.sh" || {
    printf 'Migration DSN does not assume its exact owner role: %s\n' "$migration_secret" >&2
    exit 1
  }
done
yq -e '
  .jobs.build."runs-on" == "mattercodex-build" and
  .jobs.build.steps[1].with.ref == "${{ vars.MATTERCODEX_PRODUCTION_WORKFLOW_SHA }}" and
  (.jobs.build.steps[2].run | contains("verify-github-owner-gate.sh")) and
  (.jobs.build.steps[3].run | contains("build-release.sh")) and
  .jobs.build.steps[3].env.HTTPS_PROXY ==
    "http://mattercodex-ci-egress-proxy.mattercodex-ci.svc.cluster.local:8080" and
  .jobs.build.steps[3].env.HTTP_PROXY ==
    "http://mattercodex-ci-egress-proxy.mattercodex-ci.svc.cluster.local:8080" and
  .jobs.build.steps[3].env.NO_PROXY ==
    "matter-codex-registry.matter-kodex-prod.svc.cluster.local:5000,localhost,127.0.0.1"
' "$repository_root/.github/workflows/build-release.yml" >/dev/null || {
  printf 'Build workflow may run mutable source before the owner gate\n' >&2
  exit 1
}
yq -e '
  .jobs.build.steps[-1].uses ==
    "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a"
' "$repository_root/.github/workflows/build-release.yml" >/dev/null || {
  printf 'Build workflow lost the proxy-safe pinned artifact uploader\n' >&2
  exit 1
}
yq -e '
  .jobs.deploy."runs-on" == "mattercodex-deploy" and
  .jobs.deploy.steps[1].with.ref == "${{ vars.MATTERCODEX_PRODUCTION_WORKFLOW_SHA }}" and
  (.jobs.deploy.steps[2].run | contains("verify-github-owner-gate.sh")) and
  (.jobs.deploy.steps[-1].run | contains("direct-production.sh"))
' "$repository_root/.github/workflows/deploy-production.yml" >/dev/null || {
  printf 'Deploy workflow may run mutable source before the owner gate\n' >&2
  exit 1
}
grep -Fq -- '--field-manager=mattercodex-production-deployer --dry-run=server' \
  "$repository_root/tools/deploy/direct-production.sh" || {
  printf 'Direct-production preflight does not use server-side apply for large ConfigMaps\n' >&2
  exit 1
}
grep -Fq 'deployments?environment=$environment&per_page=100' \
  "$repository_root/tools/release/verify-github-owner-gate.sh"
grep -Fq 'deployments/$deployment_id/statuses?per_page=100' \
  "$repository_root/tools/release/verify-github-owner-gate.sh"
grep -Fq 'curl --config "$curl_config" --fail --silent --show-error' \
  "$repository_root/tools/release/verify-github-owner-gate.sh"
grep -Fq 'unset GH_TOKEN' "$repository_root/tools/release/verify-github-owner-gate.sh"
if rg -q 'gh api' "$repository_root/tools/release/verify-github-owner-gate.sh"; then
  printf 'Workflow owner gate still depends on gh CLI unavailable in the pinned runner image\n' >&2
  exit 1
fi
if rg -q 'repos/\$GITHUB_REPOSITORY/environments/' \
  "$repository_root/tools/release/verify-github-owner-gate.sh"; then
  printf 'Workflow owner gate still requires unavailable environment administration permission\n' >&2
  exit 1
fi
if rg -q 'runnerGroup|runner-groups|mattercodex-production-(build|deploy)([^a-z]|$)' \
  "$repository_root/infra/arc/build-runner-values.yaml" \
  "$repository_root/infra/arc/deploy-runner-values.yaml" \
  "$repository_root/infra/github/bootstrap-actions-policy.sh"; then
  printf 'Repo-scoped ARC configuration still depends on an organization runner group\n' >&2
  exit 1
fi
grep -Fq 'repos/$repository/actions/runners?per_page=1' \
  "$repository_root/infra/github/bootstrap-actions-policy.sh" || {
  printf 'GitHub policy preflight does not verify repository runner API access\n' >&2
  exit 1
}
for proxy_contract in \
  'build_proxy=http://mattercodex-ci-egress-proxy.mattercodex-ci.svc.cluster.local:8080' \
  'build-arg:HTTPS_PROXY=$build_proxy' \
  'build-arg:NO_PROXY=$build_no_proxy'; do
  grep -Fq "$proxy_contract" "$repository_root/tools/release/build-release.sh" || {
    printf 'Release builder lost the exact BuildKit proxy contract\n' >&2
    exit 1
  }
  grep -Fq "$proxy_contract" "$repository_root/tools/release/shims/docker" || {
    printf 'Protected agent-runner shim lost the exact BuildKit proxy contract\n' >&2
    exit 1
  }
done

jq '.images[0].pull_ref = "localhost:5001/mattercodex/control-plane:latest"' "$temporary_directory/valid.json" >"$temporary_directory/mutable.json"
mutable_sha=$(sha256sum "$temporary_directory/mutable.json" | awk '{print $1}')
if "$validator" --lock "$temporary_directory/mutable.json" --source-sha "$source_sha" --sha256 "$mutable_sha" >/dev/null 2>&1; then
  printf 'Mutable image reference was accepted\n' >&2
  exit 1
fi
if "$validator" --lock "$temporary_directory/valid.json" --source-sha 3333333333333333333333333333333333333333 --sha256 "$lock_sha" >/dev/null 2>&1; then
  printf 'Mismatched source SHA was accepted\n' >&2
  exit 1
fi
printf 'Release lock negative checks completed\n'
