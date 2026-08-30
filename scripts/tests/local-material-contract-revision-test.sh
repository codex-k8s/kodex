#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local material contract revision test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
source_script="$repository_root/tools/dev/reconcile-local-material.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fixture_root="$temporary_directory/repository"
state_directory="$temporary_directory/state"
cluster_state="$temporary_directory/cluster"
fake_bin="$temporary_directory/bin"
kubeconfig="$temporary_directory/kubeconfig"
mkdir -p "$fixture_root/tools/dev" "$fixture_root/tools/install" "$fixture_root/tools/deploy" \
  "$state_directory/provider-accounts/default" "$state_directory/cache" "$cluster_state" "$fake_bin"
cp "$source_script" "$fixture_root/tools/dev/reconcile-local-material.sh"
for path in \
  tools/install/secret-projections.json \
  tools/install/generate-material.sh \
  tools/install/materialize-secrets.sh \
  tools/install/nats-runtime-users.tsv \
  tools/deploy/materialize-nats-operator-files.sh \
  tools/install/materialize-nats-runtime-users.sh \
  tools/install/reconcile-nats-runtime-users.sh; do
  mkdir -p "$fixture_root/$(dirname -- "$path")"
  printf 'fixture:%s\n' "$path" >"$fixture_root/$path"
done
printf 'fixture\n' >"$kubeconfig"
printf 'credential\n' >"$state_directory/credentials.env"
printf 'provider\n' >"$state_directory/provider-accounts/default/auth.json"
printf 'cache\n' >"$state_directory/cache/preserved"
chmod 0700 "$state_directory" "$state_directory/provider-accounts" \
  "$state_directory/provider-accounts/default" "$state_directory/cache"
chmod 0600 "$state_directory/credentials.env" "$state_directory/provider-accounts/default/auth.json"

cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'config current-context') printf 'fixture-local\n' ;;
  'get --raw=/readyz') printf 'ok\n' ;;
  'get namespace/kodex-system -o json')
    [[ -f "${KODEX_TEST_CLUSTER_STATE:?}/kodex-system" ]] || exit 1
    cat "${KODEX_TEST_CLUSTER_STATE:?}/kodex-system"
    ;;
  'get namespace/identity -o json')
    [[ -f "${KODEX_TEST_CLUSTER_STATE:?}/identity" ]] || exit 1
    cat "${KODEX_TEST_CLUSTER_STATE:?}/identity"
    ;;
  'get namespace/kodex-system') [[ -f "${KODEX_TEST_CLUSTER_STATE:?}/kodex-system" ]] ;;
  'get namespace/identity') [[ -f "${KODEX_TEST_CLUSTER_STATE:?}/identity" ]] ;;
  'delete namespace kodex-system identity --wait=false')
    rm -f "${KODEX_TEST_CLUSTER_STATE:?}/kodex-system" "${KODEX_TEST_CLUSTER_STATE:?}/identity"
    ;;
  *) printf 'unexpected kubectl call: %s\n' "$*" >&2; exit 1 ;;
esac
EOF
chmod +x "$fake_bin/kubectl" "$fixture_root/tools/dev/reconcile-local-material.sh"
export PATH="$fake_bin:$PATH"
export KUBECONFIG="$kubeconfig"
export KODEX_TEST_CLUSTER_STATE="$cluster_state"

create_material_fixture() {
  local certificate_file
  mkdir -p "$state_directory/material/nats"
  printf 'digest\n' >"$state_directory/material/projections.sha256"
  printf '3\n' >"$state_directory/material/nats/runtime-user-policy.version"
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=fixture.local' \
    -keyout "$temporary_directory/fixture.key" \
    -out "$temporary_directory/fixture.crt" >/dev/null 2>&1
  for certificate_file in \
    node-pull/client.crt \
    projections/kodex-buildkit-tls/server.crt \
    projections/kodex-image-registry-pull/tls.crt \
    projections/kodex-image-registry-promotion/tls.crt \
    projections/role-image-builder-buildkit-client/tls.crt; do
    mkdir -p "$state_directory/material/$(dirname -- "$certificate_file")"
    cp "$temporary_directory/fixture.crt" \
      "$state_directory/material/$certificate_file"
  done
}

