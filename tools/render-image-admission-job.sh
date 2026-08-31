#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-image-admission-job.sh staging|production vYYYYMMDDHHMMSS-revision [all|claim|scan|sign|admit|promote]" >&2
}

if [[ $# -lt 2 || $# -gt 3 ]]; then
  usage
  exit 64
fi

environment_name=$1
run_id=$2
requested_phase=${3:-all}
[[ $environment_name == staging || $environment_name == production ]] || { usage; exit 64; }
[[ $run_id =~ ^v[0-9]{14}-[a-f0-9]{40}$ ]] || { echo "run_id is invalid" >&2; exit 64; }
[[ $requested_phase =~ ^(all|claim|scan|sign|admit|promote)$ ]] || { usage; exit 64; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 69; }

# Версионированный ConfigMap задаёт только owner intent. Artifact/build tuple
# выдаётся control-plane после запуска claim phase и не принимается от caller.
if [[ -n ${IMAGE_ADMISSION_POLICY_JSON:-} ]]; then
  [[ ${#IMAGE_ADMISSION_POLICY_JSON} -le 65536 ]] || { echo "admission policy document is too large" >&2; exit 78; }
  intent=$IMAGE_ADMISSION_POLICY_JSON
else
  command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required" >&2; exit 69; }
  intent=$(kubectl --namespace kodex-system get configmap kodex-image-admission-policy -o json)
fi
tools_image=$(jq -er '.data.toolsImage' <<<"$intent")
admission_image=$(jq -er '.data.admissionImage' <<<"$intent")
authority_image=$(jq -er '.data.authorityImage' <<<"$intent")
promotion_repository=$(jq -er '.data.promotionRepository' <<<"$intent")
promotion_evidence_repository=$(jq -er '.data.promotionEvidenceRepository' <<<"$intent")
evidence_repository=$(jq -er '.data.evidenceRepository' <<<"$intent")
promoted_pull_repository=$(jq -er '.data.promotedPullRepository' <<<"$intent")
policy_revision=$(jq -er '.data.policyRevision' <<<"$intent")
policy_sha256=$(jq -er '.data.policySHA256' <<<"$intent")
tools_digest=$(jq -er '.metadata.annotations["kodex.dev/admission-tools-sha256"]' <<<"$intent")
required_tools=$(jq -er '.data.requiredTools' <<<"$intent")
builder_identity=$(jq -er '.data.builderIdentity' <<<"$intent")
build_type=$(jq -er '.data.buildType' <<<"$intent")
trusted_role_base_repository=$(jq -er '.data.trustedRoleBaseRepository' <<<"$intent")
trusted_role_base_digest=$(jq -er '.data.trustedRoleBaseDigest' <<<"$intent")
role_runtime_contract_revision=$(jq -er '.data.roleRuntimeContractRevision' <<<"$intent")
role_runtime_contract_sha256=$(jq -er '.data.roleRuntimeContractSHA256' <<<"$intent")
local_profile=$(jq -r '.metadata.labels["kodex.dev/local-profile"] // ""' <<<"$intent")
jq -e '.immutable == true and .metadata.labels["kodex.dev/owner-intent"] == "true"' <<<"$intent" >/dev/null ||
  { echo "admission owner intent is not immutable" >&2; exit 78; }
[[ -z $local_profile || $local_profile == hot-reload ]] ||
  { echo "admission local profile is invalid" >&2; exit 78; }
for image in "$tools_image" "$admission_image" "$authority_image"; do
  [[ $image =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
    { echo "admission image binding is invalid" >&2; exit 78; }
done
[[ ${tools_image##*@} == "$tools_digest" ]] || { echo "admission tools digest mismatch" >&2; exit 78; }
for repository in "$promotion_repository" "$promotion_evidence_repository" "$evidence_repository" "$promoted_pull_repository"; do
  [[ $repository =~ ^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*$ ]] ||
    { echo "promotion repository binding is invalid" >&2; exit 78; }
done
[[ ${promotion_repository#*/} == "${promoted_pull_repository#*/}" ]] ||
  { echo "promotion and pull repository paths differ" >&2; exit 78; }
[[ $evidence_repository == kodex-image-registry-evidence.kodex-system.svc.cluster.local:5007/evidence/role-image-admission ]] ||
  { echo "evidence repository binding is invalid" >&2; exit 78; }
[[ $promotion_evidence_repository == kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003/kodex/evidence ]] ||
  { echo "promotion evidence repository binding is invalid" >&2; exit 78; }
[[ $policy_revision =~ ^[1-9][0-9]*$ ]] || { echo "policy revision is invalid" >&2; exit 78; }
[[ $policy_sha256 =~ ^[a-f0-9]{64}$ ]] || { echo "policy digest is invalid" >&2; exit 78; }
[[ $required_tools == base64,cmp,cosign,grype,image-admission-bridge,jq,regctl,sha256sum,syft,wc ]] ||
  { echo "admission tools contract is invalid" >&2; exit 78; }
[[ $builder_identity == spiffe://kodex.local/ns/kodex-system/sa/role-image-builder ]] ||
  { echo "builder identity is invalid" >&2; exit 78; }
[[ $build_type == https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md ]] ||
  { echo "build type is invalid" >&2; exit 78; }
[[ $trusted_role_base_repository =~ ^[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9./_-]*$ ]] ||
  { echo "trusted role base repository is invalid" >&2; exit 78; }
[[ $trusted_role_base_digest =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "trusted role base digest is invalid" >&2; exit 78; }
[[ $role_runtime_contract_revision =~ ^[1-9][0-9]*$ ]] || { echo "runtime contract revision is invalid" >&2; exit 78; }
[[ $role_runtime_contract_sha256 =~ ^[a-f0-9]{64}$ ]] || { echo "runtime contract digest is invalid" >&2; exit 78; }

run_sha256=$(printf '%s\n' "$environment_name" "$run_id" "$admission_image" "$authority_image" "$tools_digest" \
  "$policy_revision" "$policy_sha256" "$promotion_repository" "$promotion_evidence_repository" \
  "$evidence_repository" "$promoted_pull_repository" | sha256sum | awk '{print $1}')
suffix=${run_sha256:0:32}
claim_name="mc-admit-$suffix"
# Claim TTL равен 15 минутам; каждый Job вместе с повторами завершается раньше.
deadline=720

emit_pvc() {
  cat <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${claim_name}
  namespace: kodex-system
  labels:
    app.kubernetes.io/name: kodex-image-admission
    kodex.dev/image-admission-id: ${suffix}
  annotations:
    kodex.dev/admission-run-sha256: ${run_sha256}
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests: {storage: 2Gi}
EOF
}

emit_job() {
  local phase=$1 service_account=$2 identity_secret=$3 protected=${4:-false}
  local workload="" grant_signer_secret=""
  if [[ $phase == claim || $phase == admit ]]; then
    workload='image-admission'
    grant_signer_secret='image-admission-platform-worker-grant-signer'
  elif [[ $phase == promote ]]; then
    workload='image-promotion'
    grant_signer_secret='image-promotion-platform-worker-grant-signer'
  fi
  cat <<EOF
---
apiVersion: batch/v1
kind: Job
metadata:
  name: ${claim_name}-${phase}
  namespace: kodex-system
  labels:
    app.kubernetes.io/name: kodex-image-admission
    app.kubernetes.io/component: image-admission
    kodex.dev/image-admission-phase: ${phase}
    kodex.dev/image-admission-id: ${suffix}
  annotations:
    kodex.dev/admission-run-sha256: ${run_sha256}
    kodex.dev/admission-policy-revision: "${policy_revision}"
spec:
  backoffLimit: 1
  activeDeadlineSeconds: ${deadline}
  ttlSecondsAfterFinished: 3600
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kodex-image-admission
        app.kubernetes.io/component: image-admission
        kodex.dev/image-admission-phase: ${phase}
        kodex.dev/image-admission-id: ${suffix}
        kodex.dev/environment: ${environment_name}
EOF
  if [[ $local_profile == hot-reload ]]; then
    cat <<EOF
        kodex.dev/local-profile: hot-reload
EOF
  fi
  if [[ $protected == true ]]; then
    cat <<EOF
        kodex.dev/internal-rpc-authority-issuer: enabled
EOF
  fi
  cat <<EOF
    spec:
      serviceAccountName: ${service_account}
      automountServiceAccountToken: false
      enableServiceLinks: false
      restartPolicy: Never
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
        fsGroup: 29000
        fsGroupChangePolicy: OnRootMismatch
        seccompProfile: {type: RuntimeDefault}
EOF
  if [[ $protected == true ]]; then
    cat <<EOF
      initContainers:
        - name: internal-rpc-authority-socket-init
          image: ${authority_image}
          command: [/usr/local/bin/internal-rpc-authority-socket-init]
          securityContext: {runAsNonRoot: true, runAsUser: 29000, runAsGroup: 29000, allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
          volumeMounts: [{name: authority-sockets, mountPath: /run/kodex}]
        - name: internal-rpc-authority-issuer
          restartPolicy: Always
          image: ${authority_image}
          command: [/usr/local/bin/internal-rpc-authority-issuer]
          env:
            - {name: DEPLOYMENT_ENVIRONMENT, value: "${environment_name}"}
EOF
    if [[ $local_profile == hot-reload ]]; then
      cat <<EOF
            - {name: OTEL_SDK_DISABLED, value: "true"}
EOF
    fi
    cat <<EOF
            - {name: OTEL_EXPORTER_OTLP_ENDPOINT, value: otel-collector.observability.svc:4317}
            - {name: OTEL_EXPORTER_OTLP_TLS_SERVER_NAME, value: otel-collector.observability.svc.cluster.local}
            - {name: OTEL_EXPORTER_OTLP_CA_FILE, value: /var/run/config/kodex/internal-rpc-authority/observability/otel-ca.pem}
            - {name: OTEL_TRACES_SAMPLER_ARG, value: "0.1"}
            - {name: SENTRY_DSN_FILE, value: /var/run/secrets/kodex/internal-rpc-authority/observability/sentry-dsn}
            - {name: SENTRY_EXPECTED_HOST, value: sentry-relay.observability.svc:8443}
            - {name: INTERNAL_RPC_AUTHORITY_WORKLOAD_ID, value: "${workload}"}
            - {name: INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID, value: "spiffe://kodex.local/ns/kodex-system/sa/${workload}"}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_ADDRESS, value: internal-rpc-authority-readback-attestor.kodex-system.svc:8443}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_SERVER_NAME, value: internal-rpc-authority-readback-attestor.kodex-system.svc}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_CA_FILE, value: /var/run/config/kodex/internal-rpc-authority/readback/ca.pem}
            - {name: INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CA_FILE, value: /var/run/config/kodex/internal-rpc-authority/restore/ca.pem}
            - {name: INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID, value: "10001"}
            - {name: INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID, value: "10001"}
            - {name: INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE, value: /var/run/secrets/kodex/internal-rpc-authority/postgres/dsn}
            - name: INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER
              valueFrom: {secretKeyRef: {name: internal-rpc-authority-${workload}-issuer-postgresql, key: username}}
            - {name: INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN, value: ":9091"}
          startupProbe: {httpGet: {path: /readyz, port: 9091}, periodSeconds: 2, failureThreshold: 30}
          readinessProbe: {httpGet: {path: /readyz, port: 9091}, periodSeconds: 5, timeoutSeconds: 3}
          livenessProbe: {httpGet: {path: /healthz, port: 9091}, periodSeconds: 10, timeoutSeconds: 2}
          resources: {requests: {cpu: 25m, memory: 32Mi}, limits: {cpu: 250m, memory: 128Mi}}
          securityContext: {runAsNonRoot: true, runAsUser: 29001, runAsGroup: 29000, allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
          volumeMounts:
            - {name: authority-sockets, mountPath: /run/kodex}
            - {name: authority-snapshot, mountPath: /var/run/config/kodex/internal-rpc-authority/snapshot, readOnly: true}
            - {name: authority-bootstrap-roots, mountPath: /usr/local/share/internal-rpc-authority/manifest-root, readOnly: true}
            - {name: authority-manifest-trust, mountPath: /var/run/config/kodex/internal-rpc-authority/manifest-trust, readOnly: true}
            - {name: authority-proof-trust, mountPath: /var/run/config/kodex/internal-rpc-authority/authority-proof-trust, readOnly: true}
            - {name: authority-issuer-key, mountPath: /var/run/secrets/kodex/internal-rpc-authority/issuer, readOnly: true}
            - {name: authority-workload-tls, mountPath: /var/run/secrets/kodex/internal-rpc-authority/workload-tls, readOnly: true}
            - {name: authority-readback-ca, mountPath: /var/run/config/kodex/internal-rpc-authority/readback, readOnly: true}
            - {name: authority-readback-credential, mountPath: /var/run/secrets/kodex/internal-rpc-authority/readback/credential, readOnly: true}
            - {name: authority-readback-possession, mountPath: /var/run/secrets/kodex/internal-rpc-authority/readback/possession, readOnly: true}
            - {name: authority-restore-ca, mountPath: /var/run/config/kodex/internal-rpc-authority/restore, readOnly: true}
            - {name: authority-restore-certificate, mountPath: /var/run/config/kodex/internal-rpc-authority/restore/controller-trust, readOnly: true}
            - {name: authority-restore-role-trust, mountPath: /var/run/config/kodex/internal-rpc-authority/restore/role-trust, readOnly: true}
            - {name: authority-restore-credential, mountPath: /var/run/secrets/kodex/internal-rpc-authority/restore/credential, readOnly: true}
            - {name: authority-restore-ack, mountPath: /var/run/secrets/kodex/internal-rpc-authority/restore/ack, readOnly: true}
            - {name: authority-postgresql, mountPath: /var/run/secrets/kodex/internal-rpc-authority/postgres, readOnly: true}
            - {name: authority-postgresql-ca, mountPath: /var/run/config/kodex/internal-rpc-authority/postgresql, readOnly: true}
            - {name: authority-observability, mountPath: /var/run/config/kodex/internal-rpc-authority/observability, readOnly: true}
            - {name: authority-sentry-dsn, mountPath: /var/run/secrets/kodex/internal-rpc-authority/observability, readOnly: true}
        - name: platform-worker-grant-agent
          restartPolicy: Always
          image: ${authority_image}
          command: [/usr/local/bin/internal-rpc-authority-platform-worker-grant-agent]
          env:
            - {name: PLATFORM_WORKER_GRANT_WORKLOAD_ID, value: "${workload}"}
            - {name: PLATFORM_WORKER_GRANT_OUTPUT_FILE, value: /application-grant/application-grant.jws}
          startupProbe: {httpGet: {path: /readyz, port: 9093}, periodSeconds: 2, timeoutSeconds: 2, failureThreshold: 30}
          readinessProbe: {httpGet: {path: /readyz, port: 9093}, periodSeconds: 5, timeoutSeconds: 2, failureThreshold: 2}
          livenessProbe: {httpGet: {path: /healthz, port: 9093}, periodSeconds: 10, timeoutSeconds: 2, failureThreshold: 3}
          resources: {requests: {cpu: 5m, memory: 16Mi}, limits: {cpu: 100m, memory: 48Mi}}
          securityContext: {runAsNonRoot: true, runAsUser: 29004, runAsGroup: 29000, allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
          volumeMounts:
            - {name: application-grant, mountPath: /application-grant}
            - {name: platform-worker-grant-signer, mountPath: /var/run/secrets/kodex/platform-worker-grant-signer, readOnly: true}
EOF
  fi
  cat <<EOF
      containers:
        - name: ${phase}
          image: ${admission_image}
          imagePullPolicy: IfNotPresent
          command: [/bin/sh, /opt/kodex/image-admission.sh, ${phase}]
          env:
            - {name: ADMISSION_RUN_ID, value: "${run_id}"}
            - {name: POLICY_REVISION, value: "${policy_revision}"}
            - {name: POLICY_SHA256, value: "${policy_sha256}"}
            - {name: ADMISSION_TOOLS_IMAGE, value: "${tools_image}"}
            - {name: ADMISSION_IMAGE, value: "${admission_image}"}
            - {name: PROMOTION_REPOSITORY, value: "${promotion_repository}"}
            - {name: PROMOTION_EVIDENCE_REPOSITORY, value: "${promotion_evidence_repository}"}
            - {name: EVIDENCE_REPOSITORY, value: "${evidence_repository}"}
            - {name: PROMOTED_PULL_REPOSITORY, value: "${promoted_pull_repository}"}
            - {name: EXPECTED_BUILDER_ID, value: "${builder_identity}"}
            - {name: EXPECTED_BUILD_TYPE, value: "${build_type}"}
            - {name: TRUSTED_ROLE_BASE_REPOSITORY, value: "${trusted_role_base_repository}"}
            - {name: TRUSTED_ROLE_BASE_DIGEST, value: "${trusted_role_base_digest}"}
            - {name: ROLE_RUNTIME_CONTRACT_REVISION, value: "${role_runtime_contract_revision}"}
            - {name: ROLE_RUNTIME_CONTRACT_SHA256, value: "${role_runtime_contract_sha256}"}
            - {name: HOME, value: /tmp}
EOF
  if [[ $protected == true ]]; then
    cat <<EOF
            - {name: INTERNAL_RPC_AUTHORITY_ISSUER_SOCKET, value: /run/kodex/internal-rpc-authority/issuer.sock}
            - {name: INTERNAL_RPC_AUTHORITY_LOCAL_ROLE, value: issuer}
            - {name: IMAGE_OWNER_CONTROL_PLANE_TARGET, value: control-plane.kodex-system.svc:8443}
            - {name: IMAGE_OWNER_CONTROL_PLANE_TLS_SERVER_NAME, value: control-plane.kodex-system.svc.cluster.local}
            - {name: IMAGE_OWNER_CONTROL_PLANE_CA_FILE, value: /control-plane/ca.pem}
            - {name: IMAGE_OWNER_CONTROL_PLANE_CERTIFICATE_FILE, value: /workload-tls/tls.crt}
            - {name: IMAGE_OWNER_CONTROL_PLANE_PRIVATE_KEY_FILE, value: /workload-tls/tls.key}
            - {name: IMAGE_OWNER_APPLICATION_GRANT_FILE, value: /application-grant/application-grant.jws}
            - {name: IMAGE_OWNER_STATE_FILE, value: /work/owner-claim.json}
            - {name: IMAGE_OWNER_PROMOTION_FILE, value: /work/owner-promotion.json}
EOF
  fi
  cat <<EOF
          volumeMounts:
            - {name: work, mountPath: /work}
            - {name: script, mountPath: /opt/kodex, readOnly: true}
            - {name: identity, mountPath: /identity, readOnly: true}
            - {name: tmp, mountPath: /tmp}
EOF
  if [[ $protected == true ]]; then
    cat <<EOF
            - {name: authority-sockets, mountPath: /run/kodex, readOnly: true}
            - {name: authority-workload-tls, mountPath: /workload-tls, readOnly: true}
            - {name: control-plane-ca, mountPath: /control-plane, readOnly: true}
            - {name: application-grant, mountPath: /application-grant, readOnly: true}
EOF
  fi
  cat <<EOF
          resources: {requests: {cpu: 100m, memory: 128Mi}, limits: {cpu: "1", memory: 1Gi}}
          securityContext: {runAsNonRoot: true, runAsUser: 10001, runAsGroup: 10001, allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
      volumes:
EOF
  if [[ $phase == promote ]]; then
    cat <<EOF
        - {name: work, emptyDir: {sizeLimit: 256Mi}}
EOF
  else
    cat <<EOF
        - name: work
          persistentVolumeClaim: {claimName: ${claim_name}}
EOF
  fi
  cat <<EOF
        - {name: tmp, emptyDir: {sizeLimit: 64Mi}}
        - {name: script, configMap: {name: kodex-image-admission, defaultMode: 0555}}
        - {name: identity, secret: {secretName: ${identity_secret}, defaultMode: 0440}}
EOF
  if [[ $protected == true ]]; then
    cat <<EOF
        - {name: authority-sockets, emptyDir: {sizeLimit: 64Mi}}
        - {name: authority-snapshot, secret: {secretName: internal-rpc-authority-snapshot, defaultMode: 0440}}
        - name: authority-bootstrap-roots
          secret:
            secretName: internal-rpc-authority-bootstrap-roots
            defaultMode: 0444
            items:
              - {key: manifest-root-public.jwk, path: bootstrap-public.jwk}
              - {key: manifest-root-metadata.json, path: bootstrap-metadata.json}
        - {name: authority-manifest-trust, secret: {secretName: internal-rpc-authority-${workload}-manifest-trust, defaultMode: 0440}}
        - {name: authority-proof-trust, secret: {secretName: internal-rpc-authority-${workload}-proof-trust, defaultMode: 0440}}
        - {name: authority-issuer-key, secret: {secretName: internal-rpc-authority-${workload}-issuer-key, defaultMode: 0440}}
        - {name: authority-workload-tls, secret: {secretName: internal-rpc-authority-${workload}-workload-tls, defaultMode: 0440}}
        - {name: control-plane-ca, configMap: {name: kodex-internal-ca, defaultMode: 0440}}
        - {name: application-grant, emptyDir: {sizeLimit: 1Mi}}
        - {name: platform-worker-grant-signer, secret: {secretName: ${grant_signer_secret}, defaultMode: 0440}}
        - {name: authority-readback-ca, configMap: {name: internal-rpc-authority-readback-attestor-ca, defaultMode: 0440}}
        - {name: authority-readback-credential, secret: {secretName: internal-rpc-authority-${workload}-issuer-readback-credential, defaultMode: 0440}}
        - {name: authority-readback-possession, secret: {secretName: internal-rpc-authority-${workload}-issuer-readback-possession, defaultMode: 0440}}
        - {name: authority-restore-ca, configMap: {name: internal-rpc-authority-restore-controller-ca, defaultMode: 0440}}
        - {name: authority-restore-certificate, secret: {secretName: internal-rpc-authority-restore-controller-tls, defaultMode: 0440, items: [{key: tls.crt, path: tls.crt}]}}
        - {name: authority-restore-role-trust, secret: {secretName: internal-rpc-authority-restore-role-trust, defaultMode: 0440, items: [{key: restore-role-trust.jws, path: restore-role-trust.jws}]}}
        - {name: authority-restore-credential, secret: {secretName: internal-rpc-authority-${workload}-issuer-restore-credential, defaultMode: 0440}}
        - {name: authority-restore-ack, secret: {secretName: internal-rpc-authority-${workload}-issuer-restore-ack, defaultMode: 0440}}
        - {name: authority-postgresql, secret: {secretName: internal-rpc-authority-${workload}-issuer-postgresql, defaultMode: 0440, items: [{key: dsn, path: dsn}, {key: username, path: username}]}}
        - {name: authority-postgresql-ca, configMap: {name: internal-rpc-authority-postgresql-ca, defaultMode: 0440}}
        - {name: authority-observability, configMap: {name: internal-rpc-authority-otel-ca, defaultMode: 0440, items: [{key: ca.pem, path: otel-ca.pem}]}}
        - {name: authority-sentry-dsn, secret: {secretName: internal-rpc-authority-sentry, defaultMode: 0440, items: [{key: dsn, path: sentry-dsn}]}}
EOF
  fi
}

if [[ $requested_phase == all || $requested_phase == claim ]]; then
  emit_pvc
  emit_job claim image-admission kodex-image-admission-owner true
fi
[[ $requested_phase != all && $requested_phase != scan ]] || emit_job scan kodex-image-scanner kodex-image-scanner false
[[ $requested_phase != all && $requested_phase != sign ]] || emit_job sign kodex-image-signer kodex-image-signer false
[[ $requested_phase != all && $requested_phase != admit ]] || emit_job admit image-admission kodex-image-admission-owner true
[[ $requested_phase != all && $requested_phase != promote ]] || emit_job promote image-promotion kodex-image-promotion-writer true
