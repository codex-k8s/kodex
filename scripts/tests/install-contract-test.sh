#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex install contract test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
cleanup() { rm -rf -- "$temporary_directory"; }
trap cleanup EXIT

export KODEX_INSTALL_MODE=existing-kubernetes
export KODEX_NAMESPACE=kodex-system
export KODEX_KUBECONFIG=/tmp/test-kubeconfig
export KODEX_KUBE_CONTEXT=kodex-test
export KODEX_RELEASE_REGISTRY_PASSWORD='test-registry-password-with-equals=='
export KODEX_CONTROL_TLS_RECOVERY_HOST=control-recovery.example.com
export KODEX_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES=203.0.113.10
export KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES=2001:db8::10
export KODEX_PUBLIC_TLS_DNS_TIMEOUT_SECONDS=7
export KODEX_PUBLIC_TLS_HTTP_TIMEOUT_SECONDS=9
export KODEX_SERVER_PUBLIC_IPV6_ADDRESS=2606:4700:4700::1111
env_file="$temporary_directory/.kodex-env"
"$repository_root/tools/install/write-env-file.sh" --output "$env_file" >/dev/null
[[ "$(stat -c '%a' "$env_file")" == 600 ]] || fail '.kodex-env mode differs from 0600'

unset KODEX_INSTALL_MODE KODEX_NAMESPACE KODEX_KUBECONFIG KODEX_KUBE_CONTEXT
unset KODEX_RELEASE_REGISTRY_PASSWORD
unset KODEX_CONTROL_TLS_RECOVERY_HOST
unset KODEX_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES
unset KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES
unset KODEX_PUBLIC_TLS_DNS_TIMEOUT_SECONDS KODEX_PUBLIC_TLS_HTTP_TIMEOUT_SECONDS
unset KODEX_SERVER_PUBLIC_IPV6_ADDRESS
# shellcheck source=../../tools/install/load-env.sh
source "$repository_root/tools/install/load-env.sh"
kodex_load_env "$env_file" || fail 'generated .kodex-env was not loaded'
[[ "$KODEX_INSTALL_MODE" == existing-kubernetes && "$KODEX_NAMESPACE" == kodex-system &&
  "$KODEX_RELEASE_REGISTRY_PASSWORD" == 'test-registry-password-with-equals==' &&
  "$KODEX_CONTROL_TLS_RECOVERY_HOST" == control-recovery.example.com &&
  "$KODEX_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES" == 203.0.113.10 &&
  "$KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES" == 2001:db8::10 &&
  "$KODEX_PUBLIC_TLS_DNS_TIMEOUT_SECONDS" == 7 &&
  "$KODEX_PUBLIC_TLS_HTTP_TIMEOUT_SECONDS" == 9 &&
  "$KODEX_SERVER_PUBLIC_IPV6_ADDRESS" == 2606:4700:4700::1111 ]] ||
  fail 'generated .kodex-env readback mismatch'

chmod 0644 "$env_file"
if kodex_load_env "$env_file" >/dev/null 2>&1; then
  fail 'over-permissive .kodex-env was accepted'
fi

for script in install.sh tools/install/bootstrap-cert-manager.sh \
  tools/install/configure-github.sh tools/install/configure-node-registry.sh \
  tools/install/configure-ipv6-ingress-bridge.sh \
  tools/install/deploy-platform.sh tools/install/generate-material.sh \
  tools/install/materialize-nats-runtime-users.sh \
  tools/install/materialize-secrets.sh tools/install/prepare-host.sh \
  tools/install/reconcile-nats-runtime-users.sh \
  tools/install/reconcile-pull-docker-config.sh \
  tools/install/release-platform.sh tools/install/reset-host.sh \
  tools/install/verify-oidc-target.sh tools/install/write-env-file.sh; do
  [[ -x "$repository_root/$script" ]] || fail "installer entrypoint is not executable: $script"
  bash -n "$repository_root/$script"
done
bash -n "$repository_root/tools/deploy/generate-identity-material.sh" \
  "$repository_root/tools/deploy/materialize-identity-secrets.sh"

