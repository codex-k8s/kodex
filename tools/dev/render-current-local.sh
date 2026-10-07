#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Kodex fresh local render failed: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' \
    "Usage: bash $0 --context k3d-kodex --state-directory <private-path> --expected-sha <clean-HEAD>" \
    'Creates a new private trusted-cluster render and private renderer log.' \
    'Requires inherited KUBECONFIG and existing trusted host mounts; absent trusted authority snapshot uses revision 1.' \
    'Uses existing public image pins and mail/fixture file paths; never loads credentials.env.' \
    'Side effects: render-local.sh primes Go/Air and frontend caches; cache misses may build, docker pull or npm ci.' \
    'No separate build, apply, bootstrap or authority state commit is performed.'
}
context=""; state_directory=""; expected_sha=""
while (($#)); do
  case "$1" in
    --context) context=${2:-}; shift 2 ;;
    --state-directory) state_directory=${2:-}; shift 2 ;;
    --expected-sha) expected_sha=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) fail 'unsupported argument' ;;
  esac
done
[[ "$context" == k3d-kodex ]] || fail 'exact trusted local context is required'
[[ "$expected_sha" =~ ^[a-f0-9]{40}$ ]] || fail 'expected clean source SHA is required'
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
for command_name in git jq kubectl yq sha256sum realpath stat mktemp base64; do
  command -v "$command_name" >/dev/null || fail 'required render tool is absent'
