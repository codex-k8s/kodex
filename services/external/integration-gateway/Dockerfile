# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build
ENV GOTOOLCHAIN=local
ARG VERSION=dev
WORKDIR /workspace

COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/controlplaneclient/ libs/go/controlplaneclient/
COPY libs/go/credentialfs/ libs/go/credentialfs/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY services/external/integration-gateway/ services/external/integration-gateway/

WORKDIR /workspace/services/external/integration-gateway
RUN test "$(go env GOVERSION)" = "go1.26.5"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/integration-gateway ./cmd/integration-gateway

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime
COPY --from=build --chmod=0555 /out/integration-gateway /usr/local/bin/integration-gateway
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/integration-gateway"]
