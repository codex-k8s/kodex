#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-image-admission-job.sh staging|production vYYYYMMDDHHMMSS-gitsha sha256:source-digest image-name sha256:image-digest" >&2
}

if [[ $# -ne 5 ]]; then
  usage
  exit 64
fi

environment_name=$1
build_tag=$2
source_digest=$3
image_name=$4
image_digest=$5

[[ $environment_name == staging || $environment_name == production ]] || { usage; exit 64; }
[[ $build_tag =~ ^v[0-9]{14}-[a-f0-9]{40}$ ]] || { echo "build_tag is invalid" >&2; exit 64; }
[[ $source_digest =~ ^sha256:[a-f0-9]{64}$ ]] &&
  [[ $source_digest != sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  { echo "source_digest is invalid" >&2; exit 64; }
[[ $image_digest =~ ^sha256:[a-f0-9]{64}$ ]] &&
  [[ $image_digest != sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  { echo "image_digest is invalid" >&2; exit 64; }
[[ $image_name =~ ^[a-z0-9]+([._-][a-z0-9]+)*$ ]] && [[ ${#image_name} -le 80 ]] ||
  { echo "image_name is invalid" >&2; exit 64; }
command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required" >&2; exit 69; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 69; }

# Tools image and policy revision are durable owner intent. A caller that may
# request admission cannot substitute either value in this renderer.
intent=$(kubectl --namespace mattercodex-system get configmap mattercodex-image-admission-policy -o json)
tools_image=$(jq -er '.data.toolsImage' <<<"$intent")
policy_revision=$(jq -er '.data.policyRevision' <<<"$intent")
tools_digest=$(jq -er '.metadata.annotations["mattercodex.dev/admission-tools-sha256"]' <<<"$intent")
required_tools=$(jq -er '.data.requiredTools' <<<"$intent")
builder_identity=$(jq -er '.data.builderIdentity' <<<"$intent")
build_type=$(jq -er '.data.buildType' <<<"$intent")
scanner_identity=$(jq -er '.data.scannerIdentity' <<<"$intent")
signer_identity=$(jq -er '.data.signerIdentity' <<<"$intent")
admission_owner_identity=$(jq -er '.data.admissionOwnerIdentity' <<<"$intent")
promotion_identity=$(jq -er '.data.promotionIdentity' <<<"$intent")
jq -e '.immutable == true and .metadata.labels["mattercodex.dev/owner-intent"] == "true"' <<<"$intent" >/dev/null ||
  { echo "admission owner intent is not immutable" >&2; exit 78; }
[[ $tools_image =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] &&
  [[ ${tools_image##*@} == "$tools_digest" ]] ||
  { echo "admission owner intent image binding is invalid" >&2; exit 78; }
[[ $policy_revision =~ ^[1-9][0-9]*$ ]] ||
  { echo "admission owner intent policy revision is invalid" >&2; exit 78; }
[[ $required_tools == base64,buildctl,cmp,cosign,curl,date,grype,jq,openssl,pgrep,regctl,sha256sum,syft ]] ||
  { echo "admission owner intent tools contract is invalid" >&2; exit 78; }
[[ $builder_identity == spiffe://mattercodex.local/ns/mattercodex-system/sa/mattercodex-role-image-builder ]] ||
  { echo "admission builder identity is invalid" >&2; exit 78; }
[[ $build_type == https://mobyproject.org/buildkit@v1 ]] ||
  { echo "admission build type is invalid" >&2; exit 78; }
for identity in "$scanner_identity" "$signer_identity" "$admission_owner_identity" "$promotion_identity"; do
  [[ $identity =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
    { echo "admission phase identity is invalid" >&2; exit 78; }
done

# Полный immutable evidence tuple задаёт отдельное хранилище и replay fence.
# Повтор того же tuple идемпотентен, новая policy/run identity не видит старых markers.
attempt_sha256=$(printf '%s\n' "$source_digest" "$build_tag" "$image_digest" \
  "$tools_digest" "$policy_revision" "$builder_identity" "$build_type" \
  "$scanner_identity" "$signer_identity" "$admission_owner_identity" \
  "$promotion_identity" | sha256sum | awk '{print $1}')
[[ $attempt_sha256 =~ ^[a-f0-9]{64}$ ]] ||
  { echo "admission attempt digest is invalid" >&2; exit 78; }
suffix=${attempt_sha256:0:32}
claim_name="mc-admit-$suffix"
deadline=1800
[[ $environment_name == production ]] && deadline=2700

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
    mattercodex.dev/admission-attempt-sha256: ${attempt_sha256}
spec:
  accessModes: [ReadWriteMany]
  resources:
    requests: {storage: 2Gi}
EOF

emit_job() {
  local phase=$1
  local service_account=$2
  local identity_spc=$3
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
    mattercodex.dev/image-admission: "true"
    mattercodex.dev/image-admission-phase: ${phase}
    mattercodex.dev/image-admission-id: ${suffix}
    mattercodex.dev/environment: ${environment_name}
  annotations:
    mattercodex.dev/source-sha256: ${source_digest}
    mattercodex.dev/image-sha256: ${image_digest}
    mattercodex.dev/build-tag: ${build_tag}
    mattercodex.dev/admission-policy-revision: "${policy_revision}"
    mattercodex.dev/admission-tools-sha256: ${tools_digest}
    mattercodex.dev/admission-attempt-sha256: ${attempt_sha256}
spec:
  backoffLimit: 1
  activeDeadlineSeconds: ${deadline}
  ttlSecondsAfterFinished: 86400
  template:
    metadata:
      labels:
        app.kubernetes.io/name: mattercodex-image-admission
        app.kubernetes.io/component: image-admission
        mattercodex.dev/image-admission: "true"
        mattercodex.dev/image-admission-phase: ${phase}
        mattercodex.dev/image-admission-id: ${suffix}
        mattercodex.dev/environment: ${environment_name}
    spec:
      serviceAccountName: ${service_account}
      automountServiceAccountToken: false
      enableServiceLinks: false
      restartPolicy: Never
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
        fsGroup: 2000
        fsGroupChangePolicy: OnRootMismatch
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: ${phase}
          image: ${tools_image}
          imagePullPolicy: IfNotPresent
          command: [/bin/sh, /opt/mattercodex/image-admission.sh, ${phase}]
          env:
            - {name: SOURCE_DIGEST, value: "${source_digest}"}
            - {name: BUILD_TAG, value: "${build_tag}"}
            - {name: IMAGE_NAME, value: "${image_name}"}
            - {name: IMAGE_DIGEST, value: "${image_digest}"}
            - {name: POLICY_REVISION, value: "${policy_revision}"}
            - {name: ADMISSION_TOOLS_IMAGE, value: "${tools_image}"}
            - {name: ADMISSION_TOOLS_SHA256, value: "${tools_digest}"}
            - {name: ADMISSION_ATTEMPT_SHA256, value: "${attempt_sha256}"}
            - {name: EXPECTED_BUILDER_ID, value: "${builder_identity}"}
            - {name: EXPECTED_BUILD_TYPE, value: "${build_type}"}
            - {name: SCANNER_IDENTITY, value: "${scanner_identity}"}
            - {name: SIGNER_IDENTITY, value: "${signer_identity}"}
            - {name: ADMISSION_OWNER_IDENTITY, value: "${admission_owner_identity}"}
            - {name: PROMOTION_IDENTITY, value: "${promotion_identity}"}
            - {name: ADMISSION_PHASE, value: "${phase}"}
            - {name: HOME, value: /tmp}
          volumeMounts:
            - {name: work, mountPath: /work}
            - {name: script, mountPath: /opt/mattercodex, readOnly: true}
            - {name: identity, mountPath: /identity, readOnly: true}
            - {name: tmp, mountPath: /tmp}
          resources:
            requests: {cpu: 100m, memory: 128Mi}
            limits: {cpu: "1", memory: 1Gi}
          securityContext:
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: [ALL]}
      volumes:
        - name: work
          persistentVolumeClaim: {claimName: ${claim_name}}
        - name: tmp
          emptyDir: {sizeLimit: 64Mi}
        - name: script
          configMap: {name: mattercodex-image-admission, defaultMode: 0555}
        - name: identity
          csi:
            driver: secrets-store.csi.k8s.io
            readOnly: true
            volumeAttributes: {secretProviderClass: ${identity_spc}}
EOF
}

emit_job scan mattercodex-image-scanner mattercodex-image-scanner
emit_job sign mattercodex-image-signer mattercodex-image-signer
emit_job admit mattercodex-image-admission-owner mattercodex-image-admission-owner
emit_job promote mattercodex-image-promotion-writer mattercodex-image-promotion-writer
