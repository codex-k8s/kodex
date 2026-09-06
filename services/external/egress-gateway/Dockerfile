# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build

ENV GOTOOLCHAIN=local

ARG VERSION=dev

WORKDIR /workspace

COPY libs/go/grpcserver/go.mod libs/go/grpcserver/go.mod
COPY libs/go/httpserver/go.mod libs/go/httpserver/go.mod
COPY libs/go/observability/go.mod libs/go/observability/go.mod
COPY libs/go/serviceruntime/go.mod libs/go/serviceruntime/go.mod
COPY libs/go/securefile/go.mod libs/go/securefile/go.mod
COPY libs/go/emailbridgeapi/go.mod libs/go/emailbridgeapi/go.mod
COPY libs/go/mailpolicy/go.mod libs/go/mailpolicy/go.mod
COPY libs/go/dnsresolver/go.mod libs/go/dnsresolver/go.mod
COPY services/external/egress-gateway/go.mod services/external/egress-gateway/go.sum services/external/egress-gateway/

WORKDIR /workspace/services/external/egress-gateway
RUN go mod download

WORKDIR /workspace
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY libs/go/securefile/ libs/go/securefile/
COPY libs/go/emailbridgeapi/ libs/go/emailbridgeapi/
COPY libs/go/mailpolicy/ libs/go/mailpolicy/
COPY libs/go/dnsresolver/ libs/go/dnsresolver/
COPY services/external/egress-gateway/ services/external/egress-gateway/

WORKDIR /workspace/services/external/egress-gateway

RUN test "$(go env GOVERSION)" = "go1.26.6"
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/egress-gateway ./cmd/egress-gateway

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime

COPY --from=build --chmod=0555 /out/egress-gateway /usr/local/bin/egress-gateway

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/egress-gateway"]
