#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Production workload contract render failed: %s\n' "$*" >&2; exit 1; }
usage() { printf 'Usage: %s --manifest <path> --output <path>\n' "$0" >&2; }

manifest=""
output=""
while (($# > 0)); do
  case "$1" in
    --manifest) manifest="${2:-}"; shift 2 ;;
    --output) output="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done
[[ -r "$manifest" ]] || fail "manifest is not readable"
[[ -n "$output" ]] || fail "output path is required"
for command_name in yq jq; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
umask 077
workloads="$temporary_directory/workloads.json"
yq -o=json eval-all '.' "$manifest" | jq -sc '
  def podspec:
    if .kind == "CronJob" then .spec.jobTemplate.spec.template.spec else .spec.template.spec end;
  def text_list: (. // []) | map(tostring) | join("\u001f");
  def mount_contract:
    (.volumeMounts // []) |
    map([.name, .mountPath, (.subPath // ""),
      (if (.readOnly // false) then "true" else "false" end)] | join("\u001e")) |
    join("\u001f");
  def secret_env_contract:
    (.env // []) |
    map(select(.valueFrom.secretKeyRef? != null) |
      [.name, .valueFrom.secretKeyRef.name, .valueFrom.secretKeyRef.key,
       (if (.valueFrom.secretKeyRef.optional // false) then "true" else "false" end)] |
      join("\u001e")) | join("\u001f");
  def secret_env_from_contract:
    (.envFrom // []) |
    map(select(.secretRef? != null) |
      [.secretRef.name, (if (.secretRef.optional // false) then "true" else "false" end)] |
      join("\u001e")) | join("\u001f");
  def volume_items_contract:
    (. // []) | map([.key,.path,(.mode // 0 | tostring)] | join("\u001b")) | join("\u001c");
  def container_contract:
    {name:.name, image_prefix:(.image | split("@")[0]),
     command:(.command | text_list), args:(.args | text_list),
     mounts:mount_contract, secret_env:secret_env_contract,
     secret_env_from:secret_env_from_contract};
  def projected_contract:
    (.projected.sources // []) |
    map(if .serviceAccountToken? != null then
      ["serviceAccountToken", .serviceAccountToken.audience,
       (.serviceAccountToken.expirationSeconds | tostring), .serviceAccountToken.path] | join("\u001d")
    elif .configMap? != null then
      ["configMap", .configMap.name,
       ((.configMap.items // []) | map([.key,.path,(.mode // 0 | tostring)] | join("\u001b")) | join("\u001c"))] |
      join("\u001d")
    elif .secret? != null then
      ["secret", .secret.name,
       ((.secret.items // []) | map([.key,.path,(.mode // 0 | tostring)] | join("\u001b")) | join("\u001c"))] |
      join("\u001d")
    elif .downwardAPI? != null then
      ["downwardAPI",
       ((.downwardAPI.items // []) | map([
         (.fieldRef.fieldPath // .resourceFieldRef.resource // ""), .path, (.mode // 0 | tostring)] |
         join("\u001b")) | join("\u001c"))] | join("\u001d")
    else "forbidden" end) | join("\u001c");
  def volume_contract:
    [ .name,
      (if .secret? != null then "secret"
       elif .configMap? != null then "configMap"
       elif .persistentVolumeClaim? != null then "pvc"
       elif .emptyDir? != null then "emptyDir"
       elif .projected? != null then "projected"
       else "forbidden" end),
      (if .secret? != null then
         [.secret.secretName,
          (if (.secret.optional // false) then "true" else "false" end),
          (.secret.defaultMode // 0 | tostring),
          (.secret.items | volume_items_contract)] | join("\u001d")
       elif .configMap? != null then
         [.configMap.name,
          (if (.configMap.optional // false) then "true" else "false" end),
          (.configMap.defaultMode // 0 | tostring),
          (.configMap.items | volume_items_contract)] | join("\u001d")
       elif .persistentVolumeClaim? != null then
         [.persistentVolumeClaim.claimName,
          (if (.persistentVolumeClaim.readOnly // false) then "true" else "false" end)] |
         join("\u001d")
       elif .emptyDir? != null then ((.emptyDir.medium // "") + "\u001d" + (.emptyDir.sizeLimit // ""))
       elif .projected? != null then
         ((.projected.defaultMode // 0 | tostring) + "\u001d" + projected_contract)
       else "forbidden" end)
    ] | join("\u001e");
  [ .[] |
    select(.kind == "Deployment" or .kind == "StatefulSet" or .kind == "Job" or .kind == "CronJob") |
    . as $workload | (podspec) as $pod |
    {key:((.kind | ascii_downcase) + "." + .metadata.name),
     service_account:($pod.serviceAccountName // "default"),
     automount:(if $pod.automountServiceAccountToken == null then true
       else $pod.automountServiceAccountToken end),
     containers:[ $pod.containers[] | container_contract ],
     init_containers:[ $pod.initContainers[]? | container_contract ],
     volumes:([ $pod.volumes[]? | volume_contract ] | join("\u001f")),
     unsupported_volumes:([ $pod.volumes[]? |
       select((([has("secret"),has("configMap"),has("persistentVolumeClaim"),has("emptyDir"),has("projected")] |
         map(select(. == true)) | length) != 1) or
         (.projected? != null and any(.projected.sources[]?;
           ([has("serviceAccountToken"),has("configMap"),has("secret"),has("downwardAPI")] |
             map(select(. == true)) | length) != 1))) ] | length)}
  ] |
  if length == 0 then error("workload set is empty")
  elif (group_by(.key) | any(.[]; length != 1)) then error("workload identity is duplicated")
  elif any(.[]; .automount != false) then error("workload ServiceAccount automount is not disabled")
  elif any(.[]; .unsupported_volumes != 0) then error("unsupported workload volume source")
  elif any(.[]; ((.containers | length) == 0) or
    (([.containers[].name] | unique | length) != (.containers | length)) or
    (([.init_containers[].name] | unique | length) != (.init_containers | length))) then
    error("workload container identity is empty or duplicated")
  else . end
' >"$workloads"

jq '
  def add_container($key; $prefix; $container):
    .[$key + "." + $prefix + "." + $container.name + ".imagePrefix"] = $container.image_prefix |
    .[$key + "." + $prefix + "." + $container.name + ".command"] = $container.command |
    .[$key + "." + $prefix + "." + $container.name + ".args"] = $container.args |
    .[$key + "." + $prefix + "." + $container.name + ".mounts"] = $container.mounts |
    .[$key + "." + $prefix + "." + $container.name + ".secretEnv"] = $container.secret_env |
    .[$key + "." + $prefix + "." + $container.name + ".secretEnvFrom"] = $container.secret_env_from;
  reduce .[] as $workload ({};
    .[$workload.key + ".serviceAccountName"] = $workload.service_account |
    .[$workload.key + ".automountServiceAccountToken"] = ($workload.automount | tostring) |
    .[$workload.key + ".containerNames"] = ($workload.containers | map(.name) | join("\u001f")) |
    .[$workload.key + ".initContainerNames"] = ($workload.init_containers | map(.name) | join("\u001f")) |
    .[$workload.key + ".volumes"] = $workload.volumes |
    reduce $workload.containers[] as $container (.; add_container($workload.key; "container"; $container)) |
    reduce $workload.init_containers[] as $container (.; add_container($workload.key; "initContainer"; $container))
  ) |
  {apiVersion:"v1",kind:"ConfigMap",
   metadata:{name:"mattercodex-production-workload-contracts",namespace:"mattercodex-system",
     labels:{"mattercodex.dev/profile":"direct-production-single-node-prototype",
       "app.kubernetes.io/managed-by":"mattercodex-owner-bootstrap"}},
   data:.}
' "$workloads" | yq -P >"$output"
printf 'Production workload contracts rendered: %s\n' "$output"
