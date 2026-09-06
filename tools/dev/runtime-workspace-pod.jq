# Только Pod текущего owner runtime binding; совпадения образа недостаточно.
[.[] |
  select((.metadata.uid as $uid | $before | index($uid)) == null) |
  select(.metadata.labels["runtime.kodex.dev/managed"] == "true" and
    .metadata.labels["runtime.kodex.dev/mode"] == "turn") |
  select(.metadata.annotations["runtime.kodex.dev/revision-digest"] == $binding.revisionDigest and
    .metadata.annotations["runtime.kodex.dev/project-hash"] == $binding.projectHash and
    .metadata.annotations["runtime.kodex.dev/session-hash"] == $binding.sessionHash and
    .metadata.annotations["runtime.kodex.dev/attempt"] == ($binding.attempt | tostring)) |
  select(
    ([(.spec.initContainers[]? | select(.name == "workspace-init") | .image),
      (.spec.containers[]? | select(.name == "role-runtime" or .name == "provider-runtime") | .image)] |
      length == 3 and all(. == $image)) and
    ([(.status.initContainerStatuses[]? | select(.name == "workspace-init") | .imageID),
      (.status.containerStatuses[]? | select(.name == "role-runtime" or .name == "provider-runtime") | .imageID)] |
      length == 3 and all(test("@sha256:[a-f0-9]{64}$")) and (unique | length == 1))
  )] |
if length == 1 then .[0] else empty end
