#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Kodex local image import failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' \
    'Usage: import-local-image.sh --context <context> [--mode import|readback] [--archive <oci.tar>]' \
    '  --repository <closed-local-platform-repository> --tag <tagged-reference>' \
    '  --exact-reference <digest-reference>' >&2
}
context=""
mode=import
archive=""
repository=""
tag=""
exact_reference=""
while (($# > 0)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --mode) mode=${2:-}; shift 2 ;;
    --archive) archive=${2:-}; shift 2 ;;
    --repository) repository=${2:-}; shift 2 ;;
    --tag) tag=${2:-}; shift 2 ;;
    --exact-reference) exact_reference=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
[[ "$mode" == import || "$mode" == readback ]] || fail 'mode is invalid'
[[ -n "$context" && "$(kubectl config current-context)" == "$context" ]] ||
  fail 'Kubernetes context mismatch'
[[ "${context,,}" != *prod* && "${context,,}" != *production* ]] ||
  fail 'production context is forbidden'
case "$repository" in
  registry.local.kodex/kodex/agent-runner|registry.local.kodex/kodex/session-archive|\
  registry.local.kodex/kodex/role-image-builder|registry.local.kodex/kodex/internal-rpc-authority|\
  registry.local.kodex/kodex/image-admission|registry.local.kodex/kodex/image-admission-tools|\
  registry.local.kodex/kodex/stt-hot-reload|registry.local.kodex/kodex/integration-hot-reload|\
  registry.local.kodex/kodex/backup-controller) ;;
  *) fail 'repository is outside the closed local platform profile' ;;
