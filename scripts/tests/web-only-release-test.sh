#!/usr/bin/env bash
set -euo pipefail

fail() { printf 'Web-only release test failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
render="$temporary_directory/render.yaml"
kubectl kustomize "$repository_root/deploy/k8s/profiles/web-only" >"$render"

yq -o=json -I=0 '.' "$render" | jq -s -e '
  map(select(.kind != null)) as $resources |
  ($resources | length) > 0 and
  ($resources | group_by([.apiVersion,.kind,(.metadata.namespace // ""),.metadata.name]) |
    all(.[]; length == 1))
' >/dev/null || fail 'release render has duplicate resources'
if yq -e 'select(.kind == "SecretProviderClass" or
  (.apiVersion | test("secrets.hashicorp.com|vault.banzaicloud.com")))' "$render" >/dev/null 2>&1; then
  fail 'release render contains a retired secret provider resource'
fi
[[ $(yq -N -r 'select(.kind == "StatefulSet") | .metadata.name' "$render" | sort -u | wc -l) -eq 2 ]] ||
  fail 'web-only stateful dependency count is invalid'
for workload in kodex-postgresql kodex-nats; do
  WORKLOAD_NAME="$workload" yq -e \
    'select(.kind == "StatefulSet" and .metadata.name == strenv(WORKLOAD_NAME))' "$render" >/dev/null ||
    fail "stateful dependency is absent: $workload"
done
for job in kodex-postgresql-runtime-credentials internal-rpc-authority-migrate \
  control-plane-migrate control-plane-broker-bootstrap release-artifact-materializer; do
  JOB_NAME="$job" yq -e 'select(.kind == "Job" and .metadata.name == strenv(JOB_NAME))' \
    "$render" >/dev/null || fail "release Job is absent: $job"
done

secret_references="$temporary_directory/secret-references"
secret_producers="$temporary_directory/secret-producers"
{
  yq -N -r '.. | select(tag == "!!map" and has("secretName")) | .secretName' "$render"
  yq -N -r '.. | select(tag == "!!map" and has("secretKeyRef")) | .secretKeyRef.name' "$render"
  yq -N -r '.. | select(tag == "!!map" and has("secretRef")) | .secretRef.name' "$render"
  yq -N -r '
    select(.kind == "Deployment" or .kind == "StatefulSet" or
      .kind == "DaemonSet" or .kind == "Job" or .kind == "CronJob") |
    (.spec.template.spec.imagePullSecrets // [])[]?.name
  ' "$render"
} | sed '/^null$/d;/^$/d' | sort -u >"$secret_references"
{
  jq -r '.secrets[].name' "$repository_root/tools/install/secret-projections.json"
  printf '%s\n' \
    internal-rpc-authority-bootstrap-roots \
    internal-rpc-authority-sentry \
    kodex-installation-ca \
    kodex-integration-credentials \
    kodex-nats-credentials \
    kodex-postgresql-bootstrap \
    kodex-postgresql-runtime-credentials \
    kodex-sentry \
    runtime-provider-openai-default-r1
  yq -N -r 'select(.kind == "Secret") | .metadata.name' "$render"
  yq -N -r 'select(.kind == "Certificate") | .spec.secretName' "$render"
} | sed '/^null$/d;/^$/d' | sort -u >"$secret_producers"
missing_secrets=$(comm -23 "$secret_references" "$secret_producers")
[[ -z "$missing_secrets" ]] ||
  fail "release references Kubernetes Secrets without a producer: ${missing_secrets//$'\n'/,}"

for script in "$repository_root/install.sh" "$repository_root/tools/install"/*.sh; do
  bash -n "$script"
done
if rg -qi 'vault|secrets-store\.csi|SecretProviderClass' \
  "$repository_root/deploy/k8s/profiles/web-only" \
  "$repository_root/tools/install/secret-projections.json"; then
  fail 'active release profile references retired secret delivery'
fi
printf 'Web-only release test completed\n'
