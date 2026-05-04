ARG GOLANG_IMAGE=127.0.0.1:5000/kodex/mirror/golang:1.25.8-alpine

FROM ${GOLANG_IMAGE} AS tools

RUN go install github.com/pressly/goose/v3/cmd/goose@v3.26.0

FROM ${GOLANG_IMAGE} AS prod

RUN addgroup -S app && adduser -S app -G app

COPY --from=tools /go/bin/goose /usr/local/bin/goose
COPY services/internal/platform-event-log/cmd/cli/migrations/ /migrations/

USER app

ENTRYPOINT ["/usr/local/bin/goose"]
