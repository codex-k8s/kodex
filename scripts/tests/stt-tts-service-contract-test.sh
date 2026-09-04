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
  if rg -n 'stt-tts-service|stt-provider-smoke' "$render"; then
    printf 'Incomplete STT unit entered active %s render\n' "$profile" >&2
    exit 1
  fi
done

if jq -e '.images[] | select(.component == "stt-tts-service")' "$repository_root/tools/release/images.json" >/dev/null; then
  printf 'Incomplete STT image entered the active release set\n' >&2
  exit 1
fi

yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
  (.spec.template.spec.terminationGracePeriodSeconds == 35 and
   .spec.template.spec.containers[] | select(.name == "stt-tts-service") |
   .resources.limits.memory == "256Mi")' "$temporary_root/production.yaml" >/dev/null
yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
  .spec.template.spec.volumes[] | select(.name == "stt-spool") |
  .emptyDir.sizeLimit == "64Mi"' "$temporary_root/production.yaml" >/dev/null
yq -e 'select(.kind == "ConfigMap" and .metadata.name == "stt-tts-service-runtime") |
  (.data.STT_REQUEST_TIMEOUT == "20s" and .data.STT_SHUTDOWN_TIMEOUT == "30s" and
   .data.STT_SPOOL_DIRECTORY == "/var/run/kodex/stt-spool")' "$temporary_root/production.yaml" >/dev/null
yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
  (.spec.template.spec.containers[] | select(.name == "stt-tts-service") |
   .readinessProbe.httpGet.path == "/readyz")' "$temporary_root/production.yaml" >/dev/null

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
  env -u GOFLAGS GOENV=off GOWORK=off go test ./... >/dev/null)

printf 'STT service contract test passed\n'
