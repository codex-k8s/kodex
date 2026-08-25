#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Fresh release deployment failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode preflight|apply-state|apply-migrations|apply-workloads|readback" \
    '  --render <exact-release.yaml> --material-directory <owner-material-directory>' >&2
}

expected_context=""
mode=""
render_file=""
material_directory=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --render) render_file="${2:-}"; shift 2 ;;
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" ]] || fail 'exact context is required'
case "$mode" in
  preflight|apply-state|apply-migrations|apply-workloads|readback) ;;
  *) fail 'mode is invalid' ;;
esac
[[ -f "$render_file" && -s "$render_file" && ! -L "$render_file" ]] || fail 'release render is invalid'
[[ -d "$material_directory" && ! -L "$material_directory" ]] || fail 'material directory is invalid'
for command_name in awk cmp jq kubectl rg sha256sum sort stat yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
vault_bootstrap="$repository_root/tools/deploy/bootstrap-vault.sh"
namespace=mattercodex-system
restore_evidence_name=internal-rpc-authority-restore-evidence
authority_roots_name=internal-rpc-authority-bootstrap-roots
authority_snapshot_name=internal-rpc-authority-snapshot
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

validate_authority_bootstrap_material() {
  local manifest_public manifest_metadata readback_public readback_metadata material_file
  local manifest_public_digest readback_public_digest
  local readback_public_canonical readback_metadata_public_canonical

  manifest_public="$material_directory/crypto/authority-bootstrap/public/manifest-root/bootstrap-public.jwk"
  manifest_metadata="$material_directory/crypto/authority-bootstrap/public/manifest-root/bootstrap-metadata.json"
  readback_public="$material_directory/crypto/authority-bootstrap/public/readback-root/bootstrap-public.jwk"
  readback_metadata="$material_directory/crypto/authority-bootstrap/public/readback-root/bootstrap-metadata.json"
  for material_file in "$manifest_public" "$manifest_metadata" "$readback_public" "$readback_metadata"; do
    [[ -f "$material_file" && -s "$material_file" && ! -L "$material_file" ]] ||
      fail 'authority bootstrap root material is absent'
    [[ $(stat -c '%a' "$material_file") == 600 ]] ||
      fail 'authority bootstrap root material mode is unsafe'
    [[ $(stat -c '%s' "$material_file") -le 65536 ]] ||
      fail 'authority bootstrap root material is oversized'
  done

  jq -e '.kty == "EC" and .crv == "P-256" and .alg == "ES256" and
    .use == "sig" and .key_ops == ["verify"] and
    (.kid | type == "string" and length > 0) and
    (.x | type == "string" and length > 0) and (.y | type == "string" and length > 0)' \
    "$manifest_public" "$readback_public" >/dev/null ||
    fail 'authority bootstrap public JWK is invalid'
  jq -e '.v == 1 and (.public_jwk == null) and
    (.kid | type == "string" and length > 0) and
    (.source_digest_sha256 | type == "string" and test("^[a-f0-9]{64}$"))' \
    "$manifest_metadata" >/dev/null || fail 'manifest root metadata is invalid'
  jq -e '.v == 1 and (.public_jwk | type == "object") and
    (.kid | type == "string" and length > 0) and
    (.source_digest_sha256 | type == "string" and test("^[a-f0-9]{64}$"))' \
    "$readback_metadata" >/dev/null || fail 'readback root metadata is invalid'

  manifest_public_digest=$(sha256sum "$manifest_public" | awk '{print $1}')
  readback_public_digest=$(sha256sum "$readback_public" | awk '{print $1}')
  [[ $(jq -r '.kid' "$manifest_public") == $(jq -r '.kid' "$manifest_metadata") &&
    "$manifest_public_digest" == $(jq -r '.source_digest_sha256' "$manifest_metadata") ]] ||
    fail 'manifest root metadata does not bind the public JWK'
  [[ $(jq -r '.kid' "$readback_public") == $(jq -r '.kid' "$readback_metadata") &&
    "$readback_public_digest" == $(jq -r '.source_digest_sha256' "$readback_metadata") ]] ||
    fail 'readback root metadata does not bind the public JWK'

  readback_public_canonical="$temporary_directory/readback-root-public.json"
  readback_metadata_public_canonical="$temporary_directory/readback-root-metadata-public.json"
  jq -Sc '.' "$readback_public" >"$readback_public_canonical"
  jq -Sc '.public_jwk' "$readback_metadata" >"$readback_metadata_public_canonical"
  cmp -s "$readback_public_canonical" "$readback_metadata_public_canonical" ||
    fail 'readback root metadata embeds another public JWK'

  authority_roots_digest=$(
    {
      for material_file in \
        "$manifest_public" "$manifest_metadata" "$readback_public" "$readback_metadata"; do
        printf '%s\n' "${material_file#"$material_directory"/}"
        sha256sum "$material_file" | awk '{print $1}'
      done
    } | sha256sum | awk '{print $1}'
  )
}

