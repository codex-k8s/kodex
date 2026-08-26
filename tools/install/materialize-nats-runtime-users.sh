#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'NATS runtime credential materialization failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --context <context> --material-directory <path>\n' "$0" >&2
}

context=""
material_directory=""
while (($# > 0)); do
  case "$1" in
    --context) context="${2:-}"; shift 2 ;;
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" ]] || fail 'context is required'
[[ "$material_directory" == /* && -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'material directory is invalid'
for command_name in awk base64 find install jq kubectl mktemp nsc rmdir sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$context" ]] || fail 'Kubernetes context mismatch'

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
policy_file="$repository_root/tools/install/nats-runtime-users.tsv"
version_file="$material_directory/nats/runtime-user-policy.version"
pending_directory="$material_directory/nats/runtime-user-policy.pending"
readonly policy_version=3
[[ -s "$policy_file" ]] || fail 'NATS runtime user policy is absent'
[[ ! -e "$pending_directory" || (-d "$pending_directory" && ! -L "$pending_directory") ]] ||
  fail 'NATS runtime user pending directory is invalid'

umask 077
temporary_directory=$(mktemp -d "$material_directory/.nats-runtime-materialization.XXXXXX")
trap 'rm -rf -- "$temporary_directory"' EXIT

digest_file() {
  sha256sum "$1" | awk '{print $1}'
}

secret_item_matches() {
  local name=$1 key=$2 file=$3 encoded remote_file
  encoded=$(kubectl -n kodex-system get secret "$name" -o json 2>/dev/null |
    jq -er --arg key "$key" '.data[$key]') || return 1
  remote_file="$temporary_directory/$name.$key"
  printf '%s' "$encoded" | base64 --decode >"$remote_file" || return 1
  [[ "$(digest_file "$remote_file")" == "$(digest_file "$file")" ]]
}

current_version=""
if [[ -f "$version_file" && ! -L "$version_file" ]]; then
  current_version=$(<"$version_file")
fi
requires_rollout=false
[[ "$current_version" == "$policy_version" && ! -e "$pending_directory" ]] ||
  requires_rollout=true

for required_file in \
  "$material_directory/nats/operator.jwt" \
  "$material_directory/nats/system-account.public" \
  "$material_directory/nats/system-account.jwt" \
  "$material_directory/nats/account.public" \
  "$material_directory/nats/account.jwt" \
  "$material_directory/projections/control-plane-nats/user.creds" \
  "$material_directory/projections/control-plane-nats-bootstrap/user.creds" \
  "$material_directory/projections/control-api-gateway-nats/user.creds"; do
  [[ -s "$required_file" && ! -L "$required_file" ]] || fail "required material is invalid: $required_file"
done

kubectl create namespace kodex-system --dry-run=client -o yaml |
  kubectl apply --server-side --field-manager=kodex-install -f - >/dev/null

apply_secret() {
  local name=$1
  shift
  kubectl -n kodex-system create secret generic "$name" "$@" --dry-run=client -o yaml |
    kubectl apply --server-side --force-conflicts --field-manager=kodex-install -f - >/dev/null
}

apply_secret_if_changed() {
  local name=$1
  shift
  local specification key file actual_count changed=false
  actual_count=$(kubectl -n kodex-system get secret "$name" -o json 2>/dev/null |
    jq -er '.data | length') || actual_count=-1
  [[ "$actual_count" == "$#" ]] || changed=true
  for specification in "$@"; do
    key=${specification%%=*}
    file=${specification#*=}
    secret_item_matches "$name" "$key" "$file" || changed=true
  done
  if [[ "$changed" == true ]]; then
    requires_rollout=true
    local -a arguments=()
    for specification in "$@"; do
      key=${specification%%=*}
      file=${specification#*=}
      arguments+=("--from-file=$key=$file")
    done
    apply_secret "$name" "${arguments[@]}"
  fi
}

apply_secret_if_changed kodex-nats-credentials \
  operator.jwt="$material_directory/nats/operator.jwt" \
  system-account.public="$material_directory/nats/system-account.public" \
  system-account.jwt="$material_directory/nats/system-account.jwt" \
  account.public="$material_directory/nats/account.public" \
  account.jwt="$material_directory/nats/account.jwt"
apply_secret_if_changed control-plane-nats \
  user.creds="$material_directory/projections/control-plane-nats/user.creds"
apply_secret_if_changed control-plane-nats-bootstrap \
  user.creds="$material_directory/projections/control-plane-nats-bootstrap/user.creds"
apply_secret_if_changed control-api-gateway-nats \
  user.creds="$material_directory/projections/control-api-gateway-nats/user.creds"

for contract in \
  'kodex-nats-credentials:account.jwt,account.public,operator.jwt,system-account.jwt,system-account.public' \
  'control-plane-nats:user.creds' \
  'control-plane-nats-bootstrap:user.creds' \
  'control-api-gateway-nats:user.creds'; do
  name=${contract%%:*}
  expected=${contract#*:}
  actual=$(kubectl -n kodex-system get secret "$name" -o json |
    jq -er '.data | keys | sort | join(",")')
  [[ "$actual" == "$expected" ]] || fail "Kubernetes Secret key readback mismatch: $name"
done

for mapping in \
  'kodex-nats-credentials:operator.jwt:nats/operator.jwt' \
  'kodex-nats-credentials:system-account.public:nats/system-account.public' \
  'kodex-nats-credentials:system-account.jwt:nats/system-account.jwt' \
  'kodex-nats-credentials:account.public:nats/account.public' \
  'kodex-nats-credentials:account.jwt:nats/account.jwt' \
  'control-plane-nats:user.creds:projections/control-plane-nats/user.creds' \
  'control-plane-nats-bootstrap:user.creds:projections/control-plane-nats-bootstrap/user.creds' \
  'control-api-gateway-nats:user.creds:projections/control-api-gateway-nats/user.creds'; do
  name=${mapping%%:*}
  remainder=${mapping#*:}
  key=${remainder%%:*}
  relative_file=${remainder#*:}
  secret_item_matches "$name" "$key" "$material_directory/$relative_file" ||
    fail "Kubernetes Secret content readback mismatch: $name/$key"
done

if [[ -d "$pending_directory" && ! -L "$pending_directory" ]]; then
  account_claim=$(nsc describe jwt --json --file "$material_directory/nats/account.jwt") ||
    fail 'describe current NATS account JWT'
  while IFS='|' read -r user_name _ _ _ _; do
    previous_credentials="$pending_directory/$user_name.previous.creds"
    current_credentials="$material_directory/nats/users/$user_name.creds"
    [[ -s "$previous_credentials" && ! -L "$previous_credentials" ]] ||
      fail "pending previous NATS credentials are invalid: $user_name"
    previous_claim=$(nsc describe jwt --json --file "$previous_credentials") ||
      fail "describe previous NATS credentials: $user_name"
    current_claim=$(nsc describe jwt --json --file "$current_credentials") ||
      fail "describe current NATS credentials: $user_name"
    user_subject=$(jq -er '.sub | select(test("^U[A-Z2-7]{55}$"))' <<<"$current_claim") ||
      fail "current NATS user subject is invalid: $user_name"
    previous_issued_at=$(jq -er --arg subject "$user_subject" '
      select(.sub == $subject and .nats.type == "user") | .iat | select(type == "number")
    ' <<<"$previous_claim") || fail "previous NATS credential lineage mismatch: $user_name"
    current_issued_at=$(jq -er '.iat | select(type == "number")' <<<"$current_claim") ||
      fail "current NATS credential issue time is invalid: $user_name"
    revocation_cutoff=$(jq -er --arg subject "$user_subject" '
      .nats.revocations[$subject] | select(type == "number")
    ' <<<"$account_claim") || fail "NATS revocation cutoff is absent: $user_name"
    ((previous_issued_at <= revocation_cutoff && current_issued_at > revocation_cutoff)) ||
      fail "NATS credential revocation ordering mismatch: $user_name"
  done <"$policy_file"
fi

if [[ "$requires_rollout" == true ]]; then
  statefulset_name=$(kubectl -n kodex-system get statefulset kodex-nats \
    --ignore-not-found -o name) || fail 'read NATS StatefulSet before credential rollout'
  if [[ -n "$statefulset_name" && "$statefulset_name" != statefulset.apps/kodex-nats ]]; then
    fail 'NATS StatefulSet readback is invalid'
  fi
  if [[ -n "$statefulset_name" ]]; then
    kubectl -n kodex-system rollout restart statefulset/kodex-nats >/dev/null
    kubectl -n kodex-system rollout status statefulset/kodex-nats --timeout=5m >/dev/null ||
      fail 'NATS rollout after credential rotation failed'
  fi
  for deployment in control-plane control-api-gateway; do
    deployment_name=$(kubectl -n kodex-system get deployment "$deployment" \
      --ignore-not-found -o name) || fail "read workload before NATS credential rollout: $deployment"
    if [[ -n "$deployment_name" && "$deployment_name" != "deployment.apps/$deployment" ]]; then
      fail "workload readback is invalid: $deployment"
    fi
    if [[ -n "$deployment_name" ]]; then
      kubectl -n kodex-system rollout restart "deployment/$deployment" >/dev/null
      kubectl -n kodex-system rollout status "deployment/$deployment" --timeout=5m >/dev/null ||
        fail "workload rollout after NATS credential rotation failed: $deployment"
    fi
  done
fi

if [[ -d "$pending_directory" && ! -L "$pending_directory" ]]; then
  find "$pending_directory" -mindepth 1 -maxdepth 1 -type f -delete
  rmdir "$pending_directory" || fail 'remove applied NATS credential evidence directory'
fi
printf '%s\n' "$policy_version" >"$temporary_directory/runtime-user-policy.version"
install -m 0600 "$temporary_directory/runtime-user-policy.version" "$version_file"

printf 'NATS runtime credentials materialized without secret output\n'
