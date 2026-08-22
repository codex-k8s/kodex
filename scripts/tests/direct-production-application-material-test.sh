#!/usr/bin/env bash
set -euo pipefail
umask 077

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
classifier="$repository_root/tools/deploy/classify-direct-production-application-material.sh"
materializer="$repository_root/tools/deploy/materialize-direct-production-application.sh"
policy="$repository_root/infra/direct-production/application-material-policy.json"
prototype_policy="$repository_root/infra/direct-production/internal-rpc-authority-prototype-material-policy.json"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
test_oidc_issuer="https://sso.example.test/realms/mattercodex"

materializer_existing_key_check=$(sed -n '/^verify_key_set_json()/,/^}/p' "$materializer")
grep -Fq '(($actual - $allowed) | length) == 0' <<<"$materializer_existing_key_check" &&
  ! grep -Fq '$expected - $actual' <<<"$materializer_existing_key_check" || {
  printf 'Materializer must preserve an allowed existing subset so new declared keys can be generated\n' >&2
  exit 1
}

printf '%s\n' '{"z":1,"a":{"y":2,"x":3}}' >"$temporary_directory/uncanonical.json"
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" canonicalize-json \
  "$temporary_directory/uncanonical.json" "$temporary_directory/canonical.json"
[[ "$(<"$temporary_directory/canonical.json")" == '{"a":{"x":3,"y":2},"z":1}' ]] &&
  node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-canonical-json \
    "$temporary_directory/canonical.json" || {
  printf 'Canonical JSON helper does not produce exact compact key ordering\n' >&2
  exit 1
}

node "$repository_root/tools/deploy/direct-production-material-helper.mjs" generate-jwk \
  "$temporary_directory/semantic-kid-private.jwk"
jq '.kid = "publisher-target-g1"' "$temporary_directory/semantic-kid-private.jwk" \
  >"$temporary_directory/semantic-kid-private.next.jwk"
mv "$temporary_directory/semantic-kid-private.next.jwk" "$temporary_directory/semantic-kid-private.jwk"
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-private-jwk \
  "$temporary_directory/semantic-kid-private.jwk"

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
  (.resources | length) == 157 and
  ([.resources[] | select(.kind == "Secret")] | length) == 138 and
  ([.resources[] | select(.kind == "ConfigMap")] | length) == 19 and
  .counts == {
    cryptographically_generated:72,
    deterministically_derived:74,
    safely_reusable_from_existing_binding:2,
    truly_external_credential:9
  } and
  all(.resources[];
    (.keys | type == "array" and length > 0 and length == (unique | length))) and
  ([.external_bindings[].keys[]] | length) == 14 and
  (.resources | group_by([.kind,.name]) | all(length == 1))
' "$classification" >/dev/null || {
  printf 'Direct-production application material classification is incomplete\n' >&2
  exit 1
}

jq -e '
  . as $policy |
  ([.external_bindings[] | [.kind,.name]] | length) ==
    ([.external_bindings[] | [.kind,.name]] | unique | length) and
  ([.external_bindings[] as $binding |
    any(.resources[];
      .kind == $binding.kind and .name == $binding.name and
      .classification == "truly_external_credential")] | all) and
  all(.external_bindings[];
    (.keys | length) > 0 and (.keys | length) == (.keys | unique | length) and
    (.requirement | type == "string" and length > 0)) and
  all(.owner_materialized_resources[]; . as $binding |
    (.requirement | type == "string" and length > 0) and
    any($policy.resources[];
      .kind == $binding.kind and .name == $binding.name and
      (.classification == "deterministically_derived" or
       .classification == "cryptographically_generated") and
      .keys == $binding.keys)) and
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
       .classification == "truly_external_credential" or
       .classification == "cryptographically_generated"))] | all) and
  ([.publisher_owned_empty_resources[] as $binding |
    any(.resources[];
      .kind == $binding.kind and .name == $binding.name and
      .classification == "deterministically_derived")] | all)
' "$policy" >/dev/null || {
  printf 'Application material policy has an ambiguous binding\n' >&2
  exit 1
}

jq -n --slurpfile authority "$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json" \
  --slurpfile material "$policy" -e '
    ($material[0].resources[] |
      select(.kind == "Secret" and .name == "control-plane-application-grants") |
      .keys) as $declared |
    all($authority[0].policy.authority_proof_producers[];
      .producer_id == "control-plane.oidc" or
      ((if (.application_credential == "MATTERMOST_SIGNED_EVENT" or
            .application_credential == "INTEGRATION_CONTINUATION_GRANT") then
          .producer_id + ".public-keyset.json"
        else
          .producer_id + ".public.jwk"
        end) as $required |
       ($declared | index($required)) != null))
  ' >/dev/null || {
  printf 'Control-plane application grant trust does not cover every authority producer\n' >&2
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
manifest_fixture='{"version":1,"source":"vault://mattercodex/interaction-gateway/mapping/production/revisions/production-r1","revision":"production-r1","channels":[{"team_id":"team","channel_id":"channel","organization_id":"11111111-1111-4111-8111-111111111111","project_id":"22222222-2222-4222-8222-222222222222","chat_id":"33333333-3333-4333-8333-333333333333","role_id":"44444444-4444-4444-8444-444444444444","locale":"ru","bot_stable_key":"primary","owner_delivery":true,"lifecycle_actor_id":"55555555-5555-4555-8555-555555555555"}],"actors":[{"mattermost_user_id":"owner","actor_id":"55555555-5555-4555-8555-555555555555","organization_id":"11111111-1111-4111-8111-111111111111","project_id":"22222222-2222-4222-8222-222222222222"}],"bots":[{"stable_key":"primary","user_id":"bot","token_file":"/var/run/secrets/mattercodex/interaction-gateway/bots/mattermost-bot-token"}]}'
manifest_digest=$(printf '%s' "$manifest_fixture" | sha256sum | awk '{print $1}')
authority_fixture="$temporary_directory/authority-fixture"
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" generate-jwk \
  "$temporary_directory/manifest-signer.private.jwk"
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" generate-jwk \
  "$temporary_directory/readback-signer.private.jwk"
