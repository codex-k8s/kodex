#!/usr/bin/env bash
set -euo pipefail
umask 077

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
classifier="$repository_root/tools/deploy/classify-direct-production-application-material.sh"
materializer="$repository_root/tools/deploy/materialize-direct-production-application.sh"
policy="$repository_root/infra/direct-production/application-material-policy.json"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

classification="$temporary_directory/classification.json"
"$classifier" --output "$classification" >/dev/null
[[ "$(stat -c '%a' "$classification")" == 600 ]] || {
  printf 'Application material classification permissions are not 0600\n' >&2
  exit 1
}

jq -e '
  .schema_version == 1 and
  .profile == "direct-production single-node prototype" and
  .namespace == "mattercodex-system" and
  (.resources | length) == 161 and
  ([.resources[] | select(.kind == "Secret")] | length) == 141 and
  ([.resources[] | select(.kind == "ConfigMap")] | length) == 20 and
  .counts == {
    cryptographically_generated:67,
    deterministically_derived:76,
    safely_reusable_from_existing_binding:2,
    truly_external_credential:16
  } and
  all(.resources[];
    (.keys | type == "array" and length > 0 and length == (unique | length))) and
  ([.external_bindings[].keys[]] | length) == 37 and
  (.resources | group_by([.kind,.name]) | all(length == 1))
' "$classification" >/dev/null || {
  printf 'Direct-production application material classification is incomplete\n' >&2
  exit 1
}

jq -e '
  ([.external_bindings[] | [.kind,.name]] | length) ==
    ([.external_bindings[] | [.kind,.name]] | unique | length) and
  ([.external_bindings[] as $binding |
    any(.resources[];
      .kind == $binding.kind and .name == $binding.name and
      .classification == "truly_external_credential")] | all) and
  all(.external_bindings[];
    (.keys | length) > 0 and (.keys | length) == (.keys | unique | length) and
    (.requirement | type == "string" and length > 0)) and
  all(.reusable_bindings[];
    (.source_namespace == "matter-kodex-prod" or
     (.target_kind == "ConfigMap" and .target_name == "mattermost-ca" and
      .source_namespace == "mattercodex-system" and
      .source_name == "mattercodex-legacy-mattermost-bridge-tls")) and
    (.key_map | type == "object" and length > 0)) and
  ([.reusable_bindings[] as $binding |
    any(.resources[];
      .kind == $binding.target_kind and .name == $binding.target_name and
      (.classification == "safely_reusable_from_existing_binding" or
       .classification == "truly_external_credential"))] | all) and
  ([.publisher_owned_empty_resources[] as $binding |
    any(.resources[];
      .kind == $binding.kind and .name == $binding.name and
      .classification == "deterministically_derived")] | all)
' "$policy" >/dev/null || {
  printf 'Application material policy has an ambiguous binding\n' >&2
  exit 1
}

external_fixture="$temporary_directory/external.yaml"
openssl req -x509 -newkey rsa:2048 -nodes -subj /CN=mattercodex-test-ca -days 1 \
  -keyout "$temporary_directory/test-ca.key" -out "$temporary_directory/test-ca.pem" >/dev/null 2>&1
