# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

ARG ADMISSION_TOOLS_IMAGE=admission-tools.invalid/mattercodex/image-admission-tools@sha256:0000000000000000000000000000000000000000000000000000000000000000

FROM docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
ENV GOTOOLCHAIN=local
ARG VERSION=dev
WORKDIR /workspace

COPY libs/go/controlplaneapi/go.mod libs/go/controlplaneapi/go.mod
COPY libs/go/controlplaneclient/go.mod libs/go/controlplaneclient/go.mod
COPY libs/go/grpcserver/go.mod libs/go/grpcserver/go.mod
COPY libs/go/httpserver/go.mod libs/go/httpserver/go.mod
COPY libs/go/internalrpcauth/go.mod libs/go/internalrpcauth/go.mod
COPY libs/go/observability/go.mod libs/go/observability/go.mod
COPY libs/go/serviceruntime/go.mod libs/go/serviceruntime/go.mod
COPY libs/go/securefile/go.mod libs/go/securefile/go.mod
COPY services/jobs/role-image-builder/go.mod services/jobs/role-image-builder/go.sum services/jobs/role-image-builder/
WORKDIR /workspace/services/jobs/role-image-builder
RUN go mod download

WORKDIR /workspace
COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/controlplaneclient/ libs/go/controlplaneclient/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY libs/go/securefile/ libs/go/securefile/
COPY services/jobs/role-image-builder/ services/jobs/role-image-builder/
WORKDIR /workspace/services/jobs/role-image-builder
RUN test "$(go env GOVERSION)" = "go1.26.6"
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/role-image-builder ./cmd/role-image-builder
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/image-admission-bridge ./cmd/image-admission-bridge
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/image-admission-controller ./cmd/image-admission-controller
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/registry-write-authorizer ./cmd/registry-write-authorizer
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/registry-pull-authorizer ./cmd/registry-pull-authorizer
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/node-pull-bootstrap ./cmd/node-pull-bootstrap

FROM moby/buildkit:v0.24.0-rootless@sha256:995077ff90af1afff56ff23018699d7511d122b2b111041f2011bd12afd5c0fe AS runtime
COPY --from=build --chmod=0555 /out/role-image-builder /usr/local/bin/role-image-builder
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/role-image-builder"]

FROM ${ADMISSION_TOOLS_IMAGE} AS admission-runtime
COPY --from=build --chmod=0555 /out/image-admission-bridge /usr/local/bin/image-admission-bridge
COPY --from=build --chmod=0555 /out/image-admission-controller /usr/local/bin/image-admission-controller
COPY --from=build --chmod=0555 /out/registry-write-authorizer /usr/local/bin/registry-write-authorizer
COPY --from=build --chmod=0555 /out/registry-pull-authorizer /usr/local/bin/registry-pull-authorizer
COPY --from=build --chmod=0555 /out/node-pull-bootstrap /usr/local/bin/node-pull-bootstrap
COPY --chmod=0555 tools/render-image-admission-job.sh /opt/mattercodex/render-image-admission-job.sh
USER 10001:10001