(cd "$repository_root/services/internal/internal-rpc-authority" &&
  go run ./cmd/internal-rpc-authority-bootstrap-material \
    --manifest-signer "$temporary_directory/manifest-signer.private.jwk" \
    --readback-signer "$temporary_directory/readback-signer.private.jwk" \
    --output "$authority_fixture")
node - "$temporary_directory/git-state.json" "$temporary_directory/provider-snapshot.json" \
  "$temporary_directory/provider-snapshot.sha256" "$temporary_directory/provider-snapshot.generation" <<'NODE'
const {createHash, generateKeyPairSync} = require("node:crypto");
const {writeFileSync} = require("node:fs");
const sha = value => createHash("sha256").update(value).digest("hex");
const gitValue = Buffer.from("test-only-git-credential");
const record = {version:1,status:"ACTIVE",content_sha256:sha(gitValue),value:gitValue.toString("base64")};
const aggregateInput = {schema_version:1,generation:1,records:{"mattercodex/integration-gateway/git-credentials/matter-codex":record}};
writeFileSync(process.argv[2], JSON.stringify({...aggregateInput,digest_sha256:sha(JSON.stringify(aggregateInput))})+"\n", {mode:0o600});
const {publicKey} = generateKeyPairSync("rsa", {modulusLength:2048});
const exported = publicKey.export({format:"jwk"});
const key = {use:"sig",kty:"RSA",kid:"test-provider-key",alg:"RS256",n:exported.n,e:exported.e};
const snapshotInput = {schema_version:1,generation:7,issuer:"https://sso.example.test/realms/mattercodex",audience:"mattercodex-integration-gateway",algorithms:["RS256"],jwks:{keys:[key]}};
const digest = sha(JSON.stringify(snapshotInput));
writeFileSync(process.argv[3], JSON.stringify({...snapshotInput,digest_sha256:digest})+"\n", {mode:0o600});
writeFileSync(process.argv[4], digest+"\n", {mode:0o600});
writeFileSync(process.argv[5], "7\n", {mode:0o600});
NODE
JWS_FIXTURE="$jws_fixture" JWK_FIXTURE="$jwk_fixture" JWKS_FIXTURE="$jwks_fixture" \
MANIFEST_FIXTURE="$manifest_fixture" MANIFEST_DIGEST="$manifest_digest" \
CA_FIXTURE="$(base64 -w0 "$temporary_directory/test-ca.pem")" \
GIT_AGGREGATE="$(base64 -w0 "$temporary_directory/git-state.json")" \
OIDC_SNAPSHOT="$(base64 -w0 "$temporary_directory/provider-snapshot.json")" \
OIDC_SHA256="$(tr -d '\n' <"$temporary_directory/provider-snapshot.sha256")" \
OIDC_GENERATION="$(tr -d '\n' <"$temporary_directory/provider-snapshot.generation")" \
AUTHORITY_MANIFEST_SIGNER="$(base64 -w0 "$temporary_directory/manifest-signer.private.jwk")" \
AUTHORITY_READBACK_SIGNER="$(base64 -w0 "$temporary_directory/readback-signer.private.jwk")" \
AUTHORITY_MANIFEST_TRUST="$(base64 -w0 "$authority_fixture/external/publisher-manifest-trust.jws")" \
AUTHORITY_READBACK_MANIFEST_ROOT="$(base64 -w0 "$authority_fixture/external/readback-manifest-root.jws")" \
AUTHORITY_READBACK_TRUST="$(base64 -w0 "$authority_fixture/external/readback-credential-trust.jws")" jq -c '
  def value($name;$key):
    if $name == "integration-gateway-git-credentials" and $key == "state.json" then (env.GIT_AGGREGATE | @base64d)
    elif $name == "integration-gateway-oidc-provider" and $key == "provider-snapshot.json" then (env.OIDC_SNAPSHOT | @base64d)
    elif $name == "integration-gateway-oidc-provider" and $key == "provider-snapshot.sha256" then env.OIDC_SHA256
    elif $name == "integration-gateway-oidc-provider" and $key == "provider-snapshot.generation" then env.OIDC_GENERATION
    elif $name == "internal-rpc-authority-publisher-manifest-signer" and $key == "private.jwk" then (env.AUTHORITY_MANIFEST_SIGNER | @base64d)
    elif $name == "internal-rpc-authority-publisher-readback-signer" and $key == "private.jwk" then (env.AUTHORITY_READBACK_SIGNER | @base64d)
    elif $name == "internal-rpc-authority-publisher-manifest-trust" and $key == "manifest-trust.jws" then (env.AUTHORITY_MANIFEST_TRUST | @base64d)
    elif $name == "internal-rpc-authority-readback-trust" and $key == "manifest-root.jws" then (env.AUTHORITY_READBACK_MANIFEST_ROOT | @base64d)
    elif $name == "internal-rpc-authority-readback-trust" and $key == "credential-trust.jws" then (env.AUTHORITY_READBACK_TRUST | @base64d)
    elif ($key | test("(\\.jws|\\.jwt)$")) then env.JWS_FIXTURE
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
     data:($binding.keys | map({key:.,value:(value($binding.name;.) | @base64)}) | from_entries)}
  else
    {apiVersion:"v1",kind:$binding.kind,metadata:{name:$binding.name,namespace:"mattercodex-system"},
     data:($binding.keys | map({key:.,value:value($binding.name;.)}) | from_entries)}
  end
' "$policy" | yq -p=json -P >"$external_fixture"
"$classifier" --output "$temporary_directory/with-external.json" --external-material-file "$external_fixture" >/dev/null

material="$temporary_directory/application-material.yaml"
"$materializer" --mode render --oidc-issuer "$test_oidc_issuer" \
  --external-material-file "$external_fixture" --output "$material" >/dev/null