authority_roots_payload() {
  jq -S '{
    immutable:(.immutable // false),
    data:(.data // {}),
    digest:(.metadata.annotations["mattercodex.dev/authority-bootstrap-roots-sha256"] // "")
  }' "$1" >"$2"
}

write_authority_bootstrap_roots_manifest() {
  local raw_manifest desired_manifest
  local manifest_root readback_root

  manifest_root="$material_directory/crypto/authority-bootstrap/public/manifest-root"
  readback_root="$material_directory/crypto/authority-bootstrap/public/readback-root"
  raw_manifest="$temporary_directory/authority-roots-raw.json"
  desired_manifest="$temporary_directory/authority-roots.json"

  kubectl -n "$namespace" create secret generic "$authority_roots_name" \
    --from-file=manifest-root-public.jwk="$manifest_root/bootstrap-public.jwk" \
    --from-file=manifest-root-metadata.json="$manifest_root/bootstrap-metadata.json" \
    --from-file=readback-root-public.jwk="$readback_root/bootstrap-public.jwk" \
    --from-file=readback-root-metadata.json="$readback_root/bootstrap-metadata.json" \
    --dry-run=client -o json >"$raw_manifest"
  jq --arg digest "$authority_roots_digest" '
    .immutable = true |
    .metadata.labels = {
      "app.kubernetes.io/name":"internal-rpc-authority",
      "app.kubernetes.io/component":"bootstrap-roots"
    } |
    .metadata.annotations = {
      "mattercodex.dev/authority-bootstrap-roots-sha256":$digest
    }
  ' "$raw_manifest" >"$desired_manifest"
}

ensure_authority_bootstrap_roots() {
  local desired_manifest desired_payload live_manifest live_payload

  desired_manifest="$temporary_directory/authority-roots.json"
  desired_payload="$temporary_directory/authority-roots-desired-payload.json"
  live_manifest="$temporary_directory/authority-roots-live.json"
  live_payload="$temporary_directory/authority-roots-live-payload.json"
  write_authority_bootstrap_roots_manifest
  authority_roots_payload "$desired_manifest" "$desired_payload"

  if kubectl -n "$namespace" get "secret/$authority_roots_name" -o json >"$live_manifest" 2>/dev/null; then
    authority_roots_payload "$live_manifest" "$live_payload"
    if cmp -s "$desired_payload" "$live_payload"; then
      return
    fi
    kubectl -n "$namespace" delete "secret/$authority_roots_name" --wait=true --timeout=2m >/dev/null
  fi
  kubectl create --field-manager=mattercodex-fresh-install -f "$desired_manifest" >/dev/null
  require_authority_bootstrap_roots
}

require_authority_bootstrap_roots() {
  local desired_manifest desired_payload live_manifest live_payload

  desired_manifest="$temporary_directory/authority-roots.json"
  desired_payload="$temporary_directory/authority-roots-desired-payload.json"
  live_manifest="$temporary_directory/authority-roots-live.json"
  live_payload="$temporary_directory/authority-roots-live-payload.json"
  write_authority_bootstrap_roots_manifest
  authority_roots_payload "$desired_manifest" "$desired_payload"
  kubectl -n "$namespace" get "secret/$authority_roots_name" -o json >"$live_manifest" ||
    fail 'authority bootstrap roots are absent; run apply-state first'
  authority_roots_payload "$live_manifest" "$live_payload"
  cmp -s "$desired_payload" "$live_payload" ||
    fail 'authority bootstrap roots drifted; run apply-state first'
}

pin_authority_root_revision() {
  local deployment_name patch

  patch=$(jq -nc --arg digest "$authority_roots_digest" '{
    spec:{template:{metadata:{annotations:{
      "mattercodex.dev/authority-bootstrap-roots-sha256":$digest
    }}}}
  }')
  for deployment_name in internal-rpc-authority-publisher \
    internal-rpc-authority-readback-attestor internal-rpc-authority-restore-controller; do
    kubectl -n "$namespace" patch "deployment/$deployment_name" --type=merge -p "$patch" >/dev/null
  done
}

validate_authority_bootstrap_material

if rg -n '__MATTERCODEX_[A-Z0-9_]+__|\.invalid|sha256:0{64}' "$render_file" >/dev/null; then
  fail 'release render contains an unresolved placeholder'
fi
yq -e 'select(.kind == "Namespace" and .metadata.name == "mattercodex-system")' \
  "$render_file" >/dev/null || fail 'release namespace contract is absent'
yq -o=json -I=0 '.' "$render_file" | jq -s -e '
  map(select(.kind != null)) as $resources |
  ($resources | length) > 0 and
  ($resources | group_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name]) |
    all(.[]; length == 1))
