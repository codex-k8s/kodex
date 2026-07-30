#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)"
renderer="$repo_root/scripts/render-internal-rpc-authority.sh"
test_digest="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
image_ref="ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:$test_digest"
kubernetes_api_cidrs="192.0.2.10/32,2001:db8::10/128"
registry="$repo_root/deploy/k8s/base/internal-rpc-authority/capability-registry.yaml"
config_map="$repo_root/deploy/k8s/base/internal-rpc-authority/configmap.yaml"
dockerfile="$repo_root/services/internal/internal-rpc-authority/Dockerfile"
registered_digest="$(sha256sum "$registry" | awk '{print $1}')"
configured_digest="$(
  yq '.data.INTERNAL_RPC_AUTHORITY_REGISTERED_SET_SOURCE_DIGEST_SHA256' "$config_map"
)"
[[ "$registered_digest" == "$configured_digest" ]]
grep -Fxq \
  '# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6' \
  "$dockerfile"
grep -Eq '^FROM docker\.io/library/golang:1\.26\.5-alpine@sha256:[a-f0-9]{64} AS build$' \
  "$dockerfile"
grep -Eq '^FROM gcr\.io/distroless/static-debian12:nonroot@sha256:[a-f0-9]{64} AS runtime$' \
  "$dockerfile"
! grep -Eq '^(ARG (GOLANG|RUNTIME)_IMAGE=|FROM [^[:space:]]+:[^@[:space:]]+ AS)' \
  "$dockerfile"
grep -Fxq 'USER 65532:65532' "$dockerfile"
! grep -Eq '^[[:space:]]*RUN[[:space:]]' <(
  sed -n '/^FROM gcr\.io\/distroless\/static-debian12:nonroot@sha256:/,$p' "$dockerfile"
)

for environment_name in staging production; do
  rendered="$(
    bash "$renderer" \
      --environment "$environment_name" \
      --image-ref "$image_ref" \
      --kubernetes-api-cidrs "$kubernetes_api_cidrs"
  )"
  grep -Fq "image: $image_ref" <<<"$rendered"
  ! grep -Fq 'sha256:0000000000000000000000000000000000000000000000000000000000000000' <<<"$rendered"
  object_count="$(yq eval-all '[.] | length' <<<"$rendered")"
  [[ "$object_count" -ge 9 ]]
  grep -Fq 'kind: ServiceMonitor' <<<"$rendered"
  grep -Fq 'name: postgresql-ca' <<<"$rendered"
  grep -Fq 'name: internal-rpc-authority-postgresql-from-migrator' <<<"$rendered"
  grep -Fq 'name: internal-rpc-authority-postgresql-from-runtime' <<<"$rendered"
  grep -Fq 'name: vault-from-internal-rpc-authority-reconciler' <<<"$rendered"
  grep -Fq 'name: internal-rpc-authority-kubernetes-api-exact-endpoints' <<<"$rendered"
  grep -Fq 'cidr: 192.0.2.10/32' <<<"$rendered"
  grep -Fq 'cidr: 2001:db8::10/128' <<<"$rendered"
  ! grep -Fq 'component: kube-apiserver' <<<"$rendered"
  grep -Fq 'absent(mattercodex_internal_rpc_authority_database_credential_reconciler_readiness' <<<"$rendered"
  grep -Fq 'port: 4317' <<<"$rendered"
  grep -Fq 'app.kubernetes.io/name: opentelemetry-collector' <<<"$rendered"
  grep -Fq 'app.kubernetes.io/name: sentry-relay' <<<"$rendered"
  grep -Fq 'name: OTEL_EXPORTER_OTLP_TLS_SERVER_NAME' <<<"$rendered"
  grep -Fq 'name: SENTRY_DSN_FILE' <<<"$rendered"
  grep -Fq 'name: internal-rpc-authority-dashboard' <<<"$rendered"
  grep -Fq 'runbook_url: https://docs.mattercodex.dev/runbooks/internal-rpc-authority' <<<"$rendered"
  [[ "$(grep -Fc 'kind: SecretProviderClass' <<<"$rendered")" == "13" ]]
  grep -Fq 'name: internal-rpc-authority-publisher-database-g5' <<<"$rendered"
  grep -Fq 'name: internal-rpc-authority-readback-database-g5' <<<"$rendered"
  grep -Fq 'name: internal-rpc-authority-database-credential-kubernetes-api' <<<"$rendered"
  ! grep -Fq 'secretName: internal-rpc-authority-publisher-postgresql' <<<"$rendered"
  ! grep -Fq 'secretName: internal-rpc-authority-readback-attestor-postgresql' <<<"$rendered"
  [[ "$(grep -Fc 'vaultSkipTLSVerify: "false"' <<<"$rendered")" == "13" ]]
  grep -Fq 'name: internal-rpc-authority-publisher-manifest-signer' <<<"$rendered"
  grep -Fq 'name: internal-rpc-authority-publisher-manifest-trust' <<<"$rendered"
  grep -Fq 'name: internal-rpc-authority-snapshot' <<<"$rendered"
  ! grep -Eq 'vaultSkipTLSVerify: "?true"?' <<<"$rendered"
  runtime_database_sources="$(
    yq eval-all '
      select(.kind == "NetworkPolicy" and
        .metadata.name == "internal-rpc-authority-postgresql-from-runtime") |
      .spec.ingress[].from[].podSelector.matchLabels["app.kubernetes.io/name"]
    ' <<<"$rendered"
  )"
  for source_name in \
    internal-rpc-authority-publisher \
    internal-rpc-authority-readback-attestor \
    internal-rpc-authority-restore-controller \
    internal-rpc-authority-restore-recovery; do
    grep -Fxq "$source_name" <<<"$runtime_database_sources"
  done
  vault_sources="$(
    yq eval-all '
      select(.kind == "NetworkPolicy" and
        .metadata.name == "vault-from-internal-rpc-authority-reconciler") |
      .spec.ingress[].from[].podSelector.matchLabels["app.kubernetes.io/name"]
    ' <<<"$rendered"
  )"
  grep -Fxq 'internal-rpc-authority-publisher' <<<"$vault_sources"
  readback_egress="$(
    yq eval-all '
      select(.kind == "NetworkPolicy" and
        .metadata.name == "internal-rpc-authority-readback-attestor-exact-paths") |
      .spec.egress
    ' <<<"$rendered"
  )"
  grep -Fq 'k8s-app: kube-dns' <<<"$readback_egress"
  ! grep -Fq 'app.kubernetes.io/name: vault' <<<"$readback_egress"
  grep -A3 -F '/usr/local/bin/internal-rpc-authority-cli' <<<"$rendered" |
    grep -Fq -- '- expand'
