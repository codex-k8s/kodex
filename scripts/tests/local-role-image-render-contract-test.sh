#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local RoleImage render contract test failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s [--source-root <path>] [--cache-root <path>]\n' "$0" >&2
}

source_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
cache_root=""
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --cache-root) cache_root=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$source_root" == /* && -x "$source_root/tools/dev/render-local.sh" &&
  -x "$source_root/tools/render-image-admission-job.sh" ]] ||
  fail 'source root is invalid'
[[ -n "$cache_root" ]] || cache_root="$source_root/.kodex-dev/cache"
[[ "$cache_root" == /* && "$cache_root" != / && "$cache_root" != "$HOME" ]] ||
  fail 'cache root is invalid'
for command_name in git jq kubectl readelf rg sha256sum yq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

for singleton_flag in --public-origin --grafana-origin --headlamp-origin; do
  flag_count=$(rg -o --fixed-strings -- "$singleton_flag" "$source_root/dev.sh" | wc -l)
  [[ "$flag_count" == 1 ]] || fail "dev.sh duplicates singleton Keycloak flag: $singleton_flag"
done
if rg -n 'GOSUMDB[=:][[:space:]]*off|GOSUMDB="?off' \
  "$source_root/tools/dev/run-go-hot-reload.sh" "$source_root/tools/dev/render-local.sh" >/dev/null; then
  fail 'local hot reload must not disable Go checksum database verification'
fi
rootless_regctl_writes=$(rg -c --fixed-strings 'docker run --rm --user 0:0' \
  "$source_root/tools/dev/build-local-image-supply-chain.sh")
[[ "$rootless_regctl_writes" == 2 ]] ||
  fail 'both rootless Docker regctl writes must use container root for the private bind mount'
for admission_tools_dockerfile in \
  "$source_root/infra/admission-tools/Dockerfile" \
  "$source_root/tools/dev/Dockerfile.local-image-supply-chain"; do
  rg -F 'bash=5.2.37-r0' "$admission_tools_dockerfile" >/dev/null ||
    fail "image admission runtime omits the pinned renderer shell: $admission_tools_dockerfile"
  rg 'RUN for tool in .*bash' "$admission_tools_dockerfile" >/dev/null ||
    fail "image admission runtime does not verify the renderer shell: $admission_tools_dockerfile"
done
rg -F -- "-name 'agent-runner-*.oci.tar' -print | LC_ALL=C sort" \
  "$source_root/tools/dev/seed-local-image-supply-chain.sh" >/dev/null ||
  fail 'runner OCI cache selection is not deterministic'
if rg -F 'multiple local runner archives match the exact digest' \
  "$source_root/tools/dev/seed-local-image-supply-chain.sh" >/dev/null; then
  fail 'equivalent exact-digest runner archives must not block repeatable local seed'
fi
seed_rootless_writes=$(rg -c --fixed-strings 'docker run --rm --network host --user 0:0' \
  "$source_root/tools/dev/seed-local-image-supply-chain.sh")
[[ "$seed_rootless_writes" == 1 ]] ||
  fail 'rootless Docker registry seed must use container root for its private bind mount'
rg -F '$gomodcache/cache/download/sumdb/sum.golang.org' \
  "$source_root/tools/dev/run-go-hot-reload.sh" >/dev/null ||
  fail 'hot-reload bootstrap does not prepare the module-cache SumDB path'
rg -F '$gopath/pkg/sumdb/sum.golang.org' \
  "$source_root/tools/dev/run-go-hot-reload.sh" >/dev/null ||
  fail 'hot-reload bootstrap does not prepare the GOPATH SumDB path'
configure_calls=$(rg -c --fixed-strings 'tools/deploy/configure-keycloak.sh' "$source_root/dev.sh")
origin_argument_uses=$(rg -c --fixed-strings '"${keycloak_origin_arguments[@]}"' "$source_root/dev.sh")
[[ "$configure_calls" == 2 && "$origin_argument_uses" == 2 ]] ||
  fail 'dev.sh Keycloak apply/readback must share one singleton origin argument set'

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
install -d -m 0700 "$cache_root"
render="$temporary_directory/render.yaml"

"$source_root/tools/dev/render-local.sh" \
  --source-root "$source_root" --cache-root "$cache_root" --output "$render" \
  --public-host control.127.0.0.1.nip.io \
  --oidc-host sso.127.0.0.1.nip.io \
  --kubernetes-service-cidr 10.43.0.1/32 \
  --kubernetes-endpoint-cidr 127.0.0.1/32 --kubernetes-endpoint-port 6443 \
  --runner-image registry.local.kodex/kodex/agent-runner@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  --session-archive-image registry.local.kodex/kodex/session-archive@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  --backup-controller-image registry.local.kodex/kodex/backup-controller@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
  --promoted-pull-host pull.127.0.0.1.nip.io \
  --role-image-builder-image registry.local.kodex/kodex/role-image-builder@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd \
  --image-admission-image registry.local.kodex/kodex/image-admission@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --image-admission-tools-image registry.local.kodex/kodex/image-admission-tools@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff \
  --authority-image registry.local.kodex/kodex/internal-rpc-authority@sha256:1111111111111111111111111111111111111111111111111111111111111111 \
  --authority-source-revision 1 \
  --role-image-input-manifest-digest sha256:2222222222222222222222222222222222222222222222222222222222222222 \
  --role-image-input-payload-sha256 3333333333333333333333333333333333333333333333333333333333333333 \
  --role-image-input-source-sha256 4444444444444444444444444444444444444444444444444444444444444444 \
  >/dev/null

air_binary="$cache_root/go-tools/air"
[[ -x "$air_binary" ]] || fail 'pinned Air executable is absent from the local tool cache'
if readelf -l "$air_binary" | rg -q 'Requesting program interpreter'; then
  fail 'pinned Air executable is dynamically linked and is not portable into the Alpine hot-reload image'
fi

policy_json=$(yq -o=json -I=0 '
  select(.kind == "ConfigMap" and .metadata.name == "kodex-image-admission-policy")
' "$render")
[[ -n "$policy_json" && "$policy_json" != null ]] || fail 'rendered owner intent is absent'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  all(.[] | select(.kind == "Deployment" or .kind == "StatefulSet" or
      .kind == "Job");
    all(((.spec.template.spec.initContainers // []) +
        (.spec.template.spec.containers // []))[];
      ([.env[]? | select(.name == "OTEL_SDK_DISABLED" and .value == "true")] |
        length) == 1))
' >/dev/null || fail 'local render does not disable telemetry in every workload container'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  (first(.[] | select(.kind == "ConfigMap" and
    .metadata.name == "kodex-image-admission-policy")) | .data) as $policy |
  all(.[] | select(.kind == "Deployment" or .kind == "StatefulSet" or
      .kind == "Job");
    .spec.template.metadata.annotations[
      "kodex.dev/runtime-admission-policy-sha256"] == $policy.policySHA256 and
    all(((.spec.template.spec.initContainers // []) +
        (.spec.template.spec.containers // []))[];
      all((.env // [])[];
        .valueFrom.configMapKeyRef.name != "kodex-image-admission-policy"))) and
  any(.[ ];
    .kind == "Deployment" and .metadata.name == "runtime-controller" and
    .spec.template.metadata.annotations["kodex.dev/controller-image"] ==
      ([.spec.template.spec.containers[] |
        select(.name == "runtime-controller") | .image] | first) and
    .spec.template.metadata.annotations["kodex.dev/authority-image"] ==
      ([.spec.template.spec.containers[] |
        select(.name == "internal-rpc-authority-issuer") | .image] | first) and
    all(.spec.template.metadata.annotations["kodex.dev/controller-image"],
        .spec.template.metadata.annotations["kodex.dev/authority-image"];
      test("@sha256:[a-f0-9]{64}$")) and
    .spec.template.metadata.annotations[
      "kodex.dev/runtime-admission-policy-sha256"] == $policy.policySHA256 and
    ([.spec.template.spec.containers[] |
      select(.name == "runtime-controller") | .env[] |
      select(.name == "RUNTIME_CONTROLLER_PROMOTED_ROLE_IMAGE_REPOSITORY") |
      .value] | first) == $policy.promotedPullRepository and
    ([.spec.template.spec.containers[] |
      select(.name == "runtime-controller") | .env[] |
      select(.name == "RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE") |
      .value] | first) == $policy.nodeReadbackImage and
    ([.spec.template.spec.containers[] |
      select(.name == "runtime-controller") | .env[] |
      select(.name == "RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION") |
      .value] | first) == $policy.roleRuntimeContractRevision and
    ([.spec.template.spec.containers[] |
      select(.name == "runtime-controller") | .env[] |
      select(.name == "RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_SHA256") |
      .value] | first) == $policy.roleRuntimeContractSHA256)
' >/dev/null || fail 'runtime-controller annotations do not match effective hot-reload images'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "internal-rpc-authority-publisher" and
    any(.spec.template.spec.volumes[];
      .name == "dev-go-tools" and (.hostPath.path | endswith("/go-tools"))) and
    any(.spec.template.spec.volumes[];
      .name == "dev-go-sumdb" and (.hostPath.path | endswith("/go-sumdb"))) and
    any(.spec.template.spec.containers[];
      .name == "publisher" and .command == ["/workspace/tools/dev/run-go-hot-reload.sh"] and
      any(.volumeMounts[]; .name == "dev-go-tools" and .mountPath == "/go/tools") and
      any(.volumeMounts[]; .name == "dev-go-sumdb" and .mountPath == "/go/pkg/sumdb") and
      any(.env[]; .name == "GOMODCACHE" and .value == "/go/pkg/mod") and
      any(.env[]; .name == "GOTMPDIR" and (.value | startswith("/go/build-cache/"))) and
      all(.env[]; .name != "GOSUMDB" or .value != "off"))) and
  any(.[];
    .kind == "PersistentVolumeClaim" and
    .metadata.name == "kodex-image-registry-evidence" and
    .metadata.labels["app.kubernetes.io/name"] == "kodex-image-registry" and
    .metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
    .spec.accessModes == ["ReadWriteOnce"] and
    .spec.resources.requests.storage == "10Gi") and
  any(.[];
    .kind == "Deployment" and .metadata.name == "kodex-image-registry-evidence" and
    any(.spec.template.spec.volumes[];
      .name == "data" and
      .persistentVolumeClaim.claimName == "kodex-image-registry-evidence")) and
  any(.[];
    .kind == "Deployment" and .metadata.name == "kodex-image-registry-pull" and
    any(.spec.template.spec.containers[];
      .name == "certificate-guard" and
      any(.env[];
        .name == "READBACK_IMAGE" and
        .value == "pull.127.0.0.1.nip.io/kodex/control-plane@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")))
' >/dev/null || fail 'hot-reload tool, evidence PVC, or pull readiness contract is invalid'
expected_frontend_sha256=$("$source_root/tools/dev/resolve-local-dockerfile-frontend.sh" \
  --source-root "$source_root" --format digest)
actual_frontend_sha256=$(jq -er '.data.frontendSHA256' <<<"$policy_json")
[[ "$actual_frontend_sha256" == "$expected_frontend_sha256" ]] ||
  fail 'owner intent frontend digest does not match the versioned frontend source'
source_revision=$(git -C "$source_root" rev-parse HEAD)
jobs="$temporary_directory/admission-jobs.yaml"
IMAGE_ADMISSION_POLICY_JSON="$policy_json" \
  "$source_root/tools/render-image-admission-job.sh" staging \
  "v20260830000000-$source_revision" all >"$jobs"

yq -o=json -I=0 '.' "$jobs" | jq -s -e '
  ([.[] | select(.kind == "PersistentVolumeClaim")] | length) == 1 and
  ([.[] | select(.kind == "Job") |
    .metadata.labels["kodex.dev/image-admission-phase"]] | sort) ==
      (["admit","claim","promote","scan","sign"] | sort) and
  all(.[] | select(.kind == "Job");
    (.spec.template.spec.containers | length) > 0 and
    all(.spec.template.spec.containers[];
      .image | test("@sha256:[a-f0-9]{64}$")))
' >/dev/null || fail 'real admission renderer did not materialize all exact phases'

if rg -n '__KODEX_[A-Z0-9_]+__|\.invalid|@sha256:0{64}' "$render" "$jobs" >/dev/null; then
  fail 'rendered supply-chain contains unresolved values'
fi

printf 'Kodex local RoleImage render contract test passed\n'
