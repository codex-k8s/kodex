# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
ENV GOTOOLCHAIN=local
ARG VERSION=dev
WORKDIR /workspace
COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/controlplaneclient/ libs/go/controlplaneclient/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/runtimesecret/ libs/go/runtimesecret/
COPY libs/go/secretbrokerapi/ libs/go/secretbrokerapi/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY libs/go/securefile/ libs/go/securefile/
COPY services/internal/secret-broker/ services/internal/secret-broker/
WORKDIR /workspace/services/internal/secret-broker
RUN test "$(go env GOVERSION)" = "go1.26.6"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/secret-broker ./cmd/secret-broker

FROM docker.io/library/node:24-bookworm@sha256:934240a162082fd8b8a2f90cd5114446443f1eba1c5378f6687167ca405e6584 AS codex-cli

ARG KODEX_CODEX_PACKAGE=@openai/codex@0.144.1

RUN set -eu; \
    npm install --global "${KODEX_CODEX_PACKAGE}"; \
    binary=$(find /usr/local/lib/node_modules/@openai/codex -type f \
      -path '*/vendor/*/bin/codex' -print -quit); \
    test -n "$binary" && test -x "$binary"; \
    "$binary" --version >/dev/null; \
    install -D -m 0555 "$binary" /out/codex; \
    npm cache clean --force

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime
COPY --from=build --chmod=0555 /out/ /usr/local/bin/
COPY --from=codex-cli --chmod=0555 /out/codex /usr/local/bin/codex
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/secret-broker"]
