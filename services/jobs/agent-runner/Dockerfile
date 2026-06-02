ARG GOLANG_IMAGE=127.0.0.1:5000/kodex/mirror/golang:1.25.8-alpine
ARG NODE_IMAGE=127.0.0.1:5000/kodex/mirror/node:22.13.1-alpine
ARG CODEX_CLI_VERSION=0.130.0

FROM ${GOLANG_IMAGE} AS build

WORKDIR /workspace

COPY go.mod go.sum ./
COPY libs/go/ libs/go/
COPY proto/ proto/
COPY services/jobs/agent-runner/ services/jobs/agent-runner/

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/agent-runner ./services/jobs/agent-runner/cmd/agent-runner

FROM ${NODE_IMAGE} AS codex-cli

ARG CODEX_CLI_VERSION

RUN npm install -g --omit=dev "@openai/codex@${CODEX_CLI_VERSION}" \
	&& /usr/local/bin/codex --version \
	&& /usr/local/bin/codex exec --help >/dev/null \
	&& npm cache clean --force

FROM ${NODE_IMAGE} AS prod

RUN addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app

COPY --from=build /out/agent-runner /kodex/bin/agent-runner
COPY --from=codex-cli /usr/local/lib/node_modules/@openai/codex /usr/local/lib/node_modules/@openai/codex
RUN ln -s ../lib/node_modules/@openai/codex/bin/codex.js /usr/local/bin/codex \
	&& /usr/local/bin/codex --version \
	&& /usr/local/bin/codex exec --help >/dev/null

USER 10001:10001

ENTRYPOINT ["/kodex/bin/agent-runner"]