' >/dev/null || fail 'release render has duplicate resource identities'

require_external_observability_crds() {
  local kind crd

  while IFS=$'\t' read -r kind crd; do
    if ! yq -e "select(.kind == \"$kind\")" "$render_file" >/dev/null 2>&1; then
      continue
    fi
    kubectl get "customresourcedefinition/$crd" -o json | jq -e '
      any(.status.conditions[]?; .type == "Established" and .status == "True")
    ' >/dev/null 2>&1 ||
      fail "external observability CRD is absent or not established: $crd; run management-surfaces apply-monitoring first"
  done <<'EOF'
PodMonitor	podmonitors.monitoring.coreos.com
PrometheusRule	prometheusrules.monitoring.coreos.com
ServiceMonitor	servicemonitors.monitoring.coreos.com
EOF
}

require_external_observability_crds

render_sha256=$(sha256sum "$render_file" | awk '{print $1}')
printf 'Fresh release preflight: render_sha256=%s mode=%s\n' "$render_sha256" "$mode"
if [[ "$mode" == preflight ]]; then
  exit 0
fi

apply_filter() {
  local name=$1 expression=$2
  local output_file="$temporary_directory/$name.yaml"
  yq "$expression" "$render_file" >"$output_file"
  [[ -s "$output_file" ]] || fail "release phase is empty: $name"
  kubectl apply --server-side --field-manager=mattercodex-fresh-install -f "$output_file" >/dev/null
}

immutable_resource_payload() {
  local kind=$1 input_file=$2 output_file=$3
  case "$kind" in
    ConfigMap)
      jq -S '{immutable:(.immutable // false),data:(.data // {}),binaryData:(.binaryData // {})}' \
        "$input_file" >"$output_file"
      ;;
    ImageAdmissionPolicyParameters)
      jq -S '.spec' "$input_file" >"$output_file"
      ;;
    *) fail "unsupported immutable release resource kind: $kind" ;;
  esac
}

