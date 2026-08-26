#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'NATS runtime user reconciliation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --material-directory <path>\n' "$0" >&2
}

material_directory=""
while (($# > 0)); do
  case "$1" in
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$material_directory" == /* && -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'material directory is invalid'
for command_name in date find install jq mktemp nsc sha256sum sleep sort xargs; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
policy_file="$repository_root/tools/install/nats-runtime-users.tsv"
registry_file="$repository_root/tools/install/secret-projections.json"
nsc_home="$material_directory/nats/nsc"
version_file="$material_directory/nats/runtime-user-policy.version"
readonly policy_version=2

[[ -d "$nsc_home" && ! -L "$nsc_home" && -s "$policy_file" && -s "$registry_file" ]] ||
  fail 'NATS policy sources are incomplete'

permission_matches() {
  local user_name=$1 publish_allow=$2 subscribe_allow=$3 readback
  readback=$(nsc -H "$nsc_home" describe user --account APPLICATION --name "$user_name" --json) ||
    return 1
  jq -e --arg name "$user_name" --arg publish "$publish_allow" --arg subscribe "$subscribe_allow" '
    .name == $name and .nats.type == "user" and
    ((.nats.pub.allow // []) | sort) == ($publish | split(",") | sort) and
    ((.nats.sub.allow // []) | sort) == ($subscribe | split(",") | sort) and
    ((.nats.pub.deny // []) | length) == 0 and
    ((.nats.sub.deny // []) | length) == 0 and (.nats.resp // null) == null
  ' <<<"$readback" >/dev/null
}

current_version=""
if [[ -f "$version_file" && ! -L "$version_file" ]]; then
  current_version=$(<"$version_file")
fi
policy_exact=true
while IFS='|' read -r user_name publish_allow subscribe_allow material_ref secret_name; do
  [[ -n "$user_name" && -n "$publish_allow" && -n "$subscribe_allow" &&
    -n "$material_ref" && -n "$secret_name" ]] || fail 'NATS user policy row is invalid'
  permission_matches "$user_name" "$publish_allow" "$subscribe_allow" || policy_exact=false
done <"$policy_file"
if [[ "$current_version" == "$policy_version" && "$policy_exact" == true ]]; then
  printf 'unchanged\n'
  exit 0
fi

umask 077
temporary_directory=$(mktemp -d "$material_directory/.nats-runtime-users.XXXXXX")
trap 'rm -rf -- "$temporary_directory"' EXIT
revocation_cutoff=$(($(date +%s) + 1))

while IFS='|' read -r user_name _ _ _ _; do
  readback=$(nsc -H "$nsc_home" describe user --account APPLICATION --name "$user_name" --json) ||
    fail "NATS user is absent: $user_name"
  user_subject=$(jq -er '.sub | select(test("^U[A-Z2-7]{55}$"))' <<<"$readback") ||
    fail "NATS user subject is invalid: $user_name"
  current_permissions=$(jq -r '
    [(.nats.pub.allow // [])[], (.nats.pub.deny // [])[],
     (.nats.sub.allow // [])[], (.nats.sub.deny // [])[]] | unique | join(",")
  ' <<<"$readback")
  if [[ -n "$current_permissions" ]]; then
    nsc -H "$nsc_home" edit user --account APPLICATION --name "$user_name" \
      --rm "$current_permissions" >/dev/null 2>&1 || fail "remove old NATS permissions: $user_name"
  fi
  nsc -H "$nsc_home" edit user --account APPLICATION --name "$user_name" \
    --rm-response-perms >/dev/null 2>&1 || fail "remove old NATS response permissions: $user_name"
  nsc -H "$nsc_home" revocations add-user --account APPLICATION \
    --user-public-key "$user_subject" --at "$revocation_cutoff" >/dev/null 2>&1 ||
    fail "revoke old NATS credentials: $user_name"
done <"$policy_file"

while (($(date +%s) <= revocation_cutoff)); do
  sleep 1
done

while IFS='|' read -r user_name publish_allow subscribe_allow material_ref secret_name; do
  nsc -H "$nsc_home" edit user --account APPLICATION --name "$user_name" \
    --allow-pub "$publish_allow" --allow-sub "$subscribe_allow" >/dev/null 2>&1 ||
    fail "apply exact NATS permissions: $user_name"
  permission_matches "$user_name" "$publish_allow" "$subscribe_allow" ||
    fail "NATS permission readback mismatch: $user_name"
  nsc -H "$nsc_home" generate creds --account APPLICATION --name "$user_name" \
    --output-file "$temporary_directory/$user_name.creds" >/dev/null 2>&1 ||
    fail "generate rotated NATS credentials: $user_name"
  [[ -s "$temporary_directory/$user_name.creds" && ! -L "$temporary_directory/$user_name.creds" ]] ||
    fail "rotated NATS credentials are invalid: $user_name"
  jq -e --arg secret "$secret_name" --arg ref "$material_ref" '
    any(.secrets[];
      .name == $secret and any(.items[];
        .key == "user.creds" and .source.type == "material" and
        .source.ref == $ref and .source.field == "credentials"))
  ' "$registry_file" >/dev/null || fail "NATS secret projection mapping mismatch: $user_name"
done <"$policy_file"

"$repository_root/tools/deploy/materialize-nats-operator-files.sh" \
  --nsc-home "$nsc_home" --output-directory "$material_directory/nats" >/dev/null

while IFS='|' read -r user_name _ _ material_ref secret_name; do
  install -m 0600 "$temporary_directory/$user_name.creds" \
    "$material_directory/nats/users/$user_name.creds"
  install -d -m 0700 "$material_directory/material/$material_ref" \
    "$material_directory/projections/$secret_name"
  install -m 0600 "$temporary_directory/$user_name.creds" \
    "$material_directory/material/$material_ref/credentials"
  install -m 0600 "$temporary_directory/$user_name.creds" \
    "$material_directory/projections/$secret_name/user.creds"
done <"$policy_file"

find "$material_directory/projections" -type f -print0 | sort -z | xargs -0 sha256sum \
  >"$temporary_directory/projections.sha256"
install -m 0600 "$temporary_directory/projections.sha256" "$material_directory/projections.sha256"
printf '%s\n' "$policy_version" >"$temporary_directory/runtime-user-policy.version"
install -m 0600 "$temporary_directory/runtime-user-policy.version" "$version_file"
printf 'changed\n'