[[ "$(stat -c '%a' "$material")" == 600 ]] || {
  printf 'Application material render permissions are not 0600\n' >&2
  exit 1
}
yq -o=json eval-all '.' "$material" | jq -s --slurpfile classification "$classification" -e '
  map(select(.kind != null)) as $resources |
  ($resources | length) == 158 and
  ([$resources[] | select(.metadata.name != "agent-runner-handoff-trust") |
    [.kind,.metadata.name]] | sort) ==
    ([$classification[0].resources[] | [.kind,.name]] | sort) and
  ([$resources[] | select(.kind == "ConfigMap" and .metadata.name == "agent-runner-handoff-trust")] |
    length) == 1 and
  all($resources[] | select(.metadata.name != "agent-runner-handoff-trust"); . as $resource |
    ([((.data // {}) | keys[]),((.binaryData // {}) | keys[])] | unique | sort) ==
    ([$classification[0].resources[] | select(.kind == $resource.kind and .name == $resource.metadata.name) | .keys[]] | unique | sort)) and
  all($resources[] | select(.metadata.name != "agent-runner-handoff-trust"); . as $resource |
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
handoff_private="$temporary_directory/handoff-private.key"
handoff_public="$temporary_directory/handoff-public.key"
jq -er '.[] | select(.kind == "Secret" and .metadata.name == "agent-runner-handoff-key") |
  .data["ed25519.key"]' "$material_json" | base64 -d >"$handoff_private"
handoff_key_id="sha256-$(sha256sum "$handoff_private" | awk '{print substr($1,1,16)}')"
jq -er --arg key "$handoff_key_id" '.[] |
  select(.kind == "ConfigMap" and .metadata.name == "agent-runner-handoff-trust") |
  . as $resource |
  (((.data // {}) | length) == 0 and ((.binaryData // {}) | keys) == [$key]) |
  select(.) | $resource.binaryData[$key]' "$material_json" | base64 -d >"$handoff_public"
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" \
  validate-ed25519-keypair "$handoff_private" "$handoff_public"
for binding in \
  'control-plane-postgres-context:control-plane-postgres-context-migration' \
  'integration-gateway-postgres-context:integration-gateway-postgres-context-migration'; do
  runtime_secret=${binding%%:*}
  migration_secret=${binding#*:}
  runtime_key=$(jq -er --arg name "$runtime_secret" \
    '.[] | select(.kind == "Secret" and .metadata.name == $name) | .data.key' \
    "$material_json")
  migration_key=$(jq -er --arg name "$migration_secret" \
    '.[] | select(.kind == "Secret" and .metadata.name == $name) | .data.key' \
    "$material_json")
  [[ "$runtime_key" == "$migration_key" ]] || {
    printf 'PostgreSQL runtime and migration context keys differ: %s/%s\n' \
      "$runtime_secret" "$migration_secret" >&2
    exit 1
  }
done
for binding in \
  'control-plane-keyset-genesis:interaction-readback.public-keyset.json' \
  'control-plane-keyset-genesis:mattermost-event.public-keyset.json' \
  'interaction-gateway-postgres-migrator:delivery-readback.public-keyset.json'; do
  name=${binding%%:*}
  key=${binding#*:}
  genesis="$temporary_directory/$name-$key"
  jq -er --arg name "$name" --arg key "$key" \
    '.[] | select(.kind=="Secret" and .metadata.name==$name) | .data[$key]' \
    "$material_json" | base64 -d >"$genesis"
  node "$repository_root/tools/deploy/direct-production-material-helper.mjs" \
    validate-canonical-json "$genesis"
  jq -e '
      .version == 1 and .revision == 1 and .high_watermark == 1 and
      .served_generation == 1 and (.keys | length == 1) and
      all(.keys[];
        .generation == 1 and .status == "CURRENT" and
        .jwk.kty == "EC" and .jwk.crv == "P-256" and
        .jwk.alg == "ES256" and .jwk.use == "sig" and
        .jwk.key_ops == ["verify"] and
        (.jwk.x | type == "string" and length > 0) and
        (.jwk.y | type == "string" and length > 0) and .jwk.d == null)
    ' "$genesis" >/dev/null || {
      printf 'Generated public keyset genesis is invalid: %s/%s\n' "$name" "$key" >&2
      exit 1
    }
done
jq -er '.[] | select(.kind=="Secret" and .metadata.name=="interaction-gateway-runtime") |
  .data["delivery-readback.public-keyset.json"]' "$material_json" |
  base64 -d | jq -e '
    .version == 1 and .revision == 1 and .high_watermark == 1 and
    .served_generation == 1 and (.keys | length == 1) and all(.keys[];
      .generation == 1 and .status == "CURRENT" and
      .jwk.kty == "EC" and .jwk.crv == "P-256" and .jwk.alg == "ES256" and
      .jwk.use == "sig" and .jwk.key_ops == ["verify"] and
      (.jwk.x | type == "string" and length > 0) and
      (.jwk.y | type == "string" and length > 0) and .jwk.d == null)
  ' >/dev/null || {
  printf 'Generated runtime public JWKS is invalid\n' >&2
  exit 1
}
while IFS=$'\t' read -r name key; do
  jq -er --arg name "$name" --arg key "$key" \
    '.[] | select(.kind=="Secret" and .metadata.name==$name) | .data[$key]' \
    "$material_json" | base64 -d >"$temporary_directory/private-jwk"
  node "$repository_root/tools/deploy/direct-production-material-helper.mjs" \
    validate-private-jwk "$temporary_directory/private-jwk"
done < <(
  jq -r '.resources[] | select(.kind=="Secret") | .name as $name | .keys[] |
    select(. == "private.jwk" or . == "evidence-private.jwk" or
      . == "mattermost-event.private.jwk" or . == "provider-readback.private.jwk") |
    [$name,.] | @tsv' "$policy"
  jq -r '.runtime_owned_empty_resources[] | select(.kind=="Secret") | .name as $name | .keys[] |
    select(. == "private.jwk") | [$name,.] | @tsv' "$prototype_policy"
)
grep -Fq "openssl rand -hex 32 | tr -d '\\n'" "$materializer" &&
  grep -Fq "base64 -d | tr -d '\\n' >\"\$root\"" "$materializer" || {
  printf 'Application material hex generation or root readback is not canonical\n' >&2
  exit 1
}
for name in integration-gateway-provider-credentials interaction-gateway-bot-credentials; do
  jq -er --arg name "$name" '.[] | select(.kind == "Secret" and .metadata.name == $name) |
    .data["state.json"]' "$material_json" | base64 -d >"$temporary_directory/$name-state.json"
  node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-aggregate \
    "$temporary_directory/$name-state.json" 1024
  jq -e '.schema_version == 1 and .generation == 1 and .records == {}' \
    "$temporary_directory/$name-state.json" >/dev/null || {
    printf 'Dynamic credential aggregate does not start from the exact empty generation: %s\n' "$name" >&2
    exit 1
  }
done
jq -er '.[] | select(.kind == "Secret" and .metadata.name == "integration-gateway-git-credentials") |
  .data["state.json"]' "$material_json" | base64 -d >"$temporary_directory/rendered-git-state.json"
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-git-aggregate \
  "$temporary_directory/rendered-git-state.json" \
  "$repository_root/deploy/k8s/base/integration-gateway/git-sources/catalog.json"
for key in provider-snapshot.json provider-snapshot.sha256 provider-snapshot.generation; do
  jq -er --arg key "$key" '.[] | select(.kind == "ConfigMap" and .metadata.name == "integration-gateway-oidc-provider") |
    .data[$key]' "$material_json" >"$temporary_directory/rendered-$key"
done
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-oidc-snapshot \
  "$temporary_directory/rendered-provider-snapshot.json" \
  "$temporary_directory/rendered-provider-snapshot.sha256" \
  "$temporary_directory/rendered-provider-snapshot.generation" "$test_oidc_issuer"
for name in control-plane-nats control-plane-nats-bootstrap control-api-gateway-nats; do
  jq -er --arg name "$name" '.[] | select(.kind=="Secret" and .metadata.name==$name) | .data["user.creds"]' "$material_json" |
    base64 -d >"$temporary_directory/$name.creds"
done
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-nats-creds \
  "$temporary_directory/control-plane-nats.creds" control-plane \
  '$JS.API.STREAM.INFO.CONTROL_PLANE,control_plane.platform.*.events,control_plane.run.*.*.events' '_INBOX.>' '' ''
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-nats-creds \
  "$temporary_directory/control-plane-nats-bootstrap.creds" control-plane-bootstrap \
  '$JS.API.STREAM.CREATE.CONTROL_PLANE,$JS.API.STREAM.INFO.CONTROL_PLANE' '_INBOX.>' '' ''
node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-nats-creds \
  "$temporary_directory/control-api-gateway-nats.creds" control-api-gateway '' \
  'control_plane.platform.*.events,control_plane.run.*.*.events' '>' ''
jq -er '.[] | select(.kind=="Secret" and .metadata.name=="control-plane-postgres-runtime") | .data.dsn' "$material_json" |
  base64 -d >"$temporary_directory/postgres-dsn"
grep -Eq '^postgresql://[^:]+:[a-f0-9]{64}@control-plane-postgresql-rw\.mattercodex-system\.svc\.cluster\.local:5432/control_plane\?sslmode=verify-full&sslrootcert=/var/run/config/mattercodex/control-plane/postgres/ca\.pem&options=-c%20role%3Dcontrol_plane_runtime$' \
  "$temporary_directory/postgres-dsn" || {
    printf 'Generated PostgreSQL DSN semantics are invalid\n' >&2
    exit 1
  }
jq -er '.[] | select(.kind=="Secret" and .metadata.name=="interaction-gateway-runtime") | .data["postgres-dsn"]' "$material_json" |
  base64 -d >"$temporary_directory/interaction-postgres-dsn"
grep -Eq '^postgresql://interaction_gateway_runtime_g2:[a-f0-9]{64}@interaction-gateway-postgresql-rw\.mattercodex-system\.svc\.cluster\.local:5432/interaction_gateway\?sslmode=verify-full&sslrootcert=/var/run/config/mattercodex/interaction-gateway/postgres/ca\.pem&options=-c%20role%3Dinteraction_gateway_runtime$' \
  "$temporary_directory/interaction-postgres-dsn" || {
    printf 'Generated interaction-gateway PostgreSQL DSN does not use the active generation\n' >&2
    exit 1
  }
for migration_binding in \
  'control-plane-postgres-migration:control_plane_migrator' \
  'integration-gateway-postgres-migrator:integration_gateway_migrator_g1' \
  'interaction-gateway-postgres-migrator:interaction_gateway_migrator' \
  'internal-rpc-authority-migrator-postgresql:internal_rpc_authority_migrator' \
  'runtime-controller-postgres-migration:runtime_controller_migrator'; do
  migration_secret=${migration_binding%%:*}
  migration_principal=${migration_binding#*:}
  jq -er --arg name "$migration_secret" \
    '.[] | select(.kind=="Secret" and .metadata.name==$name) | .data.dsn' "$material_json" |
    base64 -d >"$temporary_directory/$migration_secret-dsn"
  grep -Eq "^postgresql://${migration_principal}:[a-f0-9]{64}@" \
    "$temporary_directory/$migration_secret-dsn" &&
    ! grep -Fq '&options=-c%20role%3D' "$temporary_directory/$migration_secret-dsn" || {
    printf 'Generated PostgreSQL migration DSN does not preserve the migrator session role: %s\n' "$migration_secret" >&2
    exit 1
  }
done
jq -er '.[] | select(.kind=="Secret" and .metadata.name=="control-api-gateway-public-tls-material") | .data["tls.crt"]' "$material_json" |
  base64 -d >"$temporary_directory/control-api.crt"
jq -er '.[] | select(.kind=="Secret" and .metadata.name=="control-api-gateway-public-tls-material") | .data["ca.crt"]' "$material_json" |
  base64 -d >"$temporary_directory/control-api-ca.crt"
openssl verify -CAfile "$temporary_directory/control-api-ca.crt" \
  -verify_hostname control-api.mattercodex.local "$temporary_directory/control-api.crt" >/dev/null 2>&1 || {
  printf 'Generated TLS hostname is invalid\n' >&2
  exit 1
}
if openssl verify -CAfile "$temporary_directory/control-api-ca.crt" \
  -verify_hostname wrong-control-api.mattercodex.local "$temporary_directory/control-api.crt" >/dev/null 2>&1; then
  printf 'TLS hostname verification accepted an unrelated hostname\n' >&2
  exit 1
fi

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
[[ "$(jq -r '.[] | select(.kind=="ConfigMap" and .metadata.name=="mattercodex-postgresql-principal-bootstrap") |
  .data["admin-memberships.tsv"]' "$foundation_json" | awk -F '\t' 'NF == 2 {count++} END {print count+0}')" == 40 ]] || {
  printf 'PostgreSQL bounded administrator registry is incomplete\n' >&2
  exit 1
}
for membership in \
  $'control_plane_role_controller\tcontrol_plane_runtime_g1' \
  $'integration_gateway_role_controller\tintegration_gateway_runtime_g1' \
  $'integration_gateway_role_controller\tintegration_gateway_runtime_g2' \
  $'interaction_gateway_role_controller\tinteraction_gateway_runtime_g1' \
  $'interaction_gateway_role_controller\tinteraction_gateway_runtime_g2' \
  $'internal_rpc_authority_credential_lifecycle_definer\tinternal_rpc_authority_publisher' \
  $'internal_rpc_authority_credential_lifecycle_definer\tinternal_rpc_authority_readback_attestor'; do
  jq -er '.[] | select(.kind=="ConfigMap" and .metadata.name=="mattercodex-postgresql-principal-bootstrap") |
    .data["admin-memberships.tsv"]' "$foundation_json" | grep -Fxq "$membership" || {
    printf 'Required PostgreSQL bounded administrator membership is absent\n' >&2
    exit 1
  }
done
[[ "$(jq -r '.[] | select(.kind=="ConfigMap" and .metadata.name=="mattercodex-postgresql-principal-bootstrap") |
  .data["retired-principals.txt"]' "$foundation_json" | rg -c '^interaction_gateway_runtime_g1$')" == 1 ]] || {
  printf 'Previous interaction-gateway PostgreSQL principal is not retired\n' >&2
  exit 1
}
for principal in \
  ira_publisher_g3 ira_publisher_g4 ira_publisher_g5 \
  ira_readback_attestor_g3 ira_readback_attestor_g4 ira_readback_attestor_g5; do
  jq -er '.[] | select(.kind=="ConfigMap" and .metadata.name=="mattercodex-postgresql-principal-bootstrap") |
    .data["principals.tsv"]' "$foundation_json" |
    awk -F '\t' -v principal="$principal" '
      $1 == principal && $2 == "internal_rpc_authority" && $3 == "" && $4 == "false" { found = 1 }
      END { exit(found ? 0 : 1) }
    ' || {
    printf 'Credential generation principal has a bootstrap-owned capability: %s\n' "$principal" >&2
    exit 1
  }
done
rg -q 'ira_publisher_g\[1-5\]|ira_readback_attestor_g\[1-5\]' \
  "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'ALTER ROLE %s PASSWORD' "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'fenced lifecycle reconciler exclusively owns generation memberships' \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -Fq 'ira_publisher_g[1-5]|ira_readback_attestor_g[1-5]) continue' \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -Fq "login.rolname !~ '^ira_publisher_g[1-5]$'" \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -Fq "login.rolname !~ '^ira_readback_attestor_g[1-5]$'" \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'actual_static_login' "$temporary_directory/postgresql-principal-reconcile.sh" || {
  printf 'Credential generation LOGIN ownership is not preserved by PostgreSQL bootstrap\n' >&2
  exit 1
}
rg -q 'WITH INHERIT FALSE, SET FALSE, ADMIN TRUE' "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'dependent lifecycle administrator grant is invalid' \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'generation grants are owned by the fenced lifecycle reconciler' \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'DECLARE admin_grantor text' "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q "REVOKE %%I FROM %%I GRANTED BY %%I.*admin_grantor" \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'actual_admin_membership.*member.admin_option.*NOT member.inherit_option.*NOT member.set_option' \
    "$temporary_directory/postgresql-principal-reconcile.sh" || {
  printf 'PostgreSQL bounded administrator readback is incomplete\n' >&2
  exit 1
}
rg -q 'CREATE TABLE IF NOT EXISTS public.goose_db_version' \
  "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'ALTER TABLE public.goose_db_version OWNER TO \$owner_role' \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.goose_db_version TO \$principal' \
    "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'has_sequence_privilege' "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q 'has_schema_privilege' "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q '\[ "\$migration_principals" -eq 5 \] \|\| exit 34' \
    "$temporary_directory/postgresql-principal-reconcile.sh" || {
  printf 'PostgreSQL Goose bootstrap ownership is incomplete\n' >&2
  exit 1
}
rg -q 'ALTER ROLE %s NOLOGIN.*pg_terminate_backend' "$temporary_directory/postgresql-principal-reconcile.sh" &&
  rg -q "format\('REVOKE %%I FROM %%I GRANTED BY %%I'" "$temporary_directory/postgresql-principal-reconcile.sh" || {
  printf 'PostgreSQL retirement boundary is incomplete\n' >&2
  exit 1
}
jq -e '
  first(.[] | select(.kind == "ConfigMap" and .metadata.name == "mattercodex-nats-config") |
    .data["nats.conf"]) as $config |
  ($config | contains("operator: __NATS_OPERATOR_JWT__")) and
  ($config | contains("system_account: __NATS_SYSTEM_ACCOUNT__")) and
  ($config | contains("resolver: MEMORY")) and
  ($config | contains("__NATS_SYSTEM_ACCOUNT__: __NATS_SYSTEM_ACCOUNT_JWT__")) and
  ($config | contains("__NATS_APPLICATION_ACCOUNT__: __NATS_APPLICATION_ACCOUNT_JWT__")) and
  ($config | contains("verify: true")) and
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

application_bootstrap="$temporary_directory/application-bootstrap.yaml"
"$repository_root/tools/release/render-direct-production-applications.sh" \
  --scope bootstrap --output "$application_bootstrap" >/dev/null
target_registry="$temporary_directory/key-delivery-targets.yaml"
yq -r 'select(.kind == "ConfigMap" and
  .metadata.name == "internal-rpc-authority-publisher-target-registry") |
  .data."key-delivery-targets.yaml"' "$interfaces" >"$target_registry"
target_registry_json="$temporary_directory/target-registry.json"
yq -o=json '.targets' "$target_registry" >"$target_registry_json"
yq -e '.source_revision == 1' "$target_registry" >/dev/null || {
  printf 'Publisher target registry does not use the forward-only profile revision\n' >&2
  exit 1
}
jq -e '
  length == 10 and
  ([.[] | [.workload_id,.role]] | unique | length) == 10 and
  ([.[] | [
    .auth_private_key.vault_path?,.manifest_trust.vault_path?,
    .authority_proof_trust.vault_path?,.authority_proof_private_key.vault_path?,
    .restore_coordination.role_credential_vault_path,
    .restore_coordination.ack_key_vault_path,
    .readback.credential_vault_path,.readback.possession_key_vault_path
  ][] | select(. != null)] | length) == 64 and
  ([.[] | [
    .auth_private_key.vault_path?,.manifest_trust.vault_path?,
    .authority_proof_trust.vault_path?,.authority_proof_private_key.vault_path?,
    .restore_coordination.role_credential_vault_path,
    .restore_coordination.ack_key_vault_path,
    .readback.credential_vault_path,.readback.possession_key_vault_path
  ][] | select(. != null)] | unique | length) == 64 and
  any(.[]; .workload_id == "integration-gateway" and .role == "AUTHORIZATION_VERIFIER" and
    .service_account == "integration-gateway" and
    .database_identity.login_principal == "ira_integration_gateway_verifier_g1" and
    .readback.credential_vault_path == "kv/data/mattercodex/internal-rpc-authority/integration-gateway/verifier/readback-credential") and
  any(.[]; .workload_id == "runtime-controller" and .role == "AUTHORIZATION_ISSUER" and
    .service_account == "runtime-controller" and
    .database_identity.login_principal == "ira_runtime_controller_issuer_g1" and
    .auth_private_key.vault_path == "kv/data/mattercodex/internal-rpc-authority/runtime-controller/issuer/auth-private") and
  any(.[]; .workload_id == "legacy-data-migration" and .role == "AUTHORIZATION_ISSUER" and
    .service_account == "legacy-data-migration" and
    .database_identity.login_principal == "ira_legacy_data_migration_issuer_g1" and
    .auth_private_key.vault_path == "kv/data/mattercodex/internal-rpc-authority/legacy-data-migration/issuer/auth-private") and
  (any(.[]; .workload_id == "runtime-s3-restore-exchanger" or
    .workload_id == "role-image-builder" or
    .workload_id == "image-admission" or
    .workload_id == "image-promotion") | not)
' "$target_registry_json" >/dev/null || {
  printf 'Publisher target registry does not close the active release profiles\n' >&2
  exit 1
}

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
  "$application_bootstrap" | jq -e '
    .rules == [{apiGroups:[""],resources:["secrets"],resourceNames:.rules[0].resourceNames,verbs:["get","update"]}] and
    (.rules[0].resourceNames | length) == 32
  ' >/dev/null || {
  printf 'Publisher RBAC contains a forbidden resource or verb\n' >&2
  exit 1
}
yq -r 'select(.kind == "Role" and .metadata.name == "internal-rpc-authority-publisher") |
  .rules[0].resourceNames[]' "$application_bootstrap" | LC_ALL=C sort -u >"$actual_publisher_resources"
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
  (any(.[]; .metadata.name == "runtime-s3-restore-exchanger") | not)
' >/dev/null || {
  printf 'Active release profiles do not have exact file-only delivery mounts\n' >&2
  exit 1
}
if rg -q 'internal-rpc-authority-runtime-restore-effect|ira_runtime_restore_effect' \
  "$repository_root/deploy" "$repository_root/infra" "$repository_root/tools"; then
  printf 'Runtime restore authority identity alias remains in infrastructure\n' >&2
  exit 1
fi

yq -o=json eval-all '.' "$interfaces" | jq -s -e '
  def exact_adapter($deployment; $name):
    $deployment.spec.template.spec.automountServiceAccountToken == false and
    $deployment.spec.template.metadata.labels["mattercodex.dev/runtime-secret-api"] == $name and
    any($deployment.spec.template.spec.containers[]; .name == $name and
      any(.volumeMounts[]; .name == "direct-kubernetes-api-token" and .readOnly == true and
        .mountPath == "/var/run/secrets/tokens/kubernetes-api") and
      any(.volumeMounts[]; .name == "direct-kubernetes-api-ca" and .readOnly == true and
        .mountPath == "/var/run/config/kubernetes.io/serviceaccount")) and
    ([$deployment.spec.template.spec.volumes[]? | select(.projected.sources[]?.serviceAccountToken != null)] | length) == 1 and
    all($deployment.spec.template.spec.containers[] | select(.name != $name);
      all(.volumeMounts[]?; (.name != "direct-kubernetes-api-token" and .name != "direct-kubernetes-api-ca" and
        .name != "direct-git-credentials" and .name != "direct-oidc-provider"))) and
    all($deployment.spec.template.spec.initContainers[]?;
      all(.volumeMounts[]?; (.name != "direct-kubernetes-api-token" and .name != "direct-kubernetes-api-ca" and
        .name != "direct-git-credentials" and .name != "direct-oidc-provider"))) and
    any($deployment.spec.template.spec.volumes[]; .name == "direct-kubernetes-api-token" and .projected.defaultMode == 256 and
      .projected.sources == [{"serviceAccountToken":{"path":"token","expirationSeconds":600}}]) and
    any($deployment.spec.template.spec.volumes[]; .name == "direct-kubernetes-api-ca" and
      .configMap == {"name":"kube-root-ca.crt","defaultMode":288,"items":[{"key":"ca.crt","path":"ca.crt"}]});
  map(select(.kind != null)) as $resources |
  first($resources[] | select(.kind == "ConfigMap" and .metadata.name == "integration-gateway-runtime")) as $integration_config |
  first($resources[] | select(.kind == "ConfigMap" and .metadata.name == "interaction-gateway-runtime")) as $interaction_config |
  first($resources[] | select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher")) as $publisher_config |
  first($resources[] | select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-database-credential-reconciler")) as $reconciler_config |
  first($resources[] | select(.kind == "ConfigMap" and .metadata.name == "control-plane-runtime")) as $control_config |
  first($resources[] | select(.kind == "ConfigMap" and .metadata.name == "runtime-controller-runtime")) as $runtime_config |
  first($resources[] | select(.kind == "Deployment" and .metadata.name == "control-plane")) as $control |
  first($resources[] | select(.kind == "Deployment" and .metadata.name == "runtime-workload-admission")) as $admission |
  first($resources[] | select(.kind == "Deployment" and .metadata.name == "integration-gateway")) as $integration |
  first($resources[] | select(.kind == "Deployment" and .metadata.name == "interaction-gateway")) as $interaction |
  ($integration_config.data.INTEGRATION_GATEWAY_DEPLOYMENT_PROFILE == "direct-production-single-node-prototype") and
  ($integration_config.data.INTEGRATION_GATEWAY_SECRET_BACKEND == "direct-production-kubernetes-file") and
  ($integration_config.data.INTEGRATION_GATEWAY_OIDC_VERIFIER_BACKEND == "direct-production-file") and
  ($integration_config.data | keys | all(startswith("INTEGRATION_GATEWAY_VAULT_") | not)) and
  ($interaction_config.data.INTERACTION_GATEWAY_DEPLOYMENT_PROFILE == "direct-production-single-node-prototype") and
  ($interaction_config.data.INTERACTION_GATEWAY_BOT_CREDENTIAL_BACKEND == "direct-production-kubernetes-file") and
  ($interaction_config.data | keys | all(startswith("INTERACTION_GATEWAY_BOT_CREDENTIAL_VAULT_") | not)) and
  ($publisher_config.data | keys | all(test("^INTERNAL_RPC_AUTHORITY_(PUBLISHER_)?VAULT_") | not)) and
  ($reconciler_config.data | keys | all(test("^INTERNAL_RPC_AUTHORITY_(PUBLISHER_)?VAULT_") | not)) and
  ($control_config.data.CONTROL_PLANE_RUNTIME_ARCHIVE_RESTORE_CAPABILITY == "disabled") and
  ($control_config.data.CONTROL_PLANE_RUNTIME_ARCHIVE_SIGNING_KEY_FILE == null) and
  ($control_config.data.CONTROL_PLANE_RUNTIME_RESTORE_SIGNING_KEY_FILE == null) and
  ($runtime_config.data.RUNTIME_ARCHIVE_RESTORE_CAPABILITY == "disabled") and
  ($runtime_config.data.RUNTIME_EXECUTION_CAPABILITY == "disabled") and
  ($runtime_config.data.RUNTIME_ARCHIVE_RESTORE_FOLLOW_UP_ISSUE == "https://github.com/codex-k8s/matter-codex/issues/310") and
  (all($runtime_config.data | keys[];
    . == "RUNTIME_ARCHIVE_RESTORE_CAPABILITY" or
    . == "RUNTIME_ARCHIVE_RESTORE_FOLLOW_UP_ISSUE" or
    (test("^RUNTIME_(S3|ARCHIVE|RESTORE)") | not))) and
  (($control.spec.template.spec.volumes[] | select(.name == "runtime-workload-signing")).secret.items == [{"key":"admission-private-key.hex","path":"admission-private-key.hex"}]) and
  (any($admission.spec.template.spec.containers[] | select(.name == "admission");
    any(.env[]?; .name == "RUNTIME_ARCHIVE_RESTORE_CAPABILITY" and .value == "disabled") and
    (any(.env[]?; .name == "RUNTIME_ADMISSION_S3_ARCHIVE_PUBLIC_KEY_FILE" or .name == "RUNTIME_ADMISSION_S3_RESTORE_PUBLIC_KEY_FILE" or .name == "RUNTIME_S3_READBACK_IMAGE") | not))) and
  (($admission.spec.template.spec.volumes[] | select(.name == "ticket-trust")).secret.items == [{"key":"public-key.hex","path":"public-key.hex"}]) and
  exact_adapter($integration; "integration-gateway") and
  exact_adapter($interaction; "interaction-gateway") and
  any($integration.spec.template.spec.volumes[]; .name == "direct-git-credentials" and
    .secret.secretName == "integration-gateway-git-credentials" and .secret.items == [{"key":"state.json","path":"state.json"}]) and
  any($integration.spec.template.spec.volumes[]; .name == "direct-oidc-provider" and
    .configMap.name == "integration-gateway-oidc-provider" and
    .configMap.items == [{"key":"provider-snapshot.json","path":"provider-snapshot.json"}]) and
  ([$resources[] | select(.metadata.name | test("^(runtime-(archive|restore-verifier|rehydrate)(-|$)|runtime-s3-(archive|restore)(-|$)|runtime-s3-(exchanger|readback)-|runtime-controller-(archive-workers-s3|s3-security-policy)$)"))] | length) == 0
' >/dev/null || {
  printf 'Direct runtime adapter render is not exact or leaks its API token\n' >&2
  exit 1
}

for gateway in integration-gateway interaction-gateway; do
  if [[ "$gateway" == integration-gateway ]]; then
    adapter_secret=integration-gateway-provider-credentials
    adapter_role=integration-gateway-provider-credential-runtime
  else
    adapter_secret=interaction-gateway-bot-credentials
    adapter_role=interaction-gateway-bot-credential-runtime
  fi
  yq -o=json 'select(.kind == "Role" and .metadata.name == "'"$adapter_role"'")' \
    "$repository_root/deploy/k8s/base/$gateway/runtime-adapter-rbac.yaml" | jq -e \
    --arg secret "$adapter_secret" '
      .rules == [{apiGroups:[""],resources:["secrets"],resourceNames:[$secret],verbs:["get","update"]}]
    ' >/dev/null || {
    printf 'Runtime adapter RBAC contains a forbidden resource or verb: %s\n' "$gateway" >&2
    exit 1
  }
  yq -o=json 'select(.kind == "RoleBinding" and .metadata.name == "'"$adapter_role"'")' \
    "$repository_root/deploy/k8s/base/$gateway/runtime-adapter-rbac.yaml" | jq -e \
    --arg gateway "$gateway" --arg role "$adapter_role" '
      .subjects == [{kind:"ServiceAccount",name:$gateway}] and
      .roleRef == {apiGroup:"rbac.authorization.k8s.io",kind:"Role",name:$role}
    ' >/dev/null || {
    printf 'Runtime adapter RoleBinding crosses the exact service account boundary: %s\n' "$gateway" >&2
    exit 1
  }
done
grep -Fq 'application-grant-rotator-kubernetes-api-exact|app.kubernetes.io/name=application-grant-rotator' "$repository_root/infra/direct-production/bootstrap.sh" &&
  grep -Fq 'integration-gateway-kubernetes-api-exact|mattercodex.dev/runtime-secret-api=integration-gateway' "$repository_root/infra/direct-production/bootstrap.sh" &&
  grep -Fq 'interaction-gateway-kubernetes-api-exact|mattercodex.dev/runtime-secret-api=interaction-gateway' "$repository_root/infra/direct-production/bootstrap.sh" &&
  grep -Fq 'mattercodex.dev/runtime-secret-api' "$repository_root/infra/direct-production/bootstrap.yaml" || {
  printf 'Owner bootstrap does not bind exact Kubernetes API egress and VAP boundaries\n' >&2
  exit 1
}

yq -o=json eval-all '.' "$application_bootstrap" | jq -s -e '
  map(select(.kind != null)) as $resources |
  ([$resources[] | select(.metadata.name | test("^(runtime-(archive|restore-verifier|rehydrate)(-|$)|runtime-s3-(archive|restore)(-|$)|runtime-s3-(exchanger|readback)-|runtime-controller-(archive-workers-s3|s3-security-policy)$)"))] | length) == 0 and
  (first($resources[] | select(.kind == "NetworkPolicy" and .metadata.name == "runtime-controller-workers-exact-paths")) |
    .spec.podSelector.matchExpressions[0].values == ["runtime-cleanup-authorizer"])
' >/dev/null || {
  printf 'Disabled archive/restore bootstrap resources remain reachable\n' >&2
  exit 1
}
yq -o=json eval-all '.' "$repository_root/infra/direct-production/bootstrap.yaml" | jq -s -e '
  any(.[]; .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "mattercodex-runtime-archive-restore-disabled" and
    .spec.failurePolicy == "Fail" and
    any(.spec.validations[]; .expression | contains("CONTROL_PLANE_RUNTIME_ARCHIVE_RESTORE_CAPABILITY")) and
    any(.spec.validations[]; .expression | contains("RUNTIME_ARCHIVE_RESTORE_CAPABILITY")) and
    any(.spec.validations[]; .expression | contains("runtime-controller-s3-security-policy"))) and
  any(.[]; .kind == "ValidatingAdmissionPolicyBinding" and
    .metadata.name == "mattercodex-runtime-archive-restore-disabled" and
    .spec.validationActions == ["Deny"])
' >/dev/null || {
  printf 'Disabled archive/restore admission boundary is absent\n' >&2
  exit 1
}

cp "$external_fixture" "$temporary_directory/missing-key.yaml"
yq -i '
  with(select(.kind == "Secret" and
    .metadata.name == "integration-gateway-provider-health-credential");
    del(.data.token))
' "$temporary_directory/missing-key.yaml"
if "$classifier" --output "$temporary_directory/rejected.json" --external-material-file "$temporary_directory/missing-key.yaml" >/dev/null 2>&1; then
  printf 'Incomplete external material was accepted\n' >&2
  exit 1
fi

cp "$external_fixture" "$temporary_directory/extra-key.yaml"
yq -i 'with(select(.kind == "Secret" and .metadata.name == "integration-gateway-provider-health-credential");
  .data.unexpected = "Zml4dHVyZQ==")' "$temporary_directory/extra-key.yaml"
if "$classifier" --output "$temporary_directory/rejected-extra.json" --external-material-file "$temporary_directory/extra-key.yaml" >/dev/null 2>&1; then
  printf 'External material with an extra key was accepted\n' >&2
  exit 1
fi

cp "$external_fixture" "$temporary_directory/empty-key.yaml"
yq -i 'with(select(.kind == "Secret" and .metadata.name == "integration-gateway-provider-health-credential");
  .data.token = "")' "$temporary_directory/empty-key.yaml"
