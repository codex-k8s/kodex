ARG GOLANG_IMAGE=127.0.0.1:5000/kodex/mirror/golang:1.25.8-alpine

FROM ${GOLANG_IMAGE} AS build

WORKDIR /workspace

COPY go.mod go.sum ./
COPY libs/go/ libs/go/
COPY proto/ proto/
COPY services/internal/provider-hub/ services/internal/provider-hub/

RUN go mod download
RUN go install github.com/githubnemo/CompileDaemon@v1.4.0
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/provider-hub ./services/internal/provider-hub/cmd/provider-hub

FROM ${GOLANG_IMAGE} AS migration-tools

RUN go install github.com/pressly/goose/v3/cmd/goose@v3.26.0

FROM ${GOLANG_IMAGE} AS dev

RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app

WORKDIR /workspace

COPY --from=build /go/pkg/mod /go/pkg/mod
COPY --from=build /go/bin/CompileDaemon /usr/local/bin/CompileDaemon
COPY --from=build /out/provider-hub /usr/local/bin/provider-hub
COPY --from=build /root/.cache/go-build /tmp/.cache/go-build
COPY --from=build /workspace/go.mod /workspace/go.sum ./
COPY --from=build /workspace/libs/go/ libs/go/
COPY --from=build /workspace/proto/ proto/
COPY --from=build /workspace/services/internal/provider-hub/ services/internal/provider-hub/
RUN chown -R app:app /tmp/.cache

ENV KODEX_HOT_RELOAD="true"
ENV GOCACHE="/tmp/.cache/go-build"

USER 10001:10001

ENTRYPOINT ["sh", "-ec", "if [ \"${KODEX_ENV:-development}\" = \"production\" ] || [ \"${KODEX_HOT_RELOAD:-true}\" = \"false\" ]; then exec /usr/local/bin/provider-hub; fi; exec CompileDaemon --directory=/workspace --build-dir=/workspace --run-dir=/workspace --build='go build -mod=readonly -o /tmp/provider-hub ./services/internal/provider-hub/cmd/provider-hub' --command='/tmp/provider-hub' --polling --graceful-kill"]

FROM ${GOLANG_IMAGE} AS migrations

RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app

COPY --from=migration-tools /go/bin/goose /usr/local/bin/goose
COPY services/internal/provider-hub/cmd/cli/migrations/ /migrations/

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/goose"]

FROM ${GOLANG_IMAGE} AS prod

RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app

COPY --from=build /out/provider-hub /usr/local/bin/provider-hub

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/provider-hub"]
