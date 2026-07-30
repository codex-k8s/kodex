ARG GOLANG_IMAGE=golang:1.26.5-alpine
ARG RUNTIME_IMAGE=alpine:3.22

FROM ${GOLANG_IMAGE} AS build

ENV GOTOOLCHAIN=local

ARG VERSION=dev

WORKDIR /workspace

COPY libs/go/grpcserver/ libs/go/grpcserver/
COPY libs/go/internalrpcauth/ libs/go/internalrpcauth/
COPY libs/go/observability/ libs/go/observability/
COPY libs/go/serviceruntime/ libs/go/serviceruntime/
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
    -o /out/internal-rpc-authority-readback-attestor \
    ./cmd/internal-rpc-authority-readback-attestor \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/internal-rpc-authority-cli \
    ./cmd/cli

FROM ${RUNTIME_IMAGE} AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 authority \
    && adduser -S -D -H -u 65532 -G authority authority

COPY --from=build --chmod=0555 /out/ /usr/local/bin/
COPY --from=build --chmod=0444 \
    /workspace/services/internal/internal-rpc-authority/internal/bootstrap/manifest-root/ \
    /usr/local/share/internal-rpc-authority/manifest-root/
COPY --from=build --chmod=0444 \
    /workspace/services/internal/internal-rpc-authority/internal/bootstrap/readback-root/ \
    /usr/local/share/internal-rpc-authority/readback-root/

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/internal-rpc-authority-verifier"]
