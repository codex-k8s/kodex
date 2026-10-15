#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Stable image publication failed: %s\n' "$*" >&2; exit 1; }
context="" plan="" mode=""
while (($#)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --plan) plan=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    *) fail 'unsupported argument' ;;
  esac
done
[[ "$context" =~ ^[A-Za-z0-9_.:@/-]{1,160}$ && "${context,,}" != *prod* && ("$mode" == apply || "$mode" == readback) ]] || fail 'exact local context and mode are required'
[[ "$plan" == /* && -f "$plan" && ! -L "$plan" && "$(stat -c '%u:%a' "$plan")" == "$(id -u):600" ]] || fail 'private plan is required'
for program in kubectl docker jq sha256sum timeout realpath; do command -v "$program" >/dev/null || fail 'required program is absent'; done
component=$(jq -er '.image.component' "$plan")
profile=$(jq -er '.artifactProfile' "$plan")
case "$component" in
  control-plane|secret-broker|email-bridge|runtime-controller|integration-gateway|integration-synthetic|control-api-gateway|staff-control-center|egress-gateway|automation-scheduler|artifact-retention|session-archive|registry-pull-authorizer) [[ "$profile" == built ]] || fail 'build profile is required' ;;
  image-admission|image-admission-tools) [[ "$profile" == retained ]] || fail 'retained profile is required' ;;
  *) fail 'component is not a stable platform image' ;;
esac
digest=$(jq -er '.image.digest' "$plan")
tag=$(jq -er '.image.tag' "$plan")
tools_image=$(jq -er '.toolsImageID' "$plan")
[[ "$digest" =~ ^sha256:[a-f0-9]{64}$ && "$tools_image" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'immutable image identity is invalid'
if [[ "$profile" == built ]]; then [[ "$tag" =~ ^stable-[a-f0-9]{40}-[a-f0-9]{12}$ ]] || fail 'build tag is invalid'; else
  [[ "$tag" == "retained-${digest#sha256:}" ]] || fail 'retained tag is invalid'
fi
[[ "$(jq -er '.context' "$plan")" == "$context" && "$(jq -er '.schema' "$plan")" == kodex.stable-image-publication/v2 ]] || fail 'plan context is invalid'
archive=$(realpath -e "$(dirname -- "$plan")/image.oci.tar")
[[ "$archive" =~ ^/[A-Za-z0-9_./-]+$ && -f "$archive" && ! -L "$archive" && "$(stat -c '%u:%a:%h' "$archive")" == "$(id -u):400:1" ]] || fail 'verified archive is invalid'
[[ "$(sha256sum "$archive" | cut -d ' ' -f 1)" == "$(jq -er '.archiveSHA256' "$plan")" ]] || fail 'verified archive changed'

temporary_directory=$(mktemp -d /tmp/kodex-stable-publish.XXXXXXXX)
port_forward_pid="" docker_pid="" container_id=""
cleanup() {
  local result=$? safe=true
  trap - EXIT INT TERM
  # Сначала прекращаем всех потребителей credentials, затем удаляем файлы.
  if [[ "$container_id" =~ ^[a-f0-9]{64}$ ]]; then
    timeout 20s docker rm --force "$container_id" >/dev/null 2>&1 || safe=false
  fi
  if [[ -n "$docker_pid" ]]; then kill "$docker_pid" >/dev/null 2>&1 || true; wait "$docker_pid" 2>/dev/null || true; fi
  if [[ -n "$port_forward_pid" ]]; then kill "$port_forward_pid" >/dev/null 2>&1 || true; wait "$port_forward_pid" 2>/dev/null || true; fi
  if [[ "$safe" == true ]]; then rm -rf -- "$temporary_directory"; else
    printf 'Stable image publication cleanup incomplete\n' >&2
    result=1
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

[[ "$(kubectl --context "$context" --request-timeout=15s get namespace kube-system -o 'jsonpath={.metadata.uid}')" == "$(jq -er '.clusterUID' "$plan")" ]] || fail 'cluster identity changed'
[[ "$(kubectl --context "$context" --request-timeout=15s get namespace kodex-system -o 'jsonpath={.metadata.uid}')" == "$(jq -er '.namespaceUID' "$plan")" ]] || fail 'namespace identity changed'
for kind in deployment service; do
  for name in kodex-image-registry-promotion kodex-image-registry-pull; do
    expected_uid=$(jq -er --arg kind "$kind" --arg name "$name" '.registries[] | select(.kind == $kind and .name == $name) | .uid' "$plan")
    [[ "$(kubectl --context "$context" --request-timeout=15s -n kodex-system get "$kind" "$name" -o 'jsonpath={.metadata.uid}')" == "$expected_uid" ]] || fail 'registry identity changed'
  done
done
secret=$(kubectl --context "$context" --request-timeout=15s -n kodex-system get secret kodex-image-promotion-writer -o json) || fail 'writer projection is unavailable'
[[ "$(jq -er '.metadata.uid + "|" + .metadata.resourceVersion' <<<"$secret")" == "$(jq -er '.writerMetadata' "$plan")" ]] || fail 'writer projection changed'
for entry in registry-client.crt:client.crt registry-client.key:client.key ca.pem:ca.pem promotion.username:username promotion.password:password; do
  key=${entry%%:*} filename=${entry#*:}
  jq -er --arg key "$key" '.data[$key] | @base64d | select(length > 0)' <<<"$secret" >"$temporary_directory/$filename" || fail 'writer projection is incomplete'
done
unset secret
kubectl --context "$context" --request-timeout=15s -n kodex-system port-forward --address 127.0.0.1 \
  service/kodex-image-registry-promotion :5003 >"$temporary_directory/forward.log" 2>&1 &
port_forward_pid=$!
port=""
for ((attempt=0; attempt<60; attempt++)); do
  kill -0 "$port_forward_pid" 2>/dev/null || fail 'registry port-forward stopped'
  port=$(sed -n 's/^Forwarding from 127\.0\.0\.1:\([0-9][0-9]*\) -> 5003$/\1/p' "$temporary_directory/forward.log")
  [[ "$port" =~ ^[0-9]+$ ]] && break
  sleep 0.25
done
[[ "$port" =~ ^[0-9]+$ && "$port" -ge 1024 && "$port" -le 65535 ]] || fail 'registry port-forward is unavailable'
target="kodex-image-registry-promotion.kodex-system.svc.cluster.local:$port"
jq -n --arg target "$target" --rawfile ca "$temporary_directory/ca.pem" \
  --rawfile cert "$temporary_directory/client.crt" --rawfile key "$temporary_directory/client.key" \
  --rawfile user "$temporary_directory/username" --rawfile pass "$temporary_directory/password" \
  '{version:1,hosts:{($target):{tls:"enabled",regcert:$ca,clientCert:$cert,clientKey:$key,
    user:($user|gsub("[\r\n]";"")),pass:($pass|gsub("[\r\n]";""))}}}' >"$temporary_directory/regctl.json"

# Helper читает только immutable input; HOME/tmp не пишутся на host mount.
container_id=$(docker create --network host --user 0:0 --read-only --cap-drop ALL \
  --security-opt no-new-privileges --pids-limit 128 --memory 256m --cpus 1 \
  --tmpfs /tmp:rw,nosuid,nodev,size=32m \
  --label kodex.dev/capability=stable-platform-publication \
  --add-host kodex-image-registry-promotion.kodex-system.svc.cluster.local:127.0.0.1 \
  -v "$temporary_directory:/work:ro" -v "$archive:/input/image.oci.tar:ro" \
  -e HOME=/tmp -e REGCTL_CONFIG=/work/regctl.json \
  -e "KODEX_PUBLICATION_MODE=$mode" -e "KODEX_PUBLICATION_TARGET=$target" \
  -e "KODEX_PUBLICATION_COMPONENT=$component" -e "KODEX_PUBLICATION_DIGEST=$digest" -e "KODEX_PUBLICATION_TAG=$tag" \
  --entrypoint /bin/sh "$tools_image" -ec '
    if [ "$KODEX_PUBLICATION_MODE" = apply ]; then
      regctl image import "$KODEX_PUBLICATION_TARGET/kodex/$KODEX_PUBLICATION_COMPONENT:$KODEX_PUBLICATION_TAG" /input/image.oci.tar
    fi
    test "$(regctl image digest "$KODEX_PUBLICATION_TARGET/kodex/$KODEX_PUBLICATION_COMPONENT@$KODEX_PUBLICATION_DIGEST")" = "$KODEX_PUBLICATION_DIGEST"
  ') || fail 'publication helper creation failed'
[[ "$container_id" =~ ^[a-f0-9]{64}$ ]] || fail 'publication helper identity is invalid'
timeout 120s docker start --attach "$container_id" >"$temporary_directory/operation.log" 2>&1 &
docker_pid=$!
wait "$docker_pid" || fail 'registry operation outcome is unknown; use readback'
docker_pid=""
[[ "$(docker inspect --format '{{.State.ExitCode}}' "$container_id")" == 0 ]] || fail 'publication helper did not complete'
printf 'Stable image registry operation completed\n'
