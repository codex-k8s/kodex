# syntax=docker.io/docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

FROM docker.io/library/golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

ENV GOTOOLCHAIN=local

ARG VERSION=dev

WORKDIR /workspace

COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
COPY contracts/authorization/v1/bootstrap-key-delivery-targets.yaml \
    contracts/authorization/v1/bootstrap-key-delivery-targets.yaml
COPY services/internal/internal-rpc-authority/ services/internal/internal-rpc-authority/

WORKDIR /workspace/services/internal/internal-rpc-authority

RUN test "$(go env GOVERSION)" = "go1.26.5"
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/internal-rpc-authority-socket-init \
    ./cmd/internal-rpc-authority-socket-init \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/internal-rpc-authority-restore-pitr \
    ./cmd/internal-rpc-authority-restore-pitr \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/internal-rpc-authority-local-readiness \
    ./cmd/internal-rpc-authority-local-readiness \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/internal-rpc-authority-issuer \
    ./cmd/internal-rpc-authority-issuer \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/internal-rpc-authority-verifier \
    ./cmd/internal-rpc-authority-verifier \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/internal-rpc-authority-database-credential-reconciler \
    ./cmd/internal-rpc-authority-database-credential-reconciler \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/internal-rpc-authority-publisher \
    ./cmd/internal-rpc-authority-publisher \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/internal-rpc-authority-readback-attestor \
    ./cmd/internal-rpc-authority-readback-attestor \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/internal-rpc-authority-restore-controller \
    ./cmd/internal-rpc-authority-restore-controller \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/internal-rpc-authority-restore-operator \
    ./cmd/internal-rpc-authority-restore-operator \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/internal-rpc-authority-restore-recovery \
    ./cmd/internal-rpc-authority-restore-recovery \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/internal-rpc-authority-cli \
    ./cmd/cli

FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35 AS runtime

COPY --from=build --chmod=0555 /out/ /usr/local/bin/
COPY --from=build \
    /workspace/services/internal/internal-rpc-authority/internal/bootstrap/manifest-root/ \
    /usr/local/share/internal-rpc-authority/manifest-root/
COPY --from=build \
    /workspace/services/internal/internal-rpc-authority/internal/bootstrap/readback-root/ \
    /usr/local/share/internal-rpc-authority/readback-root/
COPY --from=build --chmod=0444 \
    /workspace/contracts/authorization/v1/bootstrap-key-delivery-targets.yaml \
    /usr/local/share/internal-rpc-authority/bootstrap-key-delivery-targets.yaml

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/internal-rpc-authority-verifier"]
