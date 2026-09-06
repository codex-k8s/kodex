#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary_root="$(mktemp -d)"
trap 'rm -rf "$temporary_root"' EXIT

kubectl kustomize "$repository_root/deploy/k8s/overlays/staging/stt-tts-service" >"$temporary_root/staging.yaml"
kubectl kustomize "$repository_root/deploy/k8s/overlays/production/stt-tts-service" >"$temporary_root/production.yaml"
kubectl kustomize "$repository_root/deploy/k8s/base/stt-tts-service-provider-smoke" >"$temporary_root/provider-smoke.yaml"

for profile in web-only web-with-mattermost; do
  render="$temporary_root/${profile}.yaml"
  kubectl kustomize "$repository_root/deploy/k8s/profiles/$profile" >"$render"
  yq -o=json -I=0 '.' "$render" | jq -s '.' >"$render.json"
  python3 "$repository_root/scripts/tests/spool-mount-boundary-test.py" "$render.json"
  yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service")' "$render" >/dev/null
  if yq -e 'select(.kind == "Job" and .metadata.name == "stt-provider-smoke")' "$render" >/dev/null 2>&1; then
    printf 'Live STT smoke entered automatic %s render\n' "$profile" >&2
    exit 1
  fi
done

for render_name in staging production web-only web-with-mattermost; do
  yq -o=json -I=0 '.' "$temporary_root/$render_name.yaml" | jq -s -e '
    all(.[] | select(.kind == "Deployment") |
      .spec.template.spec | (.containers[]?, .initContainers[]?);
      all((.startupProbe, .readinessProbe, .livenessProbe) | select(. != null);
        ([keys[] | select(. == "exec" or . == "httpGet" or
          . == "tcpSocket" or . == "grpc")] | length) == 1))
  ' >/dev/null || {
    printf 'Kubernetes probe must contain exactly one handler in %s\n' "$render_name" >&2
    exit 1
  }
done

jq -e '[.images[] | select(.component == "stt-tts-service")] | length == 1' "$repository_root/tools/release/images.json" >/dev/null
yq -e '[.targets[] | select(.workload_id == "stt-tts-service" and .startup_readback_required == true)] | length == 2' \
  "$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml" >/dev/null

for profile in web-only web-with-mattermost; do
  render="$temporary_root/${profile}.yaml"
  yq -e 'select(.kind == "Certificate" and .metadata.name == "internal-rpc-authority-stt-tts-service-workload") |
    .spec.secretName == "internal-rpc-authority-stt-tts-service-workload-tls"' "$render" >/dev/null
  yq -e 'select(.kind == "Role" and .metadata.name == "internal-rpc-authority-stt-key-delivery") |
    (.rules[0].verbs | join(",")) == "get,update"' "$render" >/dev/null
  yq -r 'select(.kind == "Role" and .metadata.name == "internal-rpc-authority-stt-key-delivery") |
    .rules[0].resourceNames[]' "$render" | sort >"$temporary_root/stt-rbac.txt"
  jq -r '.secrets[] | select(.dynamic == true and (.name | startswith("internal-rpc-authority-stt-tts-service-"))) |
    .name' "$repository_root/tools/install/secret-projections.json" | sort >"$temporary_root/stt-secrets.txt"
  diff -u "$temporary_root/stt-secrets.txt" "$temporary_root/stt-rbac.txt"
  yq -r 'select(.kind == "ConfigMap" and (.metadata.name | test("^egress-gateway-policy-"))) |
    .data."policy.json"' "$render" >"$temporary_root/policy.json"
  digest=$(cd "$repository_root/services/external/egress-gateway" && env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/policy-digest "$temporary_root/policy.json")
  revision=$(jq -r '.metadata.revision' "$temporary_root/policy.json")
  for component in stt-tts-service egress-gateway; do
    if [[ "$component" == stt-tts-service ]]; then
      expected_revision=$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "stt-tts-service-runtime") | .data.STT_EGRESS_EXPECTED_REVISION' "$render")
      expected_digest=$(yq -r 'select(.kind == "ConfigMap" and .metadata.name == "stt-tts-service-runtime") | .data.STT_EGRESS_EXPECTED_DIGEST' "$render")
    else
      expected_revision=$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") | .spec.template.spec.containers[0].env[] | select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_REVISION") | .value' "$render")
      expected_digest=$(yq -r 'select(.kind == "Deployment" and .metadata.name == "egress-gateway") | .spec.template.spec.containers[0].env[] | select(.name == "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST") | .value' "$render")
    fi
    [[ "$expected_revision" == "$revision" && "$expected_digest" == "$digest" ]] || { printf 'STT egress generation mismatch in %s\n' "$profile" >&2; exit 1; }
  done
  jq -e '[.spec.profiles[] | select(.name == "openai-stt")] == [{"name":"openai-stt","workload":"stt-tts-service","operation":"openai.transcription","destinations":[{"hostname":"api.openai.com","port":443}]}]' "$temporary_root/policy.json" >/dev/null
  for policy in control-plane-stt-projection-ingress stt-authority-attestor-ingress stt-authority-restore-ingress stt-authority-postgresql-ingress; do
    POLICY_NAME="$policy" yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == strenv(POLICY_NAME)) |
      .spec.ingress[0].from[0].podSelector.matchLabels."app.kubernetes.io/name" == "stt-tts-service"' "$render" >/dev/null
  done
  yq -r 'select(.kind == "ConfigMap" and .metadata.name == "internal-rpc-authority-publisher-target-registry") |
    .data."key-delivery-targets.yaml"' "$render" | yq -e '[.targets[] |
      select(.workload_id == "stt-tts-service" and .startup_readback_required == true)] | length == 2' >/dev/null
