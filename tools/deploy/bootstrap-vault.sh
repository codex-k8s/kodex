#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Vault bootstrap failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "Usage: $0 --context <exact-context> --mode initialize|configure-core|configure-policies|configure-database|configure-database-runtime|configure-image-pki|readback" \
    '  --material-directory <path> [--render <release.yaml>]' >&2
}

expected_context=""
mode=""
material_directory=""
render_file=""
while (($# > 0)); do
  case "$1" in
    --context) expected_context="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --material-directory) material_directory="${2:-}"; shift 2 ;;
    --render) render_file="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$expected_context" && -d "$material_directory" && ! -L "$material_directory" ]] ||
  fail 'exact context and material directory are required'
case "$mode" in
  initialize|configure-core|configure-policies|configure-database|configure-database-runtime|configure-image-pki|readback) ;;
  *) fail 'mode is invalid' ;;
esac
for command_name in awk base64 jq kubectl openssl rg sha256sum sleep sort stat yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
[[ "$(kubectl config current-context)" == "$expected_context" ]] || fail 'current Kubernetes context mismatch'
kubectl -n mattercodex-system get pod vault-0 >/dev/null 2>&1 || fail 'Vault Pod is absent'

vault_directory="$material_directory/vault"
root_token_file="$vault_directory/root-token"
unseal_key_file="$vault_directory/unseal-key"
mkdir -p "$vault_directory"
chmod 0700 "$vault_directory"

vault_status() {
  kubectl -n mattercodex-system exec vault-0 -- sh -ec '
    export VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
    export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
    status_code=0
    status_json=$(vault status -format=json) || status_code=$?
    case "$status_code" in
      0|2) printf "%s\n" "$status_json" ;;
      *) exit "$status_code" ;;
    esac
  '
}

read_vault_boolean() {
  local field=$1
  jq -r --arg field "$field" '
    .[$field] as $value |
    if ($value | type) == "boolean" then
      $value | tostring
    else
      error("Vault status field is not boolean: " + $field)
    end
  '
}

require_root_material() {
  local material_file_path
  for material_file_path in "$root_token_file" "$unseal_key_file"; do
    [[ -f "$material_file_path" && -s "$material_file_path" && ! -L "$material_file_path" ]] ||
      fail 'Vault owner material is absent'
    [[ $(stat -c '%a' "$material_file_path") == 600 ]] || fail 'Vault owner material mode is unsafe'
  done
}

read_single_line_secret() {
  local file_path=$1 label=$2 value
  [[ -f "$file_path" && -s "$file_path" && ! -L "$file_path" ]] || fail "$label is absent"
  value=$(<"$file_path")
  [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] ||
    fail "$label must be a non-empty single line"
  printf '%s' "$value"
}

vault_cli() {
  local root_token
  require_root_material
  root_token=$(read_single_line_secret "$root_token_file" 'Vault root token')
  printf '%s\n' "$root_token" |
    kubectl -n mattercodex-system exec -i vault-0 -- sh -ec '
      IFS= read -r VAULT_TOKEN
      export VAULT_TOKEN VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
      export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
      exec vault "$@"
    ' sh "$@"
}

vault_input() {
  local input_file=$1 root_token
  shift
  require_root_material
  root_token=$(read_single_line_secret "$root_token_file" 'Vault root token')
  { printf '%s\n' "$root_token"; cat "$input_file"; } |
    kubectl -n mattercodex-system exec -i vault-0 -- sh -ec '
      IFS= read -r VAULT_TOKEN
      export VAULT_TOKEN VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
      export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
      exec vault "$@"
    ' sh "$@"
}

