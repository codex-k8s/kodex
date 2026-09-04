# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
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
RUN test "$(go env GOVERSION)" = "go1.26.6"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/stt-tts-service ./cmd/stt-tts-service
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" -o /out/stt-acceptance ./cmd/stt-acceptance

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime
COPY --from=build --chmod=0555 /out/ /usr/local/bin/
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/stt-tts-service"]