done
jq -e '[.secrets[] | select(.name | startswith("internal-rpc-authority-stt-tts-service-"))] | length == 13' \
  "$repository_root/tools/install/secret-projections.json" >/dev/null

yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
  (.spec.template.spec.terminationGracePeriodSeconds == 35 and
   .spec.template.spec.containers[] | select(.name == "stt-tts-service") |
   .resources.limits.memory == "256Mi")' "$temporary_root/production.yaml" >/dev/null
yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
  .spec.template.spec.volumes[] | select(.name == "stt-spool") |
  (.emptyDir.sizeLimit == "64Mi" and .emptyDir.medium == "Memory")' "$temporary_root/production.yaml" >/dev/null
yq -e 'select(.kind == "ConfigMap" and .metadata.name == "stt-tts-service-runtime") |
  (.data.STT_REQUEST_TIMEOUT == "20s" and .data.STT_SHUTDOWN_TIMEOUT == "30s" and
   .data.STT_SPOOL_DIRECTORY == "/var/lib/kodex/stt-spool")' "$temporary_root/production.yaml" >/dev/null
yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
  (.spec.template.spec.containers[] | select(.name == "stt-tts-service") |
   .readinessProbe.httpGet.path == "/readyz")' "$temporary_root/production.yaml" >/dev/null
yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
  (.spec.template.metadata.labels."kodex.dev/internal-rpc-authority-abi" == "2" and
   ([.spec.template.spec.containers[].name] | contains([
     "stt-tts-service", "internal-rpc-authority-issuer", "internal-rpc-authority-verifier"
   ])) and
   ([.spec.template.spec.containers[] | select(.name == "stt-tts-service") | .env[]?.name] |
     contains(["INTERNAL_RPC_AUTHORITY_ISSUER_SOCKET", "INTERNAL_RPC_AUTHORITY_VERIFIER_SOCKET"])))' \
  "$temporary_root/production.yaml" >/dev/null

jq -e '
  .policy.authority_abi_version == 2 and
  ([.policy.operation_bindings[] |
    select(.operation_id == "platform.stt.policy.resolve") |
    select(has("authority_proof_producer_id") | not) |
    select(.continuation.parent_operation_id == "platform.stt.transcribe")] | length == 1) and
  ([.policy.operation_bindings[] |
    select(.operation_id == "platform.stt.credential.project") |
    select(has("authority_proof_producer_id") | not) |
    select(.continuation.parent_operation_id == "platform.stt.transcribe")] | length == 1)
' "$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json" >/dev/null

jq -e '[.policy.operation_bindings[] |
  select(.operation_id == "platform.stt.model-catalog.get") |
  select(.caller_workload_id == "control-api-gateway" and
    .target_workload_id == "stt-tts-service" and
    .audience == "urn:kodex:internal-rpc:stt-tts-service" and
    .full_method == "/stt.v1.SpeechToTextService/GetModelCatalog" and
    .permission == "system.configuration.manage" and
    .authority_proof_producer_id == "control-plane.oidc-stt" and
    .project_required == false and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN",
      "version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})
  ] | length == 1' \
  "$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json" >/dev/null

yq -e 'select(.kind == "Job" and .metadata.name == "stt-provider-smoke") |
  (.spec.backoffLimit == 0 and .spec.activeDeadlineSeconds == 90 and
   .spec.template.spec.restartPolicy == "Never" and
   .spec.template.spec.containers[0].name == "provider-smoke" and
   (.spec.template.spec.containers[0].command | contains(["/usr/local/bin/stt-provider-smoke"])))' \
  "$temporary_root/provider-smoke.yaml" >/dev/null
yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "stt-provider-smoke-exact-egress") |
  [.spec.egress[].to[]?.podSelector.matchLabels."app.kubernetes.io/name"] |
  contains(["egress-gateway"])' "$temporary_root/provider-smoke.yaml" >/dev/null
yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "egress-gateway-stt-provider-smoke-ingress") |
  [.spec.ingress[].from[]?.podSelector.matchLabels."app.kubernetes.io/name"] |
  contains(["stt-provider-smoke"])' "$temporary_root/provider-smoke.yaml" >/dev/null
if yq -e '.. | select(tag == "!!str") | select(test("sk-[A-Za-z0-9]"))' "$temporary_root/provider-smoke.yaml" >/dev/null 2>&1; then
  printf 'Credential value entered the provider smoke manifest\n' >&2
  exit 1
fi

grep -Fq '56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e' \
  "$repository_root/services/internal/stt-tts-service/internal/providersmoke/smoke.go"
grep -Fq 'rpc Transcribe(stream TranscribeRequest)' "$repository_root/contracts/proto/stt/v1/stt.proto"
grep -Fq 'delegated/continuation proof' "$repository_root/contracts/proto/stt/v1/stt.proto"
if rg -n 'Transcription(Policy|Credential)ProjectionServiceCheckReadiness' "$repository_root/contracts/proto/stt/v1/stt.proto"; then
  printf 'Projection readiness RPC unexpectedly entered the STT contract\n' >&2
  exit 1
fi
if rg -n 'rpc .*TTS|rpc .*Synthesize|service TextToSpeech' "$repository_root/contracts/proto/stt"; then
  printf 'TTS method unexpectedly entered the public contract\n' >&2
  exit 1
fi

docker buildx build --check \
  -f "$repository_root/services/internal/stt-tts-service/Dockerfile" \
  "$repository_root" >/dev/null
(cd "$repository_root/services/internal/stt-tts-service" &&
  env -u GOFLAGS -u KODEX_STT_PROVIDER_SMOKE_OPENAI_API_KEY GOENV=off GOWORK=off go test ./... >/dev/null)

printf 'STT service contract test passed\n'
