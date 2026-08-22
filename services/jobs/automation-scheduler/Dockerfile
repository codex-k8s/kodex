# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

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
COPY services/jobs/automation-scheduler/go.mod services/jobs/automation-scheduler/go.sum services/jobs/automation-scheduler/
WORKDIR /workspace/services/jobs/automation-scheduler
RUN go mod download

WORKDIR /workspace
COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/controlplaneclient/ libs/go/controlplaneclient/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY services/jobs/automation-scheduler/ services/jobs/automation-scheduler/
WORKDIR /workspace/services/jobs/automation-scheduler
RUN test "$(go env GOVERSION)" = "go1.26.6"
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/automation-scheduler ./cmd/automation-scheduler

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime
COPY --from=build --chmod=0555 /out/automation-scheduler /usr/local/bin/automation-scheduler
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/automation-scheduler"]
