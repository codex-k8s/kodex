#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary_root="$(mktemp -d)"
trap 'rm -rf "$temporary_root"' EXIT

for source in \
  deploy/k8s/overlays/staging/stt-tts-service \
  deploy/k8s/overlays/production/stt-tts-service \
  deploy/k8s/profiles/web-only \
  deploy/k8s/profiles/web-with-mattermost; do
  kubectl kustomize "$repository_root/$source" >"$temporary_root/$(echo "$source" | tr / _).yaml"
done

for profile in web-only web-with-mattermost; do
  render="$temporary_root/deploy_k8s_profiles_${profile}.yaml"
  yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
    (.spec.template.spec.serviceAccountName == "stt-tts-service" and
    .spec.template.spec.automountServiceAccountToken == false)' "$render" >/dev/null
  yq -e 'select(.kind == "Deployment" and .metadata.name == "stt-tts-service") |
    .spec.template.spec.containers[] | select(.name == "stt-tts-service") |
    (.securityContext.readOnlyRootFilesystem == true and
    .securityContext.allowPrivilegeEscalation == false)' "$render" >/dev/null
done

yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == "stt-tts-service-exact-runtime-paths") |
  [.spec.egress[].to[]?.podSelector.matchLabels."app.kubernetes.io/name"] |
  contains(["control-plane", "secret-broker", "egress-gateway"])' \
  "$temporary_root/deploy_k8s_overlays_production_stt-tts-service.yaml" >/dev/null

production_profile="$temporary_root/deploy_k8s_profiles_web-only.yaml"
for owner_policy in control-plane-exact-runtime-paths secret-broker-exact-runtime-paths egress-gateway-exact-runtime-paths; do
  owner_policy="$owner_policy" yq -e 'select(.kind == "NetworkPolicy" and .metadata.name == strenv(owner_policy)) |
    [.spec.ingress[].from[]?.podSelector.matchLabels."app.kubernetes.io/name"] |
    contains(["stt-tts-service"])' "$production_profile" >/dev/null
done

grep -Fq 'https://api.openai.com/v1/audio/transcriptions' \
  "$repository_root/services/internal/stt-tts-service/internal/clients/openai/client.go"
grep -Fq 'http://egress-gateway.kodex-system.svc.cluster.local:8080' \
  "$repository_root/services/internal/stt-tts-service/internal/clients/openai/client.go"
docker buildx build --check \
  -f "$repository_root/services/internal/stt-tts-service/Dockerfile" \
  "$repository_root" >/dev/null
if rg -n 'rpc .*TTS|rpc .*Synthesize|service TextToSpeech' "$repository_root/contracts/proto/stt"; then
  printf 'TTS method unexpectedly entered the public contract\n' >&2
  exit 1
fi

(cd "$repository_root/services/internal/stt-tts-service" &&
  env -u GOFLAGS GOENV=off GOWORK=off go test ./... >/dev/null)

printf 'STT service contract test passed\n'
