#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Runtime artifact transfer test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT

for command_name in go kubectl yq jq python3 timeout; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

(
  cd "$repository_root/services/internal/runtime-controller"
  env -u GOFLAGS GOENV=off GOWORK=off timeout 180s go test -timeout 120s -count=1 -race \
    ./internal/callback -run 'Test(ArtifactTransfer|ArtifactSpool|ArtifactProjection|ContextArtifact|CatalogBody)'
)

for profile in web-only web-with-mattermost; do
  render="$temporary_directory/$profile.yaml"
  timeout 60s kubectl kustomize "$repository_root/deploy/k8s/profiles/$profile" >"$render"
  yq -o=json -I=0 '.' "$render" | jq -s '.' >"$render.json"
  python3 "$repository_root/scripts/tests/spool-mount-boundary-test.py" "$render.json"
  yq -o=json -I=0 '.' "$render" | jq -s -e '
    any(.[]; .kind == "ConfigMap" and .metadata.name == "runtime-controller-runtime" and
      .data.RUNTIME_CONTROLLER_FILE_TRANSFER_TIMEOUT == "2m" and
      .data.RUNTIME_CONTROLLER_ARTIFACT_SPOOL_DIRECTORY == "/var/lib/kodex/runtime-controller/artifact-spool") and
    any(.[]; .kind == "Deployment" and .metadata.name == "runtime-controller" and
      .spec.template.spec.securityContext.fsGroup == 29000 and
      any(.spec.template.spec.volumes[]; .name == "artifact-spool" and .emptyDir.sizeLimit == "2Gi" and (.emptyDir.medium // "") == "") and
      any(.spec.template.spec.containers[]; .name == "runtime-controller" and
        .securityContext.runAsUser == 10001 and .securityContext.runAsGroup == 10001 and
        .securityContext.readOnlyRootFilesystem == true and .securityContext.allowPrivilegeEscalation == false and
        .resources.limits."ephemeral-storage" == "2Gi" and
        any(.volumeMounts[]; .name == "artifact-spool" and
          .mountPath == "/var/lib/kodex/runtime-controller/artifact-spool" and (.readOnly // false) == false and
          .subPath == "controller" and .mountPropagation == null)) and
      all(.spec.template.spec.containers[] | select(.name != "runtime-controller");
        all(.volumeMounts[]?; .name != "artifact-spool")) and
      all(.spec.template.spec.initContainers[]? | select(.name != "artifact-spool-init");
        all(.volumeMounts[]?; .name != "artifact-spool")))
  ' >/dev/null || fail "$profile spool ownership, limits or configuration differ"
done

printf 'Runtime artifact transfer tests passed\n'