action=$("$fixture_root/tools/dev/reconcile-local-material.sh" \
  --context fixture-local --state-directory "$state_directory" --mode reconcile)
[[ "$action" == create ]] || fail 'fresh state did not request material creation'

create_material_fixture
"$fixture_root/tools/dev/reconcile-local-material.sh" \
  --context fixture-local --state-directory "$state_directory" --mode commit >/dev/null
marker="$state_directory/material-contract-revision.json"
[[ "$(stat -c '%a' "$marker")" == 600 ]] || fail 'revision marker is not private'
jq -e '
  .version == 1 and
  (.secretProjectionContractSHA256 | test("^[a-f0-9]{64}$")) and
  (.natsMaterialContractSHA256 | test("^[a-f0-9]{64}$"))
' "$marker" >/dev/null || fail 'revision marker contract is invalid'
action=$("$fixture_root/tools/dev/reconcile-local-material.sh" \
  --context fixture-local --state-directory "$state_directory" --mode reconcile)
[[ "$action" == reuse ]] || fail 'matching revision was not reused'

printf '%s\n' '{"metadata":{"labels":{"app.kubernetes.io/part-of":"kodex","kodex.dev/environment":"staging","kodex.dev/local-profile":"hot-reload"}}}' \
  >"$cluster_state/kodex-system"
printf '%s\n' '{"metadata":{"labels":{"app.kubernetes.io/part-of":"kodex","kodex.dev/capability":"identity"}}}' \
  >"$cluster_state/identity"
printf 'radar\n' >"$cluster_state/radar-dev"
printf 'changed\n' >>"$fixture_root/tools/install/secret-projections.json"
action=$("$fixture_root/tools/dev/reconcile-local-material.sh" \
  --context fixture-local --state-directory "$state_directory" --mode reconcile)
[[ "$action" == recreate ]] || fail 'contract drift did not request recreation'
[[ ! -e "$state_directory/material" && ! -e "$marker" ]] ||
  fail 'stale material or marker survived reconciliation'
[[ ! -e "$cluster_state/kodex-system" && ! -e "$cluster_state/identity" ]] ||
  fail 'local application namespaces survived reconciliation'
[[ -e "$cluster_state/radar-dev" ]] || fail 'unrelated radar-dev state was touched'
for preserved in credentials.env provider-accounts/default/auth.json cache/preserved; do
  [[ -f "$state_directory/$preserved" ]] || fail "preserved local state was removed: $preserved"
done

create_material_fixture
"$fixture_root/tools/dev/reconcile-local-material.sh" \
  --context fixture-local --state-directory "$state_directory" --mode commit >/dev/null
printf '%s\n' '{"metadata":{"labels":{"app.kubernetes.io/part-of":"unmanaged"}}}' \
  >"$cluster_state/kodex-system"
printf 'changed-again\n' >>"$fixture_root/tools/install/secret-projections.json"
if "$fixture_root/tools/dev/reconcile-local-material.sh" \
  --context fixture-local --state-directory "$state_directory" --mode reconcile >/dev/null 2>&1; then
  fail 'unmanaged namespace was accepted for deletion'
fi
[[ -e "$cluster_state/kodex-system" ]] || fail 'unmanaged namespace was deleted'

reconcile_line=$(grep -n 'reconcile-local-material.sh.*--context' "$repository_root/dev.sh" | head -1 | cut -d: -f1)
namespace_line=$(grep -n '^kubectl create namespace kodex-system' "$repository_root/dev.sh" | cut -d: -f1)
commit_line=$(grep -n 'reconcile-local-material.sh.*--context' "$repository_root/dev.sh" | tail -1 | cut -d: -f1)
nats_line=$(grep -n 'materialize-nats-runtime-users.sh' "$repository_root/dev.sh" | cut -d: -f1)
[[ "$reconcile_line" =~ ^[0-9]+$ && "$namespace_line" =~ ^[0-9]+$ &&
  "$commit_line" =~ ^[0-9]+$ && "$nats_line" =~ ^[0-9]+$ ]] ||
  fail 'dev.sh material reconciliation calls are absent'
((reconcile_line < namespace_line && nats_line < commit_line)) ||
  fail 'dev.sh material reconciliation order is invalid'

printf 'Kodex local material contract revision tests passed\n'
