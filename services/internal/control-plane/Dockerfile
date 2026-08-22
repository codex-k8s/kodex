# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

ENV GOTOOLCHAIN=local

ARG VERSION=dev

WORKDIR /workspace

COPY libs/go/cache/ libs/go/cache/
COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/eventing/ libs/go/eventing/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/integrationgatewayauth/ libs/go/integrationgatewayauth/
COPY libs/go/interactiongatewayapi/ libs/go/interactiongatewayapi/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/oidcidentity/ libs/go/oidcidentity/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY services/internal/control-plane/ services/internal/control-plane/

WORKDIR /workspace/services/internal/control-plane

RUN test "$(go env GOVERSION)" = "go1.26.5"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/control-plane ./cmd/control-plane \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/control-plane-cli ./cmd/cli

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime

COPY --from=build --chmod=0555 /out/ /usr/local/bin/

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/control-plane"]