oidc_fixture="$temporary_directory/oidc-pods.json"
fake_bin="$temporary_directory/bin"
mkdir -p "$fake_bin"
cat >"$fake_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == 'config current-context' ]]; then
  printf '%s\n' "$FAKE_KUBE_CONTEXT"
  exit 0
fi
selector=""
previous=""
for argument in "$@"; do
  if [[ "$previous" == -l ]]; then selector=$argument; fi
  previous=$argument
done
if [[ "$*" == *' get pods '* ]]; then
  if [[ "$selector" == "$FAKE_OIDC_SELECTOR" ]]; then
    cat "$FAKE_OIDC_FIXTURE"
  else
    printf '%s\n' '{"items":[]}'
  fi
  exit 0
fi
exit 1
EOF
chmod +x "$fake_bin/kubectl"
cat >"$oidc_fixture" <<'EOF'
{"items":[{"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]},"spec":{"containers":[{"ports":[{"protocol":"TCP","containerPort":8443}]}]}}]}
EOF
oidc_verifier="$repository_root/tools/install/verify-oidc-target.sh"
export FAKE_KUBE_CONTEXT=kodex-test
export FAKE_OIDC_SELECTOR='app.kubernetes.io/name=sso,app.kubernetes.io/component=identity-provider'
export FAKE_OIDC_FIXTURE="$oidc_fixture"
PATH="$fake_bin:$PATH" "$oidc_verifier" --context kodex-test --namespace identity \
  --pod-name sso --pod-component identity-provider --target-port 8443 >/dev/null ||
  fail 'valid bundled OIDC target was rejected'
if PATH="$fake_bin:$PATH" "$oidc_verifier" --context kodex-test --namespace identity \
  --pod-name sso --pod-component keycloak --target-port 8443 >/dev/null 2>&1; then
  fail 'OIDC selector without a matching pod was accepted'
fi
if PATH="$fake_bin:$PATH" "$oidc_verifier" --context kodex-test --namespace identity \
  --pod-name sso --pod-component identity-provider --target-port 9443 >/dev/null 2>&1; then
  fail 'OIDC target port absent from the selected workload was accepted'
fi

dockerfile_path_validator="$repository_root/tools/release/validate-image-dockerfile-path.sh"
"$dockerfile_path_validator" infra/dockerfile-frontend/Dockerfile ||
  fail 'canonical Dockerfile frontend path was rejected'
for rejected_dockerfile_path in \
  infra/admission-tools/Dockerfile \
  infra/dockerfile-frontend/Other.Dockerfile \
  infra/dockerfile-frontend/../admission-tools/Dockerfile; do
  if "$dockerfile_path_validator" "$rejected_dockerfile_path" >/dev/null 2>&1; then
    fail "non-canonical infrastructure Dockerfile path was accepted: $rejected_dockerfile_path"
  fi
done

material_assignment_line=$(grep -n '^material_directory=' "$repository_root/install.sh" | cut -d: -f1)
registry_credentials_guard_line=$(grep -n \
  '^if ! component_selected registry && any_component_selected secrets platform &&$' \
  "$repository_root/install.sh" | cut -d: -f1)
[[ -n "$material_assignment_line" && -n "$registry_credentials_guard_line" &&
  "$material_assignment_line" -lt "$registry_credentials_guard_line" ]] ||
  fail 'existing installation material is not resolved before registry credential validation'
sed -n "$((registry_credentials_guard_line + 1))p" "$repository_root/install.sh" |
  grep -Fq '[[ ! -e "$material_directory" ]]' ||
  fail 'existing installation material does not bypass duplicate registry credential input'

rg -n 'Vault|SecretProviderClass|secrets-store\.csi' \
  "$repository_root/install.sh" "$repository_root/tools/install" \
  --glob '!deploy-platform.sh' >/dev/null &&
  fail 'retired secret backend remains in installer'

if rg -Fq 'runtime-user-policy.version" "$version_file"' \
  "$repository_root/tools/install/reconcile-nats-runtime-users.sh"; then
  fail 'NATS reconciliation marks a policy applied before Kubernetes materialization'
fi
rg -Fq 'runtime-user-policy.pending' \
  "$repository_root/tools/install/reconcile-nats-runtime-users.sh" ||
  fail 'NATS reconciliation does not preserve interrupted-upgrade evidence'
rg -Fq 'credential_matches "$material_directory/nats/users/$user_name.creds"' \
  "$repository_root/tools/install/reconcile-nats-runtime-users.sh" ||
  fail 'NATS reconciliation does not compare permissions embedded in material credentials'
rg -Fq 'Kubernetes Secret content readback mismatch' \
  "$repository_root/tools/install/materialize-nats-runtime-users.sh" ||
  fail 'NATS materialization does not compare exact Kubernetes Secret content'
rg -Fq 'NATS credential revocation ordering mismatch' \
  "$repository_root/tools/install/materialize-nats-runtime-users.sh" ||
  fail 'NATS materialization does not prove previous credential revocation ordering'
rg -Fq 'runtime-user-policy.version" "$version_file"' \
  "$repository_root/tools/install/materialize-nats-runtime-users.sh" ||
  fail 'NATS materialization does not commit the cluster-applied policy revision'
rg -Fq -- '--ignore-not-found -o name' \
  "$repository_root/tools/install/materialize-nats-runtime-users.sh" ||
  fail 'NATS materialization does not distinguish absent workloads from API errors'

jq -e '
  .version == 1 and .namespace == "kodex-system" and (.secrets | length > 0) and
  ([.secrets[].name] | length == (unique | length)) and
  all(.secrets[]; (.items | type == "array" and length > 0) and
    ([.items[].key] | length == (unique | length)) and
    all(.items[]; ((.required // true) | type == "boolean")))
' "$repository_root/tools/install/secret-projections.json" >/dev/null ||
  fail 'secret projection registry contract is invalid'
rg -Fq '[.items[].key]' "$repository_root/tools/install/deploy-platform.sh" ||
	fail 'dynamic Secret readback does not use the projection item registry'
jq -e '
	([.secrets[] | select(.dynamic == true and
		(any(.items[]; .key == "issuance_directive_jti") | not))] | length) > 0 and
	([.secrets[] | select(.dynamic == true and
		any(.items[]; .key == "issuance_directive_jti"))] | length) > 0
' "$repository_root/tools/install/secret-projections.json" >/dev/null ||
	fail 'authority bootstrap and runtime projection phases are not represented'
jq -n -e '
	def valid($required; $allowed; $actual):
		(($required - $actual) | length == 0) and
		(($actual - $allowed) | length == 0);
	valid(["current"]; ["current","previous"]; ["current"]) and
	(valid(["current"]; ["current","previous"]; ["previous"]) | not) and
	(valid(["current"]; ["current","previous"]; ["current","unknown"]) | not)
' >/dev/null || fail 'dynamic Secret required and allowed key-set contract is invalid'
for projection_contract in \
	'--argjson event_scoped "$event_scoped"' \
	'$event_scoped and $generation == "0" and ($data | length) == 0' \
	'"app.kubernetes.io/managed-by"] =='; do
	rg -Fq -- "$projection_contract" "$repository_root/tools/install/deploy-platform.sh" ||
		fail "event-scoped authority projection contract is absent: $projection_contract"
done
jq -n -e '
	def valid($event_scoped; $generation; $data; $required; $allowed):
		([$data | keys[] | select(. != "_generation")] | sort) as $actual |
		($event_scoped and $generation == "0" and ($data | length) == 0) or
		(($generation | test("^[1-9][0-9]*$")) and
			(($required - $actual) | length == 0) and
			(($actual - $allowed) | length == 0) and ($data | length) > 1);
	valid(true; "0"; {}; ["current"]; ["current"]) and
	(valid(false; "0"; {}; ["current"]; ["current"]) | not) and
	valid(true; "1"; {_generation:"MQ==",current:"value"}; ["current"]; ["current"]) and
	(valid(true; "0"; {current:"value"}; ["current"]; ["current"]) | not) and
	(valid(true; "1"; {_generation:"MQ==",unknown:"value"}; ["current"]; ["current"]) | not)
' >/dev/null || fail 'event-scoped authority placeholder contract is invalid'
publisher_apply_line=$(grep -nE '^[[:space:]]+apply_render authority-publisher ' \
	"$repository_root/tools/install/deploy-platform.sh" | cut -d: -f1)
bootstrap_wait_line=$(grep -nE '^[[:space:]]+wait_authority_projections bootstrap$' \
	"$repository_root/tools/install/deploy-platform.sh" | cut -d: -f1)
workloads_apply_line=$(grep -nE '^[[:space:]]+apply_render workloads-before-role-image-builder ' \
	"$repository_root/tools/install/deploy-platform.sh" | cut -d: -f1)
materializer_apply_line=$(grep -nE '^[[:space:]]+apply_job release-artifact-materializer$' \
	"$repository_root/tools/install/deploy-platform.sh" | cut -d: -f1)
for job_terminal_contract in \
	'.type == "Complete" and .status == "True"' \
	'.type == "Failed" and .status == "True"' \
	'release Job timed out: $name'; do
	rg -Fq -- "$job_terminal_contract" "$repository_root/tools/install/deploy-platform.sh" ||
		fail "release Job terminal-state contract is absent: $job_terminal_contract"
done
if rg -Fq -- 'wait --for=condition=Complete' "$repository_root/tools/install/deploy-platform.sh"; then
	fail 'release Job lifecycle still waits only for successful completion'
fi
builder_dependencies_wait_line=$(grep -nE \
	'^[[:space:]]+for dependency in kodex-image-registry-pull kodex-buildkit; do$' \
	"$repository_root/tools/install/deploy-platform.sh" | cut -d: -f1)
role_image_builder_apply_line=$(grep -nE '^[[:space:]]+apply_render role-image-builder ' \
	"$repository_root/tools/install/deploy-platform.sh" | cut -d: -f1)
full_wait_line=$(grep -n '^wait_authority_projections all$' \
	"$repository_root/tools/install/deploy-platform.sh" | cut -d: -f1)
assistant_wait_line=$(grep -n '^wait_system_assistant$' \
	"$repository_root/tools/install/deploy-platform.sh" | cut -d: -f1)
[[ "$publisher_apply_line" -lt "$bootstrap_wait_line" &&
	"$bootstrap_wait_line" -lt "$workloads_apply_line" &&
	"$workloads_apply_line" -lt "$materializer_apply_line" &&
	"$materializer_apply_line" -lt "$builder_dependencies_wait_line" &&
	"$builder_dependencies_wait_line" -lt "$role_image_builder_apply_line" &&
	"$role_image_builder_apply_line" -lt "$full_wait_line" &&
	"$full_wait_line" -lt "$assistant_wait_line" ]] ||
	fail 'authority bootstrap, materialization, role image builder and full readback phases are misordered'
for assistant_readback_contract in \
	'pod/system-assistant-warm' \
	'.status.phase == "Bound"' \
	'lease/runtime-controller-leader' \
	'/proxy/assistant/readyz'; do
	rg -Fq -- "$assistant_readback_contract" "$repository_root/tools/install/deploy-platform.sh" ||
		fail "system assistant release readback contract is absent: $assistant_readback_contract"
done
grep -Fq 'RUNTIME_CONTROLLER_STORAGE_CLASS: ""' \
	"$repository_root/deploy/k8s/base/runtime-controller/configmap.yaml" ||
	fail 'runtime session PVC does not use the installation-selected default StorageClass'
if rg -Fq 'gh variable get' "$repository_root/tools/install/configure-github.sh"; then
  fail 'GitHub variable readback relies on an unsupported gh subcommand'
fi
grep -Fq 'repos/$repository/actions/variables/$name' \
  "$repository_root/tools/install/configure-github.sh" ||
  fail 'GitHub variable readback does not use the repository REST endpoint'
grep -Fq 'while ((attempt <= 15))' "$repository_root/tools/install/configure-github.sh" ||
  fail 'GitHub variable readback does not have a bounded retry budget'
for recovery_contract in \
  'set_variable KODEX_CONTROL_TLS_RECOVERY_HOST' \
  'gh variable delete KODEX_CONTROL_TLS_RECOVERY_HOST' \
  '--control-tls-recovery-host "$CONTROL_TLS_RECOVERY_HOST"' \
  '.spec.dnsNames += [strenv(CONTROL_TLS_RECOVERY_HOST)]'; do
  rg -Fq -- "$recovery_contract" "$repository_root/tools/install/configure-github.sh" \
    "$repository_root/.github/workflows/deploy-production.yml" \
    "$repository_root/tools/release/render-web-only.sh" ||
    fail "Control Center TLS recovery contract is absent: $recovery_contract"
done
grep -Fq 'preflight-custom-resource-definitions' \
  "$repository_root/tools/install/deploy-platform.sh" ||
  fail 'platform preflight does not validate CustomResourceDefinitions separately'
grep -Fq '.apiVersion != \"$api_version\"' \
  "$repository_root/tools/install/deploy-platform.sh" ||
  fail 'platform preflight does not exclude exact API versions introduced by the render'
for preflight_contract in \
  'apply --server-side --dry-run=server' \
  '--field-manager=kodex-install' \
  '--mode prepare-preflight' \
  '--public-tls-mode "$KODEX_PUBLIC_TLS_MODE"' \
  'KODEX_PUBLIC_TLS_MODE=deferred|enabled'; do
  rg -Fq -- "$preflight_contract" \
    "$repository_root/tools/install/deploy-platform.sh" \
    "$repository_root/tools/install/release-platform.sh" \
    "$repository_root/install.sh" "$repository_root/docs/runbooks/fresh-install.md" ||
    fail "platform release contract is absent: $preflight_contract"
done
for owner_gate_contract in \
  '--workflow-sha-file "$workflow_sha_file"' \
  'bootstrap-actions-policy.sh' \
  'authorize_runner_gate kodex-ci build' \
  'authorize_runner_gate kodex-ci-deploy render' \
  'runner owner gate projection did not refresh'; do
  rg -Fq -- "$owner_gate_contract" \
    "$repository_root/install.sh" "$repository_root/tools/install/release-platform.sh" ||
    fail "release owner gate refresh contract is absent: $owner_gate_contract"
done
jq -n -e '
  def exact_line($actual; $expected):
    $actual == $expected or $actual == ($expected + "\n");
  exact_line("build\n"; "build") and
  exact_line("codex-k8s/kodex/.github/workflows/build-release.yml@refs/heads/main\n";
    "codex-k8s/kodex/.github/workflows/build-release.yml@refs/heads/main") and
  (exact_line("build\nextra"; "build") | not)
' >/dev/null || fail 'runner owner gate exact-line normalization contract is invalid'
rg -Fq 'reconcile_image_admission_policy_parameters' \
  "$repository_root/tools/install/deploy-platform.sh" ||
  fail 'immutable image admission parameters do not have a release reconciliation path'
(
  context=default
  namespace=kodex-system
  render_file=/dev/null
  kubectl() {
    printf '%s' '{"spec":{"revision":"same"}}'
  }
  yq() {
    printf '%s' '{"spec":{"revision":"same"}}'
  }
  source <(sed -n '/^reconcile_image_admission_policy_parameters() {$/,/^}$/p' \
    "$repository_root/tools/install/deploy-platform.sh")
  reconcile_image_admission_policy_parameters
) || fail 'unchanged image admission parameters make an idempotent apply fail'
grep -Fq 'KODEX_DISABLE_OBSERVABILITY=true' "$repository_root/.kodex-env.example" ||
  fail 'bundled install does not disable unavailable external telemetry exporters by default'
grep -Fq 'KODEX_OIDC_CONNECT_ADDRESS=sso.identity.svc.cluster.local:443' \
  "$repository_root/.kodex-env.example" ||
  fail 'bundled install does not use the OIDC Service port'
grep -Fq 'KODEX_OIDC_POD_COMPONENT=identity-provider' \
  "$repository_root/.kodex-env.example" ||
  fail 'bundled OIDC selector disagrees with the Keycloak workload labels'
rg -Fq 'tools/install/verify-oidc-target.sh' "$repository_root/install.sh" ||
  fail 'platform release does not verify its exact OIDC NetworkPolicy target'
grep -Fq 'KODEX_DISABLE_OBSERVABILITY:-true' \
  "$repository_root/install.sh" "$repository_root/tools/install/configure-github.sh" ||
  fail 'installer and GitHub configuration disagree on the external telemetry default'
for registry_pull_contract in \
  'pull_auth=$(jq -er --arg host "$internal_pull_host"' \
  '.auths[$host] = {auth:$auth}' \
  '"$output_directory/registry/pull/dockerconfig.json.next"'; do
  rg -Fq -- "$registry_pull_contract" "$repository_root/tools/install/generate-material.sh" ||
    fail "pull Docker config alias contract is absent: $registry_pull_contract"
done
pull_material="$temporary_directory/pull-material"
mkdir -p \
  "$pull_material/registry/pull" \
  "$pull_material/material/kodex/image-registry/pull" \
  "$pull_material/projections/kodex-image-registry-pull"
jq -n '{auths:{"kodex-image-registry.kodex-system.svc.cluster.local:5000":{auth:"test-auth"}}}' \
  >"$pull_material/registry/pull/dockerconfig.json"
for target in \
  "$pull_material/material/kodex/image-registry/pull/dockerconfigjson" \
  "$pull_material/projections/kodex-image-registry-pull/pull-dockerconfigjson" \
  "$pull_material/projections/kodex-image-registry-pull/probe-dockerconfig.json"; do
  install -m 0600 "$pull_material/registry/pull/dockerconfig.json" "$target"
done
"$repository_root/tools/install/reconcile-pull-docker-config.sh" \
  --material-directory "$pull_material" --promoted-pull-host images.example.com >/dev/null
pull_sha256=$(sha256sum "$pull_material/registry/pull/dockerconfig.json" | awk '{print $1}')
jq -e '
  (.auths | keys | sort) == [
    "images.example.com",
    "kodex-image-registry.kodex-system.svc.cluster.local:5000"
  ] and ([.auths[].auth] | unique) == ["test-auth"]
' "$pull_material/registry/pull/dockerconfig.json" >/dev/null ||
  fail 'existing pull Docker config was not canonicalized'
"$repository_root/tools/install/reconcile-pull-docker-config.sh" \
  --material-directory "$pull_material" --promoted-pull-host images.example.com >/dev/null
[[ "$(sha256sum "$pull_material/registry/pull/dockerconfig.json" | awk '{print $1}')" == "$pull_sha256" ]] ||
  fail 'pull Docker config reconciliation is not idempotent'
for target in \
  "$pull_material/material/kodex/image-registry/pull/dockerconfigjson" \
  "$pull_material/projections/kodex-image-registry-pull/pull-dockerconfigjson" \
  "$pull_material/projections/kodex-image-registry-pull/probe-dockerconfig.json"; do
  [[ "$(sha256sum "$target" | awk '{print $1}')" == "$pull_sha256" ]] ||
    fail 'pull Docker config copies disagree after reconciliation'
done
if rg -Fq 'select(.kind != "PodMonitor" and .kind != "ServiceMonitor" and .kind != "PrometheusRule")' \
  "$repository_root/tools/release/render-web-only.sh"; then
  fail 'external exporter disablement removes Prometheus discovery resources'
fi
for monitoring_kind in PodMonitor ServiceMonitor PrometheusRule; do
  MONITORING_KIND="$monitoring_kind" yq -e \
    'select(.kind == strenv(MONITORING_KIND))' \
    < <(kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only") >/dev/null ||
    fail "web-only profile does not contain $monitoring_kind resources"
done
(
  runtime_directory="$temporary_directory/render-filter-runtime"
  render_file="$runtime_directory/release.yaml"
  temporary_directory="$runtime_directory/output"
  mkdir -p "$temporary_directory"
  printf '%s\n' 'apiVersion: v1' 'kind: Namespace' >"$render_file"
  yq() {
    command cat -- "$2"
  }
  # Execute the production helper under set -u instead of checking syntax only.
  source <(sed -n '/^render_filter() {$/,/^}$/p' \
    "$repository_root/tools/install/deploy-platform.sh")
  render_filter_output=$(render_filter known '.')
  [[ "$render_filter_output" == "$temporary_directory/known.yaml" ]] ||
    fail 'release render helper returned an unexpected path'
  cmp -s "$render_file" "$render_filter_output" ||
    fail 'release render helper did not materialize the filtered manifest'
)

for firewall_contract in \
  'systemctl disable --now nftables' \
  'nft delete table inet kodex_fw' \
  'ufw --force reset' \
  'ufw default deny routed' \
  'ufw route allow from "$pod_cidr"' \
  'ufw route allow proto tcp to "$pod_cidr" port 80' \
  'ufw route allow proto tcp to "$pod_cidr" port 443'; do
  rg -Fq "$firewall_contract" "$repository_root/tools/install/prepare-host.sh" ||
    fail "bare-metal firewall contract is absent: $firewall_contract"
done
for ipv6_bridge_contract in \
  '--server-public-ipv6-address "${KODEX_SERVER_PUBLIC_IPV6_ADDRESS:-}"' \
  'ipv6_ingress_bridge_script="$script_directory/configure-ipv6-ingress-bridge.sh"' \
  '"$ipv6_ingress_bridge_script" --mode preflight' \
  '"$ipv6_ingress_bridge_script" --mode apply' \
  '"$ipv6_ingress_bridge_script" --mode readback'; do
  rg -Fq -- "$ipv6_bridge_contract" \
    "$repository_root/install.sh" "$repository_root/tools/install/prepare-host.sh" ||
    fail "bare-metal IPv6 ingress bridge contract is absent: $ipv6_bridge_contract"
done
rg -Fq 'KODEX_SERVER_PUBLIC_IPV6_ADDRESS' \
  "$repository_root/tools/install/write-env-file.sh" "$repository_root/.kodex-env.example" ||
  fail 'public IPv6 address is absent from the env contract'
rg -Fq '"$script_directory/configure-ipv6-ingress-bridge.sh" --mode apply' \
  "$repository_root/tools/install/reset-host.sh" ||
  fail 'host reset does not retire the IPv6 ingress bridge'
rg -Fq 'legacy kodex_fw nftables policy remains active' \
  "$repository_root/tools/install/reset-host.sh" ||
  fail 'host reset does not reject the legacy nftables policy'
rg -Fq 'node_ready=true' "$repository_root/tools/install/prepare-host.sh" ||
  fail 'bare-metal installer does not wait for a ready Kubernetes node'
rg -Fq 'no ready Kubernetes node became available' \
  "$repository_root/tools/install/prepare-host.sh" ||
  fail 'bare-metal installer does not report a node readiness timeout'
rg -Fq 'dnsutils' "$repository_root/tools/install/prepare-host.sh" ||
  fail 'bare-metal installer does not install the DNS preflight client'
rg -Fq 'for command_name in certutil cosign dig go helm kubectl nsc yq' \
  "$repository_root/tools/install/prepare-host.sh" ||
  fail 'bare-metal host readback does not require DNS and browser trust clients'
rg -Fq '  - traefik' "$repository_root/tools/install/prepare-host.sh" ||
  fail 'bare-metal k3s does not disable the bundled Traefik release'
rg -Fq 'systemctl restart k3s' "$repository_root/tools/install/prepare-host.sh" ||
  fail 'bare-metal host apply does not activate changed k3s configuration'

identity_inputs="$temporary_directory/identity-inputs"
identity_material="$temporary_directory/identity-material"
mkdir -p "$identity_inputs" "$identity_material"
printf '%s' kodex-admin >"$identity_inputs/admin-username"
printf '%s' 'test-admin-initial-password' >"$identity_inputs/admin-password"
printf '%s' kodex-owner >"$identity_inputs/owner-username"
printf '%s' owner@example.com >"$identity_inputs/owner-email"
printf '%s' 'test-owner-initial-password' >"$identity_inputs/owner-password"
"$repository_root/tools/deploy/generate-identity-material.sh" \
  --material-directory "$identity_material" \
  --admin-username-file "$identity_inputs/admin-username" \
  --admin-initial-password-file "$identity_inputs/admin-password" \
  --owner-username-file "$identity_inputs/owner-username" \
  --owner-email-file "$identity_inputs/owner-email" \
  --owner-initial-password-file "$identity_inputs/owner-password" >/dev/null
for surface in control-center grafana headlamp; do
  cookie_secret="$identity_material/management/oauth2-$surface/cookie-secret"
  [[ "$(wc -c <"$cookie_secret")" -eq 32 ]] ||
    fail "generated OAuth2 Proxy cookie Secret length is invalid: $surface"
  [[ "$(stat -c '%a' "$cookie_secret")" == 600 ]] ||
    fail "generated OAuth2 Proxy cookie Secret mode is invalid: $surface"
done

printf 'Kodex install contract tests passed\n'
