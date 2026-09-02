#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex remote disposable cluster contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
fake_bin="$temporary_directory/bin"
mkdir -p "$fake_bin"

cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'config current-context') printf 'default\n' ;;
  'get --raw=/readyz') printf 'ok\n' ;;
  "get namespace kube-system -o jsonpath={.metadata.uid}")
    printf '11111111-1111-1111-1111-111111111111'
    ;;
  'config view --minify --raw -o json')
    printf '%s\n' '{"clusters":[{"cluster":{"server":"https://127.0.0.1:6443","certificate-authority-data":"Zml4dHVyZS1jYQ=="}}]}'
    ;;
  delete\ *) printf '%s\n' "$*" >>"${KODEX_TEST_DELETE_LOG:?}" ;;
  'get namespace kodex-runtime'|'get namespace kodex-system'|'get namespace identity'|'get namespace kodex-trust')
    exit 1
    ;;
  *) printf 'unexpected kubectl call: %s\n' "$*" >&2; exit 1 ;;
esac
EOF
cat >"$fake_bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ ${1:-} != -n ]] || shift
case "${1:-} ${2:-}" in
  'test -f') [[ -f "${KODEX_TEST_MARKER_FILE:?}" ]] ;;
  'test -L') exit 1 ;;
  'test -e') [[ -e "${KODEX_TEST_MARKER_FILE:?}" ]] ;;
  'stat -c') printf '0:0:600\n' ;;
  'cat --') cat "${KODEX_TEST_MARKER_FILE:?}" ;;
  'cat /etc/rancher/k3s/k3s.yaml') cat "${KODEX_TEST_KUBECONFIG_SOURCE:?}" ;;
  *) printf 'unexpected sudo call: %s\n' "$*" >&2; exit 1 ;;
esac
EOF
cat >"$fake_bin/id" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  -nG) printf 'fixture docker\n' ;;
  *) /usr/bin/id "$@" ;;
