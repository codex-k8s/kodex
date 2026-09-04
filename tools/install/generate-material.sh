#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex installation material generation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --output-directory <new-directory>" \
    '  --release-registry-host <dns> --promoted-pull-host <dns>' \
    '  [--release-registry-username-file <path> --release-registry-password-file <path>]' >&2
}

output_directory=""
release_registry_host=""
promoted_pull_host=""
release_registry_username_file=""
release_registry_password_file=""
while (($# > 0)); do
  case "$1" in
    --output-directory) output_directory="${2:-}"; shift 2 ;;
    --release-registry-host) release_registry_host="${2:-}"; shift 2 ;;
    --promoted-pull-host) promoted_pull_host="${2:-}"; shift 2 ;;
    --release-registry-username-file) release_registry_username_file="${2:-}"; shift 2 ;;
    --release-registry-password-file) release_registry_password_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

valid_dns_name() {
  [[ "$1" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$1" == *.* ]]
}

[[ -n "$output_directory" && "$output_directory" != / && "$output_directory" != "$HOME" ]] ||
  fail 'safe output directory is required'
[[ ! -e "$output_directory" ]] || fail 'output directory already exists'
valid_dns_name "$release_registry_host" || fail 'release registry host is invalid'
valid_dns_name "$promoted_pull_host" || fail 'promoted pull host is invalid'
[[ "$release_registry_host" != "$promoted_pull_host" ]] ||
  fail 'release registry and promoted pull hosts must differ'
if [[ -n "$release_registry_username_file" || -n "$release_registry_password_file" ]]; then
  [[ -f "$release_registry_username_file" && -s "$release_registry_username_file" &&
    ! -L "$release_registry_username_file" ]] || fail 'release registry username file is invalid'
  [[ -f "$release_registry_password_file" && -s "$release_registry_password_file" &&
    ! -L "$release_registry_password_file" ]] || fail 'release registry password file is invalid'
fi
for command_name in base64 cosign go htpasswd jq nsc openssl sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
registry_file="$repository_root/tools/install/secret-projections.json"
nats_user_policy_file="$repository_root/tools/install/nats-runtime-users.tsv"
jq -e '
  .version == 1 and .namespace == "kodex-system" and (.secrets | length > 0) and
  ([.secrets[].name] | length == (unique | length)) and
  all(.secrets[];
    (.name | type == "string" and test("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$")) and
    ((.dynamic // false) | type == "boolean") and
    (.items | type == "array" and length > 0) and
    ([.items[].key] | length == (unique | length)) and
    all(.items[];
      (.key | type == "string" and test("^[A-Za-z0-9._-]+$")) and
      ((.required // true) | type == "boolean") and
      (.source | type == "object") and (.source.type | type == "string" and length > 0)))
' "$registry_file" >/dev/null || fail 'secret projection registry is invalid'

umask 077
mkdir -p \
  "$output_directory/authorities" \
  "$output_directory/certificates" \
  "$output_directory/control-api" \
  "$output_directory/crypto" \
  "$output_directory/database" \
  "$output_directory/material" \
  "$output_directory/nats/users" \
  "$output_directory/postgresql/roles" \
  "$output_directory/projections" \
  "$output_directory/registry"

create_authority() {
  local name=$1 directory="$output_directory/authorities/$1"
  mkdir -p "$directory"
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 \
    -out "$directory/ca.key" >/dev/null 2>&1
  openssl req -x509 -new -sha256 -key "$directory/ca.key" -days 3650 \
    -subj "/CN=Kodex $name installation CA" \
    -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -addext 'subjectKeyIdentifier=hash' \
    -out "$directory/ca.crt" >/dev/null 2>&1
}

for authority in pki pki-buildkit-push pki-node-pull pki-public; do
  create_authority "$authority"
done

issue_certificate() {
  local source_json=$1 authority profile common_name alt_names uri_sans cache_key directory ext_file
  local subject_alt_name=""
  authority=$(jq -er '.authority' <<<"$source_json")
  profile=$(jq -er '.profile' <<<"$source_json")
  common_name=$(jq -er '.arguments.common_name' <<<"$source_json")
  alt_names=$(jq -r '.arguments.alt_names // ""' <<<"$source_json")
  uri_sans=$(jq -r '.arguments.uri_sans // ""' <<<"$source_json")
  common_name=${common_name//registry-pull.invalid/$promoted_pull_host}
  alt_names=${alt_names//registry-pull.invalid/$promoted_pull_host}
  cache_key=$(printf '%s\0%s\0%s\0%s\0%s' "$authority" "$profile" "$common_name" "$alt_names" "$uri_sans" |
    sha256sum | awk '{print $1}')
  directory="$output_directory/certificates/$cache_key"
  [[ -s "$directory/tls.crt" && -s "$directory/tls.key" ]] && {
    printf '%s' "$directory"
    return
  }
  mkdir -p "$directory"
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 \
    -out "$directory/tls.key" >/dev/null 2>&1
  openssl req -new -sha256 -key "$directory/tls.key" -subj "/CN=$common_name" \
    -out "$directory/request.csr" >/dev/null 2>&1
  ext_file="$directory/extensions.cnf"
  {
    printf '%s\n' \
      'basicConstraints=critical,CA:FALSE' \
      'keyUsage=critical,digitalSignature,keyEncipherment' \
      'extendedKeyUsage=serverAuth,clientAuth' \
      'subjectKeyIdentifier=hash' \
      'authorityKeyIdentifier=keyid,issuer'
    if [[ -n "$alt_names" ]]; then
      subject_alt_name=$(awk -v list="$alt_names" 'BEGIN {
        count=split(list, names, ",");
        for (i=1; i<=count; i++) printf "%sDNS:%s", i == 1 ? "" : ",", names[i]
      }')
    elif [[ "$common_name" == *.* ]]; then
      subject_alt_name="DNS:$common_name"
    fi
    if [[ -n "$uri_sans" ]]; then
      while IFS= read -r uri; do
        [[ -n "$uri" ]] || continue
        subject_alt_name+="${subject_alt_name:+,}URI:$uri"
      done < <(tr ',' '\n' <<<"$uri_sans")
    fi
    if [[ -n "$subject_alt_name" ]]; then
      printf 'subjectAltName=%s\n' "$subject_alt_name"
    fi
  } >"$ext_file"
  openssl x509 -req -sha256 -days 825 \
    -in "$directory/request.csr" \
    -CA "$output_directory/authorities/$authority/ca.crt" \
    -CAkey "$output_directory/authorities/$authority/ca.key" \
    -CAcreateserial -extfile "$ext_file" -out "$directory/tls.crt" >/dev/null 2>&1
  rm -f -- "$directory/request.csr" "$directory/extensions.cnf"
  printf '%s' "$directory"
}

put_material() {
  local ref=$1 field=$2 source=$3 destination="$output_directory/material/$1/$2"
  [[ -s "$source" && ! -L "$source" ]] || fail "material source is invalid: $ref/$field"
  mkdir -p "$(dirname -- "$destination")"
  install -m 0600 "$source" "$destination"
}

write_value() {
  local path=$1 value=$2
  mkdir -p "$(dirname -- "$path")"
  printf '%s' "$value" >"$path"
  chmod 0600 "$path"
}

postgresql_bootstrap_password="$output_directory/postgresql/bootstrap-password"
openssl rand -hex 32 >"$postgresql_bootstrap_password"
for role in \
  control_plane_migrator control_plane_runtime_g1 artifact_retention_runtime_g1 internal_rpc_authority_migrator \
  kodex_backup_reader \
  ira_restore_controller_g1 ira_publisher_g4 ira_readback_attestor_g4 \
  ira_role_image_builder_issuer_g1 ira_image_admission_issuer_g1 \
  ira_image_promotion_issuer_g1 ira_automation_scheduler_issuer_g1 \
  ira_session_archive_issuer_g1 ira_secret_broker_issuer_g1 \
  ira_control_api_gateway_issuer_g1 ira_control_plane_issuer_g1 ira_control_plane_verifier_g1 \
  ira_control_plane_resolver_g1 ira_integration_gateway_issuer_g1 \
  ira_interaction_gateway_issuer_g1 ira_runtime_controller_issuer_g1 \
  ira_secret_broker_verifier_g1 ira_stt_tts_service_issuer_g1 ira_stt_tts_service_verifier_g1; do
  openssl rand -hex 32 >"$output_directory/postgresql/roles/$role"
done

create_database_material() {
  local role=$1 database=$2 host=$3 ca_path=$4 password dsn
  password=$(<"$output_directory/postgresql/roles/$role")
  dsn="postgresql://$role:$password@$host:5432/$database?sslmode=verify-full&sslrootcert=$ca_path"
  write_value "$output_directory/database/$role/username" "$role"
  write_value "$output_directory/database/$role/password" "$password"
  write_value "$output_directory/database/$role/dsn" "$dsn"
}

create_database_material control_plane_migrator control_plane \
  control-plane-postgresql-rw.kodex-system.svc.cluster.local \
  /var/run/config/kodex/control-plane/postgres/ca.pem
create_database_material control_plane_runtime_g1 control_plane \
  control-plane-postgresql-rw.kodex-system.svc.cluster.local \
  /var/run/config/kodex/control-plane/postgres/ca.pem
create_database_material artifact_retention_runtime_g1 control_plane \
  control-plane-postgresql-rw.kodex-system.svc.cluster.local \
  /var/run/config/kodex/artifact-retention/postgres/ca.pem
create_database_material internal_rpc_authority_migrator internal_rpc_authority \
  internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local \
  /var/run/config/kodex/internal-rpc-authority/postgresql/ca.pem
while IFS= read -r role; do
  create_database_material "$role" internal_rpc_authority \
    internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local \
    /var/run/config/kodex/internal-rpc-authority/postgresql/ca.pem
done < <(jq -r '[.secrets[].items[].source | select(.type == "database") | .ref] | unique[]' "$registry_file")

put_material kodex/control-plane/postgres-migration dsn \
  "$output_directory/database/control_plane_migrator/dsn"
put_material kodex/control-plane/postgres-runtime dsn \
  "$output_directory/database/control_plane_runtime_g1/dsn"
put_material kodex/artifact-retention/postgres-runtime dsn \
  "$output_directory/database/artifact_retention_runtime_g1/dsn"
put_material internal-rpc-authority/postgres-migration dsn \
  "$output_directory/database/internal_rpc_authority_migrator/dsn"

openssl rand -hex 32 >"$output_directory/control-api/session-current.hex"
openssl rand -hex 32 >"$output_directory/control-api/session-previous.hex"
openssl rand -base64 48 | tr -d '\n' >"$output_directory/control-api/lease-signing.key"
put_material kodex/control-api-gateway/session current.hex \
  "$output_directory/control-api/session-current.hex"
put_material kodex/control-api-gateway/session previous.hex \
  "$output_directory/control-api/session-previous.hex"
put_material kodex/control-plane/lease-signing key \
  "$output_directory/control-api/lease-signing.key"

control_api_tls_source=$(jq -cn '{
  authority:"pki", profile:"kodex-control-api-gateway",
  arguments:{
    common_name:"control-api.kodex.local",
    alt_names:"control-api.kodex.local,control-api-gateway,control-api-gateway.kodex-system.svc,control-api-gateway.kodex-system.svc.cluster.local",
    ttl:"720h"
  }
}')
control_api_tls_directory=$(issue_certificate "$control_api_tls_source")
control_api_certificate_sha256=$(sha256sum "$control_api_tls_directory/tls.crt" | awk '{print $1}')
jq -cn --arg certificate_sha256 "$control_api_certificate_sha256" '{
  generation:1,
  certificateSha256:$certificate_sha256,
  predecessorGeneration:0,
  predecessorCertificateSha256:("0" * 64)
}' >"$output_directory/control-api/public-tls-material.json"
put_material kodex/control-api-gateway/public-tls-material material.json \
  "$output_directory/control-api/public-tls-material.json"

runtime_execution_tls_source=$(jq -cn '{
  authority:"pki", profile:"kodex-runtime-execution-client",
  arguments:{
    common_name:"agent-runner.kodex-runtime.svc.cluster.local",
    alt_names:"agent-runner,agent-runner.kodex-runtime.svc,agent-runner.kodex-runtime.svc.cluster.local",
    uri_sans:"spiffe://kodex.local/ns/kodex-runtime/sa/agent-runner",
    ttl:"720h"
  }
}')
runtime_execution_tls_directory=$(issue_certificate "$runtime_execution_tls_source")
put_material kodex/runtime-execution-client/tls tls.crt \
  "$runtime_execution_tls_directory/tls.crt"
put_material kodex/runtime-execution-client/tls tls.key \
  "$runtime_execution_tls_directory/tls.key"
put_material kodex/runtime-execution-client/tls ca.crt \
  "$output_directory/authorities/pki/ca.crt"

create_registry_credential() {
  local name=$1 host=$2 username_file=${3:-} password_file=${4:-}
  local directory="$output_directory/registry/$1" username password auth
  mkdir -p "$directory"
  if [[ -n "$username_file" ]]; then
    username=$(<"$username_file")
    password=$(<"$password_file")
    [[ "$username" =~ ^[A-Za-z0-9._-]{3,64}$ ]] ||
      fail 'release registry username is invalid'
    [[ ${#password} -ge 20 && ${#password} -le 256 && "$password" != *$'\n'* &&
      "$password" != *$'\r'* ]] || fail 'release registry password is invalid'
    write_value "$directory/username" "$username"
    write_value "$directory/password" "$password"
  else
    username="kodex-${name}"
    write_value "$directory/username" "$username"
    openssl rand -base64 48 | tr -d '\n' >"$directory/password"
  fi
  password=$(<"$directory/password")
  htpasswd -i -B -C 12 -c "$directory/htpasswd" "$username" \
    <"$directory/password" >/dev/null 2>&1
  auth=$(printf '%s:%s' "$username" "$password" | base64 | tr -d '\n')
  jq -n --arg host "$host" --arg auth "$auth" '{auths:{($host):{auth:$auth}}}' \
    >"$directory/dockerconfig.json"
  chmod 0600 "$directory"/*
}

internal_pull_host=kodex-image-registry.kodex-system.svc.cluster.local:5000
internal_promotion_host=kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003
internal_staging_host=kodex-image-registry-staging-read.kodex-system.svc.cluster.local:5004
for name in pull buildkit-base-pull input-read; do
  create_registry_credential "$name" "$internal_pull_host"
done
# kubelet обращается к promoted registry по публичному имени, а внутренние
# readiness-проверки используют Service DNS. Оба endpoint принадлежат одной
# pull identity и должны присутствовать в одном Docker config.
pull_auth=$(jq -er --arg host "$internal_pull_host" '.auths[$host].auth' \
  "$output_directory/registry/pull/dockerconfig.json")
jq --arg host "$promoted_pull_host" --arg auth "$pull_auth" \
  '.auths[$host] = {auth:$auth}' \
  "$output_directory/registry/pull/dockerconfig.json" \
  >"$output_directory/registry/pull/dockerconfig.json.next"
mv -- "$output_directory/registry/pull/dockerconfig.json.next" \
  "$output_directory/registry/pull/dockerconfig.json"
chmod 0600 "$output_directory/registry/pull/dockerconfig.json"
for name in staging-read evidence-probe evidence-admission evidence-promotion admin scanner signer admission promotion-staging; do
  create_registry_credential "$name" "$internal_staging_host"
done
create_registry_credential promotion "$internal_promotion_host"
create_registry_credential release-source "$release_registry_host" \
  "$release_registry_username_file" "$release_registry_password_file"

# Staging-read registry принимает разные application identities. Общий ACL
# содержит только их bcrypt-записи и не раздаёт потребителям общий пароль.
staging_read_acl_directory="$output_directory/registry/staging-read-authorized"
mkdir -p "$staging_read_acl_directory"
: >"$staging_read_acl_directory/htpasswd"
for name in staging-read scanner signer admission promotion-staging; do
  cat -- "$output_directory/registry/$name/htpasswd" >>"$staging_read_acl_directory/htpasswd"
done
chmod 0600 "$staging_read_acl_directory/htpasswd"

for name in pull buildkit-base-pull staging-read evidence-probe evidence-admission evidence-promotion admin scanner signer admission promotion-staging promotion; do
  for field in username password; do
    put_material "kodex/image-registry/$name" "$field" "$output_directory/registry/$name/$field"
  done
  put_material "kodex/image-registry/$name" htpasswd "$output_directory/registry/$name/htpasswd"
  put_material "kodex/image-registry/$name" dockerconfigjson "$output_directory/registry/$name/dockerconfig.json"
done
put_material kodex/image-registry/staging-read-authorized htpasswd \
  "$staging_read_acl_directory/htpasswd"
put_material kodex/role-image-builder/input-read docker-config \
  "$output_directory/registry/input-read/dockerconfig.json"
put_material kodex/release-registry/pull dockerconfigjson \
  "$output_directory/registry/release-source/dockerconfig.json"

mkdir -p "$output_directory/registry/signing"
openssl rand -base64 48 | tr -d '\n' >"$output_directory/registry/signing/password"
COSIGN_PASSWORD="$(<"$output_directory/registry/signing/password")" \
  cosign generate-key-pair --output-key-prefix "$output_directory/registry/signing/cosign" >/dev/null
put_material kodex/image-admission/signing password "$output_directory/registry/signing/password"
put_material kodex/image-admission/signing private_key "$output_directory/registry/signing/cosign.key"
put_material kodex/image-admission/signing public_key "$output_directory/registry/signing/cosign.pub"

(
  cd -- "$repository_root/services/internal/internal-rpc-authority"
  GOWORK=off go run ./cmd/fresh-install-key-material "$output_directory/crypto"
  GOWORK=off go run ./cmd/internal-rpc-authority-bootstrap-material \
    --manifest-signer "$output_directory/crypto/publisher/manifest-signer/private.jwk" \
    --readback-signer "$output_directory/crypto/publisher/readback-signer/private.jwk" \
    --restore-signer "$output_directory/crypto/publisher/restore-signer/private.jwk" \
    --output "$output_directory/crypto/authority-bootstrap"
)

for worker in automation-scheduler session-archive integration-gateway interaction-gateway runtime-controller role-image-builder image-admission image-promotion secret-broker control-plane; do
  put_material "kodex/platform-worker-grants/$worker" private.jwk \
    "$output_directory/crypto/platform-worker/$worker/private.jwk"
  put_material "kodex/platform-worker-grants/$worker" public-jwk \
    "$output_directory/crypto/platform-worker/$worker/public.jwk"
done
put_material internal-rpc-authority/publisher/manifest-signer private.jwk \
  "$output_directory/crypto/publisher/manifest-signer/private.jwk"
put_material internal-rpc-authority/publisher/readback-signer private.jwk \
  "$output_directory/crypto/publisher/readback-signer/private.jwk"
put_material internal-rpc-authority/publisher/restore-signer private.jwk \
  "$output_directory/crypto/publisher/restore-signer/private.jwk"
put_material internal-rpc-authority/restore/pitr-evidence private.jwk \
  "$output_directory/crypto/restore/pitr-evidence/private.jwk"
put_material internal-rpc-authority/restore/pitr-evidence public.jwk \
  "$output_directory/crypto/restore/pitr-evidence/public.jwk"
put_material internal-rpc-authority/publisher/manifest-trust manifest-trust.jws \
  "$output_directory/crypto/authority-bootstrap/external/publisher-manifest-trust.jws"
put_material internal-rpc-authority/readback/trust manifest-root.jws \
  "$output_directory/crypto/authority-bootstrap/external/readback-manifest-root.jws"
put_material internal-rpc-authority/readback/trust credential-trust.jws \
  "$output_directory/crypto/authority-bootstrap/external/readback-credential-trust.jws"
put_material internal-rpc-authority/restore/trust manifest-trust.jws \
  "$output_directory/crypto/authority-bootstrap/external/publisher-manifest-trust.jws"
put_material internal-rpc-authority/restore/trust restore-role-trust.jws \
  "$output_directory/crypto/authority-bootstrap/external/restore-role-trust.jws"

nsc_home="$output_directory/nats/nsc"
nsc -H "$nsc_home" add operator --name KODEX --sys >/dev/null 2>&1
nsc -H "$nsc_home" add account --name APPLICATION >/dev/null 2>&1
"$repository_root/tools/deploy/configure-nats-application-account.sh" --nsc-home "$nsc_home" >/dev/null

generate_nats_user() {
  local user_name=$1
  local publish_allow=$2
  local subscribe_allow=$3
  local readback
  nsc -H "$nsc_home" add user --account APPLICATION --name "$user_name" \
    --allow-pub "$publish_allow" --allow-sub "$subscribe_allow" >/dev/null 2>&1
  readback=$(nsc -H "$nsc_home" describe user --account APPLICATION --name "$user_name" --json)
  jq -e --arg name "$user_name" --arg publish "$publish_allow" --arg subscribe "$subscribe_allow" '
    .name == $name and .nats.type == "user" and
    ((.nats.pub.allow // []) | sort) == ($publish | split(",") | sort) and
    ((.nats.sub.allow // []) | sort) == ($subscribe | split(",") | sort) and
    ((.nats.pub.deny // []) | length) == 0 and
    ((.nats.sub.deny // []) | length) == 0 and (.nats.resp // null) == null
  ' <<<"$readback" >/dev/null || fail "NATS user permission readback mismatch: $user_name"
  nsc -H "$nsc_home" generate creds --account APPLICATION --name "$user_name" \
    --output-file "$output_directory/nats/users/$user_name.creds" >/dev/null 2>&1
}

while IFS='|' read -r user_name publish_allow subscribe_allow _ _; do
  generate_nats_user "$user_name" "$publish_allow" "$subscribe_allow"
done <"$nats_user_policy_file"
"$repository_root/tools/deploy/materialize-nats-operator-files.sh" \
  --nsc-home "$nsc_home" --output-directory "$output_directory/nats"
while IFS='|' read -r user_name _ _ material_ref _; do
  put_material "$material_ref" credentials "$output_directory/nats/users/$user_name.creds"
done <"$nats_user_policy_file"

# Bare-metal k3s consumes this installation-scoped node identity directly.
node_source=$(jq -cn --arg host "$promoted_pull_host" '{
  authority:"pki-node-pull", profile:"kodex-node-pull-installer",
  arguments:{common_name:"kodex-node-pull-installer",alt_names:$host}}')
node_directory=$(issue_certificate "$node_source")
mkdir -p "$output_directory/node-pull"
install -m 0600 "$node_directory/tls.crt" "$output_directory/node-pull/client.crt"
install -m 0600 "$node_directory/tls.key" "$output_directory/node-pull/client.key"
install -m 0600 "$output_directory/authorities/pki-public/ca.crt" "$output_directory/node-pull/ca.crt"
install -m 0600 "$output_directory/registry/pull/username" "$output_directory/node-pull/username"
install -m 0600 "$output_directory/registry/pull/password" "$output_directory/node-pull/password"

while IFS= read -r encoded; do
  item=$(base64 --decode <<<"$encoded")
  secret_name=$(jq -er '.secret' <<<"$item")
  key=$(jq -er '.key' <<<"$item")
  source=$(jq -c '.source' <<<"$item")
  source_type=$(jq -er '.type' <<<"$source")
  destination="$output_directory/projections/$secret_name/$key"
  mkdir -p "$(dirname -- "$destination")"
  case "$source_type" in
    material)
      ref=$(jq -er '.ref' <<<"$source")
      field=$(jq -er '.field' <<<"$source")
      source_file="$output_directory/material/$ref/$field"
      ;;
    database)
      ref=$(jq -er '.ref' <<<"$source")
      field=$(jq -er '.field' <<<"$source")
      source_file="$output_directory/database/$ref/$field"
      ;;
    certificate)
      operation=$(jq -er '.operation' <<<"$source")
      authority=$(jq -er '.authority' <<<"$source")
      field=$(jq -er '.field' <<<"$source")
      if [[ "$operation" == cert && "$field" == certificate ]]; then
        source_file="$output_directory/authorities/$authority/ca.crt"
      elif [[ "$operation" == issue ]]; then
        certificate_directory=$(issue_certificate "$source")
        case "$field" in
          certificate) source_file="$certificate_directory/tls.crt" ;;
          private_key) source_file="$certificate_directory/tls.key" ;;
          *) fail "unsupported certificate field: $field" ;;
        esac
      else
        fail "unsupported certificate operation: $operation/$field"
      fi
      ;;
    *) fail "unsupported secret source type: $source_type" ;;
  esac
  [[ -s "$source_file" && ! -L "$source_file" ]] ||
    fail "projection source is absent: $secret_name/$key"
  install -m 0600 "$source_file" "$destination"
done < <(jq -r '
  .secrets[] | select(.dynamic != true) as $secret |
  $secret.items[] | {secret:$secret.name,key:.key,source:.source} | @base64
' "$registry_file")

find "$output_directory" -type f -exec chmod 0600 {} +
find "$output_directory/projections" -type f -exec test -s {} \; ||
  fail 'generated secret projection is incomplete'
find "$output_directory/projections" -type f -print0 | sort -z | xargs -0 sha256sum \
  >"$output_directory/projections.sha256"
chmod 0600 "$output_directory/projections.sha256"
printf 'Kodex installation material generated: %s\n' "$output_directory"