replace_immutable_resource_on_drift() {
  local resource=$1 kind=$2 name=$3
  local desired_yaml="$temporary_directory/immutable-$name.yaml"
  local desired_json="$temporary_directory/immutable-$name-desired.json"
  local desired_payload="$temporary_directory/immutable-$name-desired-payload.json"
  local live_json="$temporary_directory/immutable-$name-live.json"
  local live_payload="$temporary_directory/immutable-$name-live-payload.json"

  RESOURCE_KIND="$kind" RESOURCE_NAME="$name" yq '
    select(.kind == strenv(RESOURCE_KIND) and .metadata.namespace == "mattercodex-system" and
      .metadata.name == strenv(RESOURCE_NAME))
  ' "$render_file" >"$desired_yaml"
  [[ -s "$desired_yaml" ]] || fail "immutable release resource is absent: $kind/$name"
  yq -o=json -I=0 '.' "$desired_yaml" >"$desired_json"
  immutable_resource_payload "$kind" "$desired_json" "$desired_payload"

  if kubectl -n "$namespace" get "$resource/$name" -o json >"$live_json" 2>/dev/null; then
    immutable_resource_payload "$kind" "$live_json" "$live_payload"
    if cmp -s "$desired_payload" "$live_payload"; then
      return
    fi
    kubectl -n "$namespace" delete "$resource/$name" --wait=true --timeout=2m >/dev/null
  fi

  kubectl create --field-manager=mattercodex-fresh-install -f "$desired_yaml" >/dev/null
  kubectl -n "$namespace" get "$resource/$name" -o json >"$live_json" ||
    fail "immutable release resource readback failed: $kind/$name"
  immutable_resource_payload "$kind" "$live_json" "$live_payload"
  cmp -s "$desired_payload" "$live_payload" ||
    fail "immutable release resource payload mismatch: $kind/$name"
}

rotate_release_immutable_resources() {
  replace_immutable_resource_on_drift configmap ConfigMap mattercodex-role-environments
  replace_immutable_resource_on_drift configmap ConfigMap mattercodex-image-admission-policy
  replace_immutable_resource_on_drift \
    imageadmissionpolicyparameters.supplychain.mattercodex.dev \
    ImageAdmissionPolicyParameters mattercodex-image-admission-policy
}

validate_restore_evidence_anchor() {
  local input_file=$1
  jq -e --arg namespace "$namespace" --arg name "$restore_evidence_name" '
    .apiVersion == "v1" and
    .kind == "Secret" and
    .metadata.namespace == $namespace and
    .metadata.name == $name and
    .type == "Opaque" and
    (.metadata.annotations as $annotations |
      ($annotations["mattercodex.dev/restore-anchor-revision"] | type == "string" and test("^[1-9][0-9]*$")) and
      ($annotations["mattercodex.dev/restore-epoch"] | type == "string" and test("^[1-9][0-9]*$")) and
      ($annotations["mattercodex.dev/restore-evidence-digest-sha256"] | type == "string" and test("^[a-f0-9]{64}$")) and
      ($annotations["mattercodex.dev/restore-predecessor-revision"] | type == "string" and test("^(0|[1-9][0-9]*)$")) and
      ($annotations["mattercodex.dev/restore-predecessor-digest-sha256"] | type == "string" and test("^[a-f0-9]{64}$")) and
      ($annotations["mattercodex.dev/restored-cluster-uid"] | type == "string" and length > 0) and
      ($annotations["mattercodex.dev/restored-timeline-id"] | type == "string" and length > 0) and
      (($annotations["mattercodex.dev/restore-anchor-revision"] | tonumber) >
        ($annotations["mattercodex.dev/restore-predecessor-revision"] | tonumber))) and
    (.data["evidence.jws"] | type == "string")
  ' "$input_file" >/dev/null || fail 'restore evidence anchor is absent or malformed'
}

