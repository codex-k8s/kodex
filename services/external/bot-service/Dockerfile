ARG GOLANG_IMAGE=golang:1.26-alpine

FROM ${GOLANG_IMAGE} AS build

WORKDIR /workspace

COPY go.mod go.sum ./
COPY libs/go/artifacttype/ libs/go/artifacttype/
COPY libs/go/i18n/ libs/go/i18n/
COPY services/external/bot-service/ services/external/bot-service/

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/bot-service ./services/external/bot-service/cmd/bot-service

FROM ${GOLANG_IMAGE} AS dev

RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app

WORKDIR /workspace

COPY --from=build /go/pkg/mod /go/pkg/mod
COPY --from=build /out/bot-service /usr/local/bin/bot-service
COPY --from=build /root/.cache/go-build /tmp/.cache/go-build
COPY --from=build /workspace/go.mod /workspace/go.sum ./
COPY --from=build /workspace/libs/go/artifacttype/ libs/go/artifacttype/
COPY --from=build /workspace/libs/go/i18n/ libs/go/i18n/
COPY --from=build /workspace/services/external/bot-service/ services/external/bot-service/
RUN chown -R app:app /tmp/.cache

ENV GOCACHE="/tmp/.cache/go-build"

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/bot-service"]

FROM alpine:3.22 AS prod

RUN apk add --no-cache ca-certificates \
	&& addgroup -S -g 10001 app \
	&& adduser -S -D -H -u 10001 -G app app

COPY --from=build /out/bot-service /usr/local/bin/bot-service

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/bot-service"]