vault_kv_put_file() {
  local path=$1 key=$2 seed_file_path=$3 root_token
  [[ "$path" =~ ^[a-zA-Z0-9][a-zA-Z0-9._/-]+$ && "$path" != kv/* ]] ||
    fail 'Vault KV seed path must be relative to the canonical mount'
  [[ -f "$seed_file_path" && -s "$seed_file_path" && ! -L "$seed_file_path" ]] ||
    fail 'Vault seed file is invalid'
  require_root_material
  root_token=$(read_single_line_secret "$root_token_file" 'Vault root token')
  { printf '%s\n' "$root_token"; cat "$seed_file_path"; } |
    kubectl -n mattercodex-system exec -i vault-0 -- sh -ec '
      IFS= read -r VAULT_TOKEN
      export VAULT_TOKEN VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
      export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
      exec vault kv put -mount=kv "$1" "$2"=-
    ' sh "$path" "$key" >/dev/null
}

vault_kv_patch_file() {
  local path=$1 key=$2 seed_file_path=$3 root_token
  [[ "$path" =~ ^[a-zA-Z0-9][a-zA-Z0-9._/-]+$ && "$path" != kv/* ]] ||
    fail 'Vault KV seed path must be relative to the canonical mount'
  [[ -f "$seed_file_path" && -s "$seed_file_path" && ! -L "$seed_file_path" ]] ||
    fail 'Vault seed file is invalid'
  require_root_material
  root_token=$(read_single_line_secret "$root_token_file" 'Vault root token')
  { printf '%s\n' "$root_token"; cat "$seed_file_path"; } |
    kubectl -n mattercodex-system exec -i vault-0 -- sh -ec '
      IFS= read -r VAULT_TOKEN
      export VAULT_TOKEN VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
      export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
      exec vault kv patch -mount=kv "$1" "$2"=-
    ' sh "$path" "$key" >/dev/null
}

if [[ "$mode" == initialize ]]; then
  status=$(vault_status || true)
  initialized=$(read_vault_boolean initialized <<<"$status")
  if [[ "$initialized" == false ]]; then
    umask 077
    init_file="$vault_directory/init.json"
    kubectl -n mattercodex-system exec vault-0 -- sh -ec '
      export VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
      export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
      vault operator init -format=json -key-shares=1 -key-threshold=1
    ' >"$init_file"
    jq -er '.root_token' "$init_file" >"$root_token_file"
    jq -er '.unseal_keys_b64[0]' "$init_file" >"$unseal_key_file"
    chmod 0600 "$root_token_file" "$unseal_key_file" "$init_file"
  fi
  require_root_material
  sealed=$(vault_status | read_vault_boolean sealed)
  if [[ "$sealed" == true ]]; then
    kubectl -n mattercodex-system exec -i vault-0 -- sh -ec '
      export VAULT_ADDR=https://vault.mattercodex-system.svc.cluster.local:8200
      export VAULT_CACERT=/vault/userconfig/vault-server-tls/ca.crt
      vault write -format=json sys/unseal key=-
    ' <"$unseal_key_file" >/dev/null
  fi
  [[ $(vault_status | jq -er '.initialized and (.sealed | not)') == true ]] || fail 'Vault initialize readback failed'
fi

if [[ "$mode" == configure-core ]]; then
  require_root_material
  mounts=$(vault_cli secrets list -format=json)
  jq -e 'has("kv/")' <<<"$mounts" >/dev/null || vault_cli secrets enable -path=kv -version=2 kv >/dev/null
  jq -e 'has("secret/")' <<<"$mounts" >/dev/null || vault_cli secrets enable -path=secret -version=2 kv >/dev/null
  jq -e 'has("database/")' <<<"$mounts" >/dev/null || vault_cli secrets enable database >/dev/null
  jq -e 'has("pki/")' <<<"$mounts" >/dev/null || vault_cli secrets enable -path=pki pki >/dev/null
  jq -e 'has("pki-public/")' <<<"$mounts" >/dev/null || vault_cli secrets enable -path=pki-public pki >/dev/null
  vault_cli secrets tune -max-lease-ttl=87600h pki >/dev/null
  vault_cli secrets tune -max-lease-ttl=87600h pki-public >/dev/null
  if ! vault_cli read -format=json pki/cert/ca >/dev/null 2>&1; then
    vault_cli write pki/root/generate/internal common_name=mattercodex-internal-pki ttl=87600h \
      key_type=rsa key_bits=4096 >/dev/null
  fi
  if ! vault_cli read -format=json pki-public/cert/ca >/dev/null 2>&1; then
    vault_cli write pki-public/root/generate/internal common_name=mattercodex-public-registry-pki ttl=87600h \
      key_type=rsa key_bits=4096 >/dev/null
  fi
  auths=$(vault_cli auth list -format=json)
  jq -e 'has("kubernetes/")' <<<"$auths" >/dev/null || vault_cli auth enable kubernetes >/dev/null
  vault_cli write auth/kubernetes/config \
    kubernetes_host=https://kubernetes.default.svc:443 \
    token_reviewer_jwt=@/var/run/secrets/kubernetes.io/serviceaccount/token \
    kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    issuer=https://kubernetes.default.svc.cluster.local >/dev/null
  if ! vault_cli audit list -format=json | jq -e 'has("file/")' >/dev/null; then
    vault_cli audit enable file file_path=/vault/audit/audit.log >/dev/null
  fi

  vault_kv_put_file mattercodex/control-plane/nats credentials "$material_directory/nats/users/control-plane.creds"
  vault_kv_put_file mattercodex/control-plane/nats-bootstrap credentials "$material_directory/nats/users/control-plane-broker-bootstrap.creds"
  vault_kv_put_file mattercodex/control-api-gateway/nats credentials "$material_directory/nats/users/control-api-gateway.creds"
  vault_kv_put_file mattercodex/control-plane/lease-signing key "$material_directory/control-api/lease-signing.key"
  vault_kv_put_file mattercodex/control-api-gateway/session current-hex "$material_directory/control-api/session-current.hex"
  vault_kv_patch_file mattercodex/control-api-gateway/session previous-hex "$material_directory/control-api/session-previous.hex"

  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  for key_name in tls.crt tls.key ca.crt; do
    kubectl -n mattercodex-system get secret mattercodex-control-api-bootstrap-tls \
      -o "jsonpath={.data['${key_name//./\\.}']}" | base64 -d >"$temporary_directory/$key_name"
  done
  printf '1\n' >"$temporary_directory/generation"
  printf '0\n' >"$temporary_directory/predecessor-generation"
  sha256sum "$temporary_directory/tls.crt" | awk '{print $1}' >"$temporary_directory/certificate-sha256"
  printf '%064d\n' 0 >"$temporary_directory/predecessor-certificate-sha256"
  vault_kv_put_file mattercodex/control-api-gateway/public-tls-material tls-crt "$temporary_directory/tls.crt"
  for entry in \
    "tls-key:$temporary_directory/tls.key" \
    "ca-crt:$temporary_directory/ca.crt" \
    "generation:$temporary_directory/generation" \
    "certificate-sha256:$temporary_directory/certificate-sha256" \
    "predecessor-generation:$temporary_directory/predecessor-generation" \
    "predecessor-certificate-sha256:$temporary_directory/predecessor-certificate-sha256"; do
    vault_kv_patch_file mattercodex/control-api-gateway/public-tls-material "${entry%%:*}" "${entry#*:}"
  done

  for worker in automation-scheduler integration-gateway runtime-controller role-image-builder image-admission image-promotion; do
    vault_kv_put_file "mattercodex/platform-worker-grants/$worker" private.jwk \
      "$material_directory/crypto/platform-worker/$worker/private.jwk"
    vault_kv_patch_file "mattercodex/platform-worker-grants/$worker" public-jwk \
      "$material_directory/crypto/platform-worker/$worker/public.jwk"
  done
  vault_kv_put_file internal-rpc-authority/publisher/restore-signer private.jwk \
    "$material_directory/crypto/publisher/restore-signer/private.jwk"
  vault_kv_put_file internal-rpc-authority/publisher/readback-signer private.jwk \
    "$material_directory/crypto/publisher/readback-signer/private.jwk"
  vault_kv_put_file internal-rpc-authority/publisher/manifest-signer private.jwk \
    "$material_directory/crypto/publisher/manifest-signer/private.jwk"
  vault_kv_put_file internal-rpc-authority/publisher/manifest-trust manifest-trust.jws \
    "$material_directory/crypto/authority-bootstrap/external/publisher-manifest-trust.jws"
  vault_kv_put_file internal-rpc-authority/readback/trust manifest-root.jws \
    "$material_directory/crypto/authority-bootstrap/external/readback-manifest-root.jws"
  vault_kv_patch_file internal-rpc-authority/readback/trust credential-trust.jws \
    "$material_directory/crypto/authority-bootstrap/external/readback-credential-trust.jws"
  vault_kv_put_file internal-rpc-authority/restore/pitr-evidence private.jwk \
    "$material_directory/crypto/restore/pitr-evidence/private.jwk"
fi

if [[ "$mode" == configure-policies ]]; then
  [[ -f "$render_file" && -s "$render_file" && ! -L "$render_file" ]] || fail 'release render is required'
  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  yq -o=json -I=0 '.' "$render_file" | jq -s '.' >"$temporary_directory/render.json"
  yq -o=json -I=0 'select(.kind == "SecretProviderClass") | .metadata.name as $spc | .spec.parameters.roleName as $role | (.spec.parameters.objects | from_yaml)[] | {"spc": $spc, "role": $role, "path": .secretPath, "method": (.method // "GET")}' \
    "$render_file" | jq -s '.' >"$temporary_directory/objects.json"

  while IFS= read -r role; do
    jq -r --arg role "$role" '.[] | select(.role==$role and (.path|type)=="string") | [.path,.method] | @tsv' \
      "$temporary_directory/objects.json" | sort -u | while IFS=$'\t' read -r path method; do
        [[ "$path" =~ ^[a-zA-Z0-9][a-zA-Z0-9._/-]{2,500}$ ]] || fail 'Vault policy path is invalid'
        if [[ "$method" == GET ]]; then capabilities='["read"]'; else capabilities='["update"]'; fi
        printf 'path "%s" { capabilities = %s }\n' "$path" "$capabilities"
      done >"$temporary_directory/$role.hcl"
    [[ -s "$temporary_directory/$role.hcl" ]] || fail "Vault role policy is empty: $role"
    vault_input "$temporary_directory/$role.hcl" policy write "spc-$role" - >/dev/null

    mapfile -t spcs < <(jq -r --arg role "$role" '.[] | select(.role==$role) | .spc' \
      "$temporary_directory/objects.json" | sort -u)
    spc_json=$(printf '%s\n' "${spcs[@]}" | jq -R . | jq -s .)
    service_accounts=$(jq -r --argjson spcs "$spc_json" '
      def podspec:
        if .kind=="CronJob" then .spec.jobTemplate.spec.template.spec
        elif (.kind=="Deployment" or .kind=="StatefulSet" or .kind=="Job" or .kind=="DaemonSet") then .spec.template.spec
        else empty end;
      [ .[] | podspec as $spec |
        select(any($spec.volumes[]?.csi.volumeAttributes.secretProviderClass?; . as $name | $spcs | index($name))) |
        $spec.serviceAccountName ] | map(select(. != null)) | unique | join(",")
    ' "$temporary_directory/render.json")
    if [[ -z "$service_accounts" ]]; then
      case "$role" in
        image-admission|internal-rpc-authority-image-admission)
          service_accounts=image-admission
          ;;
        image-promotion|internal-rpc-authority-image-promotion)
          service_accounts=image-promotion
          ;;
        mattercodex-image-scanner)
          service_accounts=mattercodex-image-scanner
          ;;
        mattercodex-image-signer)
          service_accounts=mattercodex-image-signer
          ;;
      esac
    fi
    [[ -n "$service_accounts" ]] || fail "Vault role has no exact ServiceAccount: $role"
    vault_cli write "auth/kubernetes/role/$role" \
      bound_service_account_names="$service_accounts" \
      bound_service_account_namespaces=mattercodex-system \
      token_policies="spc-$role" token_ttl=30m token_max_ttl=1h >/dev/null
  done < <(jq -r '.[].role' "$temporary_directory/objects.json" | sort -u)

  cat >"$temporary_directory/control-api-gateway-vso.hcl" <<'HCL'
path "kv/data/mattercodex/control-api-gateway/public-tls-material" { capabilities = ["read"] }
path "kv/data/mattercodex/control-api-gateway/session" { capabilities = ["read"] }
HCL
  vault_input "$temporary_directory/control-api-gateway-vso.hcl" policy write control-api-gateway-vso - >/dev/null
  vault_cli write auth/kubernetes/role/control-api-gateway \
    bound_service_account_names=control-api-gateway \
    bound_service_account_namespaces=mattercodex-system \
    token_policies=spc-control-api-gateway,control-api-gateway-vso \
    token_ttl=30m token_max_ttl=1h >/dev/null
fi

if [[ "$mode" == configure-database ]]; then
  require_root_material
  kubectl -n mattercodex-system wait --for=condition=Ready pod/mattercodex-postgresql-0 --timeout=300s >/dev/null
  database_password_file="$material_directory/postgresql/password"
  [[ -f "$database_password_file" && -s "$database_password_file" ]] || fail 'PostgreSQL bootstrap password is absent'
  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  database_password=$(read_single_line_secret "$database_password_file" 'PostgreSQL bootstrap password')
  for role in control_plane_migrator control_plane_runtime_g1 internal_rpc_authority_migrator; do
    openssl rand -base64 48 >"$temporary_directory/$role"
    role_password=$(read_single_line_secret "$temporary_directory/$role" 'generated PostgreSQL role password')
    printf '%s\n%s\n' "$database_password" "$role_password" |
      kubectl -n mattercodex-system exec -i mattercodex-postgresql-0 -- sh -ec '
        IFS= read -r PGPASSWORD
        IFS= read -r role_password
        export PGPASSWORD
        printf "ALTER ROLE %s PASSWORD '"'"'%s'"'"';\n" "$1" "$role_password" |
          psql --host=127.0.0.1 --username=postgres --dbname=postgres --set=ON_ERROR_STOP=1
      ' sh "$role" >/dev/null
  done

  database_connection="postgresql://postgres:$(jq -rn --arg value "$database_password" '$value|@uri')@mattercodex-postgresql.mattercodex-system.svc.cluster.local:5432/postgres?sslmode=verify-full&sslrootcert=/vault/userconfig/vault-server-tls/ca.crt"
  jq -cn --arg connection "$database_connection" '{
    plugin_name:"postgresql-database-plugin",
    allowed_roles:"control-plane-migrator,control-plane-runtime-g1,internal-rpc-authority-migrator,internal-rpc-authority-publisher-g3,internal-rpc-authority-publisher-g4,internal-rpc-authority-publisher-g5,internal-rpc-authority-readback-attestor-g3,internal-rpc-authority-readback-attestor-g4,internal-rpc-authority-readback-attestor-g5",
    connection_url:$connection
  }' >"$temporary_directory/database-config.json"

  configure_verified_database() {
    local error_file="$temporary_directory/database-config.error"
    local attempt=1 delay
    for delay in 1 2 3 5 8 13; do
      if vault_input "$temporary_directory/database-config.json" \
        write database/config/mattercodex-postgresql - >/dev/null 2>"$error_file"; then
        return
      fi
      rg -q 'error verifying connection:.*connection refused' "$error_file" ||
        fail 'Vault database configuration failed with a non-transient error'
      printf 'Vault PostgreSQL connection is not ready; retrying: next_attempt=%d delay=%ss\n' \
        "$((attempt + 1))" "$delay"
      sleep "$delay"
      attempt=$((attempt + 1))
    done
    if vault_input "$temporary_directory/database-config.json" \
      write database/config/mattercodex-postgresql - >/dev/null 2>"$error_file"; then
      return
    fi
    if rg -q 'error verifying connection:.*connection refused' "$error_file"; then
      fail 'Vault PostgreSQL connection did not become ready within the bounded retry budget'
    fi
    fail 'Vault database configuration failed with a non-transient error'
  }

  configure_verified_database

  control_plane_postgresql_host=control-plane-postgresql-rw.mattercodex-system.svc.cluster.local
  internal_rpc_authority_postgresql_host=internal-rpc-authority-postgresql-rw.mattercodex-system.svc.cluster.local

  write_dsn() {
    local vault_path=$1 username=$2 password_file=$3 database=$4 postgresql_host=$5 ca_file=$6
    local encoded_password password
    password=$(read_single_line_secret "$password_file" 'generated PostgreSQL role password')
    encoded_password=$(jq -rn --arg value "$password" '$value|@uri')
    printf 'postgresql://%s:%s@%s:5432/%s?sslmode=verify-full&sslrootcert=%s\n' \
      "$username" "$encoded_password" "$postgresql_host" "$database" "$ca_file" \
      >"$temporary_directory/dsn"
    vault_kv_put_file "$vault_path" dsn "$temporary_directory/dsn"
  }
  write_dsn mattercodex/control-plane/postgres-migration control_plane_migrator \
    "$temporary_directory/control_plane_migrator" control_plane "$control_plane_postgresql_host" \
    /var/run/config/mattercodex/control-plane/postgres/ca.pem
  write_dsn mattercodex/control-plane/postgres-runtime control_plane_runtime_g1 \
    "$temporary_directory/control_plane_runtime_g1" control_plane "$control_plane_postgresql_host" \
    /var/run/config/mattercodex/control-plane/postgres/ca.pem
  write_dsn internal-rpc-authority/postgres-migration internal_rpc_authority_migrator \
    "$temporary_directory/internal_rpc_authority_migrator" internal_rpc_authority \
    "$internal_rpc_authority_postgresql_host" \
    /var/run/config/mattercodex/internal-rpc-authority/postgresql/ca.pem
fi

if [[ "$mode" == configure-database-runtime ]]; then
  require_root_material
  kubectl -n mattercodex-system wait --for=condition=Complete \
    job/internal-rpc-authority-migrate --timeout=300s >/dev/null
  database_password_file="$material_directory/postgresql/password"
  [[ -f "$database_password_file" && -s "$database_password_file" ]] || fail 'PostgreSQL bootstrap password is absent'
  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  database_password=$(read_single_line_secret "$database_password_file" 'PostgreSQL bootstrap password')

  openssl rand -base64 48 >"$temporary_directory/ira_database_credential_reconciler"
  reconciler_password=$(read_single_line_secret \
    "$temporary_directory/ira_database_credential_reconciler" 'generated PostgreSQL reconciler password')
  printf '%s\n%s\n' "$database_password" "$reconciler_password" |
    kubectl -n mattercodex-system exec -i mattercodex-postgresql-0 -- sh -ec '
      IFS= read -r PGPASSWORD
      IFS= read -r role_password
      export PGPASSWORD
      printf "ALTER ROLE ira_database_credential_reconciler PASSWORD '\''%s'\'';\n" "$role_password" |
        psql --host=127.0.0.1 --username=postgres --dbname=postgres --set=ON_ERROR_STOP=1
    ' >/dev/null
  encoded_password=$(jq -rn --arg value "$reconciler_password" '$value|@uri')
  internal_rpc_authority_postgresql_host=internal-rpc-authority-postgresql-rw.mattercodex-system.svc.cluster.local
  printf 'postgresql://ira_database_credential_reconciler:%s@%s:5432/internal_rpc_authority?sslmode=verify-full&sslrootcert=/var/run/config/mattercodex/internal-rpc-authority/postgresql/ca.pem\n' \
    "$encoded_password" "$internal_rpc_authority_postgresql_host" \
    >"$temporary_directory/reconciler-dsn"
  vault_kv_put_file internal-rpc-authority/database-credential-reconciler dsn \
    "$temporary_directory/reconciler-dsn"

  for mapping in \
    internal-rpc-authority-publisher-g3:ira_publisher_g3 \
    internal-rpc-authority-publisher-g4:ira_publisher_g4 \
    internal-rpc-authority-publisher-g5:ira_publisher_g5 \
    internal-rpc-authority-readback-attestor-g3:ira_readback_attestor_g3 \
    internal-rpc-authority-readback-attestor-g4:ira_readback_attestor_g4 \
    internal-rpc-authority-readback-attestor-g5:ira_readback_attestor_g5; do
    role_name=${mapping%%:*}
    principal=${mapping#*:}
    vault_cli write "database/static-roles/$role_name" \
      db_name=mattercodex-postgresql username="$principal" rotation_period=1h >/dev/null
    vault_cli read -format=json "database/static-creds/$role_name" |
      jq -e --arg username "$principal" '.data.username == $username and (.data.password | length) >= 32' \
      >/dev/null || fail "database static credential readback failed: $role_name"
  done
fi

if [[ "$mode" == configure-image-pki ]]; then
  require_root_material
  [[ -f "$render_file" && -s "$render_file" && ! -L "$render_file" ]] || fail 'release render is required'
  promoted_pull_host=$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "mattercodex-image-admission-policy") | .data.pullRegistryHost' "$render_file")
  [[ "$promoted_pull_host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && "$promoted_pull_host" == *.* ]] ||
    fail 'promoted pull host is invalid'

  mounts=$(vault_cli secrets list -format=json)
  for mount in pki-buildkit-push pki-node-pull; do
    if ! jq -e --arg key "$mount/" 'has($key)' <<<"$mounts" >/dev/null; then
      vault_cli secrets enable -path="$mount" pki >/dev/null
    fi
    vault_cli secrets tune -max-lease-ttl=87600h "$mount" >/dev/null
  done
  if ! vault_cli read -format=json pki-buildkit-push/cert/ca >/dev/null 2>&1; then
    vault_cli write pki-buildkit-push/root/generate/internal \
      common_name=mattercodex-buildkit-staging-push-root ttl=87600h key_type=rsa key_bits=4096 >/dev/null
  fi
  if ! vault_cli read -format=json pki-node-pull/cert/ca >/dev/null 2>&1; then
    vault_cli write pki-node-pull/root/generate/internal \
      common_name=mattercodex-node-pull-root ttl=87600h key_type=rsa key_bits=4096 >/dev/null
  fi

  configure_server_role() {
    local mount=$1 role=$2 service=$3
    vault_cli write "$mount/roles/$role" \
      allowed_domains="$service,$service.mattercodex-system.svc,$service.mattercodex-system.svc.cluster.local" \
      allow_bare_domains=true allow_subdomains=false allow_glob_domains=false enforce_hostnames=true require_cn=true \
      server_flag=true client_flag=false key_type=rsa key_bits=3072 \
      key_usage=DigitalSignature,KeyEncipherment ext_key_usage=ServerAuth ttl=1h max_ttl=2h >/dev/null
  }
  configure_client_role() {
    local mount=$1 role=$2 common_name=$3
    vault_cli write "$mount/roles/$role" \
      allowed_domains="$common_name,$common_name.mattercodex-system.svc" \
      allow_bare_domains=true allow_subdomains=false allow_glob_domains=false enforce_hostnames=true require_cn=true \
      server_flag=false client_flag=true key_type=rsa key_bits=3072 \
      key_usage=DigitalSignature,KeyEncipherment ext_key_usage=ClientAuth ttl=30m max_ttl=1h >/dev/null
  }

  configure_server_role pki mattercodex-buildkit-server mattercodex-buildkit
  configure_server_role pki-buildkit-push mattercodex-image-registry-push mattercodex-image-registry-push
  configure_server_role pki mattercodex-image-registry-staging-read mattercodex-image-registry-staging-read
  configure_server_role pki mattercodex-image-registry-evidence mattercodex-image-registry-evidence
  configure_server_role pki mattercodex-image-registry-admin mattercodex-image-registry-admin
  configure_server_role pki mattercodex-image-registry-promotion mattercodex-image-registry-promotion
  vault_cli write pki-public/roles/mattercodex-image-registry-pull \
    allowed_domains="$promoted_pull_host,mattercodex-image-registry,mattercodex-image-registry.mattercodex-system.svc,mattercodex-image-registry.mattercodex-system.svc.cluster.local" \
    allow_bare_domains=true allow_subdomains=false allow_glob_domains=false enforce_hostnames=true require_cn=true \
    server_flag=true client_flag=false key_type=rsa key_bits=3072 \
    key_usage=DigitalSignature,KeyEncipherment ext_key_usage=ServerAuth ttl=1h max_ttl=2h >/dev/null

  for mapping in \
    mattercodex-buildkit-probe:mattercodex-buildkit-probe \
    mattercodex-buildkit-client:role-image-builder \
    mattercodex-buildkit-base-pull:mattercodex-buildkit-base-pull \
    mattercodex-role-image-input-read:role-image-builder-input-read \
    mattercodex-image-registry-pull-probe:mattercodex-image-registry-pull-probe \
    mattercodex-image-registry-admin-probe:mattercodex-image-registry-admin-probe \
    mattercodex-image-registry-promotion-probe:mattercodex-image-registry-promotion-probe \
    mattercodex-image-registry-evidence-probe:mattercodex-image-registry-evidence-probe \
    mattercodex-image-scanner:mattercodex-image-scanner \
    mattercodex-image-signer:mattercodex-image-signer \
    image-admission:image-admission \
    image-promotion:image-promotion \
    mattercodex-registry-cleanup:mattercodex-registry-cleanup \
    release-artifact-materializer:release-artifact-materializer; do
    configure_client_role pki "${mapping%%:*}" "${mapping#*:}"
  done
  configure_client_role pki-buildkit-push mattercodex-buildkit-staging-push mattercodex-buildkit-staging-push
  configure_client_role pki-buildkit-push mattercodex-image-registry-push-probe mattercodex-image-registry-push-probe
  vault_cli write pki-node-pull/roles/mattercodex-node-pull \
    allowed_domains=mattercodex-node-pull allow_bare_domains=false allow_subdomains=true \
    allow_glob_domains=false enforce_hostnames=true require_cn=true allow_ip_sans=true \
    server_flag=false client_flag=true key_type=rsa key_bits=3072 \
    key_usage=DigitalSignature,KeyEncipherment ext_key_usage=ClientAuth ttl=30m max_ttl=30m >/dev/null

  temporary_directory=$(mktemp -d)
  trap 'rm -rf -- "$temporary_directory"' EXIT
  image_material="$material_directory/image-registry"
  for required_directory in pull buildkit-base-pull input-read staging-read evidence-probe evidence-admission \
    evidence-promotion admin promotion scanner signer admission promotion-staging release-source signing; do
    [[ -d "$image_material/$required_directory" && ! -L "$image_material/$required_directory" ]] ||
      fail "image registry material is absent: $required_directory"
  done

  pull_user=$(<"$image_material/pull/username")
  pull_password=$(<"$image_material/pull/password")
  pull_auth=$(printf '%s:%s' "$pull_user" "$pull_password" | base64 | tr -d '\n')
  jq -n --arg host "$promoted_pull_host" --arg auth "$pull_auth" '{auths:{($host):{auth:$auth}}}' \
    >"$temporary_directory/pull-dockerconfig.json"

  cat "$image_material/scanner/htpasswd" "$image_material/signer/htpasswd" \
    "$image_material/admission/htpasswd" "$image_material/promotion-staging/htpasswd" \
    >"$temporary_directory/staging-read.htpasswd"

  seed_credential() {
    local vault_path=$1 source_name=$2
    vault_kv_put_file "$vault_path" username "$image_material/$source_name/username"
    vault_kv_patch_file "$vault_path" password "$image_material/$source_name/password"
  }
  vault_kv_put_file mattercodex/image-registry/pull htpasswd "$image_material/pull/htpasswd"
  vault_kv_patch_file mattercodex/image-registry/pull dockerconfigjson "$temporary_directory/pull-dockerconfig.json"
  vault_kv_put_file mattercodex/image-registry/buildkit-base-pull dockerconfigjson "$image_material/buildkit-base-pull/dockerconfig.json"
  vault_kv_put_file mattercodex/role-image-builder/input-read docker-config "$image_material/input-read/dockerconfig.json"
  vault_kv_put_file mattercodex/image-registry/staging-read htpasswd "$temporary_directory/staging-read.htpasswd"
  for mapping in \
    mattercodex/image-registry/evidence-probe:evidence-probe \
    mattercodex/image-registry/evidence-admission:evidence-admission \
    mattercodex/image-registry/evidence-promotion:evidence-promotion \
    mattercodex/image-registry/scanner:scanner \
    mattercodex/image-registry/signer:signer \
    mattercodex/image-registry/admission:admission \
    mattercodex/image-registry/promotion-staging:promotion-staging; do
    seed_credential "${mapping%%:*}" "${mapping#*:}"
  done
  seed_credential mattercodex/image-registry/admin admin
  vault_kv_patch_file mattercodex/image-registry/admin htpasswd "$image_material/admin/htpasswd"
  seed_credential mattercodex/image-registry/promotion promotion
  vault_kv_patch_file mattercodex/image-registry/promotion htpasswd "$image_material/promotion/htpasswd"
  vault_kv_patch_file mattercodex/image-registry/promotion dockerconfigjson "$image_material/promotion/dockerconfig.json"
  vault_kv_put_file mattercodex/release-registry/pull dockerconfigjson "$image_material/release-source/dockerconfig.json"
  vault_kv_put_file mattercodex/image-admission/signing private_key "$image_material/signing/cosign.key"
  vault_kv_patch_file mattercodex/image-admission/signing public_key "$image_material/signing/cosign.pub"
  vault_kv_patch_file mattercodex/image-admission/signing password "$image_material/signing/password"

  for required_path in \
    pki/roles/mattercodex-buildkit-server \
    pki-public/roles/mattercodex-image-registry-pull \
    pki-buildkit-push/roles/mattercodex-image-registry-push \
    pki-node-pull/roles/mattercodex-node-pull \
    kv/data/mattercodex/image-registry/pull \
    kv/data/mattercodex/image-registry/promotion \
    kv/data/mattercodex/image-admission/signing; do
    vault_cli read -format=json "$required_path" >/dev/null || fail "image PKI readback failed: $required_path"
  done
fi

if [[ "$mode" == readback ]]; then
  require_root_material
  vault_status | jq -e '.initialized == true and .sealed == false' >/dev/null || fail 'Vault status readback failed'
  vault_cli secrets list -format=json | jq -e 'has("kv/") and has("secret/") and has("database/") and has("pki/")' >/dev/null ||
    fail 'Vault engines readback failed'
  vault_cli auth list -format=json | jq -e 'has("kubernetes/")' >/dev/null || fail 'Vault auth readback failed'
fi

printf 'Vault bootstrap completed: %s\n' "$mode"
