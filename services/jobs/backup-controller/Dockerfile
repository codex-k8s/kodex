# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS build

ENV GOTOOLCHAIN=local

ARG VERSION=dev

WORKDIR /workspace

COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/httpserver/ libs/go/httpserver/
COPY libs/go/objectstorage/ libs/go/objectstorage/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/securefile/ libs/go/securefile/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY services/jobs/backup-controller/ services/jobs/backup-controller/

WORKDIR /workspace/services/jobs/backup-controller

RUN test "$(go env GOVERSION)" = "go1.26.10"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/backup-controller ./cmd/backup-controller

FROM docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7 AS runtime

COPY --from=build --chmod=0555 /out/backup-controller /usr/local/bin/backup-controller

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/backup-controller"]
