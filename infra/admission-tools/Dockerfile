# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM ghcr.io/sigstore/cosign/cosign:v3.1.3@sha256:9e5c2f2edc34351160407ca3416c61855bdf9403c3c5936e0f0be7fc261611b8 AS cosign
FROM docker.io/anchore/syft:v1.51.0@sha256:678bfa565b60f747aac0f8e964fe5588a24445b8d0a480e91f6efd70020dfbb0 AS syft
FROM docker.io/anchore/grype:v0.117.0@sha256:ddf9e9f204049f3a4a0955ef70873cabab6a31432125ad4f20a490b54950a253 AS grype
FROM docker.io/regclient/regctl:v0.11.5@sha256:dbe356c6cf9f8f85e302b9e47fed481ef3f1b04807350e99b02ab2cadee0a993 AS regctl

FROM docker.io/library/alpine:3.22.1@sha256:eafc1edb577d2e9b458664a15f23ea1c370214193226069eb22921169fc7e43f
ARG SOURCE_SHA=unknown
RUN apk add --no-cache \
      ca-certificates=20260611-r0 \
      coreutils=9.7-r1 \
      curl=8.14.1-r3 \
      grep=3.12-r0 \
      jq=1.8.1-r0 \
      openssl=3.5.7-r0 \
      procps-ng=4.0.4-r3
COPY --from=cosign --chmod=0555 /ko-app/cosign /usr/local/bin/cosign
COPY --from=syft --chmod=0555 /syft /usr/local/bin/syft
COPY --from=grype --chmod=0555 /grype /usr/local/bin/grype
COPY --from=regctl --chmod=0555 /regctl /usr/local/bin/regctl
RUN for tool in base64 cmp cosign curl grype jq openssl pgrep regctl sha256sum syft wc; do \
      command -v "$tool" >/dev/null || exit 1; \
    done
LABEL org.opencontainers.image.title="MatterCodex image admission tools" \
      org.opencontainers.image.source="https://github.com/codex-k8s/matter-codex" \
      org.opencontainers.image.revision="$SOURCE_SHA"
USER 10001:10001
