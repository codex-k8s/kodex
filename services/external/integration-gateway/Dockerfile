# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

ENV GOTOOLCHAIN=local

ARG VERSION=dev

WORKDIR /workspace

COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/controlplaneclient/ libs/go/controlplaneclient/
COPY libs/go/exactkubernetessecret/ libs/go/exactkubernetessecret/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/integrationgatewayapi/ libs/go/integrationgatewayapi/
COPY libs/go/integrationgatewayauth/ libs/go/integrationgatewayauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY services/external/integration-gateway/ services/external/integration-gateway/

WORKDIR /workspace/services/external/integration-gateway

RUN test "$(go env GOVERSION)" = "go1.26.5"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/integration-gateway ./cmd/integration-gateway \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/integration-gateway-cli ./cmd/cli \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/provider-health-adapter ./cmd/provider-health-adapter
FROM docker.io/library/node:24-bookworm-slim@sha256:3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03 AS runtime

ARG CODEX_PACKAGE=@openai/codex@0.144.1

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

RUN set -eu; \
    test -f /etc/apt/sources.list.d/debian.sources; \
    sed -i 's#^URIs: http://deb.debian.org/#URIs: https://deb.debian.org/#' /etc/apt/sources.list.d/debian.sources; \
    grep -Fqx 'URIs: https://deb.debian.org/debian' /etc/apt/sources.list.d/debian.sources; \
    grep -Fqx 'URIs: https://deb.debian.org/debian-security' /etc/apt/sources.list.d/debian.sources; \
    if grep -Eq '^URIs: http://' /etc/apt/sources.list.d/debian.sources; then exit 1; elif [ "$?" -gt 1 ]; then exit 1; fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates git; \
    npm install --global --omit=dev "${CODEX_PACKAGE}"; \
    npm cache clean --force; \
    groupadd --gid 10001 mattercodex; \
    useradd --uid 10001 --gid 10001 --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin mattercodex; \
    install --directory --owner=10001 --group=10001 --mode=0750 /var/lib/mattercodex/integration-gateway; \
    touch /var/lib/mattercodex/integration-gateway/.mountpoint; \
    chown 10001:10001 /var/lib/mattercodex/integration-gateway/.mountpoint; \
    rm -rf /var/lib/apt/lists/*

COPY --from=build --chmod=0555 /out/ /usr/local/bin/

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/integration-gateway"]
