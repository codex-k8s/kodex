# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS build
ENV GOTOOLCHAIN=local
ARG VERSION=dev
WORKDIR /workspace
COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/controlplaneclient/ libs/go/controlplaneclient/
COPY libs/go/eventing/ libs/go/eventing/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/modelmeasurementwire/ libs/go/modelmeasurementwire/
COPY libs/go/providerfinancewire/ libs/go/providerfinancewire/
COPY libs/go/repositorybundle/ libs/go/repositorybundle/
COPY libs/go/runtimecontract/ libs/go/runtimecontract/
COPY libs/go/runtimepolicywire/ libs/go/runtimepolicywire/
COPY libs/go/secretbrokerapi/ libs/go/secretbrokerapi/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY libs/go/securefile/ libs/go/securefile/
COPY services/internal/runtime-controller/ services/internal/runtime-controller/
WORKDIR /workspace/services/internal/runtime-controller
RUN test "$(go env GOVERSION)" = "go1.26.10"
RUN go mod download
RUN --mount=type=tmpfs,target=/tmp,size=4294967296 \
    export GOCACHE=/tmp/go-build-cache GOTMPDIR=/tmp/go-tmp GOMAXPROCS=2 GOMEMLIMIT=768MiB \
 && mkdir -p "$GOCACHE" "$GOTMPDIR" \
 && CGO_ENABLED=0 GOOS=linux go build -p=1 -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/runtime-controller ./cmd/runtime-controller

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime
COPY --from=build --chmod=0555 /out/ /usr/local/bin/
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/runtime-controller"]
