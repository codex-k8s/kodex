# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
ENV GOTOOLCHAIN=local
ARG VERSION=dev
WORKDIR /workspace

COPY libs/go/httpserver/go.mod libs/go/httpserver/
COPY libs/go/grpcserver/go.mod libs/go/grpcserver/
COPY libs/go/objectstorage/go.mod libs/go/objectstorage/go.sum libs/go/objectstorage/
COPY libs/go/observability/go.mod libs/go/observability/
COPY libs/go/serviceruntime/go.mod libs/go/serviceruntime/
COPY libs/go/securefile/go.mod libs/go/securefile/
COPY services/jobs/artifact-retention/go.mod services/jobs/artifact-retention/go.sum services/jobs/artifact-retention/
WORKDIR /workspace/services/jobs/artifact-retention
RUN go mod download

WORKDIR /workspace
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/objectstorage/ libs/go/objectstorage/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY libs/go/securefile/ libs/go/securefile/
COPY services/jobs/artifact-retention/ services/jobs/artifact-retention/
WORKDIR /workspace/services/jobs/artifact-retention
RUN test "$(go env GOVERSION)" = "go1.26.6"
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/artifact-retention ./cmd/artifact-retention

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime
COPY --from=build --chmod=0555 /out/artifact-retention /usr/local/bin/artifact-retention
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/artifact-retention"]
