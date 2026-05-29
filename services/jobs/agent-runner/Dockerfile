ARG GOLANG_IMAGE=127.0.0.1:5000/kodex/mirror/golang:1.25.8-alpine

FROM ${GOLANG_IMAGE} AS build

WORKDIR /workspace

COPY go.mod go.sum ./
COPY libs/go/ libs/go/
COPY proto/ proto/
COPY services/jobs/agent-runner/ services/jobs/agent-runner/

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/agent-runner ./services/jobs/agent-runner/cmd/agent-runner

FROM scratch AS prod

COPY --from=build /out/agent-runner /kodex/bin/agent-runner

USER 10001:10001

ENTRYPOINT ["/kodex/bin/agent-runner"]