ensure_restore_evidence_anchor() {
  local anchor_manifest="$temporary_directory/restore-evidence-anchor.yaml"
  local anchor_json="$temporary_directory/restore-evidence-anchor.json"
  RESTORE_EVIDENCE_NAME="$restore_evidence_name" yq '
    select(.kind == "Secret" and .metadata.namespace == "mattercodex-system" and
      .metadata.name == strenv(RESTORE_EVIDENCE_NAME))
  ' "$render_file" >"$anchor_manifest"
  [[ -s "$anchor_manifest" ]] || fail 'restore evidence anchor is absent from release render'

  if ! kubectl -n "$namespace" get secret "$restore_evidence_name" >/dev/null 2>&1; then
    kubectl create -f "$anchor_manifest" >/dev/null
  fi
  kubectl -n "$namespace" get secret "$restore_evidence_name" -o json >"$anchor_json" ||
    fail 'restore evidence anchor readback failed'
  validate_restore_evidence_anchor "$anchor_json"
}

validate_authority_snapshot() {
  local input_file=$1
  jq -e --arg namespace "$namespace" --arg name "$authority_snapshot_name" '
    .apiVersion == "v1" and
    .kind == "Secret" and
    .metadata.namespace == $namespace and
    .metadata.name == $name and
    .type == "Opaque" and
    ((.data | keys) == ["snapshot.jws"]) and
    (.metadata.annotations as $annotations |
      ($annotations["mattercodex.dev/source-revision"] | type == "string" and test("^(0|[1-9][0-9]*)$")) and
      ($annotations["mattercodex.dev/signer-generation"] | type == "string" and test("^(0|[1-9][0-9]*)$")) and
      (($annotations["mattercodex.dev/source-revision"] | tonumber) as $revision |
       ($annotations["mattercodex.dev/signer-generation"] | tonumber) as $generation |
       if $revision == 0 then
         $generation == 0 and
         $annotations["mattercodex.dev/source-digest-sha256"] == "" and
         .data["snapshot.jws"] == ""
       else
         $generation > 0 and
         ($annotations["mattercodex.dev/source-digest-sha256"] | type == "string" and test("^[a-f0-9]{64}$")) and
         (.data["snapshot.jws"] | type == "string" and length > 0)
       end))
  ' "$input_file" >/dev/null || fail 'authority snapshot is absent or malformed'
}

write_authority_snapshot_seed_manifest() {
  local output_file=$1
  AUTHORITY_SNAPSHOT_NAME="$authority_snapshot_name" yq '
    select(.kind == "Secret" and .metadata.namespace == "mattercodex-system" and
      .metadata.name == strenv(AUTHORITY_SNAPSHOT_NAME))
  ' "$render_file" >"$output_file"
  [[ -s "$output_file" ]] || fail 'authority snapshot seed is absent from release render'
}

ensure_authority_snapshot() {
  local seed_manifest="$temporary_directory/authority-snapshot-seed.yaml"
  local seed_json="$temporary_directory/authority-snapshot-seed.json"
  local live_json="$temporary_directory/authority-snapshot-live.json"

  write_authority_snapshot_seed_manifest "$seed_manifest"
  yq -o=json -I=0 '.' "$seed_manifest" >"$seed_json"
  validate_authority_snapshot "$seed_json"

  if kubectl -n "$namespace" get secret "$authority_snapshot_name" -o json >"$live_json" 2>/dev/null; then
    validate_authority_snapshot "$live_json"
    return
  fi

  if ! kubectl create --field-manager=mattercodex-fresh-install -f "$seed_manifest" >/dev/null; then
    kubectl -n "$namespace" get secret "$authority_snapshot_name" -o json >"$live_json" ||
      fail 'authority snapshot seed creation failed'
  else
    kubectl -n "$namespace" get secret "$authority_snapshot_name" -o json >"$live_json" ||
      fail 'authority snapshot seed readback failed'
  fi
  validate_authority_snapshot "$live_json"
}

require_authority_snapshot() {
  local live_json="$temporary_directory/authority-snapshot-live.json"
  kubectl -n "$namespace" get secret "$authority_snapshot_name" -o json >"$live_json" ||
    fail 'authority snapshot is absent; run apply-state first'
  validate_authority_snapshot "$live_json"
}

