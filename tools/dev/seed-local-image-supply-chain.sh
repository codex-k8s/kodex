#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local image supply-chain seed failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    'Usage: seed-local-image-supply-chain.sh --context <exact-context>' \
    '  --state-directory <path> --render <path>' >&2
}

context=""
state_directory=""
render=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --render) render=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -n "$context" && "$(kubectl config current-context)" == "$context" ]] ||
  fail 'Kubernetes context mismatch'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'
[[ "$state_directory" == /* && -d "$state_directory" && ! -L "$state_directory" ]] ||
  fail 'state directory is invalid'
[[ -f "$render" && -s "$render" && ! -L "$render" ]] || fail 'local render is invalid'
for command_name in docker jq kubectl sha256sum tar yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

namespace=kodex-system
tools_tag=$(<"$state_directory/image-supply-chain-tools-docker-tag")
[[ "$tools_tag" =~ ^kodex-local/image-admission-tools:[a-f0-9]{64}$ ]] ||
  fail 'local admission tools Docker tag is invalid'
runner_reference=$(<"$state_directory/agent-runner-image")
[[ "$runner_reference" =~ @sha256:[a-f0-9]{64}$ ]] || fail 'local runner reference is invalid'
runner_digest=${runner_reference#*@}
role_input_metadata="$state_directory/role-image-input.json"
role_input_archive=$(jq -er --arg root "$state_directory" '
  select(.version == 1 and (.sourceRevision | test("^[a-f0-9]{40}$")) and
    (.manifestDigest | test("^sha256:[a-f0-9]{64}$"))) |
  $root + "/cache/image-supply-chain/role-input-" + .sourceRevision + ".oci.tar"
' "$role_input_metadata") || fail 'role image input metadata is invalid'
[[ -f "$role_input_archive" && -s "$role_input_archive" && ! -L "$role_input_archive" ]] ||
  fail 'role image input archive is absent'
role_input_digest=$(jq -er '.manifestDigest' "$role_input_metadata")
source_revision=$(jq -er '.sourceRevision' "$role_input_metadata")

runner_archive=""
while IFS= read -r candidate; do
  [[ -f "$candidate" && -s "$candidate" && ! -L "$candidate" ]] || continue
  candidate_digest=$(tar -xOf "$candidate" index.json 2>/dev/null | jq -er '
    if (.manifests | length) == 1 then .manifests[0].digest else empty end
  ' 2>/dev/null || true)
  if [[ "$candidate_digest" == "$runner_digest" && -z "$runner_archive" ]]; then
    # Rebuilds can produce multiple cache keys for the same canonical manifest.
    # The sorted first exact-digest archive is deterministic and equivalent.
    runner_archive=$candidate
  fi
done < <(find "$state_directory/cache" -maxdepth 1 -type f \
  -name 'agent-runner-*.oci.tar' -print | LC_ALL=C sort)
[[ -n "$runner_archive" ]] || fail 'exact local runner OCI archive is absent'

source_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
frontend_reference=$("$source_root/tools/dev/resolve-local-dockerfile-frontend.sh" \
  --source-root "$source_root" --format reference)
frontend_digest=${frontend_reference#*@}
rendered_frontend_sha256=$(yq -N -r '
  select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy") |
  .data.frontendSHA256
' "$render")
[[ "$rendered_frontend_sha256" == "${frontend_digest#sha256:}" ]] ||
  fail 'rendered Dockerfile frontend digest does not match the versioned source'
rendered_role_input=$(yq -N -r '
  select(.kind == "ConfigMap" and .metadata.name == "kodex-role-environments") |
  .data."catalog.json" | from_json | .context.contextRef
' "$render")
expected_role_input="oci://kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/role-image-inputs@$role_input_digest"
[[ "$rendered_role_input" == "$expected_role_input" ]] ||
  fail 'rendered role image input digest does not match the seed archive'

kubectl -n "$namespace" rollout status deployment/kodex-image-registry-promotion --timeout=10m >/dev/null ||
  fail 'promotion registry is unavailable'
temporary_directory=$(mktemp -d)
port_forward_pid=""
cleanup() {
  [[ -z "$port_forward_pid" ]] || kill "$port_forward_pid" >/dev/null 2>&1 || true
  if [[ -d "$temporary_directory/docker" || -d "$temporary_directory/home" ]]; then
    docker run --rm --user 0:0 \
      -v "$temporary_directory:/work" --entrypoint /bin/sh "$tools_tag" \
      -ec 'rm -rf /work/docker /work/home' >/dev/null 2>&1 || true
  fi
  rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

secret=$(kubectl -n "$namespace" get secret/kodex-image-promotion-writer -o json) ||
  fail 'promotion writer Secret is absent'
for entry in \
  'registry-client.crt:client.crt' \
  'registry-client.key:client.key' \
  'ca.pem:ca.pem' \
  'promotion.username:username' \
  'promotion.password:password'; do
  key=${entry%%:*}
  filename=${entry#*:}
  jq -er --arg key "$key" '.data[$key] | @base64d' <<<"$secret" >"$temporary_directory/$filename" ||
    fail 'promotion writer Secret projection is incomplete'
  chmod 0600 "$temporary_directory/$filename"
done
unset secret

kubectl -n "$namespace" port-forward service/kodex-image-registry-promotion \
  5003:5003 >"$temporary_directory/port-forward.log" 2>&1 &
port_forward_pid=$!
for attempt in $(seq 1 60); do
  if (exec 3<>/dev/tcp/127.0.0.1/5003) 2>/dev/null; then
    exec 3>&-
    break
  fi
  kill -0 "$port_forward_pid" >/dev/null 2>&1 || fail 'promotion registry port-forward stopped'
  ((attempt < 60)) || fail 'promotion registry port-forward is unavailable'
  sleep 1
done

# Root inside the rootless Docker namespace maps to the daemon owner and can
# write the private temporary directory without granting host root privileges.
docker run --rm --network host --user 0:0 \
  --add-host kodex-image-registry-promotion.kodex-system.svc.cluster.local:127.0.0.1 \
  -v "$temporary_directory:/work" \
  -v "$runner_archive:/input/runner.oci.tar:ro" \
  -v "$role_input_archive:/input/role-input.oci.tar:ro" \
  -e "KODEX_FRONTEND_REFERENCE=$frontend_reference" \
  -e "KODEX_FRONTEND_DIGEST=$frontend_digest" \
  -e "KODEX_RUNNER_DIGEST=$runner_digest" \
  -e "KODEX_SOURCE_REVISION=$source_revision" \
  -e "KODEX_ROLE_INPUT_DIGEST=$role_input_digest" \
  --entrypoint /bin/sh "$tools_tag" -ec '
    umask 077
    export HOME=/work/home
    export DOCKER_CONFIG=/work/docker
    mkdir -p "$HOME" "$DOCKER_CONFIG"
    target=kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003
    export REGCTL_CONFIG="$HOME/regctl.json"
    jq -n --arg target "$target" --rawfile ca /work/ca.pem \
      --rawfile cert /work/client.crt --rawfile key /work/client.key \
      --rawfile user /work/username --rawfile pass /work/password \
      "{version:1,hosts:{(\$target):{tls:\"enabled\",regcert:\$ca,
        clientCert:\$cert,clientKey:\$key,user:(\$user|gsub(\"[\\r\\n]\";\"\")),
        pass:(\$pass|gsub(\"[\\r\\n]\";\"\"))}}}" >"$REGCTL_CONFIG"
    regctl image import "$target/kodex/agent-runner:local-base" /input/runner.oci.tar
    regctl image import "$target/kodex/control-plane:local-readiness" /input/runner.oci.tar
    regctl image copy "$KODEX_FRONTEND_REFERENCE" "$target/kodex/dockerfile:local-frontend"
    regctl image import "$target/kodex/role-image-inputs:$KODEX_SOURCE_REVISION" \
      /input/role-input.oci.tar
    test "$(regctl image digest "$target/kodex/agent-runner:local-base")" = \
      "$KODEX_RUNNER_DIGEST"
    test "$(regctl image digest "$target/kodex/control-plane:local-readiness")" = \
      "$KODEX_RUNNER_DIGEST"
    test "$(regctl image digest "$target/kodex/dockerfile:local-frontend")" = \
      "$KODEX_FRONTEND_DIGEST"
    test "$(regctl image digest "$target/kodex/role-image-inputs:$KODEX_SOURCE_REVISION")" = \
      "$KODEX_ROLE_INPUT_DIGEST"
  ' ||
  fail 'promotion registry seed or exact digest readback failed'

kubectl -n "$namespace" rollout status deployment/kodex-image-registry-pull --timeout=10m >/dev/null ||
  fail 'pull registry did not become ready after seed'
printf 'Kodex local image supply-chain seed completed for source %s\n' "$source_revision"