esac
EOF
cat >"$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == info ]]
EOF
chmod +x "$fake_bin"/*

marker_file="$temporary_directory/cluster-identity.json"
kubeconfig="$temporary_directory/kubeconfig"
delete_log="$temporary_directory/delete.log"
ca_sha256=$(printf 'fixture-ca' | sha256sum | awk '{print $1}')
jq -n --arg ca_sha256 "$ca_sha256" '{version:1,
  clusterUID:"11111111-1111-1111-1111-111111111111",
  apiEndpoint:"https://127.0.0.1:6443",caSHA256:$ca_sha256}' >"$marker_file"
printf 'fixture\n' >"$kubeconfig"
: >"$delete_log"
export PATH="$fake_bin:$PATH"
export KODEX_TEST_MARKER_FILE="$marker_file"
export KODEX_TEST_KUBECONFIG_SOURCE="$kubeconfig"
export KODEX_TEST_DELETE_LOG="$delete_log"

if KODEX_DEV_TLS_MODE=public-acme "$repository_root/dev.sh" status \
  --kubeconfig "$kubeconfig" --context default \
  --state-directory "$temporary_directory/state" >/dev/null 2>&1; then
  fail 'public development accepted a command without marker and expected SHA'
fi

if "$repository_root/dev.sh" down --kubeconfig "$kubeconfig" --context default \
  --state-directory "$temporary_directory/state" \
  --cluster-marker /var/lib/kodex-dev/cluster-identity.json >/dev/null 2>&1; then
  fail 'down accepted a missing disposable confirmation'
fi
[[ ! -s "$delete_log" ]] || fail 'down mutated the cluster before confirmation'

KODEX_DEV_CONFIRM_DOWN=I_UNDERSTAND_THIS_REMOVES_KODEX_FROM_THE_BOUND_DISPOSABLE_CLUSTER \
  "$repository_root/dev.sh" down --kubeconfig "$kubeconfig" --context default \
    --state-directory "$temporary_directory/state" \
    --cluster-marker /var/lib/kodex-dev/cluster-identity.json >/dev/null
[[ -s "$delete_log" ]] || fail 'confirmed down did not execute deletion commands'

for mismatch in clusterUID apiEndpoint caSHA256; do
  : >"$delete_log"
  jq --arg mismatch "$mismatch" '
    if $mismatch == "clusterUID" then .clusterUID = "22222222-2222-2222-2222-222222222222"
    elif $mismatch == "apiEndpoint" then .apiEndpoint = "https://127.0.0.1:7443"
    else .caSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    end
  ' "$marker_file" >"$marker_file.tmp"
  mv "$marker_file.tmp" "$marker_file"
  if KODEX_DEV_CONFIRM_DOWN=I_UNDERSTAND_THIS_REMOVES_KODEX_FROM_THE_BOUND_DISPOSABLE_CLUSTER \
    "$repository_root/dev.sh" down --kubeconfig "$kubeconfig" --context default \
      --state-directory "$temporary_directory/state" \
      --cluster-marker /var/lib/kodex-dev/cluster-identity.json >/dev/null 2>&1; then
    fail "down accepted a disposable cluster marker mismatch: $mismatch"
  fi
  [[ ! -s "$delete_log" ]] || fail "marker mismatch reached a deletion command: $mismatch"
  jq -n --arg ca_sha256 "$ca_sha256" '{version:1,
    clusterUID:"11111111-1111-1111-1111-111111111111",
    apiEndpoint:"https://127.0.0.1:6443",caSHA256:$ca_sha256}' >"$marker_file"
done

source_fixture="$temporary_directory/source-repository"
source_state="$temporary_directory/source-state"
mkdir -p "$source_fixture/tools/dev"
cp "$repository_root/dev.sh" "$source_fixture/dev.sh"
for helper in configure-local-api-endpoint.sh bootstrap-cluster.sh deploy-local.sh; do
  cat >"$source_fixture/tools/dev/$helper" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF
done
chmod +x "$source_fixture/dev.sh" "$source_fixture/tools/dev"/*.sh
git -C "$source_fixture" init -q
git -C "$source_fixture" config user.name 'Kodex contract'
git -C "$source_fixture" config user.email 'contract@kodex.local'
git -C "$source_fixture" add .
git -C "$source_fixture" commit -qm 'fixture'
source_revision=$(git -C "$source_fixture" rev-parse HEAD)
source_digest=$(
  cd -- "$source_fixture"
  {
    printf 'BASE_TREE\0%s\0' "$(git rev-parse 'HEAD^{tree}')"
    git diff --no-ext-diff --binary HEAD --
  } | sha256sum | awk '{print $1}'
)
mkdir -p "$source_state"
cat >"$source_state/render.yaml" <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: kodex-dev-source-provenance
  namespace: kodex-system
data:
  sourceRevision: "$source_revision"
  sourceContentSHA256: "$source_digest"
  sourceDirty: "false"
EOF
"$source_fixture/dev.sh" status --kubeconfig "$kubeconfig" --context default \
  --state-directory "$source_state" --cluster-marker /var/lib/kodex-dev/cluster-identity.json \
  --expected-sha "$source_revision" >/dev/null
jq -e --arg revision "$source_revision" --arg digest "$source_digest" '
  .headSHA == $revision and .renderedSHA == $revision and
  .currentContentSHA256 == $digest and .renderedContentSHA256 == $digest and
  .renderedDirty == false and .dirty == false and
  .renderContentMatches == true and .shaAttested == true
' "$source_state/source-provenance-status.json" >/dev/null ||
  fail 'clean source provenance was not attested'

printf '\n# hot reload\n' >>"$source_fixture/tools/dev/deploy-local.sh"
"$source_fixture/dev.sh" status --kubeconfig "$kubeconfig" --context default \
  --state-directory "$source_state" --cluster-marker /var/lib/kodex-dev/cluster-identity.json \
  --expected-sha "$source_revision" >/dev/null
jq -e --arg revision "$source_revision" --arg rendered_digest "$source_digest" '
  .headSHA == $revision and .renderedSHA == $revision and
  .renderedContentSHA256 == $rendered_digest and
  .currentContentSHA256 != $rendered_digest and .renderedDirty == false and .dirty == true and
  .renderContentMatches == false and .shaAttested == false
' "$source_state/source-provenance-status.json" >/dev/null ||
  fail 'dirty hot reload source was incorrectly attested as an exact SHA'

git -C "$source_fixture" add tools/dev/deploy-local.sh
git -C "$source_fixture" commit -qm 'stale fixture'
stale_revision=$(git -C "$source_fixture" rev-parse HEAD)
if "$source_fixture/dev.sh" status --kubeconfig "$kubeconfig" --context default \
  --state-directory "$source_state" --cluster-marker /var/lib/kodex-dev/cluster-identity.json \
  --expected-sha "$stale_revision" >/dev/null 2>&1; then
  fail 'status accepted a rendered revision from another HEAD'
fi

fixture_root="$temporary_directory/repository"
mkdir -p "$fixture_root/tools/dev" "$fixture_root/tools/install"
cp "$repository_root/tools/dev/remote-dev.sh" "$fixture_root/tools/dev/remote-dev.sh"
cp "$repository_root/tools/install/load-env.sh" "$fixture_root/tools/install/load-env.sh"
cat >"$fixture_root/dev.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"${KODEX_TEST_REMOTE_COMMAND_LOG:?}"
kubeconfig=""
while (($# > 0)); do
  case "$1" in
    --kubeconfig) kubeconfig=${2:?}; shift 2 ;;
    *) shift ;;
  esac
done
[[ -f "$kubeconfig" && "$(stat -c '%a' "$kubeconfig")" == 600 ]]
printf '%s\n' "$kubeconfig" >"${KODEX_TEST_TEMP_KUBECONFIG_LOG:?}"
EOF
chmod +x "$fixture_root/dev.sh" "$fixture_root/tools/dev/remote-dev.sh" \
  "$fixture_root/tools/install/load-env.sh"
git -C "$fixture_root" init -q
git -C "$fixture_root" config user.name 'Kodex contract'
git -C "$fixture_root" config user.email 'contract@kodex.local'
git -C "$fixture_root" add .
git -C "$fixture_root" commit -qm 'fixture'
git -C "$fixture_root" remote add origin https://github.com/codex-k8s/kodex.git
expected_sha=$(git -C "$fixture_root" rev-parse HEAD)

env_file="$temporary_directory/remote.env"
cat >"$env_file" <<EOF
KODEX_REMOTE_SERVER_PUBLIC_IP=192.0.2.10
KODEX_REMOTE_CONTROL_HOST=control.example.test
KODEX_REMOTE_OIDC_HOST=sso.example.test
KODEX_REMOTE_TELEPORT_HOST=teleport.example.test
KODEX_REMOTE_REGISTRY_HOST=registry.example.test
KODEX_REMOTE_PROMOTED_PULL_HOST=pull.example.test
KODEX_REMOTE_ACME_EMAIL=owner@example.test
KODEX_REMOTE_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES=192.0.2.10
KODEX_REMOTE_STATE_DIRECTORY=$temporary_directory/remote-state
EOF
chmod 0600 "$env_file"
remote_command_log="$temporary_directory/remote-command.log"
temporary_kubeconfig_log="$temporary_directory/temporary-kubeconfig.log"
export KODEX_TEST_REMOTE_COMMAND_LOG="$remote_command_log"
export KODEX_TEST_TEMP_KUBECONFIG_LOG="$temporary_kubeconfig_log"

jq -n --arg ca_sha256 "$ca_sha256" '{version:1,
  clusterUID:"11111111-1111-1111-1111-111111111111",
  apiEndpoint:"https://127.0.0.1:6443",caSHA256:$ca_sha256}' >"$marker_file"
"$fixture_root/tools/dev/remote-dev.sh" up --env-file "$env_file" \
  --expected-sha "$expected_sha" >/dev/null
rg -F -- '--cluster-marker /var/lib/kodex-dev/cluster-identity.json' \
  "$remote_command_log" >/dev/null || fail 'remote command omitted the disposable marker'
rg -F -- "--expected-sha $expected_sha" "$remote_command_log" >/dev/null ||
  fail 'remote command omitted the expected SHA'
temporary_kubeconfig=$(<"$temporary_kubeconfig_log")
[[ ! -e "$temporary_kubeconfig" ]] || fail 'temporary kubeconfig survived the remote command'

printf '\n# dirty\n' >>"$fixture_root/dev.sh"
if "$fixture_root/tools/dev/remote-dev.sh" up --env-file "$env_file" \
  --expected-sha "$expected_sha" >/dev/null 2>&1; then
  fail 'initial remote deployment accepted a dirty checkout'
fi
"$fixture_root/tools/dev/remote-dev.sh" status --env-file "$env_file" \
  --expected-sha "$expected_sha" >/dev/null
git -C "$fixture_root" checkout -q -- dev.sh

git -C "$fixture_root" remote set-url origin https://github.com/codex-k8s/kodex-old.git
if "$fixture_root/tools/dev/remote-dev.sh" status --env-file "$env_file" \
  --expected-sha "$expected_sha" >/dev/null 2>&1; then
  fail 'remote command accepted an unexpected origin URL'
fi
git -C "$fixture_root" remote set-url origin https://github.com/codex-k8s/kodex.git
if "$fixture_root/tools/dev/remote-dev.sh" status --env-file "$env_file" \
  --expected-sha 0000000000000000000000000000000000000000 >/dev/null 2>&1; then
  fail 'remote command accepted an unexpected HEAD'
fi

if rg -n 'KODEX_REMOTE_KUBECONFIG|k3s/k3s\.yaml.*kodex-dev-remote' \
  "$repository_root/tools/dev/remote-dev.sh" >/dev/null; then
  fail 'remote flow retains a permanent operator kubeconfig'
fi
for teleport_ownership_contract in \
  'infra/teleport/bootstrap-host.sh' \
  'infra/teleport/bootstrap.sh' \
  'teleport_backend_address=10.254.254.1'; do
  rg -Fq -- "$teleport_ownership_contract" "$repository_root/tools/dev/remote-dev.sh" ||
    fail "host-owned Teleport orchestration is absent: $teleport_ownership_contract"
done
for marker_contract in \
  'create_cluster_marker' \
  'sudo -n install -m 0600 -o root -g root "$temporary_marker" "$cluster_marker"' \
  'verify_cluster_marker'; do
  rg -Fq -- "$marker_contract" "$repository_root/tools/dev/remote-dev.sh" ||
    fail "root-owned cluster marker contract is absent: $marker_contract"
done
rg -Fq '.stableDuringCommand = true' "$repository_root/dev.sh" ||
  fail 'E2E source evidence does not require a stable start and end fingerprint'

printf 'Kodex remote disposable cluster contract tests passed\n'
