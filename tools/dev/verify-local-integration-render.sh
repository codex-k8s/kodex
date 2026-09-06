#!/usr/bin/env bash
set -euo pipefail

render=${1:?render path is required}
image=${2:?exact integration image is required}
[[ "$image" =~ ^registry\.local\.kodex/kodex/integration-hot-reload@sha256:[a-f0-9]{64}$ ]] || exit 1
yq -o=json -I=0 '.' "$render" | jq -s -e --arg image "$image" '
  [.[] | select(.kind == "Deployment" and .metadata.name == "integration-gateway")] |
  length == 1 and all(.[];
    .spec.template.spec as $pod |
    any($pod.containers[];
      .name == "integration-gateway" and .image == $image and
      .imagePullPolicy == "IfNotPresent" and
      .command == ["/workspace/tools/dev/run-go-hot-reload.sh"] and
      .args == ["services/external/integration-gateway","./cmd/integration-gateway","integration-gateway"] and
      .securityContext.runAsUser == 10001 and
      .securityContext.runAsGroup == 10001 and
      .securityContext.readOnlyRootFilesystem == true and
      .securityContext.allowPrivilegeEscalation == false and
      .securityContext.capabilities.drop == ["ALL"] and
      any(.volumeMounts[]; .name == "dev-source" and .mountPath == "/workspace" and .readOnly == true) and
      any(.volumeMounts[]; .name == "configuration-writeback-scratch" and .mountPath == "/tmp")
    ) and
    any($pod.volumes[]; .name == "configuration-writeback-scratch" and .emptyDir == {medium:"Memory",sizeLimit:"64Mi"}) and
    all($pod.containers[] | select(.name != "integration-gateway"); .image != $image)
  )
' >/dev/null || {
  printf 'Local integration runtime image or storage boundary is invalid\n' >&2
  exit 1
}
