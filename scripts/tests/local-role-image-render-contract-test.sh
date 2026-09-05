#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local RoleImage render contract test failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s [--source-root <path>] [--cache-root <path>] [--profile web-only|web-with-mattermost]\n' "$0" >&2
}

source_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
cache_root=""
deployment_profile=web-only
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --cache-root) cache_root=${2:-}; shift 2 ;;
    --profile) deployment_profile=${2:-}; shift 2 ;;
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
binding_cleanup_line=$(rg -n -F \
  'kubectl delete validatingadmissionpolicybindings.admissionregistration.k8s.io' \
  "$source_root/dev.sh" | cut -d: -f1)
policy_cleanup_line=$(rg -n -F \
  'kubectl delete validatingadmissionpolicies.admissionregistration.k8s.io' \
  "$source_root/dev.sh" | cut -d: -f1)
namespace_cleanup_line=$(rg -n -F \
  'for namespace in kodex-runtime kodex-system identity kodex-trust; do' \
  "$source_root/dev.sh" | cut -d: -f1)
[[ "$binding_cleanup_line" -lt "$policy_cleanup_line" &&
  "$policy_cleanup_line" -lt "$namespace_cleanup_line" ]] ||
  fail 'local reset does not remove fail-closed admission guards before namespaces'
for admission_resource in \
  internal-rpc-authority-restore-anchor-forward-only \
  internal-rpc-authority-restore-pitr-cluster-owner \
  kodex-image-admission-controller-jobs \
  kodex-image-admission-controller-workspaces \
  runtime-execution-network-policy \
  runtime-execution-rbac \
  runtime-execution-service-account \
  runtime-execution-ticket-exact-projection \
  runtime-revision-exact-configmap-projection \
  runtime-role-pod-exact-secret-projection; do
  rg -F "$admission_resource" "$source_root/dev.sh" >/dev/null ||
    fail "local reset omits admission resource: $admission_resource"
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
rg -F 'regctl registry set "$host" --skip-check --tls enabled' \
  "$source_root/deploy/k8s/base/image-supply-chain/image-admission.sh" >/dev/null ||
  fail 'authenticated registry configuration still performs an unauthenticated connectivity check'
rg -F 'regctl registry login "$host" --skip-check' \
  "$source_root/deploy/k8s/base/image-supply-chain/image-admission.sh" >/dev/null ||
  fail 'authenticated registry login still performs a pre-credential connectivity check'
for authenticated_registry_script in \
  "$source_root/deploy/k8s/base/image-supply-chain/cleanup.sh" \
  "$source_root/tools/dev/seed-local-image-supply-chain.sh"; do
  rg -F -- '--skip-check' "$authenticated_registry_script" >/dev/null ||
    fail "authenticated registry helper omits skip-check: $authenticated_registry_script"
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
rg -Fq -- "-ec 'rm -rf /work/docker /work/home'" \
  "$source_root/tools/dev/seed-local-image-supply-chain.sh" ||
  fail 'registry seed cleanup cannot remove container-owned temporary state'
rg -Fq 'serverstransport.traefik.io/staff-control-center' \
  "$source_root/tools/dev/deploy-local.sh" ||
  fail 'local deploy does not remove the obsolete frontend ServersTransport'
rg -F 'chmod -R a-w "$go_module_cache" "$go_sumdb_cache" "$cache_root/go-tools"' \
  "$source_root/tools/dev/render-local.sh" >/dev/null ||
  fail 'host priming does not make shared Go material read-only'
rg -F 'test ! -w "$readonly_directory"' \
  "$source_root/tools/dev/run-go-hot-reload.sh" >/dev/null ||
  fail 'hot-reload bootstrap does not reject a writable shared Go path'
configure_calls=$(rg -c --fixed-strings 'tools/deploy/configure-keycloak.sh' "$source_root/dev.sh")
origin_argument_uses=$(rg -c --fixed-strings '"${keycloak_origin_arguments[@]}"' "$source_root/dev.sh")
[[ "$configure_calls" == 2 && "$origin_argument_uses" == 2 ]] ||
  fail 'dev.sh Keycloak apply/readback must share one singleton origin argument set'
