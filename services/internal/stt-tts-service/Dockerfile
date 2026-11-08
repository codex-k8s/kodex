# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS build
ENV GOTOOLCHAIN=local
ARG VERSION=dev
WORKDIR /workspace
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/securefile/ libs/go/securefile/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY libs/go/sttapi/ libs/go/sttapi/
COPY services/internal/stt-tts-service/ services/internal/stt-tts-service/
WORKDIR /workspace/services/internal/stt-tts-service
RUN test "$(go env GOVERSION)" = "go1.26.10"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/stt-tts-service ./cmd/stt-tts-service
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" -o /out/stt-provider-smoke ./cmd/stt-provider-smoke

FROM docker.io/library/alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40 AS runtime
RUN apk add --no-cache ffmpeg=8.0.1-r1 ca-certificates=20261020-r0
COPY --from=build --chmod=0555 /out/ /usr/local/bin/
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/stt-tts-service"]
