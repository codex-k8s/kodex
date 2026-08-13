#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-image-admission-job.sh staging|production vYYYYMMDDHHMMSS-gitsha" >&2
}

if [[ $# -ne 2 ]]; then
  usage
  exit 64
fi

environment_name=$1
run_id=$2
[[ $environment_name == staging || $environment_name == production ]] || { usage; exit 64; }
[[ $run_id =~ ^v[0-9]{14}-[a-f0-9]{40}$ ]] || { echo "run_id is invalid" >&2; exit 64; }
command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required" >&2; exit 69; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 69; }

# Версионированный ConfigMap задаёт только owner intent. Artifact/build tuple
# выдаётся control-plane после запуска claim phase и не принимается от caller.
intent=$(kubectl --namespace mattercodex-system get configmap mattercodex-image-admission-policy -o json)
tools_image=$(jq -er '.data.toolsImage' <<<"$intent")
admission_image=$(jq -er '.data.admissionImage' <<<"$intent")
authority_image=$(jq -er '.data.authorityImage' <<<"$intent")
promotion_repository=$(jq -er '.data.promotionRepository' <<<"$intent")
promotion_evidence_repository=$(jq -er '.data.promotionEvidenceRepository' <<<"$intent")
evidence_repository=$(jq -er '.data.evidenceRepository' <<<"$intent")
promoted_pull_repository=$(jq -er '.data.promotedPullRepository' <<<"$intent")
policy_revision=$(jq -er '.data.policyRevision' <<<"$intent")
policy_sha256=$(jq -er '.data.policySHA256' <<<"$intent")
tools_digest=$(jq -er '.metadata.annotations["mattercodex.dev/admission-tools-sha256"]' <<<"$intent")
required_tools=$(jq -er '.data.requiredTools' <<<"$intent")
builder_identity=$(jq -er '.data.builderIdentity' <<<"$intent")
build_type=$(jq -er '.data.buildType' <<<"$intent")
trusted_role_base_repository=$(jq -er '.data.trustedRoleBaseRepository' <<<"$intent")
trusted_role_base_digest=$(jq -er '.data.trustedRoleBaseDigest' <<<"$intent")
role_runtime_contract_revision=$(jq -er '.data.roleRuntimeContractRevision' <<<"$intent")
role_runtime_contract_sha256=$(jq -er '.data.roleRuntimeContractSHA256' <<<"$intent")
jq -e '.immutable == true and .metadata.labels["mattercodex.dev/owner-intent"] == "true"' <<<"$intent" >/dev/null ||
  { echo "admission owner intent is not immutable" >&2; exit 78; }
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
[[ $evidence_repository == mattercodex-image-registry-evidence.mattercodex-system.svc.cluster.local:5007/evidence/role-image-admission ]] ||
  { echo "evidence repository binding is invalid" >&2; exit 78; }
[[ $promotion_evidence_repository == mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003/mattercodex/evidence ]] ||
  { echo "promotion evidence repository binding is invalid" >&2; exit 78; }
[[ $policy_revision =~ ^[1-9][0-9]*$ ]] || { echo "policy revision is invalid" >&2; exit 78; }
[[ $policy_sha256 =~ ^[a-f0-9]{64}$ ]] || { echo "policy digest is invalid" >&2; exit 78; }
[[ $required_tools == base64,cmp,cosign,grype,image-admission-bridge,jq,regctl,sha256sum,syft,wc ]] ||
  { echo "admission tools contract is invalid" >&2; exit 78; }
[[ $builder_identity == spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder ]] ||
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

cat <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${claim_name}
  namespace: mattercodex-system
  labels:
    app.kubernetes.io/name: mattercodex-image-admission
    mattercodex.dev/image-admission-id: ${suffix}
  annotations:
    mattercodex.dev/admission-run-sha256: ${run_sha256}
spec:
  accessModes: [ReadWriteMany]
  resources:
    requests: {storage: 2Gi}
EOF

emit_job() {
  local phase=$1 service_account=$2 identity_spc=$3 protected=${4:-false}
  local workload="" grant_spc=""
  if [[ $phase == claim || $phase == admit ]]; then
    workload=image-admission
    grant_spc=image-admission-application-grant
  elif [[ $phase == promote ]]; then
    workload=image-promotion
    grant_spc=image-promotion-application-grant
  fi
  cat <<EOF
---
apiVersion: batch/v1
kind: Job
metadata:
  name: ${claim_name}-${phase}
  namespace: mattercodex-system
  labels:
    app.kubernetes.io/name: mattercodex-image-admission
    app.kubernetes.io/component: image-admission
    mattercodex.dev/image-admission-phase: ${phase}
    mattercodex.dev/image-admission-id: ${suffix}
  annotations:
    mattercodex.dev/admission-run-sha256: ${run_sha256}
    mattercodex.dev/admission-policy-revision: "${policy_revision}"
spec:
  backoffLimit: 1
  activeDeadlineSeconds: ${deadline}
  ttlSecondsAfterFinished: 86400
  template:
    metadata:
      labels:
        app.kubernetes.io/name: mattercodex-image-admission
        app.kubernetes.io/component: image-admission
        mattercodex.dev/image-admission-phase: ${phase}
        mattercodex.dev/image-admission-id: ${suffix}
        mattercodex.dev/environment: ${environment_name}
EOF
  if [[ $protected == true ]]; then
    cat <<EOF
        mattercodex.dev/internal-rpc-authority-issuer: enabled
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
          volumeMounts: [{name: authority-sockets, mountPath: /run/mattercodex}]
        - name: internal-rpc-authority-issuer
          restartPolicy: Always
          image: ${authority_image}
          command: [/usr/local/bin/internal-rpc-authority-issuer]
          env:
            - {name: DEPLOYMENT_ENVIRONMENT, value: "${environment_name}"}
            - {name: OTEL_EXPORTER_OTLP_ENDPOINT, value: otel-collector.observability.svc:4317}
            - {name: OTEL_EXPORTER_OTLP_TLS_SERVER_NAME, value: otel-collector.observability.svc.cluster.local}
            - {name: OTEL_EXPORTER_OTLP_CA_FILE, value: /var/run/config/mattercodex/internal-rpc-authority/observability/otel-ca.pem}
            - {name: OTEL_TRACES_SAMPLER_ARG, value: "0.1"}
            - {name: SENTRY_DSN_FILE, value: /var/run/secrets/mattercodex/internal-rpc-authority/observability/sentry-dsn}
            - {name: SENTRY_EXPECTED_HOST, value: sentry-relay.observability.svc:8443}
            - {name: INTERNAL_RPC_AUTHORITY_WORKLOAD_ID, value: "${workload}"}
            - {name: INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID, value: "spiffe://mattercodex.local/ns/mattercodex-system/sa/${workload}"}
            - {name: INTERNAL_RPC_AUTHORITY_VAULT_AUTH_ROLE, value: "internal-rpc-authority-${workload}"}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_ADDRESS, value: internal-rpc-authority-readback-attestor.mattercodex-system.svc:8443}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_SERVER_NAME, value: internal-rpc-authority-readback-attestor.mattercodex-system.svc}
            - {name: INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_CA_FILE, value: /var/run/config/mattercodex/internal-rpc-authority/readback/ca.pem}
            - {name: INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CA_FILE, value: /var/run/config/mattercodex/internal-rpc-authority/restore/ca.pem}
            - {name: INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID, value: "10001"}
            - {name: INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID, value: "10001"}
            - {name: INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE, value: /var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn}
            - name: INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER
              valueFrom: {secretKeyRef: {name: internal-rpc-authority-${workload}-issuer-postgresql, key: username}}
            - {name: INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN, value: ":9091"}
          startupProbe: {httpGet: {path: /readyz, port: 9091}, periodSeconds: 2, failureThreshold: 30}
          readinessProbe: {httpGet: {path: /readyz, port: 9091}, periodSeconds: 5, timeoutSeconds: 3}
          livenessProbe: {httpGet: {path: /livez, port: 9091}, periodSeconds: 10, timeoutSeconds: 2}
          resources: {requests: {cpu: 25m, memory: 32Mi}, limits: {cpu: 250m, memory: 128Mi}}
          securityContext: {runAsNonRoot: true, runAsUser: 29001, runAsGroup: 29000, allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
          volumeMounts:
            - {name: authority-sockets, mountPath: /run/mattercodex}
            - {name: authority-snapshot, mountPath: /var/run/config/mattercodex/internal-rpc-authority/snapshot, readOnly: true}
            - {name: authority-manifest-trust, mountPath: /var/run/config/mattercodex/internal-rpc-authority/manifest-trust, readOnly: true}
            - {name: authority-proof-trust, mountPath: /var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust, readOnly: true}
            - {name: authority-issuer-key, mountPath: /var/run/secrets/mattercodex/internal-rpc-authority/issuer, readOnly: true}
            - {name: authority-workload-tls, mountPath: /var/run/secrets/mattercodex/internal-rpc-authority/workload-tls, readOnly: true}
            - {name: authority-readback-ca, mountPath: /var/run/config/mattercodex/internal-rpc-authority/readback, readOnly: true}
            - {name: authority-vault-ca, mountPath: /var/run/config/mattercodex/internal-rpc-authority/vault, readOnly: true}
            - {name: authority-vault-token, mountPath: /var/run/secrets/tokens/vault, readOnly: true}
            - {name: authority-restore-ca, mountPath: /var/run/config/mattercodex/internal-rpc-authority/restore, readOnly: true}
            - {name: authority-restore-certificate, mountPath: /var/run/config/mattercodex/internal-rpc-authority/restore/controller-trust, readOnly: true}
            - {name: authority-restore-role-trust, mountPath: /var/run/config/mattercodex/internal-rpc-authority/restore/role-trust, readOnly: true}
            - {name: authority-postgresql, mountPath: /var/run/secrets/mattercodex/internal-rpc-authority/postgres, readOnly: true}
            - {name: authority-postgresql-ca, mountPath: /var/run/config/mattercodex/internal-rpc-authority/postgresql, readOnly: true}
            - {name: authority-observability, mountPath: /var/run/config/mattercodex/internal-rpc-authority/observability, readOnly: true}
            - {name: authority-sentry-dsn, mountPath: /var/run/secrets/mattercodex/internal-rpc-authority/observability, readOnly: true}
EOF
  fi
  cat <<EOF
      containers:
        - name: ${phase}
          image: ${admission_image}
          imagePullPolicy: IfNotPresent
          command: [/bin/sh, /opt/mattercodex/image-admission.sh, ${phase}]
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
            - {name: INTERNAL_RPC_AUTHORITY_ISSUER_SOCKET, value: /run/mattercodex/internal-rpc-authority/issuer.sock}
            - {name: INTERNAL_RPC_AUTHORITY_LOCAL_ROLE, value: issuer}
            - {name: IMAGE_OWNER_CONTROL_PLANE_TARGET, value: control-plane.mattercodex-system.svc:8443}
            - {name: IMAGE_OWNER_CONTROL_PLANE_TLS_SERVER_NAME, value: control-plane.mattercodex-system.svc.cluster.local}
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
            - {name: script, mountPath: /opt/mattercodex, readOnly: true}
            - {name: identity, mountPath: /identity, readOnly: true}
            - {name: tmp, mountPath: /tmp}
EOF
  if [[ $protected == true ]]; then
    cat <<EOF
            - {name: authority-sockets, mountPath: /run/mattercodex, readOnly: true}
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
        - {name: script, configMap: {name: mattercodex-image-admission, defaultMode: 0555}}
        - name: identity
          csi:
            driver: secrets-store.csi.k8s.io
            readOnly: true
            volumeAttributes: {secretProviderClass: ${identity_spc}}
EOF
  if [[ $protected == true ]]; then
    cat <<EOF
        - {name: authority-sockets, emptyDir: {sizeLimit: 8Mi}}
        - {name: authority-snapshot, secret: {secretName: internal-rpc-authority-snapshot, defaultMode: 0440}}
        - {name: authority-manifest-trust, csi: {driver: secrets-store.csi.k8s.io, readOnly: true, volumeAttributes: {secretProviderClass: internal-rpc-authority-${workload}-manifest-trust}}}
        - {name: authority-proof-trust, csi: {driver: secrets-store.csi.k8s.io, readOnly: true, volumeAttributes: {secretProviderClass: internal-rpc-authority-${workload}-proof-trust}}}
        - {name: authority-issuer-key, csi: {driver: secrets-store.csi.k8s.io, readOnly: true, volumeAttributes: {secretProviderClass: internal-rpc-authority-${workload}-issuer-key}}}
        - {name: authority-workload-tls, secret: {secretName: internal-rpc-authority-${workload}-workload-tls, defaultMode: 0440}}
        - {name: control-plane-ca, configMap: {name: mattercodex-internal-ca, defaultMode: 0440}}
        - {name: application-grant, csi: {driver: secrets-store.csi.k8s.io, readOnly: true, volumeAttributes: {secretProviderClass: ${grant_spc}}}}
        - {name: authority-readback-ca, configMap: {name: internal-rpc-authority-readback-attestor-ca, defaultMode: 0440}}
        - {name: authority-vault-ca, configMap: {name: internal-rpc-authority-vault-ca, defaultMode: 0440}}
        - name: authority-vault-token
          projected: {defaultMode: 0400, sources: [{serviceAccountToken: {path: token, audience: vault, expirationSeconds: 600}}]}
        - {name: authority-restore-ca, configMap: {name: internal-rpc-authority-restore-controller-ca, defaultMode: 0440}}
        - {name: authority-restore-certificate, secret: {secretName: internal-rpc-authority-restore-controller-tls, defaultMode: 0440, items: [{key: tls.crt, path: tls.crt}]}}
        - {name: authority-restore-role-trust, secret: {secretName: internal-rpc-authority-restore-role-trust, defaultMode: 0440, items: [{key: restore-role-trust.jws, path: restore-role-trust.jws}]}}
        - {name: authority-postgresql, secret: {secretName: internal-rpc-authority-${workload}-issuer-postgresql, defaultMode: 0440, items: [{key: dsn, path: dsn}, {key: username, path: username}]}}
        - {name: authority-postgresql-ca, configMap: {name: internal-rpc-authority-postgresql-ca, defaultMode: 0440}}
        - {name: authority-observability, configMap: {name: internal-rpc-authority-otel-ca, defaultMode: 0440, items: [{key: ca.pem, path: otel-ca.pem}]}}
        - {name: authority-sentry-dsn, secret: {secretName: internal-rpc-authority-sentry, defaultMode: 0440, items: [{key: dsn, path: sentry-dsn}]}}
EOF
  fi
}

emit_job claim image-admission mattercodex-image-admission-owner true
emit_job scan mattercodex-image-scanner mattercodex-image-scanner false
emit_job sign mattercodex-image-signer mattercodex-image-signer false
emit_job admit image-admission mattercodex-image-admission-owner true
emit_job promote image-promotion mattercodex-image-promotion-writer true
