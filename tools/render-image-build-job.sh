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
command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required" >&2; exit 69; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 69; }

# Builder, tools и policy — immutable owner intent. Значения caller не могут
# попасть в подписанный provenance вместо этой серверной конфигурации.
intent=$(kubectl --namespace mattercodex-system get configmap mattercodex-image-admission-policy -o json)
tools_image=$(jq -er '.data.toolsImage' <<<"$intent")
tools_digest=$(jq -er '.metadata.annotations["mattercodex.dev/admission-tools-sha256"]' <<<"$intent")
policy_revision=$(jq -er '.data.policyRevision' <<<"$intent")
builder_identity=$(jq -er '.data.builderIdentity' <<<"$intent")
build_type=$(jq -er '.data.buildType' <<<"$intent")
required_tools=$(jq -er '.data.requiredTools' <<<"$intent")
jq -e '.immutable == true and .metadata.labels["mattercodex.dev/owner-intent"] == "true"' <<<"$intent" >/dev/null ||
  { echo "build owner intent is not immutable" >&2; exit 78; }
[[ $tools_digest =~ ^sha256:[a-f0-9]{64}$ ]] &&
  [[ $tools_digest != sha256:0000000000000000000000000000000000000000000000000000000000000000 ]] ||
  { echo "build tools digest is invalid" >&2; exit 78; }
