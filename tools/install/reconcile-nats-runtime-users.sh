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
for command_name in date find install jq mktemp nsc rmdir sha256sum sleep sort xargs; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
policy_file="$repository_root/tools/install/nats-runtime-users.tsv"
registry_file="$repository_root/tools/install/secret-projections.json"
nsc_home="$material_directory/nats/nsc"
version_file="$material_directory/nats/runtime-user-policy.version"
pending_directory="$material_directory/nats/runtime-user-policy.pending"
readonly policy_version=3

[[ -d "$nsc_home" && ! -L "$nsc_home" && -s "$policy_file" && -s "$registry_file" ]] ||
  fail 'NATS policy sources are incomplete'
[[ ! -e "$pending_directory" || (-d "$pending_directory" && ! -L "$pending_directory") ]] ||
  fail 'NATS runtime user pending directory is invalid'
if [[ -d "$pending_directory" &&
  -z "$(find "$pending_directory" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
  rmdir "$pending_directory" || fail 'remove empty NATS runtime user pending directory'
fi

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

credential_matches() {
  local credentials_file=$1 user_name=$2 publish_allow=$3 subscribe_allow=$4 readback
  [[ -s "$credentials_file" && ! -L "$credentials_file" ]] || return 1
  readback=$(nsc describe jwt --json --file "$credentials_file") || return 1
  jq -e --arg name "$user_name" --arg publish "$publish_allow" \
    --arg subscribe "$subscribe_allow" '
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
credentials_exact=true
while IFS='|' read -r user_name publish_allow subscribe_allow material_ref secret_name; do
  [[ -n "$user_name" && -n "$publish_allow" && -n "$subscribe_allow" &&
    -n "$material_ref" && -n "$secret_name" ]] || fail 'NATS user policy row is invalid'
  permission_matches "$user_name" "$publish_allow" "$subscribe_allow" || policy_exact=false
  credential_matches "$material_directory/nats/users/$user_name.creds" \
    "$user_name" "$publish_allow" "$subscribe_allow" || credentials_exact=false
done <"$policy_file"
if [[ "$current_version" == "$policy_version" && "$policy_exact" == true &&
  "$credentials_exact" == true &&
  ! -e "$pending_directory" ]]; then
  printf 'unchanged\n'
  exit 0
fi
if [[ "$policy_exact" == true && "$credentials_exact" == true &&
  ! -e "$pending_directory" ]]; then
  printf 'changed\n'
  exit 0
fi

umask 077
temporary_directory=$(mktemp -d "$material_directory/.nats-runtime-users.XXXXXX")
trap 'rm -rf -- "$temporary_directory"' EXIT

install_runtime_credentials() {
  local source_directory=$1 user_name material_ref secret_name
  while IFS='|' read -r user_name _ _ material_ref secret_name; do
    install -m 0600 "$source_directory/$user_name.creds" \
      "$material_directory/nats/users/$user_name.creds"
    install -d -m 0700 "$material_directory/material/$material_ref" \
      "$material_directory/projections/$secret_name"
    install -m 0600 "$source_directory/$user_name.creds" \
      "$material_directory/material/$material_ref/credentials"
    install -m 0600 "$source_directory/$user_name.creds" \
      "$material_directory/projections/$secret_name/user.creds"
  done <"$policy_file"
  find "$material_directory/projections" -type f -print0 | sort -z | xargs -0 sha256sum \
    >"$temporary_directory/projections.sha256"
  install -m 0600 "$temporary_directory/projections.sha256" \
    "$material_directory/projections.sha256"
}

if [[ "$policy_exact" == true && -d "$pending_directory" ]]; then
  account_claim=$(nsc -H "$nsc_home" describe account --name APPLICATION --json) ||
    fail 'describe NATS application account'
  while IFS='|' read -r user_name publish_allow subscribe_allow _ _; do
    pending_credentials="$pending_directory/$user_name.previous.creds"
    [[ -s "$pending_credentials" && ! -L "$pending_credentials" ]] ||
      fail "pending previous NATS credentials are invalid: $user_name"
    current_credentials="$material_directory/nats/users/$user_name.creds"
    current_valid=false
    if [[ -s "$current_credentials" && ! -L "$current_credentials" ]]; then
      current_claim=$(nsc describe jwt --json --file "$current_credentials") || current_claim='{}'
      user_subject=$(nsc -H "$nsc_home" describe user --account APPLICATION \
        --name "$user_name" --json | jq -er '.sub') ||
        fail "NATS user subject is invalid: $user_name"
      if jq -e --arg subject "$user_subject" --arg name "$user_name" \
        --arg publish "$publish_allow" --arg subscribe "$subscribe_allow" '
        .sub == $subject and .name == $name and .nats.type == "user" and
        (.iat | type == "number") and
        ((.nats.pub.allow // []) | sort) == ($publish | split(",") | sort) and
        ((.nats.sub.allow // []) | sort) == ($subscribe | split(",") | sort) and
        ((.nats.pub.deny // []) | length) == 0 and
        ((.nats.sub.deny // []) | length) == 0 and (.nats.resp // null) == null
      ' <<<"$current_claim" >/dev/null; then
        current_issued_at=$(jq -er '.iat' <<<"$current_claim")
        revocation_cutoff=$(jq -er --arg subject "$user_subject" '
          .nats.revocations[$subject] | select(type == "number")
        ' <<<"$account_claim") || fail "NATS revocation cutoff is absent: $user_name"
        ((current_issued_at > revocation_cutoff)) && current_valid=true
      fi
    fi
    if [[ "$current_valid" == false ]]; then
      nsc -H "$nsc_home" generate creds --account APPLICATION --name "$user_name" \
        --output-file "$temporary_directory/$user_name.creds" >/dev/null 2>&1 ||
        fail "regenerate interrupted NATS credentials: $user_name"
    else
      install -m 0600 "$current_credentials" "$temporary_directory/$user_name.creds"
    fi
  done <"$policy_file"
  "$repository_root/tools/deploy/materialize-nats-operator-files.sh" \
    --nsc-home "$nsc_home" --output-directory "$material_directory/nats" >/dev/null
  install_runtime_credentials "$temporary_directory"
  printf 'changed\n'
  exit 0
fi

install -d -m 0700 "$pending_directory"
revocation_cutoff=$(($(date +%s) + 1))

while IFS='|' read -r user_name _ _ _ _; do
  readback=$(nsc -H "$nsc_home" describe user --account APPLICATION --name "$user_name" --json) ||
    fail "NATS user is absent: $user_name"
  user_subject=$(jq -er '.sub | select(test("^U[A-Z2-7]{55}$"))' <<<"$readback") ||
    fail "NATS user subject is invalid: $user_name"
  previous_credentials="$material_directory/nats/users/$user_name.creds"
  pending_credentials="$pending_directory/$user_name.previous.creds"
  if [[ ! -e "$pending_credentials" ]]; then
    [[ -s "$previous_credentials" && ! -L "$previous_credentials" ]] ||
      fail "previous NATS credentials are invalid: $user_name"
    previous_claim=$(nsc describe jwt --json --file "$previous_credentials") ||
      fail "describe previous NATS credentials: $user_name"
    jq -e --arg subject "$user_subject" '
      .sub == $subject and (.iat | type == "number") and .nats.type == "user"
    ' <<<"$previous_claim" >/dev/null ||
      fail "previous NATS credential subject mismatch: $user_name"
    install -m 0600 "$previous_credentials" "$pending_credentials"
  fi
done <"$policy_file"

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

install_runtime_credentials "$temporary_directory"
printf 'changed\n'