for cleanup_contract in \
  'cleanup_local_image_admission_runs' \
  'pause_local_image_admission_controller' \
  'image_admission_controller_restore_replicas' \
  'deployment/image-admission-controller' \
  '--replicas=0' \
  '(.status.availableReplicas // 0) == 0' \
  'app.kubernetes.io/name=kodex-image-admission,kodex.dev/image-admission-orchestrated=true' \
  '^mc-admit-[a-f0-9]{32}-(claim|scan|sign|admit|promote)$' \
  '^mc-admit-[a-f0-9]{32}$'; do
  rg -F -- "$cleanup_contract" "$source_root/tools/dev/deploy-local.sh" >/dev/null ||
    fail "local admission revision cleanup omits contract: $cleanup_contract"
done

deployment_readback_filter="$source_root/tools/dev/readback-rendered-deployments.jq"
[[ -f "$deployment_readback_filter" && ! -L "$deployment_readback_filter" ]] ||
  fail 'rendered Deployment readback filter is unavailable'
policy_fixture='{"policySHA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}'
workloads_fixture='{"items":[
  {"metadata":{"name":"runtime-controller"},"spec":{"template":{"metadata":{"annotations":{"kodex.dev/runtime-admission-policy-sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"spec":{"containers":[{"name":"runtime-controller","env":[]}]}}}},
  {"metadata":{"name":"oauth2-control-center"},"spec":{"template":{"metadata":{"annotations":{}},"spec":{"containers":[{"name":"oauth2-proxy","env":[]}]}}}}
]}'
jq -e --argjson policy "$policy_fixture" \
  --argjson expected_deployments '["runtime-controller"]' \
  -f "$deployment_readback_filter" <<<"$workloads_fixture" >/dev/null ||
  fail 'rendered Deployment readback rejects an unrelated managed surface'
if jq -e --argjson policy "$policy_fixture" \
  --argjson expected_deployments '["runtime-controller","secret-broker"]' \
  -f "$deployment_readback_filter" <<<"$workloads_fixture" >/dev/null; then
  fail 'rendered Deployment readback accepts an absent expected workload'
fi

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
install -d -m 0700 "$cache_root"
render="$temporary_directory/render.yaml"

"$source_root/tools/dev/render-local.sh" \
  --profile "$deployment_profile" \
  --source-root "$source_root" --cache-root "$cache_root" --output "$render" \
  --public-host control.127.0.0.1.nip.io \
  --oidc-host sso.127.0.0.1.nip.io \
  --tls-mode public-acme \
  --kubernetes-service-cidr 10.43.0.1/32 \
  --kubernetes-endpoint-cidr 127.0.0.1/32 --kubernetes-endpoint-port 6443 \
  --runner-image registry.local.kodex/kodex/agent-runner@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  --session-archive-image registry.local.kodex/kodex/session-archive@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  --stt-hot-reload-image registry.local.kodex/kodex/stt-hot-reload@sha256:5555555555555555555555555555555555555555555555555555555555555555 \
  --backup-controller-image registry.local.kodex/kodex/backup-controller@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
  --promoted-pull-host pull.127.0.0.1.nip.io \
  --role-image-builder-image registry.local.kodex/kodex/role-image-builder@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd \
  --image-admission-image registry.local.kodex/kodex/image-admission@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee \
  --image-admission-tools-image registry.local.kodex/kodex/image-admission-tools@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff \
  --authority-image registry.local.kodex/kodex/internal-rpc-authority@sha256:1111111111111111111111111111111111111111111111111111111111111111 \
  --provider-apparmor-profile kodex-provider-runtime \
  --authority-source-revision 1 \
  --role-image-input-manifest-digest sha256:2222222222222222222222222222222222222222222222222222222222222222 \
  --role-image-input-payload-sha256 3333333333333333333333333333333333333333333333333333333333333333 \
  --role-image-input-source-sha256 4444444444444444444444444444444444444444444444444444444444444444 \
  >/dev/null

stt_image=registry.local.kodex/kodex/stt-hot-reload@sha256:5555555555555555555555555555555555555555555555555555555555555555
"$source_root/tools/dev/verify-local-profile-render.sh" "$render" "$deployment_profile"
opposite_profile=web-only
[[ "$deployment_profile" != web-only ]] || opposite_profile=web-with-mattermost
if "$source_root/tools/dev/verify-local-profile-render.sh" "$render" "$opposite_profile" >/dev/null 2>&1; then
  fail 'profile verifier accepted a render from another profile'
fi
profile_negative="$temporary_directory/profile-negative.yaml"
if [[ "$deployment_profile" == web-with-mattermost ]]; then
  for mutation in \
    'select(.kind != "Deployment" or .metadata.name != "interaction-gateway")' \
    '(select(.kind == "Deployment" and .metadata.name == "interaction-gateway") | .spec.template.spec.containers) |= map(select(.name != "platform-worker-grant-agent"))' \
    '(select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") | .data."key-delivery-targets.yaml") |= (from_yaml | .targets |= map(select(.workload_id != "interaction-gateway")) | to_yaml)'; do
    yq "$mutation" "$render" >"$profile_negative"
    if "$source_root/tools/dev/verify-local-profile-render.sh" "$profile_negative" "$deployment_profile" >/dev/null 2>&1; then
      fail "profile verifier accepted missing Mattermost material: $mutation"
    fi
  done
fi
"$source_root/tools/dev/verify-local-stt-render.sh" "$render" "$stt_image"
stt_negative="$temporary_directory/stt-negative.yaml"
for mutation in \
  'select(.kind != "Deployment" or .metadata.name != "stt-tts-service")' \
  '(select(.kind == "Deployment" and .metadata.name == "stt-tts-service") | .spec.template.spec.containers) |= map(select(.name != "internal-rpc-authority-issuer"))' \
  '(select(.kind == "Deployment" and .metadata.name == "stt-tts-service") | .spec.template.spec.containers[] | select(.name == "stt-tts-service") | .readinessProbe.exec) = {"command":["true"]}' \
  '(select(.kind == "Deployment" and .metadata.name == "stt-tts-service") | .spec.template.spec.containers[] | .volumeMounts[] | select(.name == "dev-go-mod") | .readOnly) = false' \
  '(select(.kind == "Deployment" and .metadata.name == "stt-tts-service") | .spec.template.spec.containers[] | select(.name == "stt-tts-service") | .image) = "golang:latest"' \
  '(select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") | .data."key-delivery-targets.yaml") |= (from_yaml | .targets |= map(select(.workload_id != "stt-tts-service")) | to_yaml)' \
  '(select(.kind == "NetworkPolicy" and .metadata.name == "stt-tts-service-exact-runtime-paths") | .spec.egress[].ports[] | select(.port == 8081) | .port) = 8080'; do
  yq "$mutation" "$render" >"$stt_negative"
  if "$source_root/tools/dev/verify-local-stt-render.sh" "$stt_negative" "$stt_image" >/dev/null 2>&1; then
    fail "local STT verifier accepted a broken render: $mutation"
  fi
done

bash "$source_root/scripts/tests/local-email-render-contract-test.sh" "$render"

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
  (first(.[] | select(.kind == "ConfigMap" and
    .metadata.name == "kodex-image-admission-policy")) | .data) as $intent |
  (first(.[] | select(.kind == "ImageAdmissionPolicyParameters" and
    .metadata.name == "kodex-image-admission-policy")) | .spec) as $parameters |
  $intent == $parameters and
  $intent.providerAppArmorProfile == "kodex-provider-runtime"
' >/dev/null || fail 'provider AppArmor profile is not projected into admission parameters'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "platform-postgresql-exact-clients" and
    any(.spec.ingress[].from[]?.podSelector;
      .matchLabels["app.kubernetes.io/name"] == "kodex-image-admission" and
      .matchLabels["app.kubernetes.io/component"] == "image-admission" and
      any(.matchExpressions[]?;
        .key == "kodex.dev/image-admission-phase" and
        .operator == "In" and
        (.values | sort) == (["admit","claim","promote"] | sort))))
' >/dev/null || fail 'PostgreSQL ingress omits protected image-admission jobs'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "internal-rpc-authority-readback-attestor-exact-paths" and
    any(.spec.ingress[].from[]?.podSelector;
      .matchLabels["app.kubernetes.io/name"] == "kodex-image-admission" and
      .matchLabels["app.kubernetes.io/component"] == "image-admission" and
      any(.matchExpressions[]?;
        .key == "kodex.dev/image-admission-phase" and
        .operator == "In" and
        (.values | sort) == (["admit","claim","promote"] | sort))))
' >/dev/null || fail 'authority attestor ingress omits protected image-admission jobs'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  all(.[] | select(.kind == "NetworkPolicy");
    all((
      ([.spec.podSelector // empty] +
       [.spec.ingress[]?.from[]?.podSelector // empty] +
       [.spec.egress[]?.to[]?.podSelector // empty])[]
    );
      (((.matchLabels["kodex.dev/image-admission-phase"]? != null) or
        any(.matchExpressions[]?;
          .key == "kodex.dev/image-admission-phase")) | not) or
      (.matchLabels["app.kubernetes.io/name"] == "kodex-image-admission" and
       .matchLabels["app.kubernetes.io/component"] == "image-admission")))
' >/dev/null || fail 'image-admission NetworkPolicy selector is based on phase without exact workload identity'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "NetworkPolicy" and
    .metadata.name == "kodex-image-registry-promotion" and
    any(.spec.ingress[].from[]?.podSelector;
      .matchLabels["app.kubernetes.io/name"] == "release-artifact-materializer" and
      .matchLabels["app.kubernetes.io/component"] == "release-bootstrap" and
      .matchLabels["kodex.dev/release-artifact-materializer"] == "true"))
' >/dev/null || fail 'promotion registry ingress does not bind the materializer to its exact workload identity'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "ValidatingAdmissionPolicy" and
    .metadata.name == "kodex-image-admission-controller-jobs" and
    any(.spec.validations[]?.expression;
      contains("object.metadata.labels['"'"'app.kubernetes.io/name'"'"'] == '"'"'kodex-image-admission'"'"'") and
      contains("object.metadata.labels['"'"'app.kubernetes.io/component'"'"'] == '"'"'image-admission'"'"'") and
      contains("object.spec.template.metadata.labels['"'"'kodex.dev/image-admission-phase'"'"'] ==") and
      contains("variables.phase") and
      contains("object.spec.template.metadata.labels['"'"'kodex.dev/image-admission-id'"'"'] ==") and
      contains("variables.admissionId")))
' >/dev/null || fail 'image-admission admission policy does not bind Job and PodTemplate identities'
expected_runtime_contract_digest=$(
  jq -cS . "$source_root/contracts/runtime-controller/v6/agent-runner-input.schema.json" |
    sha256sum | awk '{print $1}'
)
actual_policy_digest=$(jq -cS '.data | del(.orchestrationRevision, .policySHA256)' \
  <<<"$policy_json" | sha256sum | awk '{print $1}')
jq -e --arg policy "$actual_policy_digest" --arg runtime "$expected_runtime_contract_digest" '
  .data.policySHA256 == $policy and
  .data.roleRuntimeContractSHA256 == $runtime
' <<<"$policy_json" >/dev/null ||
  fail 'local policy or role runtime contract identity is not content-addressed'
source_revision=$(git -C "$source_root" rev-parse HEAD)
source_digest=$(
  cd -- "$source_root"
  {
    printf 'BASE_TREE\0%s\0' "$(git rev-parse 'HEAD^{tree}')"
    git diff --no-ext-diff --binary HEAD --
    while IFS= read -r -d '' path; do
      printf 'UNTRACKED\0%s\0' "$path"
      if [[ -L "$path" ]]; then
        printf 'SYMLINK\0%s\0' "$(readlink -- "$path")"
      elif [[ -f "$path" ]]; then
        sha256sum -- "$path"
      else
        printf 'OTHER\0'
      fi
    done < <(git ls-files --others --exclude-standard -z)
  } | sha256sum | awk '{print $1}'
)
source_dirty=false
[[ -z "$(git -C "$source_root" status --porcelain --untracked-files=all)" ]] || source_dirty=true
yq -o=json -I=0 '.' "$render" | jq -s -e \
  --arg source_revision "$source_revision" --arg source_digest "$source_digest" \
  --arg source_dirty "$source_dirty" --arg profile "$deployment_profile" '
    . as $resources |
    (first($resources[] | select(.kind == "ConfigMap" and
      .metadata.namespace == "kodex-system" and
      .metadata.name == "kodex-dev-source-provenance")) | .data) as $provenance |
    $provenance == {
      sourceRevision:$source_revision,
      deploymentProfile:$profile,
      sourceContentSHA256:$source_digest,
      sourceDirty:$source_dirty
    } and
    all($resources[] | select(.kind == "Deployment" or .kind == "StatefulSet" or
      .kind == "Job");
      .spec.template.metadata.annotations["kodex.dev/source-revision"] ==
        $source_revision and
      .spec.template.metadata.annotations["kodex.dev/source-content-sha256"] ==
        $source_digest and
      .spec.template.metadata.annotations["kodex.dev/deployment-profile"] == $profile)
  ' >/dev/null || fail 'rendered source provenance is not bound to the actual worktree content'
[[ "$actual_policy_digest" != "$source_digest" &&
  "$expected_runtime_contract_digest" != "$source_digest" ]] ||
  fail 'policy, role runtime contract, and source revision identities unexpectedly collide'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  all(.[];
    .kind != "ServersTransport" or .metadata.name != "staff-control-center") and
  any(.[];
    .kind == "Service" and .metadata.name == "staff-control-center" and
    (.metadata.annotations // {} |
      has("traefik.ingress.kubernetes.io/service.serverstransport") | not) and
    .spec.ports == [{"name":"http","port":8080,"targetPort":"http","protocol":"TCP"}]) and
  any(.[];
    .kind == "Role" and .metadata.name == "image-admission-controller" and
    any(.rules[];
      .apiGroups == ["batch"] and .resources == ["jobs"] and
      (.verbs | sort) == (["create","delete","get","list"] | sort)) and
    any(.rules[];
      .apiGroups == [""] and .resources == ["persistentvolumeclaims"] and
      (.verbs | sort) == (["create","delete","get","list"] | sort)))
' >/dev/null || fail 'image admission controller cannot clean terminal jobs and workspaces'
yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "Ingress" and .metadata.name == "staff-control-center" and
    .metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] ==
      "kodex-system-oauth2-control-center-chain@kubernetescrd,kodex-system-staff-control-center-retry@kubernetescrd" and
    .spec.rules[0].http.paths[0].backend.service.port.name == "http") and
  any(.[];
    .kind == "Ingress" and .metadata.name == "staff-control-center-api" and
    .metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] ==
      "kodex-system-oauth2-control-center-auth@kubernetescrd" and
    .spec.rules[0].http.paths[0].backend.service ==
      {name:"control-api-gateway",port:{name:"https"}})
