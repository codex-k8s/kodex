#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Fresh install material generation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --output-directory <new-empty-directory>" \
    '  --release-registry-host <exact-dns-name>' >&2
}

output_directory=""
release_registry_host=""
while (($# > 0)); do
  case "$1" in
    --output-directory) output_directory="${2:-}"; shift 2 ;;
    --release-registry-host) release_registry_host="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$output_directory" && "$output_directory" != / && "$output_directory" != "$HOME" ]] ||
  fail 'safe output directory is required'
[[ ! -e "$output_directory" ]] || fail 'output directory already exists'
[[ "$release_registry_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$release_registry_host" == *.* ]] ||
  fail 'release registry host is invalid'
for command_name in cosign go htpasswd jq nsc openssl; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

umask 077
mkdir -p \
  "$output_directory/installation-ca" \
  "$output_directory/postgresql" \
  "$output_directory/nats/users" \
  "$output_directory/registry" \
  "$output_directory/image-registry" \
  "$output_directory/control-api" \
  "$output_directory/crypto" \
  "$output_directory/vault"

openssl req -x509 -newkey rsa:4096 -sha384 -nodes \
  -days 3650 -subj '/CN=MatterCodex installation CA' \
  -keyout "$output_directory/installation-ca/tls.key" \
  -out "$output_directory/installation-ca/tls.crt" >/dev/null 2>&1
openssl rand -base64 48 >"$output_directory/postgresql/password"
openssl rand -base64 48 >"$output_directory/postgresql/internal-rpc-authority-restore-controller-password"
printf 'mattercodex-release\n' >"$output_directory/registry/username"
openssl rand -base64 48 >"$output_directory/registry/password"
openssl rand -hex 32 >"$output_directory/control-api/session-current.hex"
openssl rand -hex 32 >"$output_directory/control-api/session-previous.hex"
openssl rand -base64 48 >"$output_directory/control-api/lease-signing.key"

create_registry_credential() {
  local name=$1 host=$2
  local username="mc-${name}"
  local directory="$output_directory/image-registry/$name"
  local auth
  mkdir -p "$directory"
  printf '%s\n' "$username" >"$directory/username"
  openssl rand -base64 48 >"$directory/password"
  htpasswd -i -B -C 12 -c "$directory/htpasswd" "$username" \
    <"$directory/password" >/dev/null 2>&1
  auth=$(printf '%s:%s' "$username" "$(<"$directory/password")" | base64 | tr -d '\n')
  jq -n --arg host "$host" --arg auth "$auth" '{auths:{($host):{auth:$auth}}}' \
    >"$directory/dockerconfig.json"
}

internal_pull_host=mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000
internal_promotion_host=mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003
for name in pull buildkit-base-pull input-read; do
  create_registry_credential "$name" "$internal_pull_host"
done
for name in staging-read evidence-probe evidence-admission evidence-promotion admin scanner signer admission promotion-staging; do
  create_registry_credential "$name" mattercodex-image-registry-staging-read.mattercodex-system.svc.cluster.local:5004
done
create_registry_credential promotion "$internal_promotion_host"
mkdir -p "$output_directory/image-registry/release-source"
install -m 0600 "$output_directory/registry/username" "$output_directory/image-registry/release-source/username"
install -m 0600 "$output_directory/registry/password" "$output_directory/image-registry/release-source/password"
release_auth=$(printf '%s:%s' "$(<"$output_directory/registry/username")" \
  "$(<"$output_directory/registry/password")" | base64 | tr -d '\n')
jq -n --arg host "$release_registry_host" --arg auth "$release_auth" \
  '{auths:{($host):{auth:$auth}}}' \
  >"$output_directory/image-registry/release-source/dockerconfig.json"

