#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Secret draft bootstrap failed: %s\n' "$1" >&2; exit 1; }
usage() {
  printf '%s\n' 'Usage: bootstrap-secret-drafts.sh ensure|rotate --context <exact-context> --keyring-file <private-file> [--kubeconfig-file <private-file>] [--expected-revision <revision>]' >&2
}

mode=${1:-}
[[ "$mode" == ensure || "$mode" == rotate ]] || { usage; exit 1; }
shift
expected_context=""
keyring_file=""
kubeconfig_file=""
expected_revision=""
readback_timeout=60
while (($# > 0)); do
  case "$1" in
    --context) expected_context=${2:-}; shift 2 ;;
    --keyring-file) keyring_file=${2:-}; shift 2 ;;
    --kubeconfig-file) kubeconfig_file=${2:-}; shift 2 ;;
    --expected-revision) expected_revision=${2:-}; shift 2 ;;
    --readback-timeout-seconds) readback_timeout=${2:-}; shift 2 ;;
    *) usage; fail 'unsupported argument' ;;
  esac
done
[[ -n "$expected_context" && "$keyring_file" == /* && -s "$keyring_file" && ! -L "$keyring_file" ]] || fail 'exact context and private keyring file are required'
[[ "$mode" != rotate || "$expected_revision" =~ ^[1-9][0-9]{0,8}$ ]] || fail 'rotation expected revision is required'
[[ "$mode" != ensure || -z "$expected_revision" ]] || fail 'bootstrap does not accept a rotation revision'
[[ "$readback_timeout" =~ ^[1-9][0-9]?$ ]] && ((readback_timeout <= 60)) || fail 'readback timeout is invalid'
for command_name in kubectl jq go cmp stat mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || fail 'required bootstrap tool is unavailable'
done
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
broker_root="$repository_root/services/internal/secret-broker"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
kube_arguments=(--context "$expected_context" --request-timeout=10s)
if [[ -n "$kubeconfig_file" ]]; then
  [[ "$kubeconfig_file" == /* && -r "$kubeconfig_file" && ! -L "$kubeconfig_file" ]] || fail 'Kubernetes credential file is invalid'
  kube_arguments+=(--kubeconfig "$kubeconfig_file")
fi
kube() { kubectl "${kube_arguments[@]}" "$@"; }
keyring_check() { (cd "$broker_root" && go run ./cmd/secret-draft-keys check --input-file "$1"); }
keyring_check "$keyring_file" >"$temporary_directory/candidate-summary.json" 2>/dev/null || fail 'candidate keyring is invalid'
candidate_revision=$(jq -er '.revision' "$temporary_directory/candidate-summary.json")
candidate_digest=$(jq -er '.digest' "$temporary_directory/candidate-summary.json")

# Namespace и RBAC устанавливаются обычным profile apply. Genesis никогда не
# входит в declarative apply: повтор установки не сбрасывает durable watermark.
kube get namespace kodex-secret-drafts >/dev/null 2>&1 || fail 'encrypted draft namespace is not installed'
kube -n kodex-secret-drafts get configmap secret-broker-draft-key-guard --ignore-not-found -o json >"$temporary_directory/guard.json" 2>/dev/null || fail 'key guard read failed'
kube -n kodex-system get secret secret-broker-draft-keyring --ignore-not-found -o json >"$temporary_directory/current.json" 2>/dev/null || fail 'keyring projection read failed'
if [[ ! -s "$temporary_directory/guard.json" ]]; then
  [[ "$mode" == ensure && ! -s "$temporary_directory/current.json" && "$candidate_revision" == 1 ]] || fail 'missing durable key guard cannot be reinitialized'
  jq -n '{apiVersion:"v1",kind:"ConfigMap",metadata:{name:"secret-broker-draft-key-guard",namespace:"kodex-secret-drafts",labels:{
    "app.kubernetes.io/managed-by":"kodex-secret-broker-bootstrap","kodex.dev/purpose":"secret-draft-key-guard"}},
    data:{"state.json":"{\"v\":1,\"manifest\":null,\"uses\":[]}"}}' >"$temporary_directory/genesis.json"
  # Конкурентный installer может создать тот же genesis. Только GET ниже
  # подтверждает фактическое состояние; существующий ConfigMap не обновляется.
  kube create -f "$temporary_directory/genesis.json" >/dev/null 2>&1 || true
  kube -n kodex-secret-drafts get configmap secret-broker-draft-key-guard -o json >"$temporary_directory/guard.json" 2>/dev/null || fail 'key guard bootstrap readback failed'
fi
jq -e '
  .metadata.name == "secret-broker-draft-key-guard" and .metadata.namespace == "kodex-secret-drafts" and
  (.metadata.uid | type == "string" and length > 0) and (.metadata.resourceVersion | type == "string" and length > 0) and
  .metadata.deletionTimestamp == null and (.immutable // false) == false and (.binaryData // {} | length) == 0 and
  .metadata.labels["app.kubernetes.io/managed-by"] == "kodex-secret-broker-bootstrap" and
  .metadata.labels["kodex.dev/purpose"] == "secret-draft-key-guard" and (.data | keys) == ["state.json"] and
  (.data["state.json"] | fromjson | .v == 1 and (.uses | type == "array"))
' "$temporary_directory/guard.json" >/dev/null 2>&1 || fail 'key guard identity or state is invalid'

read_current() {
  jq -e '.metadata.namespace == "kodex-system" and .metadata.name == "secret-broker-draft-keyring" and
    (.metadata.uid | type == "string" and length > 0) and (.metadata.resourceVersion | type == "string" and length > 0) and
    .metadata.deletionTimestamp == null and (.immutable // false) == false and .type == "Opaque" and
    .metadata.labels["app.kubernetes.io/managed-by"] == "kodex-secret-broker-bootstrap" and
    .metadata.labels["kodex.dev/purpose"] == "secret-draft-keyring" and (.data | keys) == ["keyring.json"]
  ' "$temporary_directory/current.json" >/dev/null 2>&1 || fail 'keyring projection identity is invalid'
  jq -rj '.data["keyring.json"] | @base64d' "$temporary_directory/current.json" >"$temporary_directory/current-keyring.json" 2>/dev/null || fail 'keyring projection payload is invalid'
  keyring_check "$temporary_directory/current-keyring.json" >"$temporary_directory/current-summary.json" 2>/dev/null || fail 'serving keyring is invalid'
}

if [[ ! -s "$temporary_directory/current.json" ]]; then
  [[ "$mode" == ensure && "$candidate_revision" == 1 ]] || fail 'rotation requires an existing keyring'
  jq -e '.data["state.json"] | fromjson | .manifest == null and .uses == []' "$temporary_directory/guard.json" >/dev/null 2>&1 || fail 'retained key guard forbids replacing a missing keyring'
  jq -n --rawfile keyring "$keyring_file" '{apiVersion:"v1",kind:"Secret",type:"Opaque",metadata:{
    name:"secret-broker-draft-keyring",namespace:"kodex-system",labels:{
    "app.kubernetes.io/managed-by":"kodex-secret-broker-bootstrap","kodex.dev/purpose":"secret-draft-keyring"}},
    data:{"keyring.json":($keyring | @base64)}}' >"$temporary_directory/create.json"
  kube create -f "$temporary_directory/create.json" >/dev/null 2>&1 || true
  kube -n kodex-system get secret secret-broker-draft-keyring -o json >"$temporary_directory/current.json" 2>/dev/null || fail 'keyring bootstrap readback failed'
fi
read_current
if ! cmp -s "$keyring_file" "$temporary_directory/current-keyring.json"; then
  [[ "$mode" == rotate ]] || fail 'existing keyring differs; explicit rotation is required'
  current_revision=$(jq -er '.revision' "$temporary_directory/current-summary.json")
  [[ "$current_revision" == "$expected_revision" && "$candidate_revision" == "$((expected_revision + 1))" ]] || fail 'keyring revision conflict'
  jq -ne --slurpfile previous "$temporary_directory/current-keyring.json" --slurpfile next "$keyring_file" '
    $previous[0] as $old | $next[0] as $new |
    all($old.keys[]; . as $key | any($new.keys[]; . == $key)) and
    ($new.keys | length) == ($old.keys | length) + 1 and
    $new.current != $old.current
  ' >/dev/null 2>&1 || fail 'rotation must retain every previous read key'
  jq --rawfile keyring "$keyring_file" '.data = {"keyring.json":($keyring | @base64)}' "$temporary_directory/current.json" >"$temporary_directory/replace.json"
  # metadata.resourceVersion — обязательный CAS; force/apply здесь запрещены.
  kube replace -f "$temporary_directory/replace.json" >/dev/null 2>&1 || fail 'keyring rotation outcome requires exact readback'
elif [[ "$mode" == rotate ]]; then
  [[ "$candidate_revision" == "$((expected_revision + 1))" ]] || fail 'rotation retry does not match expected revision'
fi
kube -n kodex-system get secret secret-broker-draft-keyring -o json >"$temporary_directory/current.json" 2>/dev/null || fail 'keyring final readback failed'
read_current
cmp -s "$keyring_file" "$temporary_directory/current-keyring.json" || fail 'keyring final bytes do not match candidate'
if [[ "$mode" == rotate ]]; then
  # Подтверждаем фактически прочитанный broker manifest, а не только API apply.
  deadline=$((SECONDS + readback_timeout))
  while :; do
    kube -n kodex-secret-drafts get configmap secret-broker-draft-key-guard -o json >"$temporary_directory/guard.json" 2>/dev/null || fail 'rotation guard readback failed'
    if jq -e --argjson revision "$candidate_revision" --arg digest "$candidate_digest" '
      .data["state.json"] | fromjson | .manifest.revision == $revision and .manifest.digest == $digest
    ' "$temporary_directory/guard.json" >/dev/null 2>&1; then break; fi
    ((SECONDS < deadline)) || fail 'broker has not acknowledged rotated keyring'
    sleep 1
  done
fi
printf 'Secret draft keyring %s and exact readback completed\n' "$mode"