if "$classifier" --output "$temporary_directory/rejected-empty.json" --external-material-file "$temporary_directory/empty-key.yaml" >/dev/null 2>&1; then
  printf 'External material with an empty key was accepted\n' >&2
  exit 1
fi

jq '.digest_sha256 = ("0" * 64)' "$temporary_directory/integration-gateway-provider-credentials-state.json" \
  >"$temporary_directory/invalid-aggregate.json"
if node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-aggregate \
  "$temporary_directory/invalid-aggregate.json" 1024 >/dev/null 2>&1; then
  printf 'Aggregate with an invalid digest was accepted\n' >&2
  exit 1
fi
printf '%s\n' 6 >"$temporary_directory/rollback-generation"
if node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-oidc-snapshot \
  "$temporary_directory/rendered-provider-snapshot.json" \
  "$temporary_directory/rendered-provider-snapshot.sha256" \
  "$temporary_directory/rollback-generation" "$test_oidc_issuer" >/dev/null 2>&1; then
  printf 'OIDC provider snapshot generation rollback was accepted\n' >&2
  exit 1
fi
jq '.records = {}' "$temporary_directory/rendered-git-state.json" >"$temporary_directory/incomplete-git-state.json"
if node "$repository_root/tools/deploy/direct-production-material-helper.mjs" validate-git-aggregate \
  "$temporary_directory/incomplete-git-state.json" \
  "$repository_root/deploy/k8s/base/integration-gateway/git-sources/catalog.json" >/dev/null 2>&1; then
  printf 'Incomplete Git credential aggregate was accepted\n' >&2
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