done
private_path() {
  local path=$1 kind=$2
  [[ "$path" == /* && ! -L "$path" && "$(realpath -e -- "$path")" == "$path" &&
    "$(stat -c '%u' -- "$path")" == "$(id -u)" &&
    $((8#$(stat -c '%a' -- "$path") & 8#077)) == 0 ]] || fail 'private input ownership or mode is invalid'
  if [[ "$kind" == directory ]]; then [[ -d "$path" ]] || fail 'private directory is absent'
  else [[ -f "$path" ]] || fail 'private regular file is absent'; fi
}
private_path "$state_directory" directory
case "$state_directory/" in "$repository_root/"*) fail 'private state must stay outside source' ;; esac
private_path "$state_directory/cache" directory
private_path "${KUBECONFIG:-}" file
for input in render.yaml authority-source-state.json role-image-input.json mail-source.json integration-fixture-bearer-token; do
  private_path "$state_directory/$input" file
done
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$expected_sha" ]] || fail 'source HEAD differs from expected SHA'
[[ -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] || fail 'clean Git source is required'
kube=(kubectl --context "$context" --request-timeout=15s)
[[ "$("${kube[@]}" config get-contexts "$context" -o name 2>/dev/null)" == "$context" ]] || fail 'trusted context is absent'
"${kube[@]}" config view --minify -o json 2>/dev/null | jq -e '
  (.clusters | length) == 1 and
  (.clusters[0].cluster.server | test("^https://127\\.[0-9]+\\.[0-9]+\\.[0-9]+:[1-9][0-9]*$"))
' >/dev/null || fail 'trusted context must use a loopback HTTPS API'
"${kube[@]}" -n kodex-system get deployment control-plane control-api-gateway staff-control-center -o json 2>/dev/null |
  jq -e --arg source "$repository_root" --arg cache "$state_directory/cache" '
    (.items | map(.metadata.name) | sort) == ["control-api-gateway","control-plane","staff-control-center"] and
    all(.items[];
      .metadata.name as $name | .spec.template as $template |
      $template.metadata.labels["kodex.dev/security-profile"] == "trusted-cluster" and
      $template.metadata.annotations["kodex.dev/source-root"] == $source and
      $template.metadata.annotations["kodex.dev/cache-root"] == $cache and
      any($template.spec.containers[];
        .name == $name and
        any(.volumeMounts[]?;
          . as $mount |
          .mountPath == (if $name == "staff-control-center" then "/workspace/services/staff/control-center" else "/workspace" end) and
          .readOnly == true and (.subPath // "") == "" and (.subPathExpr // "") == "" and
          any($template.spec.volumes[];
            .name == $mount.name and .hostPath.path ==
              (if $name == "staff-control-center" then $source + "/services/staff/control-center" else $source end)))))
  ' >/dev/null || fail 'existing trusted workload host mounts differ from this source/state'

# Из старого render разрешено извлечь только закрытый набор публичных параметров.
public_inputs=$(yq -o=json -I=0 '.' "$state_directory/render.yaml" | jq -sce '
  def one($kind;$name): [.[] | select(.kind==$kind and .metadata.name==$name)] |
    if length==1 then .[0] else error("public render resource is ambiguous") end;
  . as $all |
  (one("ConfigMap";"kodex-platform-endpoints")) as $endpoints |
  (one("Ingress";"staff-control-center")) as $ingress |
  (one("Certificate";"staff-control-center-public")) as $certificate |
  (one("ConfigMap";"kodex-image-admission-policy")) as $policy |
  (one("ConfigMap";"kodex-dev-source-provenance")) as $provenance |
  {publicHost:$ingress.spec.rules[0].host, oidcHost:$endpoints.data.oidcTlsServerName,
   ingressClass:$ingress.spec.ingressClassName, clusterIssuer:$certificate.spec.issuerRef.name,
   pullHost:$policy.data.pullRegistryHost, appArmor:$policy.data.providerAppArmorProfile,
   profile:$provenance.data.deploymentProfile} |
  select(.profile=="web-only" and .clusterIssuer=="kodex-local")
') || fail 'previous public render inputs are invalid'
read_pin() {
  local filename=$1 pin
  case "$filename" in agent-runner-image|session-archive-image|stt-hot-reload-image|integration-hot-reload-image|backup-controller-image|role-image-builder-image|image-admission-image|image-admission-tools-image|internal-rpc-authority-image) ;; *) fail 'image pin filename is not allowed' ;; esac
  private_path "$state_directory/$filename" file
  pin=$(<"$state_directory/$filename")
  [[ "$pin" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] || fail 'public image pin is invalid'
  printf '%s' "$pin"
}

# Алгоритм fingerprint и выбора revision совпадает с dev.sh:252–307,474–518.
# Trusted render намеренно удаляет publisher: отсутствующий/пустой snapshot
# обрабатывается как revision 0, как и в каноническом dev.sh.
current_revision=0
encoded=$("${kube[@]}" -n kodex-system get secret/internal-rpc-authority-snapshot -o 'jsonpath={.data.snapshot\.jws}' 2>/dev/null) || encoded=""
if [[ -n "$encoded" ]]; then
  compact=$(printf '%s' "$encoded" | base64 --decode 2>/dev/null) || fail 'authority snapshot encoding is invalid'
  IFS=. read -r _ payload _ <<<"$compact"
  [[ -n "$payload" ]] || fail 'authority snapshot payload is absent'
  case $((${#payload} % 4)) in 0) ;; 2) payload="${payload}==" ;; 3) payload="${payload}=" ;; *) fail 'authority snapshot payload encoding is invalid' ;; esac
  current_revision=$(printf '%s' "$payload" | tr '_-' '/+' | base64 --decode 2>/dev/null |
    jq -er '.source_revision | select(type=="number" and .>=1 and .<=9007199254740991 and floor==.)') || fail 'authority snapshot source revision is invalid'
fi
unset encoded compact payload
source_fingerprint=$(
  (
    cd -- "$repository_root"
    printf 'BASE_TREE\0%s\0' "$(git rev-parse 'HEAD^{tree}')"
    git diff --no-ext-diff --binary HEAD --
    while IFS= read -r -d '' path; do
      printf 'UNTRACKED\0%s\0' "$path"
      if [[ -L "$path" ]]; then printf 'SYMLINK\0%s\0' "$(readlink -- "$path")"
      elif [[ -f "$path" ]]; then sha256sum -- "$path"
      else printf 'OTHER\0'; fi
    done < <(git ls-files --others --exclude-standard -z)
  ) | sha256sum | awk '{print $1}'
)
source_fingerprint=$(printf '%s\0%s\0' "$source_fingerprint" web-only | sha256sum | awk '{print $1}')
state_revision=$(jq -er 'select(.version==1) | .sourceRevision | select(type=="number" and .>=1 and .<=9007199254740991 and floor==.)' "$state_directory/authority-source-state.json") || fail 'authority source state revision is invalid'
state_fingerprint=$(jq -er '.sourceFingerprint | select(type=="string" and test("^[a-f0-9]{64}$"))' "$state_directory/authority-source-state.json") || fail 'authority source state fingerprint is invalid'
if ((current_revision == 0)); then
  authority_source_revision=1
elif ((state_revision == current_revision)) && [[ "$state_fingerprint" == "$source_fingerprint" ]]; then
  authority_source_revision=$current_revision
else
  ((current_revision < 9007199254740991)) || fail 'authority source revision is exhausted'
  authority_source_revision=$((current_revision + 1))
fi
api_service_ip=$("${kube[@]}" -n default get service kubernetes -o 'jsonpath={.spec.clusterIP}' 2>/dev/null) || fail 'API Service readback failed'
api_slices=$("${kube[@]}" -n default get endpointslice -l kubernetes.io/service-name=kubernetes -o json 2>/dev/null) || fail 'API EndpointSlice readback failed'
api_endpoint_ip=$(jq -er '[.items[] | select(.addressType=="IPv4") | .endpoints[] | select(.conditions.ready!=false) | .addresses[] | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$"))] | unique | if length==1 then .[0] else error("ambiguous API endpoint") end' <<<"$api_slices") || fail 'one ready API IPv4 endpoint is required'
api_endpoint_port=$(jq -er '[.items[].ports[] | select(.protocol=="TCP" and .port!=null) | .port] | unique | if length==1 then .[0] else error("ambiguous API port") end' <<<"$api_slices") || fail 'one API TCP port is required'
unset api_slices
args=(--source-root "$repository_root" --cache-root "$state_directory/cache"
  --mail-configuration "$state_directory/mail-source.json" --integration-fixture-bearer-token-file "$state_directory/integration-fixture-bearer-token"
  --profile web-only --security-profile trusted-cluster --host-uid "$(id -u)" --host-gid "$(id -g)" --tls-mode local-ca
  --kubernetes-service-cidr "$api_service_ip/32" --kubernetes-endpoint-cidr "$api_endpoint_ip/32" --kubernetes-endpoint-port "$api_endpoint_port"
  --authority-source-revision "$authority_source_revision")
for mapping in public-host:publicHost oidc-host:oidcHost ingress-class:ingressClass cluster-issuer:clusterIssuer promoted-pull-host:pullHost provider-apparmor-profile:appArmor; do
  value=$(jq -er --arg key "${mapping#*:}" '.[$key] | select(type=="string")' <<<"$public_inputs") || fail 'public render field is absent'
  args+=("--${mapping%%:*}" "$value")
done
for mapping in runner:agent-runner session-archive:session-archive stt-hot-reload:stt-hot-reload integration-hot-reload:integration-hot-reload backup-controller:backup-controller role-image-builder:role-image-builder image-admission:image-admission image-admission-tools:image-admission-tools authority:internal-rpc-authority; do
  pin=$(read_pin "${mapping#*:}-image") || fail 'public image pin is unavailable'
  args+=("--${mapping%%:*}-image" "$pin")
done
for mapping in manifest-digest:manifestDigest payload-sha256:payloadSha256 source-sha256:sourceSha256; do
  digest=$(jq -er --arg key "${mapping#*:}" '.[$key] | select(type=="string")' "$state_directory/role-image-input.json") || fail 'role input digest is absent'
  if [[ "$mapping" == manifest-digest:* ]]; then [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'role manifest digest is invalid'
  else [[ "$digest" =~ ^[a-f0-9]{64}$ ]] || fail 'role input digest is invalid'; fi
  args+=("--role-image-input-${mapping%%:*}" "$digest")
done
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$expected_sha" && -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] || fail 'source changed during preflight'
output=$(mktemp "$state_directory/render-$expected_sha.XXXXXX.yaml")
log=$(mktemp "$state_directory/render-$expected_sha.XXXXXX.log")
bash "$repository_root/tools/dev/render-local.sh" "${args[@]}" --output "$output" >"$log" 2>&1 || fail "renderer failed; inspect private log: $log"
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$expected_sha" && -z "$(git -C "$repository_root" status --porcelain --untracked-files=all)" ]] || fail 'source changed during render'
printf 'Fresh private render: %s\nPrivate renderer log: %s\nAuthority source revision: %s\nSource fingerprint: %s\n' "$output" "$log" "$authority_source_revision" "$source_fingerprint"