' >/dev/null || fail 'public hot-reload render does not enforce Control Center OAuth routing'
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
  any(.[];
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
    any(.spec.template.spec.volumes[];
      .name == "dev-build-publisher" and
      (.hostPath.path | endswith("/go-build-v2/internal-rpc-authority-publisher-publisher"))) and
    any(.spec.template.spec.containers[];
      .name == "publisher" and .command == ["/workspace/tools/dev/run-go-hot-reload.sh"] and
      any(.volumeMounts[]; .name == "dev-go-tools" and .mountPath == "/go/tools" and
        .readOnly == true) and
      any(.volumeMounts[]; .name == "dev-go-mod" and .mountPath == "/go/pkg/mod" and
        .readOnly == true) and
      any(.volumeMounts[]; .name == "dev-go-sumdb" and .mountPath == "/go/pkg/sumdb" and
        .readOnly == true) and
      any(.volumeMounts[]; .name == "dev-build-publisher" and
        .mountPath == "/go/build-cache" and (.readOnly // false) == false) and
      any(.env[]; .name == "GOMODCACHE" and .value == "/go/pkg/mod") and
      any(.env[]; .name == "GOCACHE" and .value == "/go/build-cache/cache") and
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
' >/dev/null || fail 'hot-reload frontend, tool, evidence PVC, or pull readiness contract is invalid'
expected_frontend_sha256=$("$source_root/tools/dev/resolve-local-dockerfile-frontend.sh" \
  --source-root "$source_root" --format digest)
actual_frontend_sha256=$(jq -er '.data.frontendSHA256' <<<"$policy_json")
[[ "$actual_frontend_sha256" == "$expected_frontend_sha256" ]] ||
  fail 'owner intent frontend digest does not match the versioned frontend source'
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
    .spec.backoffLimit == 0 and
    .spec.template.metadata.labels["kodex.dev/local-profile"] == "hot-reload" and
    (.spec.template.spec.containers | length) > 0 and
    all(.spec.template.spec.containers[];
      .image | test("@sha256:[a-f0-9]{64}$"))) and
  all(.[] | select(.kind == "Job" and
      (.metadata.labels["kodex.dev/image-admission-phase"] |
        IN("claim", "admit", "promote")));
    ([.spec.template.spec.initContainers[] |
      select(.name == "internal-rpc-authority-issuer") |
      .env[]? |
      select(.name == "OTEL_SDK_DISABLED" and .value == "true")] |
      length) == 1 and
    any(.spec.template.spec.initContainers[];
      .name == "internal-rpc-authority-issuer" and
      any(.volumeMounts[];
        .name == "authority-bootstrap-roots" and
        .mountPath == "/usr/local/share/internal-rpc-authority/manifest-root" and
        .readOnly == true)) and
    any(.spec.template.spec.volumes[];
      .name == "authority-bootstrap-roots" and
      .secret.secretName == "internal-rpc-authority-bootstrap-roots" and
      .secret.defaultMode == 444 and
      .secret.items == [
        {"key":"manifest-root-public.jwk","path":"bootstrap-public.jwk"},
        {"key":"manifest-root-metadata.json","path":"bootstrap-metadata.json"}
      ]) and
    any(.spec.template.spec.volumes[];
      .name == "authority-sockets" and
      .emptyDir.sizeLimit == "64Mi"))
' >/dev/null || fail 'real admission renderer did not materialize all exact phases'

yq -o=json -I=0 '.' "$render" | jq -s -e '
  any(.[];
    .kind == "Deployment" and .metadata.name == "secret-broker" and
    any(.spec.template.spec.initContainers[]?;
      .name == "codex-cli-install" and
      .resources.requests.cpu == "10m" and
      .resources.requests.memory == "64Mi" and
      .resources.limits.cpu == "100m" and
      .resources.limits.memory == "256Mi" and
      any(.args[]?; contains("binary=/usr/local/bin/codex")) and
      all(.args[]?; contains("node_modules/@openai/codex") | not)))
' >/dev/null || fail 'secret-broker Codex CLI init resources are not bounded for a clean local install'

if rg -n '__KODEX_[A-Z0-9_]+__|\.invalid|@sha256:0{64}' "$render" "$jobs" >/dev/null; then
  fail 'rendered supply-chain contains unresolved values'
fi

printf 'Kodex local RoleImage render contract test passed\n'