done

if bash "$renderer" \
  --environment staging \
  --image-ref 'ghcr.io/codex-k8s/matter-codex/internal-rpc-authority:latest' \
  --kubernetes-api-cidrs "$kubernetes_api_cidrs" \
  >/dev/null 2>&1; then
  printf 'FAIL: mutable image tag was accepted\n' >&2
  exit 1
fi

if bash "$renderer" \
  --environment production \
  --image-ref 'ghcr.io/codex-k8s/matter-codex/internal-rpc-authority@sha256:0000000000000000000000000000000000000000000000000000000000000000' \
  --kubernetes-api-cidrs "$kubernetes_api_cidrs" \
  >/dev/null 2>&1; then
  printf 'FAIL: zero image digest was accepted\n' >&2
  exit 1
fi

if bash "$renderer" \
  --environment staging \
  --image-ref "$image_ref" \
  --kubernetes-api-cidrs '0.0.0.0/0' \
  >/dev/null 2>&1; then
  printf 'FAIL: non-exact Kubernetes API destination was accepted\n' >&2
  exit 1
fi

fixture_dir="$(mktemp -d "$repo_root/deploy/k8s/.internal-rpc-authority-render-test.XXXXXX")"
trap 'rm -rf -- "$fixture_dir"' EXIT
issuer_component="../components/internal-rpc-authority-control-api-gateway-issuer"
verifier_component="../components/internal-rpc-authority-control-plane-verifier"
{
  printf '%s\n' \
    'apiVersion: kustomize.config.k8s.io/v1beta1' \
    'kind: Kustomization' \
    'resources:' \
    '  - workloads.yaml' \
    'components:' \
    "  - $issuer_component" \
    "  - $verifier_component"
} >"$fixture_dir/kustomization.yaml"
{
  printf '%s\n' \
    'apiVersion: apps/v1' \
    'kind: Deployment' \
    'metadata:' \
    '  name: control-api-gateway' \
    'spec:' \
    '  selector:' \
    '    matchLabels:' \
    '      app: control-api-gateway' \
    '  template:' \
    '    metadata:' \
    '      labels:' \
    '        app: control-api-gateway' \
    '    spec:' \
    '      containers:' \
    '        - name: application' \
    '          image: example.invalid/application@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \
    '---' \
    'apiVersion: apps/v1' \
    'kind: Deployment' \
    'metadata:' \
    '  name: control-plane' \
    'spec:' \
    '  selector:' \
    '    matchLabels:' \
    '      app: control-plane' \
    '  template:' \
    '    metadata:' \
    '      labels:' \
    '        app: control-plane' \
    '    spec:' \
    '      containers:' \
    '        - name: application' \
    '          image: example.invalid/application@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc'
} >"$fixture_dir/workloads.yaml"
consumer_render="$(
  kubectl kustomize --load-restrictor=LoadRestrictionsNone "$fixture_dir"
)"
grep -Fq 'name: internal-rpc-authority-socket-init' <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-issuer' <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-verifier' <<<"$consumer_render"
grep -Fq 'mountPath: /run/mattercodex' <<<"$consumer_render"
grep -Fq 'kind: PodMonitor' <<<"$consumer_render"
grep -Fq 'name: authority-metrics' <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-postgresql-ca' <<<"$consumer_render"
grep -Fq 'name: INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID' <<<"$consumer_render"
grep -Fq 'name: control-api-gateway-internal-rpc-authority-exact-paths' <<<"$consumer_render"
grep -Fq 'name: control-plane-internal-rpc-authority-exact-paths' <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-vault-token' <<<"$consumer_render"
grep -Fq 'audience: vault' <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-workload-tls' <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-restore-controller-certificate' <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-restore-role-trust' <<<"$consumer_render"
grep -Fq 'name: OTEL_EXPORTER_OTLP_ENDPOINT' <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-observability' <<<"$consumer_render"
grep -Fq 'app.kubernetes.io/name: opentelemetry-collector' <<<"$consumer_render"
grep -Fq 'app.kubernetes.io/name: sentry-relay' <<<"$consumer_render"
[[ "$(grep -Fc 'kind: SecretProviderClass' <<<"$consumer_render")" == "6" ]]
grep -Fq 'name: internal-rpc-authority-control-api-gateway-issuer-key' \
  <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-control-plane-resolver-key' \
  <<<"$consumer_render"
grep -Fq 'name: internal-rpc-authority-resolver-trust' <<<"$consumer_render"
! grep -Fq 'name: internal-rpc-authority-readback-key' <<<"$consumer_render"
! grep -Fq 'name: internal-rpc-authority-readback-credential' <<<"$consumer_render"

printf 'PASS: internal-rpc-authority Kubernetes render boundary\n'
