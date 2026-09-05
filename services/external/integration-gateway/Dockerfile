# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
ENV GOTOOLCHAIN=local
ARG VERSION=dev
WORKDIR /workspace

COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/controlplaneclient/ libs/go/controlplaneclient/
COPY libs/go/credentialfs/ libs/go/credentialfs/
COPY libs/go/emailbridgeapi/ libs/go/emailbridgeapi/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/integrationpackage/ libs/go/integrationpackage/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY libs/go/securefile/ libs/go/securefile/
COPY services/external/integration-gateway/ services/external/integration-gateway/

WORKDIR /workspace/services/external/integration-gateway
RUN test "$(go env GOVERSION)" = "go1.26.6"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/integration-gateway ./cmd/integration-gateway

FROM docker.io/library/alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS runtime
RUN apk add --no-cache git=2.52.0-r0 ca-certificates && \
    addgroup -g 10001 kodex && adduser -D -H -u 10001 -G kodex kodex
COPY --from=build --chmod=0555 /out/integration-gateway /usr/local/bin/integration-gateway
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/integration-gateway"]