jws_fixture=$(node -e '
  const h=Buffer.from(JSON.stringify({alg:"EdDSA",kid:"test"})).toString("base64url");
  const p=Buffer.from("{}").toString("base64url");
  const s=Buffer.alloc(64).toString("base64url");
  process.stdout.write(`${h}.${p}.${s}`)')
jwk_fixture='{"kty":"OKP","crv":"Ed25519","x":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","alg":"EdDSA","kid":"test","use":"sig"}'
jwks_fixture="{\"keys\":[$jwk_fixture]}"
manifest_fixture='{"schema_version":1,"revision":"production-r1"}'
manifest_digest=$(printf '%s' "$manifest_fixture" | sha256sum | awk '{print $1}')
JWS_FIXTURE="$jws_fixture" JWK_FIXTURE="$jwk_fixture" JWKS_FIXTURE="$jwks_fixture" \
MANIFEST_FIXTURE="$manifest_fixture" MANIFEST_DIGEST="$manifest_digest" \
CA_FIXTURE="$(base64 -w0 "$temporary_directory/test-ca.pem")" jq -c '
  def value($key):
    if ($key | test("(\\.jws|\\.jwt)$")) then env.JWS_FIXTURE
    elif ($key | endswith(".jwk")) then env.JWK_FIXTURE
    elif ($key | endswith("public-keyset.json")) then env.JWKS_FIXTURE
    elif $key == "manifest.yaml" then env.MANIFEST_FIXTURE
    elif $key == "manifest.sha256" then env.MANIFEST_DIGEST
    elif $key == "revision" then "production-r1"
    elif ($key | endswith("-arn")) then "arn:aws:iam::123456789012:role/mattercodex-test"
    elif $key == "ca.pem" then (env.CA_FIXTURE | @base64d)
    else "0123456789abcdef0123456789abcdef" end;
  .external_bindings[] as $binding |
  if $binding.kind == "Secret" then
    {apiVersion:"v1",kind:$binding.kind,metadata:{name:$binding.name,namespace:"mattercodex-system"},
     data:($binding.keys | map({key:.,value:(value(.) | @base64)}) | from_entries)}
  else
    {apiVersion:"v1",kind:$binding.kind,metadata:{name:$binding.name,namespace:"mattercodex-system"},
     data:($binding.keys | map({key:.,value:value(.)}) | from_entries)}
  end
' "$policy" | yq -p=json -P >"$external_fixture"
"$classifier" --output "$temporary_directory/with-external.json" --external-material-file "$external_fixture" >/dev/null

material="$temporary_directory/application-material.yaml"
"$materializer" --mode render --external-material-file "$external_fixture" --output "$material" >/dev/null
[[ "$(stat -c '%a' "$material")" == 600 ]] || {
  printf 'Application material render permissions are not 0600\n' >&2
  exit 1
}
yq -o=json eval-all '.' "$material" | jq -s --slurpfile classification "$classification" -e '
  map(select(.kind != null)) |
  length == 161 and
  ([.[] | [.kind,.metadata.name]] | sort) == ([$classification[0].resources[] | [.kind,.name]] | sort) and
  all(.[]; . as $resource |
    ([((.data // {}) | keys[]),((.binaryData // {}) | keys[])] | unique | sort) ==
    ([$classification[0].resources[] | select(.kind == $resource.kind and .name == $resource.metadata.name) | .keys[]] | unique | sort)) and
  all(.[]; . as $resource |
    all([((.data // {}) | to_entries[]),((.binaryData // {}) | to_entries[])][]; . as $entry |
      (any(($classification[0].runtime_owned_empty_resources // [])[];
        .kind == $resource.kind and
        .name == $resource.metadata.name and
        ((.keys // []) | index($entry.key) != null)) and
       $entry.value == "") or
      $entry.value != ""))
' >/dev/null || {
  printf 'Application material render differs from the exact interface set\n' >&2
  exit 1
}

material_json="$temporary_directory/application-material.json"
yq -o=json eval-all '.' "$material" | jq -s 'map(select(.kind != null))' >"$material_json"
yq -er 'select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-vault-ca") |
  .data."ca.pem"' "$external_fixture" >"$temporary_directory/vault-ca.pem"
openssl x509 -in "$temporary_directory/vault-ca.pem" -outform DER \
  >"$temporary_directory/vault-ca.der"
for name in integration-gateway-vault-ca interaction-gateway-vault-ca; do
  jq -er --arg name "$name" '.[] | select(.kind == "Secret" and .metadata.name == $name) |
    .data."ca.crt"' "$material_json" | base64 -d >"$temporary_directory/$name.pem"
  openssl x509 -in "$temporary_directory/$name.pem" -outform DER \
    >"$temporary_directory/$name.der"
  cmp -s "$temporary_directory/vault-ca.der" "$temporary_directory/$name.der" || {
    printf 'Vault client CA does not match the exact external Vault authority: %s\n' "$name" >&2
    exit 1
  }
done
for name in control-plane-nats runtime-controller-nats; do
  jq -er --arg name "$name" '.[] | select(.kind=="Secret" and .metadata.name==$name) | .data["user.creds"]' "$material_json" |
    base64 -d >"$temporary_directory/$name.creds"
done
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-nats-creds \
  "$temporary_directory/control-plane-nats.creds" control-plane \
  '$JS.API.>,control_plane.runtime_configuration_changed' '_INBOX.>'
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-nats-creds \
  "$temporary_directory/runtime-controller-nats.creds" runtime-controller \
  '$JS.ACK.>,$JS.API.>' '_INBOX.>,control_plane.runtime_configuration_changed'
jq -er '.[] | select(.kind=="Secret" and .metadata.name=="control-plane-postgres-runtime") | .data.dsn' "$material_json" |
  base64 -d >"$temporary_directory/postgres-dsn"
grep -Eq '^postgresql://[^:]+:[a-f0-9]{64}@control-plane-postgresql-rw\.mattercodex-system\.svc\.cluster\.local:5432/control_plane\?sslmode=verify-full&sslrootcert=/var/run/config/mattercodex/control-plane/postgres/ca\.pem&options=-c%20role%3Dcontrol_plane_runtime$' \
  "$temporary_directory/postgres-dsn" || {
    printf 'Generated PostgreSQL DSN semantics are invalid\n' >&2
    exit 1
  }
jq -er '.[] | select(.kind=="Secret" and .metadata.name=="control-api-gateway-public-tls-material") | .data["tls.crt"]' "$material_json" |
  base64 -d >"$temporary_directory/control-api.crt"
openssl x509 -in "$temporary_directory/control-api.crt" -noout -checkhost control-api.mattercodex.local >/dev/null 2>&1 || {
  printf 'Generated TLS hostname is invalid\n' >&2
  exit 1
}

foundation="$temporary_directory/foundation.yaml"
kubectl kustomize "$repository_root/deploy/k8s/base/direct-production-foundation" >"$foundation"
foundation_json="$temporary_directory/foundation.json"
yq -o=json eval-all '.' "$foundation" | jq -s 'map(select(.kind != null))' >"$foundation_json"
jq -er '.[] | select(.kind=="ConfigMap" and .metadata.name=="mattercodex-postgresql-principal-bootstrap") |
  .data["reconcile.sh"]' "$foundation_json" >"$temporary_directory/postgresql-principal-reconcile.sh"
sh -n "$temporary_directory/postgresql-principal-reconcile.sh"
[[ "$(jq -r '.[] | select(.kind=="ConfigMap" and .metadata.name=="mattercodex-postgresql-principal-bootstrap") |
  .data["principals.tsv"]' "$foundation_json" | awk -F '\t' 'NF >= 4 {count++} END {print count+0}')" == 29 ]] || {
  printf 'PostgreSQL principal registry is incomplete\n' >&2
  exit 1
}
rg -q 'ALTER ROLE %s NOLOGIN.*pg_terminate_backend' "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q "format\('REVOKE %%I FROM %%I'" "$temporary_directory/postgresql-principal-reconcile.sh" || {
  printf 'PostgreSQL retirement boundary is incomplete\n' >&2
  exit 1
}
jq -e '
  first(.[] | select(.kind == "ConfigMap" and .metadata.name == "mattercodex-nats-config") |
    .data["nats.conf"]) as $config |
  ($config | contains("operator: $NATS_OPERATOR_JWT")) and
  ($config | contains("resolver: MEMORY")) and ($config | contains("verify: true")) and
  ($config | contains("username") | not) and ($config | contains("password") | not)
' "$foundation_json" >/dev/null || {
  printf 'Foundation NATS operator/account TLS contract is invalid\n' >&2
  exit 1
}
for bridge in mattermost bot-service; do
  BRIDGE="$bridge" jq -e '
    first(.[] | select(.kind == "Deployment" and .metadata.name == ("mattercodex-legacy-" + env.BRIDGE + "-bridge"))) |
    .spec.replicas == 2 and .spec.strategy.rollingUpdate.maxUnavailable == 0 and
    .spec.template.spec.containers[0].readinessProbe.httpGet.path == "/readyz" and
    .spec.template.spec.containers[0].readinessProbe.httpGet.port == "readiness"
  ' "$foundation_json" >/dev/null || {
    printf 'Legacy TLS bridge rollout/readiness contract is invalid\n' >&2
    exit 1
  }
done
for bridge_key in mattermost.yaml bot-service.yaml; do
  jq -er --arg key "$bridge_key" '.[] | select(.kind=="ConfigMap" and .metadata.name=="mattercodex-legacy-transport-bridges") |
    .data[$key]' "$foundation_json" >"$temporary_directory/$bridge_key"
  yq -e '.' "$temporary_directory/$bridge_key" >/dev/null || {
    printf 'Legacy TLS bridge Envoy configuration is not YAML\n' >&2
    exit 1
  }
done
jq -e '
  first(.[] | select(.kind == "ConfigMap" and .metadata.name == "mattercodex-legacy-transport-bridges") |
    .data["mattermost.yaml"]) as $config |
  ($config | contains("mattermost.matter-kodex-prod.svc.cluster.local")) and
  ($config | contains("require_client_certificate: true")) and ($config | contains("port_value: 8065")) and
  ($config | contains("spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway"))
' "$foundation_json" >/dev/null && jq -e '
  first(.[] | select(.kind == "ConfigMap" and .metadata.name == "mattercodex-legacy-transport-bridges") |
    .data["bot-service.yaml"]) as $config |
  ($config | contains("matter-codex-bot-service.matter-kodex-prod.svc.cluster.local")) and
  ($config | contains("require_client_certificate: true")) and ($config | contains("port_value: 8080")) and
  ($config | contains("spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway"))
' "$foundation_json" >/dev/null || {
  printf 'Legacy TLS bridge upstream contract is invalid\n' >&2
  exit 1
}
if jq -e 'any(.[]; .metadata.namespace == "matter-kodex-prod")' "$foundation_json" >/dev/null; then
  printf 'Foundation render mutates the legacy namespace\n' >&2
  exit 1
fi
jq -e '
  first(.[] | select(.kind == "NetworkPolicy" and .metadata.name == "legacy-mattermost-bridge-exact-path")) |
  .spec.egress[0].to[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "matter-kodex-prod" and
  .spec.egress[0].to[0].podSelector.matchLabels."app.kubernetes.io/name" == "mattermost" and
  .spec.egress[0].ports[0].port == 8065
' "$foundation_json" >/dev/null && jq -e '
  first(.[] | select(.kind == "NetworkPolicy" and .metadata.name == "legacy-bot-service-bridge-exact-path")) |
  .spec.egress[0].to[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "matter-kodex-prod" and
  .spec.egress[0].to[0].podSelector.matchLabels."app.kubernetes.io/name" == "matter-codex-bot-service" and
  .spec.egress[0].ports[0].port == 8080
' "$foundation_json" >/dev/null || {
  printf 'Legacy TLS bridge NetworkPolicy destination is not exact\n' >&2
  exit 1
}
interfaces="$temporary_directory/interfaces.yaml"
"$repository_root/tools/release/render-direct-production-applications.sh" --scope interfaces --output "$interfaces" >/dev/null
rg -q 'mattercodex-legacy-mattermost-bridge\.mattercodex-system\.svc\.cluster\.local' "$interfaces" &&
  rg -q 'mattercodex-legacy-bot-service-bridge\.mattercodex-system\.svc\.cluster\.local' "$interfaces" || {
  printf 'Application TLS bridge endpoints are absent from render\n' >&2
  exit 1
}
if rg -q 'https://mattermost\.mattermost\.svc\.cluster\.local|matter-codex-bot-service\.mattercodex-system\.svc\.cluster\.local' "$interfaces"; then
  printf 'Application render retained a legacy plaintext/TLS fallback endpoint\n' >&2
  exit 1
fi

target_registry="$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml"
target_registry_json="$temporary_directory/target-registry.json"
yq -o=json '.targets' "$target_registry" >"$target_registry_json"
jq -e '
  length == 13 and
  ([.[] | [.workload_id,.role]] | unique | length) == 13 and
  ([.[] | [
    .auth_private_key.vault_path?,.manifest_trust.vault_path?,
    .authority_proof_trust.vault_path?,.authority_proof_private_key.vault_path?,
    .restore_coordination.role_credential_vault_path,
    .restore_coordination.ack_key_vault_path,
    .readback.credential_vault_path,.readback.possession_key_vault_path
  ][] | select(. != null)] | length) == 85 and
  ([.[] | [
    .auth_private_key.vault_path?,.manifest_trust.vault_path?,
    .authority_proof_trust.vault_path?,.authority_proof_private_key.vault_path?,
    .restore_coordination.role_credential_vault_path,
    .restore_coordination.ack_key_vault_path,
    .readback.credential_vault_path,.readback.possession_key_vault_path
  ][] | select(. != null)] | unique | length) == 85 and
  any(.[]; .workload_id == "integration-gateway" and .role == "AUTHORIZATION_VERIFIER" and
    .service_account == "integration-gateway" and
    .database_identity.login_principal == "ira_integration_gateway_verifier_g1" and
    .readback.credential_vault_path == "kv/data/mattercodex/internal-rpc-authority/integration-gateway/verifier/readback-credential") and
  any(.[]; .workload_id == "runtime-controller" and .role == "AUTHORIZATION_ISSUER" and
    .service_account == "runtime-controller" and
    .database_identity.login_principal == "ira_runtime_controller_issuer_g1" and
    .auth_private_key.vault_path == "kv/data/mattercodex/internal-rpc-authority/runtime-controller/issuer/auth-private") and
  any(.[]; .workload_id == "runtime-s3-restore-exchanger" and .role == "AUTHORIZATION_ISSUER" and
    .service_account == "runtime-s3-restore-exchanger" and
    .database_identity.login_principal == "ira_runtime_s3_restore_exchanger_issuer_g1" and
    .auth_private_key.vault_path == "kv/data/mattercodex/internal-rpc-authority/runtime-s3-restore-exchanger/issuer/auth-private")
' "$target_registry_json" >/dev/null || {
  printf 'Publisher target registry does not close the three release profiles\n' >&2
  exit 1
}

publisher_rbac="$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/rbac.yaml"
expected_publisher_resources="$temporary_directory/expected-publisher-resources"
actual_publisher_resources="$temporary_directory/actual-publisher-resources"
jq -r '.[] | . as $target |
  (if .role == "AUTHORIZATION_ISSUER" then "issuer"
   elif .role == "AUTHORIZATION_VERIFIER" then "verifier" else "resolver" end) as $role |
  ("internal-rpc-authority-" + .workload_id) as $prefix |
  [$prefix + "-" + $role + "-delivery",
   (if .auth_private_key then $prefix + "-" + $role + "-key" else empty end),
   (if .manifest_trust then $prefix + "-manifest-trust" else empty end),
   (if .authority_proof_trust then
      (if $role == "resolver" then $prefix + "-resolver-trust" else $prefix + "-proof-trust" end)
    else empty end),
   (if .authority_proof_private_key then $prefix + "-resolver-key" else empty end)][]' \
  "$target_registry_json" | { cat; printf '%s\n' internal-rpc-authority-snapshot; } |
  LC_ALL=C sort -u >"$expected_publisher_resources"
yq -o=json 'select(.kind == "Role" and .metadata.name == "internal-rpc-authority-publisher")' \
  "$publisher_rbac" | jq -e '
    .rules == [{apiGroups:[""],resources:["secrets"],resourceNames:.rules[0].resourceNames,verbs:["get","update"]}] and
    (.rules[0].resourceNames | length) == 44
  ' >/dev/null || {
  printf 'Publisher RBAC contains a forbidden resource or verb\n' >&2
  exit 1
}
yq -r 'select(.kind == "Role" and .metadata.name == "internal-rpc-authority-publisher") |
  .rules[0].resourceNames[]' "$publisher_rbac" | LC_ALL=C sort -u >"$actual_publisher_resources"
cmp -s "$expected_publisher_resources" "$actual_publisher_resources" || {
  printf 'Publisher RBAC differs from the target registry\n' >&2
  exit 1
}

yq -o=json eval-all '.' "$interfaces" | jq -s -e '
  def profile($workload; $container; $secret; $init):
    first(.[] | select(.kind == "Deployment" and .metadata.name == $workload)) as $deployment |
    (if $init then $deployment.spec.template.spec.initContainers else $deployment.spec.template.spec.containers end) as $containers |
    any($containers[]; .name == $container and
      any(.env[]?; .name == "INTERNAL_RPC_AUTHORITY_WORKLOAD_ID" and .value == $workload) and
      any(.env[]?; .name == "INTERNAL_RPC_AUTHORITY_SECRET_BACKEND" and .value == "direct-production-kubernetes-file") and
      any(.volumeMounts[]?;
        .mountPath == "/var/run/secrets/mattercodex/internal-rpc-authority/prototype-delivery/primary" and
        .readOnly == true) and
      (any(.volumeMounts[]?;
        .name == "kube-api-access" or
        (.mountPath | startswith("/var/run/secrets/kubernetes.io/serviceaccount"))) | not)) and
    any($deployment.spec.template.spec.volumes[]?;
      .secret.secretName == $secret and .secret.defaultMode == 288);
  profile("integration-gateway"; "internal-rpc-authority-verifier";
    "internal-rpc-authority-integration-gateway-verifier-delivery"; false) and
  profile("runtime-controller"; "internal-rpc-authority-issuer";
    "internal-rpc-authority-runtime-controller-issuer-delivery"; false) and
  profile("runtime-s3-restore-exchanger"; "internal-rpc-authority-issuer";
    "internal-rpc-authority-runtime-s3-restore-exchanger-issuer-delivery"; true) and
  (any(.[]; .kind == "Deployment" and .metadata.name == "runtime-s3-restore-exchanger" and
    any(.spec.template.spec.volumes[]?;
      .name == "restore-effect-workload-tls" and
      .secret.secretName == "runtime-restore-effect-workload-tls")) and
   any(.[]; .kind == "Deployment" and .metadata.name == "runtime-s3-restore-exchanger" and
    any(.spec.template.spec.volumes[]?;
      .name == "authority-workload-tls" and
      .secret.secretName == "internal-rpc-authority-runtime-s3-restore-exchanger-workload-tls")))
' >/dev/null || {
  printf 'Three release profiles do not have exact file-only delivery mounts\n' >&2
  exit 1
}
if rg -q 'internal-rpc-authority-runtime-restore-effect|ira_runtime_restore_effect' \
  "$repository_root/deploy" "$repository_root/infra" "$repository_root/tools"; then
  printf 'Runtime restore authority identity alias remains in infrastructure\n' >&2
  exit 1
fi

while IFS=$'\t' read -r workload_name config_map_name address_env tls_server_name_env ca_file_env; do
  yq -o=json eval-all '.' "$interfaces" | jq -s -e \
    --arg workload_name "$workload_name" --arg config_map_name "$config_map_name" \
    --arg address_env "$address_env" --arg tls_server_name_env "$tls_server_name_env" \
    --arg ca_file_env "$ca_file_env" '
    def exact_values($data):
      ($data[$address_env] == "https://vault.mattercodex-system.svc:8200" or
       $data[$address_env] == "https://vault.mattercodex-system.svc.cluster.local:8200") and
      $data[$tls_server_name_env] == "vault.mattercodex-system.svc.cluster.local" and
      ($data[$ca_file_env] | type == "string" and startswith("/var/run/config/mattercodex/"));
    if $config_map_name != "-" then
      any(.[]; .kind == "ConfigMap" and .metadata.name == $config_map_name and exact_values(.data)) and
      any(.[]; .kind == "Deployment" and .metadata.name == $workload_name and
        any(.spec.template.spec.containers[]?;
          any(.envFrom[]?; .configMapRef.name == $config_map_name)))
    else
      any(.[]; .kind == "Deployment" and .metadata.name == $workload_name and
        any(((.spec.template.spec.containers // []) +
             (.spec.template.spec.initContainers // []))[]?;
          (.env // [] | map({key:.name,value:.value}) | from_entries) as $data |
          exact_values($data)))
    end
  ' >/dev/null || {
    printf 'Expected live Vault application boundary is absent: %s\n' "$workload_name" >&2
    exit 1
  }
  grep -Fq "\"$address_env\"" \
    "$repository_root/infra/direct-production/bootstrap.sh" || {
    printf 'Owner bootstrap does not preflight a live Vault boundary: %s\n' "$workload_name" >&2
    exit 1
  }
done <<'EOF'
integration-gateway	integration-gateway-runtime	INTEGRATION_GATEWAY_VAULT_ADDRESS	INTEGRATION_GATEWAY_VAULT_TLS_SERVER_NAME	INTEGRATION_GATEWAY_VAULT_CA_FILE
interaction-gateway	interaction-gateway-runtime	INTERACTION_GATEWAY_BOT_CREDENTIAL_VAULT_ADDRESS	INTERACTION_GATEWAY_BOT_CREDENTIAL_VAULT_TLS_SERVER_NAME	INTERACTION_GATEWAY_BOT_CREDENTIAL_VAULT_CA_FILE
runtime-s3-restore-exchanger	-	RUNTIME_VAULT_ADDRESS	RUNTIME_VAULT_TLS_SERVER_NAME	RUNTIME_VAULT_CA_FILE
runtime-s3-archive-exchanger	-	RUNTIME_VAULT_ADDRESS	RUNTIME_VAULT_TLS_SERVER_NAME	RUNTIME_VAULT_CA_FILE
EOF
grep -Fq 'kubernetes.io/service-name=vault' "$repository_root/infra/direct-production/bootstrap.sh" &&
  grep -Fq 'required exact Vault endpoint binding is absent' \
    "$repository_root/infra/direct-production/bootstrap.sh" || {
  printf 'Owner bootstrap Vault EndpointSlice readback is absent\n' >&2
  exit 1
}

application_bootstrap="$temporary_directory/application-bootstrap.yaml"
"$repository_root/tools/release/render-direct-production-applications.sh" \
  --scope bootstrap --output "$application_bootstrap" >/dev/null
yq -o=json eval-all '.' "$application_bootstrap" | jq -s -e '
  def exact_vault($name):
    first(.[] | select(.kind == "NetworkPolicy" and .metadata.name == $name)) as $policy |
    any($policy.spec.egress[]?;
      .to == [{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"vault"}}}] and
      .ports == [{"protocol":"TCP","port":8200}]);
  exact_vault("integration-gateway-exact-runtime-paths") and
  exact_vault("interaction-gateway-exact-runtime-paths") and
  exact_vault("runtime-s3-restore-exchanger-exact-paths") and
  exact_vault("runtime-s3-archive-exchanger-exact-paths")
' >/dev/null || {
  printf 'Live Vault NetworkPolicy destination is not exact\n' >&2
  exit 1
}

cp "$external_fixture" "$temporary_directory/missing-key.yaml"
yq -i '
  with(select(.kind == "Secret" and
    .metadata.name == "automation-scheduler-application-grant");
    del(.data."application-grant.jws"))
' "$temporary_directory/missing-key.yaml"
if "$classifier" --output "$temporary_directory/rejected.json" --external-material-file "$temporary_directory/missing-key.yaml" >/dev/null 2>&1; then
  printf 'Incomplete external material was accepted\n' >&2
  exit 1
fi

cp "$external_fixture" "$temporary_directory/extra-key.yaml"
yq -i 'with(select(.kind == "Secret" and .metadata.name == "automation-scheduler-application-grant");
  .data.unexpected = "Zml4dHVyZQ==")' "$temporary_directory/extra-key.yaml"
if "$classifier" --output "$temporary_directory/rejected-extra.json" --external-material-file "$temporary_directory/extra-key.yaml" >/dev/null 2>&1; then
  printf 'External material with an extra key was accepted\n' >&2
  exit 1
fi

cp "$external_fixture" "$temporary_directory/empty-key.yaml"
yq -i 'with(select(.kind == "Secret" and .metadata.name == "automation-scheduler-application-grant");
  .data."application-grant.jws" = "")' "$temporary_directory/empty-key.yaml"
if "$classifier" --output "$temporary_directory/rejected-empty.json" --external-material-file "$temporary_directory/empty-key.yaml" >/dev/null 2>&1; then
  printf 'External material with an empty key was accepted\n' >&2
  exit 1
fi

cp "$external_fixture" "$temporary_directory/insecure.yaml"
chmod 0644 "$temporary_directory/insecure.yaml"
if "$classifier" --output "$temporary_directory/insecure-output.json" --external-material-file "$temporary_directory/insecure.yaml" >/dev/null 2>&1; then
  printf 'Insecure external material permissions were accepted\n' >&2
  exit 1
fi

if jq -r '.. | strings' "$policy" |
  grep -Eiq '(BEGIN [A-Z ]*PRIVATE KEY|password=|token=|postgres(ql)?://[^[:space:]]+@)'; then
  printf 'Application material policy contains a credential value\n' >&2
  exit 1
fi

printf 'Direct-production application material classification checks completed\n'
