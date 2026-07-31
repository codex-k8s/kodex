#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: render-image-build-job.sh staging|production vYYYYMMDDHHMMSS-gitsha source-pvc sha256:source-tar-digest image-name" >&2
}

if [[ $# -ne 5 ]]; then
  usage
  exit 2
fi

environment_name=$1
build_tag=$2
source_pvc=$3
source_digest=$4
image_name=$5

case "$environment_name" in
  staging|production) ;;
  *) usage; exit 2 ;;
esac
if [[ ! "$build_tag" =~ ^v[0-9]{14}-[a-f0-9]{40}$ ]]; then
  echo "build_tag must bind UTC timestamp and exact git SHA" >&2
  exit 2
fi
if [[ ! "$source_pvc" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] ||
  [[ ${#source_pvc} -gt 63 ]]; then
  echo "source_pvc is invalid" >&2
  exit 2
fi
if [[ ! "$source_digest" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  [[ "$source_digest" == "sha256:0000000000000000000000000000000000000000000000000000000000000000" ]]; then
  echo "source_digest is invalid" >&2
  exit 2
fi
if [[ ! "$image_name" =~ ^[a-z0-9]+([._-][a-z0-9]+)*$ ]] ||
  [[ ${#image_name} -gt 80 ]]; then
  echo "image_name is invalid" >&2
  exit 2
fi

git_sha=${build_tag#*-}
job_name="mc-build-${build_tag:1:14}-${git_sha:0:12}"
deadline=1200
if [[ "$environment_name" == production ]]; then
  deadline=1800
fi

cat <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ${job_name}
  namespace: mattercodex-system
  labels:
    app.kubernetes.io/name: mattercodex-role-image-builder
    app.kubernetes.io/component: image-build-client
    mattercodex.dev/image-build-client: "true"
    mattercodex.dev/environment: ${environment_name}
  annotations:
    mattercodex.dev/source-sha256: ${source_digest}
    mattercodex.dev/source-revision: git:${git_sha}
    mattercodex.dev/image-tag: ${build_tag}
spec:
  backoffLimit: 0
  activeDeadlineSeconds: ${deadline}
  ttlSecondsAfterFinished: 86400
  template:
    metadata:
      labels:
        app.kubernetes.io/name: mattercodex-role-image-builder
        app.kubernetes.io/component: image-build-client
        mattercodex.dev/image-build-client: "true"
        mattercodex.dev/environment: ${environment_name}
      annotations:
        mattercodex.dev/source-sha256: ${source_digest}
        mattercodex.dev/source-revision: git:${git_sha}
        mattercodex.dev/image-tag: ${build_tag}
    spec:
      serviceAccountName: mattercodex-role-image-builder
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
      containers:
        - name: build
          image: moby/buildkit:v0.24.0-rootless@sha256:995077ff90af1afff56ff23018699d7511d122b2b111041f2011bd12afd5c0fe
          imagePullPolicy: IfNotPresent
          command:
            - /bin/sh
            - -ec
          args:
            - |
              succeeded=false
              trap '[ "\$succeeded" = true ] || touch /work/build.failed' EXIT
              actual="sha256:\$(sha256sum /source/context.tar | awk '{print \$1}')"
              [ "\$actual" = "${source_digest}" ]
              tar -tf /source/context.tar | while IFS= read -r entry; do
                case "\$entry" in
                  /*|../*|*/../*|*/..) exit 1 ;;
                esac
              done
              mkdir -p /work/context
              tar -xf /source/context.tar -C /work/context
              [ -f /work/context/Dockerfile ]
              buildctl \
                --addr tcp://mattercodex-buildkit.mattercodex-system.svc.cluster.local:1234 \
                --tlscacert /var/run/secrets/mattercodex/buildkit/ca.pem \
                --tlscert /var/run/secrets/mattercodex/buildkit/client.crt \
                --tlskey /var/run/secrets/mattercodex/buildkit/client.key \
                --tlsservername mattercodex-buildkit.mattercodex-system.svc.cluster.local \
                build \
                --frontend dockerfile.v0 \
                --local context=/work/context \
                --local dockerfile=/work/context \
                --opt filename=Dockerfile \
                --opt label:org.opencontainers.image.revision=${git_sha} \
                --opt label:mattercodex.dev/source-sha256=${source_digest} \
                --attest type=provenance,mode=max \
                --output type=image,name=mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001/mattercodex/${image_name}:${build_tag},push=true \
                --metadata-file /work/metadata.json
              digest="\$(sed -n 's/.*"containerimage.digest"[[:space:]]*:[[:space:]]*"\(sha256:[a-f0-9]\{64\}\)".*/\1/p' /work/metadata.json)"
              echo "\$digest" | grep -Eq '^sha256:[a-f0-9]{64}\$'
              printf '%s\n' "\$digest" > /work/image.digest
              touch /work/build.complete
              succeeded=true
          env:
            - name: DOCKER_CONFIG
              value: /var/run/secrets/mattercodex/registry/push
            - name: HOME
              value: /tmp
          resources:
            requests:
              cpu: 250m
              memory: 256Mi
            limits:
              cpu: "2"
              memory: 2Gi
          securityContext:
            runAsNonRoot: true
            runAsUser: 1000
            runAsGroup: 1000
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          volumeMounts:
            - name: source
              mountPath: /source
              readOnly: true
            - name: work
              mountPath: /work
            - name: buildkit-client-tls
              mountPath: /var/run/secrets/mattercodex/buildkit
              readOnly: true
            - name: push-dockerconfig
              mountPath: /var/run/secrets/mattercodex/registry/push
              readOnly: true
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: source
          persistentVolumeClaim:
            claimName: ${source_pvc}
            readOnly: true
        - name: work
          emptyDir:
            sizeLimit: 2Gi
        - name: buildkit-client-tls
          csi:
            driver: secrets-store.csi.k8s.io
            readOnly: true
            volumeAttributes:
              secretProviderClass: mattercodex-role-image-builder-tls
        - name: push-dockerconfig
          secret:
            secretName: mattercodex-image-push
            items:
              - key: .dockerconfigjson
                path: config.json
        - name: tmp
          emptyDir:
            sizeLimit: 16Mi
EOF