[[ $tools_image =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] &&
  [[ ${tools_image##*@} == "$tools_digest" ]] ||
  { echo "build tools image binding is invalid" >&2; exit 78; }
[[ $required_tools == base64,buildctl,cmp,cosign,curl,date,grype,jq,openssl,pgrep,regctl,sha256sum,syft ]] ||
  { echo "build tools contract is invalid" >&2; exit 78; }
[[ $policy_revision =~ ^[1-9][0-9]*$ ]] ||
  { echo "build policy revision is invalid" >&2; exit 78; }
[[ $builder_identity == spiffe://mattercodex.local/ns/mattercodex-system/sa/mattercodex-role-image-builder ]] ||
  { echo "builder identity is invalid" >&2; exit 78; }
[[ $build_type == https://mobyproject.org/buildkit@v1 ]] ||
  { echo "build type is invalid" >&2; exit 78; }

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
    mattercodex.dev/admission-tools-sha256: ${tools_digest}
    mattercodex.dev/admission-policy-revision: "${policy_revision}"
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
        mattercodex.dev/admission-tools-sha256: ${tools_digest}
        mattercodex.dev/admission-policy-revision: "${policy_revision}"
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
          image: ${tools_image}
          imagePullPolicy: IfNotPresent
          command:
            - /bin/sh
            - -ec
          args:
            - |
              succeeded=false
              trap '[ "\$succeeded" = true ] || touch /work/build.failed' EXIT
              for tool in base64 buildctl cosign jq sha256sum; do
                command -v "\$tool" >/dev/null
              done
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
              staging_host=mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001
              staging_tag="\$staging_host/mattercodex/${image_name}:${build_tag}"
              mkdir -p "/tmp/docker/certs.d/\$staging_host"
              cp /var/run/secrets/mattercodex/registry/push/config.json /tmp/docker/config.json
              cp /var/run/secrets/mattercodex/buildkit/ca.pem "/tmp/docker/certs.d/\$staging_host/ca.crt"
              cp /var/run/secrets/mattercodex/buildkit/registry-client.crt "/tmp/docker/certs.d/\$staging_host/client.cert"
              cp /var/run/secrets/mattercodex/buildkit/registry-client.key "/tmp/docker/certs.d/\$staging_host/client.key"
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
                --opt label:mattercodex.dev/build-tag=${build_tag} \
                --opt label:mattercodex.dev/admission-tools-sha256=${tools_digest} \
                --opt label:mattercodex.dev/admission-policy-revision=${policy_revision} \
                --attest type=provenance,mode=max,builder-id=${builder_identity} \
                --output type=image,name="\$staging_tag",push=true \
                --metadata-file /work/metadata.json
              digest="\$(sed -n 's/.*"containerimage.digest"[[:space:]]*:[[:space:]]*"\(sha256:[a-f0-9]\{64\}\)".*/\1/p' /work/metadata.json)"
              echo "\$digest" | grep -Eq '^sha256:[a-f0-9]{64}\$'
              printf '%s\n' "\$digest" > /work/image.digest
              exact_ref="\$staging_host/mattercodex/${image_name}@\$digest"
              cosign download attestation "\$exact_ref" > /work/buildkit-provenance.jsonl
              jq -s '[.[] |
                  select(.payloadType == "application/vnd.in-toto+json") |
                  . as \$envelope |
                  try (\$envelope.payload | @base64d | fromjson) catch empty |
                  select(.predicateType == "https://slsa.dev/provenance/v1") |
                  \$envelope
                ] | if length == 1 then .[0]
                    else error("exactly one BuildKit provenance is required") end' \
                /work/buildkit-provenance.jsonl > /work/buildkit-provenance.dsse.json
              jq -er 'select(.payloadType == "application/vnd.in-toto+json") | .payload' \
                /work/buildkit-provenance.dsse.json | base64 -d > /work/provenance.raw.json
              source_hex=${source_digest#sha256:}
              jq --arg source_uri "mattercodex:source/${build_tag}" \
                --arg source_hex "\$source_hex" --arg source "${source_digest}" \
                --arg image "\${digest#sha256:}" \
                --arg subject "\$staging_host/mattercodex/${image_name}" \
                --arg build_tag "${build_tag}" --arg tools "${tools_digest}" \
                --arg policy "${policy_revision}" --arg builder "${builder_identity}" \
                --arg build_type "${build_type}" '
                  if ._type != "https://in-toto.io/Statement/v1" or
                    .predicateType != "https://slsa.dev/provenance/v1" or
                    (.subject | type) != "array" or (.subject | length) != 1 or
                    .subject[0].digest.sha256 != \$image or
                    .predicate.buildDefinition.buildType != \$build_type or
                    .predicate.runDetails.builder.id != \$builder or
                    (.predicate.buildDefinition.resolvedDependencies | type) != "array" or
                    any(.predicate.buildDefinition.resolvedDependencies[]?;
                      (.uri | startswith("mattercodex:source/")))
                  then error("untrusted BuildKit provenance")
                  else .subject[0].name = \$subject |
                    .predicate.buildDefinition.externalParameters.args[
                      "label:mattercodex.dev/source-sha256"] = \$source |
                    .predicate.buildDefinition.externalParameters.args[
                      "label:mattercodex.dev/build-tag"] = \$build_tag |
                    .predicate.buildDefinition.externalParameters.args[
                      "label:mattercodex.dev/admission-tools-sha256"] = \$tools |
                    .predicate.buildDefinition.externalParameters.args[
                      "label:mattercodex.dev/admission-policy-revision"] = \$policy |
                    .predicate.buildDefinition.resolvedDependencies +=
                      [{uri:\$source_uri,digest:{sha256:\$source_hex}}]
                  end
                ' /work/provenance.raw.json > /work/provenance.normalized.json
              jq -e --arg image "\${digest#sha256:}" --arg source "${source_digest}" \
                --arg subject "\$staging_host/mattercodex/${image_name}" \
                --arg build_tag "${build_tag}" --arg tools_digest "${tools_digest}" \
                --arg policy_revision "${policy_revision}" \
                --arg builder_id "${builder_identity}" --arg build_type "${build_type}" \
                -f /opt/mattercodex/provenance-policy.jq \
                /work/provenance.normalized.json >/dev/null
              export COSIGN_PASSWORD="\$(cat /var/run/secrets/mattercodex/buildkit/builder.password)"
              cosign attest --yes \
                --key /var/run/secrets/mattercodex/buildkit/builder.key \
                --type https://mattercodex.dev/attestation/trusted-build/v1 \
                --predicate /work/provenance.normalized.json "\$exact_ref"
              cosign verify-attestation \
                --key /var/run/secrets/mattercodex/buildkit/builder.pub \
                --type https://mattercodex.dev/attestation/trusted-build/v1 \
                "\$exact_ref" >/work/trusted-build.readback.json
              touch /work/build.complete
              succeeded=true
          env:
            - name: DOCKER_CONFIG
              value: /tmp/docker
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
            - name: admission-script
              mountPath: /opt/mattercodex
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
        - name: admission-script
          configMap:
            name: mattercodex-image-admission
            defaultMode: 0555
        - name: tmp
          emptyDir:
            sizeLimit: 16Mi
EOF
