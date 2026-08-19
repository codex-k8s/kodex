# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build
ENV GOTOOLCHAIN=local
ARG VERSION=dev
WORKDIR /workspace
COPY libs/go/controlplaneapi/go.mod libs/go/controlplaneapi/go.mod
COPY libs/go/controlplaneclient/go.mod libs/go/controlplaneclient/go.mod
COPY libs/go/internalrpcauth/go.mod libs/go/internalrpcauth/go.mod
COPY services/jobs/legacy-data-migration/go.mod services/jobs/legacy-data-migration/go.sum services/jobs/legacy-data-migration/
WORKDIR /workspace/services/jobs/legacy-data-migration
RUN go mod download

WORKDIR /workspace
COPY libs/go/controlplaneapi/ libs/go/controlplaneapi/
COPY libs/go/controlplaneclient/ libs/go/controlplaneclient/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY services/jobs/legacy-data-migration/ services/jobs/legacy-data-migration/
WORKDIR /workspace/services/jobs/legacy-data-migration
RUN test "$(go env GOVERSION)" = "go1.26.5"
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/legacy-data-migration ./cmd/legacy-data-migration

# pg_dump и pg_restore имеют ту же PostgreSQL major line, что и authoritative DB.
FROM docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7 AS runtime
COPY --from=build --chmod=0555 /out/legacy-data-migration /usr/local/bin/legacy-data-migration
RUN mkdir -p /var/run/mattercodex/legacy-migration-staging && \
    chown 10001:10001 /var/run/mattercodex/legacy-migration-staging && \
    chmod 0700 /var/run/mattercodex/legacy-migration-staging
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/legacy-data-migration"]
