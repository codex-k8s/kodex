#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-image-admission-job.sh staging|production vYYYYMMDDHHMMSS-gitsha sha256:source-digest image-name sha256:image-digest admission-tools-image@sha256:digest policy-revision" >&2
}

if [[ $# -ne 7 ]]; then
  usage
  exit 64
fi

environment_name=$1
build_tag=$2
source_digest=$3
image_name=$4
image_digest=$5
tools_image=$6
policy_revision=$7

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
[[ $tools_image =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] ||
  { echo "admission tools image must be immutable" >&2; exit 64; }
[[ $policy_revision =~ ^[1-9][0-9]*$ ]] || { echo "policy_revision is invalid" >&2; exit 64; }

job_name="mc-admit-${image_digest:7:20}"
deadline=1800
[[ $environment_name == production ]] && deadline=2700

cat <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ${job_name}
  namespace: mattercodex-system
  labels:
    app.kubernetes.io/name: mattercodex-image-admission
    app.kubernetes.io/component: image-admission
    mattercodex.dev/image-admission: "true"
    mattercodex.dev/environment: ${environment_name}
  annotations:
    mattercodex.dev/source-sha256: ${source_digest}
    mattercodex.dev/image-sha256: ${image_digest}
    mattercodex.dev/build-tag: ${build_tag}
    mattercodex.dev/admission-policy-revision: "${policy_revision}"
spec:
  backoffLimit: 0
  activeDeadlineSeconds: ${deadline}
  ttlSecondsAfterFinished: 86400
  template:
    metadata:
      labels:
        app.kubernetes.io/name: mattercodex-image-admission
        app.kubernetes.io/component: image-admission
        mattercodex.dev/image-admission: "true"
        mattercodex.dev/environment: ${environment_name}
    spec:
      serviceAccountName: mattercodex-image-admission
      automountServiceAccountToken: false
      enableServiceLinks: false
      restartPolicy: Never
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
        fsGroup: 2000
        fsGroupChangePolicy: OnRootMismatch
        seccompProfile:
          type: RuntimeDefault
      initContainers:
        - name: evidence-scanner
          image: ${tools_image}
          imagePullPolicy: IfNotPresent
          command: [/bin/sh, /opt/mattercodex/image-admission.sh, scan]
          env: &admission-env
            - {name: SOURCE_DIGEST, value: "${source_digest}"}
            - {name: BUILD_TAG, value: "${build_tag}"}
            - {name: IMAGE_NAME, value: "${image_name}"}
            - {name: IMAGE_DIGEST, value: "${image_digest}"}
            - {name: POLICY_REVISION, value: "${policy_revision}"}
            - {name: ADMISSION_TOOLS_IMAGE, value: "${tools_image}"}
            - {name: HOME, value: /tmp}
          volumeMounts:
            - {name: work, mountPath: /work}
            - {name: script, mountPath: /opt/mattercodex, readOnly: true}
            - {name: registry-ca, mountPath: /registry-ca, readOnly: true}
            - {name: push-client, mountPath: /registry-push, readOnly: true}
            - {name: tmp, mountPath: /tmp}
          securityContext: &locked-context
            runAsNonRoot: true
            runAsUser: 10001
            runAsGroup: 10001
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: [ALL]}
          resources: &admission-resources
            requests: {cpu: 100m, memory: 128Mi}
            limits: {cpu: "1", memory: 1Gi}
        - name: evidence-signer
          image: ${tools_image}
          imagePullPolicy: IfNotPresent
          command: [/bin/sh, /opt/mattercodex/image-admission.sh, sign]
          env: *admission-env
          volumeMounts:
            - {name: work, mountPath: /work}
            - {name: script, mountPath: /opt/mattercodex, readOnly: true}
            - {name: registry-ca, mountPath: /registry-ca, readOnly: true}
            - {name: push-client, mountPath: /registry-push, readOnly: true}
            - {name: signing, mountPath: /signing, readOnly: true}
            - {name: tmp, mountPath: /tmp}
          securityContext: *locked-context
          resources: *admission-resources
        - name: admission-owner
          image: ${tools_image}
          imagePullPolicy: IfNotPresent
          command: [/bin/sh, /opt/mattercodex/image-admission.sh, admit]
          env: *admission-env
          volumeMounts:
            - {name: work, mountPath: /work}
            - {name: script, mountPath: /opt/mattercodex, readOnly: true}
            - {name: registry-ca, mountPath: /registry-ca, readOnly: true}
            - {name: push-client, mountPath: /registry-push, readOnly: true}
            - {name: admission-owner, mountPath: /admission, readOnly: true}
            - {name: tmp, mountPath: /tmp}
          securityContext: *locked-context
          resources: *admission-resources
      containers:
        - name: promotion
          image: ${tools_image}
          imagePullPolicy: IfNotPresent
          command: [/bin/sh, /opt/mattercodex/image-admission.sh, promote]
          env: *admission-env
          volumeMounts:
            - {name: work, mountPath: /work}
            - {name: script, mountPath: /opt/mattercodex, readOnly: true}
            - {name: registry-ca, mountPath: /registry-ca, readOnly: true}
            - {name: push-client, mountPath: /registry-push, readOnly: true}
            - {name: promotion-client, mountPath: /registry-promotion, readOnly: true}
            - {name: admission-public, mountPath: /admission-public, readOnly: true}
            - {name: tmp, mountPath: /tmp}
          securityContext: *locked-context
          resources: *admission-resources
      volumes:
        - name: work
          emptyDir: {sizeLimit: 2Gi}
        - name: tmp
          emptyDir: {sizeLimit: 64Mi}
        - name: script
          configMap:
            name: mattercodex-image-admission
            defaultMode: 0555
        - name: registry-ca
          secret: {secretName: mattercodex-image-registry-ca}
        - name: push-client
          secret: {secretName: mattercodex-image-push-client}
        - name: promotion-client
          secret: {secretName: mattercodex-image-promotion-client}
        - name: signing
          csi:
            driver: secrets-store.csi.k8s.io
            readOnly: true
            volumeAttributes: {secretProviderClass: mattercodex-image-signer}
        - name: admission-owner
          csi:
            driver: secrets-store.csi.k8s.io
            readOnly: true
            volumeAttributes: {secretProviderClass: mattercodex-image-admission-owner}
        - name: admission-public
          csi:
            driver: secrets-store.csi.k8s.io
            readOnly: true
            volumeAttributes: {secretProviderClass: mattercodex-image-promotion-verifier}
EOF