mkdir -p "$output_directory/image-registry/signing"
openssl rand -base64 48 >"$output_directory/image-registry/signing/password"
COSIGN_PASSWORD="$(<"$output_directory/image-registry/signing/password")" \
  cosign generate-key-pair --output-key-prefix "$output_directory/image-registry/signing/cosign" \
  >/dev/null

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
(
  cd -- "$repository_root/services/internal/internal-rpc-authority"
  GOWORK=off go run ./cmd/fresh-install-key-material "$output_directory/crypto"
  GOWORK=off go run ./cmd/internal-rpc-authority-bootstrap-material \
    --manifest-signer "$output_directory/crypto/publisher/manifest-signer/private.jwk" \
    --readback-signer "$output_directory/crypto/publisher/readback-signer/private.jwk" \
    --restore-signer "$output_directory/crypto/publisher/restore-signer/private.jwk" \
    --output "$output_directory/crypto/authority-bootstrap"
)

nsc_home="$output_directory/nats/nsc"
nsc -H "$nsc_home" add operator --name MATTERCODEX --sys >/dev/null 2>&1
nsc -H "$nsc_home" add account --name APPLICATION >/dev/null 2>&1
"$repository_root/tools/deploy/configure-nats-application-account.sh" \
  --nsc-home "$nsc_home" >/dev/null

add_application_user() {
  local user_name=$1
  nsc -H "$nsc_home" add user --account APPLICATION --name "$user_name" \
    --allow-pubsub 'mattercodex.>' \
    --allow-pub '$JS.API.>,_INBOX.>' \
    --allow-sub '_INBOX.>' >/dev/null 2>&1
  nsc -H "$nsc_home" generate creds --account APPLICATION --name "$user_name" \
    --output-file "$output_directory/nats/users/$user_name.creds" >/dev/null 2>&1
}

add_application_user control-plane
add_application_user control-plane-broker-bootstrap
add_application_user control-api-gateway

"$repository_root/tools/deploy/materialize-nats-operator-files.sh" \
  --nsc-home "$nsc_home" --output-directory "$output_directory/nats"

for file_path in \
  "$output_directory/installation-ca/tls.crt" \
  "$output_directory/installation-ca/tls.key" \
  "$output_directory/postgresql/password" \
  "$output_directory/postgresql/internal-rpc-authority-restore-controller-password" \
  "$output_directory/nats/operator.jwt" \
  "$output_directory/nats/system-account.jwt" \
  "$output_directory/nats/system-account.public" \
  "$output_directory/nats/account.jwt" \
  "$output_directory/nats/account.public" \
  "$output_directory/nats/users/control-plane.creds" \
  "$output_directory/nats/users/control-plane-broker-bootstrap.creds" \
  "$output_directory/nats/users/control-api-gateway.creds" \
  "$output_directory/registry/username" \
  "$output_directory/registry/password"; do
  [[ -s "$file_path" && ! -L "$file_path" ]] || fail 'generated material readback failed'
  chmod 0600 "$file_path"
done
for directory in "$output_directory"/image-registry/*; do
  [[ -d "$directory" ]] || continue
  find "$directory" -maxdepth 1 -type f -exec chmod 0600 {} +
done
for required_file in \
  "$output_directory/image-registry/pull/dockerconfig.json" \
  "$output_directory/image-registry/promotion/dockerconfig.json" \
  "$output_directory/image-registry/release-source/dockerconfig.json" \
  "$output_directory/image-registry/signing/cosign.key" \
  "$output_directory/image-registry/signing/cosign.pub" \
  "$output_directory/image-registry/signing/password"; do
  [[ -s "$required_file" && ! -L "$required_file" ]] || fail 'generated image registry material readback failed'
done
for file_path in \
  "$output_directory/control-api/session-current.hex" \
  "$output_directory/control-api/session-previous.hex" \
  "$output_directory/control-api/lease-signing.key"; do
  [[ -s "$file_path" && ! -L "$file_path" ]] || fail 'generated control API material readback failed'
  chmod 0600 "$file_path"
done

printf 'Fresh install material generated: %s\n' "$output_directory"