wait_statefulset() {
  local name=$1
  kubectl -n "$namespace" rollout status "statefulset/$name" --timeout=10m >/dev/null ||
    fail "StatefulSet rollout failed: $name"
}

wait_vault_static_secrets() {
  local allow_deferred_restore_secret=${1:-false}
  local resource_name

  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    if [[ "$allow_deferred_restore_secret" == true &&
      "$resource_name" == internal-rpc-authority-restore-controller-postgresql ]]; then
      continue
    fi
    kubectl -n "$namespace" wait --for=condition=Ready \
      "vaultstaticsecrets.secrets.hashicorp.com/$resource_name" --timeout=5m >/dev/null ||
      fail "VaultStaticSecret is not ready: $resource_name"
  done < <(yq -N -r 'select(.kind == "VaultStaticSecret") | .metadata.name' "$render_file" | sort -u)
}

wait_vault_dynamic_secrets() {
  local resource_name destination_name
  while IFS=$'\t' read -r resource_name destination_name; do
    [[ -n "$resource_name" && -n "$destination_name" ]] || continue
    kubectl -n "$namespace" wait --for=condition=Ready \
      "vaultdynamicsecrets.secrets.hashicorp.com/$resource_name" --timeout=5m >/dev/null ||
      fail "VaultDynamicSecret is not ready: $resource_name"
    kubectl -n "$namespace" get secret "$destination_name" -o json | jq -e '
      .type == "Opaque" and
      (.data | keys | sort) == ["dsn", "username"] and
      (.data.dsn | type == "string" and length > 0) and
      (.data.username | type == "string" and length > 0)
    ' >/dev/null || fail "VaultDynamicSecret destination readback failed: $destination_name"
  done < <(yq -N -r '
    select(.kind == "VaultDynamicSecret") |
    [.metadata.name,.spec.destination.name] | @tsv
  ' "$render_file" | sort -u)
}

wait_trust_material() {
  local allow_deferred_restore_secret=${1:-false}
  local resource_name

  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    kubectl -n "$namespace" wait --for=condition=Ready \
      "certificates.cert-manager.io/$resource_name" --timeout=5m >/dev/null ||
      fail "Certificate is not ready: $resource_name"
  done < <(yq -N -r 'select(.kind == "Certificate") | .metadata.name' "$render_file" | sort -u)

  while IFS= read -r resource_name; do
    [[ -n "$resource_name" ]] || continue
    kubectl wait --for=condition=Synced \
      "bundles.trust.cert-manager.io/$resource_name" --timeout=5m >/dev/null ||
      fail "CA Bundle is not synced: $resource_name"
  done < <(yq -N -r 'select(.kind == "Bundle") | .metadata.name' "$render_file" | sort -u)

  wait_vault_static_secrets "$allow_deferred_restore_secret"
}

apply_restore_controller_database_secret() {
  local output_file="$temporary_directory/restore-controller-database-secret.yaml"
  yq '
    select(.kind == "VaultStaticSecret" and
      .metadata.namespace == "mattercodex-system" and
      .metadata.name == "internal-rpc-authority-restore-controller-postgresql")
  ' "$render_file" >"$output_file"
  [[ -s "$output_file" ]] || fail 'restore controller VaultStaticSecret is absent'
  kubectl apply --server-side --field-manager=mattercodex-fresh-install \
    -f "$output_file" >/dev/null
  kubectl -n "$namespace" wait --for=condition=Ready \
    vaultstaticsecret.secrets.hashicorp.com/internal-rpc-authority-restore-controller-postgresql \
    --timeout=2m >/dev/null || fail 'restore controller PostgreSQL credential was not synchronized'
  kubectl -n "$namespace" get secret internal-rpc-authority-restore-controller-postgresql \
    -o json | jq -e '
      .type == "Opaque" and
      (.data.dsn | type == "string" and length > 0)
    ' >/dev/null || fail 'restore controller PostgreSQL credential readback failed'
}

apply_job() {
  local name=$1
  local output_file="$temporary_directory/job-$name.yaml"
  if kubectl -n "$namespace" get job "$name" >/dev/null 2>&1; then
    if [[ $(kubectl -n "$namespace" get job "$name" -o jsonpath='{.status.succeeded}') == 1 ]]; then
      return
    fi
    kubectl -n "$namespace" delete job "$name" --wait=true --timeout=5m >/dev/null
  fi
  JOB_NAME="$name" yq 'select(.kind == "Job" and .metadata.name == strenv(JOB_NAME))' \
    "$render_file" >"$output_file"
  [[ -s "$output_file" ]] || fail "release Job is absent: $name"
  kubectl apply --server-side --field-manager=mattercodex-fresh-install -f "$output_file" >/dev/null
  kubectl -n "$namespace" wait --for=condition=Complete "job/$name" --timeout=15m >/dev/null ||
    fail "release Job failed: $name"
}

if [[ "$mode" == apply-state ]]; then
  kubectl -n "$namespace" get pod vault-0 >/dev/null 2>&1 || fail 'Vault must be installed before state phase'
  "$vault_bootstrap" --context "$expected_context" --mode initialize \
    --material-directory "$material_directory"
  "$vault_bootstrap" --context "$expected_context" --mode configure-core \
    --material-directory "$material_directory"
  "$vault_bootstrap" --context "$expected_context" --mode configure-policies \
    --material-directory "$material_directory" --render "$render_file"
  "$vault_bootstrap" --context "$expected_context" --mode configure-image-pki \
    --material-directory "$material_directory" --render "$render_file"

  ensure_restore_evidence_anchor
  ensure_authority_bootstrap_roots
  ensure_authority_snapshot
  apply_filter custom-resource-definitions '
    select(.kind == "CustomResourceDefinition")
  '
  while IFS= read -r custom_resource_definition; do
    [[ -n "$custom_resource_definition" ]] || continue
    kubectl wait --for=condition=Established \
      "customresourcedefinition/$custom_resource_definition" --timeout=2m >/dev/null ||
      fail "CustomResourceDefinition was not established: $custom_resource_definition"
  done < <(yq -N -r '
    select(.kind == "CustomResourceDefinition") | .metadata.name
  ' "$render_file" | sort -u)
  rotate_release_immutable_resources
  apply_filter foundation '
    select(.kind != "CustomResourceDefinition" and
      .kind != "Deployment" and .kind != "StatefulSet" and .kind != "DaemonSet" and
      .kind != "Job" and .kind != "CronJob" and
      ((.kind == "VaultStaticSecret" and .metadata.namespace == "mattercodex-system" and
        .metadata.name == "internal-rpc-authority-restore-controller-postgresql") | not) and
      ((.kind == "Secret" and .metadata.namespace == "mattercodex-system" and
        (.metadata.name == "internal-rpc-authority-restore-evidence" or
         .metadata.name == "internal-rpc-authority-snapshot")) | not))
  '
  wait_trust_material true
  apply_filter stateful '
    select(.kind == "StatefulSet" and
      (.metadata.name == "mattercodex-postgresql" or .metadata.name == "mattercodex-nats"))
  '
  wait_statefulset mattercodex-postgresql
  wait_statefulset mattercodex-nats
  "$vault_bootstrap" --context "$expected_context" --mode configure-database \
    --material-directory "$material_directory" --render "$render_file"
fi

if [[ "$mode" == apply-migrations ]]; then
  wait_statefulset mattercodex-postgresql
  wait_statefulset mattercodex-nats
  apply_job internal-rpc-authority-migrate
  "$vault_bootstrap" --context "$expected_context" --mode configure-database-runtime \
    --material-directory "$material_directory" --render "$render_file"
  wait_vault_dynamic_secrets
  apply_restore_controller_database_secret
  apply_job control-plane-migrate
  apply_job control-plane-broker-bootstrap
fi

if [[ "$mode" == apply-workloads ]]; then
  for migration_job in internal-rpc-authority-migrate control-plane-migrate control-plane-broker-bootstrap; do
    [[ $(kubectl -n "$namespace" get job "$migration_job" -o jsonpath='{.status.succeeded}') == 1 ]] ||
      fail "completed prerequisite Job is absent: $migration_job"
  done
  ensure_restore_evidence_anchor
  require_authority_bootstrap_roots
  require_authority_snapshot
  wait_vault_dynamic_secrets
  rotate_release_immutable_resources
  apply_filter workloads '
    select(.kind != "Job" and
      ((.kind == "Secret" and .metadata.namespace == "mattercodex-system" and
        (.metadata.name == "internal-rpc-authority-restore-evidence" or
         .metadata.name == "internal-rpc-authority-snapshot")) | not))
  '
  pin_authority_root_revision
  wait_trust_material
  for materializer_dependency in egress-gateway mattercodex-image-registry-promotion; do
    kubectl -n "$namespace" rollout status "deployment/$materializer_dependency" --timeout=15m >/dev/null ||
      fail "release artifact materializer dependency failed: $materializer_dependency"
  done
  apply_job release-artifact-materializer
  for registry_deployment in \
    mattercodex-image-registry-push mattercodex-image-registry-staging-read \
    mattercodex-image-registry-evidence mattercodex-image-registry-admin \
    mattercodex-image-registry-promotion mattercodex-image-registry-pull mattercodex-buildkit; do
    kubectl -n "$namespace" rollout status "deployment/$registry_deployment" --timeout=15m >/dev/null ||
      fail "image supply chain rollout failed: $registry_deployment"
  done
  while IFS= read -r deployment_name; do
    [[ -n "$deployment_name" ]] || continue
    kubectl -n "$namespace" rollout status "deployment/$deployment_name" --timeout=15m >/dev/null ||
      fail "Deployment rollout failed: $deployment_name"
  done < <(yq -N -r 'select(.kind == "Deployment") | .metadata.name' "$render_file" | sort -u)
  while IFS= read -r daemonset_name; do
    [[ -n "$daemonset_name" ]] || continue
    kubectl -n "$namespace" rollout status "daemonset/$daemonset_name" --timeout=15m >/dev/null ||
      fail "DaemonSet rollout failed: $daemonset_name"
  done < <(yq -N -r 'select(.kind == "DaemonSet") | .metadata.name' "$render_file" | sort -u)
fi

if [[ "$mode" == readback ]]; then
  "$vault_bootstrap" --context "$expected_context" --mode readback \
    --material-directory "$material_directory" --render "$render_file"
  ensure_restore_evidence_anchor
  require_authority_bootstrap_roots
  require_authority_snapshot
  wait_trust_material
  wait_vault_dynamic_secrets
  while IFS=$'\t' read -r kind name; do
    [[ -n "$kind" && -n "$name" ]] || continue
    case "$kind" in
      Deployment|StatefulSet|DaemonSet)
        kubectl -n "$namespace" rollout status "${kind,,}/$name" --timeout=60s >/dev/null ||
          fail "workload readback failed: $kind/$name"
        ;;
      Job)
        [[ $(kubectl -n "$namespace" get job "$name" -o jsonpath='{.status.succeeded}') == 1 ]] ||
          fail "Job readback failed: $name"
        ;;
    esac
  done < <(yq -N -r 'select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "DaemonSet" or .kind == "Job") | [.kind,.metadata.name] | @tsv' \
    "$render_file" | sort -u)
  failing_pods=$(kubectl -n "$namespace" get pods -o json | jq -r '
    [.items[] | select(any(.status.containerStatuses[]?;
      .state.waiting.reason == "CrashLoopBackOff" or .state.waiting.reason == "ImagePullBackOff" or
      .state.waiting.reason == "ErrImagePull" or .state.waiting.reason == "CreateContainerConfigError")) |
      .metadata.name] | join(",")
  ')
  [[ -z "$failing_pods" ]] || fail "failing Pods remain: $failing_pods"
fi

printf 'Fresh release deployment completed: %s\n' "$mode"