esac
[[ "$tag" == "$repository":* ]] || fail 'tagged reference does not match repository'
[[ "${tag#"$repository":}" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || fail 'tag is invalid'
[[ "$exact_reference" == "$repository"@sha256:* ]] || fail 'exact reference does not match repository'
manifest_digest=${exact_reference#*@}
[[ "$manifest_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'manifest digest is invalid'
for command_name in kubectl jq sha256sum awk grep; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done
if [[ "$mode" == import ]]; then
  [[ "$archive" == /* && -f "$archive" && -s "$archive" && ! -L "$archive" ]] || fail 'OCI archive is invalid'
  # Один exact descriptor и только имя своего repository; importer не pin-ит чужие refs.
  tar -xOf "$archive" index.json 2>/dev/null | jq -e --arg digest "$manifest_digest" --arg tag "$tag" '
    (.manifests | length) == 1 and .manifests[0].digest == $digest and
    (.manifests[0].annotations as $a |
      (($a | has("io.containerd.image.name")) or ($a | has("org.opencontainers.image.ref.name"))) and
      (($a | has("io.containerd.image.name") | not) or $a["io.containerd.image.name"] == $tag) and
      (($a | has("org.opencontainers.image.ref.name") | not) or
       $a["org.opencontainers.image.ref.name"] == $tag or
       $a["org.opencontainers.image.ref.name"] == ($tag | split(":") | last)))
  ' >/dev/null || fail 'OCI archive exact descriptor or name mismatch'
fi

# Native local recipes поддерживают только linux/amd64; другая node не пропускается.
node_inventory=$(kubectl --context "$context" get nodes -o json | jq -ce '
  [.items[] | {name:.metadata.name, os:.status.nodeInfo.operatingSystem,
               architecture:.status.nodeInfo.architecture}] |
  if length > 0 and all(.[]; .os == "linux" and .architecture == "amd64")
  then sort_by(.name) else error("unsupported workload node platform") end
') || fail 'local workload node architecture readback failed'
runtime_socket=/run/k3s/containerd/containerd.sock
declare -a ctr_command cri_command

verify_image_descriptor() {
  local reference=$1 record
  record=$("${ctr_command[@]}" images list "name==$reference" 2>/dev/null) ||
    fail 'immutable image descriptor readback failed'
  awk -v ref="$reference" -v digest="$manifest_digest" '
    $1 == ref {
      count++
      if ($3 != digest || $(NF-1) != "linux/amd64") exit 1
      managed=0; pinned=0
      n=split($NF, labels, ",")
      for (i=1; i<=n; i++) {
        if (labels[i] == "io.cri-containerd.image=managed") managed=1
        if (labels[i] == "io.cri-containerd.pinned=pinned") pinned=1
      }
      if (!managed || !pinned) exit 1
    }
    END {if (count != 1) exit 1}
  ' <<<"$record" || fail 'immutable image descriptor, platform or durable pin mismatch'
}

publish_digest_alias() {
  verify_image_descriptor "$tag"
  # --local копирует весь source Image с Labels в атомарный Create, без unpinned alias.
  "${ctr_command[@]}" images tag --local --force "$tag" "$exact_reference" >/dev/null 2>&1 ||
    fail 'pinned immutable image alias publication failed'
}

verify_imported_image() {
  local status deadline=$((SECONDS + 10))
  verify_image_descriptor "$exact_reference"
  "${ctr_command[@]}" content get "$manifest_digest" 2>/dev/null | sha256sum |
    awk -v expected="${manifest_digest#sha256:}" '$1 == expected {found=1} END {exit !found}' ||
    fail 'imported image manifest digest mismatch'
  "${ctr_command[@]}" images check --quiet "name==$exact_reference" 2>/dev/null |
    grep -Fx "$exact_reference" >/dev/null || fail 'imported image content or native unpack is incomplete'
  # CRI обновляет cache по image events: label в ctr сам по себе не доказывает pin kubelet.
  while ((SECONDS <= deadline)); do
    status=$("${cri_command[@]}" inspecti "$exact_reference" 2>/dev/null) || status=""
    if jq -e --arg reference "$exact_reference" '
      .status.pinned == true and (.status.repoDigests | index($reference) != null) and
      (.status.id | test("^sha256:[a-f0-9]{64}$")) and
      .info.imageSpec.os == "linux" and .info.imageSpec.architecture == "amd64"
    ' <<<"$status" >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  fail 'CRI exact immutable image or durable pin readback failed'
}

if [[ "$context" == k3d-* ]]; then
  for command_name in docker k3d; do
    command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
  done
  cluster=${context#k3d-}
  [[ "$cluster" =~ ^[a-z0-9][a-z0-9-]*$ ]] || fail 'k3d cluster name is invalid'
  # Читается только exact cluster label; prefix имени не доказывает cluster ownership.
  nodes_json=$(k3d node list -o json | jq -ce --arg cluster "$cluster" '
    [.[] | select(.runtimeLabels["k3d.cluster"] == $cluster and
       (.role == "server" or .role == "agent")) | .name] | sort |
    if length > 0 and length == (unique | length) then . else error("node registry is invalid") end
  ') || fail 'exact k3d workload node registry is unavailable'
  [[ "$nodes_json" == "$(jq -c '[.[].name]' <<<"$node_inventory")" ]] ||
    fail 'k3d and Kubernetes workload node registries mismatch'
  mapfile -t nodes < <(jq -r '.[]' <<<"$nodes_json")
  for node in "${nodes[@]}"; do
    ctr_command=(docker exec "$node" ctr --address "$runtime_socket" -n k8s.io)
    cri_command=(docker exec "$node" crictl --runtime-endpoint "unix://$runtime_socket" --image-endpoint "unix://$runtime_socket")
    if [[ "$mode" == import ]]; then
      # Labels назначаются при создании image metadata, без окна unpinned alias после import.
      docker exec -i "$node" ctr --address "$runtime_socket" -n k8s.io images import \
        --platform linux/amd64 --base-name "$repository" --digests \
        --label io.cri-containerd.image=managed --label io.cri-containerd.pinned=pinned \
        - <"$archive" >/dev/null 2>&1 || fail 'pinned k3d image import failed'
      publish_digest_alias
    fi
    verify_imported_image
  done
else
  for command_name in k3s crictl sudo; do
    command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
  done
  [[ "$(jq -r length <<<"$node_inventory")" == 1 ]] || fail 'single-host k3s import requires one workload node'
  [[ -S "$runtime_socket" ]] || fail 'local k3s containerd socket is absent'
  sudo -n true >/dev/null 2>&1 || fail 'passwordless sudo is required for local k3s image import'
  ctr_command=(sudo -n k3s ctr --address "$runtime_socket" -n k8s.io)
  cri_command=(sudo -n crictl --runtime-endpoint "unix://$runtime_socket" --image-endpoint "unix://$runtime_socket")
  if [[ "$mode" == import ]]; then
    "${ctr_command[@]}" images import --platform linux/amd64 --base-name "$repository" --digests \
      --label io.cri-containerd.image=managed --label io.cri-containerd.pinned=pinned \
      "$archive" >/dev/null 2>&1 || fail 'pinned local k3s image import failed'
    publish_digest_alias
  fi
  verify_imported_image
fi
printf 'Kodex local image durable pin verified: %s\n' "$exact_reference"
