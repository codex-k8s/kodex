ARG GOLANG_IMAGE=127.0.0.1:5000/kodex/mirror/golang:1.25.8-alpine

FROM ${GOLANG_IMAGE} AS build
WORKDIR /workspace

COPY go.mod go.sum ./
COPY libs/go/ libs/go/
COPY proto/ proto/
COPY services/internal/agent-manager/ services/internal/agent-manager/

RUN go mod download
RUN go install github.com/githubnemo/CompileDaemon@v1.4.0
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/agent-manager ./services/internal/agent-manager/cmd/agent-manager

FROM ${GOLANG_IMAGE} AS migration-tools
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.26.0

FROM ${GOLANG_IMAGE} AS dev
WORKDIR /workspace

COPY go.mod go.sum ./
COPY libs/go/ libs/go/
COPY proto/ proto/
COPY services/internal/agent-manager/ services/internal/agent-manager/
COPY --from=build /go/bin/CompileDaemon /usr/local/bin/CompileDaemon

CMD ["CompileDaemon", "--build=go build -o /tmp/agent-manager ./services/internal/agent-manager/cmd/agent-manager", "--command=/tmp/agent-manager"]

FROM ${GOLANG_IMAGE} AS migrations
COPY --from=migration-tools /go/bin/goose /usr/local/bin/goose
COPY services/internal/agent-manager/cmd/cli/migrations/ /migrations/
ENTRYPOINT ["/usr/local/bin/goose"]

FROM ${GOLANG_IMAGE} AS prod
COPY --from=build /out/agent-manager /usr/local/bin/agent-manager
ENTRYPOINT ["/usr/local/bin/agent-manager"]
